package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const categoryConflictDetail = "分类设置已被其他成员更新，请重新打开分类管理后再保存"

func categoryEntry(name string, visible bool) map[string]any {
	return map[string]any{"name": name, "visible": visible}
}

func categoryPreferences(entries []map[string]any, deletedDefaults []string) map[string]any {
	items := make([]any, len(entries))
	for index, entry := range entries {
		items[index] = entry
	}
	if deletedDefaults == nil {
		deletedDefaults = []string{}
	}
	return map[string]any{
		"entries":          items,
		"deleted_defaults": deletedDefaults,
	}
}

func categoryPutBody(baseRevision any, expense, income map[string]any) map[string]any {
	return map[string]any{
		"base_revision": baseRevision,
		"expense":       expense,
		"income":        income,
	}
}

func validCategoryPutBody(baseRevision any) map[string]any {
	return categoryPutBody(
		baseRevision,
		categoryPreferences([]map[string]any{categoryEntry("餐饮", true)}, nil),
		categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil),
	)
}

func fetchCategorySettings(t *testing.T, client *testClient) map[string]any {
	t.Helper()
	return client.object(t, http.MethodGet, "/api/categories/", nil, http.StatusOK)
}

func categoryHouseholdID(t *testing.T, client *testClient) int64 {
	t.Helper()
	me := client.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	return jsonInt64(t, mustMap(t, me["household"])["id"])
}

func categoryPreferencesForKind(t *testing.T, payload map[string]any, kind string) map[string]any {
	t.Helper()
	return mustMap(t, payload[kind])
}

func categoryEntriesForKind(t *testing.T, payload map[string]any, kind string) []map[string]any {
	t.Helper()
	preferences := categoryPreferencesForKind(t, payload, kind)
	raw := mustSlice(t, preferences["entries"])
	entries := make([]map[string]any, len(raw))
	for index, value := range raw {
		entries[index] = mustMap(t, value)
	}
	return entries
}

func categoryStrings(t *testing.T, value any) []string {
	t.Helper()
	raw := mustSlice(t, value)
	values := make([]string, len(raw))
	for index, item := range raw {
		values[index] = mustString(t, item)
	}
	return values
}

func categoryNamesChecked(t *testing.T, entries []map[string]any) []string {
	t.Helper()
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = mustString(t, entry["name"])
	}
	return names
}

func assertCategoryNames(t *testing.T, entries []map[string]any, want ...string) {
	t.Helper()
	got := categoryNamesChecked(t, entries)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("分类顺序 = %v，期望 %v", got, want)
	}
}

func assertCategoryUninitialized(t *testing.T, payload map[string]any) {
	t.Helper()
	if mustBool(t, payload["initialized"]) {
		t.Fatalf("未初始化家庭的 initialized 应为 false: %s", mustJSON(t, payload))
	}
	if revision := jsonInt64(t, payload["revision"]); revision != 0 {
		t.Fatalf("未初始化家庭的 revision = %d，期望 0", revision)
	}
	for _, kind := range []string{"expense", "income"} {
		value, exists := payload[kind]
		if !exists || value != nil {
			t.Fatalf("未初始化家庭的 %s 应为 null: %s", kind, mustJSON(t, payload))
		}
	}
}

func assertCategoryInitialized(t *testing.T, payload map[string]any, revision int64) {
	t.Helper()
	if !mustBool(t, payload["initialized"]) {
		t.Fatalf("初始化后的 initialized 应为 true: %s", mustJSON(t, payload))
	}
	if got := jsonInt64(t, payload["revision"]); got != revision {
		t.Fatalf("revision = %d，期望 %d: %s", got, revision, mustJSON(t, payload))
	}
	for _, kind := range []string{"expense", "income"} {
		preferences := categoryPreferencesForKind(t, payload, kind)
		if _, exists := preferences["entries"]; !exists {
			t.Fatalf("初始化后的 %s 缺少 entries: %s", kind, mustJSON(t, payload))
		}
		if _, exists := preferences["deleted_defaults"]; !exists {
			t.Fatalf("初始化后的 %s 缺少 deleted_defaults: %s", kind, mustJSON(t, payload))
		}
	}
}

func TestCategorySettingsSharedHouseholdLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	householdID := categoryHouseholdID(t, admin)
	admin.createUser(t, "alice", strongPassword)
	alice := env.loginUser(t, "alice", strongPassword)

	initial := fetchCategorySettings(t, admin)
	assertCategoryUninitialized(t, initial)
	assertCategoryUninitialized(t, fetchCategorySettings(t, alice))
	if got := env.count(t, "household_category_settings"); got != 0 {
		t.Fatalf("未初始化家庭的 GET 不应写入分类设置，实际 %d 行", got)
	}

	expense := categoryPreferences([]map[string]any{
		{
			"name":           " 餐饮 ",
			"visible":        true,
			"default_name":   "餐饮",
			"icon":           "utensils",
			"previous_names": []string{"吃饭", "吃喝"},
		},
		{
			"name":        "早餐",
			"visible":     false,
			"parent_name": "餐饮",
			"icon":        "coffee",
		},
		categoryEntry("交通", true),
	}, []string{"其他支出"})
	income := categoryPreferences([]map[string]any{
		{
			"name":         "工资",
			"visible":      true,
			"default_name": "工资",
		},
		categoryEntry(" 奖金 ", true),
		{
			"name":        "兼职",
			"visible":     false,
			"parent_name": "工资",
		},
	}, []string{"理财"})

	created := admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0, expense, income), http.StatusOK)
	assertCategoryInitialized(t, created, 1)

	createdExpense := categoryEntriesForKind(t, created, "expense")
	assertCategoryNames(t, createdExpense, "餐饮", "早餐", "交通")
	if !mustBool(t, createdExpense[0]["visible"]) ||
		mustString(t, createdExpense[0]["default_name"]) != "餐饮" ||
		mustString(t, createdExpense[0]["icon"]) != "utensils" {
		t.Fatalf("支出分类标准化失败: %s", mustJSON(t, createdExpense[0]))
	}
	if got := strings.Join(categoryStrings(t, createdExpense[0]["previous_names"]), "\x00"); got != "吃饭\x00吃喝" {
		t.Fatalf("历史名称顺序错误: %s", mustJSON(t, createdExpense[0]))
	}
	if _, exists := createdExpense[0]["parent_name"]; exists {
		t.Fatalf("顶级分类不应输出 parent_name: %s", mustJSON(t, createdExpense[0]))
	}
	if mustString(t, createdExpense[1]["parent_name"]) != "餐饮" ||
		mustBool(t, createdExpense[1]["visible"]) ||
		mustString(t, createdExpense[1]["icon"]) != "coffee" {
		t.Fatalf("子分类字段错误: %s", mustJSON(t, createdExpense[1]))
	}
	if !mustBool(t, createdExpense[2]["visible"]) {
		t.Fatalf("交通分类应保持启用: %s", mustJSON(t, createdExpense[2]))
	}
	for _, optional := range []string{"parent_name", "icon", "default_name", "previous_names"} {
		if _, exists := createdExpense[2][optional]; exists {
			t.Fatalf("缺省可选字段 %s 不应输出: %s", optional, mustJSON(t, createdExpense[2]))
		}
	}
	if got := strings.Join(categoryStrings(t, categoryPreferencesForKind(t, created, "expense")["deleted_defaults"]), "\x00"); got != "其他支出" {
		t.Fatalf("支出 deleted_defaults 错误: %s", mustJSON(t, created))
	}

	createdIncome := categoryEntriesForKind(t, created, "income")
	assertCategoryNames(t, createdIncome, "工资", "奖金", "兼职")
	if mustString(t, createdIncome[1]["name"]) != "奖金" {
		t.Fatalf("收入分类名称未 trim: %s", mustJSON(t, createdIncome[1]))
	}
	if got := strings.Join(categoryStrings(t, categoryPreferencesForKind(t, created, "income")["deleted_defaults"]), "\x00"); got != "理财" {
		t.Fatalf("收入 deleted_defaults 错误: %s", mustJSON(t, created))
	}

	if got := env.scalarInt(t,
		"SELECT count(*) FROM household_category_settings WHERE household_id = $1",
		householdID); got != 1 {
		t.Fatalf("分类设置行数 = %d，期望 1", got)
	}
	if got := env.scalarInt(t,
		"SELECT revision FROM household_category_settings WHERE household_id = $1",
		householdID); got != 1 {
		t.Fatalf("数据库 revision = %d，期望 1", got)
	}

	// A different member of the same household sees the exact shared snapshot.
	shared := fetchCategorySettings(t, alice)
	assertCategoryInitialized(t, shared, 1)
	if mustJSON(t, shared["expense"]) != mustJSON(t, created["expense"]) ||
		mustJSON(t, shared["income"]) != mustJSON(t, created["income"]) {
		t.Fatalf("同家庭成员应看到相同分类: %s / %s", mustJSON(t, shared), mustJSON(t, created))
	}

	// Any member can replace both kinds with one revision-checked snapshot.
	aliceExpense := categoryPreferences([]map[string]any{
		categoryEntry("餐饮", true),
		categoryEntry("日用", true),
	}, nil)
	aliceIncome := categoryPreferences([]map[string]any{
		categoryEntry("工资", true),
		categoryEntry("年终奖", true),
	}, []string{"理财"})
	updated := alice.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(1, aliceExpense, aliceIncome), http.StatusOK)
	assertCategoryInitialized(t, updated, 2)
	assertCategoryNames(t, categoryEntriesForKind(t, updated, "expense"), "餐饮", "日用")
	assertCategoryNames(t, categoryEntriesForKind(t, updated, "income"), "工资", "年终奖")
	if got := len(categoryStrings(t, categoryPreferencesForKind(t, updated, "expense")["deleted_defaults"])); got != 0 {
		t.Fatalf("空 deleted_defaults 应返回 []，实际 %s", mustJSON(t, updated))
	}

	adminView := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, adminView, 2)
	if mustJSON(t, adminView["expense"]) != mustJSON(t, updated["expense"]) ||
		mustJSON(t, adminView["income"]) != mustJSON(t, updated["income"]) {
		t.Fatalf("其他成员的保存应对全家庭可见: %s / %s", mustJSON(t, adminView), mustJSON(t, updated))
	}
}

