// Package service 包含了应用的业务逻辑层。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/llm"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/metrics"
	"github.com/ericthz/zebra-rag/pkg/trace"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ChatService 定义了会话操作的接口。
type ChatService interface {
	StreamResponse(ctx context.Context, query string, user *model.User, conversationID string, ws *websocket.Conn, shouldStop func() bool) error
}

type chatService struct {
	searchService    SearchService
	llmClient        llm.Client
	conversationRepo repository.ConversationRepository
	rewriter         QueryRewriter    // 用于 Agentic 自评重检索；可为 nil
	intentClassifier IntentClassifier // 用于意图路由；可为 nil
}

// NewChatService 创建一个新的 ChatService 实例。
func NewChatService(searchService SearchService, llmClient llm.Client, conversationRepo repository.ConversationRepository, rewriter QueryRewriter, intentClassifier IntentClassifier) ChatService {
	return &chatService{
		searchService:    searchService,
		llmClient:        llmClient,
		conversationRepo: conversationRepo,
		rewriter:         rewriter,
		intentClassifier: intentClassifier,
	}
}

// StreamResponse 协调 RAG 流程并流式传输 LLM 响应。
func (s *chatService) StreamResponse(ctx context.Context, query string, user *model.User, conversationID string, ws *websocket.Conn, shouldStop func() bool) error {
	metrics.IncChat()
	log.Infof("[Chat] 收到对话请求 requestId=%s conversationId=%s", trace.FromContext(ctx), conversationID)
	// 0. 意图路由：闲聊直接作答（跳过检索）
	var results []model.SearchResponseDTO
	var err error
	if config.Conf.Search.IntentRouting.Enabled && s.intentClassifier != nil &&
		s.intentClassifier.Classify(ctx, query) == IntentChitchat {
		log.Infof("[Intent] 判定为闲聊，跳过检索: %s", query)
	} else {
		// 1. 检索上下文（Agentic：检索后自评，不充分则改写重检索）
		results, err = s.retrieveWithSelfCheck(ctx, query, user)
		if err != nil {
			metrics.IncChatError()
			return fmt.Errorf("failed to retrieve context: %w", err)
		}
	}

	// 2. 构建上下文与 system 消息、历史
	contextText := s.buildContextText(results)
	systemMsg := s.buildSystemMessage(contextText)
	// 会话上下文：无效/已删除的 conversationID 也按新会话处理（避免写入孤儿 history）
	convID := conversationID
	if convID != "" {
		exists, cErr := s.conversationRepo.ConversationExists(ctx, convID)
		if cErr != nil || !exists {
			log.Warnf("[Chat] 会话 %s 无效或已删除，创建新会话", convID)
			convID = ""
		}
	}
	if convID == "" {
		title := query
		if r := []rune(title); len(r) > 30 {
			title = string(r[:30])
		}
		if convID, err = s.conversationRepo.CreateConversation(ctx, user.ID, title); err != nil {
			log.Errorf("Failed to create conversation: %v", err)
		}
	}
	history, err := s.loadHistory(ctx, convID)
	if err != nil {
		log.Errorf("Failed to load conversation history: %v", err)
		history = []model.ChatMessage{}
	}
	// 上下文预算：限制送入 LLM 的历史条数与总长度，防止长会话超出模型上下文窗口
	history = boundHistory(history)
	messages := s.composeMessages(systemMsg, history, query)

	// 拦截 websocket writer 以捕获完整答案，并包装为 JSON 分块
	answerBuilder := &strings.Builder{}
	interceptor := &wsWriterInterceptor{conn: ws, writer: answerBuilder, shouldStop: shouldStop}

	// 3. 调用 LLM 客户端以流式传输响应（带生成参数）
	gen := s.buildGenerationParams()
	var llmMsgs []llm.Message
	for _, m := range messages {
		llmMsgs = append(llmMsgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	err = s.llmClient.StreamChatMessages(ctx, llmMsgs, gen, interceptor)
	if err != nil {
		metrics.IncChatError()
		return err
	}

	// 4. 发送完成通知，并将对话保存到 Redis
	sendCompletion(ws)
	fullAnswer := answerBuilder.String()
	if len(fullAnswer) > 0 {
		// 使用后台上下文，因为即使原始请求被取消，我们也希望保存成功生成的答案
		err = s.addMessageToConversation(context.Background(), user.ID, convID, query, fullAnswer)
		if err != nil {
			// 只记录错误，不返回给客户端，因为流式响应已经成功
			log.Errorf("Failed to save conversation history: %v", err)
		}
	}

	return nil
}

// retrieveWithSelfCheck 执行 Agentic 检索：先检索，若结果不充分则用查询改写重检索（最多 maxRetries 次）。
func (s *chatService) retrieveWithSelfCheck(ctx context.Context, query string, user *model.User) ([]model.SearchResponseDTO, error) {
	results, err := s.searchService.HybridSearch(ctx, query, 10, user)
	if err != nil {
		return nil, err
	}
	agentic := config.Conf.Search.Agentic
	if !agentic.Enabled || s.rewriter == nil || s.sufficient(results, agentic.MinScore) {
		return results, nil
	}
	log.Infof("[Agentic] 首轮检索不充分（top 分 %.4f），尝试改写重检索", maxScore(results))
	usedQuery := query
	for i := 0; i < agentic.MaxRetries; i++ {
		newQuery, rwErr := s.rewriter.Rewrite(ctx, usedQuery)
		if rwErr != nil || newQuery == usedQuery {
			break
		}
		usedQuery = newQuery
		retried, rErr := s.searchService.HybridSearch(ctx, usedQuery, 10, user)
		if rErr != nil {
			log.Warnf("[Agentic] 重检索失败: %v", rErr)
			break
		}
		results = retried
		if s.sufficient(results, agentic.MinScore) {
			break
		}
	}
	return results, nil
}

// sufficient 判断检索结果是否充分：结果非空且最高分不低于阈值。
func (s *chatService) sufficient(results []model.SearchResponseDTO, minScore float64) bool {
	if len(results) == 0 {
		return false
	}
	return maxScore(results) >= minScore
}

func maxScore(results []model.SearchResponseDTO) float64 {
	m := 0.0
	for _, r := range results {
		if r.Score > m {
			m = r.Score
		}
	}
	return m
}

// buildPrompt 根据用户输入和搜索结果构建prompt
func (s *chatService) buildContextText(searchResults []model.SearchResponseDTO) string {
	if len(searchResults) == 0 {
		return ""
	}
	// 与 Processor 的 chunkSize 对齐，尽量不截断分块内容
	const maxSnippetLen = 1000
	var contextBuilder strings.Builder
	for i, r := range searchResults {
		snippet := r.ParentText
		if snippet == "" {
			snippet = r.TextContent
		}
		if len(snippet) > maxSnippetLen {
			snippet = snippet[:maxSnippetLen] + "…"
		}
		fileLabel := r.FileName
		if fileLabel == "" {
			fileLabel = "unknown"
		}
		contextBuilder.WriteString(fmt.Sprintf("[%d] (%s) %s\n", i+1, fileLabel, snippet))
	}
	return contextBuilder.String()
}

func (s *chatService) buildSystemMessage(contextText string) string {
	// 从 llm.prompt 读取规则与包裹符
	p := config.Conf.LLM.Prompt
	rules := p.Rules
	refStart := p.RefStart
	if refStart == "" {
		refStart = "<<REF>>"
	}
	refEnd := p.RefEnd
	if refEnd == "" {
		refEnd = "<<END>>"
	}
	noRes := p.NoResultText
	if noRes == "" {
		noRes = "（本轮无检索结果）"
	}
	var sys strings.Builder
	if rules != "" {
		sys.WriteString(rules)
		sys.WriteString("\n\n")
	}
	sys.WriteString(refStart)
	sys.WriteString("\n")
	if contextText != "" {
		sys.WriteString(contextText)
	} else {
		sys.WriteString(noRes)
		sys.WriteString("\n")
	}
	sys.WriteString(refEnd)
	return sys.String()
}

func (s *chatService) loadHistory(ctx context.Context, convID string) ([]model.ChatMessage, error) {
	return s.conversationRepo.GetConversationHistory(ctx, convID)
}

// boundHistory 限制送入 LLM 的历史消息：保留最近 maxMessages 条，且累计长度不超过 maxRunes。
// 从头部逐条裁减，始终保留最近几条（一问一答），避免长会话超出模型上下文窗口。
func boundHistory(history []model.ChatMessage) []model.ChatMessage {
	const maxMessages = 20
	const maxRunes = 8000
	if len(history) <= maxMessages && historyRuneLen(history) <= maxRunes {
		return history
	}
	kept := history
	for len(kept) > 2 {
		if len(kept) <= maxMessages && historyRuneLen(kept) <= maxRunes {
			break
		}
		kept = kept[1:]
	}
	return kept
}

func historyRuneLen(history []model.ChatMessage) int {
	total := 0
	for _, m := range history {
		total += len([]rune(m.Content))
	}
	return total
}

func (s *chatService) composeMessages(systemMsg string, history []model.ChatMessage, userInput string) []model.ChatMessage {
	msgs := make([]model.ChatMessage, 0, len(history)+2)
	msgs = append(msgs, model.ChatMessage{Role: "system", Content: systemMsg})
	msgs = append(msgs, history...)
	msgs = append(msgs, model.ChatMessage{Role: "user", Content: userInput})
	return msgs
}

// addMessageToConversation 是一个用于管理 Redis 中对话历史的辅助函数。
func (s *chatService) addMessageToConversation(ctx context.Context, userID uint, conversationID, question, answer string) error {
	history, err := s.conversationRepo.GetConversationHistory(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to get conversation history: %w", err)
	}

	// 添加用户消息
	history = append(history, model.ChatMessage{
		Role:      "user",
		Content:   question,
		Timestamp: time.Now(),
	})

	// 添加助手消息
	history = append(history, model.ChatMessage{
		Role:      "assistant",
		Content:   answer,
		Timestamp: time.Now(),
	})

	if err := s.conversationRepo.UpdateConversationHistory(ctx, conversationID, history); err != nil {
		return err
	}
	return s.conversationRepo.TouchConversation(ctx, userID, conversationID, len(history))
}

// wsWriterInterceptor 是对 websocket.Conn 的封装，用于捕获写入的消息。
type wsWriterInterceptor struct {
	conn       *websocket.Conn
	writer     *strings.Builder
	shouldStop func() bool
}

// WriteMessage 满足 llm.MessageWriter 接口。
func (w *wsWriterInterceptor) WriteMessage(messageType int, data []byte) error {
	if w.shouldStop != nil && w.shouldStop() {
		// 停止标志生效：跳过下发
		return nil
	}
	w.writer.Write(data)
	// 将原始分块包装成 {"chunk":"..."}
	payload := map[string]string{"chunk": string(data)}
	b, _ := json.Marshal(payload)
	return w.conn.WriteMessage(messageType, b)
}

// sendCompletion 发送完成通知 JSON
func sendCompletion(ws *websocket.Conn) {
	notif := map[string]interface{}{
		"type":      "completion",
		"status":    "finished",
		"message":   "响应已完成",
		"timestamp": time.Now().UnixMilli(),
		"date":      time.Now().Format("2006-01-02T15:04:05"),
	}
	b, _ := json.Marshal(notif)
	_ = ws.WriteMessage(websocket.TextMessage, b)
}

func (s *chatService) buildGenerationParams() *llm.GenerationParams {
	var gp llm.GenerationParams
	if config.Conf.LLM.Generation.Temperature != 0 {
		t := config.Conf.LLM.Generation.Temperature
		gp.Temperature = &t
	}
	if config.Conf.LLM.Generation.TopP != 0 {
		p := config.Conf.LLM.Generation.TopP
		gp.TopP = &p
	}
	if config.Conf.LLM.Generation.MaxTokens != 0 {
		m := config.Conf.LLM.Generation.MaxTokens
		gp.MaxTokens = &m
	}
	if gp.Temperature == nil && gp.TopP == nil && gp.MaxTokens == nil {
		return nil
	}
	return &gp
}
