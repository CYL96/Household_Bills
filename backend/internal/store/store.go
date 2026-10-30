package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound                  = errors.New("记录不存在")
	ErrInvalidOwner              = errors.New("归属成员不存在")
	ErrInvalidAccount            = errors.New("账户不存在")
	ErrInvalidTransfer           = errors.New("转入账户无效")
	ErrBalanceOutOfRange         = errors.New("余额超出允许范围")
	ErrTransferAccountNotAllowed = errors.New("非转账交易不能设置转入账户")
	ErrUsernameTaken             = errors.New("用户名已被使用")
	ErrPasswordChanged           = errors.New("登录凭据已变更")
	ErrForbidden                 = errors.New("没有权限执行此操作")
	ErrInvalidCurrentPassword    = errors.New("当前密码错误")
	ErrAdminUsernameFixed        = errors.New("管理员用户名不可修改")
	ErrAdminNotDeletable         = errors.New("管理员账号不可删除")
)

// TransferCategory is the fixed category for transfer transactions. Transfers
// never appear in income or expense statistics, which are filtered by kind.
const TransferCategory = "账户转账"

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
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Nickname      string `json:"nickname"`
	IsAdmin       bool   `json:"is_admin"`
	AvatarVersion int    `json:"avatar_version"`
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
	OwnerID             *int64
	OwnerName           string
}

// Transaction separates the recorder (CreatedBy) from the member the money
// belongs to (OwnerID). Both always reference active or soft-deleted users in
// the same household; CreatedBy never changes after insertion.
type Transaction struct {
	ID                  int64
	AccountID           int64
	AccountName         string
	TransferAccountID   *int64
	TransferAccountName string
	Kind                string
	AmountCents         int64
	Category            string
	Date                string
	Note                string
	CreatedBy           int64
	CreatedByName       string
	OwnerID             int64
	OwnerName           string
}

// AccountUpdate leaves fields nil when they were omitted. Balance carries the
// requested current balance; the store derives the hidden opening balance from
// the ledger. OwnerSet distinguishes an omitted owner from an explicit null,
// since an explicit null must make the wallet public while an omitted field
// preserves the owner.
type AccountUpdate struct {
	Name     *string
	Balance  *int64
	OwnerID  *int64
	OwnerSet bool
}

// TransactionInput describes a new ledger row. A nil OwnerID attributes the
// transaction to the recording user; a non-nil value must be an active member
// of the same household.
type TransactionInput struct {
	AccountID         int64
	TransferAccountID *int64
	Kind              string
	AmountCents       int64
	Category          string
	Date              string
	Note              string
	OwnerID           *int64
}

// TransactionPatch leaves fields nil when they were omitted.
// TransferAccountSet distinguishes an omitted transfer_account from an explicit
// null, since an explicit null must clear the destination while an omitted
// field preserves it. OwnerID nil preserves the current attribution; a non-nil
// value reassigns it without ever changing the recorder.
type TransactionPatch struct {
	AccountID          *int64
	TransferAccountID  *int64
	TransferAccountSet bool
	Kind               *string
	AmountCents        *int64
	Category           *string
	Date               *string
	Note               *string
	OwnerID            *int64
}

type TransactionFilters struct {
	MonthStart string
	MonthEnd   string
	AccountID  *int64
	Kind       string
	Categories []string
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
