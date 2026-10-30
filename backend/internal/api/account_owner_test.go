package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func accountOwnerFields(t *testing.T, account map[string]any) (any, string) {
	t.Helper()
	owner, exists := account["owner"]
	if !exists {
		t.Fatalf("账户响应缺少 owner 字段: %s", mustJSON(t, account))
	}
	name, exists := account["owner_name"]
	if !exists {
		t.Fatalf("账户响应缺少 owner_name 字段: %s", mustJSON(t, account))
	}
	return owner, mustString(t, name)
}

func findTestAccount(t *testing.T, accounts []map[string]any, id int64) map[string]any {
	t.Helper()
	for _, account := range accounts {
		if jsonInt64(t, account["id"]) == id {
			return account
		}
	}
	t.Fatalf("账户 %d 不在列表中: %s", id, mustJSON(t, accounts))
	return nil
}

func TestAccountOwnerAssignment(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)

	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)
	bobCreated := admin.createUser(t, "bob", strongPassword)
	bobID := jsonInt64(t, bobCreated["id"])
	alice.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"nickname": "艾丽丝"}, http.StatusOK)

	// An omitted owner and an explicit JSON null both create public wallets, and
	// both owner fields are always present on the wire.
	publicWalletIDs := make([]int64, 0, 2)
	for _, body := range []map[string]any{
		{"name": "公共现金", "balance": "10.00"},
		{"name": "公共银行卡", "balance": "20.00", "owner": nil},
	} {
		account := alice.object(t, http.MethodPost, "/api/accounts/", body, http.StatusCreated)
		publicWalletIDs = append(publicWalletIDs, jsonInt64(t, account["id"]))
		owner, ownerName := accountOwnerFields(t, account)
		if owner != nil || ownerName != "" {
			t.Fatalf("公开账户归属应为空: %s", mustJSON(t, account))
		}
	}

	// A non-admin member may assign any active non-admin household member,
	// including themself and another ordinary member.
	aliceWallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小艾钱包", "balance": "30.00", "owner": aliceID,
	}, http.StatusCreated)
	aliceWalletID := jsonInt64(t, aliceWallet["id"])
	owner, ownerName := accountOwnerFields(t, aliceWallet)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "艾丽丝" {
		t.Fatalf("成员创建自己名下账户失败: %s", mustJSON(t, aliceWallet))
	}

	bobWallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小鲍钱包", "owner": bobID,
	}, http.StatusCreated)
	bobWalletID := jsonInt64(t, bobWallet["id"])
	owner, ownerName = accountOwnerFields(t, bobWallet)
	if owner == nil || jsonInt64(t, owner) != bobID || ownerName != "bob" {
		t.Fatalf("成员应可为另一位普通成员创建名下账户: %s", mustJSON(t, bobWallet))
	}

	// The label is derived on every read, so nickname changes are reflected.
	alice.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"nickname": "小艾"}, http.StatusOK)
	accounts := admin.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	if len(accounts) != 4 {
		t.Fatalf("家庭成员应看到全部 4 个账户: %s", mustJSON(t, accounts))
	}
	for _, publicWalletID := range publicWalletIDs {
		owner, ownerName = accountOwnerFields(t, findTestAccount(t, accounts, publicWalletID))
		if owner != nil || ownerName != "" {
			t.Fatalf("列表中的公开账户归属应为空: %s", mustJSON(t, accounts))
		}
	}
	owner, ownerName = accountOwnerFields(t, findTestAccount(t, accounts, aliceWalletID))
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "小艾" {
		t.Fatalf("账户归属名称应随昵称更新: %s", mustJSON(t, accounts))
	}

	// PATCH without an owner preserves it.
	patched := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", aliceWalletID),
		map[string]any{"name": "小艾的钱包"}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, patched)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "小艾" {
		t.Fatalf("省略 owner 的更新不应清除归属: %s", mustJSON(t, patched))
	}
	owner, ownerName = accountOwnerFields(t, findTestAccount(t, admin.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), aliceWalletID))
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "小艾" {
		t.Fatalf("管理员应看到相同的归属: %s", mustJSON(t, patched))
	}

	// An explicit null clears the owner; a positive integer assigns one.
	cleared := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", aliceWalletID),
		map[string]any{"owner": nil}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, cleared)
	if owner != nil || ownerName != "" {
		t.Fatalf("显式 null 应清除归属: %s", mustJSON(t, cleared))
	}

	assigned := admin.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", aliceWalletID),
		map[string]any{"owner": aliceID}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, assigned)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "小艾" {
		t.Fatalf("显式成员 ID 应设置归属: %s", mustJSON(t, assigned))
	}

	// Omitting owner while changing the current balance also preserves it.
	updated := admin.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", aliceWalletID),
		map[string]any{"balance": "40.00"}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, updated)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "小艾" ||
		mustString(t, updated["balance"]) != "40.00" {
		t.Fatalf("当前余额更新不应改变归属: %s", mustJSON(t, updated))
	}

	// The second owned wallet keeps its owner while another member edits it.
	renamed := admin.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", bobWalletID),
		map[string]any{"name": "小鲍的账户"}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, renamed)
	if owner == nil || jsonInt64(t, owner) != bobID || ownerName != "bob" {
		t.Fatalf("其他成员修改账户不应改变归属: %s", mustJSON(t, renamed))
	}
}

