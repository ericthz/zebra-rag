package conversation

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册会话域路由：会话列表/归档/消息/删除。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewConversationHandler(app.ConversationService)
	conversation := api.Group("/users/conversation")
	conversation.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	{
		conversation.GET("", h.ListConversations)
		conversation.GET("/archived", h.ListArchivedConversations)
		conversation.POST("/:id/archive", h.ArchiveConversation)
		conversation.GET("/:id/messages", h.GetConversationMessages)
		conversation.DELETE("/:id", h.DeleteConversation)
	}
}
