package search

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册检索域路由：混合检索。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewSearchHandler(app.SearchService)
	search := api.Group("/search")
	search.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	search.GET("/hybrid", h.HybridSearch)
}
