package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func transactionOwnerFields(t *testing.T, transaction map[string]any) (int64, string) {
	t.Helper()
	owner, exists := transaction["owner"]
	if !exists {
		t.Fatalf("交易响应缺少 owner 字段: %s", mustJSON(t, transaction))
	}
	if owner == nil {
		t.Fatalf("交易归属不应为空: %s", mustJSON(t, transaction))
	}
	name, exists := transaction["owner_name"]
	if !exists {
		t.Fatalf("交易响应缺少 owner_name 字段: %s", mustJSON(t, transaction))
	}
	return jsonInt64(t, owner), mustString(t, name)
}

func findTestTransaction(t *testing.T, transactions []map[string]any, id int64) map[string]any {
	t.Helper()
	for _, transaction := range transactions {
		if jsonInt64(t, transaction["id"]) == id {
			return transaction
		}
	}
	t.Fatalf("交易 %d 不在列表中: %s", id, mustJSON(t, transactions))
	return nil
}

func TestTransactionOwnerAssignment(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])

	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)
	alice.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"nickname": "艾丽丝"}, http.StatusOK)
	bobCreated := admin.createUser(t, "bob", strongPassword)
	bobID := jsonInt64(t, bobCreated["id"])

	wallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "家庭账户", "balance": "100.00",
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	savings := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "储蓄账户", "balance": "0.00",
	}, http.StatusCreated)
	savingsID := jsonInt64(t, savings["id"])

	// Omitting owner attributes a new transaction to the recording member,
	// independent of the wallet owner.
	expense := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "12.34",
		"category": "餐饮", "date": "2026-10-01T12:00:00",
	}, http.StatusCreated)
	expenseID := jsonInt64(t, expense["id"])
	owner, ownerName := transactionOwnerFields(t, expense)
	if owner != aliceID || ownerName != "艾丽丝" {
		t.Fatalf("省略归属应默认记录人: %s", mustJSON(t, expense))
	}
	if jsonInt64(t, expense["created_by"]) != aliceID ||
		mustString(t, expense["created_by_name"]) != "艾丽丝" {
		t.Fatalf("默认归属不应改变记录人: %s", mustJSON(t, expense))
	}

	// A member may record any kind of transaction for another active member.
	income := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "20.00",
		"category": "礼金", "date": "2026-10-02T09:00:00", "owner": bobID,
	}, http.StatusCreated)
	incomeID := jsonInt64(t, income["id"])
	owner, ownerName = transactionOwnerFields(t, income)
	if owner != bobID || ownerName != "bob" {
		t.Fatalf("成员应可代另一位成员记账: %s", mustJSON(t, income))
	}
	if jsonInt64(t, income["created_by"]) != aliceID ||
		mustString(t, income["created_by_name"]) != "艾丽丝" {
		t.Fatalf("代记账不应改变记录人: %s", mustJSON(t, income))
	}

	transfer := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "transfer", "amount": "5.00",
		"date": "2026-10-03T10:00:00", "transfer_account": savingsID,
		"owner": bobID,
	}, http.StatusCreated)
	transferID := jsonInt64(t, transfer["id"])
	owner, ownerName = transactionOwnerFields(t, transfer)
	if owner != bobID || ownerName != "bob" ||
		jsonInt64(t, transfer["created_by"]) != aliceID {
		t.Fatalf("转账也应保留独立的归属和记录人: %s", mustJSON(t, transfer))
	}

	// The administrator is an active household member, so self-attribution is
	// allowed by default and another member may attribute to the administrator.
	adminTransaction := admin.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "1.00",
		"category": "补贴", "date": "2026-10-04T08:00:00",
	}, http.StatusCreated)
	owner, ownerName = transactionOwnerFields(t, adminTransaction)
	if owner != adminID || ownerName != adminUsername ||
		jsonInt64(t, adminTransaction["created_by"]) != adminID {
		t.Fatalf("管理员省略归属应默认自己: %s", mustJSON(t, adminTransaction))
	}
	adminOwned := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "2.00",
		"category": "餐饮", "date": "2026-10-04T09:00:00", "owner": adminID,
	}, http.StatusCreated)
	owner, ownerName = transactionOwnerFields(t, adminOwned)
	if owner != adminID || ownerName != adminUsername ||
		jsonInt64(t, adminOwned["created_by"]) != aliceID {
		t.Fatalf("成员应可归属给管理员: %s", mustJSON(t, adminOwned))
	}

	// Attribution names are derived on every read, so nicknames update.
	admin.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", bobID),
		map[string]any{"nickname": "鲍勃"}, http.StatusOK)
	transactions := admin.array(t, http.MethodGet, "/api/transactions/", nil, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, findTestTransaction(t, transactions, incomeID))
	if owner != bobID || ownerName != "鲍勃" {
		t.Fatalf("交易归属名称应随昵称更新: %s", mustJSON(t, transactions))
	}

	// A patch that omits owner preserves the current attribution.
	patched := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", incomeID),
		map[string]any{"amount": "22.00", "note": "改金额"}, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, patched)
	if owner != bobID || ownerName != "鲍勃" ||
		mustString(t, patched["amount"]) != "22.00" ||
		jsonInt64(t, patched["created_by"]) != aliceID {
		t.Fatalf("省略归属的更新不应改变归属或记录人: %s", mustJSON(t, patched))
	}

	// An explicit owner changes attribution without changing the recorder.
	reassigned := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", incomeID),
		map[string]any{"owner": aliceID}, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, reassigned)
	if owner != aliceID || ownerName != "艾丽丝" ||
		jsonInt64(t, reassigned["created_by"]) != aliceID ||
		mustString(t, reassigned["created_by_name"]) != "艾丽丝" {
		t.Fatalf("修改归属不应改变记录人: %s", mustJSON(t, reassigned))
	}

	// The transfer's attribution remains untouched by other edits.
	transfer = alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", transferID),
		map[string]any{"note": "转账备注"}, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, transfer)
	if owner != bobID || ownerName != "鲍勃" ||
		jsonInt64(t, transfer["created_by"]) != aliceID {
		t.Fatalf("更新转账不应改变归属: %s", mustJSON(t, transfer))
	}

	// Migration 0008 makes attribution mandatory, so no stored transaction can
	// lack an owner; the same migration backfills existing rows to the recorder.
	if got := env.scalarInt(t,
		"SELECT count(*) FROM transactions WHERE owner_user_id IS NULL"); got != 0 {
		t.Fatalf("交易归属列存在 NULL，数量 %d", got)
	}
	if got := env.scalarInt(t, `
		SELECT count(*) FROM pg_attribute
		WHERE attrelid = 'transactions'::regclass
		  AND attname = 'owner_user_id' AND attnotnull`); got != 1 {
		t.Fatal("交易归属列应为 NOT NULL")
	}
	if got := env.scalarInt(t, `
		SELECT count(*) FROM pg_constraint
		WHERE conname = 'transactions_owner_fk' AND contype = 'f'
		  AND conrelid = 'transactions'::regclass`); got != 1 {
		t.Fatal("交易归属缺少同家庭复合外键")
	}
	if got := env.scalarInt(t,
		"SELECT count(*) FROM transactions WHERE id = $1 AND owner_user_id = $2",
		expenseID, aliceID); got != 1 {
		t.Fatalf("默认归属未持久化到 owner_user_id")
	}
}

