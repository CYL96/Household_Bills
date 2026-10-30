package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"household-bills/backend/internal/api"
	"household-bills/backend/internal/config"
	"household-bills/backend/internal/credentials"
	"household-bills/backend/internal/store"
)

const (
	strongPassword  = "correct horse battery staple"
	rotatedPassword = "another strong passphrase"

	adminUsername      = store.BootstrapAdminUsername
	adminHouseholdName = store.BootstrapHouseholdName
)

type testEnv struct {
	server *httptest.Server
	db     *store.Store
}

type testResponse struct {
	status int
	body   []byte
}

type testClient struct {
	t      *testing.T
	base   string
	client *http.Client
	jar    http.CookieJar
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithAdmin(t, true)
}

func newTestEnvWithoutAdmin(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithAdmin(t, false)
}

func newTestEnvWithAdmin(t *testing.T, withAdmin bool) *testEnv {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过 PostgreSQL 集成测试")
	}
	if appURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); appURL != "" && appURL == databaseURL {
		t.Fatal("TEST_DATABASE_URL 必须指向专用测试数据库，不能与 DATABASE_URL 相同")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		t.Fatalf("执行测试数据库迁移失败: %v", err)
	}
	// TEST_DATABASE_URL must point at a dedicated test database; these tests
	// intentionally truncate their own tables and never touch app data elsewhere.
	if _, err := database.Pool().Exec(ctx,
		"TRUNCATE TABLE household_category_settings, sessions, transactions, accounts, users, households RESTART IDENTITY CASCADE"); err != nil {
		database.Close()
		t.Fatalf("清理测试数据库失败: %v", err)
	}
	t.Cleanup(database.Close)

	env := &testEnv{db: database}
	if withAdmin {
		env.ensureAdmin(t, strongPassword)
	}

	handler := api.New(config.Config{
		CookieSecure: false,
		FrontendDist: "",
	}, database, log.New(io.Discard, "", 0))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	env.server = server
	return env
}

func newRawClient(t *testing.T, env *testEnv) *testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar 失败: %v", err)
	}
	return &testClient{
		t:      t,
		base:   env.server.URL,
		client: &http.Client{Jar: jar, Timeout: 10 * time.Second},
		jar:    jar,
	}
}

func newTestClient(t *testing.T, env *testEnv) *testClient {
	t.Helper()
	client := newRawClient(t, env)
	client.bootstrap(t)
	return client
}

func (e *testEnv) adminClient(t *testing.T) *testClient {
	t.Helper()
	return e.loginUser(t, adminUsername, strongPassword)
}

func (e *testEnv) loginUser(t *testing.T, username, password string) *testClient {
	t.Helper()
	client := newTestClient(t, e)
	client.login(t, username, password)
	return client
}

func (e *testEnv) ensureAdmin(t *testing.T, password string) store.BootstrapResult {
	t.Helper()
	result, err := e.ensureAdminResult(t, password)
	if err != nil {
		t.Fatalf("初始化管理员失败: %v", err)
	}
	return result
}

func (e *testEnv) ensureAdminResult(t *testing.T, password string) (store.BootstrapResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.db.EnsureAdmin(ctx, password)
}

// seedUser creates an isolated household and one account directly so tests can
// model multiple households without the removed public registration API.
func (e *testEnv) seedUser(
	t *testing.T,
	householdName, username, password string,
	isAdmin bool,
) (householdID int64, userID int64) {
	t.Helper()
	passwordHash, err := credentials.HashPassword(password)
	if err != nil {
		t.Fatalf("生成测试密码哈希失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := e.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("开启测试数据事务失败: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx,
		"INSERT INTO households (name) VALUES ($1) RETURNING id",
		householdName).Scan(&householdID); err != nil {
		t.Fatalf("创建测试家庭失败: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, household_id, is_admin)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		username, passwordHash, householdID, isAdmin).Scan(&userID); err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交测试数据失败: %v", err)
	}
	return householdID, userID
}

func (c *testClient) bootstrap(t *testing.T) map[string]any {
	t.Helper()
	payload := c.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if user, exists := payload["user"]; !exists || user != nil {
		t.Fatalf("匿名 me 应返回 user:null，实际: %s", mustJSON(t, payload))
	}
	if c.cookieValue("csrftoken") == "" {
		t.Fatal("GET /api/auth/me/ 未设置 csrftoken cookie")
	}
	return payload
}

func (c *testClient) login(t *testing.T, username, password string) map[string]any {
	t.Helper()
	payload := c.object(t, http.MethodPost, "/api/auth/login/", map[string]any{
		"username": username,
		"password": password,
	}, http.StatusOK)
	if c.cookieValue("sessionid") == "" {
		t.Fatal("登录成功后应设置 session cookie")
	}
	return payload
}

func (c *testClient) createUser(t *testing.T, username, password string) map[string]any {
	t.Helper()
	return c.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username": username,
		"password": password,
	}, http.StatusCreated)
}

func (c *testClient) do(method, requestPath string, body any, mutations ...func(*http.Request)) testResponse {
	c.t.Helper()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("序列化请求失败: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.base+requestPath, requestBody)
	if err != nil {
		c.t.Fatalf("创建请求失败: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", c.base)
	if method != http.MethodGet && method != http.MethodHead {
		if token := c.cookieValue("csrftoken"); token != "" {
			request.Header.Set("X-CSRFToken", token)
		}
	}
	for _, mutate := range mutations {
		mutate(request)
	}

	response, err := c.client.Do(request)
	if err != nil {
		c.t.Fatalf("请求 %s %s 失败: %v", method, requestPath, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatalf("读取响应失败: %v", err)
	}
	return testResponse{status: response.StatusCode, body: data}
}

// removeCookie simulates a browser-side cookie expiry for the named cookie.
func (c *testClient) removeCookie(t *testing.T, name string) {
	t.Helper()
	u, err := url.Parse(c.base)
	if err != nil {
		t.Fatalf("解析测试服务器地址失败: %v", err)
	}
	c.jar.SetCookies(u, []*http.Cookie{{Name: name, Path: "/", MaxAge: -1}})
	if value := c.cookieValue(name); value != "" {
		t.Fatalf("删除 cookie %s 失败，仍为 %q", name, value)
	}
}

func (c *testClient) cookieValue(name string) string {
	u, err := url.Parse(c.base)
	if err != nil {
		return ""
	}
	for _, cookie := range c.jar.Cookies(u) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func (r testResponse) expect(t *testing.T, want int) testResponse {
	t.Helper()
	if r.status != want {
		t.Fatalf("状态码 = %d，期望 %d；响应: %s", r.status, want, string(r.body))
	}
	return r
}

func (r testResponse) object(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(r.body, &value); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v；响应: %s", err, string(r.body))
	}
	return value
}

func (r testResponse) array(t *testing.T) []map[string]any {
	t.Helper()
	var value []map[string]any
	if err := json.Unmarshal(r.body, &value); err != nil {
		t.Fatalf("响应不是 JSON 数组: %v；响应: %s", err, string(r.body))
	}
	return value
}

func (c *testClient) object(t *testing.T, method, requestPath string, body any, want int, mutations ...func(*http.Request)) map[string]any {
	t.Helper()
	response := c.do(method, requestPath, body, mutations...).expect(t, want)
	return response.object(t)
}

func (c *testClient) array(t *testing.T, method, requestPath string, body any, want int, mutations ...func(*http.Request)) []map[string]any {
	t.Helper()
	response := c.do(method, requestPath, body, mutations...).expect(t, want)
	return response.array(t)
}

func (e *testEnv) count(t *testing.T, table string) int {
	t.Helper()
	return e.scalarInt(t, "SELECT count(*) FROM "+table)
}

func (e *testEnv) scalarInt(t *testing.T, query string, args ...any) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var value int
	if err := e.db.Pool().QueryRow(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("查询计数失败: %v", err)
	}
	return value
}

func jsonInt64(t *testing.T, value any) int64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("字段不是数字: %#v", value)
	}
	return int64(number)
}

func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("字段不是对象: %#v", value)
	}
	return object
}

func mustSlice(t *testing.T, value any) []any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("字段不是数组: %#v", value)
	}
	return items
}

func mustString(t *testing.T, value any) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("字段不是字符串: %#v", value)
	}
	return text
}

func mustBool(t *testing.T, value any) bool {
	t.Helper()
	flag, ok := value.(bool)
	if !ok {
		t.Fatalf("字段不是布尔值: %#v", value)
	}
	return flag
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(encoded)
}

func withoutCSRF(request *http.Request) {
	request.Header.Del("X-CSRFToken")
	request.Header.Del("X-CSRF-Token")
}

func withOrigin(value string) func(*http.Request) {
	return func(request *http.Request) {
		request.Header.Set("Origin", value)
	}
}

func withoutOrigin(request *http.Request) {
	request.Header.Del("Origin")
}

func replaySession(t *testing.T, base, token string) testResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, base+"/api/auth/me/", nil)
	if err != nil {
		t.Fatalf("创建会话重放请求失败: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "sessionid", Value: token})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("重放旧会话失败: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取会话重放响应失败: %v", err)
	}
	return testResponse{status: response.StatusCode, body: body}
}