func TestCategorySettingsHouseholdIsolation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminHouseholdID := categoryHouseholdID(t, admin)
	env.seedUser(t, "乙家", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)

	assertCategoryUninitialized(t, fetchCategorySettings(t, bob))

	adminExpense := categoryPreferences([]map[string]any{categoryEntry("餐饮", true)}, nil)
	adminIncome := categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil)
	adminCreated := admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0, adminExpense, adminIncome), http.StatusOK)
	assertCategoryInitialized(t, adminCreated, 1)

	// The household is derived from the session. A forged household value in
	// the query string does not grant access to another household's snapshot,
	// and a positive base revision cannot create a row.
	crossHousehold := bob.do(http.MethodPut,
		fmt.Sprintf("/api/categories/?household=%d", adminHouseholdID),
		categoryPutBody(1,
			categoryPreferences([]map[string]any{categoryEntry("入侵", true)}, nil),
			categoryPreferences([]map[string]any{categoryEntry("入侵收入", true)}, nil)))
	crossHousehold.expect(t, http.StatusConflict)
	if detail := mustString(t, crossHousehold.object(t)["detail"]); detail != categoryConflictDetail {
		t.Fatalf("跨家庭写入 detail = %q，期望 %q", detail, categoryConflictDetail)
	}
	assertCategoryUninitialized(t, fetchCategorySettings(t, bob))
	adminAfterCross := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, adminAfterCross, 1)
	if mustJSON(t, adminAfterCross["expense"]) != mustJSON(t, adminCreated["expense"]) {
		t.Fatalf("跨家庭请求不得修改目标家庭: %s", mustJSON(t, adminAfterCross))
	}

	bobExpense := categoryPreferences([]map[string]any{categoryEntry("乙家餐饮", true)}, nil)
	bobIncome := categoryPreferences([]map[string]any{categoryEntry("乙家工资", true)}, nil)
	bobCreated := bob.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0, bobExpense, bobIncome), http.StatusOK)
	assertCategoryInitialized(t, bobCreated, 1)
	assertCategoryNames(t, categoryEntriesForKind(t, bobCreated, "expense"), "乙家餐饮")

	adminFinal := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, adminFinal, 1)
	assertCategoryNames(t, categoryEntriesForKind(t, adminFinal, "expense"), "餐饮")
	if got := env.scalarInt(t, "SELECT count(*) FROM household_category_settings"); got != 2 {
		t.Fatalf("两个家庭应各有一行分类设置，实际 %d 行", got)
	}
}

