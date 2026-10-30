package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func assertNoOpeningBalance(t *testing.T, account map[string]any) {
	t.Helper()
	if _, exists := account["opening_balance"]; exists {
		t.Fatalf("账户响应不应再暴露期初余额: %s", mustJSON(t, account))
	}
}

func expectAccountBalances(t *testing.T, client *testClient, expected map[int64]string) {
	t.Helper()
	accounts := client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	for accountID, want := range expected {
		account := findTestAccount(t, accounts, accountID)
		if got := mustString(t, account["balance"]); got != want {
			t.Fatalf("账户 %d 余额 = %q，期望 %q: %s",
				accountID, got, want, mustJSON(t, account))
		}
		assertNoOpeningBalance(t, account)
	}
}

func expectTotalBalance(t *testing.T, client *testClient, month, want string) {
	t.Helper()
	summary := client.object(t,
		http.MethodGet, "/api/summary/?month="+month, nil, http.StatusOK)
	if got := mustString(t, summary["total_balance"]); got != want {
		t.Fatalf("家庭总余额 = %q，期望 %q: %s", got, want, mustJSON(t, summary))
	}
}

func TestAccountBalanceCorrectionAndLedgerChanges(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	admin.createUser(t, "alice", strongPassword)
	client := env.loginUser(t, "alice", strongPassword)

	wallet := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "现金", "balance": "100.00"}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	savings := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "储蓄", "balance": 0}, http.StatusCreated)
	savingsID := jsonInt64(t, savings["id"])
	assertNoOpeningBalance(t, wallet)

	client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "50.00",
		"category": "工资", "date": "2026-10-01T00:00:00",
	}, http.StatusCreated)
	expense := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "20.00",
		"category": "餐饮", "date": "2026-10-02T00:00:00",
	}, http.StatusCreated)
	expenseID := jsonInt64(t, expense["id"])
	transfer := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "transfer", "amount": "30.00",
		"date": "2026-10-03T00:00:00", "transfer_account": savingsID,
	}, http.StatusCreated)
	transferID := jsonInt64(t, transfer["id"])
	expectAccountBalances(t, client, map[int64]string{walletID: "100.00", savingsID: "30.00"})

	// Correcting the current balance only adjusts the hidden opening balance.
	patched := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"balance": "200.00"}, http.StatusOK)
	if mustString(t, patched["balance"]) != "200.00" {
		t.Fatalf("校正当前余额失败: %s", mustJSON(t, patched))
	}
	assertNoOpeningBalance(t, patched)
	expectAccountBalances(t, client, map[int64]string{walletID: "200.00", savingsID: "30.00"})
	expectTotalBalance(t, client, "2026-10", "230.00")

	// The destination of an incoming transfer can be corrected independently.
	patched = client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", savingsID),
		map[string]any{"balance": 75}, http.StatusOK)
	if mustString(t, patched["balance"]) != "75.00" {
		t.Fatalf("数字形式的当前余额应被接受: %s", mustJSON(t, patched))
	}
	assertNoOpeningBalance(t, patched)
	expectAccountBalances(t, client, map[int64]string{walletID: "200.00", savingsID: "75.00"})

	// Negative current balances remain valid.
	patched = client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"balance": "-25.00"}, http.StatusOK)
	if mustString(t, patched["balance"]) != "-25.00" {
		t.Fatalf("负当前余额应被接受: %s", mustJSON(t, patched))
	}
	expectAccountBalances(t, client, map[int64]string{walletID: "-25.00", savingsID: "75.00"})

	// Ledger mutations after a correction keep changing balances normally.
	income := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": savingsID, "kind": "income", "amount": "10.00",
		"category": "工资", "date": "2026-10-04T00:00:00",
	}, http.StatusCreated)
	incomeID := jsonInt64(t, income["id"])
	expectAccountBalances(t, client, map[int64]string{walletID: "-25.00", savingsID: "85.00"})

	client.object(t, http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", incomeID),
		map[string]any{"amount": "15.00"}, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{walletID: "-25.00", savingsID: "90.00"})

	client.object(t, http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", incomeID),
		map[string]any{"account": walletID}, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{walletID: "-10.00", savingsID: "75.00"})

	client.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", incomeID), nil).
		expect(t, http.StatusNoContent)
	expectAccountBalances(t, client, map[int64]string{walletID: "-25.00", savingsID: "75.00"})

	client.object(t, http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{"amount": "40.00"}, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{walletID: "-35.00", savingsID: "85.00"})

	client.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", transferID), nil).
		expect(t, http.StatusNoContent)
	expectAccountBalances(t, client, map[int64]string{walletID: "5.00", savingsID: "45.00"})

	client.object(t, http.MethodPatch, fmt.Sprintf("/api/transactions/%d/", expenseID),
		map[string]any{"amount": "5.00"}, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{walletID: "20.00", savingsID: "45.00"})

	client.do(http.MethodDelete, fmt.Sprintf("/api/transactions/%d/", expenseID), nil).
		expect(t, http.StatusNoContent)
	expectAccountBalances(t, client, map[int64]string{walletID: "25.00", savingsID: "45.00"})
	expectTotalBalance(t, client, "2026-10", "70.00")
}

