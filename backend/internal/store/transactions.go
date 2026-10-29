package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const transactionSelect = `
	SELECT t.id, t.account_id, a.name, t.kind, t.amount_cents,
	       t.category, t.tx_date, t.note, t.created_by, u.username
	FROM transactions t
	JOIN accounts a ON a.id = t.account_id
	JOIN users u ON u.id = t.created_by`

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
			" AND t.tx_date >= %s::date AND t.tx_date < %s::date",
			monthStart, monthEnd)
	}
	if filters.AccountID != nil {
		query += " AND t.account_id = " + addArgument(*filters.AccountID)
	}
	if filters.Kind != "" {
		query += " AND t.kind = " + addArgument(filters.Kind)
	}
	if filters.Query != "" {
		categoryParam := addArgument(filters.Query)
		noteParam := addArgument(filters.Query)
		query += fmt.Sprintf(
			" AND (strpos(lower(t.category), lower(%s)) > 0 OR strpos(lower(t.note), lower(%s)) > 0)",
			categoryParam, noteParam)
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

	var accountName string
	err = tx.QueryRow(ctx,
		"SELECT name FROM accounts WHERE id = $1 AND household_id = $2",
		input.AccountID, householdID).Scan(&accountName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, ErrInvalidAccount
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("查询账户失败: %w", err)
	}

	var transactionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO transactions
			(household_id, account_id, kind, amount_cents, category, tx_date, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8)
		RETURNING id`,
		householdID, input.AccountID, input.Kind, input.AmountCents,
		input.Category, input.Date, input.Note, userID).Scan(&transactionID)
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
		var accountName string
		err := tx.QueryRow(ctx,
			"SELECT name FROM accounts WHERE id = $1 AND household_id = $2",
			*patch.AccountID, householdID).Scan(&accountName)
		if errors.Is(err, pgx.ErrNoRows) {
			return Transaction{}, ErrInvalidAccount
		}
		if err != nil {
			return Transaction{}, fmt.Errorf("查询账户失败: %w", err)
		}
		next.AccountID = *patch.AccountID
		next.AccountName = accountName
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

	_, err = tx.Exec(ctx, `
		UPDATE transactions
		SET account_id = $3,
		    kind = $4,
		    amount_cents = $5,
		    category = $6,
		    tx_date = $7::date,
		    note = $8,
		    updated_at = now()
		WHERE id = $1 AND household_id = $2`,
		transactionID, householdID, next.AccountID, next.Kind,
		next.AmountCents, next.Category, next.Date, next.Note)
	if err != nil {
		return Transaction{}, fmt.Errorf("更新交易失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, fmt.Errorf("提交交易更新失败: %w", err)
	}
	return next, nil
}

func (s *Store) DeleteTransaction(ctx context.Context, householdID, transactionID int64) error {
	tag, err := s.pool.Exec(ctx,
		"DELETE FROM transactions WHERE id = $1 AND household_id = $2",
		transactionID, householdID)
	if err != nil {
		return fmt.Errorf("删除交易失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		&transactionDate, &transaction.Note, &transaction.CreatedBy,
		&transaction.CreatedByName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Transaction{}, err
		}
		return Transaction{}, fmt.Errorf("读取交易失败: %w", err)
	}
	transaction.Date = transactionDate.Format("2006-01-02")
	return transaction, nil
}