func TestCategorySettingsStaleRevisionDoesNotWrite(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	householdID := categoryHouseholdID(t, admin)

	firstExpense := categoryPreferences([]map[string]any{categoryEntry("餐饮", true)}, nil)
	firstIncome := categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil)
	created := admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0, firstExpense, firstIncome), http.StatusOK)
	assertCategoryInitialized(t, created, 1)

	staleBody := categoryPutBody(0,
		categoryPreferences([]map[string]any{categoryEntry("过期支出", true)}, nil),
		categoryPreferences([]map[string]any{categoryEntry("过期收入", true)}, nil))
	stale := admin.do(http.MethodPut, "/api/categories/", staleBody)
	stale.expect(t, http.StatusConflict)
	if detail := mustString(t, stale.object(t)["detail"]); detail != categoryConflictDetail {
		t.Fatalf("重复首次插入 detail = %q，期望 %q", detail, categoryConflictDetail)
	}
	unchanged := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, unchanged, 1)
	if mustJSON(t, unchanged["expense"]) != mustJSON(t, created["expense"]) ||
		mustJSON(t, unchanged["income"]) != mustJSON(t, created["income"]) {
		t.Fatalf("冲突保存不得修改任一分类: %s", mustJSON(t, unchanged))
	}

	secondExpense := categoryPreferences([]map[string]any{
		categoryEntry("餐饮", true), categoryEntry("交通", true),
	}, nil)
	secondIncome := categoryPreferences([]map[string]any{
		categoryEntry("工资", true), categoryEntry("奖金", true),
	}, nil)
	second := admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(1, secondExpense, secondIncome), http.StatusOK)
	assertCategoryInitialized(t, second, 2)

	staleOldRevision := admin.do(http.MethodPut, "/api/categories/",
		categoryPutBody(1,
			categoryPreferences([]map[string]any{categoryEntry("旧写入", true)}, nil),
			categoryPreferences([]map[string]any{categoryEntry("旧收入", true)}, nil)))
	staleOldRevision.expect(t, http.StatusConflict)
	if detail := mustString(t, staleOldRevision.object(t)["detail"]); detail != categoryConflictDetail {
		t.Fatalf("旧 revision detail = %q，期望 %q", detail, categoryConflictDetail)
	}

	final := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, final, 2)
	if mustJSON(t, final["expense"]) != mustJSON(t, second["expense"]) ||
		mustJSON(t, final["income"]) != mustJSON(t, second["income"]) {
		t.Fatalf("过期 revision 写入不得修改任一分类: %s", mustJSON(t, final))
	}
	if got := env.scalarInt(t,
		"SELECT revision FROM household_category_settings WHERE household_id = $1",
		householdID); got != 2 {
		t.Fatalf("冲突后数据库 revision = %d，期望 2", got)
	}
}

type categoryConcurrentResult struct {
	status int
	body   []byte
	err    error
}

func categoryPutRequest(t *testing.T, client *testClient, body map[string]any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化分类请求失败: %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPut, client.base+"/api/categories/", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("创建分类请求失败: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", client.base)
	request.Header.Set("X-CSRFToken", client.cookieValue("csrftoken"))
	return request
}