func TestAccountPatchWithoutBalancePreservesBalance(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	client := env.loginUser(t, "alice", strongPassword)

	wallet := client.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "工资卡", "balance": "100.00", "owner": aliceID,
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "20.00",
		"category": "工资", "date": "2026-10-01T00:00:00",
	}, http.StatusCreated)
	expectAccountBalances(t, client, map[int64]string{walletID: "120.00"})
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 10000 {
		t.Fatalf("内部期初余额 = %d，期望 10000", got)
	}

	renamed := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "新工资卡"}, http.StatusOK)
	if mustString(t, renamed["balance"]) != "120.00" {
		t.Fatalf("仅改名不应改变当前余额: %s", mustJSON(t, renamed))
	}
	assertNoOpeningBalance(t, renamed)

	cleared := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"owner": nil}, http.StatusOK)
	if mustString(t, cleared["balance"]) != "120.00" {
		t.Fatalf("仅改归属不应改变当前余额: %s", mustJSON(t, cleared))
	}
	assertNoOpeningBalance(t, cleared)

	assigned := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "共享工资卡", "owner": aliceID}, http.StatusOK)
	if mustString(t, assigned["balance"]) != "120.00" {
		t.Fatalf("同时改名和归属不应改变当前余额: %s", mustJSON(t, assigned))
	}
	assertNoOpeningBalance(t, assigned)
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 10000 {
		t.Fatalf("省略 balance 的更新改变了内部期初余额: %d", got)
	}
	expectAccountBalances(t, client, map[int64]string{walletID: "120.00"})
}

func TestAccountBalanceValidation(t *testing.T) {
	env := newTestEnv(t)
	client := env.adminClient(t)

	wallet := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "零钱"}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	if mustString(t, wallet["balance"]) != "0.00" {
		t.Fatalf("省略当前余额应默认为 0.00: %s", mustJSON(t, wallet))
	}
	assertNoOpeningBalance(t, wallet)

	for _, balance := range []any{
		"1.234",
		"10000000000.00",
		"",
		nil,
		true,
		map[string]any{"amount": "1.00"},
	} {
		response := client.do(http.MethodPatch,
			fmt.Sprintf("/api/accounts/%d/", walletID),
			map[string]any{"name": "零钱", "balance": balance})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["balance"]; !exists {
			t.Fatalf("非法余额 %#v 应返回 balance 字段错误: %s",
				balance, string(response.body))
		}
		expectAccountBalances(t, client, map[int64]string{walletID: "0.00"})
	}

	patched := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"balance": 12.5}, http.StatusOK)
	if mustString(t, patched["balance"]) != "12.50" {
		t.Fatalf("数字形式的当前余额错误: %s", mustJSON(t, patched))
	}
	assertNoOpeningBalance(t, patched)
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 1250 {
		t.Fatalf("当前余额写入内部期初余额 = %d，期望 1250", got)
	}
}

