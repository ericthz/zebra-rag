// Package middleware 存放 Gin 框架的中间件。
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// RateLimitMiddleware 基于 Redis 的固定窗口限流。
// key = ratelimit:{clientIP}:{path}，窗口内超过 limit 次请求返回 429。
func RateLimitMiddleware(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil || limit <= 0 {
			c.Next()
			return
		}
		key := fmt.Sprintf("ratelimit:%s:%s", c.ClientIP(), c.FullPath())
		ctx := context.Background()
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			log.Warnf("[RateLimit] Redis 计数失败: %v", err)
			c.Next()
			return
		}
		if count == 1 {
			_ = rdb.Expire(ctx, key, window).Err()
		}
		if count > int64(limit) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "请求过于频繁，请稍后再试",
				"data":    nil,
			})
			return
		}
		c.Next()
	}
}
