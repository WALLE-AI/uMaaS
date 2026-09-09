package auth

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	authsvc "github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/response"
)

// AdminRoutes 挂载 /admin/auth/*。
//
// 这组端点前端目前一个都没有（services.ts 里没有任何 login/session 调用），
// 因此契约以本文件为准，写 openapi.admin.yaml 时照此补齐（ARCHITECTURE.md §10-E）。
func (h *Handler) AdminRoutes(r chi.Router) {
	r.Post("/login", h.adminLogin)
	r.Post("/logout", h.adminLogout)
	r.Get("/session", h.adminSession)
	r.Post("/totp/confirm", h.adminConfirmTOTP)
}

type adminUserPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	TwoFactor bool   `json:"two_factor"`
}

type adminSessionPayload struct {
	Admin     adminUserPayload `json:"admin"`
	ExpiresAt string           `json:"expires_at"`
}

func toAdminPayload(p *domain.AdminPrincipal) adminUserPayload {
	return adminUserPayload{
		ID:        strconv.FormatInt(p.Account.ID, 10),
		Name:      p.Account.Name,
		Email:     p.Account.Email,
		Role:      string(p.Account.Role),
		TwoFactor: p.Account.TOTPEnabled,
	}
}

func (h *Handler) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	res, err := h.svc.AdminLogin(r.Context(), authsvc.AdminLoginInput{
		Email: req.Email, Password: req.Password, TOTPCode: req.TOTPCode,
		IP: clientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	h.setSessionCookie(w, AdminSessionCookieName, res.Token, res.ExpiresAt)
	response.OK(w, r, adminSessionPayload{
		Admin:     toAdminPayload(res.Principal),
		ExpiresAt: res.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.AdminLogout(r.Context(), cookieValue(r, AdminSessionCookieName)); err != nil {
		response.Error(w, r, err)
		return
	}
	h.clearSessionCookie(w, AdminSessionCookieName)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) adminSession(w http.ResponseWriter, r *http.Request) {
	principal, err := h.svc.AdminSession(r.Context(), cookieValue(r, AdminSessionCookieName))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, adminSessionPayload{
		Admin:     toAdminPayload(principal),
		ExpiresAt: principal.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// adminConfirmTOTP 完成 2FA 绑定。
//
// 需要密码而不是会话：绑定前的账号**根本无法登录**（全员强制 2FA），
// 因此这条路径上不可能有会话可用。
func (h *Handler) adminConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.svc.ConfirmAdminTOTP(r.Context(), req.Email, req.Password, req.Code); err != nil {
		response.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── 中间件 ──────────────────────────────────────────────────────

type ctxKey int

const (
	ctxKeyPrincipal ctxKey = iota
	ctxKeyAdmin
)

// RequireSession 是用户会话中间件。
func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := h.svc.Session(r.Context(), cookieValue(r, SessionCookieName))
		if err != nil {
			response.Error(w, r, err)
			return
		}
		ctx := contextWithPrincipal(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin 是管理员会话中间件。
//
// 与 RequireSession 是两条独立的链：平台管理员不是"某个工作空间的 admin"，
// 合并会让每次鉴权都要回答"这个角色是对哪个工作空间说的"（ARCHITECTURE.md §4.5）。
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := h.svc.AdminSession(r.Context(), cookieValue(r, AdminSessionCookieName))
		if err != nil {
			response.Error(w, r, err)
			return
		}
		ctx := contextWithAdmin(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdminWrite 在 RequireAdmin 之后使用，拦住 viewer 的写操作。
func RequireAdminWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := AdminFromContext(r.Context())
		if !ok {
			response.Error(w, r, domain.Unauthorized("no_session", "not authenticated"))
			return
		}
		if !admin.Account.Role.CanWrite() {
			response.Error(w, r, domain.Forbidden("read_only_role",
				"this admin role cannot perform write operations"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func contextWithPrincipal(ctx context.Context, p *domain.Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal, p)
}

func contextWithAdmin(ctx context.Context, p *domain.AdminPrincipal) context.Context {
	return context.WithValue(ctx, ctxKeyAdmin, p)
}

// PrincipalFromContext 取出已认证用户。
func PrincipalFromContext(ctx context.Context) (*domain.Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal).(*domain.Principal)
	return p, ok
}

// AdminFromContext 取出已认证管理员。
func AdminFromContext(ctx context.Context) (*domain.AdminPrincipal, bool) {
	p, ok := ctx.Value(ctxKeyAdmin).(*domain.AdminPrincipal)
	return p, ok
}

// CurrentUser 实现 GET /me。
func (h *Handler) CurrentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		response.Error(w, r, domain.Unauthorized("no_session", "not authenticated"))
		return
	}
	response.OK(w, r, toUserPayload(principal))
}
