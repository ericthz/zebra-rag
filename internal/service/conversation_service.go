// Package service 包含了应用的业务逻辑层。
package service

import (
	"context"

	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
)

// ConversationService 定义了对话业务逻辑的接口。
type ConversationService interface {
	// GetConversationHistory 获取用户最近会话的历史（兼容旧调用）。
	GetConversationHistory(ctx context.Context, userID uint) ([]model.ChatMessage, error)
	// AddMessageToConversation 将消息追加到用户最近会话。
	AddMessageToConversation(ctx context.Context, userID uint, message model.ChatMessage) error

	// 多会话管理
	ListConversations(ctx context.Context, userID uint) ([]repository.ConversationMeta, error)
	ListArchivedConversations(ctx context.Context, userID uint) ([]repository.ConversationMeta, error)
	GetConversationMessages(ctx context.Context, userID uint, conversationID string) ([]model.ChatMessage, error)
	ArchiveConversation(ctx context.Context, userID uint, conversationID string) error
	DeleteConversation(ctx context.Context, userID uint, conversationID string) error
}

type conversationService struct {
	repo repository.ConversationRepository
}

// NewConversationService 创建一个新的 ConversationService。
func NewConversationService(repo repository.ConversationRepository) ConversationService {
	return &conversationService{repo: repo}
}

// GetConversationHistory 获取用户最近会话的历史。
func (s *conversationService) GetConversationHistory(ctx context.Context, userID uint) ([]model.ChatMessage, error) {
	conversationID, err := s.repo.GetOrCreateConversationID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetConversationHistory(ctx, conversationID)
}

// AddMessageToConversation 将消息追加到用户最近会话。
func (s *conversationService) AddMessageToConversation(ctx context.Context, userID uint, message model.ChatMessage) error {
	conversationID, err := s.repo.GetOrCreateConversationID(ctx, userID)
	if err != nil {
		return err
	}
	return s.appendMessage(ctx, userID, conversationID, message)
}

// ListConversations 返回用户「未归档」会话列表。
func (s *conversationService) ListConversations(ctx context.Context, userID uint) ([]repository.ConversationMeta, error) {
	return s.repo.ListConversations(ctx, userID)
}

// ListArchivedConversations 返回用户「已归档」会话列表。
func (s *conversationService) ListArchivedConversations(ctx context.Context, userID uint) ([]repository.ConversationMeta, error) {
	return s.repo.ListArchivedConversations(ctx, userID)
}

// GetConversationMessages 返回指定会话的消息。
func (s *conversationService) GetConversationMessages(ctx context.Context, userID uint, conversationID string) ([]model.ChatMessage, error) {
	_, messages, err := s.repo.GetConversation(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// ArchiveConversation 归档指定会话。
func (s *conversationService) ArchiveConversation(ctx context.Context, userID uint, conversationID string) error {
	return s.repo.ArchiveConversation(ctx, userID, conversationID)
}

// DeleteConversation 删除指定会话。
func (s *conversationService) DeleteConversation(ctx context.Context, userID uint, conversationID string) error {
	return s.repo.DeleteConversation(ctx, userID, conversationID)
}

// appendMessage 加载历史 -> 追加 -> 保存并刷新会话元信息。
func (s *conversationService) appendMessage(ctx context.Context, userID uint, conversationID string, message model.ChatMessage) error {
	history, err := s.repo.GetConversationHistory(ctx, conversationID)
	if err != nil {
		return err
	}
	history = append(history, message)
	if err := s.repo.UpdateConversationHistory(ctx, conversationID, history); err != nil {
		return err
	}
	return s.repo.TouchConversation(ctx, userID, conversationID, len(history))
}
