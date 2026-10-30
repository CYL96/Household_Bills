package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"household-bills/backend/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) handleMe(c *gin.Context) {
	if err := a.ensureCSRFCookie(c); err != nil {
		a.logger.Printf("生成 CSRF cookie 失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	auth, ok := currentAuth(c)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"user": nil})
		return
	}
	me, err := a.store.Me(c.Request.Context(), auth.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		a.clearSessionCookie(c)
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	if err != nil {
		a.logger.Printf("读取用户信息失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	c.JSON(http.StatusOK, me)
}

func (a *API) handleLogin(c *gin.Context) {
	var request loginRequest
	if !decodeJSON(c, &request) {
		return
	}
	username := strings.TrimSpace(request.Username)
	if username == "" || request.Password == "" ||
		utf8.RuneCountInString(username) > 150 || strings.ContainsRune(username, '\x00') {
		abortDetail(c, http.StatusBadRequest, "用户名或密码错误。")
		return
	}

	user, passwordHash, found, err := a.store.FindUserByUsername(c.Request.Context(), username)
	if err != nil {
		a.logger.Printf("登录查询失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if !found {
		// Perform a bcrypt comparison even for unknown users to reduce the
		// timing difference between an unknown username and a wrong password.
		_ = bcrypt.CompareHashAndPassword(a.dummyHash, []byte(request.Password))
		abortDetail(c, http.StatusBadRequest, "用户名或密码错误。")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(request.Password)); err != nil {
		abortDetail(c, http.StatusBadRequest, "用户名或密码错误。")
		return
	}

	me, err := a.store.Me(c.Request.Context(), user.ID)
	if errors.Is(err, store.ErrNotFound) {
		// The account was deleted after its password was verified.
		abortDetail(c, http.StatusBadRequest, "用户名或密码错误。")
		return
	}
	if err != nil {
		a.logger.Printf("读取用户信息失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	sessionToken, sessionHash, sessionExpires, err := generateSession()
	if err != nil {
		a.logger.Printf("生成会话失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	err = a.store.CreateSession(
		c.Request.Context(), user.ID, passwordHash, sessionHash, sessionExpires)
	if errors.Is(err, store.ErrPasswordChanged) {
		// The password was rotated after this request verified the old hash;
		// report the same generic failure as any other rejected login.
		abortDetail(c, http.StatusBadRequest, "用户名或密码错误。")
		return
	}
	if err != nil {
		a.logger.Printf("创建登录会话失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}

	a.setSessionCookie(c, sessionToken, sessionExpires)
	c.JSON(http.StatusOK, me)
}

func (a *API) handleLogout(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	if err := a.store.DeleteSession(c.Request.Context(), auth.SessionHash); err != nil {
		a.logger.Printf("退出登录失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	a.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}
