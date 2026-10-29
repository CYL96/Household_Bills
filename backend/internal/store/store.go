package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound        = errors.New("记录不存在")
	ErrInvalidAccount  = errors.New("账户不存在")
	ErrUsernameTaken   = errors.New("用户名已被使用")
	ErrPasswordChanged = errors.New("登录凭据已变更")
)

// Store owns the PostgreSQL connection pool and all SQL access. The household
// identifier is always supplied from the authenticated session and is part of
// every household-scoped query.
type Store struct {
	pool *pgxpool.Pool
}

// Open validates the PostgreSQL connection string and creates a connection pool.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL 格式无效（仅支持 PostgreSQL）")
	}
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "household-bills"
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("创建数据库连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Pool exposes the underlying pool for lifecycle probes and dedicated
// integration-test setup. Application queries live in this package.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

type UserRef struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

type HouseholdRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// User is the authenticated user together with the household context.
type User struct {
	ID            int64
	Username      string
	IsAdmin       bool
	HouseholdID   int64
	HouseholdName string
}

// Me is the session bootstrap payload returned by auth endpoints.
type Me struct {
	User      UserRef      `json:"user"`
	Household HouseholdRef `json:"household"`
	Members   []UserRef    `json:"members"`
}

type Account struct {
	ID                  int64
	Name                string
	OpeningBalanceCents int64
	BalanceCents        int64
}

type Transaction struct {
	ID              int64
	AccountID       int64
	AccountName     string
	Kind            string
	AmountCents     int64
	Category        string
	Date            string
	Note            string
	CreatedBy       int64
	CreatedByName   string
}

type AccountUpdate struct {
	Name           *string
	OpeningBalance *int64
}

type TransactionInput struct {
	AccountID  int64
	Kind       string
	AmountCents int64
	Category   string
	Date       string
	Note       string
}

type TransactionPatch struct {
	AccountID   *int64
	Kind        *string
	AmountCents *int64
	Category    *string
	Date        *string
	Note        *string
}

type TransactionFilters struct {
	MonthStart string
	MonthEnd   string
	AccountID  *int64
	Kind       string
	Query      string
}

type CategoryTotal struct {
	Category    string
	AmountCents int64
}

type DailyTotal struct {
	Date         string
	IncomeCents  int64
	ExpenseCents int64
}

type Summary struct {
	Month             string
	IncomeCents       int64
	ExpenseCents      int64
	NetCents          int64
	TotalBalanceCents int64
	Categories        []CategoryTotal
	Daily             []DailyTotal
}