func TestAuthSessionAndRemovedRegistration(t *testing.T) {
	env := newTestEnv(t)
	client := newRawClient(t, env)
	me := client.bootstrap(t)
	if me["user"] != nil {
		t.Fatalf("匿名 me 应为 null: %s", mustJSON(t, me))
	}

	// Registration is gone even with valid CSRF cookies and Origin; the old
	// request shape must not create users or households.
	removed := client.do(http.MethodPost, "/api/auth/register/", map[string]any{
		"username":       "alice",
		"password":       strongPassword,
		"household_name": "甲家",
		"invite_code":    "",
	}).expect(t, http.StatusNotFound)
	if detail := mustString(t, removed.object(t)["detail"]); detail != "接口不存在。" {
		t.Fatalf("注册路由 detail = %q，期望 接口不存在。", detail)
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("注册路由不应创建用户，实际 %d", got)
	}
	if got := env.count(t, "households"); got != 1 {
		t.Fatalf("注册路由不应创建家庭，实际 %d", got)
	}

	// Login is CSRF protected even though it is anonymous.
	loginBody := map[string]any{"username": adminUsername, "password": strongPassword}
	loginClient := newTestClient(t, env)
	loginClient.do(http.MethodPost, "/api/auth/login/", loginBody, withoutCSRF).
		expect(t, http.StatusForbidden)
	loginClient.do(http.MethodPost, "/api/auth/login/", loginBody,
		withOrigin("https://evil.example")).expect(t, http.StatusForbidden)
	loginClient.do(http.MethodPost, "/api/auth/login/", loginBody, withoutOrigin).
		expect(t, http.StatusForbidden)

	loginClient.do(http.MethodPost, "/api/auth/login/",
		map[string]any{"username": adminUsername, "password": "wrong password"}).
		expect(t, http.StatusBadRequest)

	me = loginClient.login(t, adminUsername, strongPassword)
	user := mustMap(t, me["user"])
	if !mustBool(t, user["is_admin"]) {
		t.Fatalf("管理员 me 应包含 is_admin:true: %s", mustJSON(t, me))
	}
	household := mustMap(t, me["household"])
	if _, exists := household["invite_code"]; exists {
		t.Fatalf("家庭信息不应再暴露 invite_code: %s", mustJSON(t, household))
	}
	members := mustSlice(t, me["members"])
	if len(members) != 1 || !mustBool(t, mustMap(t, members[0])["is_admin"]) {
		t.Fatalf("管理员家庭应有 1 名管理员成员: %s", mustJSON(t, me))
	}
	adminID := jsonInt64(t, user["id"])

	// Server-side expiry is enforced on the next request.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := env.db.Pool().Exec(ctx,
		"UPDATE sessions SET expires_at = now() - interval '1 second' WHERE user_id = $1", adminID)
	cancel()
	if err != nil {
		t.Fatalf("设置会话过期失败: %v", err)
	}
	me = loginClient.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if me["user"] != nil {
		t.Fatalf("过期会话应视为匿名: %s", mustJSON(t, me))
	}
	// Protected endpoints report the expired session as 401 so the frontend
	// can clear stale authentication instead of treating it as a CSRF error.
	loginClient.do(http.MethodGet, "/api/accounts/", nil).expect(t, http.StatusUnauthorized)

	// Re-login, verify logout CSRF, then log out successfully.
	loginClient.login(t, adminUsername, strongPassword)
	oldSession := loginClient.cookieValue("sessionid")
	if oldSession == "" {
		t.Fatal("重新登录后缺少 session cookie")
	}
	loginClient.do(http.MethodPost, "/api/auth/logout/", nil, withoutCSRF).
		expect(t, http.StatusForbidden)
	loginClient.do(http.MethodPost, "/api/auth/logout/", nil).
		expect(t, http.StatusNoContent)
	me = loginClient.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if me["user"] != nil {
		t.Fatal("退出登录后 me 应为 null")
	}

	// The server-side session row must also be invalidated, not just the cookie.
	replayed := replaySession(t, loginClient.base, oldSession)
	replayed.expect(t, http.StatusOK)
	if replayed.object(t)["user"] != nil {
		t.Fatal("退出登录后旧 session cookie 不应再可用")
	}

	notFound := loginClient.do(http.MethodGet, "/api/no-such-endpoint/", nil).
		expect(t, http.StatusNotFound)
	if _, exists := notFound.object(t)["detail"]; !exists {
		t.Fatalf("未知 API 路径应返回 JSON detail: %s", string(notFound.body))
	}
}

func TestProtectedRequestsClassifyMissingSessionBeforeCSRF(t *testing.T) {
	env := newTestEnv(t)
	accountBody := map[string]any{"name": "不应创建", "balance": "0.00"}

	// A protected mutation with neither a session nor a CSRF token must be
	// reported as unauthenticated so clients can clear stale auth state.
	anonymous := newRawClient(t, env)
	response := anonymous.do(http.MethodPost, "/api/accounts/", accountBody)
	response.expect(t, http.StatusUnauthorized)
	if detail := mustString(t, response.object(t)["detail"]); detail != "请先登录。" {
		t.Fatalf("未登录响应 detail = %q，期望 请先登录。", detail)
	}
	anonymous.do(http.MethodGet, "/api/users/", nil).expect(t, http.StatusUnauthorized)
	anonymous.do(http.MethodPost, "/api/users/", map[string]any{
		"username": "alice", "password": strongPassword,
	}).expect(t, http.StatusUnauthorized)
	if got := env.count(t, "accounts"); got != 0 {
		t.Fatalf("未登录请求不应创建账户，实际 %d", got)
	}

	// Anonymous auth endpoints still enforce CSRF with 403.
	anonymous.do(http.MethodPost, "/api/auth/login/",
		map[string]any{"username": adminUsername, "password": strongPassword}, withoutCSRF).
		expect(t, http.StatusForbidden)

	// An expired server session and a client-expired CSRF cookie arriving
	// together on a protected mutation must also be 401, not 403.
	expired := env.adminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := env.db.Pool().Exec(ctx,
		"UPDATE sessions SET expires_at = now() - interval '1 second'")
	cancel()
	if err != nil {
		t.Fatalf("设置会话过期失败: %v", err)
	}
	expired.removeCookie(t, "csrftoken")
	response = expired.do(http.MethodPost, "/api/accounts/", accountBody)
	response.expect(t, http.StatusUnauthorized)
	if detail := mustString(t, response.object(t)["detail"]); detail != "请先登录。" {
		t.Fatalf("过期会话响应 detail = %q，期望 请先登录。", detail)
	}
	if got := env.count(t, "accounts"); got != 0 {
		t.Fatalf("过期会话请求不应创建账户，实际 %d", got)
	}

	// A valid session without CSRF is still a CSRF failure.
	active := env.adminClient(t)
	active.removeCookie(t, "csrftoken")
	active.do(http.MethodPost, "/api/accounts/", accountBody).
		expect(t, http.StatusForbidden)
}

