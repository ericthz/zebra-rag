package upload

import (
	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册上传域路由：秒传/分片/合并/状态/类型/快传。
func RegisterRoutes(api *gin.RouterGroup, app *bootstrap.App) {
	h := NewUploadHandler(app.UploadService)
	upload := api.Group("/upload")
	upload.Use(middleware.AuthMiddleware(app.JWTManager, app.UserService))
	{
		upload.POST("/check", h.CheckFile)
		upload.POST("/chunk", h.UploadChunk)
		upload.POST("/merge", h.MergeChunks)
		upload.GET("/status", h.GetUploadStatus)
		upload.GET("/supported-types", h.GetSupportedFileTypes)
		upload.POST("/fast-upload", h.FastUpload)
	}
}
