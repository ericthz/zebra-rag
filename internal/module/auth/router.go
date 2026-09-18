package auth

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册认证相关路由：令牌刷新。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := &AuthHandler{userService: app.UserService}
	api.POST("/auth/refreshToken", h.RefreshToken)
}