func TestAccountOwnerValidation(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])

	malformedOwners := []any{"1", 1.5, 0, -7, true, map[string]any{"id": 1}, []any{1}}
	createdBefore := env.count(t, "accounts")
	for index, owner := range malformedOwners {
		response := alice.do(http.MethodPost, "/api/accounts/", map[string]any{
			"name":  fmt.Sprintf("坏归属-%d", index),
			"owner": owner,
		})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("非法 owner %#v 应返回 owner 字段错误: %s", owner, string(response.body))
		}
	}
	if got := env.count(t, "accounts"); got != createdBefore {
		t.Fatalf("非法归属不应创建账户，账户数 %d，期望 %d", got, createdBefore)
	}

	// A foreign-household member and a nonexistent id are indistinguishable.
	_, bobID := env.seedUser(t, "乙家", "bob", strongPassword, false)
	foreign := alice.do(http.MethodPost, "/api/accounts/", map[string]any{
		"name": "跨家归属", "owner": bobID,
	})
	foreign.expect(t, http.StatusBadRequest)
	missing := alice.do(http.MethodPost, "/api/accounts/", map[string]any{
		"name": "跨家归属", "owner": 999999,
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
	if got := env.count(t, "accounts"); got != createdBefore {
		t.Fatalf("跨家庭归属不应创建账户，账户数 %d，期望 %d", got, createdBefore)
	}

	// Even an active administrator cannot be selected as an owner.
	adminOwner := alice.do(http.MethodPost, "/api/accounts/", map[string]any{
		"name": "管理员归属", "owner": adminID,
	})
	adminOwner.expect(t, http.StatusBadRequest)
	if _, exists := adminOwner.object(t)["owner"]; !exists {
		t.Fatalf("管理员归属应返回 owner 字段错误: %s", string(adminOwner.body))
	}
	if string(adminOwner.body) != string(missing.body) {
		t.Fatalf("管理员与不存在的归属应返回相同错误: %s / %s",
			string(adminOwner.body), string(missing.body))
	}
	if got := env.count(t, "accounts"); got != createdBefore {
		t.Fatalf("管理员归属不应创建账户，账户数 %d，期望 %d", got, createdBefore)
	}

	wallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小艾钱包", "balance": "12.34", "owner": aliceID,
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])

	foreignPatch := alice.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "被篡改", "owner": bobID})
	foreignPatch.expect(t, http.StatusBadRequest)
	if _, exists := foreignPatch.object(t)["owner"]; !exists {
		t.Fatalf("跨家庭归属更新应返回 owner 字段错误: %s", string(foreignPatch.body))
	}
	for index, owner := range malformedOwners {
		response := alice.do(http.MethodPatch,
			fmt.Sprintf("/api/accounts/%d/", walletID),
			map[string]any{"name": fmt.Sprintf("被篡改-%d", index), "owner": owner})
		response.expect(t, http.StatusBadRequest)
		if _, exists := response.object(t)["owner"]; !exists {
			t.Fatalf("非法 owner %#v 应返回 owner 字段错误: %s", owner, string(response.body))
		}
	}

	// Sending the administrator explicitly is rejected even alongside other
	// changes, leaving the wallet untouched.
	adminPatch := alice.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "被篡改", "balance": "99.99", "owner": adminID})
	adminPatch.expect(t, http.StatusBadRequest)
	if _, exists := adminPatch.object(t)["owner"]; !exists {
		t.Fatalf("管理员归属更新应返回 owner 字段错误: %s", string(adminPatch.body))
	}
	stored := findTestAccount(t, alice.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), walletID)
	owner, ownerName := accountOwnerFields(t, stored)
	if mustString(t, stored["name"]) != "小艾钱包" ||
		mustString(t, stored["balance"]) != "12.34" ||
		owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "alice" {
		t.Fatalf("失败的归属更新不应修改账户: %s", mustJSON(t, stored))
	}

	// Another household's wallet is still not writable, even with a valid owner.
	bob := env.loginUser(t, "bob", strongPassword)
	bobWallet := bob.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "乙家账户", "owner": bobID,
	}, http.StatusCreated)
	bobWalletID := jsonInt64(t, bobWallet["id"])
	alice.do(http.MethodPatch, fmt.Sprintf("/api/accounts/%d/", bobWalletID),
		map[string]any{"owner": aliceID}).expect(t, http.StatusNotFound)
}

