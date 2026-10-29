package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/store"
)

type transactionCreateRequest struct {
	Account  *int64          `json:"account"`
	Kind     *string         `json:"kind"`
	Amount   json.RawMessage `json:"amount"`
	Category *string         `json:"category"`
	Date     *string         `json:"date"`
	Note     *string         `json:"note"`
}

type transactionPatchRequest struct {
	Account  *int64          `json:"account"`
	Kind     *string         `json:"kind"`
	Amount   json.RawMessage `json:"amount"`
	Category *string         `json:"category"`
	Date     *string         `json:"date"`
	Note     *string         `json:"note"`
}

func (a *API) handleListTransactions(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	filters := store.TransactionFilters{}
	if monthValue, present := c.GetQuery("month"); present {
		monthStart, monthEnd, valid := monthRange(monthValue)
		if !valid {
			abortFields(c, map[string][]string{"month": {"月份格式应为 YYYY-MM。"}})
			return
		}
		filters.MonthStart = monthStart
		filters.MonthEnd = monthEnd
	}
	if accountValue, present := c.GetQuery("account"); present {
		accountID, valid := positiveID(accountValue)
		if !valid {
			abortFields(c, map[string][]string{"account": {"账户参数无效。"}})
			return
		}
		filters.AccountID = &accountID
	}
	if kindValue, present := c.GetQuery("kind"); present {
		kind, valid := normalizeKind(kindValue)
		if !valid {
			abortFields(c, map[string][]string{"kind": {"类型只能是 income 或 expense。"}})
			return
		}
		filters.Kind = kind
	}
	if queryValue, present := c.GetQuery("q"); present {
		queryValue = strings.TrimSpace(queryValue)
		if strings.ContainsRune(queryValue, '\x00') {
			abortFields(c, map[string][]string{"q": {"搜索内容包含非法字符。"}})
			return
		}
		filters.Query = queryValue
	}

	transactions, err := a.store.ListTransactions(
		c.Request.Context(), auth.User.HouseholdID, filters)
	if err != nil {
		a.logger.Printf("读取交易列表失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	response := make([]gin.H, 0, len(transactions))
	for _, transaction := range transactions {
		response = append(response, transactionResponse(transaction))
	}
	c.JSON(http.StatusOK, response)
}

func (a *API) handleCreateTransaction(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	var request transactionCreateRequest
	if !decodeJSON(c, &request) {
		return
	}
	fields := make(map[string][]string)

	var accountID int64
	if request.Account == nil {
		fields["account"] = []string{"请选择账户。"}
	} else if *request.Account <= 0 {
		fields["account"] = []string{"账户参数无效。"}
	} else {
		accountID = *request.Account
	}

	kind := ""
	if request.Kind == nil {
		fields["kind"] = []string{"请选择收支类型。"}
	} else if parsedKind, valid := normalizeKind(*request.Kind); !valid {
		fields["kind"] = []string{"类型只能是 income 或 expense。"}
	} else {
		kind = parsedKind
	}

	amount := int64(0)
	if len(request.Amount) == 0 {
		fields["amount"] = []string{"请填写金额。"}
	} else if parsedAmount, err := parseMoneyRaw(request.Amount); err != nil {
		fields["amount"] = []string{moneyErrorMessage(err)}
	} else if parsedAmount <= 0 {
		fields["amount"] = []string{"金额必须大于 0。"}
	} else {
		amount = parsedAmount
	}

	category := ""
	if request.Category == nil {
		fields["category"] = []string{"请选择分类。"}
	} else if parsedCategory, message := cleanText(*request.Category, 64, "分类"); message != "" {
		fields["category"] = []string{message}
	} else {
		category = parsedCategory
	}

	date := ""
	if request.Date == nil {
		fields["date"] = []string{"请选择日期。"}
	} else if parsedDate, valid := normalizeDate(*request.Date); !valid {
		fields["date"] = []string{"日期格式应为 YYYY-MM-DD。"}
	} else {
		date = parsedDate
	}

	note := ""
	if request.Note != nil {
		if message := validateNote(*request.Note); message != "" {
			fields["note"] = []string{message}
		} else {
			note = *request.Note
		}
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	transaction, err := a.store.CreateTransaction(
		c.Request.Context(), auth.User.HouseholdID, auth.User.ID,
		store.TransactionInput{
			AccountID:   accountID,
			Kind:        kind,
			AmountCents: amount,
			Category:    category,
			Date:        date,
			Note:        note,
		})
	if errors.Is(err, store.ErrInvalidAccount) {
		abortFields(c, map[string][]string{"account": {"账户不存在或不属于当前家庭。"}})
		return
	}
	if err != nil {
		a.logger.Printf("创建交易失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusCreated, transactionResponse(transaction))
}

func (a *API) handlePatchTransaction(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	transactionID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该交易。")
		return
	}

	var request transactionPatchRequest
	if !decodeJSON(c, &request) {
		return
	}
	fields := make(map[string][]string)
	patch := store.TransactionPatch{}

	if request.Account != nil {
		if *request.Account <= 0 {
			fields["account"] = []string{"账户参数无效。"}
		} else {
			accountID := *request.Account
			patch.AccountID = &accountID
		}
	}
	if request.Kind != nil {
		if kind, valid := normalizeKind(*request.Kind); !valid {
			fields["kind"] = []string{"类型只能是 income 或 expense。"}
		} else {
			patch.Kind = &kind
		}
	}
	if request.Amount != nil {
		if amount, err := parseMoneyRaw(request.Amount); err != nil {
			fields["amount"] = []string{moneyErrorMessage(err)}
		} else if amount <= 0 {
			fields["amount"] = []string{"金额必须大于 0。"}
		} else {
			patch.AmountCents = &amount
		}
	}
	if request.Category != nil {
		if category, message := cleanText(*request.Category, 64, "分类"); message != "" {
			fields["category"] = []string{message}
		} else {
			patch.Category = &category
		}
	}
	if request.Date != nil {
		if date, valid := normalizeDate(*request.Date); !valid {
			fields["date"] = []string{"日期格式应为 YYYY-MM-DD。"}
		} else {
			patch.Date = &date
		}
	}
	if request.Note != nil {
		if message := validateNote(*request.Note); message != "" {
			fields["note"] = []string{message}
		} else {
			patch.Note = request.Note
		}
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	transaction, err := a.store.UpdateTransaction(
		c.Request.Context(), auth.User.HouseholdID, transactionID, patch)
	if errors.Is(err, store.ErrNotFound) {
		abortDetail(c, http.StatusNotFound, "未找到该交易。")
		return
	}
	if errors.Is(err, store.ErrInvalidAccount) {
		abortFields(c, map[string][]string{"account": {"账户不存在或不属于当前家庭。"}})
		return
	}
	if err != nil {
		a.logger.Printf("更新交易失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, transactionResponse(transaction))
}

func (a *API) handleDeleteTransaction(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	transactionID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该交易。")
		return
	}
	err := a.store.DeleteTransaction(
		c.Request.Context(), auth.User.HouseholdID, transactionID)
	if errors.Is(err, store.ErrNotFound) {
		abortDetail(c, http.StatusNotFound, "未找到该交易。")
		return
	}
	if err != nil {
		a.logger.Printf("删除交易失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.Status(http.StatusNoContent)
}
