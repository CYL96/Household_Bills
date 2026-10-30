package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

const transactionSelect = `
	SELECT t.id, t.account_id, a.name, t.kind, t.amount_cents,
	       t.category, t.tx_date, t.note,
	       t.transfer_account_id, COALESCE(ta.name, ''),
	       t.created_by, COALESCE(NULLIF(u.nickname, ''), u.username),
	       t.owner_user_id, COALESCE(NULLIF(o.nickname, ''), o.username, '')
	FROM transactions t
	JOIN accounts a ON a.id = t.account_id
	LEFT JOIN accounts ta ON ta.id = t.transfer_account_id
	JOIN users u ON u.id = t.created_by
	LEFT JOIN users o ON o.id = t.owner_user_id`

func (s *Store) ListTransactions(ctx context.Context, householdID int64, filters TransactionFilters) ([]Transaction, error) {
	query := transactionSelect + " WHERE t.household_id = $1"
	args := []any{householdID}
	addArgument := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if filters.MonthStart != "" && filters.MonthEnd != "" {
		monthStart := addArgument(filters.MonthStart)
		monthEnd := addArgument(filters.MonthEnd)
		query += fmt.Sprintf(
			" AND t.tx_date >= %s::timestamp AND t.tx_date < %s::timestamp",
			monthStart, monthEnd)
	}
	if filters.AccountID != nil {
		accountParam := addArgument(*filters.AccountID)
		query += fmt.Sprintf(
			" AND (t.account_id = %s OR t.transfer_account_id = %s)",
			accountParam, accountParam)
	}
	if filters.Kind != "" {
		query += " AND t.kind = " + addArgument(filters.Kind)
	}
	if filters.Query != "" {
		categoryParam := addArgument(filters.Query)
		noteParam := addArgument(filters.Query)
		transferNameParam := addArgument(filters.Query)
		query += fmt.Sprintf(
			" AND (strpos(lower(t.category), lower(%s)) > 0"+
				" OR strpos(lower(t.note), lower(%s)) > 0"+
				" OR strpos(lower(COALESCE(ta.name, '')), lower(%s)) > 0)",
			categoryParam, noteParam, transferNameParam)
	}
	if len(filters.Categories) > 0 {
		query += " AND t.category = ANY(" + addArgument(filters.Categories) + "::text[])"
	}

	query += " ORDER BY t.tx_date DESC, t.id DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取交易失败: %w", err)
	}
	defer rows.Close()

	transactions := make([]Transaction, 0)
	for rows.Next() {
		transaction, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取交易失败: %w", err)
	}
	return transactions, nil
}