func TestAccountOwnerSoftDeletedMember(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)
	alice.object(t, http.MethodPatch, fmt.Sprintf("/api/users/%d/", aliceID),
		map[string]any{"nickname": "艾丽丝"}, http.StatusOK)

	wallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小艾钱包", "balance": "20.00", "owner": aliceID,
	}, http.StatusCreated)
	walletID := jsonInt64(t, wallet["id"])

	admin.createUser(t, "bob", strongPassword)
	bob := env.loginUser(t, "bob", strongPassword)

	admin.do(http.MethodDelete, fmt.Sprintf("/api/users/%d/", aliceID), nil).
		expect(t, http.StatusNoContent)

	// The soft-deleted member's wallet keeps its owner id and display name.
	stored := findTestAccount(t, bob.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), walletID)
	owner, ownerName := accountOwnerFields(t, stored)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "艾丽丝" {
		t.Fatalf("软删除成员的归属应继续可读: %s", mustJSON(t, stored))
	}

	// Editing a former member's wallet while omitting owner still works.
	patched := bob.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "小艾的钱包", "balance": "25.00"}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, patched)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "艾丽丝" ||
		mustString(t, patched["balance"]) != "25.00" {
		t.Fatalf("省略 owner 应保留已删除成员的归属: %s", mustJSON(t, patched))
	}

	// Explicitly assigning a departed member is rejected and changes nothing.
	reassigned := admin.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"name": "被篡改", "owner": aliceID})
	reassigned.expect(t, http.StatusBadRequest)
	if _, exists := reassigned.object(t)["owner"]; !exists {
		t.Fatalf("已删除成员归属应返回 owner 字段错误: %s", string(reassigned.body))
	}
	stored = findTestAccount(t, bob.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), walletID)
	owner, ownerName = accountOwnerFields(t, stored)
	if mustString(t, stored["name"]) != "小艾的钱包" ||
		mustString(t, stored["balance"]) != "25.00" ||
		owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "艾丽丝" {
		t.Fatalf("失败的重新归属不应修改账户: %s", mustJSON(t, stored))
	}

	// Creating a wallet for a departed member is rejected atomically.
	accountsBefore := env.count(t, "accounts")
	created := bob.do(http.MethodPost, "/api/accounts/", map[string]any{
		"name": "幽灵钱包", "owner": aliceID,
	})
	created.expect(t, http.StatusBadRequest)
	if _, exists := created.object(t)["owner"]; !exists {
		t.Fatalf("已删除成员应返回 owner 字段错误: %s", string(created.body))
	}
	if got := env.count(t, "accounts"); got != accountsBefore {
		t.Fatalf("已删除成员不应创建账户，账户数 %d，期望 %d", got, accountsBefore)
	}

	// An explicit null clears the former member's label.
	cleared := bob.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", walletID),
		map[string]any{"owner": nil}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, cleared)
	if owner != nil || ownerName != "" {
		t.Fatalf("显式 null 应清除已删除成员的归属: %s", mustJSON(t, cleared))
	}
}

