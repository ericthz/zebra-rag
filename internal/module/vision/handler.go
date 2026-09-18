// Package vision 提供多模态图片理解相关的 HTTP 处理器与路由注册。
package vision

import (
	"io"
	"net/http"

	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/gin-gonic/gin"
)

// VisionHandler 处理图片理解（多模态）相关的 API 请求。
type VisionHandler struct {
	service service.VisionService
}

// NewVisionHandler 创建一个新的 VisionHandler。
func NewVisionHandler(service service.VisionService) *VisionHandler {
	return &VisionHandler{service: service}
}

// Caption 上传图片并返回描述。
func (h *VisionHandler) Caption(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": http.StatusNotImplemented, "message": "多模态未启用", "data": nil})
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "缺少图片文件", "data": nil})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "图片读取失败", "data": nil})
		return
	}
	caption, err := h.service.CaptionImage(c.Request.Context(), header.Filename, data)
	if err != nil {
		log.Errorf("VisionHandler.Caption: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "图片理解失败: " + err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": gin.H{"caption": caption}})
}
