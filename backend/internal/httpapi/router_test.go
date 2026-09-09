package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/assembly"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/gateway"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi"
)

func testConfig() *config.Config {
	cfg, err := config.Load("")
	if err != nil {
		panic(err)
	}
	return cfg
}

func TestMain(m *testing.M) {
	// config.Load 要求 DSN 非空，这里给一个不会被真正连接的值。
	_ = m
	m.Run()
}

func newControlPlane(t *testing.T, health func(context.Context) error) http.Handler {
	t.Setenv("UMAAS_DATABASE__DSN", "postgres://localhost/umaas_test")
	cfg := testConfig()
	svcs, err := assembly.Build(cfg)
	require.NoError(t, err)
	return httpapi.NewRouter(httpapi.Deps{
		Config: cfg, Services: svcs, Health: health, Version: "test",
	})
}

func TestHealthzDoesNotTouchDatabase(t *testing.T) {
	called := false
	h := newControlPlane(t, func(context.Context) error {
		called = true
		return errors.New("db is down")
	})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	// /healthz 表示"进程活着"。查数据库会让编排系统在依赖故障时
	// 反复重启一个其实健康的进程。
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, called, "/healthz must not check the database")
}

func TestReadyzReflectsDependencies(t *testing.T) {
	h := newControlPlane(t, func(context.Context) error { return errors.New("db is down") })

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "degraded", body["data"].(map[string]any)["status"])
}

func TestReadyzOKWhenHealthy(t *testing.T) {
	h := newControlPlane(t, func(context.Context) error { return nil })

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// 每个响应都要有 request_id，且未提供时由中间件生成。
func TestRequestIDGenerated(t *testing.T) {
	h := newControlPlane(t, nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.NotEmpty(t, w.Header().Get("X-Request-Id"))
}

// 上游传来的 request_id 要透传，这样 Gateway → Control 能串成一条链。
func TestRequestIDPropagated(t *testing.T) {
	h := newControlPlane(t, nil)

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set("X-Request-Id", "req_from_gateway")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, "req_from_gateway", w.Header().Get("X-Request-Id"))
}

// 但不能把任意用户输入原样写进日志与响应头。
func TestMaliciousRequestIDRejected(t *testing.T) {
	h := newControlPlane(t, nil)

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set("X-Request-Id", "bad\nX-Injected: 1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	got := w.Header().Get("X-Request-Id")
	assert.NotContains(t, got, "\n")
	assert.NotEqual(t, "bad\nX-Injected: 1", got)
}

// 数据平面绝不能包信封——客户端是 OpenAI SDK，按 {"error":{...}} 解析。
func TestDataPlaneDoesNotWrapInEnvelope(t *testing.T) {
	t.Setenv("UMAAS_DATABASE__DSN", "postgres://localhost/umaas_test")
	cfg := testConfig()
	svcs, err := assembly.Build(cfg)
	require.NoError(t, err)
	h := gateway.NewRouter(gateway.Deps{Config: cfg, Services: svcs, Version: "test"})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Contains(t, body, "error")
	assert.NotContains(t, body, "data", "data plane must stay OpenAI-compatible, no envelope")
	assert.NotContains(t, body, "request_id", "envelope fields must not appear in the data plane body")
	// request_id 走响应头，不进响应体。
	assert.NotEmpty(t, w.Header().Get("X-Request-Id"))
}