func TestAccountOwnerLegacyAdminWallet(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	adminMe := admin.object(t, http.MethodGet, "/api/auth/me/", nil, http.StatusOK)
	adminID := jsonInt64(t, mustMap(t, adminMe["user"])["id"])
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)

	// Seed wallets the way an older build could have: the API now rejects
	// assigning an administrator, so the legacy state is written directly.
	editableWallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "历史管理员钱包", "balance": "30.00",
	}, http.StatusCreated)
	editableWalletID := jsonInt64(t, editableWallet["id"])
	clearableWallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "待清空管理员钱包", "balance": "5.00",
	}, http.StatusCreated)
	clearableWalletID := jsonInt64(t, clearableWallet["id"])

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, walletID := range []int64{editableWalletID, clearableWalletID} {
		if _, err := env.db.Pool().Exec(ctx,
			"UPDATE accounts SET owner_user_id = $1 WHERE id = $2",
			adminID, walletID); err != nil {
			t.Fatalf("设置账户 %d 的历史管理员归属失败: %v", walletID, err)
		}
	}

	// Reads keep the legacy owner and its label.
	accounts := alice.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
	for _, walletID := range []int64{editableWalletID, clearableWalletID} {
		account := findTestAccount(t, accounts, walletID)
		owner, ownerName := accountOwnerFields(t, account)
		if owner == nil || jsonInt64(t, owner) != adminID || ownerName != adminUsername {
			t.Fatalf("历史管理员归属应继续可读: %s", mustJSON(t, account))
		}
	}

	// Omitting owner still edits the wallet and preserves the legacy owner.
	patched := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", editableWalletID),
		map[string]any{"name": "历史管理员账户", "balance": "35.00"}, http.StatusOK)
	owner, ownerName := accountOwnerFields(t, patched)
	if owner == nil || jsonInt64(t, owner) != adminID || ownerName != adminUsername ||
		mustString(t, patched["balance"]) != "35.00" {
		t.Fatalf("省略 owner 应保留历史管理员归属: %s", mustJSON(t, patched))
	}

	// Explicitly resending the administrator is rejected atomically.
	rejected := alice.do(http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", editableWalletID),
		map[string]any{"name": "被篡改", "balance": "99.99", "owner": adminID})
	rejected.expect(t, http.StatusBadRequest)
	if _, exists := rejected.object(t)["owner"]; !exists {
		t.Fatalf("历史管理员归属更新应返回 owner 字段错误: %s", string(rejected.body))
	}
	stored := findTestAccount(t, alice.array(
		t, http.MethodGet, "/api/accounts/", nil, http.StatusOK), editableWalletID)
	owner, ownerName = accountOwnerFields(t, stored)
	if mustString(t, stored["name"]) != "历史管理员账户" ||
		mustString(t, stored["balance"]) != "35.00" ||
		owner == nil || jsonInt64(t, owner) != adminID || ownerName != adminUsername {
		t.Fatalf("失败的管理员归属更新不应修改账户: %s", mustJSON(t, stored))
	}

	// The legacy owner can be reassigned to an ordinary member.
	reassigned := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", editableWalletID),
		map[string]any{"owner": aliceID}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, reassigned)
	if owner == nil || jsonInt64(t, owner) != aliceID || ownerName != "alice" {
		t.Fatalf("历史管理员归属应可改为普通成员: %s", mustJSON(t, reassigned))
	}

	// Or cleared back to public.
	cleared := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", clearableWalletID),
		map[string]any{"owner": nil}, http.StatusOK)
	owner, ownerName = accountOwnerFields(t, cleared)
	if owner != nil || ownerName != "" {
		t.Fatalf("显式 null 应清除历史管理员归属: %s", mustJSON(t, cleared))
	}
}