func executeCategoryRequest(httpClient *http.Client, request *http.Request) categoryConcurrentResult {
	response, err := httpClient.Do(request)
	if err != nil {
		return categoryConcurrentResult{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return categoryConcurrentResult{status: response.StatusCode, body: body, err: err}
}

func TestCategorySettingsConcurrentRevisionConflict(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	householdID := categoryHouseholdID(t, admin)

	admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0,
			categoryPreferences([]map[string]any{categoryEntry("餐饮", true)}, nil),
			categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil)),
		http.StatusOK)

	firstClient := env.loginUser(t, adminUsername, strongPassword)
	secondClient := env.loginUser(t, adminUsername, strongPassword)
	firstBody := categoryPutBody(1,
		categoryPreferences([]map[string]any{categoryEntry("并发甲", true)}, nil),
		categoryPreferences([]map[string]any{categoryEntry("并发收入甲", true)}, nil))
	secondBody := categoryPutBody(1,
		categoryPreferences([]map[string]any{categoryEntry("并发乙", true)}, nil),
		categoryPreferences([]map[string]any{categoryEntry("并发收入乙", true)}, nil))

	requests := []*http.Request{
		categoryPutRequest(t, firstClient, firstBody),
		categoryPutRequest(t, secondClient, secondBody),
	}
	clients := []*testClient{firstClient, secondClient}
	results := make([]categoryConcurrentResult, len(requests))
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index] = executeCategoryRequest(clients[index].client, requests[index])
		}(index)
	}
	wait.Wait()

	successes := 0
	conflicts := 0
	successIndex := -1
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("并发分类保存请求失败: %v", result.err)
		}
		switch result.status {
		case http.StatusOK:
			successes++
			successIndex = index
		case http.StatusConflict:
			conflicts++
			var payload map[string]any
			if err := json.Unmarshal(result.body, &payload); err != nil {
				t.Fatalf("并发冲突响应不是 JSON: %v；%s", err, string(result.body))
			}
			if detail := mustString(t, payload["detail"]); detail != categoryConflictDetail {
				t.Fatalf("并发冲突 detail = %q，期望 %q", detail, categoryConflictDetail)
			}
		default:
			t.Fatalf("并发保存状态码 = %d，响应: %s", result.status, string(result.body))
		}
	}
	if successes != 1 || conflicts != 1 || successIndex < 0 {
		t.Fatalf("并发保存应恰好一个成功一个 409，实际成功 %d 冲突 %d",
			successes, conflicts)
	}

	var winner map[string]any
	if err := json.Unmarshal(results[successIndex].body, &winner); err != nil {
		t.Fatalf("成功响应不是 JSON: %v；%s", err, string(results[successIndex].body))
	}
	assertCategoryInitialized(t, winner, 2)
	winnerName := mustString(t, categoryEntriesForKind(t, winner, "expense")[0]["name"])
	if winnerName != "并发甲" && winnerName != "并发乙" {
		t.Fatalf("成功响应包含意外分类: %s", mustJSON(t, winner))
	}

	final := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, final, 2)
	if mustJSON(t, final) != mustJSON(t, winner) {
		t.Fatalf("并发保存后读取结果应等于成功响应: %s / %s",
			mustJSON(t, final), mustJSON(t, winner))
	}
	if got := env.scalarInt(t,
		"SELECT revision FROM household_category_settings WHERE household_id = $1",
		householdID); got != 2 {
		t.Fatalf("并发保存后数据库 revision = %d，期望 2", got)
	}
	if got := env.scalarInt(t,
		"SELECT count(*) FROM household_category_settings WHERE household_id = $1",
		householdID); got != 1 {
		t.Fatalf("并发保存后分类设置行数 = %d，期望 1", got)
	}
}

