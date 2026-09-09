// Package gateway 是推理数据平面：/v1/*，OpenAI 兼容。
//
// 与控制平面的三条硬差异（ARCHITECTURE.md §1）：
//   - 响应**不包信封**，保持 OpenAI 原样兼容
//   - 中间件链独立，不复用控制平面的信封/序列化中间件
//   - 连接池独立：长连接会把控制平面的池吃干净
package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/assembly"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/middleware"
	"github.com/WALLE-AI/uMaaS/backend/internal/observ"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

type Deps struct {
	Config   *config.Config
	Services *assembly.Services
	Version  string
	Metrics  *observ.HTTPMetrics
}

// NewRouter 组装数据平面路由。
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	// 只用最小中间件集。任何会缓冲响应体的中间件都不能加——
	// 一层缓冲就会把"流式"变成"慢的非流式"（ARCHITECTURE.md §6.1）。
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	r.Use(middleware.Logger)
	if deps.Metrics != nil {
		r.Use(deps.Metrics.Middleware("data"))
	}

	r.Route("/v1", func(r chi.Router) {
		// I3 起挂载：
		//   r.Post("/chat/completions", completions.Handler(deps))
		r.NotFound(openAIError(http.StatusNotFound, "not_found",
			"the requested endpoint is not implemented yet"))
	})

	return r
}

// openAIError 按 OpenAI 的错误格式返回，**不包我们自己的信封**。
//
// 客户端是 OpenAI SDK，它按 {"error": {...}} 解析。包了信封会让 SDK 报解析失败，
// 而错误信息本身就丢了——排查时看到的是 SDK 的解析异常，不是真实原因。
func openAIError(status int, code, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if id := platform.RequestID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		w.WriteHeader(status)
		body := map[string]any{
			"error": map[string]any{
				"message": message,
				"type":    "invalid_request_error",
				"code":    code,
			},
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			slog.ErrorContext(r.Context(), "encode openai error failed", "error", err)
		}
	}
}
