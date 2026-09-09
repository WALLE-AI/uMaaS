// Package domain 定义领域模型与 Repository 接口，不含实现。
//
// 领域层不认识 HTTP：它返回带语义的错误，由 httpapi 层统一翻译成状态码
// （ARCHITECTURE.md §5.1）。在每个 handler 里手写状态码，同一类错误在不同端点
// 返回不同状态码是迟早的事。
package domain

import (
	"errors"
	"fmt"
)

// 领域错误的语义与契约的错误码表一一对应（ARCHITECTURE.md §5.1）。
var (
	ErrNotFound     = errors.New("resource not found")   // → 404
	ErrConflict     = errors.New("resource conflict")    // → 409
	ErrValidation   = errors.New("validation failed")    // → 422
	ErrUnauthorized = errors.New("unauthenticated")      // → 401
	ErrForbidden    = errors.New("forbidden")            // → 403
	ErrRateLimited  = errors.New("rate limited")         // → 429
	ErrInsufficient = errors.New("insufficient balance") // → 402
	ErrUnavailable  = errors.New("upstream unavailable") // → 503
)

// Error 是带字段定位的领域错误。Field 用于 422 的逐字段报错。
type Error struct {
	Kind    error  // 上面那组哨兵之一
	Code    string // 稳定的机器可读码，如 "model_not_found"
	Message string // 面向人的说明
	Field   string // 可选：出错的字段路径，如 "pricing.input_per_million"
	cause   error
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (field %s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 让 errors.Is(err, domain.ErrNotFound) 成立。
func (e *Error) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return e.Kind
}

func (e *Error) WithCause(cause error) *Error {
	clone := *e
	clone.cause = cause
	return &clone
}

func NotFound(code, msg string) *Error {
	return &Error{Kind: ErrNotFound, Code: code, Message: msg}
}

func Conflict(code, msg string) *Error {
	return &Error{Kind: ErrConflict, Code: code, Message: msg}
}

func Validation(code, msg, field string) *Error {
	return &Error{Kind: ErrValidation, Code: code, Message: msg, Field: field}
}

func Unauthorized(code, msg string) *Error {
	return &Error{Kind: ErrUnauthorized, Code: code, Message: msg}
}

func Forbidden(code, msg string) *Error {
	return &Error{Kind: ErrForbidden, Code: code, Message: msg}
}
