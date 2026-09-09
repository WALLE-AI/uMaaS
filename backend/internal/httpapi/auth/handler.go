// Package auth 是控制平面的身份端点：/auth/* 与 /me。
package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	authsvc "github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/response"
)

// SessionCookieName 由契约规定（securitySchemes.cookieSession.name）。
const SessionCookieName = "umaas_session"

// AdminSessionCookieName 与用户会话**不同名**：
// 同名会让 admin 与 web 在同域部署时互相覆盖，而两者的 TTL 与策略并不相同。
const AdminSessionCookieName = "umaas_admin_session"

type Handler struct {
	svc    *authsvc.Service
	secure bool // 生产环境为 true：cookie 只走 HTTPS
}

func New(svc *authsvc.Service, secure bool) *Handler {
	return &Handler{svc: svc, secure: secure}
}

// Routes 挂载 /auth/*。
func (h *Handler) Routes(r chi.Router) {
	r.Post("/login", h.login)
	r.Post("/register", h.register)
	r.Get("/session", h.session)
	r.Post("/logout", h.logout)
	r.Post("/password/reset-request", h.requestPasswordReset)
	r.Get("/oauth/{provider}", h.startOAuth)
}

// ── 契约的载荷类型 ──────────────────────────────────────────────

type userPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	WorkspaceID string `json:"workspace_id"`
}

type sessionPayload struct {
	User      userPayload `json:"user"`
	ExpiresAt string      `json:"expires_at"`
}

func toUserPayload(p *domain.Principal) userPayload {
	return userPayload{
		ID:          strconv.FormatInt(p.User.ID, 10),
		Name:        p.User.Name,
		Email:       p.User.Email,
		AvatarURL:   p.User.AvatarURL,
		WorkspaceID: strconv.FormatInt(p.WorkspaceID, 10),
	}
}

// ── 端点 ────────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	res, err := h.svc.Login(r.Context(), authsvc.LoginInput{
		Email: req.Email, Password: req.Password, Remember: req.Remember,
		IP: clientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	h.setSessionCookie(w, SessionCookieName, res.Token, res.ExpiresAt)
	response.OK(w, r, sessionPayload{
		User:      toUserPayload(res.Principal),
		ExpiresAt: res.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

type registerRequest struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	AcceptedTerms bool   `json:"accepted_terms"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	res, err := h.svc.Register(r.Context(), authsvc.RegisterInput{
		Name: req.Name, Email: req.Email, Password: req.Password,
		AcceptedTerms: req.AcceptedTerms,
		IP:            clientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	h.setSessionCookie(w, SessionCookieName, res.Token, res.ExpiresAt)
	response.JSON(w, r, http.StatusCreated, sessionPayload{
		User:      toUserPayload(res.Principal),
		ExpiresAt: res.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil)
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	principal, err := h.svc.Session(r.Context(), cookieValue(r, SessionCookieName))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, sessionPayload{
		User:      toUserPayload(principal),
		ExpiresAt: principal.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), cookieValue(r, SessionCookieName)); err != nil {
		response.Error(w, r, err)
		return
	}
	h.clearSessionCookie(w, SessionCookieName)
	w.WriteHeader(http.StatusNoContent)
}

// requestPasswordReset 无论账号是否存在都返回 204。
//
// 契约的 summary 就写着 "without revealing account existence"——
// 返回 404 等于把这个接口变成账号枚举器。
func (h *Handler) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	// 邮件发送在 I1 范围外（需要邮件通道）。这里只吞掉请求并保持契约语义，
	// 不做一半——留一个"有时 404 有时 204"的实现比不实现更糟。
	w.WriteHeader(http.StatusNoContent)
}

// startOAuth 目前返回 501 之外的明确错误：OAuth 需要在 settings 里配好
// client id/secret（I6 的 X12），此前不该假装可用。
func (h *Handler) startOAuth(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider != "github" && provider != "google" {
		response.Error(w, r, domain.Validation("unsupported_provider",
			"provider must be github or google", "provider"))
		return
	}
	// return_to 必须校验，否则这是个开放重定向——
	// 攻击者可以用我们的域名把用户导去任意站点。
	if rt := r.URL.Query().Get("return_to"); rt != "" {
		if u, err := url.Parse(rt); err != nil || u.IsAbs() {
			response.Error(w, r, domain.Validation("invalid_return_to",
				"return_to must be a relative path", "return_to"))
			return
		}
	}
	response.Error(w, r, domain.NotFound("oauth_not_configured",
		"OAuth sign-in is not configured yet"))
}

// ── cookie ──────────────────────────────────────────────────────

// setSessionCookie 按契约设置会话 cookie：HttpOnly + Secure + SameSite=Lax。
//
// SameSite=Lax 意味着跨站 POST 不带 cookie，所以 OAuth 回调必须是 GET 重定向
// （契约里的 /auth/oauth/{provider} 正是 302）。
func (h *Handler) setSessionCookie(w http.ResponseWriter, name, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// ── 辅助 ────────────────────────────────────────────────────────

// decodeJSON 解析请求体，并拒绝未知字段。
//
// 拒绝未知字段是为了让"字段名写错"在开发期就暴露，而不是变成一个
// 被静默忽略的参数——`remember` 拼错成 `remeber` 时用户会以为记住我坏了。
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Validation("invalid_body", "request body is not valid JSON", "")
	}
	return nil
}

// clientIP 取真实客户端 IP。
//
// 只信任反代设置的 X-Real-IP：X-Forwarded-For 可以被客户端伪造，
// 拿它做审计与限流会让攻击者能自选身份。
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}

func splitHostPort(addr string) (string, string, error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return addr, "", nil
}