func TestAccountBalanceDerivedOpeningRange(t *testing.T) {
	env := newTestEnv(t)
	client := env.adminClient(t)

	wallet := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "边界账户", "balance": "0.00"}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "10.00",
		"category": "工资", "date": "2026-10-01T00:00:00",
	}, http.StatusCreated)
	expectAccountBalances(t, client, map[int64]string{walletID: "10.00"})

	// Requesting the minimum current balance after income would derive an
	// opening below the database check constraint.
	response := client.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "不应生效", "balance": "-9999999999.99"})
	response.expect(t, http.StatusBadRequest)
	if _, exists := response.object(t)["balance"]; !exists {
		t.Fatalf("越界期初余额应返回 balance 字段错误: %s", string(response.body))
	}
	stored := findTestAccount(t, client.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), walletID)
	if mustString(t, stored["name"]) != "边界账户" ||
		mustString(t, stored["balance"]) != "10.00" {
		t.Fatalf("失败的余额校正不应修改账户: %s", mustJSON(t, stored))
	}
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 0 {
		t.Fatalf("失败的余额校正改动了内部期初余额: %d", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM transactions"); got != 1 {
		t.Fatalf("失败的余额校正不应写入交易，实际 %d 条", got)
	}

	client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "20.00",
		"category": "餐饮", "date": "2026-10-02T00:00:00",
	}, http.StatusCreated)
	expectAccountBalances(t, client, map[int64]string{walletID: "-10.00"})

	// With a negative net the maximum current balance derives an opening above
	// the database range and must be rejected just as atomically.
	response = client.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"balance": "9999999999.99"})
	response.expect(t, http.StatusBadRequest)
	if _, exists := response.object(t)["balance"]; !exists {
		t.Fatalf("越界期初余额应返回 balance 字段错误: %s", string(response.body))
	}
	expectAccountBalances(t, client, map[int64]string{walletID: "-10.00"})
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 0 {
		t.Fatalf("失败的余额校正改动了内部期初余额: %d", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM transactions"); got != 2 {
		t.Fatalf("失败的余额校正不应写入交易，实际 %d 条", got)
	}

	patched := client.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"balance": "0.00"}, http.StatusOK)
	if mustString(t, patched["balance"]) != "0.00" {
		t.Fatalf("范围校验后正常校正失败: %s", mustJSON(t, patched))
	}
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 1000 {
		t.Fatalf("正常校正后的内部期初余额 = %d，期望 1000", got)
	}
}

func TestAccountBalanceHouseholdIsolation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	adminWallet := admin.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "甲家现金", "balance": "5.00"}, http.StatusCreated)
	adminWalletID := jsonInt64(t, adminWallet["id"])

	_, bobID := env.seedUser(t, "乙家", "bob", strongPassword, false)
	bob := env.loginUser(t, "bob", strongPassword)
	bobWallet := bob.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "乙家银行卡", "balance": "25.00", "owner": bobID,
	}, http.StatusCreated)
	bobWalletID := jsonInt64(t, bobWallet["id"])
	bob.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": bobWalletID, "kind": "income", "amount": "5.00",
		"category": "工资", "date": "2026-10-01T00:00:00",
	}, http.StatusCreated)
	expectAccountBalances(t, bob, map[int64]string{bobWalletID: "30.00"})
	expectTotalBalance(t, bob, "2026-10", "30.00")

	admin.do(http.MethodPatch, fmt.Sprintf("/api/accounts/%d/", bobWalletID),
		map[string]any{"balance": "999.00"}).expect(t, http.StatusNotFound)
	expectAccountBalances(t, bob, map[int64]string{bobWalletID: "30.00"})

	if accounts := bob.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK); len(accounts) != 1 {
		t.Fatalf("乙应只看到自己的账户: %s", mustJSON(t, accounts))
	}
	expectAccountBalances(t, admin, map[int64]string{adminWalletID: "5.00"})
	expectTotalBalance(t, admin, "2026-10", "5.00")
}

type accountAsyncResult struct {
	response testResponse
	err      error
}