func TestAccountOwnerBalancesAndSharing(t *testing.T) {
	env := newTestEnv(t)
	admin := env.adminClient(t)
	aliceCreated := admin.createUser(t, "alice", strongPassword)
	aliceID := jsonInt64(t, aliceCreated["id"])
	alice := env.loginUser(t, "alice", strongPassword)
	bobCreated := admin.createUser(t, "bob", strongPassword)
	bobID := jsonInt64(t, bobCreated["id"])

	publicWallet := admin.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "公共账户", "balance": "100.00",
	}, http.StatusCreated)
	publicWalletID := jsonInt64(t, publicWallet["id"])
	aliceWallet := admin.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小艾钱包", "balance": "50.00", "owner": aliceID,
	}, http.StatusCreated)
	aliceWalletID := jsonInt64(t, aliceWallet["id"])
	bobWallet := alice.object(t, http.MethodPost, "/api/accounts/", map[string]any{
		"name": "小鲍钱包", "balance": "30.00", "owner": bobID,
	}, http.StatusCreated)
	bobWalletID := jsonInt64(t, bobWallet["id"])

	admin.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": publicWalletID, "kind": "income", "amount": "20.00",
		"category": "工资", "date": "2026-10-03T00:00:00",
	}, http.StatusCreated)
	alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": aliceWalletID, "kind": "expense", "amount": "5.00",
		"category": "餐饮", "date": "2026-10-04T00:00:00",
	}, http.StatusCreated)
	alice.object(t, http.MethodPost, "/api/transactions/", map[string]any{
		"account": bobWalletID, "kind": "income", "amount": "10.00",
		"category": "工资", "date": "2026-10-05T00:00:00",
	}, http.StatusCreated)

	// Ownership never hides a wallet or changes its derived balance.
	expectedBalances := map[int64]string{
		publicWalletID: "120.00",
		aliceWalletID:  "45.00",
		bobWalletID:    "40.00",
	}
	for _, client := range []*testClient{admin, alice} {
		accounts := client.array(t, http.MethodGet, "/api/accounts/", nil, http.StatusOK)
		if len(accounts) != 3 {
			t.Fatalf("家庭成员应看到全部 3 个账户: %s", mustJSON(t, accounts))
		}
		for accountID, want := range expectedBalances {
			account := findTestAccount(t, accounts, accountID)
			if mustString(t, account["balance"]) != want {
				t.Fatalf("账户 %d 余额 = %q，期望 %q: %s",
					accountID, mustString(t, account["balance"]), want, mustJSON(t, account))
			}
		}
	}

	// Another member may edit a member-owned wallet without changing its owner.
	renamed := alice.object(t, http.MethodPatch,
		fmt.Sprintf("/api/accounts/%d/", bobWalletID),
		map[string]any{"name": "小鲍的账户"}, http.StatusOK)
	owner, ownerName := accountOwnerFields(t, renamed)
	if owner == nil || jsonInt64(t, owner) != bobID || ownerName != "bob" {
		t.Fatalf("共享修改不应改变归属: %s", mustJSON(t, renamed))
	}

	// Household totals include every wallet regardless of ownership.
	for _, client := range []*testClient{admin, alice} {
		summary := client.object(t, http.MethodGet, "/api/summary/?month=2026-10",
			nil, http.StatusOK)
		if mustString(t, summary["total_balance"]) != "205.00" {
			t.Fatalf("家庭总余额应包含全部归属的账户: %s", mustJSON(t, summary))
		}
	}
}
