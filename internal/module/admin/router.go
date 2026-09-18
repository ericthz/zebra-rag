package admin

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册管理域路由：用户/会话/组织标签管理（认证 + 管理员授权）。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewAdminHandler(app.AdminService, app.UserService)
	admin := api.Group("/admin")
	admin.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService), middleware.AdminAuthMiddleware())
	{
		admin.GET("/users/list", h.ListUsers)
		admin.POST("/users", h.CreateUser)
		admin.DELETE("/users/:userId", h.DeleteUser)
		admin.PUT("/users/:userId/org-tags", h.AssignOrgTagsToUser)
		admin.GET("/conversation", h.GetAllConversations)
		admin.DELETE("/conversation/:userId/:conversationId", h.DeleteConversation)
	}

	orgTags := admin.Group("/org-tags")
	{
		orgTags.POST("", h.CreateOrganizationTag)
		orgTags.GET("", h.ListOrganizationTags)
		orgTags.GET("/tree", h.GetOrganizationTagTree)
		orgTags.PUT("/:id", h.UpdateOrganizationTag)
		orgTags.DELETE("/:id", h.DeleteOrganizationTag)
	}
}
