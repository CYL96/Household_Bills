package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"household-bills/backend/internal/credentials"
)

const (
	// BootstrapAdminUsername is the only provisioned administrator identity.
	BootstrapAdminUsername = "admin"
	// BootstrapHouseholdName is created for the bootstrap administrator. Users
	// created by the administrator join this household.
	BootstrapHouseholdName = "家庭账本"

	// bootstrapLockID is a dedicated transaction-scoped advisory lock key,
	// distinct from the migration lock. It serializes startup initialization
	// across concurrently starting application instances.
	bootstrapLockID int64 = 7_206_211_919_111
)

// BootstrapResult reports what startup changed for the administrator account.
// It never carries password material.
type BootstrapResult struct {
	Created         bool
	PasswordRotated bool
}

// EnsureAdmin atomically initializes the single bootstrap administrator. The
// admin is identified by is_admin, never by username, so an existing ordinary
// account cannot be promoted. A supplied password is compared with the stored
// bcrypt hash; only a real change rewrites the hash and deletes that admin's
// sessions. An empty password leaves an existing admin untouched so restarts
// do not require ADMIN_PASSWORD once the account exists.
func (s *Store) EnsureAdmin(ctx context.Context, password string) (BootstrapResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("开启管理员初始化事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLockID); err != nil {
		return BootstrapResult{}, fmt.Errorf("获取管理员初始化锁失败: %w", err)
	}

	var adminID int64
	var passwordHash string
	err = tx.QueryRow(ctx, `
		SELECT id, password_hash
		FROM users
		WHERE is_admin
		ORDER BY id
		LIMIT 1
		FOR UPDATE`).Scan(&adminID, &passwordHash)
	switch {
	case err == nil:
		if password == "" {
			return BootstrapResult{}, nil
		}
		if message := credentials.PasswordValidationError(password); message != "" {
			return BootstrapResult{}, fmt.Errorf("ADMIN_PASSWORD 无效：%s", message)
		}
		if credentials.PasswordMatches(passwordHash, password) {
			return BootstrapResult{}, nil
		}

		newHash, err := credentials.HashPassword(password)
		if err != nil {
			return BootstrapResult{}, fmt.Errorf("生成管理员密码哈希失败: %w", err)
		}
		tag, err := tx.Exec(ctx,
			"UPDATE users SET password_hash = $2 WHERE id = $1 AND is_admin",
			adminID, newHash)
		if err != nil {
			return BootstrapResult{}, fmt.Errorf("更新管理员密码失败: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return BootstrapResult{}, errors.New("更新管理员密码时发生并发冲突，请重试")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", adminID); err != nil {
			return BootstrapResult{}, fmt.Errorf("清理管理员会话失败: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return BootstrapResult{}, fmt.Errorf("提交管理员密码更新失败: %w", err)
		}
		return BootstrapResult{PasswordRotated: true}, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Continue below and create the bootstrap admin.
	default:
		return BootstrapResult{}, fmt.Errorf("查询管理员账号失败: %w", err)
	}

	if password == "" {
		return BootstrapResult{}, errors.New(
			"首次启动必须设置环境变量 ADMIN_PASSWORD 以初始化管理员账号（至少 10 个字符）")
	}
	if message := credentials.PasswordValidationError(password); message != "" {
		return BootstrapResult{}, fmt.Errorf("ADMIN_PASSWORD 无效：%s", message)
	}

	// Never promote or overwrite an existing ordinary account that owns the
	// bootstrap username; fail closed and leave it untouched.
	var collidingUserID int64
	err = tx.QueryRow(ctx,
		"SELECT id FROM users WHERE username = $1 FOR UPDATE",
		BootstrapAdminUsername).Scan(&collidingUserID)
	switch {
	case err == nil:
		return BootstrapResult{}, fmt.Errorf(
			"用户名 %q 已被普通账号占用，未将其提升为管理员；请先处理该账号后再启动",
			BootstrapAdminUsername)
	case !errors.Is(err, pgx.ErrNoRows):
		return BootstrapResult{}, fmt.Errorf("检查管理员用户名冲突失败: %w", err)
	}

	household, err := createHousehold(ctx, tx, BootstrapHouseholdName)
	if err != nil {
		return BootstrapResult{}, err
	}

	newHash, err := credentials.HashPassword(password)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("生成管理员密码哈希失败: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO users (username, password_hash, household_id, is_admin)
		VALUES ($1, $2, $3, true)`, BootstrapAdminUsername, newHash, household.ID)
	switch {
	case isUniqueViolation(err, "users_username_key"):
		return BootstrapResult{}, fmt.Errorf(
			"用户名 %q 已被普通账号占用，未将其提升为管理员；请先处理该账号后再启动",
			BootstrapAdminUsername)
	case isUniqueViolation(err, "users_single_admin_key"):
		return BootstrapResult{}, errors.New("管理员账号已被其他进程初始化，请重试")
	case err != nil:
		return BootstrapResult{}, fmt.Errorf("创建管理员账号失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("提交管理员初始化失败: %w", err)
	}
	return BootstrapResult{Created: true}, nil
}

// ListUsers returns every account in one household, including the
// administrator. Callers pass the household from the authenticated session.
func (s *Store) ListUsers(ctx context.Context, householdID int64) ([]UserRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, is_admin
		FROM users
		WHERE household_id = $1
		ORDER BY id`, householdID)
	if err != nil {
		return nil, fmt.Errorf("读取用户列表失败: %w", err)
	}
	defer rows.Close()

	users := make([]UserRef, 0)
	for rows.Next() {
		var user UserRef
		if err := rows.Scan(&user.ID, &user.Username, &user.IsAdmin); err != nil {
			return nil, fmt.Errorf("读取用户列表失败: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取用户列表失败: %w", err)
	}
	return users, nil
}

// CreateUser inserts a non-admin account into the caller's household. It never
// creates a session, so the administrator's current session is unaffected.
func (s *Store) CreateUser(
	ctx context.Context,
	householdID int64,
	username string,
	passwordHash string,
) (UserRef, error) {
	var user UserRef
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, household_id, is_admin)
		VALUES ($1, $2, $3, false)
		RETURNING id, username, is_admin`,
		username, passwordHash, householdID).
		Scan(&user.ID, &user.Username, &user.IsAdmin)
	if isUniqueViolation(err, "users_username_key") {
		return UserRef{}, ErrUsernameTaken
	}
	if err != nil {
		return UserRef{}, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

func createHousehold(ctx context.Context, tx pgx.Tx, name string) (HouseholdRef, error) {
	var household HouseholdRef
	err := tx.QueryRow(ctx, `
		INSERT INTO households (name)
		VALUES ($1)
		RETURNING id, name`, name).
		Scan(&household.ID, &household.Name)
	if err != nil {
		return HouseholdRef{}, fmt.Errorf("创建家庭失败: %w", err)
	}
	return household, nil
}
