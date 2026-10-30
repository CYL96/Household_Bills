package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/money"
	"household-bills/backend/internal/store"
)

const maxRequestBodyBytes = 1 << 20

var (
	errMissingMoney = errors.New("未提供金额")
	monthPattern    = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	datePattern     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$`)
)

func abortDetail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"detail": message})
}

func abortFields(c *gin.Context, fields map[string][]string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, fields)
}

func decodeJSON(c *gin.Context, destination any) bool {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		abortDetail(c, http.StatusBadRequest, "请求体读取失败或超过大小限制。")
		return false
	}
	if len(bytes.TrimSpace(data)) == 0 {
		abortDetail(c, http.StatusBadRequest, "请求体不能为空。")
		return false
	}
	if err := json.Unmarshal(data, destination); err != nil {
		abortDetail(c, http.StatusBadRequest, "请求体格式错误。")
		return false
	}
	return true
}

// parseMoneyRaw converts a JSON string or JSON number to exact cents without
// ever decoding the value into a float.
func parseMoneyRaw(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, errMissingMoney
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, errMissingMoney
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return 0, money.ErrFormat
		}
		return money.Parse(text)
	}
	return money.Parse(string(trimmed))
}

func moneyErrorMessage(err error) string {
	switch {
	case errors.Is(err, errMissingMoney):
		return "请填写金额。"
	case errors.Is(err, money.ErrRange):
		return "金额超出允许范围。"
	default:
		return "金额格式不正确，应为最多两位小数的数字。"
	}
}

func monthRange(value string) (start, end string, ok bool) {
	if !monthPattern.MatchString(value) {
		return "", "", false
	}
	year, err := strconv.Atoi(value[0:4])
	if err != nil || year < 1 {
		return "", "", false
	}
	month, err := strconv.Atoi(value[5:7])
	if err != nil {
		return "", "", false
	}
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	return first.Format("2006-01-02"), next.Format("2006-01-02"), true
}

func normalizeDate(value string) (string, bool) {
	if !datePattern.MatchString(value) {
		return "", false
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", value)
	if err != nil || parsed.Year() < 1 {
		return "", false
	}
	return parsed.Format("2006-01-02T15:04:05"), true
}

func normalizeKind(value string) (string, bool) {
	value = strings.TrimSpace(value)
	switch value {
	case "income", "expense", "transfer":
		return value, true
	default:
		return "", false
	}
}

func cleanText(raw string, maxRunes int, label string) (string, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", label + "不能为空。"
	}
	if strings.ContainsRune(value, '\x00') {
		return "", label + "包含非法字符。"
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return "", label + "不能超过 " + strconv.Itoa(maxRunes) + " 个字符。"
	}
	return value, ""
}

func validateNote(note string) string {
	if strings.ContainsRune(note, '\x00') {
		return "备注包含非法字符。"
	}
	return ""
}

func positiveID(raw string) (int64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func accountResponse(account store.Account) gin.H {
	return gin.H{
		"id":              account.ID,
		"name":            account.Name,
		"opening_balance": money.Format(account.OpeningBalanceCents),
		"balance":         money.Format(account.BalanceCents),
	}
}

func transactionResponse(transaction store.Transaction) gin.H {
	return gin.H{
		"id":                    transaction.ID,
		"account":               transaction.AccountID,
		"account_name":          transaction.AccountName,
		"transfer_account":      transaction.TransferAccountID,
		"transfer_account_name": transaction.TransferAccountName,
		"kind":                  transaction.Kind,
		"amount":                money.Format(transaction.AmountCents),
		"category":              transaction.Category,
		"date":                  transaction.Date,
		"note":                  transaction.Note,
		"created_by":            transaction.CreatedBy,
		"created_by_name":       transaction.CreatedByName,
	}
}

func summaryResponse(summary store.Summary) gin.H {
	categories := make([]gin.H, 0, len(summary.Categories))
	for _, item := range summary.Categories {
		categories = append(categories, gin.H{
			"category": item.Category,
			"amount":   money.Format(item.AmountCents),
		})
	}
	daily := make([]gin.H, 0, len(summary.Daily))
	for _, item := range summary.Daily {
		daily = append(daily, gin.H{
			"date":    item.Date,
			"income":  money.Format(item.IncomeCents),
			"expense": money.Format(item.ExpenseCents),
		})
	}
	return gin.H{
		"month":         summary.Month,
		"income":        money.Format(summary.IncomeCents),
		"expense":       money.Format(summary.ExpenseCents),
		"net":           money.Format(summary.NetCents),
		"total_balance": money.Format(summary.TotalBalanceCents),
		"categories":    categories,
		"daily":         daily,
	}
}
