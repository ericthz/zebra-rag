// Package chat 提供对话（WebSocket）相关的 HTTP 处理器与路由注册。
package chat

import (
	"encoding/json"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/token"
	"github.com/ericthz/zebra-rag/pkg/trace"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许所有来源
		},
	}
)

// ChatHandler 负责处理 WebSocket 会话连接。
type ChatHandler struct {
	chatService   service.ChatService
	userService   service.UserService
	wsSessionRepo repository.WSSessionRepository
	// 每连接停止标志
	stopFlags sync.Map // key: session pointer string, value: bool
}

// NewChatHandler 创建一个新的 ChatHandler。
func NewChatHandler(chatService service.ChatService, userService service.UserService, wsSessionRepo repository.WSSessionRepository) *ChatHandler {
	return &ChatHandler{
		chatService:   chatService,
		userService:   userService,
		wsSessionRepo: wsSessionRepo,
	}
}

// GetWebsocketToken 生成短时有效的 WS 会话令牌，用于建立 WebSocket 连接与停止流。
// 相比把长期 JWT 放在 URL 中，短时随机令牌可显著降低凭证泄漏风险。
func (h *ChatHandler) GetWebsocketToken(c *gin.Context) {
	claimsValue, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "未认证", "data": nil})
		return
	}
	claims := claimsValue.(*token.CustomClaims)
	wsToken := "WSS_SESSION_" + token.GenerateRandomString(24)
	if err := h.wsSessionRepo.Create(c.Request.Context(), wsToken, claims.Username); err != nil {
		log.Errorf("创建 WS 会话令牌失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建会话失败", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": gin.H{"wsToken": wsToken}})
}

// Handle 处理一个传入的 WebSocket 连接。
func (h *ChatHandler) Handle(c *gin.Context) {
	wsToken := c.Param("token")
	username, err := h.wsSessionRepo.GetUsername(c.Request.Context(), wsToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "无效或已过期的会话令牌", "data": nil})
		return
	}

	// 获取用户模型
	user, err := h.userService.GetProfile(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "无法获取用户信息", "data": nil})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error("WebSocket 升级失败", err)
		return
	}
	defer conn.Close()

	log.Infof("WebSocket 连接已建立，用户: %s", username)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Warnf("从 WebSocket 读取消息失败: %v", err)
			break
		}
		log.Infof("收到 WebSocket 消息: %s", string(message))

		// 1) JSON 停止指令: {"type":"stop","_internal_cmd_token":"<本连接的 wsToken>"}
		//    停止令牌与连接绑定（即 URL 中的 wsToken），不再使用全局令牌，避免多用户互相干扰
		var ctrl map[string]interface{}
		if len(message) > 0 && message[0] == '{' {
			if err := json.Unmarshal(message, &ctrl); err == nil {
				if t, ok := ctrl["type"].(string); ok && t == "stop" {
					if tok, ok := ctrl["_internal_cmd_token"].(string); ok && tok == wsToken {
						key := sessionKey(conn)
						h.stopFlags.Store(key, true)
						// 回发停止确认
						resp := map[string]interface{}{
							"type":      "stop",
							"message":   "响应已停止",
							"timestamp": time.Now().UnixMilli(),
							"date":      time.Now().Format("2006-01-02T15:04:05"),
						}
						b, _ := json.Marshal(resp)
						_ = conn.WriteMessage(websocket.TextMessage, b)
						continue
					}
				}
			}
		}

		// 解析 JSON 消息: {"conversationId":"...","query":"...","requestId":"..."}（兼容纯文本）
		query := string(message)
		conversationID := ""
		requestID := ""
		if len(message) > 0 && message[0] == '{' {
			var req struct {
				ConversationID string `json:"conversationId"`
				Query          string `json:"query"`
				RequestID      string `json:"requestId"`
			}
			if err := json.Unmarshal(message, &req); err == nil && req.Query != "" {
				query = req.Query
				conversationID = req.ConversationID
				requestID = req.RequestID
			}
		}

		// 将客户端 Request-ID 透传到上下文，贯穿检索与生成日志（链路关联）
		reqCtx := c.Request.Context()
		if requestID != "" {
			reqCtx = trace.WithID(reqCtx, requestID)
		}

		// 调用 ChatService 处理完整的 RAG 和流式逻辑
		shouldStop := func() bool {
			key := sessionKey(conn)
			v, ok := h.stopFlags.Load(key)
			return ok && v.(bool)
		}
		// 清除旧标志
		h.stopFlags.Delete(sessionKey(conn))
		err = h.chatService.StreamResponse(reqCtx, query, user, conversationID, conn, shouldStop)
		if err != nil {
			log.Errorf("处理流式响应失败: %v", err)
			// 统一 JSON 错误
			errResp := map[string]string{"error": "AI服务暂时不可用，请稍后重试"}
			b, _ := json.Marshal(errResp)
			_ = conn.WriteMessage(websocket.TextMessage, b)
			// 错误时也发送 completion 通知
			resp := map[string]interface{}{
				"type":      "completion",
				"status":    "finished",
				"message":   "响应已完成",
				"timestamp": time.Now().UnixMilli(),
				"date":      time.Now().Format("2006-01-02T15:04:05"),
			}
			cb, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, cb)
			break
		}
	}
}

func sessionKey(conn *websocket.Conn) string {
	return fmt.Sprintf("%p", conn)
}
