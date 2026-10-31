package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// loginWithCookies posts a login and returns the raw Set-Cookie attributes so
// tests can assert browser-session versus persistent cookie behaviour.
func (c *testClient) loginWithCookies(t *testing.T, body map[string]any) (int, []*http.Cookie) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化登录请求失败: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, c.base+"/api/auth/login/", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("创建登录请求失败: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", c.base)
	request.Header.Set("X-CSRFToken", c.cookieValue("csrftoken"))
	response, err := c.client.Do(request)
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("读取登录响应失败: %v", err)
	}
	return response.StatusCode, response.Cookies()
}

func (e *testEnv) sessionExpiry(t *testing.T, token string) time.Time {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var expires time.Time
	if err := e.db.Pool().QueryRow(ctx,
		"SELECT expires_at FROM sessions WHERE token_hash = $1", sum[:]).Scan(&expires); err != nil {
		t.Fatalf("读取会话过期时间失败: %v", err)
	}
	return expires
}

func mustCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("响应缺少 %s cookie", name)
	return nil
}

func TestLoginRememberSessionLifetime(t *testing.T) {
	env := newTestEnv(t)
	const day = 24 * time.Hour

	cases := []struct {
		name       string
		body       map[string]any
		persistent bool
		minTTL     time.Duration
		maxTTL     time.Duration
	}{
		{
			name:       "勾选记住登录",
			body:       map[string]any{"username": adminUsername, "password": strongPassword, "remember": true},
			persistent: true,
			minTTL:     29 * day,
			maxTTL:     30 * day,
		},
		{
			name:   "未勾选记住登录",
			body:   map[string]any{"username": adminUsername, "password": strongPassword, "remember": false},
			minTTL: 11 * time.Hour,
			maxTTL: 12 * time.Hour,
		},
		{
			name:   "省略 remember 字段",
			body:   map[string]any{"username": adminUsername, "password": strongPassword},
			minTTL: 11 * time.Hour,
			maxTTL: 12 * time.Hour,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, env)
			before := time.Now()
			status, cookies := client.loginWithCookies(t, tc.body)
			if status != http.StatusOK {
				t.Fatalf("登录状态码 = %d，期望 200", status)
			}

			session := mustCookie(t, cookies, "sessionid")
			if !session.HttpOnly {
				t.Fatal("session cookie 必须是 HttpOnly")
			}
			if session.Secure {
				t.Fatal("测试环境未启用 HTTPS，session cookie 不应带 Secure")
			}
			if session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
				t.Fatalf("session cookie 属性异常: SameSite=%v Path=%q", session.SameSite, session.Path)
			}
			if session.Value == "" || session.Value == strongPassword {
				t.Fatal("session cookie 必须是不含密码的随机令牌")
			}

			expires := env.sessionExpiry(t, session.Value)
			dbTTL := expires.Sub(before)
			if dbTTL < tc.minTTL || dbTTL > tc.maxTTL+time.Minute {
				t.Fatalf("数据库会话期限 = %v，期望约 %v", dbTTL, tc.maxTTL)
			}

			if !tc.persistent {
				if session.MaxAge != 0 {
					t.Fatalf("临时会话 cookie 不应带 MaxAge，实际 %d", session.MaxAge)
				}
				if !session.Expires.IsZero() {
					t.Fatalf("临时会话 cookie 不应带 Expires，实际 %v", session.Expires)
				}
				return
			}

			if session.MaxAge <= 0 || session.Expires.IsZero() {
				t.Fatalf("持久会话 cookie 必须带 MaxAge 和 Expires: MaxAge=%d Expires=%v", session.MaxAge, session.Expires)
			}
			maxAge := time.Duration(session.MaxAge) * time.Second
			if maxAge < tc.minTTL || maxAge > tc.maxTTL+time.Minute {
				t.Fatalf("cookie MaxAge = %v，期望约 %v", maxAge, tc.maxTTL)
			}
			if diff := maxAge - time.Until(session.Expires); diff < -5*time.Second || diff > 5*time.Second {
				t.Fatalf("cookie MaxAge 与 Expires 不一致: MaxAge=%v Expires=%v", maxAge, session.Expires)
			}
			cookieTTL := session.Expires.Sub(before)
			if cookieTTL < tc.minTTL || cookieTTL > tc.maxTTL+time.Minute {
				t.Fatalf("cookie 期限 = %v，期望约 %v", cookieTTL, tc.maxTTL)
			}
			if diff := session.Expires.Sub(expires); diff < -2*time.Second || diff > 2*time.Second {
				t.Fatalf("cookie 与数据库会话期限不一致: cookie=%v db=%v", session.Expires, expires)
			}
		})
	}
}

func TestRememberedSessionRestoresAfterBrowserRestartAndLogoutRevokes(t *testing.T) {
	env := newTestEnv(t)
	client := newTestClient(t, env)
	client.object(t, http.MethodPost, "/api/auth/login/", map[string]any{
		"username": adminUsername, "password": strongPassword, "remember": true,
	}, http.StatusOK)
	token := client.cookieValue("sessionid")
	if token == "" {
		t.Fatal("记住登录后缺少 session cookie")
	}

	// A fresh client with no cookies replays the persisted token, modelling a
	// browser restart that restores the remembered cookie.
	restored := replaySession(t, client.base, token)
	restored.expect(t, http.StatusOK)
	me := restored.object(t)
	if me["user"] == nil {
		t.Fatalf("持久会话在浏览器重启后应仍有效: %s", mustJSON(t, me))
	}
	if got := mustString(t, mustMap(t, me["user"])["username"]); got != adminUsername {
		t.Fatalf("恢复会话的用户 = %q，期望 %q", got, adminUsername)
	}

	// Logging out must revoke the DB row, so the persisted cookie cannot be
	// replayed even while its 30-day browser lifetime has not elapsed.
	client.do(http.MethodPost, "/api/auth/logout/", nil).expect(t, http.StatusNoContent)
	replayed := replaySession(t, client.base, token)
	replayed.expect(t, http.StatusOK)
	if replayed.object(t)["user"] != nil {
		t.Fatal("退出登录后持久 session cookie 不应再可用")
	}
}