func TestAdminUserManagement(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminUser := mustMap(t, adminMe["user"])
	adminID := jsonInt64(t, adminUser["id"])
	adminHouseholdID := jsonInt64(t, mustMap(t, adminMe["household"])["id"])
	if !mustBool(t, adminUser["is_admin"]) {
		t.Fatal("引导管理员应带有 is_admin:true")
	}

	// Anonymous access is 401, not 403, so clients know to log in.
	anonymous := newTestClient(t, env)
	anonymous.do(http.MethodGet, "/api/users/", nil).expect(t, http.StatusUnauthorized)
	anonymous.do(http.MethodPost, "/api/users/", map[string]any{
		"username": "alice", "password": strongPassword,
	}).expect(t, http.StatusUnauthorized)

	adminSession := admin.cookieValue("sessionid")
	if adminSession == "" {
		t.Fatal("管理员登录后缺少 session cookie")
	}

	// CSRF checks still guard the admin mutation.
	admin.do(http.MethodPost, "/api/users/",
		map[string]any{"username": "csrf", "password": strongPassword}, withoutCSRF).
		expect(t, http.StatusForbidden)
	admin.do(http.MethodPost, "/api/users/",
		map[string]any{"username": "csrf", "password": strongPassword},
		withOrigin("https://evil.example")).expect(t, http.StatusForbidden)

	invalidUsers := []struct {
		name     string
		username string
		password string
		field    string
	}{
		{"空用户名", "", strongPassword, "username"},
		{"非法用户名", "bad name", strongPassword, "username"},
		{"短密码", "shorty", "short", "password"},
		{"超长密码", "longy", strings.Repeat("a", 73), "password"},
	}
	for _, testCase := range invalidUsers {
		response := admin.do(http.MethodPost, "/api/users/", map[string]any{
			"username": testCase.username,
			"password": testCase.password,
		})
		if response.status != http.StatusBadRequest {
			t.Fatalf("%s 状态码 = %d，期望 400；响应: %s",
				testCase.name, response.status, string(response.body))
		}
		if _, exists := response.object(t)[testCase.field]; !exists {
			t.Fatalf("%s 应返回 %s 字段错误: %s",
				testCase.name, testCase.field, string(response.body))
		}
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("校验失败不应创建用户，实际 %d", got)
	}

	// Injected role or household fields must not grant privileges or choose a
	// different household; the request struct cannot bind them.
	injected := admin.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username":     "mallory",
		"password":     strongPassword,
		"is_admin":     true,
		"household_id": 999999,
	}, http.StatusCreated)
	if mustBool(t, injected["is_admin"]) {
		t.Fatalf("注入的 is_admin 不应生效: %s", mustJSON(t, injected))
	}

	aliceCreated := admin.createUser(t, "alice", strongPassword)
	if mustString(t, aliceCreated["username"]) != "alice" ||
		mustBool(t, aliceCreated["is_admin"]) {
		t.Fatalf("创建用户响应错误: %s", mustJSON(t, aliceCreated))
	}

	duplicate := admin.do(http.MethodPost, "/api/users/", map[string]any{
		"username": "alice", "password": strongPassword,
	})
	duplicate.expect(t, http.StatusBadRequest)
	if _, exists := duplicate.object(t)["username"]; !exists {
		t.Fatalf("重复用户名应返回 username 错误: %s", string(duplicate.body))
	}
	if got := env.count(t, "users"); got != 3 {
		t.Fatalf("用户数量 = %d，期望 3", got)
	}

	// Every creation landed in the caller's household with no administrator role.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var aliceHouseholdID int64
	var aliceIsAdmin bool
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT household_id, is_admin FROM users WHERE username = $1", "alice").
		Scan(&aliceHouseholdID, &aliceIsAdmin); err != nil {
		t.Fatalf("读取 alice 失败: %v", err)
	}
	if aliceHouseholdID != adminHouseholdID || aliceIsAdmin {
		t.Fatalf("alice household=%d is_admin=%t，期望 household=%d is_admin=false",
			aliceHouseholdID, aliceIsAdmin, adminHouseholdID)
	}
	var malloryHouseholdID int64
	var malloryIsAdmin bool
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT household_id, is_admin FROM users WHERE username = $1", "mallory").
		Scan(&malloryHouseholdID, &malloryIsAdmin); err != nil {
		t.Fatalf("读取 mallory 失败: %v", err)
	}
	if malloryHouseholdID != adminHouseholdID || malloryIsAdmin {
		t.Fatalf("mallory household=%d is_admin=%t，期望 household=%d is_admin=false",
			malloryHouseholdID, malloryIsAdmin, adminHouseholdID)
	}

	// The list includes the administrator and marks roles correctly.
	users := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if len(users) != 3 {
		t.Fatalf("用户列表应有 3 人: %s", mustJSON(t, users))
	}
	roles := make(map[string]bool, len(users))
	for _, item := range users {
		roles[mustString(t, item["username"])] = mustBool(t, item["is_admin"])
	}
	if isAdmin, exists := roles[adminUsername]; !exists || !isAdmin {
		t.Fatalf("用户列表缺少管理员: %s", mustJSON(t, users))
	}
	if isAdmin, exists := roles["alice"]; !exists || isAdmin {
		t.Fatalf("alice 应为普通用户: %s", mustJSON(t, users))
	}
	if isAdmin, exists := roles["mallory"]; !exists || isAdmin {
		t.Fatalf("mallory 应为普通用户: %s", mustJSON(t, users))
	}

	// Creating users must not replace or invalidate the administrator session.
	if admin.cookieValue("sessionid") != adminSession {
		t.Fatal("创建用户不应替换管理员会话")
	}
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, adminMe["user"])["id"]) != adminID ||
		!mustBool(t, mustMap(t, adminMe["user"])["is_admin"]) {
		t.Fatalf("创建用户后管理员身份错误: %s", mustJSON(t, adminMe))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 1 {
		t.Fatalf("创建用户不应产生或替换会话，实际会话数 %d", got)
	}

	// The created account can log in, sees the same household, and shares the ledger.
	alice := env.loginUser(t, "alice", strongPassword)
	aliceMe := alice.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	aliceUser := mustMap(t, aliceMe["user"])
	if mustBool(t, aliceUser["is_admin"]) {
		t.Fatal("alice 不应是管理员")
	}
	if jsonInt64(t, mustMap(t, aliceMe["household"])["id"]) != adminHouseholdID {
		t.Fatalf("alice 应加入管理员家庭: %s", mustJSON(t, aliceMe))
	}
	if got := len(mustSlice(t, aliceMe["members"])); got != 3 {
		t.Fatalf("管理员家庭应有 3 名成员，实际 %d", got)
	}
	account := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "家庭账户", "balance": "10.00"}, http.StatusCreated)
	accountID := jsonInt64(t, account["id"])
	aliceAccounts := alice.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if len(aliceAccounts) != 1 || jsonInt64(t, aliceAccounts[0]["id"]) != accountID {
		t.Fatalf("家庭成员应共享账户: %s", mustJSON(t, aliceAccounts))
	}

	// Ordinary users are forbidden from listing or creating users, and the
	// rejected request must not create anything.
	beforeForbidden := env.count(t, "users")
	response := alice.do(http.MethodGet, "/api/users/", nil)
	response.expect(t, http.StatusForbidden)
	if detail := mustString(t, response.object(t)["detail"]); detail != "没有权限执行此操作。" {
		t.Fatalf("普通用户列表 detail = %q", detail)
	}
	alice.do(http.MethodPost, "/api/users/", map[string]any{
		"username": "eve", "password": strongPassword,
	}).expect(t, http.StatusForbidden)
	if got := env.count(t, "users"); got != beforeForbidden {
		t.Fatalf("普通用户越权创建后用户数量 = %d，期望 %d", got, beforeForbidden)
	}
}

