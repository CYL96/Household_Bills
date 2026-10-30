package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/credentials"
	"household-bills/backend/internal/store"
)

type createUserRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

// handleListUsers returns the administrator's household roster, including the
// administrator. The household always comes from the authenticated session.
func (a *API) handleListUsers(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	users, err := a.store.ListUsers(c.Request.Context(), auth.User.HouseholdID)
	if err != nil {
		a.logger.Printf("读取用户列表失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, users)
}

// handleCreateUser creates a non-admin account in the administrator's
// household. Unknown JSON fields such as is_admin or household_id are ignored
// by the bind target and can never grant privileges or choose a household.
func (a *API) handleCreateUser(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}

	var request createUserRequest
	if !decodeJSON(c, &request) {
		return
	}

	fields := make(map[string][]string)
	username, usernameMessage := credentials.NormalizeUsername(request.Username)
	if usernameMessage != "" {
		fields["username"] = []string{usernameMessage}
	}
	nickname := strings.TrimSpace(request.Nickname)
	if nickname != "" {
		var nicknameMessage string
		nickname, nicknameMessage = cleanText(nickname, 50, "昵称")
		if nicknameMessage != "" {
			fields["nickname"] = []string{nicknameMessage}
		}
	}
	if message := credentials.PasswordValidationError(request.Password); message != "" {
		fields["password"] = []string{message}
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	passwordHash, err := credentials.HashPassword(request.Password)
	if err != nil {
		a.logger.Printf("生成密码哈希失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	user, err := a.store.CreateUser(
		c.Request.Context(), auth.User.HouseholdID, username, nickname, passwordHash)
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		abortFields(c, map[string][]string{"username": {"该用户名已被使用。"}})
		return
	case err != nil:
		a.logger.Printf("创建用户失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	c.JSON(http.StatusCreated, user)
}
