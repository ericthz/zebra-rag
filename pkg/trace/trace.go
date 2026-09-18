// Package trace 提供轻量链路关联工具（Request-ID 透传，C2.2）。
// 独立于 internal 包以避免 import 环。
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Header 是请求/响应头中携带的关联 ID 名。
const Header = "X-Request-Id"

type ctxKey struct{}

// WithID 将 Request-ID 写入 context.Context。
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 从 context.Context 读取 Request-ID，无则返回空串。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// NewID 生成 32 位十六进制随机 ID。
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(b)
}