func TestCategorySettingsValidation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	householdID := categoryHouseholdID(t, admin)

	admin.object(t, http.MethodPut, "/api/categories/",
		validCategoryPutBody(0), http.StatusOK)
	baseline := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, baseline, 1)

	longName := strings.Repeat("餐", 65)
	longIcon := strings.Repeat("a", 65)
	longAlias := strings.Repeat("旧", 65)

	cases := []struct {
		name  string
		field string
		body  func() map[string]any
	}{
		{
			name: "缺少 base_revision", field: "base_revision",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				delete(body, "base_revision")
				return body
			},
		},
		{
			name: "base_revision 为 null", field: "base_revision",
			body: func() map[string]any { return validCategoryPutBody(nil) },
		},
		{
			name: "base_revision 为负数", field: "base_revision",
			body: func() map[string]any { return validCategoryPutBody(-1) },
		},
		{
			name: "base_revision 为小数", field: "base_revision",
			body: func() map[string]any { return validCategoryPutBody(1.5) },
		},
		{
			name: "base_revision 为字符串", field: "base_revision",
			body: func() map[string]any { return validCategoryPutBody("1") },
		},
		{
			name: "缺少 expense", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				delete(body, "expense")
				return body
			},
		},
		{
			name: "expense 为 null", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = nil
				return body
			},
		},
		{
			name: "expense 不是对象", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = []any{}
				return body
			},
		},
		{
			name: "expense 缺少 entries", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = map[string]any{"deleted_defaults": []string{}}
				return body
			},
		},
		{
			name: "expense 的 entries 为 null", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = map[string]any{
					"entries": nil, "deleted_defaults": []string{},
				}
				return body
			},
		},
		{
			name: "expense 缺少 deleted_defaults", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = map[string]any{
					"entries": []any{categoryEntry("餐饮", true)},
				}
				return body
			},
		},
		{
			name: "expense 的 deleted_defaults 为 null", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = map[string]any{
					"entries":          []any{categoryEntry("餐饮", true)},
					"deleted_defaults": nil,
				}
				return body
			},
		},
		{
			name: "expense 的 deleted_defaults 不是数组", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = map[string]any{
					"entries":          []any{categoryEntry("餐饮", true)},
					"deleted_defaults": map[string]any{"旧": true},
				}
				return body
			},
		},
		{
			name: "expense 内名称重复", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("餐饮", true), categoryEntry("餐饮", false),
				}, nil)
				return body
			},
		},
		{
			name: "expense 名称为空", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("", true),
				}, nil)
				return body
			},
		},
		{
			name: "expense 名称仅空白", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("   ", true),
				}, nil)
				return body
			},
		},
		{
			name: "expense 名称超过 64 个码点", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry(longName, true),
				}, nil)
				return body
			},
		},
		{
			name: "expense 名称含 NUL", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("餐\x00饮", true),
				}, nil)
				return body
			},
		},
		{
			name: "expense 缺少 visible", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 的 visible 为 null", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": nil},
				}, nil)
				return body
			},
		},
		{
			name: "expense 的 visible 不是布尔值", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": "yes"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 名称不是字符串", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": 7, "visible": true},
				}, nil)
				return body
			},
		},
		{
			name: "expense 上级分类不存在", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": true, "parent_name": "不存在"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 上级分类为自身", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": true, "parent_name": "餐饮"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 超过两级分类", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("餐饮", true),
					{"name": "早餐", "visible": true, "parent_name": "餐饮"},
					{"name": "咖啡", "visible": true, "parent_name": "早餐"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 上级分类形成环", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "甲", "visible": true, "parent_name": "乙"},
					{"name": "乙", "visible": true, "parent_name": "甲"},
				}, nil)
				return body
			},
		},
		{
			name: "expense 全部顶级分类停用", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					categoryEntry("餐饮", false),
					{"name": "早餐", "visible": true, "parent_name": "餐饮"},
				}, nil)
				return body
			},
		},
		{
			name: "expense default_name 重复", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": true, "default_name": "餐饮"},
					{"name": "吃喝", "visible": true, "default_name": "餐饮"},
				}, nil)
				return body
			},
		},
		{
			name: "expense default_name 超过 64 个码点", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences([]map[string]any{
					{"name": "餐饮", "visible": true, "default_name": longName},
				}, nil)
				return body
			},
		},
		{
			name: "expense 图标格式非法", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				entry := categoryEntry("餐饮", true)
				entry["icon"] = "Bad_Icon"
				body["expense"] = categoryPreferences([]map[string]any{entry}, nil)
				return body
			},
		},
		{
			name: "expense 图标超长", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				entry := categoryEntry("餐饮", true)
				entry["icon"] = longIcon
				body["expense"] = categoryPreferences([]map[string]any{entry}, nil)
				return body
			},
		},
		{
			name: "expense 历史名称为空", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				entry := categoryEntry("餐饮", true)
				entry["previous_names"] = []string{""}
				body["expense"] = categoryPreferences([]map[string]any{entry}, nil)
				return body
			},
		},
		{
			name: "expense 历史名称超长", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				entry := categoryEntry("餐饮", true)
				entry["previous_names"] = []string{longAlias}
				body["expense"] = categoryPreferences([]map[string]any{entry}, nil)
				return body
			},
		},
		{
			name: "expense 历史名称不是字符串", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				entry := categoryEntry("餐饮", true)
				entry["previous_names"] = []any{7}
				body["expense"] = categoryPreferences([]map[string]any{entry}, nil)
				return body
			},
		},
		{
			name: "expense 已删除默认分类为空", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences(
					[]map[string]any{categoryEntry("餐饮", true)}, []string{""})
				return body
			},
		},
		{
			name: "expense 已删除默认分类含 NUL", field: "expense",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["expense"] = categoryPreferences(
					[]map[string]any{categoryEntry("餐饮", true)}, []string{"其他\x00支出"})
				return body
			},
		},
		{
			name: "缺少 income", field: "income",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				delete(body, "income")
				return body
			},
		},
		{
			name: "income 内名称重复", field: "income",
			body: func() map[string]any {
				body := validCategoryPutBody(1)
				body["income"] = categoryPreferences([]map[string]any{
					categoryEntry("工资", true), categoryEntry("工资", true),
				}, nil)
				return body
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := admin.do(http.MethodPut, "/api/categories/", testCase.body())
			response.expect(t, http.StatusBadRequest)
			payload := response.object(t)
			messages, exists := payload[testCase.field]
			if !exists {
				t.Fatalf("响应缺少字段 %q: %s", testCase.field, string(response.body))
			}
			if len(mustSlice(t, messages)) == 0 {
				t.Fatalf("字段 %q 应包含至少一条错误: %s", testCase.field, string(response.body))
			}
		})
	}

	combined := validCategoryPutBody(1)
	combined["base_revision"] = -1
	combined["income"] = nil
	combinedResponse := admin.do(http.MethodPut, "/api/categories/", combined)
	combinedResponse.expect(t, http.StatusBadRequest)
	combinedPayload := combinedResponse.object(t)
	for _, field := range []string{"base_revision", "income"} {
		if _, exists := combinedPayload[field]; !exists {
			t.Fatalf("组合错误应包含字段 %q: %s", field, string(combinedResponse.body))
		}
	}

	after := fetchCategorySettings(t, admin)
	if mustJSON(t, after) != mustJSON(t, baseline) {
		t.Fatalf("非法保存不得修改分类: %s / %s", mustJSON(t, after), mustJSON(t, baseline))
	}
	if got := env.scalarInt(t,
		"SELECT revision FROM household_category_settings WHERE household_id = $1",
		householdID); got != 1 {
		t.Fatalf("非法保存后数据库 revision = %d，期望 1", got)
	}
}

