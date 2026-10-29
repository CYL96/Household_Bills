package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type dbQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// FindUserByUsername returns the login record for a username. The caller
// performs constant-time-ish bcrypt comparison and must never log the hash.
func (s *Store) FindUserByUsername(ctx context.Context, username string) (User, string, bool, error) {
	var user User
	var passwordHash string
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.is_admin, u.password_hash, h.id, h.name
		FROM users u
		JOIN households h ON h.id = u.household_id
		WHERE u.username = $1`, username).
		Scan(&user.ID, &user.Username, &user.IsAdmin, &passwordHash,
			&user.HouseholdID, &user.HouseholdName)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", false, nil
	}
	if err != nil {
		return User{}, "", false, fmt.Errorf("查询用户失败: %w", err)
	}
	return user, passwordHash, true, nil
}

// SessionUser resolves a hashed session token, enforcing the server-side
// expiry timestamp. Raw session tokens are never stored in PostgreSQL.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte) (User, bool, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.is_admin, h.id, h.name
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN households h ON h.id = u.household_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(&user.ID, &user.Username, &user.IsAdmin,
			&user.HouseholdID, &user.HouseholdName)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("查询会话失败: %w", err)
	}
	return user, true, nil
}

// CreateSession inserts a session only when verifiedPasswordHash is still the
// user's current password hash. The user row is locked FOR UPDATE for the same
// transaction that inserts the session, matching EnsureAdmin's rotation lock.
// A password rotation therefore either commits before this call (the hash is
// stale and ErrPasswordChanged is returned without a session) or after it (the
// rotation deletes the freshly inserted session). The verified hash itself is
// never logged or returned.
func (s *Store) CreateSession(
	ctx context.Context,
	userID int64,
	verifiedPasswordHash string,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启登录会话事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentPasswordHash string
	err = tx.QueryRow(ctx,
		"SELECT password_hash FROM users WHERE id = $1 FOR UPDATE", userID).
		Scan(&currentPasswordHash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrPasswordChanged
	case err != nil:
		return fmt.Errorf("锁定登录用户失败: %w", err)
	case currentPasswordHash != verifiedPasswordHash:
		return ErrPasswordChanged
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`, tokenHash, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交登录会话失败: %w", err)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
	if err != nil {
		return fmt.Errorf("删除会话失败: %w", err)
	}
	return nil
}

func (s *Store) Me(ctx context.Context, userID int64) (Me, error) {
	return loadMe(ctx, s.pool, userID)
}

func loadMe(ctx context.Context, q dbQuerier, userID int64) (Me, error) {
	var me Me
	err := q.QueryRow(ctx, `
		SELECT u.id, u.username, u.is_admin, h.id, h.name
		FROM users u
		JOIN households h ON h.id = u.household_id
		WHERE u.id = $1`, userID).
		Scan(&me.User.ID, &me.User.Username, &me.User.IsAdmin,
			&me.Household.ID, &me.Household.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Me{}, ErrNotFound
	}
	if err != nil {
		return Me{}, fmt.Errorf("读取用户信息失败: %w", err)
	}

	members := make([]UserRef, 0)
	rows, err := q.Query(ctx, `
		SELECT id, username, is_admin
		FROM users
		WHERE household_id = $1
		ORDER BY id`, me.Household.ID)
	if err != nil {
		return Me{}, fmt.Errorf("读取家庭成员失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member UserRef
		if err := rows.Scan(&member.ID, &member.Username, &member.IsAdmin); err != nil {
			return Me{}, fmt.Errorf("读取家庭成员失败: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return Me{}, fmt.Errorf("读取家庭成员失败: %w", err)
	}
	me.Members = members
	return me, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}