func TestBootstrapAdminLifecycle(t *testing.T) {
	env := newTestEnvWithoutAdmin(t)

	missing, err := env.ensureAdminResult(t, "")
	if err == nil {
		t.Fatal("首次启动缺少 ADMIN_PASSWORD 时应失败")
	}
	if !strings.Contains(err.Error(), "ADMIN_PASSWORD") {
		t.Fatalf("缺少密码错误应提示 ADMIN_PASSWORD，实际: %v", err)
	}
	if missing != (store.BootstrapResult{}) {
		t.Fatalf("失败的初始化不应返回变更: %#v", missing)
	}
	invalid, err := env.ensureAdminResult(t, "short")
	if err == nil {
		t.Fatal("首次启动密码不合规时应失败")
	}
	if !strings.Contains(err.Error(), "ADMIN_PASSWORD") {
		t.Fatalf("密码不合规错误应提示 ADMIN_PASSWORD，实际: %v", err)
	}
	if invalid != (store.BootstrapResult{}) {
		t.Fatalf("失败的初始化不应返回变更: %#v", invalid)
	}
	if got := env.count(t, "users"); got != 0 {
		t.Fatalf("失败初始化不应创建用户，实际 %d", got)
	}
	if got := env.count(t, "households"); got != 0 {
		t.Fatalf("失败初始化不应创建家庭，实际 %d", got)
	}

	first, err := env.ensureAdminResult(t, strongPassword)
	if err != nil {
		t.Fatalf("首次初始化管理员失败: %v", err)
	}
	if !first.Created || first.PasswordRotated {
		t.Fatalf("首次初始化结果错误: %#v", first)
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("用户数量 = %d，期望 1", got)
	}
	if got := env.count(t, "households"); got != 1 {
		t.Fatalf("家庭数量 = %d，期望 1", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM users WHERE is_admin"); got != 1 {
		t.Fatalf("管理员数量 = %d，期望 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var username, householdName string
	var isAdmin bool
	if err := env.db.Pool().QueryRow(ctx, `
		SELECT u.username, u.is_admin, h.name
		FROM users u
		JOIN households h ON h.id = u.household_id`).
		Scan(&username, &isAdmin, &householdName); err != nil {
		t.Fatalf("读取引导管理员失败: %v", err)
	}
	if username != adminUsername || !isAdmin || householdName != adminHouseholdName {
		t.Fatalf("引导管理员 = %q is_admin=%t household=%q", username, isAdmin, householdName)
	}

	// Login proves the bootstrap password was stored correctly and the me
	// payload exposes the admin role without an invitation code.
	admin := env.loginUser(t, adminUsername, strongPassword)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if !mustBool(t, mustMap(t, adminMe["user"])["is_admin"]) {
		t.Fatalf("引导管理员 me 缺少 is_admin: %s", mustJSON(t, adminMe))
	}
	if _, exists := mustMap(t, adminMe["household"])["invite_code"]; exists {
		t.Fatalf("家庭成员信息不应暴露 invite_code: %s", mustJSON(t, adminMe))
	}
	members := mustSlice(t, adminMe["members"])
	if len(members) != 1 || !mustBool(t, mustMap(t, members[0])["is_admin"]) {
		t.Fatalf("管理员家庭成员错误: %s", mustJSON(t, adminMe))
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 1 {
		t.Fatalf("登录后会话数量 = %d，期望 1", got)
	}

	// An ordinary member's session must survive administrator password
	// rotation; only the administrator's sessions are invalidated.
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	if mustBool(t, aliceCreated["is_admin"]) {
		t.Fatalf("管理员创建的用户不应是管理员: %s", mustJSON(t, aliceCreated))
	}
	alice := env.loginUser(t, "alice", strongPassword)
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 2 {
		t.Fatalf("管理员与普通用户登录后会话数量 = %d，期望 2", got)
	}

	// Re-running with the same password is an idempotent no-op and preserves
	// the current admin session.
	second, err := env.ensureAdminResult(t, strongPassword)
	if err != nil {
		t.Fatalf("重复初始化失败: %v", err)
	}
	if second.Created || second.PasswordRotated {
		t.Fatalf("相同密码不应变更: %#v", second)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 2 {
		t.Fatalf("相同密码重启不应删除会话，实际 %d", got)
	}
	if me := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK); me["user"] == nil {
		t.Fatal("相同密码重启后当前会话应保持有效")
	}

	// Once the admin exists, omitting ADMIN_PASSWORD leaves it and sessions
	// unchanged.
	optional, err := env.ensureAdminResult(t, "")
	if err != nil {
		t.Fatalf("已有管理员时缺少 ADMIN_PASSWORD 不应失败: %v", err)
	}
	if optional.Created || optional.PasswordRotated {
		t.Fatalf("缺少 ADMIN_PASSWORD 不应变更已有管理员: %#v", optional)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 2 {
		t.Fatalf("缺少 ADMIN_PASSWORD 不应删除会话，实际 %d", got)
	}
	if me := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK); me["user"] == nil {
		t.Fatal("缺少 ADMIN_PASSWORD 重启后当前会话应保持有效")
	}

	// A different valid password rotates only the admin hash and invalidates
	// that admin's existing sessions.
	rotated, err := env.ensureAdminResult(t, rotatedPassword)
	if err != nil {
		t.Fatalf("轮换管理员密码失败: %v", err)
	}
	if rotated.Created || !rotated.PasswordRotated {
		t.Fatalf("密码轮换结果错误: %#v", rotated)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 1 {
		t.Fatalf("密码轮换应只删除管理员会话，实际剩余 %d", got)
	}
	if me := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK); me["user"] != nil {
		t.Fatalf("密码轮换后旧会话应失效: %s", mustJSON(t, me))
	}
	admin.do(http.MethodGet, "/api/accounts/", nil).expect(t, http.StatusUnauthorized)
	if me := alice.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK); me["user"] == nil {
		t.Fatal("管理员密码轮换不应使普通用户会话失效")
	}

	oldLogin := newTestClient(t, env)
	oldLogin.do(http.MethodPost, "/api/auth/login/",
		map[string]any{"username": adminUsername, "password": strongPassword}).
		expect(t, http.StatusBadRequest)
	newLogin := env.loginUser(t, adminUsername, rotatedPassword)
	newLoginMe := newLogin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if newLoginMe["user"] == nil {
		t.Fatal("轮换后的新密码应能登录")
	}

	// Omitting ADMIN_PASSWORD after rotation leaves the new password and the
	// new session untouched.
	optional, err = env.ensureAdminResult(t, "")
	if err != nil {
		t.Fatalf("轮换后缺少 ADMIN_PASSWORD 不应失败: %v", err)
	}
	if optional.Created || optional.PasswordRotated {
		t.Fatalf("缺少 ADMIN_PASSWORD 不应再次变更管理员: %#v", optional)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 2 {
		t.Fatalf("缺少 ADMIN_PASSWORD 不应删除新会话，实际 %d", got)
	}
	if me := newLogin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK); me["user"] == nil {
		t.Fatal("缺少 ADMIN_PASSWORD 重启后新会话应保持有效")
	}

	// Repeating the rotated password remains idempotent and keeps the session.
	repeated, err := env.ensureAdminResult(t, rotatedPassword)
	if err != nil {
		t.Fatalf("重复轮换密码失败: %v", err)
	}
	if repeated.Created || repeated.PasswordRotated {
		t.Fatalf("重复相同新密码不应变更: %#v", repeated)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM sessions"); got != 2 {
		t.Fatalf("重复相同新密码应保留会话，实际 %d", got)
	}

	if got := env.count(t, "users"); got != 2 {
		t.Fatalf("管理员初始化不应产生重复用户，实际 %d", got)
	}
	if got := env.count(t, "households"); got != 1 {
		t.Fatalf("管理员初始化不应产生重复家庭，实际 %d", got)
	}
}

func TestBootstrapAdminConcurrentStartup(t *testing.T) {
	env := newTestEnvWithoutAdmin(t)

	const workers = 6
	type bootstrapOutcome struct {
		result store.BootstrapResult
		err    error
	}
	outcomes := make(chan bootstrapOutcome, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := env.db.EnsureAdmin(ctx, strongPassword)
			outcomes <- bootstrapOutcome{result: result, err: err}
		}()
	}
	waitGroup.Wait()
	close(outcomes)

	created := 0
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("并发初始化管理员失败: %v", outcome.err)
		}
		if outcome.result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("并发初始化应只有一个创建者，实际 %d", created)
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("并发初始化后用户数量 = %d，期望 1", got)
	}
	if got := env.count(t, "households"); got != 1 {
		t.Fatalf("并发初始化后家庭数量 = %d，期望 1", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM users WHERE is_admin"); got != 1 {
		t.Fatalf("并发初始化后管理员数量 = %d，期望 1", got)
	}
	env.loginUser(t, adminUsername, strongPassword)
}

func TestBootstrapAdminUsernameCollision(t *testing.T) {
	env := newTestEnvWithoutAdmin(t)
	householdID, userID := env.seedUser(t, "原有家庭", adminUsername, strongPassword, false)

	_, err := env.ensureAdminResult(t, strongPassword)
	if err == nil {
		t.Fatal("用户名 admin 已被普通账号占用时应拒绝初始化")
	}
	if !strings.Contains(err.Error(), adminUsername) || !strings.Contains(err.Error(), "普通") {
		t.Fatalf("冲突错误应说明普通账号占用，实际: %v", err)
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("冲突不应创建用户，实际 %d", got)
	}
	if got := env.count(t, "households"); got != 1 {
		t.Fatalf("冲突不应创建家庭，实际 %d", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM users WHERE is_admin"); got != 0 {
		t.Fatalf("冲突不应提升任何账号，管理员数量 %d", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var actualHouseholdID int64
	var isAdmin bool
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT household_id, is_admin FROM users WHERE id = $1", userID).
		Scan(&actualHouseholdID, &isAdmin); err != nil {
		t.Fatalf("读取冲突用户失败: %v", err)
	}
	if isAdmin || actualHouseholdID != householdID {
		t.Fatalf("冲突用户不应被修改: household=%d is_admin=%t", actualHouseholdID, isAdmin)
	}

	// The original ordinary account still authenticates and remains ordinary.
	existing := env.loginUser(t, adminUsername, strongPassword)
	existingMe := existing.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustBool(t, mustMap(t, existingMe["user"])["is_admin"]) {
		t.Fatalf("原普通账号不应被提升: %s", mustJSON(t, existingMe))
	}
	if jsonInt64(t, mustMap(t, existingMe["household"])["id"]) != householdID {
		t.Fatalf("原普通账号家庭被改变: %s", mustJSON(t, existingMe))
	}
}

func TestBootstrapAdminPreservesExistingHousehold(t *testing.T) {
	env := newTestEnvWithoutAdmin(t)
	legacyHouseholdID, _ := env.seedUser(t, "旧家庭", "legacy", strongPassword, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := env.db.Pool().Exec(ctx, `
		INSERT INTO accounts (household_id, name, opening_balance_cents)
		VALUES ($1, $2, $3)`, legacyHouseholdID, "旧账户", int64(12345)); err != nil {
		t.Fatalf("插入旧账户失败: %v", err)
	}

	created, err := env.ensureAdminResult(t, strongPassword)
	if err != nil {
		t.Fatalf("存在旧家庭时初始化管理员失败: %v", err)
	}
	if !created.Created {
		t.Fatalf("首次初始化应创建管理员: %#v", created)
	}
	if got := env.count(t, "users"); got != 2 {
		t.Fatalf("旧用户应保留，用户数量 = %d，期望 2", got)
	}
	if got := env.count(t, "households"); got != 2 {
		t.Fatalf("旧家庭应保留，家庭数量 = %d，期望 2", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM users WHERE is_admin"); got != 1 {
		t.Fatalf("管理员数量 = %d，期望 1", got)
	}

	var adminHouseholdID int64
	var adminHousehold string
	if err := env.db.Pool().QueryRow(ctx, `
		SELECT h.id, h.name
		FROM users u
		JOIN households h ON h.id = u.household_id
		WHERE u.is_admin`).Scan(&adminHouseholdID, &adminHousehold); err != nil {
		t.Fatalf("读取管理员家庭失败: %v", err)
	}
	if adminHousehold != adminHouseholdName {
		t.Fatalf("管理员家庭名称 = %q，期望 %q", adminHousehold, adminHouseholdName)
	}
	if adminHouseholdID == legacyHouseholdID {
		t.Fatal("管理员不应占用已有家庭")
	}

	var legacyIsAdmin bool
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT is_admin FROM users WHERE username = $1", "legacy").
		Scan(&legacyIsAdmin); err != nil {
		t.Fatalf("读取旧用户失败: %v", err)
	}
	if legacyIsAdmin {
		t.Fatal("已有普通账号不应在迁移中被提升为管理员")
	}

	legacy := env.loginUser(t, "legacy", strongPassword)
	legacyMe := legacy.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, legacyMe["household"])["id"]) != legacyHouseholdID {
		t.Fatalf("旧用户家庭被改变: %s", mustJSON(t, legacyMe))
	}
	if mustBool(t, mustMap(t, legacyMe["user"])["is_admin"]) {
		t.Fatalf("旧用户不应成为管理员: %s", mustJSON(t, legacyMe))
	}
	legacyAccounts := legacy.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if len(legacyAccounts) != 1 ||
		mustString(t, legacyAccounts[0]["name"]) != "旧账户" ||
		mustString(t, legacyAccounts[0]["balance"]) != "123.45" {
		t.Fatalf("旧账本数据应完整保留: %s", mustJSON(t, legacyAccounts))
	}
	if _, exists := legacyAccounts[0]["opening_balance"]; exists {
		t.Fatalf("账户响应不应再暴露期初余额: %s", mustJSON(t, legacyAccounts))
	}
}

func TestHouseholdIsolationAndSharedMembers(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminHouseholdID := jsonInt64(t, mustMap(t, adminMe["household"])["id"])

	bobHouseholdID, _ := env.seedUser(t, "乙家", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)
	if bobHouseholdID == adminHouseholdID {
		t.Fatal("测试家庭应彼此隔离")
	}

	adminAccount := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "甲现金", "balance": "0.00"}, http.StatusCreated)
	bobAccount := bob.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "乙银行卡", "balance": "0.00"}, http.StatusCreated)
	adminAccountID := jsonInt64(t, adminAccount["id"])
	bobAccountID := jsonInt64(t, bobAccount["id"])

	adminTransaction := admin.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": adminAccountID, "kind": "income", "amount": "100.00",
		"category": "工资", "date": "2026-10-01T00:00:00", "note": "甲的收入",
	}, http.StatusCreated)
	bobTransaction := bob.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": bobAccountID, "kind": "expense", "amount": "7.50",
		"category": "日常", "date": "2026-10-02T00:00:00", "note": "乙的支出",
	}, http.StatusCreated)
	adminTransactionID := jsonInt64(t, adminTransaction["id"])
	bobTransactionID := jsonInt64(t, bobTransaction["id"])

	if got := len(admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 1 {
		t.Fatalf("管理员家庭应只看到 1 条交易，实际 %d", got)
	}
	if got := len(bob.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 1 {
		t.Fatalf("乙应只看到 1 条交易，实际 %d", got)
	}
	adminAccounts := admin.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if len(adminAccounts) != 1 || jsonInt64(t, adminAccounts[0]["id"]) != adminAccountID {
		t.Fatalf("管理员账户列表泄漏或缺失: %s", mustJSON(t, adminAccounts))
	}

	// Foreign rows are invisible and non-writable.
	admin.do(http.MethodPatch, fmt.Sprintf("/api/accounts/%d/", bobAccountID),
		map[string]any{"name": "被篡改"}).expect(t, http.StatusNotFound)
	admin.do(http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", bobTransactionID),
		map[string]any{"amount": "1.00"}).expect(t, http.StatusNotFound)
	admin.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", bobTransactionID), nil).
		expect(t, http.StatusNotFound)
	if got := len(bob.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 1 {
		t.Fatal("乙的交易不应被其他家庭删除")
	}
	bobAccounts := bob.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, bobAccounts[0]["name"]) != "乙银行卡" {
		t.Fatal("乙的账户不应被其他家庭修改")
	}

	// Foreign account validation on create and update.
	foreignCreate := admin.do(http.MethodPost, "/api/transactions/", map[string]any{
		"account": bobAccountID, "kind": "expense", "amount": "1.00",
		"category": "餐饮", "date": "2026-10-03T00:00:00",
	})
	foreignCreate.expect(t, http.StatusBadRequest)
	if _, exists := foreignCreate.object(t)["account"]; !exists {
		t.Fatalf("使用其他家庭账户应返回 account 错误: %s", string(foreignCreate.body))
	}
	foreignPatch := admin.do(http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", adminTransactionID),
		map[string]any{"account": bobAccountID})
	foreignPatch.expect(t, http.StatusBadRequest)
	if _, exists := foreignPatch.object(t)["account"]; !exists {
		t.Fatalf("更新到其他家庭账户应返回 account 错误: %s", string(foreignPatch.body))
	}
	adminTransactions := admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)
	if len(adminTransactions) != 1 ||
		jsonInt64(t, adminTransactions[0]["account"]) != adminAccountID {
		t.Fatalf("失败的跨家庭更新不应修改交易: %s", mustJSON(t, adminTransactions))
	}

	// A foreign account filter produces no rows, never another household's data.
	foreignFilter := admin.array(t, http.MethodGet,
		fmt.Sprintf("/api/transactions/?account=%d", bobAccountID), nil, http.StatusOK)
	if len(foreignFilter) != 0 {
		t.Fatalf("其他家庭账户过滤结果应为空: %s", mustJSON(t, foreignFilter))
	}

	adminSummary := admin.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	bobSummary := bob.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, adminSummary["income"]) != "100.00" ||
		mustString(t, adminSummary["expense"]) != "0.00" ||
		mustString(t, adminSummary["total_balance"]) != "100.00" {
		t.Fatalf("管理员家庭汇总错误: %s", mustJSON(t, adminSummary))
	}
	if mustString(t, bobSummary["income"]) != "0.00" ||
		mustString(t, bobSummary["expense"]) != "7.50" ||
		mustString(t, bobSummary["total_balance"]) != "-7.50" {
		t.Fatalf("乙的汇总错误: %s", mustJSON(t, bobSummary))
	}

	// A user created by the administrator joins the admin household and shares
	// read/write access to its ledger.
	carolCreated := admin.createUser(t, "carol", strongPassword)
	if mustBool(t, carolCreated["is_admin"]) {
		t.Fatalf("管理员创建的用户不应是管理员: %s", mustJSON(t, carolCreated))
	}
	carol := env.loginUser(t, "carol", strongPassword)
	carolMe := carol.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if jsonInt64(t, mustMap(t, carolMe["household"])["id"]) != adminHouseholdID {
		t.Fatalf("管理员创建的用户应加入管理员家庭: %s", mustJSON(t, carolMe))
	}
	carolTransactions := carol.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)
	if len(carolTransactions) != 1 {
		t.Fatalf("家庭成员应共享交易读取，实际 %d", len(carolTransactions))
	}
	updated := carol.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", adminTransactionID),
		map[string]any{"amount": "120.00", "note": "家庭共享修改"}, http.StatusOK)
	if mustString(t, updated["created_by_name"]) != adminUsername {
		t.Fatal("修改交易不应改变创建人")
	}
	adminTransactions = admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)
	if mustString(t, adminTransactions[0]["amount"]) != "120.00" {
		t.Fatal("家庭成员应能共享修改交易")
	}
	if got := len(bob.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 1 {
		t.Fatal("其他家庭不应看到共享修改")
	}
	adminMe = admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if got := len(mustSlice(t, adminMe["members"])); got != 2 {
		t.Fatalf("管理员家庭应有 2 名成员，实际 %d", got)
	}
	adminUsers := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	if len(adminUsers) != 2 {
		t.Fatalf("管理员用户列表应只含本家庭 2 人: %s", mustJSON(t, adminUsers))
	}
}

func TestBalanceAndSummaryLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	if mustBool(t, aliceCreated["is_admin"]) {
		t.Fatal("新建的 alice 不应是管理员")
	}
	client := env.loginUser(t, "alice", strongPassword)

	mainAccount := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "日常账户", "balance": "100.00"}, http.StatusCreated)
	mainAccountID := jsonInt64(t, mainAccount["id"])
	if mustString(t, mainAccount["balance"]) != "100.00" {
		t.Fatalf("账户金额格式错误: %s", mustJSON(t, mainAccount))
	}
	if _, exists := mainAccount["opening_balance"]; exists {
		t.Fatalf("账户响应不应再暴露期初余额: %s", mustJSON(t, mainAccount))
	}
	creditAccount := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "信用卡", "balance": "-50.00"}, http.StatusCreated)
	creditAccountID := jsonInt64(t, creditAccount["id"])
	if mustString(t, creditAccount["balance"]) != "-50.00" {
		t.Fatalf("负当前余额应被接受: %s", mustJSON(t, creditAccount))
	}

	for _, balance := range []any{"1.234", "10000000000.00", "", nil} {
		response := client.do(http.MethodPost, "/api/accounts/",
			map[string]any{"name": "坏账户", "balance": balance})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["balance"]; !exists {
			t.Fatalf("非法余额 %#v 应返回 balance 字段错误: %s", balance, string(response.body))
		}
	}

	invalidTransactions := []map[string]any{
		{"account": mainAccountID, "kind": "expense", "amount": "0.00", "category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "-1.00", "category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "1.234", "category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "10000000000.00", "category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "abc", "category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "transfer", "amount": "1.00", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "transfer", "amount": "1.00", "transfer_account": mainAccountID, "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "transfer", "amount": "1.00", "transfer_account": 0, "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "transfer", "amount": "1.00", "transfer_account": 999999, "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "1.00", "category": "餐饮", "date": "2026-10-01T00:00:00", "transfer_account": creditAccountID},
		{"account": mainAccountID, "kind": "expense", "amount": "1.00", "category": "", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "1.00", "category": "餐饮", "date": "2026-2-01"},
		{"account": mainAccountID, "kind": "expense", "amount": "1.00", "category": "餐饮", "date": "2026-02-30"},
	}
	for index, body := range invalidTransactions {
		response := client.do(http.MethodPost, "/api/transactions/", body)
		if response.status != http.StatusBadRequest {
			t.Fatalf("非法交易 #%d 状态码 = %d，期望 400；响应: %s",
				index, response.status, string(response.body))
		}
	}
	client.do(http.MethodPost, "/api/transactions/", map[string]any{
		"kind": "expense", "amount": "1.00", "category": "餐饮", "date": "2026-10-01T00:00:00",
	}).expect(t, http.StatusBadRequest)
	client.do(http.MethodGet, "/api/transactions/?month=2026-13", nil).
		expect(t, http.StatusBadRequest)

	income := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": mainAccountID, "kind": "income", "amount": "50.00",
		"category": "工资", "date": "2026-10-03T00:00:00", "note": "十月工资",
	}, http.StatusCreated)
	expense := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": mainAccountID, "kind": "expense", "amount": "20.00",
		"category": "餐饮", "date": "2026-10-05T00:00:00", "note": "聚餐",
	}, http.StatusCreated)
	client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": mainAccountID, "kind": "expense", "amount": "10.00",
		"category": "餐饮", "date": "2026-09-30T00:00:00",
	}, http.StatusCreated)
	incomeID := jsonInt64(t, income["id"])
	expenseID := jsonInt64(t, expense["id"])
	if mustString(t, income["amount"]) != "50.00" ||
		mustString(t, income["date"]) != "2026-10-03T00:00:00" ||
		mustString(t, income["account_name"]) != "日常账户" ||
		mustString(t, income["created_by_name"]) != "alice" {
		t.Fatalf("交易响应字段错误: %s", mustJSON(t, income))
	}

	october := client.array(t, http.MethodGet, "/api/transactions/?month=2026-10", nil, http.StatusOK)
	if len(october) != 2 {
		t.Fatalf("十月应有 2 条交易，实际 %d", len(october))
	}
	if mustString(t, october[0]["date"]) != "2026-10-05T00:00:00" ||
		mustString(t, october[1]["date"]) != "2026-10-03T00:00:00" {
		t.Fatalf("交易应按日期倒序: %s", mustJSON(t, october))
	}
	if got := len(client.array(t, http.MethodGet,
		"/api/transactions/?month=2026-10&kind=expense", nil, http.StatusOK)); got != 1 {
		t.Fatalf("收支类型过滤错误，实际 %d", got)
	}
	if got := len(client.array(t, http.MethodGet,
		"/api/transactions/?month=2026-10&q=%E9%A4%90%E9%A5%AE", nil, http.StatusOK)); got != 1 {
		t.Fatalf("文本搜索过滤错误，实际 %d", got)
	}
	if got := len(client.array(t, http.MethodGet,
		fmt.Sprintf("/api/transactions/?account=%d", mainAccountID), nil, http.StatusOK)); got != 3 {
		t.Fatalf("账户过滤应返回 3 条交易，实际 %d", got)
	}
	if got := len(client.array(t, http.MethodGet,
		"/api/transactions/?month=2026-09", nil, http.StatusOK)); got != 1 {
		t.Fatalf("九月应有 1 条交易，实际 %d", got)
	}

	summary := client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["income"]) != "50.00" ||
		mustString(t, summary["expense"]) != "20.00" ||
		mustString(t, summary["net"]) != "30.00" ||
		mustString(t, summary["total_balance"]) != "70.00" {
		t.Fatalf("十月汇总错误: %s", mustJSON(t, summary))
	}
	categories := mustSlice(t, summary["categories"])
	if len(categories) != 1 {
		t.Fatalf("支出分类应有 1 项: %s", mustJSON(t, categories))
	}
	category := mustMap(t, categories[0])
	if mustString(t, category["category"]) != "餐饮" ||
		mustString(t, category["amount"]) != "20.00" {
		t.Fatalf("支出分类错误: %s", mustJSON(t, category))
	}
	daily := mustSlice(t, summary["daily"])
	if len(daily) != 2 {
		t.Fatalf("每日趋势应有 2 天: %s", mustJSON(t, daily))
	}
	firstDay := mustMap(t, daily[0])
	secondDay := mustMap(t, daily[1])
	if mustString(t, firstDay["date"]) != "2026-10-03" ||
		mustString(t, firstDay["income"]) != "50.00" ||
		mustString(t, firstDay["expense"]) != "0.00" ||
		mustString(t, secondDay["date"]) != "2026-10-05" ||
		mustString(t, secondDay["income"]) != "0.00" ||
		mustString(t, secondDay["expense"]) != "20.00" {
		t.Fatalf("每日趋势错误: %s", mustJSON(t, daily))
	}

	accounts := client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if len(accounts) != 2 {
		t.Fatalf("账户数量错误: %s", mustJSON(t, accounts))
	}
	if mustString(t, accounts[0]["balance"]) != "120.00" {
		t.Fatalf("账户余额应包含所有月份交易: %s", mustJSON(t, accounts[0]))
	}
	if mustString(t, accounts[1]["balance"]) != "-50.00" {
		t.Fatalf("信用账户余额错误: %s", mustJSON(t, accounts[1]))
	}

	client.do(http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", expenseID),
		map[string]any{"amount": "30.00", "note": "修改后"}).
		expect(t, http.StatusOK)
	client.do(http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", incomeID),
		map[string]any{"amount": "1.234"}).
		expect(t, http.StatusBadRequest)

	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["expense"]) != "30.00" ||
		mustString(t, summary["net"]) != "20.00" ||
		mustString(t, summary["total_balance"]) != "60.00" {
		t.Fatalf("修改支出后汇总错误: %s", mustJSON(t, summary))
	}
	categories = mustSlice(t, summary["categories"])
	if mustString(t, mustMap(t, categories[0])["amount"]) != "30.00" {
		t.Fatalf("修改支出后分类错误: %s", mustJSON(t, categories))
	}

	patchedAccount := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", mainAccountID),
		map[string]any{"balance": "200.00"}, http.StatusOK)
	if mustString(t, patchedAccount["balance"]) != "200.00" {
		t.Fatalf("写入当前余额后账户余额错误: %s", mustJSON(t, patchedAccount))
	}
	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["total_balance"]) != "150.00" {
		t.Fatalf("校正当前余额后总余额错误: %s", mustJSON(t, summary))
	}
	accounts = client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "200.00" {
		t.Fatalf("校正当前余额后账户余额错误: %s", mustJSON(t, accounts[0]))
	}

	client.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", expenseID), nil).
		expect(t, http.StatusNoContent)
	october = client.array(t, http.MethodGet, "/api/transactions/?month=2026-10", nil, http.StatusOK)
	if len(october) != 1 {
		t.Fatalf("删除后十月应有 1 条交易，实际 %d", len(october))
	}
	accounts = client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "230.00" {
		t.Fatalf("删除支出后账户余额错误: %s", mustJSON(t, accounts[0]))
	}
	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["expense"]) != "0.00" ||
		mustString(t, summary["net"]) != "50.00" ||
		mustString(t, summary["total_balance"]) != "180.00" {
		t.Fatalf("删除支出后汇总错误: %s", mustJSON(t, summary))
	}
	if len(mustSlice(t, summary["categories"])) != 0 {
		t.Fatalf("删除支出后分类应为空: %s", mustJSON(t, summary))
	}
	daily = mustSlice(t, summary["daily"])
	if len(daily) != 1 || mustString(t, mustMap(t, daily[0])["date"]) != "2026-10-03" {
		t.Fatalf("删除支出后每日趋势错误: %s", mustJSON(t, daily))
	}

	client.do(http.MethodGet, "/api/summary/", nil).expect(t, http.StatusBadRequest)
	client.do(http.MethodGet, "/api/summary/?month=2026-13", nil).
		expect(t, http.StatusBadRequest)
}