func (s *Store) CreateTransaction(ctx context.Context, householdID, userID int64, input TransactionInput) (Transaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Transaction{}, fmt.Errorf("开启交易事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transferAccountID := input.TransferAccountID
	accountIDs := [2]int64{input.AccountID}
	accountCount := 1
	if input.Kind == "transfer" && transferAccountID != nil &&
		*transferAccountID > 0 && *transferAccountID != input.AccountID {
		accountIDs[accountCount] = *transferAccountID
		accountCount++
	}
	missingID, err := lockAccounts(ctx, tx, householdID, accountIDs[:accountCount]...)
	if errors.Is(err, ErrInvalidAccount) {
		if missingID == input.AccountID {
			return Transaction{}, ErrInvalidAccount
		}
		return Transaction{}, ErrInvalidTransfer
	}
	if err != nil {
		return Transaction{}, err
	}

	ownerID := userID
	if input.OwnerID != nil {
		ownerID = *input.OwnerID
	}
	if _, err := lockActiveTransactionOwner(ctx, tx, householdID, ownerID); err != nil {
		return Transaction{}, err
	}

	if input.Kind == "transfer" {
		if transferAccountID == nil || *transferAccountID <= 0 ||
			*transferAccountID == input.AccountID {
			return Transaction{}, ErrInvalidTransfer
		}
		input.Category = TransferCategory
	} else if transferAccountID != nil {
		return Transaction{}, ErrTransferAccountNotAllowed
	}

	var transactionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO transactions
			(household_id, account_id, kind, amount_cents, category, tx_date, note, created_by, owner_user_id, transfer_account_id)
		VALUES ($1, $2, $3, $4, $5, $6::timestamp, $7, $8, $9, $10)
		RETURNING id`,
		householdID, input.AccountID, input.Kind, input.AmountCents,
		input.Category, input.Date, input.Note, userID, ownerID, transferAccountID).Scan(&transactionID)
	if err != nil {
		return Transaction{}, fmt.Errorf("创建交易失败: %w", err)
	}

	transaction, err := getTransaction(ctx, tx, transactionID, householdID, false)
	if err != nil {
		return Transaction{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, fmt.Errorf("提交交易失败: %w", err)
	}
	return transaction, nil
}

func (s *Store) UpdateTransaction(ctx context.Context, householdID, transactionID int64, patch TransactionPatch) (Transaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Transaction{}, fmt.Errorf("开启交易更新事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := getTransaction(ctx, tx, transactionID, householdID, true)
	if err != nil {
		return Transaction{}, err
	}

	next := current
	if patch.AccountID != nil {
		next.AccountID = *patch.AccountID
	}
	if patch.Kind != nil {
		next.Kind = *patch.Kind
	}
	if patch.AmountCents != nil {
		next.AmountCents = *patch.AmountCents
	}
	if patch.Category != nil {
		next.Category = *patch.Category
	}
	if patch.Date != nil {
		next.Date = *patch.Date
	}
	if patch.Note != nil {
		next.Note = *patch.Note
	}
	if patch.TransferAccountSet {
		next.TransferAccountID = patch.TransferAccountID
		next.TransferAccountName = ""
	}
	// Resending the current owner must remain possible after that member was
	// soft-deleted, so only a changed assignment is validated below.
	ownerChanged := false
	if patch.OwnerID != nil && *patch.OwnerID != current.OwnerID {
		next.OwnerID = *patch.OwnerID
		next.OwnerName = ""
		ownerChanged = true
	}
	if next.Kind == "transfer" {
		if next.TransferAccountID == nil || *next.TransferAccountID <= 0 ||
			*next.TransferAccountID == next.AccountID {
			return Transaction{}, ErrInvalidTransfer
		}
		next.Category = TransferCategory
	} else {
		if patch.TransferAccountSet && patch.TransferAccountID != nil {
			return Transaction{}, ErrTransferAccountNotAllowed
		}
		next.TransferAccountID = nil
		next.TransferAccountName = ""
	}

	accountIDs := [4]int64{current.AccountID, next.AccountID}
	accountCount := 2
	if current.TransferAccountID != nil {
		accountIDs[accountCount] = *current.TransferAccountID
		accountCount++
	}
	if next.TransferAccountID != nil {
		accountIDs[accountCount] = *next.TransferAccountID
		accountCount++
	}
	missingID, err := lockAccounts(ctx, tx, householdID, accountIDs[:accountCount]...)
	if errors.Is(err, ErrInvalidAccount) {
		if missingID == current.AccountID || missingID == next.AccountID {
			return Transaction{}, ErrInvalidAccount
		}
		return Transaction{}, ErrInvalidTransfer
	}
	if err != nil {
		return Transaction{}, err
	}
	if ownerChanged {
		ownerName, err := lockActiveTransactionOwner(
			ctx, tx, householdID, next.OwnerID)
		if err != nil {
			return Transaction{}, err
		}
		next.OwnerName = ownerName
	}

	if patch.AccountID != nil {
		var accountName string
		err := tx.QueryRow(ctx,
			"SELECT name FROM accounts WHERE id = $1 AND household_id = $2",
			next.AccountID, householdID).Scan(&accountName)
		if errors.Is(err, pgx.ErrNoRows) {
			return Transaction{}, ErrInvalidAccount
		}
		if err != nil {
			return Transaction{}, fmt.Errorf("查询账户失败: %w", err)
		}
		next.AccountName = accountName
	}
	if next.Kind == "transfer" {
		transferAccountName, err := resolveTransferAccountName(
			ctx, tx, householdID, next.AccountID, next.TransferAccountID)
		if err != nil {
			return Transaction{}, err
		}
		next.TransferAccountName = transferAccountName
	}

	_, err = tx.Exec(ctx, `
		UPDATE transactions
		SET account_id = $3,
		    kind = $4,
		    amount_cents = $5,
		    category = $6,
		    tx_date = $7::timestamp,
		    note = $8,
		    owner_user_id = $9,
		    transfer_account_id = $10,
		    updated_at = now()
		WHERE id = $1 AND household_id = $2`,
		transactionID, householdID, next.AccountID, next.Kind,
		next.AmountCents, next.Category, next.Date, next.Note,
		next.OwnerID, next.TransferAccountID)
	if err != nil {
		return Transaction{}, fmt.Errorf("更新交易失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, fmt.Errorf("提交交易更新失败: %w", err)
	}
	return next, nil
}

func (s *Store) DeleteTransaction(ctx context.Context, householdID, transactionID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启交易删除事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := getTransaction(ctx, tx, transactionID, householdID, true)
	if err != nil {
		return err
	}

	accountIDs := [2]int64{current.AccountID}
	accountCount := 1
	if current.TransferAccountID != nil {
		accountIDs[accountCount] = *current.TransferAccountID
		accountCount++
	}
	if _, err := lockAccounts(ctx, tx, householdID, accountIDs[:accountCount]...); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		"DELETE FROM transactions WHERE id = $1 AND household_id = $2",
		transactionID, householdID)
	if err != nil {
		return fmt.Errorf("删除交易失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交交易删除失败: %w", err)
	}
	return nil
}

// lockAccounts locks the affected wallets in ascending id order before a
// transaction mutation changes ledger rows. Every balance-changing writer uses
// the same order, so wallet rows serialize balance corrections without
// deadlocks or retries. It returns the id of the first missing wallet so the
// caller can map the failure to the right request field.
func lockAccounts(
	ctx context.Context,
	q dbQuerier,
	householdID int64,
	accountIDs ...int64,
) (int64, error) {
	sort.Slice(accountIDs, func(i, j int) bool {
		return accountIDs[i] < accountIDs[j]
	})
	for i, accountID := range accountIDs {
		if i > 0 && accountID == accountIDs[i-1] {
			continue
		}
		var lockedID int64
		err := q.QueryRow(ctx, `
			SELECT id FROM accounts
			WHERE id = $1 AND household_id = $2
			FOR UPDATE`, accountID, householdID).Scan(&lockedID)
		if errors.Is(err, pgx.ErrNoRows) {
			return accountID, ErrInvalidAccount
		}
		if err != nil {
			return 0, fmt.Errorf("锁定账户失败: %w", err)
		}
	}
	return 0, nil
}

// lockActiveTransactionOwner resolves the display name of any active member of
// the household, including the administrator, while holding a shared lock on
// the row. Transactions are attributed to a member rather than to a wallet, so
// unlike account ownership the administrator is eligible. Soft deletion takes
// an exclusive lock, so a concurrent deletion can never leave a new
// attribution pointing at a member that was already gone.
func lockActiveTransactionOwner(
	ctx context.Context,
	q dbQuerier,
	householdID, ownerID int64,
) (string, error) {
	var name string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(nickname, ''), username)
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		FOR SHARE`, ownerID, householdID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidOwner
	}
	if err != nil {
		return "", fmt.Errorf("校验交易归属成员失败: %w", err)
	}
	return name, nil
}

