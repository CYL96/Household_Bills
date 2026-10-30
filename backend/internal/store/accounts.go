package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"household-bills/backend/internal/money"
)

// accountNetExpression sums every ledger row that changes the wallet aliased
// "a". It is shared by the derived balance and the opening-balance correction
// so both always apply identical transfer semantics. Household scoping comes
// from the wallet itself instead of an extra parameter.
const accountNetExpression = `
	COALESCE((
		SELECT SUM(CASE
		               WHEN t.kind = 'income' THEN t.amount_cents
		               WHEN t.kind = 'transfer' AND t.transfer_account_id = a.id
		                   THEN t.amount_cents
		               ELSE -t.amount_cents
		           END)
		FROM transactions t
		WHERE t.household_id = a.household_id
		  AND (t.account_id = a.id OR t.transfer_account_id = a.id)
	), 0)::numeric`

const accountBalanceExpression = `(a.opening_balance_cents::numeric + ` +
	accountNetExpression + `)`

// accountSelect reads one wallet with its derived balance and optional owner
// label. The user join deliberately keeps soft-deleted members so a departed
// member's wallet still renders its owner.
const accountSelect = `
	SELECT a.id, a.name, a.opening_balance_cents,
	       ` + accountBalanceExpression + `::text,
	       a.owner_user_id,
	       COALESCE(NULLIF(u.nickname, ''), u.username, '')
	FROM accounts a
	LEFT JOIN users u ON u.id = a.owner_user_id`

