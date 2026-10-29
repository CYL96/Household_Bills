package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) handleSummary(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	month, present := c.GetQuery("month")
	if !present {
		abortFields(c, map[string][]string{"month": {"请选择月份。"}})
		return
	}
	monthStart, monthEnd, valid := monthRange(month)
	if !valid {
		abortFields(c, map[string][]string{"month": {"月份格式应为 YYYY-MM。"}})
		return
	}

	summary, err := a.store.GetSummary(
		c.Request.Context(), auth.User.HouseholdID, month, monthStart, monthEnd)
	if err != nil {
		a.logger.Printf("读取汇总失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, summaryResponse(summary))
}
