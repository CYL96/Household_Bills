package api

import (
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"household-bills/backend/internal/config"
	"household-bills/backend/internal/credentials"
	"household-bills/backend/internal/store"
)

type API struct {
	cfg       config.Config
	store     *store.Store
	logger    *log.Logger
	dummyHash []byte
}

// New constructs the complete Gin HTTP handler. API routes are registered
// explicitly with trailing slashes; everything else is either a JSON API 404
// or the SPA static handler.
func New(cfg config.Config, st *store.Store, logger *log.Logger) http.Handler {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	gin.SetMode(gin.ReleaseMode)

	dummyHash, _ := bcrypt.GenerateFromPassword(
		[]byte("not-a-real-account-password"), credentials.BcryptCost)
	a := &API{
		cfg:       cfg,
		store:     st,
		logger:    logger,
		dummyHash: dummyHash,
	}

	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	if err := router.SetTrustedProxies(nil); err != nil {
		logger.Printf("设置代理信任失败: %v", err)
	}
	router.Use(
		gin.Logger(),
		gin.CustomRecovery(func(c *gin.Context, _ any) {
			if c.Writer.Written() {
				c.Abort()
				return
			}
			abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		}),
	)

	api := router.Group("/api")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}, a.sessionMiddleware())

	// Anonymous endpoints keep the CSRF check. Protected endpoints run
	// requireAuth first so a missing or expired session is always reported as
	// 401 even when the CSRF cookie has expired at the same time.
	anonymous := api.Group("")
	anonymous.Use(a.csrfMiddleware())
	anonymous.GET("/auth/me/", a.handleMe)
	anonymous.POST("/auth/login/", a.handleLogin)

	authenticated := api.Group("")
	authenticated.Use(a.requireAuth, a.csrfMiddleware())
	authenticated.POST("/auth/logout/", a.handleLogout)
	authenticated.GET("/users/", a.requireAdmin, a.handleListUsers)
	authenticated.POST("/users/", a.requireAdmin, a.handleCreateUser)
	authenticated.GET("/accounts/", a.handleListAccounts)
	authenticated.POST("/accounts/", a.handleCreateAccount)
	authenticated.PATCH("/accounts/:id/", a.handlePatchAccount)
	authenticated.GET("/transactions/", a.handleListTransactions)
	authenticated.POST("/transactions/", a.handleCreateTransaction)
	authenticated.PATCH("/transactions/:id/", a.handlePatchTransaction)
	authenticated.DELETE("/transactions/:id/", a.handleDeleteTransaction)
	authenticated.GET("/summary/", a.handleSummary)

	router.NoRoute(a.handleNoRoute)
	router.NoMethod(a.handleNoMethod)
	return router
}
