package user

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册用户域路由：注册/登录（公开+限流）与个人账号管理（需认证）。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewUserHandler(app.UserService)
	rl := middleware.RateLimitMiddleware(app.RDB, app.RateLimit, app.RateLimitWindow)

	users := api.Group("/users")
	users.POST("/register", rl, h.Register)
	users.POST("/login", rl, h.Login)

	authed := users.Group("/")
	authed.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	{
		authed.GET("/me", h.GetProfile)
		authed.PUT("/me", h.UpdateProfile)
		authed.PUT("/password", h.ChangePassword)
		authed.GET("/stats", h.GetUserStats)
		authed.POST("/logout", h.Logout)
		authed.PUT("/primary-org", h.SetPrimaryOrg)
		authed.GET("/org-tags", h.GetUserOrgTags)
	}
}
