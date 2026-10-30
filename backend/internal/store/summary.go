package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetSummary computes all monthly figures and the all-time household balance
// inside one repeatable-read snapshot. No cached balances are used.
func (s *Store) GetSummary(ctx context.Context, householdID int64, month, monthStart, monthEnd string) (Summary, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("开启汇总事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	summary := Summary{
		Month:      month,
		Categories: make([]CategoryTotal, 0),
		Daily:      make([]DailyTotal, 0),
	}

	var incomeText, expenseText string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'income'), 0)::text,
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'expense'), 0)::text
		FROM transactions
		WHERE household_id = $1
		  AND tx_date >= $2::timestamp
		  AND tx_date < $3::timestamp`,
		householdID, monthStart, monthEnd).Scan(&incomeText, &expenseText)
	if err != nil {
		return Summary{}, fmt.Errorf("读取月度收支失败: %w", err)
	}
	summary.IncomeCents, err = parseAggregateCents(incomeText)
	if err != nil {
		return Summary{}, fmt.Errorf("读取月度收入失败: %w", err)
	}
	summary.ExpenseCents, err = parseAggregateCents(expenseText)
	if err != nil {
		return Summary{}, fmt.Errorf("读取月度支出失败: %w", err)
	}
	summary.NetCents = summary.IncomeCents - summary.ExpenseCents

	var totalText string
	err = tx.QueryRow(ctx, `
		SELECT (
			COALESCE((SELECT SUM(opening_balance_cents) FROM accounts WHERE household_id = $1), 0)::numeric
			+ COALESCE((SELECT SUM(CASE
			                        WHEN kind = 'income' THEN amount_cents
			                        WHEN kind = 'expense' THEN -amount_cents
			                        ELSE 0
			                      END)
			            FROM transactions WHERE household_id = $1), 0)::numeric
		)::text`, householdID).Scan(&totalText)
	if err != nil {
		return Summary{}, fmt.Errorf("读取总余额失败: %w", err)
	}
	summary.TotalBalanceCents, err = parseAggregateCents(totalText)
	if err != nil {
		return Summary{}, fmt.Errorf("读取总余额失败: %w", err)
	}

	categoryRows, err := tx.Query(ctx, `
		SELECT category, SUM(amount_cents)::text
		FROM transactions
		WHERE household_id = $1
		  AND kind = 'expense'
		  AND tx_date >= $2::timestamp
		  AND tx_date < $3::timestamp
		GROUP BY category
		ORDER BY SUM(amount_cents) DESC, category ASC`,
		householdID, monthStart, monthEnd)
	if err != nil {
		return Summary{}, fmt.Errorf("读取支出分类失败: %w", err)
	}
	for categoryRows.Next() {
		var item CategoryTotal
		var amountText string
		if err := categoryRows.Scan(&item.Category, &amountText); err != nil {
			categoryRows.Close()
			return Summary{}, fmt.Errorf("读取支出分类失败: %w", err)
		}
		item.AmountCents, err = parseAggregateCents(amountText)
		if err != nil {
			categoryRows.Close()
			return Summary{}, fmt.Errorf("读取支出分类失败: %w", err)
		}
		summary.Categories = append(summary.Categories, item)
	}
	categoryRows.Close()
	if err := categoryRows.Err(); err != nil {
		return Summary{}, fmt.Errorf("读取支出分类失败: %w", err)
	}

	dailyRows, err := tx.Query(ctx, `
		SELECT tx_date::date,
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'income'), 0)::text,
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'expense'), 0)::text
		FROM transactions
		WHERE household_id = $1
		  AND kind IN ('income', 'expense')
		  AND tx_date >= $2::timestamp
		  AND tx_date < $3::timestamp
		GROUP BY tx_date::date
		ORDER BY tx_date::date ASC`,
		householdID, monthStart, monthEnd)
	if err != nil {
		return Summary{}, fmt.Errorf("读取每日收支失败: %w", err)
	}
	for dailyRows.Next() {
		var item DailyTotal
		var date time.Time
		var income, expense string
		if err := dailyRows.Scan(&date, &income, &expense); err != nil {
			dailyRows.Close()
			return Summary{}, fmt.Errorf("读取每日收支失败: %w", err)
		}
		item.Date = date.Format("2006-01-02")
		item.IncomeCents, err = parseAggregateCents(income)
		if err != nil {
			dailyRows.Close()
			return Summary{}, fmt.Errorf("读取每日收入失败: %w", err)
		}
		item.ExpenseCents, err = parseAggregateCents(expense)
		if err != nil {
			dailyRows.Close()
			return Summary{}, fmt.Errorf("读取每日支出失败: %w", err)
		}
		summary.Daily = append(summary.Daily, item)
	}
	dailyRows.Close()
	if err := dailyRows.Err(); err != nil {
		return Summary{}, fmt.Errorf("读取每日收支失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("提交汇总事务失败: %w", err)
	}
	return summary, nil
}
