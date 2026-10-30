package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestUserNicknameDisplay(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	for _, nickname := range []string{strings.Repeat("家", 51), "小\x00明"} {
		response := admin.do(http.MethodPost, "/api/users/", map[string]any{
			"username": "invalid", "password": strongPassword, "nickname": nickname,
		}).expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["nickname"]; !exists {
			t.Fatalf("非法昵称应返回 nickname 字段错误: %s", response.body)
		}
	}
	if got := env.count(t, "users"); got != 1 {
		t.Fatalf("非法昵称不应创建用户，实际用户数 %d", got)
	}

	// The limit counts Unicode characters, not bytes or UTF-16 code units.
	nickname := strings.Repeat("家😀", 25)
	created := admin.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username": "alice", "password": strongPassword, "nickname": " \t" + nickname + "\n",
	}, http.StatusCreated)
	if mustString(t, created["nickname"]) != nickname {
		t.Fatalf("昵称应去除首尾空白并保留 50 个 Unicode 字符: %s", mustJSON(t, created))
	}

	// A nickname is a display label, not a login identifier.
	anonymous := newTestClient(t, env)
	anonymous.do(http.MethodPost, "/api/auth/login/", map[string]any{
		"username": nickname, "password": strongPassword,
	}).expect(t, http.StatusBadRequest)
	alice := env.loginUser(t, "alice", strongPassword)
	me := alice.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	if mustString(t, mustMap(t, me["user"])["nickname"]) != nickname {
		t.Fatalf("登录后应能读取自己的昵称: %s", mustJSON(t, me))
	}
	var memberNickname string
	for _, item := range mustSlice(t, me["members"]) {
		member := mustMap(t, item)
		if mustString(t, member["username"]) == "alice" {
			memberNickname = mustString(t, member["nickname"])
		}
	}
	if memberNickname != nickname {
		t.Fatalf("家庭成员昵称 = %q，期望 %q", memberNickname, nickname)
	}

	// Empty nicknames remain valid and use the username in ledger displays.
	blank := admin.object(t, http.MethodPost, "/api/users/", map[string]any{
		"username": "blank", "password": strongPassword, "nickname": " \t\n",
	}, http.StatusCreated)
	if mustString(t, blank["nickname"]) != "" {
		t.Fatalf("空白昵称应规范为空串: %s", mustJSON(t, blank))
	}
	users := admin.array(t, http.MethodGet, "/api/users/", nil, http.StatusOK)
	var listedNickname string
	for _, user := range users {
		if mustString(t, user["username"]) == "alice" {
			listedNickname = mustString(t, user["nickname"])
		}
	}
	if listedNickname != nickname {
		t.Fatalf("用户列表昵称 = %q，期望 %q", listedNickname, nickname)
	}

	account := admin.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "家庭账户", "opening_balance": "0.00",
	}, http.StatusCreated)
	blankClient := env.loginUser(t, "blank", strongPassword)
	for _, testCase := range []struct {
		client *testClient
		name   string
	}{
		{alice, nickname},
		{blankClient, "blank"},
		{admin, adminUsername},
	} {
		transaction := testCase.client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
			"account": account["id"], "kind": "expense", "amount": "1.00",
			"category": "餐饮", "date": "2026-10-01T12:00:00",
		}, http.StatusCreated)
		if mustString(t, transaction["created_by_name"]) != testCase.name {
			t.Fatalf("流水记录人应优先昵称、无昵称回退用户名: %s", mustJSON(t, transaction))
		}
	}
}
