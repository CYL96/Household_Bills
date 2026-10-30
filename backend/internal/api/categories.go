package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/store"
)

const categoryConflictMessage = "分类设置已被其他成员更新，请重新打开分类管理后再保存"

// Category icon identifiers are opaque frontend-owned IDs. The backend only
// enforces a safe stable shape instead of duplicating the icon catalog.
var categoryIconPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

type categoryPutRequest struct {
	BaseRevision json.RawMessage `json:"base_revision"`
	Expense      json.RawMessage `json:"expense"`
	Income       json.RawMessage `json:"income"`
}

type categoryPreferencesRequest struct {
	Entries         json.RawMessage `json:"entries"`
	DeletedDefaults json.RawMessage `json:"deleted_defaults"`
}

type categoryEntryRequest struct {
	Name    *string `json:"name"`
	Visible *bool   `json:"visible"`
	// Optional fields keep their raw form so an explicit JSON null can be
	// rejected; only an omitted field means "not set".
	ParentName    json.RawMessage `json:"parent_name"`
	Icon          json.RawMessage `json:"icon"`
	DefaultName   json.RawMessage `json:"default_name"`
	PreviousNames json.RawMessage `json:"previous_names"`
}

func (a *API) handleGetCategories(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	settings, initialized, err := a.store.GetCategorySettings(
		c.Request.Context(), auth.User.HouseholdID)
	if err != nil {
		a.logger.Printf("读取分类设置失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, categorySettingsPayload(settings, initialized))
}

func (a *API) handlePutCategories(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	var request categoryPutRequest
	if !decodeJSON(c, &request) {
		return
	}

	fields := make(map[string][]string)
	baseRevision, valid := parseCategoryRevision(request.BaseRevision)
	if !valid {
		fields["base_revision"] = []string{"请填写有效的分类设置版本。"}
	}

	expense, messages := parseCategoryPreferences(request.Expense, "支出")
	if len(messages) > 0 {
		fields["expense"] = messages
	}
	income, messages := parseCategoryPreferences(request.Income, "收入")
	if len(messages) > 0 {
		fields["income"] = messages
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	settings, err := a.store.ReplaceCategorySettings(
		c.Request.Context(), auth.User.HouseholdID, baseRevision, expense, income)
	if errors.Is(err, store.ErrCategoryConflict) {
		abortDetail(c, http.StatusConflict, categoryConflictMessage)
		return
	}
	if err != nil {
		a.logger.Printf("保存分类设置失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, categorySettingsPayload(settings, true))
}

func categorySettingsPayload(settings store.CategorySettings, initialized bool) gin.H {
	if !initialized {
		return gin.H{
			"initialized": false,
			"revision":    0,
			"expense":     nil,
			"income":      nil,
		}
	}
	expense := settings.Expense
	income := settings.Income
	if expense.Entries == nil {
		expense.Entries = []store.CategoryEntry{}
	}
	if expense.DeletedDefaults == nil {
		expense.DeletedDefaults = []string{}
	}
	if income.Entries == nil {
		income.Entries = []store.CategoryEntry{}
	}
	if income.DeletedDefaults == nil {
		income.DeletedDefaults = []string{}
	}
	return gin.H{
		"initialized": true,
		"revision":    settings.Revision,
		"expense":     expense,
		"income":      income,
	}
}

func parseCategoryRevision(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, false
	}
	return revision, true
}

// parseCategoryPreferences decodes one complete kind. Structural problems are
// reported under the caller's expense/income field instead of the generic
// request-body error so the frontend can attach them to the right section.
func parseCategoryPreferences(
	raw json.RawMessage, label string,
) (store.CategoryPreferences, []string) {
	if len(raw) == 0 {
		return store.CategoryPreferences{}, []string{label + "分类设置不能为空。"}
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return store.CategoryPreferences{}, []string{label + "分类设置不能为空。"}
	}

	var request categoryPreferencesRequest
	if err := json.Unmarshal(trimmed, &request); err != nil {
		return store.CategoryPreferences{}, []string{label + "分类设置格式不正确。"}
	}
	if len(request.Entries) == 0 {
		return store.CategoryPreferences{}, []string{label + "分类设置缺少 entries。"}
	}
	entriesRaw := bytes.TrimSpace(request.Entries)
	if bytes.Equal(entriesRaw, []byte("null")) {
		return store.CategoryPreferences{}, []string{label + "分类设置的 entries 必须是数组。"}
	}
	var entries []categoryEntryRequest
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return store.CategoryPreferences{}, []string{label + "分类条目格式不正确。"}
	}

	if len(request.DeletedDefaults) == 0 {
		return store.CategoryPreferences{}, []string{label + "分类设置缺少 deleted_defaults。"}
	}
	deletedRaw := bytes.TrimSpace(request.DeletedDefaults)
	if bytes.Equal(deletedRaw, []byte("null")) {
		return store.CategoryPreferences{}, []string{label + "分类设置的 deleted_defaults 必须是数组。"}
	}
	var deletedDefaults []string
	if err := json.Unmarshal(deletedRaw, &deletedDefaults); err != nil {
		return store.CategoryPreferences{}, []string{label + "分类设置的 deleted_defaults 格式不正确。"}
	}

	return validateCategoryPreferences(entries, deletedDefaults, label)
}

func validateCategoryPreferences(
	entries []categoryEntryRequest, deletedDefaults []string, label string,
) (store.CategoryPreferences, []string) {
	messages := make([]string, 0)
	normalizedEntries := make([]store.CategoryEntry, len(entries))
	names := make(map[string]struct{}, len(entries))

	for index, entry := range entries {
		prefix := fmt.Sprintf("%s分类第 %d 项", label, index+1)
		normalized := store.CategoryEntry{}
		if entry.Name == nil {
			messages = append(messages, prefix+"缺少名称。")
		} else if name, message := cleanText(*entry.Name, 64, prefix+"名称"); message != "" {
			messages = append(messages, message)
		} else {
			normalized.Name = name
			if _, duplicate := names[name]; duplicate {
				messages = append(messages,
					fmt.Sprintf("%s中分类名称「%s」重复。", label, name))
			} else {
				names[name] = struct{}{}
			}
		}

		if entry.Visible == nil {
			messages = append(messages, prefix+"缺少显示状态。")
		} else {
			normalized.Visible = *entry.Visible
		}

		if parent, present, message := parseOptionalCategoryString(
			entry.ParentName, prefix+"的上级分类名称"); message != "" {
			messages = append(messages, message)
		} else if present {
			parent = strings.TrimSpace(parent)
			if parent != "" {
				if value, message := cleanText(parent, 64, prefix+"的上级分类名称"); message != "" {
					messages = append(messages, message)
				} else {
					normalized.ParentName = value
				}
			}
		}

		if icon, present, message := parseOptionalCategoryString(
			entry.Icon, prefix+"的图标标识"); message != "" {
			messages = append(messages, message)
		} else if present {
			icon = strings.TrimSpace(icon)
			if icon != "" {
				if !categoryIconPattern.MatchString(icon) {
					messages = append(messages, prefix+"的图标标识无效。")
				} else {
					normalized.Icon = icon
				}
			}
		}

		if defaultName, present, message := parseOptionalCategoryString(
			entry.DefaultName, prefix+"的默认分类名称"); message != "" {
			messages = append(messages, message)
		} else if present {
			defaultName = strings.TrimSpace(defaultName)
			if defaultName != "" {
				if value, message := cleanText(defaultName, 64, prefix+"的默认分类名称"); message != "" {
					messages = append(messages, message)
				} else {
					normalized.DefaultName = value
				}
			}
		}

		previousNames, message := parseOptionalCategoryNames(
			entry.PreviousNames, prefix+"的历史名称")
		if message != "" {
			messages = append(messages, message)
		} else {
			for aliasIndex, alias := range previousNames {
				aliasLabel := fmt.Sprintf("%s的第 %d 个历史名称", prefix, aliasIndex+1)
				value, message := cleanText(alias, 64, aliasLabel)
				if message != "" {
					messages = append(messages, message)
					continue
				}
				normalized.PreviousNames = append(normalized.PreviousNames, value)
			}
		}
		normalizedEntries[index] = normalized
	}

	topLevel := make(map[string]struct{}, len(normalizedEntries))
	for _, entry := range normalizedEntries {
		if entry.Name != "" && entry.ParentName == "" {
			topLevel[entry.Name] = struct{}{}
		}
	}
	for _, entry := range normalizedEntries {
		if entry.Name == "" || entry.ParentName == "" {
			continue
		}
		switch {
		case entry.ParentName == entry.Name:
			messages = append(messages,
				fmt.Sprintf("%s分类「%s」不能把自身设为上级分类。", label, entry.Name))
		case !containsKey(names, entry.ParentName):
			messages = append(messages,
				fmt.Sprintf("%s分类「%s」的上级分类「%s」不存在。",
					label, entry.Name, entry.ParentName))
		case !containsKey(topLevel, entry.ParentName):
			messages = append(messages,
				fmt.Sprintf("%s分类「%s」的上级分类必须是顶级分类。",
					label, entry.Name))
		}
	}

	defaultNames := make(map[string]struct{}, len(normalizedEntries))
	for _, entry := range normalizedEntries {
		if entry.DefaultName == "" {
			continue
		}
		if _, duplicate := defaultNames[entry.DefaultName]; duplicate {
			messages = append(messages,
				fmt.Sprintf("%s分类的默认分类「%s」重复。", label, entry.DefaultName))
			continue
		}
		defaultNames[entry.DefaultName] = struct{}{}
	}

	enabledTopLevel := false
	for _, entry := range normalizedEntries {
		if entry.Visible && entry.ParentName == "" {
			enabledTopLevel = true
			break
		}
	}
	if !enabledTopLevel {
		messages = append(messages,
			label+"分类至少需要保留一个启用的顶级分类。")
	}

	normalizedDeleted := make([]string, 0, len(deletedDefaults))
	for index, defaultName := range deletedDefaults {
		value, message := cleanText(defaultName, 64,
			fmt.Sprintf("%s分类第 %d 个已删除默认分类", label, index+1))
		if message != "" {
			messages = append(messages, message)
			continue
		}
		normalizedDeleted = append(normalizedDeleted, value)
	}

	return store.CategoryPreferences{
		Entries:         normalizedEntries,
		DeletedDefaults: normalizedDeleted,
	}, messages
}

func containsKey(values map[string]struct{}, key string) bool {
	_, exists := values[key]
	return exists
}

func parseOptionalCategoryString(
	raw json.RawMessage, label string,
) (value string, present bool, message string) {
	if len(raw) == 0 {
		return "", false, ""
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return "", true, label + "不能为 null。"
	}
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", true, label + "必须是字符串。"
	}
	return value, true, ""
}

func parseOptionalCategoryNames(
	raw json.RawMessage, label string,
) ([]string, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, label + "不能为 null。"
	}
	var names []string
	if err := json.Unmarshal(trimmed, &names); err != nil {
		return nil, label + "必须是字符串数组。"
	}
	return names, ""
}
