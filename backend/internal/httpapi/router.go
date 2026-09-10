// Package httpapi 是控制平面：/api/v1/*，统一信封、会话、分页、CRUD。
//
// 与数据平面的中间件链**独立组装**。控制平面的信封中间件会破坏 OpenAI 兼容性，
// 因此绝不能共用一条链（ARCHITECTURE.md §1）。
package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/assembly"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	admincatalogapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/admincatalog"
	authapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/auth"
	catalogapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/middleware"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/response"
	"github.com/WALLE-AI/uMaaS/backend/internal/observ"
)

// Deps 是控制平面需要的依赖。
type Deps struct {
	Config   *config.Config
	Services *assembly.Services
	// Health 由 main 注入，避免 httpapi 直接依赖 store 包。
	Health func(ctx context.Context) error
	// Version 是构建版本，供 /healthz 返回。
	Version string
	// Auth 是身份端点与会话中间件。为 nil 时 /auth/* 与 /me 不挂载
	// （单元测试里常用）。
	Auth *authapi.Handler
	// Catalog 是公开目录端点（I2 / B2）。
	Catalog *catalogapi.Handler
	// AdminCatalog 是 admin 的目录与价目写端点（I2 / M1a）。
	// 它挂在 RequireAdmin 之后，因此 Auth 为 nil 时不会被挂载。
	AdminCatalog *admincatalogapi.Handler
	// Metrics 为 nil 时不记指标。
	Metrics *observ.HTTPMetrics
}

// HealthPayload 是 /healthz 与 /readyz 的响应体。
type HealthPayload struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	Topology string `json:"topology"`
}

// NewRouter 组装控制平面路由。
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(deps.Config.Security.CORSAllowedOrigins))
	if deps.Metrics != nil {
		r.Use(deps.Metrics.Middleware("control"))
	}

	// /healthz：进程活着就返回 200。**不查数据库**——
	// 让编排系统能区分"进程挂了"（该重启）与"依赖挂了"（重启无用）。
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		response.OK(w, r, HealthPayload{
			Status:   "ok",
			Version:  deps.Version,
			Topology: string(deps.Config.Topology),
		})
	})

	// /readyz：依赖也就绪才算就绪，供负载均衡摘流量用。
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.Health != nil {
			if err := deps.Health(r.Context()); err != nil {
				response.JSON(w, r, http.StatusServiceUnavailable, HealthPayload{
					Status:   "degraded",
					Version:  deps.Version,
					Topology: string(deps.Config.Topology),
				}, nil)
				return
			}
		}
		response.OK(w, r, HealthPayload{
			Status:   "ready",
			Version:  deps.Version,
			Topology: string(deps.Config.Topology),
		})
	})

	r.Route("/api/v1", func(r chi.Router) {
		if deps.Auth != nil {
			r.Route("/auth", deps.Auth.Routes)

			// /me 需要会话。用子路由挂中间件，避免把 RequireSession
			// 套到整个 /api/v1 上——目录类端点是公开的。
			r.Group(func(r chi.Router) {
				r.Use(deps.Auth.RequireSession)
				r.Get("/me", deps.Auth.CurrentUser)
			})

			// 管理端自成一套鉴权链（ARCHITECTURE.md §4.5）。
			r.Route("/admin", func(r chi.Router) {
				r.Route("/auth", deps.Auth.AdminRoutes)

				r.Group(func(r chi.Router) {
					r.Use(deps.Auth.RequireAdmin)
					if deps.AdminCatalog != nil {
						r.Route("/catalog", deps.AdminCatalog.Routes)
					}
					// I6 起，其余 /admin/* 端点继续挂在这个组下。
				})
			})
		}

		// 目录是**公开的**：未登录用户也要能浏览模型与价格
		// （计价 §3.6-② 推论），所以挂在 RequireSession 之外。
		if deps.Catalog != nil {
			deps.Catalog.Routes(r)
		}

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			response.Error(w, r, notImplemented())
		})
	})

	return r
}
