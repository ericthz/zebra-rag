package document

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册文档域路由：列表/删除/下载/预览/长文档摘要。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewDocumentHandler(app.DocumentService, app.UserService, app.SummarizeService)
	documents := api.Group("/documents")
	documents.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	{
		documents.GET("/accessible", h.ListAccessibleFiles)
		documents.GET("/uploads", h.ListUploadedFiles)
		documents.DELETE("/:fileMd5", h.DeleteDocument)
		documents.GET("/download", h.GenerateDownloadURL)
		documents.GET("/preview", h.PreviewFile)
		documents.POST("/:fileMd5/summarize", h.SummarizeDocument)
	}
}