func attemptAccountRequest(
	client *testClient,
	method, requestPath string,
	body any,
) accountAsyncResult {
	encoded, err := json.Marshal(body)
	if err != nil {
		return accountAsyncResult{err: err}
	}
	request, err := http.NewRequest(method, client.base+requestPath, bytes.NewReader(encoded))
	if err != nil {
		return accountAsyncResult{err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", client.base)
	if method != http.MethodGet && method != http.MethodHead {
		if token := client.cookieValue("csrftoken"); token != "" {
			request.Header.Set("X-CSRFToken", token)
		}
	}
	response, err := client.client.Do(request)
	if err != nil {
		return accountAsyncResult{err: err}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return accountAsyncResult{err: err}
	}
	return accountAsyncResult{
		response: testResponse{status: response.StatusCode, body: data},
	}
}

func awaitAccountResponse(
	t *testing.T,
	ctx context.Context,
	results <-chan accountAsyncResult,
) testResponse {
	t.Helper()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("并发账户请求失败: %v", result.err)
		}
		return result.response
	case <-ctx.Done():
		t.Fatal("等待并发账户请求完成超时")
		return testResponse{}
	}
}

func beginAccountRowLock(t *testing.T, env *testEnv, accountID int64) (pgx.Tx, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	tx, err := env.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("开启账户锁定事务失败: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var lockedID int64
	if err := tx.QueryRow(ctx,
		"SELECT id FROM accounts WHERE id = $1 FOR UPDATE", accountID).Scan(&lockedID); err != nil {
		t.Fatalf("锁定账户 %d 失败: %v", accountID, err)
	}
	if lockedID != accountID {
		t.Fatalf("锁定账户 = %d，期望 %d", lockedID, accountID)
	}
	return tx, ctx
}

func TestAccountBalancePatchWaitsForCommittedLedgerMutation(t *testing.T) {
	env := newTestEnv(t)
	client := env.adminClient(t)

	wallet := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "竞态账户", "balance": "0.00"}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])

	tx, ctx := beginAccountRowLock(t, env, walletID)
	results := make(chan accountAsyncResult, 1)
	go func() {
		results <- attemptAccountRequest(client, http.MethodPatch,
			fmt.Sprintf("/api/accounts/%d/", walletID),
			map[string]any{"balance": "100.00"})
	}()
	waitForBlockedQuery(t, env)

	var householdID, userID int64
	if err := tx.QueryRow(ctx,
		"SELECT household_id FROM accounts WHERE id = $1", walletID).
		Scan(&householdID); err != nil {
		t.Fatalf("读取账户家庭失败: %v", err)
	}
	if err := tx.QueryRow(ctx,
		"SELECT id FROM users WHERE household_id = $1 ORDER BY id LIMIT 1", householdID).
		Scan(&userID); err != nil {
		t.Fatalf("读取家庭用户失败: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions
			(household_id, account_id, kind, amount_cents, category, tx_date, note, created_by)
		VALUES ($1, $2, 'income', 1000, '工资', DATE '2026-10-01', '', $3)`,
		householdID, walletID, userID); err != nil {
		t.Fatalf("写入并发收入失败: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交并发收入失败: %v", err)
	}

	response := awaitAccountResponse(t, ctx, results)
	response.expect(t, http.StatusOK)
	patched := response.object(t)
	if mustString(t, patched["balance"]) != "100.00" {
		t.Fatalf("等待后校正的当前余额错误: %s", mustJSON(t, patched))
	}
	assertNoOpeningBalance(t, patched)

	expectAccountBalances(t, client, map[int64]string{walletID: "100.00"})
	if got := env.scalarInt(t,
		"SELECT opening_balance_cents FROM accounts WHERE id = $1", walletID); got != 9000 {
		t.Fatalf("校正后内部期初余额 = %d，期望 9000", got)
	}
	if got := env.scalarInt(t, "SELECT count(*) FROM transactions"); got != 1 {
		t.Fatalf("并发收入交易数量 = %d，期望 1", got)
	}
	expectTotalBalance(t, client, "2026-10", "100.00")
}

func TestAccountBalanceSerializesWithTransactionMutations(t *testing.T) {
	env := newTestEnv(t)
	client := env.adminClient(t)

	source := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "转出账户", "balance": "100.00"}, http.StatusCreated)
	sourceID := jsonInt64(t, source["id"])
	target := client.object(t, http.MethodPost, "/api/accounts/",
		map[string]any{"name": "转入账户", "balance": "0.00"}, http.StatusCreated)
	targetID := jsonInt64(t, target["id"])
	income := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": sourceID, "kind": "income", "amount": "10.00",
		"category": "工资", "date": "2026-10-01T00:00:00",
	}, http.StatusCreated)
	incomeID := jsonInt64(t, income["id"])
	transfer := client.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": sourceID, "kind": "transfer", "amount": "5.00",
		"date": "2026-10-02T00:00:00", "transfer_account": targetID,
	}, http.StatusCreated)
	transferID := jsonInt64(t, transfer["id"])
	expectAccountBalances(t, client, map[int64]string{sourceID: "105.00", targetID: "5.00"})

	// Creating a transaction on a locked account waits before mutating.
	tx, ctx := beginAccountRowLock(t, env, sourceID)
	results := make(chan accountAsyncResult, 1)
	go func() {
		results <- attemptAccountRequest(client, http.MethodPost, "/api/transactions/",
			map[string]any{
				"account": sourceID, "kind": "income", "amount": "20.00",
				"category": "工资", "date": "2026-10-03T00:00:00",
			})
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交创建等待失败: %v", err)
	}
	created := awaitAccountResponse(t, ctx, results)
	created.expect(t, http.StatusCreated)
	extraIncomeID := jsonInt64(t, created.object(t)["id"])
	expectAccountBalances(t, client, map[int64]string{sourceID: "125.00", targetID: "5.00"})

	// Updating a transaction waits on its current account.
	tx, ctx = beginAccountRowLock(t, env, sourceID)
	go func() {
		results <- attemptAccountRequest(client, http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", incomeID),
			map[string]any{"amount": "30.00"})
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交更新等待失败: %v", err)
	}
	awaitAccountResponse(t, ctx, results).expect(t, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{sourceID: "145.00", targetID: "5.00"})

	// Deleting a transaction waits on the account whose balance it changes.
	tx, ctx = beginAccountRowLock(t, env, sourceID)
	go func() {
		results <- attemptAccountRequest(client, http.MethodDelete,
			fmt.Sprintf("/api/transactions/%d/", incomeID), nil)
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交删除等待失败: %v", err)
	}
	awaitAccountResponse(t, ctx, results).expect(t, http.StatusNoContent)
	expectAccountBalances(t, client, map[int64]string{sourceID: "115.00", targetID: "5.00"})

	// Moving a transaction waits on the old account, whose balance it removes.
	tx, ctx = beginAccountRowLock(t, env, sourceID)
	go func() {
		results <- attemptAccountRequest(client, http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", extraIncomeID),
			map[string]any{"account": targetID})
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交移动等待失败: %v", err)
	}
	awaitAccountResponse(t, ctx, results).expect(t, http.StatusOK)
	expectAccountBalances(t, client, map[int64]string{sourceID: "95.00", targetID: "25.00"})

	// A transfer create waits on its destination account as well.
	tx, ctx = beginAccountRowLock(t, env, targetID)
	go func() {
		results <- attemptAccountRequest(client, http.MethodPost, "/api/transactions/",
			map[string]any{
				"account": sourceID, "kind": "transfer", "amount": "10.00",
				"date": "2026-10-04T00:00:00", "transfer_account": targetID,
			})
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交转账等待失败: %v", err)
	}
	awaitAccountResponse(t, ctx, results).expect(t, http.StatusCreated)
	expectAccountBalances(t, client, map[int64]string{sourceID: "85.00", targetID: "35.00"})

	// Deleting a transfer waits on its source account.
	tx, ctx = beginAccountRowLock(t, env, sourceID)
	go func() {
		results <- attemptAccountRequest(client, http.MethodDelete,
			fmt.Sprintf("/api/transactions/%d/", transferID), nil)
	}()
	waitForBlockedQuery(t, env)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交转账删除等待失败: %v", err)
	}
	awaitAccountResponse(t, ctx, results).expect(t, http.StatusNoContent)
	expectAccountBalances(t, client, map[int64]string{sourceID: "90.00", targetID: "30.00"})
	expectTotalBalance(t, client, "2026-10", "120.00")
}
