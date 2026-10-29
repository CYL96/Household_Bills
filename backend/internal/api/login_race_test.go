package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"household-bills/backend/internal/credentials"
	"household-bills/backend/internal/store"
)

// TestAdminPasswordRotationRejectsStaleLoginSession reproduces the login /
// password-rotation race sequentially: a login reads and verifies the old hash,
// EnsureAdmin rotates the password and revokes sessions, and the stale login
// must not be able to create a new session afterwards.
func TestAdminPasswordRotationRejectsStaleLoginSession(t *testing.T) {
	env := newTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, staleHash, found, err := env.db.FindUserByUsername(ctx, adminUsername)
	if err != nil {
		t.Fatalf("读取管理员登录记录失败: %v", err)
	}
	if !found {
		t.Fatal("应找到引导管理员")
	}
	if staleHash == "" {
		t.Fatal("登录记录应包含用于验证的密码哈希")
	}

	// A login that completed before the rotation is revoked with that
	// administrator's other sessions.
	preRotationToken := []byte("pre-rotation-session-token")
	if err := env.db.CreateSession(
		ctx, user.ID, staleHash, preRotationToken, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("创建轮换前会话失败: %v", err)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", user.ID); got != 1 {
		t.Fatalf("轮换前会话数量 = %d，期望 1", got)
	}

	rotated, err := env.ensureAdminResult(t, rotatedPassword)
	if err != nil {
		t.Fatalf("轮换管理员密码失败: %v", err)
	}
	if !rotated.PasswordRotated || rotated.Created {
		t.Fatalf("轮换结果错误: %#v", rotated)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", user.ID); got != 0 {
		t.Fatalf("轮换后会话数量 = %d，期望 0", got)
	}

	// This models a request that already verified staleHash before the
	// rotation committed. It must not insert a session after revocation.
	staleToken := []byte("stale-verified-session-token")
	err = env.db.CreateSession(
		ctx, user.ID, staleHash, staleToken, time.Now().Add(time.Hour))
	if !errors.Is(err, store.ErrPasswordChanged) {
		t.Fatalf("旧密码哈希创建会话错误 = %v，期望 store.ErrPasswordChanged", err)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", user.ID); got != 0 {
		t.Fatalf("轮换后旧密码哈希不应创建会话，实际 %d", got)
	}

	// The old password is rejected with the ordinary generic credentials
	// error, and the rotated password logs in normally.
	oldLogin := newTestClient(t, env)
	oldResponse := oldLogin.do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": adminUsername,
		"password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	if detail := mustString(t, oldResponse.object(t)["detail"]); detail != "用户名或密码错误。" {
		t.Fatalf("旧密码登录 detail = %q，期望通用凭据错误", detail)
	}
	env.loginUser(t, adminUsername, rotatedPassword)
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", user.ID); got != 1 {
		t.Fatalf("新密码登录后会话数量 = %d，期望 1", got)
	}
}

