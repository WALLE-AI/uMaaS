package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authsvc "github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	authapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/middleware"
)

// stubService 只实现 handler 需要的那部分，避免为 HTTP 层的测试拖进整个服务。
// 这里用真实 Service + 内存仓储更省事，但那会让"cookie 属性"这类断言
// 依赖服务层的行为——两层的失败混在一起，定位成本反而更高。

type fakeIdentity struct {
	principal *domain.Principal
	loginErr  error
	// created 记录本次是否刚签发过会话。登录路径会在写入后立刻回查，
	// 假实现要能反映这一点，否则登录永远拿不到 principal。
	created bool
}

func (f *fakeIdentity) CreateUserWithWorkspace(context.Context, domain.NewUser) (*domain.User, *domain.Workspace, error) {
	return &domain.User{ID: 1, Email: "ada@example.com", Name: "Ada", Status: domain.UserActive},
		&domain.Workspace{ID: 100}, nil
}
func (f *fakeIdentity) GetUserByEmail(context.Context, string) (*domain.User, string, error) {
	if f.loginErr != nil {
		return nil, "", f.loginErr
	}
	// $argon2id$... 对应密码 "supersecret"
	return &domain.User{ID: 1, Email: "ada@example.com", Name: "Ada", Status: domain.UserActive},
		testPasswordHash, nil
}
func (f *fakeIdentity) GetUserByID(context.Context, int64) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeIdentity) PrimaryMembership(context.Context, int64) (int64, domain.Role, error) {
	return 100, domain.RoleOwner, nil
}
func (f *fakeIdentity) CreateSession(context.Context, domain.NewSession) (*domain.Session, error) {
	f.created = true
	return &domain.Session{ID: 1}, nil
}
func (f *fakeIdentity) LookupSession(context.Context, []byte) (*domain.Principal, error) {
	if f.principal != nil {
		return f.principal, nil
	}
	if f.created {
		return &domain.Principal{
			User:        domain.User{ID: 1, Email: "ada@example.com", Name: "Ada"},
			WorkspaceID: 100,
			ExpiresAt:   time.Now().Add(time.Hour),
		}, nil
	}
	return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
}
func (f *fakeIdentity) RevokeSession(context.Context, []byte) error { return nil }

var testPasswordHash string

func init() {
	// 在 init 里算一次，避免每个用例都跑 argon2。
	h, err := hashForTests("supersecret")
	if err != nil {
		panic(err)
	}
	testPasswordHash = h
}

func newHandler(t *testing.T, secure bool, principal *domain.Principal) http.Handler {
	t.Helper()
	ids := &fakeIdentity{principal: principal}
	svc := authsvc.NewService(ids, nil, nil, nil)
	h := authapi.New(svc, secure)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1/auth", h.Routes)
	r.Group(func(r chi.Router) {
		r.Use(h.RequireSession)
		r.Get("/api/v1/me", h.CurrentUser)
	})
	return r
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// 契约规定 HttpOnly + Secure + SameSite=Lax，并明确写了
// "Access tokens are not stored in localStorage"。
func TestLoginSetsHardenedCookie(t *testing.T) {
	h := newHandler(t, true, nil)

	w := postJSON(t, h, "/api/v1/auth/login",
		`{"email":"ada@example.com","password":"supersecret","remember":false}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]

	assert.Equal(t, "umaas_session", c.Name, "cookie name is fixed by the contract")
	assert.True(t, c.HttpOnly, "cookie must be HttpOnly")
	assert.True(t, c.Secure, "cookie must be Secure in production")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
}

// 会话令牌绝不能出现在响应体里——它只走 Set-Cookie。
func TestLoginDoesNotLeakTokenInBody(t *testing.T) {
	h := newHandler(t, true, nil)
	w := postJSON(t, h, "/api/v1/auth/login",
		`{"email":"ada@example.com","password":"supersecret","remember":false}`)

	token := w.Result().Cookies()[0].Value
	assert.NotContains(t, w.Body.String(), token)
}

func TestLoginResponseMatchesContract(t *testing.T) {
	h := newHandler(t, false, nil)
	w := postJSON(t, h, "/api/v1/auth/login",
		`{"email":"ada@example.com","password":"supersecret","remember":false}`)

	var body struct {
		Data struct {
			User struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				WorkspaceID string `json:"workspace_id"`
			} `json:"user"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, "ada@example.com", body.Data.User.Email)
	assert.NotEmpty(t, body.Data.User.WorkspaceID)
	assert.NotEmpty(t, body.RequestID)
	// 时间必须是 ISO 8601 UTC（契约的对接检查表）。
	_, err := time.Parse(time.RFC3339, body.Data.ExpiresAt)
	assert.NoError(t, err)
}

// 密码错误必须是 401 而不是 404/422：区分开就等于账号枚举。
func TestLoginWrongPasswordIs401(t *testing.T) {
	h := newHandler(t, false, nil)
	w := postJSON(t, h, "/api/v1/auth/login",
		`{"email":"ada@example.com","password":"wrong-password","remember":false}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, w.Result().Cookies(), "failed login must not set a cookie")
}

// 字段名写错时要报错，而不是静默当默认值——
// `remember` 拼成 `remeber` 时用户会以为"记住我"坏了。
func TestUnknownFieldsRejected(t *testing.T) {
	h := newHandler(t, false, nil)
	w := postJSON(t, h, "/api/v1/auth/login",
		`{"email":"ada@example.com","password":"supersecret","remeber":true}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestLogoutClearsCookie(t *testing.T) {
	h := newHandler(t, false, &domain.Principal{
		User: domain.User{ID: 1, Email: "ada@example.com"}, WorkspaceID: 100,
	})

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: "umaas_session", Value: "whatever"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, w.Result().Cookies(), 1)
	assert.Equal(t, -1, w.Result().Cookies()[0].MaxAge)
}

func TestMeRequiresSession(t *testing.T) {
	h := newHandler(t, false, nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMeReturnsCurrentUser(t *testing.T) {
	h := newHandler(t, false, &domain.Principal{
		User:        domain.User{ID: 7, Email: "ada@example.com", Name: "Ada"},
		WorkspaceID: 100,
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: "umaas_session", Value: "token"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "7", body.Data.ID)
	assert.Equal(t, "100", body.Data.WorkspaceID)
}

// 契约的 summary 写着 "without revealing account existence"。
func TestPasswordResetAlwaysReturns204(t *testing.T) {
	h := newHandler(t, false, nil)
	w := postJSON(t, h, "/api/v1/auth/password/reset-request", `{"email":"nobody@example.com"}`)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// return_to 必须是相对路径，否则这是个开放重定向：
// 攻击者能用我们的域名把用户导去任意站点。
func TestOAuthRejectsAbsoluteReturnTo(t *testing.T) {
	h := newHandler(t, false, nil)

	r := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/oauth/github?mode=login&return_to=https://evil.example.com", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_return_to")
}

func TestOAuthRejectsUnknownProvider(t *testing.T) {
	h := newHandler(t, false, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/facebook?mode=login", nil))
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
