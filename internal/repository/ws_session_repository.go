// Package repository 提供了数据访问层的实现。
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

// wsSessionTTL 定义 WebSocket 会话令牌的有效期。
// 短时有效，避免像长期 JWT 那样出现在 URL 中造成长期泄漏。
const wsSessionTTL = time.Hour

// WSSessionRepository 管理 WebSocket 会话令牌（短时有效，绑定用户名）。
type WSSessionRepository interface {
	Create(ctx context.Context, token, username string) error
	GetUsername(ctx context.Context, token string) (string, error)
	Delete(ctx context.Context, token string) error
}

type redisWSSessionRepository struct {
	redisClient *redis.Client
}

// NewWSSessionRepository 创建一个基于 Redis 的 WS 会话仓库。
func NewWSSessionRepository(redisClient *redis.Client) WSSessionRepository {
	return &redisWSSessionRepository{redisClient: redisClient}
}

func (r *redisWSSessionRepository) key(token string) string {
	return "ws:session:" + token
}

func (r *redisWSSessionRepository) Create(ctx context.Context, token, username string) error {
	return r.redisClient.Set(ctx, r.key(token), username, wsSessionTTL).Err()
}

func (r *redisWSSessionRepository) GetUsername(ctx context.Context, token string) (string, error) {
	val, err := r.redisClient.Get(ctx, r.key(token)).Result()
	if err == redis.Nil {
		return "", errors.New("无效或已过期的会话令牌")
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *redisWSSessionRepository) Delete(ctx context.Context, token string) error {
	return r.redisClient.Del(ctx, r.key(token)).Err()
}
