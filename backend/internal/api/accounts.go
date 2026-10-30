package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/money"
	"household-bills/backend/internal/store"
)

const accountOwnerErrorMessage = "账户归属成员不存在。"

type accountCreateRequest struct {
	Name    string          `json:"name"`
	Balance json.RawMessage `json:"balance"`
	Owner   json.RawMessage `json:"owner"`
}

type accountPatchRequest struct {
	Name    *string         `json:"name"`
	Balance json.RawMessage `json:"balance"`
	Owner   json.RawMessage `json:"owner"`
}

func (a *API) handleListAccounts(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	accounts, err := a.store.ListAccounts(c.Request.Context(), auth.User.HouseholdID)
	if err != nil {
		a.logger.Printf("读取账户列表失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	response := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		response = append(response, accountResponse(account))
	}
	c.JSON(http.StatusOK, response)
}

func (a *API) handleCreateAccount(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	var request accountCreateRequest
	if !decodeJSON(c, &request) {
		return
	}
	fields := make(map[string][]string)
	name, message := cleanText(request.Name, 100, "账户名称")
	if message != "" {
		fields["name"] = []string{message}
	}

	balance := int64(0)
	if request.Balance != nil {
		parsed, err := parseMoneyRaw(request.Balance)
		if err != nil {
			fields["balance"] = []string{moneyErrorMessage(err)}
		} else {
			balance = parsed
		}
	}

	ownerID, _, ownerOK := parseOwnerRaw(request.Owner)
	if !ownerOK {
		fields["owner"] = []string{accountOwnerErrorMessage}
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	account, err := a.store.CreateAccount(
		c.Request.Context(), auth.User.HouseholdID, name, balance, ownerID)
	if errors.Is(err, store.ErrInvalidOwner) {
		abortFields(c, map[string][]string{"owner": {accountOwnerErrorMessage}})
		return
	}
	if err != nil {
		a.logger.Printf("创建账户失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusCreated, accountResponse(account))
}

func (a *API) handlePatchAccount(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	accountID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该账户。")
		return
	}

	var request accountPatchRequest
	if !decodeJSON(c, &request) {
		return
	}
	fields := make(map[string][]string)
	var update store.AccountUpdate

	if request.Name != nil {
		name, message := cleanText(*request.Name, 100, "账户名称")
		if message != "" {
			fields["name"] = []string{message}
		} else {
			update.Name = &name
		}
	}
	if request.Balance != nil {
		balance, err := parseMoneyRaw(request.Balance)
		if err != nil {
			fields["balance"] = []string{moneyErrorMessage(err)}
		} else {
			update.Balance = &balance
		}
	}

	ownerID, ownerSet, ownerOK := parseOwnerRaw(request.Owner)
	if !ownerOK {
		fields["owner"] = []string{accountOwnerErrorMessage}
	} else if ownerSet {
		update.OwnerID = ownerID
		update.OwnerSet = true
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	account, err := a.store.UpdateAccount(
		c.Request.Context(), auth.User.HouseholdID, accountID, update)
	if errors.Is(err, store.ErrInvalidOwner) {
		abortFields(c, map[string][]string{"owner": {accountOwnerErrorMessage}})
		return
	}
	if errors.Is(err, store.ErrBalanceOutOfRange) {
		abortFields(c, map[string][]string{"balance": {moneyErrorMessage(money.ErrRange)}})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		abortDetail(c, http.StatusNotFound, "未找到该账户。")
		return
	}
	if err != nil {
		a.logger.Printf("更新账户失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, accountResponse(account))
}