func TestCategorySettingsRejectsNullOptionalEntryFields(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	householdID := categoryHouseholdID(t, admin)

	admin.object(t, http.MethodPut, "/api/categories/",
		validCategoryPutBody(0), http.StatusOK)
	baseline := fetchCategorySettings(t, admin)
	assertCategoryInitialized(t, baseline, 1)

	for _, field := range []string{"parent_name", "icon", "default_name", "previous_names"} {
		t.Run(field, func(t *testing.T) {
			entry := categoryEntry("餐饮", true)
			entry[field] = nil
			body := categoryPutBody(1,
				categoryPreferences([]map[string]any{entry}, nil),
				categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil))

			response := admin.do(http.MethodPut, "/api/categories/", body)
			response.expect(t, http.StatusBadRequest)
			payload := response.object(t)
			messages, exists := payload["expense"]
			if !exists {
				t.Fatalf("字段 %s 为 null 应返回 expense 字段错误: %s",
					field, string(response.body))
			}
			if len(mustSlice(t, messages)) == 0 {
				t.Fatalf("字段 %s 为 null 应包含错误消息: %s",
					field, string(response.body))
			}

			after := fetchCategorySettings(t, admin)
			assertCategoryInitialized(t, after, 1)
			if mustJSON(t, after["expense"]) != mustJSON(t, baseline["expense"]) ||
				mustJSON(t, after["income"]) != mustJSON(t, baseline["income"]) {
				t.Fatalf("字段 %s 为 null 不得修改任一分类: %s",
					field, mustJSON(t, after))
			}
		})
	}

	if got := env.scalarInt(t,
		"SELECT revision FROM household_category_settings WHERE household_id = $1",
		householdID); got != 1 {
		t.Fatalf("显式 null 被拒后数据库 revision = %d，期望 1", got)
	}
}

