package vision

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册视觉域路由：图片理解/描述。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewVisionHandler(app.VisionService)
	vision := api.Group("/vision")
	vision.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	vision.POST("/caption", h.Caption)
}