// resolveTransferAccountName returns the destination name after validating
// that it differs from the source and belongs to the same household. The
// caller holds the destination row lock, so the read cannot race a rename.
func resolveTransferAccountName(
	ctx context.Context,
	q dbQuerier,
	householdID, accountID int64,
	transferAccountID *int64,
) (string, error) {
	if transferAccountID == nil || *transferAccountID <= 0 ||
		*transferAccountID == accountID {
		return "", ErrInvalidTransfer
	}
	var name string
	err := q.QueryRow(ctx,
		"SELECT name FROM accounts WHERE id = $1 AND household_id = $2",
		*transferAccountID, householdID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidTransfer
	}
	if err != nil {
		return "", fmt.Errorf("查询转入账户失败: %w", err)
	}
	return name, nil
}

func getTransaction(ctx context.Context, q dbQuerier, transactionID, householdID int64, forUpdate bool) (Transaction, error) {
	query := transactionSelect + " WHERE t.id = $1 AND t.household_id = $2"
	if forUpdate {
		query += " FOR UPDATE OF t"
	}
	transaction, err := scanTransaction(q.QueryRow(ctx, query, transactionID, householdID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, ErrNotFound
	}
	return transaction, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransaction(row rowScanner) (Transaction, error) {
	var transaction Transaction
	var transactionDate time.Time
	err := row.Scan(
		&transaction.ID, &transaction.AccountID, &transaction.AccountName,
		&transaction.Kind, &transaction.AmountCents, &transaction.Category,
		&transactionDate, &transaction.Note,
		&transaction.TransferAccountID, &transaction.TransferAccountName,
		&transaction.CreatedBy, &transaction.CreatedByName,
		&transaction.OwnerID, &transaction.OwnerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Transaction{}, err
		}
		return Transaction{}, fmt.Errorf("读取交易失败: %w", err)
	}
	transaction.Date = transactionDate.Format("2006-01-02T15:04:05")
	return transaction, nil
}