func TestTransactionSecondPrecision(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	admin.createUser(t, "alice", strongPassword)
	client := env.loginUser(t, "alice", strongPassword)

	account := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "秒级账户", "balance": "0.00"}, http.StatusCreated)
	accountID := jsonInt64(t, account["id"])

	invalidDates := []string{
		"2026-10-01",
		"2026-10-01T12:00",
		"2026-10-01T24:00:00",
		"2026-10-01T12:60:00",
		"2026-10-01T12:00:60",
		"2026-10-01T12:00:00.5",
		"2026-10-01T12:00:00Z",
		"2026-10-01T12:00:00+08:00",
		"2026-02-30T12:00:00",
	}
	for _, date := range invalidDates {
		response := client.do(http.MethodPost, "/api/transactions/", map[string]any{
			"account": accountID, "kind": "expense", "amount": "1.00",
			"category": "餐饮", "date": date,
		})
		if response.status != http.StatusBadRequest {
			t.Fatalf("非法日期时间 %q 状态码 = %d，期望 400；响应: %s",
				date, response.status, string(response.body))
		}
		if _, exists := response.object(t)["date"]; !exists {
			t.Fatalf("非法日期时间 %q 应返回 date 字段错误: %s",
				date, string(response.body))
		}
	}
	if got := len(client.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 0 {
		t.Fatalf("非法日期时间不应写入交易，实际 %d 条", got)
	}

	late := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "expense", "amount": "10.00",
		"category": "餐饮", "date": "2026-10-31T23:59:59", "note": "月末最后一秒",
	}, http.StatusCreated)
	nextMonth := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "expense", "amount": "20.00",
		"category": "餐饮", "date": "2026-11-01T00:00:00", "note": "次月第一秒",
	}, http.StatusCreated)
	earlier := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "income", "amount": "7.00",
		"category": "工资", "date": "2026-10-02T09:30:15",
	}, http.StatusCreated)
	firstSameSecond := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "income", "amount": "5.00",
		"category": "工资", "date": "2026-10-02T09:30:16",
	}, http.StatusCreated)
	secondSameSecond := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": accountID, "kind": "expense", "amount": "3.00",
		"category": "餐饮", "date": "2026-10-02T09:30:16",
	}, http.StatusCreated)

	for _, item := range []struct {
		response map[string]any
		date     string
	}{
		{late, "2026-10-31T23:59:59"},
		{nextMonth, "2026-11-01T00:00:00"},
		{earlier, "2026-10-02T09:30:15"},
		{firstSameSecond, "2026-10-02T09:30:16"},
		{secondSameSecond, "2026-10-02T09:30:16"},
	} {
		if got := mustString(t, item.response["date"]); got != item.date {
			t.Fatalf("创建返回日期时间 = %q，期望 %q: %s",
				got, item.date, mustJSON(t, item.response))
		}
	}

	october := client.array(t, http.MethodGet, "/api/transactions/?month=2026-10", nil, http.StatusOK)
	if len(october) != 4 {
		t.Fatalf("十月应有 4 条交易，实际 %d: %s", len(october), mustJSON(t, october))
	}
	wantOctoberDates := []string{
		"2026-10-31T23:59:59",
		"2026-10-02T09:30:16",
		"2026-10-02T09:30:16",
		"2026-10-02T09:30:15",
	}
	for index, want := range wantOctoberDates {
		if got := mustString(t, october[index]["date"]); got != want {
			t.Fatalf("十月交易 #%d 日期时间 = %q，期望 %q: %s",
				index, got, want, mustJSON(t, october))
		}
	}
	if jsonInt64(t, october[1]["id"]) != jsonInt64(t, secondSameSecond["id"]) ||
		jsonInt64(t, october[2]["id"]) != jsonInt64(t, firstSameSecond["id"]) {
		t.Fatalf("同一秒交易应按 id 倒序: %s", mustJSON(t, october))
	}

	november := client.array(t, http.MethodGet, "/api/transactions/?month=2026-11", nil, http.StatusOK)
	if len(november) != 1 || mustString(t, november[0]["date"]) != "2026-11-01T00:00:00" {
		t.Fatalf("次月第一秒应归入次月: %s", mustJSON(t, november))
	}

	summary := client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["income"]) != "12.00" ||
		mustString(t, summary["expense"]) != "13.00" ||
		mustString(t, summary["net"]) != "-1.00" {
		t.Fatalf("十月秒级汇总错误: %s", mustJSON(t, summary))
	}
	daily := mustSlice(t, summary["daily"])
	if len(daily) != 2 {
		t.Fatalf("同一日多个时间应合并为一天，实际 %d 天: %s", len(daily), mustJSON(t, daily))
	}
	secondDay := mustMap(t, daily[0])
	lastDay := mustMap(t, daily[1])
	if mustString(t, secondDay["date"]) != "2026-10-02" ||
		mustString(t, secondDay["income"]) != "12.00" ||
		mustString(t, secondDay["expense"]) != "3.00" ||
		mustString(t, lastDay["date"]) != "2026-10-31" ||
		mustString(t, lastDay["income"]) != "0.00" ||
		mustString(t, lastDay["expense"]) != "10.00" {
		t.Fatalf("每日秒级聚合错误: %s", mustJSON(t, daily))
	}
	novemberSummary := client.object(t, http.MethodGet, "/api/summary/?month=2026-11", nil, http.StatusOK)
	novemberDaily := mustSlice(t, novemberSummary["daily"])
	if mustString(t, novemberSummary["expense"]) != "20.00" ||
		len(novemberDaily) != 1 ||
		mustString(t, mustMap(t, novemberDaily[0])["date"]) != "2026-11-01" {
		t.Fatalf("次月第一秒应归入次月统计: %s", mustJSON(t, novemberSummary))
	}

	// A patch that omits date preserves the stored second-precision value.
	patched := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", jsonInt64(t, late["id"])),
		map[string]any{"amount": "11.00"}, http.StatusOK)
	if mustString(t, patched["date"]) != "2026-10-31T23:59:59" {
		t.Fatalf("省略日期的修改应保留原时间: %s", mustJSON(t, patched))
	}

	// A patch can move a transaction to a different date and second.
	patched = client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", jsonInt64(t, earlier["id"])),
		map[string]any{"date": "2026-10-03T08:00:01"}, http.StatusOK)
	if mustString(t, patched["date"]) != "2026-10-03T08:00:01" {
		t.Fatalf("修改日期时间响应错误: %s", mustJSON(t, patched))
	}
	october = client.array(t, http.MethodGet, "/api/transactions/?month=2026-10", nil, http.StatusOK)
	if len(october) != 4 ||
		mustString(t, october[0]["date"]) != "2026-10-31T23:59:59" ||
		mustString(t, october[1]["date"]) != "2026-10-03T08:00:01" {
		t.Fatalf("修改后列表应按完整时间倒序: %s", mustJSON(t, october))
	}

	for _, date := range []string{"2026-10-03", "2026-10-03T08:61:00"} {
		response := client.do(http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", jsonInt64(t, earlier["id"])),
			map[string]any{"date": date})
		if response.status != http.StatusBadRequest {
			t.Fatalf("非法修改日期时间 %q 状态码 = %d，期望 400；响应: %s",
				date, response.status, string(response.body))
		}
		if _, exists := response.object(t)["date"]; !exists {
			t.Fatalf("非法修改日期时间 %q 应返回 date 字段错误: %s",
				date, string(response.body))
		}
	}
	preserved := client.array(t, http.MethodGet, "/api/transactions/?month=2026-10", nil, http.StatusOK)
	if len(preserved) != 4 ||
		mustString(t, preserved[1]["date"]) != "2026-10-03T08:00:01" {
		t.Fatalf("非法日期修改不应改动交易: %s", mustJSON(t, preserved))
	}

	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	daily = mustSlice(t, summary["daily"])
	if len(daily) != 3 ||
		mustString(t, mustMap(t, daily[1])["date"]) != "2026-10-03" ||
		mustString(t, mustMap(t, daily[1])["income"]) != "7.00" ||
		mustString(t, mustMap(t, daily[2])["date"]) != "2026-10-31" ||
		mustString(t, mustMap(t, daily[2])["expense"]) != "11.00" {
		t.Fatalf("修改时间后每日统计错误: %s", mustJSON(t, daily))
	}
}

func TestTransferLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	admin.createUser(t, "alice", strongPassword)
	client := env.loginUser(t, "alice", strongPassword)

	source := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "储蓄卡", "balance": "1000.00"}, http.StatusCreated)
	target := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "旅行钱包", "balance": "100.00"}, http.StatusCreated)
	sourceID := jsonInt64(t, source["id"])
	targetID := jsonInt64(t, target["id"])

	// A second household supplies a valid but foreign destination account.
	_, _ = env.seedUser(t, "外家", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)
	foreignAccount := bob.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "外部账户", "balance": "50.00"}, http.StatusCreated)
	foreignAccountID := jsonInt64(t, foreignAccount["id"])

	invalidTransfers := []map[string]any{
		{"account": sourceID, "kind": "transfer", "amount": "10.00", "date": "2026-10-10T00:00:00"},
		{"account": sourceID, "kind": "transfer", "amount": "10.00", "transfer_account": sourceID, "date": "2026-10-10T00:00:00"},
		{"account": sourceID, "kind": "transfer", "amount": "10.00", "transfer_account": foreignAccountID, "date": "2026-10-10T00:00:00"},
		{"account": sourceID, "kind": "transfer", "amount": "10.00", "transfer_account": 999999, "date": "2026-10-10T00:00:00"},
		{"account": sourceID, "kind": "expense", "amount": "10.00", "category": "餐饮", "date": "2026-10-10T00:00:00", "transfer_account": targetID},
	}
	for index, body := range invalidTransfers {
		response := client.do(http.MethodPost, "/api/transactions/", body)
		if response.status != http.StatusBadRequest {
			t.Fatalf("非法转账 #%d 状态码 = %d，期望 400；响应: %s",
				index, response.status, string(response.body))
		}
	}
	if got := len(client.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 0 {
		t.Fatalf("非法转账不应写入交易，实际 %d 条", got)
	}

	// The category is fixed server-side even when the client sends another one.
	transfer := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": sourceID, "kind": "transfer", "amount": "200.00",
		"category": "餐饮", "date": "2026-10-10T00:00:00", "note": "十月调仓",
		"transfer_account": targetID,
	}, http.StatusCreated)
	transferID := jsonInt64(t, transfer["id"])
	if mustString(t, transfer["kind"]) != "transfer" ||
		jsonInt64(t, transfer["account"]) != sourceID ||
		jsonInt64(t, transfer["transfer_account"]) != targetID ||
		mustString(t, transfer["transfer_account_name"]) != "旅行钱包" ||
		mustString(t, transfer["category"]) != "账户转账" ||
		mustString(t, transfer["amount"]) != "200.00" {
		t.Fatalf("转账响应字段错误: %s", mustJSON(t, transfer))
	}

	accounts := client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "800.00" ||
		mustString(t, accounts[1]["balance"]) != "300.00" {
		t.Fatalf("转账后账户余额错误: %s", mustJSON(t, accounts))
	}
	summary := client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["income"]) != "0.00" ||
		mustString(t, summary["expense"]) != "0.00" ||
		mustString(t, summary["net"]) != "0.00" ||
		mustString(t, summary["total_balance"]) != "1100.00" {
		t.Fatalf("转账不应影响收支或家庭总余额: %s", mustJSON(t, summary))
	}
	if len(mustSlice(t, summary["categories"])) != 0 ||
		len(mustSlice(t, summary["daily"])) != 0 {
		t.Fatalf("转账不应进入分类或每日收支: %s", mustJSON(t, summary))
	}

	if got := len(client.array(t, http.MethodGet,
		fmt.Sprintf("/api/transactions/?account=%d", sourceID), nil, http.StatusOK)); got != 1 {
		t.Fatalf("转出账户过滤应包含转账，实际 %d", got)
	}
	targetFiltered := client.array(t, http.MethodGet,
		fmt.Sprintf("/api/transactions/?account=%d", targetID), nil, http.StatusOK)
	if len(targetFiltered) != 1 ||
		jsonInt64(t, targetFiltered[0]["transfer_account"]) != targetID {
		t.Fatalf("转入账户过滤应包含转账: %s", mustJSON(t, targetFiltered))
	}
	if got := len(client.array(t, http.MethodGet,
		"/api/transactions/?kind=transfer", nil, http.StatusOK)); got != 1 {
		t.Fatalf("转账类型过滤错误，实际 %d", got)
	}
	if got := len(client.array(t, http.MethodGet,
		fmt.Sprintf("/api/transactions/?q=%s", url.QueryEscape("旅行钱包")), nil, http.StatusOK)); got != 1 {
		t.Fatalf("按转入账户名搜索错误，实际 %d", got)
	}

	patched := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{"amount": "250.00", "note": "金额调整"}, http.StatusOK)
	if mustString(t, patched["amount"]) != "250.00" ||
		jsonInt64(t, patched["transfer_account"]) != targetID ||
		mustString(t, patched["category"]) != "账户转账" {
		t.Fatalf("修改转账响应错误: %s", mustJSON(t, patched))
	}
	accounts = client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "750.00" ||
		mustString(t, accounts[1]["balance"]) != "350.00" {
		t.Fatalf("修改转账金额后余额错误: %s", mustJSON(t, accounts))
	}

	// The destination must stay in the household and differ from the source.
	for index, body := range []map[string]any{
		{"transfer_account": foreignAccountID},
		{"transfer_account": sourceID},
		{"account": targetID},
		{"kind": "transfer", "transfer_account": nil},
		{"kind": "expense", "category": "餐饮", "transfer_account": targetID},
	} {
		response := client.do(http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", transferID), body)
		if response.status != http.StatusBadRequest {
			t.Fatalf("非法转账修改 #%d 状态码 = %d，期望 400；响应: %s",
				index, response.status, string(response.body))
		}
	}

	// Switching to an expense clears the destination and applies expense
	// semantics to the source account; converting back restores the transfer.
	converted := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{
			"kind": "expense", "category": "餐饮", "transfer_account": nil,
		}, http.StatusOK)
	if mustString(t, converted["kind"]) != "expense" ||
		converted["transfer_account"] != nil ||
		mustString(t, converted["transfer_account_name"]) != "" {
		t.Fatalf("切换为支出后应清除转入账户: %s", mustJSON(t, converted))
	}
	accounts = client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "750.00" ||
		mustString(t, accounts[1]["balance"]) != "100.00" {
		t.Fatalf("切换为支出后余额错误: %s", mustJSON(t, accounts))
	}
	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["expense"]) != "250.00" ||
		mustString(t, summary["net"]) != "-250.00" ||
		mustString(t, summary["total_balance"]) != "850.00" {
		t.Fatalf("切换为支出后汇总错误: %s", mustJSON(t, summary))
	}
	if len(mustSlice(t, summary["categories"])) != 1 ||
		len(mustSlice(t, summary["daily"])) != 1 {
		t.Fatalf("切换为支出后应计入分类与每日趋势: %s", mustJSON(t, summary))
	}

	restored := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{
			"kind": "transfer", "category": "餐饮", "transfer_account": targetID,
		}, http.StatusOK)
	if mustString(t, restored["kind"]) != "transfer" ||
		jsonInt64(t, restored["transfer_account"]) != targetID ||
		mustString(t, restored["category"]) != "账户转账" {
		t.Fatalf("恢复转账后响应错误: %s", mustJSON(t, restored))
	}
	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["income"]) != "0.00" ||
		mustString(t, summary["expense"]) != "0.00" ||
		mustString(t, summary["total_balance"]) != "1100.00" ||
		len(mustSlice(t, summary["categories"])) != 0 ||
		len(mustSlice(t, summary["daily"])) != 0 {
		t.Fatalf("恢复转账后收支统计应保持干净: %s", mustJSON(t, summary))
	}

	// Another household cannot read or mutate the transfer.
	if got := len(bob.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 0 {
		t.Fatalf("其他家庭不应看到转账，实际 %d 条", got)
	}
	bob.do(http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{"amount": "1.00"}).expect(t, http.StatusNotFound)
	bob.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", transferID), nil).
		expect(t, http.StatusNotFound)

	client.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", transferID), nil).
		expect(t, http.StatusNoContent)
	accounts = client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if mustString(t, accounts[0]["balance"]) != "1000.00" ||
		mustString(t, accounts[1]["balance"]) != "100.00" {
		t.Fatalf("删除转账后余额应恢复: %s", mustJSON(t, accounts))
	}
	summary = client.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	if mustString(t, summary["total_balance"]) != "1100.00" ||
		mustString(t, summary["income"]) != "0.00" ||
		mustString(t, summary["expense"]) != "0.00" {
		t.Fatalf("删除转账后汇总错误: %s", mustJSON(t, summary))
	}
	if got := len(client.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 0 {
		t.Fatalf("删除转账后交易列表应为空，实际 %d 条", got)
	}

	// The new UI sends transfer_account:null for income and expense payloads.
	income := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": sourceID, "kind": "income", "amount": "5.00",
		"category": "其他", "date": "2026-10-11T00:00:00", "transfer_account": nil,
	}, http.StatusCreated)
	if income["transfer_account"] != nil ||
		mustString(t, income["transfer_account_name"]) != "" {
		t.Fatalf("收入不应保存转入账户: %s", mustJSON(t, income))
	}
	client.do(http.MethodDelete,
		fmt.Sprintf("/api/transactions/%d/", jsonInt64(t, income["id"])), nil).
		expect(t, http.StatusNoContent)
}