func TestTransactionOwnerValidation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)

	wallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "家庭账户", "balance": "0.00",
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])

	malformedOwners := []any{"1", 1.5, 0, -7, true, map[string]any{"id": 1}, []any{1}}
	transactionsBefore := env.count(t, "transactions")
	for _, owner := range malformedOwners {
		response := alice.do(http.MethodPost, "/api/transactions/", map[string]any{
			"account": walletID, "kind": "expense", "amount": "1.00",
			"category": "餐饮", "date": "2026-10-01T00:00:00",
			"owner": owner,
		})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("非法 owner %#v 应返回 owner 字段错误: %s", owner, string(response.body))
		}
	}
	nullCreate := alice.do(http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "1.00",
		"category": "餐饮", "date": "2026-10-01T00:00:00", "owner": nil,
	})
	nullCreate.expect(t, http.StatusBadRequest)
	if _, exists := nullCreate.object(t)["owner"]; !exists {
		t.Fatalf("显式 null 归属应返回 owner 字段错误: %s", string(nullCreate.body))
	}
	if got := env.count(t, "transactions"); got != transactionsBefore {
		t.Fatalf("非法归属不应创建交易，交易数 %d，期望 %d", got, transactionsBefore)
	}

	// A foreign-household member and a nonexistent id are indistinguishable.
	_, bobID := env.seedUser(t, "乙家", "bob", strongPassword, false)
	foreign := alice.do(http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "1.00",
		"category": "餐饮", "date": "2026-10-01T00:00:00", "owner": bobID,
	})
	foreign.expect(t, http.StatusBadRequest)
	missing := alice.do(http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "1.00",
		"category": "餐饮", "date": "2026-10-01T00:00:00", "owner": 999999,
	})
	missing.expect(t, http.StatusBadRequest)
	for _, response := range []testResponse{foreign, missing} {
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("不可用归属应返回 owner 字段错误: %s", string(response.body))
		}
	}
	if string(foreign.body) != string(missing.body) {
		t.Fatalf("其他家庭成员与不存在的归属应返回相同错误: %s / %s",
			string(foreign.body), string(missing.body))
	}
	if got := env.count(t, "transactions"); got != transactionsBefore {
		t.Fatalf("跨家庭归属不应创建交易，交易数 %d，期望 %d", got, transactionsBefore)
	}

	transaction := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "3.00",
		"category": "餐饮", "date": "2026-10-02T00:00:00", "note": "原始",
	}, http.StatusCreated)
	transactionID := jsonInt64(t, transaction["id"])

	invalidPatches := append(append([]any{}, malformedOwners...), nil)
	for _, owner := range invalidPatches {
		response := alice.do(http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", transactionID),
			map[string]any{"amount": "9.99", "note": "被篡改", "owner": owner})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("非法 owner %#v 更新应返回 owner 字段错误: %s",
				owner, string(response.body))
		}
	}
	for _, owner := range []any{bobID, 999999} {
		response := alice.do(http.MethodPatch,
			fmt.Sprintf("/api/transactions/%d/", transactionID),
			map[string]any{"amount": "9.99", "owner": owner})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("不可用归属更新应返回 owner 字段错误: %s", string(response.body))
		}
	}

	stored := findTestTransaction(t, alice.array(
		t, http.MethodGet, "/api/transactions/", nil, http.StatusOK), transactionID)
	owner, ownerName := transactionOwnerFields(t, stored)
	if mustString(t, stored["amount"]) != "3.00" ||
		mustString(t, stored["note"]) != "原始" ||
		owner != aliceID || ownerName != "alice" ||
		jsonInt64(t, stored["created_by"]) != aliceID {
		t.Fatalf("失败的归属更新不应修改交易: %s", mustJSON(t, stored))
	}
}

