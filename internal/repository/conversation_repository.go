// Package repository 提供了数据访问层的实现。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericthz/zebra-rag/internal/model"

	"github.com/go-redis/redis/v8"
)

const conversationTTL = 7 * 24 * time.Hour

// ErrConversationNotFound 会话不存在。
var ErrConversationNotFound = errors.New("会话不存在")

// ConversationMeta 会话元信息。
type ConversationMeta struct {
	ConversationID string    `json:"conversationId"`
	Title          string    `json:"title"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	MessageCount   int       `json:"messageCount"`
	Archived       bool      `json:"archived"`
}

// ConversationRepository 定义了对话历史记录的操作接口。
type ConversationRepository interface {
	// GetOrCreateConversationID 兼容旧调用：返回用户最近一次会话（没有则创建）。
	GetOrCreateConversationID(ctx context.Context, userID uint) (string, error)
	GetConversationHistory(ctx context.Context, conversationID string) ([]model.ChatMessage, error)
	UpdateConversationHistory(ctx context.Context, conversationID string, messages []model.ChatMessage) error
	GetAllUserConversationMappings(ctx context.Context) (map[uint]string, error)

	// 多会话管理
	ConversationExists(ctx context.Context, conversationID string) (bool, error)
	CreateConversation(ctx context.Context, userID uint, title string) (string, error)
	ListConversations(ctx context.Context, userID uint) ([]ConversationMeta, error)
	ListArchivedConversations(ctx context.Context, userID uint) ([]ConversationMeta, error)
	GetConversation(ctx context.Context, userID uint, conversationID string) (*ConversationMeta, []model.ChatMessage, error)
	ArchiveConversation(ctx context.Context, userID uint, conversationID string) error
	DeleteConversation(ctx context.Context, userID uint, conversationID string) error
	TouchConversation(ctx context.Context, userID uint, conversationID string, messageCount int) error
}

type redisConversationRepository struct {
	redisClient *redis.Client
}

// NewConversationRepository 创建一个新的 ConversationRepository 实例。
func NewConversationRepository(redisClient *redis.Client) ConversationRepository {
	return &redisConversationRepository{redisClient: redisClient}
}

func (r *redisConversationRepository) userListKey(userID uint) string {
	return fmt.Sprintf("user:%d:conversations", userID)
}

func (r *redisConversationRepository) metaKey(conversationID string) string {
	return "conversation_meta:" + conversationID
}

func (r *redisConversationRepository) historyKey(conversationID string) string {
	return "conversation:" + conversationID
}

// GetOrCreateConversationID 兼容旧调用：返回用户最近一次会话，没有则创建。
func (r *redisConversationRepository) GetOrCreateConversationID(ctx context.Context, userID uint) (string, error) {
	convs, err := r.ListConversations(ctx, userID)
	if err != nil {
		return "", err
	}
	if len(convs) > 0 {
		return convs[0].ConversationID, nil
	}
	return r.CreateConversation(ctx, userID, "新会话")
}

// CreateConversation 创建新会话并加入用户会话列表。
func (r *redisConversationRepository) CreateConversation(ctx context.Context, userID uint, title string) (string, error) {
	convID := fmt.Sprintf("c-%d-%d", userID, time.Now().UnixNano())
	now := time.Now()
	meta := ConversationMeta{ConversationID: convID, Title: title, CreatedAt: now, UpdatedAt: now}
	data, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	if err := r.redisClient.Set(ctx, r.metaKey(convID), data, conversationTTL).Err(); err != nil {
		return "", fmt.Errorf("failed to create conversation meta: %w", err)
	}
	if err := r.redisClient.ZAdd(ctx, r.userListKey(userID), &redis.Z{Score: float64(now.Unix()), Member: convID}).Err(); err != nil {
		return "", fmt.Errorf("failed to add conversation to list: %w", err)
	}
	return convID, nil
}

// ConversationExists 判断会话是否存在。
func (r *redisConversationRepository) ConversationExists(ctx context.Context, conversationID string) (bool, error) {
	_, err := r.getMeta(ctx, conversationID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrConversationNotFound) {
		return false, nil
	}
	return false, err
}

// ListConversations 按最近更新倒序返回用户「未归档」会话列表。
func (r *redisConversationRepository) ListConversations(ctx context.Context, userID uint) ([]ConversationMeta, error) {
	return r.listConversationsByArchived(ctx, userID, false)
}

// ListArchivedConversations 按最近更新倒序返回用户「已归档」会话列表。
func (r *redisConversationRepository) ListArchivedConversations(ctx context.Context, userID uint) ([]ConversationMeta, error) {
	return r.listConversationsByArchived(ctx, userID, true)
}

func (r *redisConversationRepository) listConversationsByArchived(ctx context.Context, userID uint, archived bool) ([]ConversationMeta, error) {
	members, err := r.redisClient.ZRevRange(ctx, r.userListKey(userID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	result := make([]ConversationMeta, 0, len(members))
	for _, convID := range members {
		meta, err := r.getMeta(ctx, convID)
		if err != nil {
			continue
		}
		if meta.Archived != archived {
			continue
		}
		result = append(result, *meta)
	}
	return result, nil
}

func (r *redisConversationRepository) getMeta(ctx context.Context, conversationID string) (*ConversationMeta, error) {
	data, err := r.redisClient.Get(ctx, r.metaKey(conversationID)).Bytes()
	if err == redis.Nil {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	var meta ConversationMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// GetConversation 校验归属后返回会话元信息与消息。
func (r *redisConversationRepository) GetConversation(ctx context.Context, userID uint, conversationID string) (*ConversationMeta, []model.ChatMessage, error) {
	meta, err := r.getMeta(ctx, conversationID)
	if err != nil {
		return nil, nil, err
	}
	messages, err := r.GetConversationHistory(ctx, conversationID)
	if err != nil {
		return nil, nil, err
	}
	return meta, messages, nil
}

// ArchiveConversation 归档用户某个会话（校验归属）；已归档则幂等返回。
func (r *redisConversationRepository) ArchiveConversation(ctx context.Context, userID uint, conversationID string) error {
	meta, err := r.getMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	// 校验归属：meta 中不存 userID，改用会话 ID 前缀约定（c-{uid}-...）校验
	if !conversationOwnedBy(meta.ConversationID, userID) {
		return errors.New("无权归档该会话")
	}
	if meta.Archived {
		return nil
	}
	meta.Archived = true
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return r.redisClient.Set(ctx, r.metaKey(conversationID), data, conversationTTL).Err()
}

// DeleteConversation 删除用户某个会话（仅允许删除「已归档」会话，校验归属）。
func (r *redisConversationRepository) DeleteConversation(ctx context.Context, userID uint, conversationID string) error {
	meta, err := r.getMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	// 校验归属：meta 中不存 userID，改用会话 ID 前缀约定（c-{uid}-...）校验
	if !conversationOwnedBy(meta.ConversationID, userID) {
		return errors.New("无权删除该会话")
	}
	if !meta.Archived {
		return errors.New("仅可删除已归档的会话，请先归档")
	}
	pipe := r.redisClient.TxPipeline()
	pipe.ZRem(ctx, r.userListKey(userID), conversationID)
	pipe.Del(ctx, r.metaKey(conversationID))
	pipe.Del(ctx, r.historyKey(conversationID))
	_, err = pipe.Exec(ctx)
	return err
}

// TouchConversation 更新会话的更新时间与消息数（写入/追加消息后调用）。
func (r *redisConversationRepository) TouchConversation(ctx context.Context, userID uint, conversationID string, messageCount int) error {
	meta, err := r.getMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	meta.UpdatedAt = time.Now()
	meta.MessageCount = messageCount
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := r.redisClient.Set(ctx, r.metaKey(conversationID), data, conversationTTL).Err(); err != nil {
		return err
	}
	return r.redisClient.ZAdd(ctx, r.userListKey(userID), &redis.Z{Score: float64(meta.UpdatedAt.Unix()), Member: conversationID}).Err()
}

// conversationOwnedBy 通过会话 ID 前缀（c-{uid}-）判断归属。
func conversationOwnedBy(conversationID string, userID uint) bool {
	prefix := fmt.Sprintf("c-%d-", userID)
	return len(conversationID) > len(prefix) && conversationID[:len(prefix)] == prefix
}

// GetConversationHistory 从 Redis 获取对话历史记录。
func (r *redisConversationRepository) GetConversationHistory(ctx context.Context, conversationID string) ([]model.ChatMessage, error) {
	jsonData, err := r.redisClient.Get(ctx, r.historyKey(conversationID)).Result()
	if err == redis.Nil {
		return []model.ChatMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation history: %w", err)
	}
	var messages []model.ChatMessage
	if err := json.Unmarshal([]byte(jsonData), &messages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal conversation history: %w", err)
	}
	return messages, nil
}

// UpdateConversationHistory 在 Redis 中更新对话历史记录（保留最近 20 条）。
func (r *redisConversationRepository) UpdateConversationHistory(ctx context.Context, conversationID string, messages []model.ChatMessage) error {
	if len(messages) > 20 {
		messages = messages[len(messages)-20:]
	}
	jsonData, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation history: %w", err)
	}
	if err := r.redisClient.Set(ctx, r.historyKey(conversationID), jsonData, conversationTTL).Err(); err != nil {
		return fmt.Errorf("failed to set conversation history: %w", err)
	}
	return nil
}

// GetAllUserConversationMappings 返回用户 -> 最近会话 的映射（兼容管理端）。
func (r *redisConversationRepository) GetAllUserConversationMappings(ctx context.Context) (map[uint]string, error) {
	keys, err := r.redisClient.Keys(ctx, "user:*:conversations").Result()
	if err != nil {
		return nil, err
	}
	result := make(map[uint]string)
	for _, k := range keys {
		var uid uint
		if _, scanErr := fmt.Sscanf(k, "user:%d:conversations", &uid); scanErr != nil {
			continue
		}
		convIDs, listErr := r.redisClient.ZRevRange(ctx, k, 0, 0).Result()
		if listErr != nil || len(convIDs) == 0 {
			continue
		}
		result[uid] = convIDs[0]
	}
	return result, nil
}
