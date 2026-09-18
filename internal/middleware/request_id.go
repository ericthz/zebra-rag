// Package middleware 存放 Gin 框架的中间件。
package middleware

import (
	"github.com/ericthz/zebra-rag/pkg/trace"
	"github.com/gin-gonic/gin"
)

// RequestIDMiddleware 生成或透传 Request-ID（链路追踪关联键，C2.2 轻量版）：
//   - 客户端可携带 X-Request-Id；
//   - 未携带则生成随机 ID；
//   - 写入 gin.Context 与响应头，便于日志与后续链路关联。
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(trace.Header)
		if rid == "" {
			rid = trace.NewID()
		}
		c.Set(trace.Header, rid)
		c.Writer.Header().Set(trace.Header, rid)
		c.Next()
	}
}

// GetRequestID 从 gin.Context 读取 Request-ID。
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(trace.Header); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
