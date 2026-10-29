package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const accountBalanceExpression = `
	(a.opening_balance_cents::numeric + COALESCE((
		SELECT SUM(CASE WHEN t.kind = 'income' THEN t.amount_cents ELSE -t.amount_cents END)
		FROM transactions t
		WHERE t.account_id = a.id
	), 0)::numeric)`

func (s *Store) ListAccounts(ctx context.Context, householdID int64) ([]Account, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.opening_balance_cents,
		       `+accountBalanceExpression+`::text
		FROM accounts a
		WHERE a.household_id = $1
		ORDER BY a.id`, householdID)
	if err != nil {
		return nil, fmt.Errorf("读取账户失败: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		var account Account
		var balance string
		if err := rows.Scan(&account.ID, &account.Name,
			&account.OpeningBalanceCents, &balance); err != nil {
			return nil, fmt.Errorf("读取账户失败: %w", err)
		}
		account.BalanceCents, err = parseAggregateCents(balance)
		if err != nil {
			return nil, fmt.Errorf("读取账户余额失败: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取账户失败: %w", err)
	}
	return accounts, nil
}

func (s *Store) CreateAccount(ctx context.Context, householdID int64, name string, openingBalanceCents int64) (Account, error) {
	var account Account
	err := s.pool.QueryRow(ctx, `
		INSERT INTO accounts (household_id, name, opening_balance_cents)
		VALUES ($1, $2, $3)
		RETURNING id, name, opening_balance_cents`,
		householdID, name, openingBalanceCents).
		Scan(&account.ID, &account.Name, &account.OpeningBalanceCents)
	if err != nil {
		return Account{}, fmt.Errorf("创建账户失败: %w", err)
	}
	account.BalanceCents = openingBalanceCents
	return account, nil
}

func (s *Store) UpdateAccount(ctx context.Context, householdID, accountID int64, update AccountUpdate) (Account, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("开启账户更新事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var nameArg any
	if update.Name != nil {
		nameArg = *update.Name
	}
	var openingArg any
	if update.OpeningBalance != nil {
		openingArg = *update.OpeningBalance
	}

	var account Account
	err = tx.QueryRow(ctx, `
		UPDATE accounts
		SET name = COALESCE($3::text, name),
		    opening_balance_cents = COALESCE($4::bigint, opening_balance_cents),
		    updated_at = now()
		WHERE id = $1 AND household_id = $2
		RETURNING id, name, opening_balance_cents`,
		accountID, householdID, nameArg, openingArg).
		Scan(&account.ID, &account.Name, &account.OpeningBalanceCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("更新账户失败: %w", err)
	}

	var balance string
	err = tx.QueryRow(ctx, `
		SELECT `+accountBalanceExpression+`::text
		FROM accounts a
		WHERE a.id = $1 AND a.household_id = $2`, accountID, householdID).Scan(&balance)
	if err != nil {
		return Account{}, fmt.Errorf("读取账户余额失败: %w", err)
	}
	account.BalanceCents, err = parseAggregateCents(balance)
	if err != nil {
		return Account{}, fmt.Errorf("读取账户余额失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("提交账户更新失败: %w", err)
	}
	return account, nil
}