func (s *Store) ListAccounts(ctx context.Context, householdID int64) ([]Account, error) {
	rows, err := s.pool.Query(ctx, accountSelect+`
		WHERE a.household_id = $1
		ORDER BY a.id`, householdID)
	if err != nil {
		return nil, fmt.Errorf("读取账户失败: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("读取账户失败: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取账户失败: %w", err)
	}
	return accounts, nil
}

// CreateAccount inserts a wallet whose opening balance starts at the requested
// current balance. A non-nil owner must be an active non-admin member of the
// same household; the shared row lock makes a concurrent soft deletion wait
// until this transaction commits.
func (s *Store) CreateAccount(
	ctx context.Context,
	householdID int64,
	name string,
	balanceCents int64,
	ownerID *int64,
) (Account, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("开启账户创建事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ownerArg any
	var ownerName string
	if ownerID != nil {
		ownerName, err = lockActiveAccountOwner(ctx, tx, householdID, *ownerID)
		if err != nil {
			return Account{}, err
		}
		ownerArg = *ownerID
	}

	var account Account
	err = tx.QueryRow(ctx, `
		INSERT INTO accounts (household_id, name, opening_balance_cents, owner_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, opening_balance_cents, owner_user_id`,
		householdID, name, balanceCents, ownerArg).
		Scan(&account.ID, &account.Name, &account.OpeningBalanceCents, &account.OwnerID)
	if err != nil {
		return Account{}, fmt.Errorf("创建账户失败: %w", err)
	}
	// A new wallet has no transactions yet, so its balance is its opening balance.
	account.BalanceCents = balanceCents
	account.OwnerName = ownerName

	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("提交账户创建失败: %w", err)
	}
	return account, nil
}

// UpdateAccount applies a household-scoped patch. When Balance is set, the
// wallet row is locked first and the ledger contribution is recomputed under
// that lock, so the hidden opening balance stores exactly the requested
// current balance minus the contribution.
func (s *Store) UpdateAccount(ctx context.Context, householdID, accountID int64, update AccountUpdate) (Account, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("开启账户更新事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Scope the wallet before validating the owner so another household's id
	// still produces the same not-found response as before.
	var existingID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM accounts
		WHERE id = $1 AND household_id = $2
		FOR UPDATE`, accountID, householdID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("锁定账户失败: %w", err)
	}

	var nameArg any
	if update.Name != nil {
		nameArg = *update.Name
	}
	var ownerArg any
	if update.OwnerSet && update.OwnerID != nil {
		if _, err := lockActiveAccountOwner(ctx, tx, householdID, *update.OwnerID); err != nil {
			return Account{}, err
		}
		ownerArg = *update.OwnerID
	}

	var openingArg any
	if update.Balance != nil {
		balance := *update.Balance
		if balance < -money.MaxCents || balance > money.MaxCents {
			return Account{}, ErrBalanceOutOfRange
		}
		netCents, err := accountNetCents(ctx, tx, householdID, accountID)
		if err != nil {
			return Account{}, err
		}
		// Compare before subtracting so an extreme net can never overflow int64.
		if netCents > balance+money.MaxCents || netCents < balance-money.MaxCents {
			return Account{}, ErrBalanceOutOfRange
		}
		openingArg = balance - netCents
	}

	_, err = tx.Exec(ctx, `
		UPDATE accounts
		SET name = COALESCE($3::text, name),
		    opening_balance_cents = COALESCE($4::bigint, opening_balance_cents),
		    owner_user_id = CASE WHEN $5::boolean THEN $6::bigint ELSE owner_user_id END,
		    updated_at = now()
		WHERE id = $1 AND household_id = $2`,
		accountID, householdID, nameArg, openingArg, update.OwnerSet, ownerArg)
	if err != nil {
		return Account{}, fmt.Errorf("更新账户失败: %w", err)
	}

	account, err := scanAccount(tx.QueryRow(ctx, accountSelect+`
		WHERE a.id = $1 AND a.household_id = $2`, accountID, householdID))
	if err != nil {
		return Account{}, fmt.Errorf("读取账户失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("提交账户更新失败: %w", err)
	}
	return account, nil
}

// accountNetCents reads the ledger contribution that the derived balance adds
// to the stored opening balance. Callers must hold the wallet row lock so the
// result cannot change before the correction is written.
func accountNetCents(
	ctx context.Context,
	q dbQuerier,
	householdID, accountID int64,
) (int64, error) {
	var netText string
	err := q.QueryRow(ctx, `
		SELECT `+accountNetExpression+`::text
		FROM accounts a
		WHERE a.id = $1 AND a.household_id = $2`,
		accountID, householdID).Scan(&netText)
	if err != nil {
		return 0, fmt.Errorf("读取账户收支失败: %w", err)
	}
	netCents, err := parseAggregateCents(netText)
	if err != nil {
		// A ledger sum outside int64 can never yield an in-range opening for
		// any user-requestable current balance, so the correction itself is
		// unrepresentable rather than an internal failure.
		return 0, ErrBalanceOutOfRange
	}
	return netCents, nil
}

// lockActiveAccountOwner resolves the display name of an active non-admin
// member of the household while holding a shared lock on the row. Soft
// deletion takes an exclusive lock, so a concurrent deletion can never leave a
// new assignment pointing at a member that was already gone. Administrators
// are not eligible for new assignments; legacy admin-owned wallets remain
// readable and editable when their owner is omitted.
func lockActiveAccountOwner(
	ctx context.Context,
	q dbQuerier,
	householdID, ownerID int64,
) (string, error) {
	var name string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(nickname, ''), username)
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		  AND NOT is_admin
		FOR SHARE`, ownerID, householdID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidOwner
	}
	if err != nil {
		return "", fmt.Errorf("校验账户归属成员失败: %w", err)
	}
	return name, nil
}

func scanAccount(row rowScanner) (Account, error) {
	var account Account
	var balance string
	if err := row.Scan(
		&account.ID, &account.Name, &account.OpeningBalanceCents,
		&balance, &account.OwnerID, &account.OwnerName); err != nil {
		return Account{}, err
	}
	cents, err := parseAggregateCents(balance)
	if err != nil {
		return Account{}, err
	}
	account.BalanceCents = cents
	return account, nil
}
