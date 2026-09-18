package chat

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册对话域路由：WS 一次性令牌（API 组）与 WebSocket 端点（引擎级）。
func RegisterRoutes(api *gin.RouterGroup, engine *gin.Engine, app *bootstrap.App) {
	h := NewChatHandler(app.ChatService, app.UserService, app.WSSessionRepository)
	chatGroup := api.Group("/chat")
	chatGroup.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	chatGroup.GET("/websocket-token", h.GetWebsocketToken)

	engine.GET("/chat/:token", h.Handle)
}
