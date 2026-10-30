package api

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/credentials"
	"household-bills/backend/internal/store"
)

const (
	maxAvatarBytes  = 2 << 20
	maxAvatarSide   = 4096
	maxAvatarPixels = 16_000_000
)

type userPatchRequest struct {
	Username        *string `json:"username"`
	Nickname        *string `json:"nickname"`
	Password        *string `json:"password"`
	CurrentPassword *string `json:"current_password"`
}

// handleUpdateUser lets members edit themselves and lets the household
// administrator edit any member. The store performs the household-scoped
// authorization under a row lock.
func (a *API) handleUpdateUser(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	targetID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	}

	var request userPatchRequest
	if !decodeJSON(c, &request) {
		return
	}

	fields := make(map[string][]string)
	var update store.UserProfileUpdate
	if request.Username != nil {
		username, message := credentials.NormalizeUsername(*request.Username)
		if message != "" {
			fields["username"] = []string{message}
		} else {
			update.Username = &username
		}
	}
	if request.Nickname != nil {
		nickname := strings.TrimSpace(*request.Nickname)
		if nickname != "" {
			var message string
			nickname, message = cleanText(nickname, 50, "昵称")
			if message != "" {
				fields["nickname"] = []string{message}
			}
		}
		update.Nickname = &nickname
	}
	// An explicitly empty password is treated as omitted: only a real new
	// password triggers validation, password rotation and session cleanup.
	passwordChange := request.Password != nil && *request.Password != ""
	if passwordChange {
		if message := credentials.PasswordValidationError(*request.Password); message != "" {
			fields["password"] = []string{message}
		}
		currentPassword := ""
		if request.CurrentPassword != nil {
			currentPassword = *request.CurrentPassword
		}
		if auth.User.ID == targetID && currentPassword == "" {
			fields["current_password"] = []string{"请填写当前密码。"}
		}
		update.CurrentPassword = currentPassword
	}
	if len(fields) > 0 {
		abortFields(c, fields)
		return
	}

	if passwordChange {
		passwordHash, err := credentials.HashPassword(*request.Password)
		if err != nil {
			a.logger.Printf("生成密码哈希失败: %v", err)
			abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
			return
		}
		update.PasswordHash = &passwordHash
	}

	user, err := a.store.UpdateUser(c.Request.Context(), auth.User, targetID, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	case errors.Is(err, store.ErrForbidden):
		abortDetail(c, http.StatusForbidden, "没有权限执行此操作。")
		return
	case errors.Is(err, store.ErrUsernameTaken):
		abortFields(c, map[string][]string{"username": {"该用户名已被使用。"}})
		return
	case errors.Is(err, store.ErrAdminUsernameFixed):
		abortFields(c, map[string][]string{
			"username": {"管理员登录名固定为 admin，不能修改。"},
		})
		return
	case errors.Is(err, store.ErrInvalidCurrentPassword):
		abortFields(c, map[string][]string{"current_password": {"当前密码错误。"}})
		return
	case err != nil:
		a.logger.Printf("更新用户失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	if update.PasswordHash != nil && auth.User.ID == targetID {
		a.clearSessionCookie(c)
	}
	c.JSON(http.StatusOK, user)
}

// handleUpdateUserAvatar accepts a raw image body, verifies it is a complete
// PNG or JPEG within the dimension limits, and stores the original bytes.
func (a *API) handleUpdateUserAvatar(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	targetID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	}

	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		abortFields(c, map[string][]string{"avatar": {"头像图片不能超过 2 MiB。"}})
		return
	}
	if len(data) == 0 {
		abortFields(c, map[string][]string{"avatar": {"头像图片不能为空。"}})
		return
	}

	config, configFormat, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		abortFields(c, map[string][]string{
			"avatar": {"头像必须是有效的 PNG 或 JPEG 图片。"},
		})
		return
	}
	contentType, supported := avatarContentType(configFormat)
	if !supported || config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxAvatarSide || config.Height > maxAvatarSide ||
		int64(config.Width)*int64(config.Height) > maxAvatarPixels {
		abortFields(c, map[string][]string{
			"avatar": {"头像必须是有效的 PNG 或 JPEG 图片，单边不超过 4096 像素且总像素不超过 1600 万。"},
		})
		return
	}

	// Decode the entire image so truncated or corrupt files cannot be stored.
	_, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != configFormat {
		abortFields(c, map[string][]string{
			"avatar": {"头像必须是有效的 PNG 或 JPEG 图片。"},
		})
		return
	}

	user, err := a.store.SetUserAvatar(
		c.Request.Context(), auth.User, targetID, data, contentType)
	switch {
	case errors.Is(err, store.ErrNotFound):
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	case errors.Is(err, store.ErrForbidden):
		abortDetail(c, http.StatusForbidden, "没有权限执行此操作。")
		return
	case err != nil:
		a.logger.Printf("更新头像失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, user)
}

// handleGetUserAvatar serves the stored original image to any member of the
// same household. The fixed MIME type and nosniff prevent content sniffing.
func (a *API) handleGetUserAvatar(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	targetID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到头像。")
		return
	}

	data, contentType, err := a.store.UserAvatar(
		c.Request.Context(), auth.User.HouseholdID, targetID)
	if errors.Is(err, store.ErrNotFound) {
		abortDetail(c, http.StatusNotFound, "未找到头像。")
		return
	}
	if err != nil {
		a.logger.Printf("读取头像失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, data)
}

// handleDeleteUser soft-deletes an ordinary member of the administrator's
// household and invalidates their sessions.
func (a *API) handleDeleteUser(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	targetID, ok := positiveID(c.Param("id"))
	if !ok {
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	}

	err := a.store.DeleteUser(
		c.Request.Context(), auth.User, targetID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		abortDetail(c, http.StatusNotFound, "未找到该用户。")
		return
	case errors.Is(err, store.ErrAdminNotDeletable):
		abortDetail(c, http.StatusBadRequest, "不能删除管理员账号。")
		return
	case err != nil:
		a.logger.Printf("删除用户失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.Status(http.StatusNoContent)
}

func avatarContentType(format string) (string, bool) {
	switch format {
	case "png":
		return "image/png", true
	case "jpeg":
		return "image/jpeg", true
	default:
		return "", false
	}
}
