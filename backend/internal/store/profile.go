package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"household-bills/backend/internal/credentials"
)

// UserProfileUpdate carries the already-normalized fields an actor may change.
// PasswordHash is set only when a new password was validated and hashed by the
// caller. CurrentPassword is required whenever an actor changes their own
// password, and is verified under the target row lock.
type UserProfileUpdate struct {
	Username        *string
	Nickname        *string
	PasswordHash    *string
	CurrentPassword string
}

// UpdateUser updates a profile visible to the actor. Self-service and
// same-household administrator access are both allowed; ordinary members may
// only modify themselves. The target row is locked so a concurrent password
// change or account deletion cannot interleave.
func (s *Store) UpdateUser(
	ctx context.Context,
	actor User,
	targetID int64,
	update UserProfileUpdate,
) (UserRef, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRef{}, fmt.Errorf("开启用户更新事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var user UserRef
	var passwordHash string
	err = tx.QueryRow(ctx, `
		SELECT id, username, nickname, is_admin, avatar_version, password_hash
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		FOR UPDATE`, targetID, actor.HouseholdID).
		Scan(&user.ID, &user.Username, &user.Nickname, &user.IsAdmin,
			&user.AvatarVersion, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRef{}, ErrNotFound
	}
	if err != nil {
		return UserRef{}, fmt.Errorf("锁定用户失败: %w", err)
	}
	if !actor.IsAdmin && actor.ID != user.ID {
		return UserRef{}, ErrForbidden
	}
	if user.IsAdmin && update.Username != nil && *update.Username != user.Username {
		return UserRef{}, ErrAdminUsernameFixed
	}
	if update.PasswordHash != nil && actor.ID == user.ID {
		if !credentials.PasswordMatches(passwordHash, update.CurrentPassword) {
			return UserRef{}, ErrInvalidCurrentPassword
		}
	}

	username := user.Username
	if update.Username != nil {
		username = *update.Username
	}
	nickname := user.Nickname
	if update.Nickname != nil {
		nickname = *update.Nickname
	}
	var passwordArg any
	if update.PasswordHash != nil {
		passwordArg = *update.PasswordHash
	}

	err = tx.QueryRow(ctx, `
		UPDATE users
		SET username = $2,
		    nickname = $3,
		    password_hash = COALESCE($4::text, password_hash)
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, username, nickname, is_admin, avatar_version`,
		targetID, username, nickname, passwordArg).
		Scan(&user.ID, &user.Username, &user.Nickname, &user.IsAdmin,
			&user.AvatarVersion)
	if isUniqueViolation(err, "users_username_key") {
		return UserRef{}, ErrUsernameTaken
	}
	if err != nil {
		return UserRef{}, fmt.Errorf("更新用户失败: %w", err)
	}

	if update.PasswordHash != nil {
		if _, err := tx.Exec(ctx,
			"DELETE FROM sessions WHERE user_id = $1", targetID); err != nil {
			return UserRef{}, fmt.Errorf("清理用户会话失败: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRef{}, fmt.Errorf("提交用户更新失败: %w", err)
	}
	return user, nil
}

// SetUserAvatar replaces a profile image. The raw bytes and the fixed MIME
// type detected by the caller are stored together; the row lock makes the
// version increment atomic.
func (s *Store) SetUserAvatar(
	ctx context.Context,
	actor User,
	targetID int64,
	data []byte,
	contentType string,
) (UserRef, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRef{}, fmt.Errorf("开启头像更新事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var target UserRef
	err = tx.QueryRow(ctx, `
		SELECT id, is_admin
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		FOR UPDATE`, targetID, actor.HouseholdID).
		Scan(&target.ID, &target.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRef{}, ErrNotFound
	}
	if err != nil {
		return UserRef{}, fmt.Errorf("锁定用户失败: %w", err)
	}
	if !actor.IsAdmin && actor.ID != target.ID {
		return UserRef{}, ErrForbidden
	}

	var user UserRef
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET avatar_data = $2,
		    avatar_type = $3,
		    avatar_version = avatar_version + 1
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, username, nickname, is_admin, avatar_version`,
		targetID, data, contentType).
		Scan(&user.ID, &user.Username, &user.Nickname, &user.IsAdmin,
			&user.AvatarVersion)
	if err != nil {
		return UserRef{}, fmt.Errorf("更新头像失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRef{}, fmt.Errorf("提交头像更新失败: %w", err)
	}
	return user, nil
}

// UserAvatar returns the stored image bytes for a member of the supplied
// household. Deleted accounts and accounts without an avatar are not found.
func (s *Store) UserAvatar(
	ctx context.Context,
	householdID int64,
	targetID int64,
) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := s.pool.QueryRow(ctx, `
		SELECT avatar_data, avatar_type
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		  AND avatar_data IS NOT NULL AND avatar_type IS NOT NULL`,
		targetID, householdID).Scan(&data, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("读取头像失败: %w", err)
	}
	return data, contentType, nil
}

// DeleteUser soft-deletes an ordinary member of the actor's household and
// invalidates every session immediately. Ledger history keeps its user row
// join, and the username remains reserved by the unique constraint.
func (s *Store) DeleteUser(
	ctx context.Context,
	actor User,
	targetID int64,
) error {
	if !actor.IsAdmin {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启用户删除事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var isAdmin bool
	err = tx.QueryRow(ctx, `
		SELECT is_admin
		FROM users
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL
		FOR UPDATE`, targetID, actor.HouseholdID).Scan(&isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("锁定用户失败: %w", err)
	}
	if isAdmin {
		return ErrAdminNotDeletable
	}

	tag, err := tx.Exec(ctx, `
		UPDATE users
		SET deleted_at = now()
		WHERE id = $1 AND household_id = $2 AND deleted_at IS NULL`,
		targetID, actor.HouseholdID)
	if err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		"DELETE FROM sessions WHERE user_id = $1", targetID); err != nil {
		return fmt.Errorf("清理用户会话失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交用户删除失败: %w", err)
	}
	return nil
}