func TestCategorySettingsAuthAndCSRF(t *testing.T) {
	env := newTestEnv(t)
	raw := newRawClient(t, env)
	body := validCategoryPutBody(0)

	raw.do(http.MethodGet, "/api/categories/", nil).expect(t, http.StatusUnauthorized)
	raw.do(http.MethodPut, "/api/categories/", body).expect(t, http.StatusUnauthorized)
	assertCategoryUninitialized(t, fetchCategorySettings(t, env.adminClient(t)))

	admin := env.adminClient(t)
	admin.do(http.MethodGet, "/api/categories/", nil).expect(t, http.StatusOK)

	noToken := admin.do(http.MethodPut, "/api/categories/", body, withoutCSRF)
	noToken.expect(t, http.StatusForbidden)
	if detail := mustString(t, noToken.object(t)["detail"]); !strings.Contains(detail, "CSRF") {
		t.Fatalf("缺少 CSRF 的 detail 应说明 CSRF: %s", string(noToken.body))
	}
	admin.do(http.MethodPut, "/api/categories/", body, withoutOrigin).expect(t, http.StatusForbidden)
	admin.do(http.MethodPut, "/api/categories/", body,
		withOrigin("http://evil.example")).expect(t, http.StatusForbidden)

	assertCategoryUninitialized(t, fetchCategorySettings(t, admin))
	missingSlash := admin.do(http.MethodGet, "/api/categories", nil)
	missingSlash.expect(t, http.StatusNotFound)
	if _, exists := missingSlash.object(t)["detail"]; !exists {
		t.Fatalf("无尾斜杠的分类路径应返回 JSON 404: %s", string(missingSlash.body))
	}

	created := admin.object(t, http.MethodPut, "/api/categories/", body, http.StatusOK)
	assertCategoryInitialized(t, created, 1)
}

func TestCategorySettingsDoesNotRewriteTransactions(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	account := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "分类历史账户", "balance": "0.00"}, http.StatusCreated)
	admin.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account":  jsonInt64(t, account["id"]),
		"kind":     "expense",
		"amount":   "10.00",
		"category": "旧分类",
		"date":     "2026-10-15T00:00:00",
		"note":     "历史分类保持不变",
	}, http.StatusCreated)

	admin.object(t, http.MethodPut, "/api/categories/",
		categoryPutBody(0,
			categoryPreferences([]map[string]any{
				{
					"name":           "新分类",
					"visible":        true,
					"previous_names": []string{"旧分类"},
				},
			}, nil),
			categoryPreferences([]map[string]any{categoryEntry("工资", true)}, nil)),
		http.StatusOK)

	transactions := admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)
	if len(transactions) != 1 {
		t.Fatalf("分类保存不应改变交易数量，实际 %d 条: %s", len(transactions), mustJSON(t, transactions))
	}
	if category := mustString(t, transactions[0]["category"]); category != "旧分类" {
		t.Fatalf("分类保存不应改写历史交易分类，实际 %q", category)
	}

	query := url.Values{}
	query.Add("category", "旧分类")
	filtered := admin.array(t, http.MethodGet,
		"/api/transactions/?"+query.Encode(), nil, http.StatusOK)
	if len(filtered) != 1 || mustString(t, filtered[0]["category"]) != "旧分类" {
		t.Fatalf("历史分类仍应可筛选: %s", mustJSON(t, filtered))
	}

	summary := admin.object(t, http.MethodGet, "/api/summary/?month=2026-10", nil, http.StatusOK)
	categories := mustSlice(t, summary["categories"])
	found := false
	for _, value := range categories {
		item := mustMap(t, value)
		if mustString(t, item["category"]) == "旧分类" {
			found = true
			if amount := mustString(t, item["amount"]); amount != "10.00" {
				t.Fatalf("历史分类汇总金额 = %s，期望 10.00", amount)
			}
		}
	}
	if !found {
		t.Fatalf("汇总仍应按原分类字符串分组: %s", mustJSON(t, summary))
	}
}
