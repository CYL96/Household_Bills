package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"household-bills/backend/internal/store"
)

const (
	sessionCookieName = "sessionid"
	csrfCookieName    = "csrftoken"
	sessionTTL        = 7 * 24 * time.Hour
	authContextKey    = "household.auth"
)

type authInfo struct {
	User        store.User
	SessionHash []byte
}

func currentAuth(c *gin.Context) (authInfo, bool) {
	value, exists := c.Get(authContextKey)
	if !exists {
		return authInfo{}, false
	}
	info, ok := value.(authInfo)
	return info, ok
}

func (a *API) sessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookieName)
		if err != nil || token == "" {
			c.Next()
			return
		}
		if !validRandomToken(token) {
			a.clearSessionCookie(c)
			c.Next()
			return
		}

		hash := hashToken(token)
		user, found, lookupErr := a.store.SessionUser(c.Request.Context(), hash)
		if lookupErr != nil {
			a.logger.Printf("读取会话失败: %v", lookupErr)
			abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
			return
		}
		if !found {
			a.clearSessionCookie(c)
			c.Next()
			return
		}

		c.Set(authContextKey, authInfo{User: user, SessionHash: hash})
		c.Next()
	}
}

func (a *API) csrfMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if safeMethod(c.Request.Method) {
			c.Next()
			return
		}

		cookieToken, err := c.Cookie(csrfCookieName)
		headerToken := c.GetHeader("X-CSRFToken")
		if headerToken == "" {
			headerToken = c.GetHeader("X-CSRF-Token")
		}
		headerToken = strings.TrimSpace(headerToken)

		if err != nil || cookieToken == "" || headerToken == "" ||
			subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
			abortDetail(c, http.StatusForbidden, "CSRF 校验失败，请刷新页面后重试。")
			return
		}
		if !a.originAllowed(c.Request) {
			abortDetail(c, http.StatusForbidden, "CSRF 校验失败，请刷新页面后重试。")
			return
		}
		c.Next()
	}
}

func (a *API) requireAuth(c *gin.Context) {
	if _, ok := currentAuth(c); !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	c.Next()
}

func (a *API) requireAdmin(c *gin.Context) {
	auth, ok := currentAuth(c)
	if !ok {
		abortDetail(c, http.StatusUnauthorized, "请先登录。")
		return
	}
	if !auth.User.IsAdmin {
		abortDetail(c, http.StatusForbidden, "没有权限执行此操作。")
		return
	}
	c.Next()
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func (a *API) originAllowed(r *http.Request) bool {
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" {
		return false
	}
	origin, ok := normalizeOrigin(rawOrigin)
	if !ok {
		return false
	}

	if len(a.cfg.AllowedOrigins) > 0 {
		for _, allowed := range a.cfg.AllowedOrigins {
			if origin == allowed {
				return true
			}
		}
		return false
	}

	expected := requestScheme(r, a.cfg.CookieSecure) + "://" + strings.ToLower(r.Host)
	return origin == expected
}

func normalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}

func requestScheme(r *http.Request, cookieSecure bool) string {
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		first := strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
		if first == "http" || first == "https" {
			return first
		}
	}
	if r.TLS != nil {
		return "https"
	}
	// Behind a TLS-terminating proxy the backend sees HTTP. When secure
	// cookies are enabled, browsers can only be using HTTPS.
	if cookieSecure {
		return "https"
	}
	return "http"
}

func (a *API) ensureCSRFCookie(c *gin.Context) error {
	if token, err := c.Cookie(csrfCookieName); err == nil && validRandomToken(token) {
		return nil
	}
	token, err := newRandomToken(32)
	if err != nil {
		return err
	}
	a.setCSRFCookie(c, token)
	return nil
}

func (a *API) setSessionCookie(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) setCSRFCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().UTC().Add(sessionTTL),
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: false,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func newRandomToken(byteLength int) (string, error) {
	random := make([]byte, byteLength)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func validRandomToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func generateSession() (token string, tokenHash []byte, expires time.Time, err error) {
	token, err = newRandomToken(32)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	return token, hashToken(token), time.Now().UTC().Add(sessionTTL), nil
}
