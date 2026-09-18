// Package conversation 提供会话管理相关的 HTTP 处理器与路由注册。
package conversation

import (
	"net/http"
	"time"

	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/token"
	"github.com/gin-gonic/gin"
)

// ConversationHandler 处理与对话相关的 API 请求。
type ConversationHandler struct {
	service service.ConversationService
}

// NewConversationHandler 创建一个新的 ConversationHandler。
func NewConversationHandler(service service.ConversationService) *ConversationHandler {
	return &ConversationHandler{service: service}
}

// ListConversations 获取用户会话列表。
func (h *ConversationHandler) ListConversations(c *gin.Context) {
	claims := c.MustGet("claims").(*token.CustomClaims)
	list, err := h.service.ListConversations(c.Request.Context(), claims.UserID)
	if err != nil {
		log.Errorf("[Conversation] 获取会话列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "获取会话列表失败", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": list})
}

// GetConversationMessages 获取指定会话的消息（支持 start_date/end_date 过滤）。
func (h *ConversationHandler) GetConversationMessages(c *gin.Context) {
	claims := c.MustGet("claims").(*token.CustomClaims)
	conversationID := c.Param("id")
	messages, err := h.service.GetConversationMessages(c.Request.Context(), claims.UserID, conversationID)
	if err != nil {
		log.Errorf("[Conversation] 获取会话消息失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "会话不存在或无权访问", "data": nil})
		return
	}

	// 时间范围筛选
	startStr := c.Query("start_date")
	endStr := c.Query("end_date")
	if startStr != "" || endStr != "" {
		var start, end time.Time
		if t, e := time.Parse("2006-01-02", startStr); e == nil {
			start = t
		}
		if t, e := time.Parse("2006-01-02", endStr); e == nil {
			end = t.Add(24 * time.Hour)
		}
		filtered := messages[:0]
		for _, msg := range messages {
			ts := msg.Timestamp
			if (!start.IsZero() && ts.Before(start)) || (!end.IsZero() && !ts.Before(end)) {
				continue
			}
			filtered = append(filtered, msg)
		}
		messages = filtered
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": messages})
}

// ListArchivedConversations 获取用户已归档会话列表。
func (h *ConversationHandler) ListArchivedConversations(c *gin.Context) {
	claims := c.MustGet("claims").(*token.CustomClaims)
	list, err := h.service.ListArchivedConversations(c.Request.Context(), claims.UserID)
	if err != nil {
		log.Errorf("[Conversation] 获取归档会话列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "获取归档会话列表失败", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": list})
}

// ArchiveConversation 归档指定会话。
func (h *ConversationHandler) ArchiveConversation(c *gin.Context) {
	claims := c.MustGet("claims").(*token.CustomClaims)
	conversationID := c.Param("id")
	if err := h.service.ArchiveConversation(c.Request.Context(), claims.UserID, conversationID); err != nil {
		log.Errorf("[Conversation] 归档会话失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "归档失败：会话不存在或无权访问", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": nil})
}

// DeleteConversation 删除指定会话（仅已归档会话可删除）。
func (h *ConversationHandler) DeleteConversation(c *gin.Context) {
	claims := c.MustGet("claims").(*token.CustomClaims)
	conversationID := c.Param("id")
	if err := h.service.DeleteConversation(c.Request.Context(), claims.UserID, conversationID); err != nil {
		log.Errorf("[Conversation] 删除会话失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "删除失败：会话不存在或无权访问", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": nil})
}
