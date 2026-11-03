package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func transactionTotals(
	t *testing.T,
	client *testClient,
	query url.Values,
) (string, string) {
	t.Helper()
	requestPath := "/api/transactions/totals/"
	if encoded := query.Encode(); encoded != "" {
		requestPath += "?" + encoded
	}
	payload := client.object(t, http.MethodGet, requestPath, nil, http.StatusOK)
	return mustString(t, payload["income"]), mustString(t, payload["expense"])
}

func expectTransactionTotals(
	t *testing.T,
	client *testClient,
	query url.Values,
	wantIncome, wantExpense string,
) {
	t.Helper()
	income, expense := transactionTotals(t, client, query)
	if income != wantIncome || expense != wantExpense {
		t.Fatalf("汇总 income/expense = %s/%s，期望 %s/%s；筛选: %s",
			income, expense, wantIncome, wantExpense, query.Encode())
	}
}

func TestTransactionTotalsMatchFilters(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	mainAccount := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "汇总主账户", "balance": "0.00"}, http.StatusCreated)
	mainAccountID := jsonInt64(t, mainAccount["id"])
	secondAccount := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "汇总储蓄卡", "balance": "0.00"}, http.StatusCreated)
	secondAccountID := jsonInt64(t, secondAccount["id"])

	for _, transaction := range []map[string]any{
		{"account": mainAccountID, "kind": "expense", "amount": "0.10",
			"category": "餐饮", "date": "2026-10-01T00:00:00", "note": "早餐"},
		{"account": mainAccountID, "kind": "expense", "amount": "0.20",
			"category": "餐饮", "date": "2026-10-01T12:00:00", "note": "午餐"},
		{"account": mainAccountID, "kind": "income", "amount": "25.50",
			"category": "工资", "date": "2026-10-02T08:30:00", "note": "十月工资"},
		{"account": mainAccountID, "kind": "transfer", "amount": "99.99",
			"transfer_account": secondAccountID, "date": "2026-10-03T00:00:00",
			"note": "转到储蓄"},
		{"account": secondAccountID, "kind": "expense", "amount": "7.77",
			"category": "交通", "date": "2026-10-04T00:00:00", "note": "地铁 餐饮"},
		{"account": mainAccountID, "kind": "expense", "amount": "5.00",
			"category": "餐饮", "date": "2026-09-30T23:59:59"},
		{"account": mainAccountID, "kind": "income", "amount": "1.00",
			"category": "其他", "date": "2026-11-01T00:00:00"},
	} {
		admin.object(t, http.MethodPost, "/api/transactions/", transaction, http.StatusCreated)
	}

	// Empty filters cover every transaction of the household. The transfer
	// never contributes 99.99 to either side.
	expectTransactionTotals(t, admin, url.Values{}, "26.50", "13.07")

	// Exact cents: 0.10 + 0.20 + 7.77 in the calendar month; September and
	// November rows stay outside.
	expectTransactionTotals(t, admin, url.Values{"month": {"2026-10"}}, "25.50", "8.07")
	expectTransactionTotals(t, admin, url.Values{"month": {"2026-12"}}, "0.00", "0.00")

	// start/end day ranges are inclusive and identical to the listing filter.
	expectTransactionTotals(t, admin, url.Values{
		"start": {"2026-09-30"}, "end": {"2026-10-01"},
	}, "0.00", "5.30")
	expectTransactionTotals(t, admin, url.Values{
		"start": {"2026-10-01"}, "end": {"2026-10-01"},
	}, "0.00", "0.30")

	// Account, kind and category filters intersect exactly like the list.
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "account": {fmt.Sprintf("%d", mainAccountID)},
	}, "25.50", "0.30")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "account": {fmt.Sprintf("%d", secondAccountID)},
	}, "0.00", "7.77")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "kind": {"expense"},
	}, "0.00", "8.07")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "kind": {"income"},
	}, "25.50", "0.00")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "kind": {"transfer"},
	}, "0.00", "0.00")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "category": {"餐饮", "交通"},
	}, "0.00", "8.07")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "category": {"餐饮"}, "kind": {"income"},
	}, "0.00", "0.00")

	// q matches category, note and transfer destination name; only the
	// income/expense rows behind those matches are summed.
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "q": {"工资"},
	}, "25.50", "0.00")
	expectTransactionTotals(t, admin, url.Values{
		"month": {"2026-10"}, "q": {"餐饮"},
	}, "0.00", "8.07")

	transferSearch := url.Values{"month": {"2026-10"}, "q": {"储蓄卡"}}
	expectTransactionTotals(t, admin, transferSearch, "0.00", "0.00")
	if got := len(admin.array(t, http.MethodGet,
		"/api/transactions/?"+transferSearch.Encode(), nil, http.StatusOK)); got != 1 {
		t.Fatalf("按转入账户名搜索应命中 1 条转账，实际 %d 条", got)
	}

	// A second household in the same month must not leak into these totals.
	_, _ = env.seedUser(t, "汇总隔离家庭", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)
	bobAccount := bob.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "乙汇总账户", "balance": "0.00"}, http.StatusCreated)
	bobAccountID := jsonInt64(t, bobAccount["id"])
	for _, transaction := range []map[string]any{
		{"account": bobAccountID, "kind": "expense", "amount": "100.00",
			"category": "餐饮", "date": "2026-10-05T00:00:00"},
		{"account": bobAccountID, "kind": "income", "amount": "50.00",
			"category": "工资", "date": "2026-10-06T00:00:00"},
	} {
		bob.object(t, http.MethodPost, "/api/transactions/", transaction, http.StatusCreated)
	}
	expectTransactionTotals(t, bob, url.Values{"month": {"2026-10"}}, "50.00", "100.00")
	expectTransactionTotals(t, admin, url.Values{"month": {"2026-10"}}, "25.50", "8.07")

	// The new endpoint is authenticated like the listing endpoint.
	anonymous := newRawClient(t, env)
	anonymous.do(http.MethodGet, "/api/transactions/totals/?month=2026-10", nil).
		expect(t, http.StatusUnauthorized)

	// Representative invalid parameters reuse the list parser and field errors.
	for _, invalidCase := range []struct {
		name  string
		query url.Values
		field string
	}{
		{"非法月份", url.Values{"month": {"2026-13"}}, "month"},
		{"缺少结束日期", url.Values{"start": {"2026-10-01"}}, "end"},
		{"月份与范围冲突", url.Values{
			"month": {"2026-10"}, "start": {"2026-10-01"}, "end": {"2026-10-02"},
		}, "month"},
		{"非法类型", url.Values{"kind": {"refund"}}, "kind"},
		{"非法账户", url.Values{"account": {"0"}}, "account"},
	} {
		response := admin.do(http.MethodGet,
			"/api/transactions/totals/?"+invalidCase.query.Encode(), nil)
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)[invalidCase.field]; !exists {
			t.Fatalf("%s 应返回 %s 字段错误: %s",
				invalidCase.name, invalidCase.field, string(response.body))
		}
	}
}
