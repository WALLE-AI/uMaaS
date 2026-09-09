// Package response 实现契约的统一信封与错误映射。
//
// 只服务控制平面。数据平面的响应必须保持 OpenAI 原样兼容，绝不包信封
// （ARCHITECTURE.md §1、附录检查表最后一条）。
package response

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

// Envelope 是契约规定的响应外壳：{data, meta?, request_id}。
type Envelope[T any] struct {
	Data      T      `json:"data"`
	Meta      *Meta  `json:"meta,omitempty"`
	RequestID string `json:"request_id"`
}

// Meta 承载分页游标与数据新鲜度。
type Meta struct {
	NextCursor *string `json:"next_cursor,omitempty"`
	UpdatedAt  *string `json:"updated_at,omitempty"`
	Total      *int    `json:"total,omitempty"`
}

// ErrorBody 是错误响应的形状。成功与失败都带 request_id。
type ErrorBody struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// JSON 写一个成功响应。
func JSON[T any](w http.ResponseWriter, r *http.Request, status int, data T, meta *Meta) {
	body := Envelope[T]{Data: data, Meta: meta, RequestID: platform.RequestID(r.Context())}
	write(w, r, status, body)
}

// OK 是 JSON 的 200 快捷方式。
func OK[T any](w http.ResponseWriter, r *http.Request, data T) {
	JSON(w, r, http.StatusOK, data, nil)
}

// Error 把领域错误翻译成 HTTP 状态码并写出。
//
// 这是全系统唯一的翻译点。领域层返回语义错误，HTTP 层在这里映射——
// 否则同一类错误在不同端点返回不同状态码是迟早的事（ARCHITECTURE.md §5.1）。
func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := mapError(err)

	if status >= 500 {
		// 5xx 记完整错误，但不把内部细节回给客户端。
		// request_id 由 observ 的 contextHandler 自动附加，这里不重复写。
		slog.ErrorContext(r.Context(), "request failed",
			"error", err,
			"path", r.URL.Path,
		)
	}
	write(w, r, status, ErrorBody{Error: detail, RequestID: platform.RequestID(r.Context())})
}

func mapError(err error) (int, ErrorDetail) {
	var domErr *domain.Error
	if errors.As(err, &domErr) {
		return statusFor(domErr.Kind), ErrorDetail{
			Code:    domErr.Code,
			Message: domErr.Message,
			Field:   domErr.Field,
		}
	}
	// 裸哨兵错误也要能映射——领域层允许直接返回 domain.ErrNotFound。
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, ErrorDetail{Code: "not_found", Message: "resource not found"}
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, ErrorDetail{Code: "conflict", Message: "resource conflict"}
	case errors.Is(err, domain.ErrValidation):
		return http.StatusUnprocessableEntity, ErrorDetail{Code: "validation_failed", Message: "validation failed"}
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, ErrorDetail{Code: "unauthorized", Message: "authentication required"}
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, ErrorDetail{Code: "forbidden", Message: "insufficient permissions"}
	case errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, ErrorDetail{Code: "rate_limited", Message: "too many requests"}
	case errors.Is(err, domain.ErrInsufficient):
		return http.StatusPaymentRequired, ErrorDetail{Code: "insufficient_balance", Message: "insufficient balance"}
	case errors.Is(err, domain.ErrUnavailable):
		return http.StatusServiceUnavailable, ErrorDetail{Code: "upstream_unavailable", Message: "upstream unavailable"}
	}
	// 未知错误一律 500，且**不回显 err.Error()**——内部错误信息可能含连接串、SQL 片段。
	return http.StatusInternalServerError, ErrorDetail{Code: "internal_error", Message: "internal server error"}
}

func statusFor(kind error) int {
	switch {
	case errors.Is(kind, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(kind, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(kind, domain.ErrValidation):
		return http.StatusUnprocessableEntity
	case errors.Is(kind, domain.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(kind, domain.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(kind, domain.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(kind, domain.ErrInsufficient):
		return http.StatusPaymentRequired
	case errors.Is(kind, domain.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func write(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if id := platform.RequestID(r.Context()); id != "" {
		w.Header().Set("X-Request-Id", id)
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// 响应头已经发出去了，这里只能记日志。
		slog.ErrorContext(r.Context(), "encode response failed", "error", err)
	}
}