// TestAdminPasswordRotationRaceBlocksStaleLogin exercises the lock ordering
// deterministically. A rotation transaction holds the administrator row lock
// with the new hash uncommitted; the login request reads the old committed
// hash, verifies it, then blocks in CreateSession on that same row lock. Once
// the rotation commits, the blocked login must not create a session and must
// receive the ordinary generic credentials error.
func TestAdminPasswordRotationRaceBlocksStaleLogin(t *testing.T) {
	env := newTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, staleHash, found, err := env.db.FindUserByUsername(ctx, adminUsername)
	if err != nil {
		t.Fatalf("读取管理员登录记录失败: %v", err)
	}
	if !found {
		t.Fatal("应找到引导管理员")
	}
	newHash, err := credentials.HashPassword(rotatedPassword)
	if err != nil {
		t.Fatalf("生成轮换密码哈希失败: %v", err)
	}

	// Emulate EnsureAdmin's rotation transaction up to the commit while holding
	// the same SELECT ... FOR UPDATE row lock it uses.
	rotation, err := env.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("开启轮换事务失败: %v", err)
	}
	defer func() { _ = rotation.Rollback(ctx) }()

	var adminID int64
	var lockedHash string
	if err := rotation.QueryRow(ctx, `
		SELECT id, password_hash
		FROM users
		WHERE is_admin
		ORDER BY id
		LIMIT 1
		FOR UPDATE`).Scan(&adminID, &lockedHash); err != nil {
		t.Fatalf("锁定管理员行失败: %v", err)
	}
	if lockedHash != staleHash {
		t.Fatal("锁定的管理员密码哈希与登录读取的哈希不一致")
	}
	if _, err := rotation.Exec(ctx,
		"UPDATE users SET password_hash = $2 WHERE id = $1 AND is_admin",
		adminID, newHash); err != nil {
		t.Fatalf("更新管理员密码失败: %v", err)
	}
	if _, err := rotation.Exec(ctx,
		"DELETE FROM sessions WHERE user_id = $1", adminID); err != nil {
		t.Fatalf("清理管理员会话失败: %v", err)
	}

	client := newRawClient(t, env)
	client.bootstrap(t)
	loginDone := make(chan loginRaceResult, 1)
	go func() {
		loginDone <- attemptLogin(client, adminUsername, strongPassword)
	}()

	waitForBlockedLogin(t, env)

	if err := rotation.Commit(ctx); err != nil {
		t.Fatalf("提交密码轮换事务失败: %v", err)
	}

	var result loginRaceResult
	select {
	case result = <-loginDone:
	case <-ctx.Done():
		t.Fatal("等待登录请求完成超时")
	}
	if result.err != nil {
		t.Fatalf("登录请求失败: %v", result.err)
	}
	if result.status != http.StatusBadRequest {
		t.Fatalf("过期哈希登录状态码 = %d，期望 %d；响应: %s",
			result.status, http.StatusBadRequest, string(result.body))
	}
	if result.sessionToken != "" {
		t.Fatal("过期哈希登录不应设置 session cookie")
	}
	var payload map[string]any
	if err := json.Unmarshal(result.body, &payload); err != nil {
		t.Fatalf("登录响应不是 JSON 对象: %v；响应: %s", err, string(result.body))
	}
	if detail := mustString(t, payload["detail"]); detail != "用户名或密码错误。" {
		t.Fatalf("过期哈希登录 detail = %q，期望通用凭据错误", detail)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", adminID); got != 0 {
		t.Fatalf("竞态后旧哈希不应创建会话，实际 %d", got)
	}

	var currentHash string
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT password_hash FROM users WHERE id = $1", adminID).
		Scan(&currentHash); err != nil {
		t.Fatalf("读取轮换后密码哈希失败: %v", err)
	}
	if currentHash != newHash {
		t.Fatal("轮换事务未提交新密码哈希")
	}

	// The rotated password still logs in normally.
	env.loginUser(t, adminUsername, rotatedPassword)
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions WHERE user_id = $1", adminID); got != 1 {
		t.Fatalf("新密码登录后会话数量 = %d，期望 1", got)
	}
}

type loginRaceResult struct {
	status       int
	body         []byte
	sessionToken string
	err          error
}

func attemptLogin(client *testClient, username, password string) loginRaceResult {
	payload, err := json.Marshal(map[string]any{
		"username": username,
		"password": password,
	})
	if err != nil {
		return loginRaceResult{err: err}
	}
	request, err := http.NewRequest(
		http.MethodPost, client.base+"/api/auth/login/", bytes.NewReader(payload))
	if err != nil {
		return loginRaceResult{err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", client.base)
	request.Header.Set("X-CSRFToken", client.cookieValue("csrftoken"))

	response, err := client.client.Do(request)
	if err != nil {
		return loginRaceResult{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return loginRaceResult{err: err}
	}
	result := loginRaceResult{status: response.StatusCode, body: body}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "sessionid" {
			result.sessionToken = cookie.Value
		}
	}
	return result
}

// waitForBlockedLogin waits until the concurrent login is visibly waiting on a
// PostgreSQL lock, which proves it already verified the old hash and reached
// the FOR UPDATE in CreateSession. The rotation transaction can then commit.
func waitForBlockedLogin(t *testing.T, env *testEnv) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var waiters int
		err := env.db.Pool().QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND state = 'active'`).Scan(&waiters)
		cancel()
		if err != nil {
			t.Fatalf("检查登录锁等待失败: %v", err)
		}
		if waiters > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("登录请求未阻塞在用户行锁上")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