func TestTransactionOwnerSoftDeletedMember(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	charlieCreated := admin.createUser(t, "charlie", strongPassword)
	charlieID := jsonInt64(t, charlieCreated["id"])
	charlie := env.loginUser(t, "charlie", strongPassword)
	charlie.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", charlieID),
		map[string]any{"nickname": "查理"}, http.StatusOK)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)

	wallet := admin.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "共享账户", "balance": "0.00",
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])
	selfRecorded := charlie.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "12.00",
		"category": "餐饮", "date": "2026-10-01T12:00:00",
	}, http.StatusCreated)
	selfRecordedID := jsonInt64(t, selfRecorded["id"])
	assigned := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "income", "amount": "6.00",
		"category": "礼金", "date": "2026-10-02T12:00:00", "owner": charlieID,
	}, http.StatusCreated)
	assignedID := jsonInt64(t, assigned["id"])

	admin.do(http.MethodDelete, fmt.Sprintf("/api/users/%d/", charlieID), nil).
		expect(t, http.StatusNoContent)

	// Reads keep both the recorder and the owner labels after soft deletion.
	for _, transactionID := range []int64{selfRecordedID, assignedID} {
		stored := findTestTransaction(t, alice.array(
			t, http.MethodGet, "/api/transactions/", nil, http.StatusOK), transactionID)
		owner, ownerName := transactionOwnerFields(t, stored)
		if owner != charlieID || ownerName != "查理" {
			t.Fatalf("软删除成员的归属应继续可读: %s", mustJSON(t, stored))
		}
	}
	stored := findTestTransaction(t, alice.array(
		t, http.MethodGet, "/api/transactions/", nil, http.StatusOK), selfRecordedID)
	if jsonInt64(t, stored["created_by"]) != charlieID ||
		mustString(t, stored["created_by_name"]) != "查理" {
		t.Fatalf("软删除成员的记录人应继续可读: %s", mustJSON(t, stored))
	}

	// Editing a former member's transaction while omitting owner still works.
	patched := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", selfRecordedID),
		map[string]any{"amount": "13.00", "note": "删除后修改"}, http.StatusOK)
	owner, ownerName := transactionOwnerFields(t, patched)
	if owner != charlieID || ownerName != "查理" ||
		jsonInt64(t, patched["created_by"]) != charlieID ||
		mustString(t, patched["created_by_name"]) != "查理" {
		t.Fatalf("省略 owner 应保留已删除成员的归属: %s", mustJSON(t, patched))
	}

	// Resending the unchanged owner also skips reassignment validation.
	unchanged := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", assignedID),
		map[string]any{"amount": "7.00", "owner": charlieID}, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, unchanged)
	if owner != charlieID || ownerName != "查理" ||
		mustString(t, unchanged["amount"]) != "7.00" {
		t.Fatalf("未变更的历史归属应可继续编辑: %s", mustJSON(t, unchanged))
	}

	// Assigning the departed member to another transaction is rejected
	// atomically.
	editable := alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "4.00",
		"category": "餐饮", "date": "2026-10-03T12:00:00",
	}, http.StatusCreated)
	editableID := jsonInt64(t, editable["id"])
	rejected := alice.do(http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", editableID),
		map[string]any{"amount": "99.00", "owner": charlieID})
	rejected.expect(t, http.StatusBadRequest)
	if _, exists := rejected.object(t)["owner"]; !exists {
		t.Fatalf("已删除成员归属应返回 owner 字段错误: %s", string(rejected.body))
	}
	stored = findTestTransaction(t, alice.array(
		t, http.MethodGet, "/api/transactions/", nil, http.StatusOK), editableID)
	owner, ownerName = transactionOwnerFields(t, stored)
	if mustString(t, stored["amount"]) != "4.00" ||
		owner != aliceID || ownerName != "alice" {
		t.Fatalf("失败的重新归属不应修改交易: %s", mustJSON(t, stored))
	}

	transactionsBefore := env.count(t, "transactions")
	created := alice.do(http.MethodPost, "/api/transactions/", map[string]any{
		"account": walletID, "kind": "expense", "amount": "1.00",
		"category": "餐饮", "date": "2026-10-04T12:00:00", "owner": charlieID,
	})
	created.expect(t, http.StatusBadRequest)
	if _, exists := created.object(t)["owner"]; !exists {
		t.Fatalf("为已删除成员记账应返回 owner 字段错误: %s", string(created.body))
	}
	if got := env.count(t, "transactions"); got != transactionsBefore {
		t.Fatalf("已删除成员不应创建交易，交易数 %d，期望 %d", got, transactionsBefore)
	}

	// A historical attribution can still be reassigned to an active member.
	reassigned := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/transactions/%d/", selfRecordedID),
		map[string]any{"owner": aliceID}, http.StatusOK)
	owner, ownerName = transactionOwnerFields(t, reassigned)
	if owner != aliceID || ownerName != "alice" ||
		jsonInt64(t, reassigned["created_by"]) != charlieID ||
		mustString(t, reassigned["created_by_name"]) != "查理" {
		t.Fatalf("历史归属应可改为现有成员且不改变记录人: %s", mustJSON(t, reassigned))
	}
}