func TestTransactionCategoryFilter(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	mainAccount := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "分类账户", "balance": "0.00"}, http.StatusCreated)
	mainAccountID := jsonInt64(t, mainAccount["id"])
	secondAccount := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "分类第二账户", "balance": "0.00"}, http.StatusCreated)
	secondAccountID := jsonInt64(t, secondAccount["id"])

	for _, transaction := range []map[string]any{
		{"account": mainAccountID, "kind": "expense", "amount": "10.00",
			"category": "餐饮", "date": "2026-10-01T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "5.00",
			"category": "早餐", "date": "2026-10-02T00:00:00"},
		{"account": mainAccountID, "kind": "expense", "amount": "20.00",
			"category": "交通", "date": "2026-10-03T00:00:00", "note": "餐饮和早餐"},
		{"account": mainAccountID, "kind": "income", "amount": "100.00",
			"category": "工资", "date": "2026-10-04T00:00:00"},
		{"account": secondAccountID, "kind": "expense", "amount": "8.00",
			"category": "早餐", "date": "2026-10-05T00:00:00"},
	} {
		admin.object(t, http.MethodPost, "/api/transactions/", transaction, http.StatusCreated)
	}

	categoryFilter := func(categories ...string) string {
		query := url.Values{}
		for _, category := range categories {
			query.Add("category", category)
		}
		return "/api/transactions/?" + query.Encode()
	}

	if got := len(admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)); got != 5 {
		t.Fatalf("未传 category 时应返回全部交易，实际 %d 条", got)
	}

	filtered := admin.array(t, http.MethodGet, categoryFilter("餐饮", "早餐"), nil, http.StatusOK)
	if len(filtered) != 3 {
		t.Fatalf("多分类过滤应返回 3 条交易，实际 %d 条: %s", len(filtered), mustJSON(t, filtered))
	}
	for _, transaction := range filtered {
		category := mustString(t, transaction["category"])
		if category != "餐饮" && category != "早餐" {
			t.Fatalf("多分类过滤返回了非目标分类: %s", mustJSON(t, filtered))
		}
	}

	if got := len(admin.array(t, http.MethodGet, categoryFilter("早餐"), nil, http.StatusOK)); got != 2 {
		t.Fatalf("分类过滤不应按备注匹配，实际 %d 条", got)
	}
	if got := len(admin.array(t, http.MethodGet, categoryFilter("餐"), nil, http.StatusOK)); got != 0 {
		t.Fatalf("分类过滤应为精确匹配，实际 %d 条", got)
	}
	if got := len(admin.array(t, http.MethodGet,
		categoryFilter("早餐", "工资")+"&kind=income", nil, http.StatusOK)); got != 1 {
		t.Fatalf("分类过滤应与收支类型相交，实际 %d 条", got)
	}
	if got := len(admin.array(t, http.MethodGet,
		categoryFilter("早餐")+fmt.Sprintf("&account=%d", secondAccountID), nil, http.StatusOK)); got != 1 {
		t.Fatalf("分类过滤应与账户过滤相交，实际 %d 条", got)
	}

	_, _ = env.seedUser(t, "分类隔离家庭", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)
	bobAccount := bob.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "乙分类账户", "balance": "0.00"}, http.StatusCreated)
	bob.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": jsonInt64(t, bobAccount["id"]), "kind": "expense", "amount": "6.00",
		"category": "早餐", "date": "2026-10-06T00:00:00",
	}, http.StatusCreated)
	if got := len(bob.array(t, http.MethodGet, categoryFilter("早餐"), nil, http.StatusOK)); got != 1 {
		t.Fatalf("分类过滤应保持家庭隔离，乙应只看到 1 条交易，实际 %d 条", got)
	}
	if got := len(admin.array(t, http.MethodGet, categoryFilter("早餐"), nil, http.StatusOK)); got != 2 {
		t.Fatalf("分类过滤不应返回其他家庭交易，实际 %d 条", got)
	}

	for _, invalidCategory := range []string{"", strings.Repeat("餐", 65), "餐\x00饮"} {
		response := admin.do(http.MethodGet, categoryFilter(invalidCategory), nil)
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["category"]; !exists {
			t.Fatalf("非法分类 %#v 应返回 category 字段错误: %s",
				invalidCategory, string(response.body))
		}
	}
}
