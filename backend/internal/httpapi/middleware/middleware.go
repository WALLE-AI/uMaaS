// Package middleware 提供控制平面与数据平面共用的中间件。
//
// 注意两个平面的中间件链是**独立组装**的：控制平面包信封、数据平面不包
// （ARCHITECTURE.md §1）。这里只提供构件，链的形状由各自的 router 决定。
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

// RequestID 生成或透传 request_id，放进 context 与响应头。
//
// 透传上游传来的 X-Request-Id 是为了让 Gateway → Control 的调用能串成一条链
// （SERVICE-DECOMPOSITION.md §9 的分布式追踪）。但只接受看起来合理的值，
// 避免把任意用户输入原样写进日志。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = platform.NewRequestID()
		}
		ctx := platform.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		isAllowed := c == '-' || c == '_' || c == '.' ||
			(c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z')
		if !isAllowed {
			return false
		}
	}
	return true
}

// Logger 记录每个请求的结构化日志。
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		slog.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.written,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", platform.RequestID(r.Context()),
		)
	})
}

// Recover 把 panic 转成 500，避免一个 handler 的 bug 打掉整个进程。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
					"request_id", platform.RequestID(r.Context()),
				)
				// 响应头可能已经发出（尤其流式），此时只能断开。
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS 按配置的允许来源放行。
//
// 不用通配符：会话走 cookie（HttpOnly + SameSite=Lax），
// 带凭证的请求下 Access-Control-Allow-Origin 不允许是 *。
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok && origin != "" {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder 记录状态码与写出字节数。
//
// 实现 http.Flusher 转发：数据平面的 SSE 依赖 Flush，
// 中间件包装如果吞掉 Flusher，流式就会退化成"慢的非流式"（ARCHITECTURE.md §6.1）。
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written int
	wrote   bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.wrote = true
	}
	n, err := s.ResponseWriter.Write(b)
	s.written += n
	return n, err
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 让 http.ResponseController 能找到底层的 ResponseWriter。
// 这是 Go 1.20+ 的约定，比类型断言 http.Flusher 更稳妥。
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
