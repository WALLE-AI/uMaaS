// Package platform 是跨领域的基础设施：ID 生成、时钟、request_id 传播。
//
// 这里的东西不属于任何业务模块，但所有模块都可能用到。放在这里而不是各自复制一份，
// 是为了让 request_id 的形状在全系统唯一——它要贯穿日志、trace 与契约响应
// （ARCHITECTURE.md §5.1、SERVICE-DECOMPOSITION.md §9）。
package platform

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
)

// NewRequestID 生成一个请求标识。契约要求成功与失败都返回 request_id，
// 因此它必须在最外层中间件生成，而不是等到 handler 里。
func NewRequestID() string {
	return "req_" + uuid.NewString()
}

// WithRequestID 把 request_id 放进 context。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestID 取出 request_id，不存在时返回空串。
//
// 不 panic 也不生成新的：调用方拿到空串说明中间件链没配好，
// 这是配置错误而不是运行时错误，应当在冒烟测试里暴露。
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}
