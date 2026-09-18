// Package document 提供文档管理相关的 HTTP 处理器与路由注册。
package document

import (
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/token"
	"github.com/gin-gonic/gin"
	"net/http"
)

// DocumentHandler 负责处理所有与文档管理相关的 API 请求。
type DocumentHandler struct {
	docService       service.DocumentService
	userService      service.UserService
	summarizeService service.SummarizeService // 可为 nil（未启用总结）
}

// NewDocumentHandler 创建一个新的 DocumentHandler 实例。
func NewDocumentHandler(docService service.DocumentService, userService service.UserService, summarizeService service.SummarizeService) *DocumentHandler {
	return &DocumentHandler{
		docService:       docService,
		userService:      userService,
		summarizeService: summarizeService,
	}
}

// SummarizeDocument 处理长文档 Map-Reduce 总结请求（B8.3）。
func (h *DocumentHandler) SummarizeDocument(c *gin.Context) {
	if h.summarizeService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": http.StatusNotImplemented, "message": "文档总结未启用", "data": nil})
		return
	}
	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}
	fileMD5 := c.Param("fileMd5")
	summary, err := h.summarizeService.SummarizeDocument(c.Request.Context(), fileMD5, user)
	if err != nil {
		log.Errorf("SummarizeDocument: %s failed: %v", fileMD5, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "总结失败: " + err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": gin.H{"fileMd5": fileMD5, "summary": summary}})
}

// ListAccessibleFiles 处理获取可访问文件列表的请求。
func (h *DocumentHandler) ListAccessibleFiles(c *gin.Context) {
	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	files, err := h.docService.ListAccessibleFiles(user)
	if err != nil {
		log.Error("ListAccessibleFiles: failed", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "获取可访问文件列表成功",
		"data":    files,
	})
}

// ListUploadedFiles 处理获取用户已上传文件列表的请求。
func (h *DocumentHandler) ListUploadedFiles(c *gin.Context) {
	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	files, err := h.docService.ListUploadedFiles(user.ID)
	if err != nil {
		log.Error("ListUploadedFiles: failed", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "获取用户上传文件列表成功",
		"data":    files,
	})
}

// DeleteDocument 处理删除文档的请求。
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	fileMD5 := c.Param("fileMd5")
	if fileMD5 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件 MD5"})
		return
	}

	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	err = h.docService.DeleteDocument(fileMD5, user)
	if err != nil {
		log.Warnf("DeleteDocument: failed for user %s, md5 %s, err: %v", user.Username, fileMD5, err)
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "文档删除成功",
	})
}

// GenerateDownloadURL 处理生成文件下载链接的请求。
func (h *DocumentHandler) GenerateDownloadURL(c *gin.Context) {
	fileName := c.Query("fileName") // Changed from Param to Query
	fileMD5 := c.Query("fileMd5")
	if fileName == "" && fileMD5 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件名"})
		return
	}

	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	downloadInfo, err := h.docService.GenerateDownloadURL(fileName, fileMD5, user)
	if err != nil {
		log.Warnf("GenerateDownloadURL: failed for user %s, file %s, err: %v", user.Username, fileName, err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "文件下载链接生成成功",
		"data":    downloadInfo,
	})
}

// PreviewFile 处理获取文件预览内容的请求。
func (h *DocumentHandler) PreviewFile(c *gin.Context) {
	fileName := c.Query("fileName")
	fileMD5 := c.Query("fileMd5")
	if fileName == "" && fileMD5 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件名"})
		return
	}

	user, err := h.getUserFromContext(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	previewInfo, err := h.docService.GetFilePreviewContent(fileName, fileMD5, user)
	if err != nil {
		log.Warnf("PreviewFile: failed for user %s, file %s, err: %v", user.Username, fileName, err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "文件预览内容获取成功",
		"data":    previewInfo,
	})
}

// getUserFromContext 是一个辅助函数，用于从 Gin 上下文中获取完整的用户模型。
func (h *DocumentHandler) getUserFromContext(c *gin.Context) (*model.User, error) {
	claimsValue, _ := c.Get("claims")
	claims := claimsValue.(*token.CustomClaims)
	return h.userService.GetProfile(claims.Username)
}
