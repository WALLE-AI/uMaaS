package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

func newRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	return r.WithContext(platform.WithRequestID(r.Context(), "req_test"))
}

func TestEnvelopeShape(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, newRequest(), map[string]string{"id": "openai/gpt-5.6-sol"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "req_test", w.Header().Get("X-Request-Id"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	// 契约：{data, meta?, request_id}，meta 缺省时不出现。
	assert.Contains(t, body, "data")
	assert.Equal(t, "req_test", body["request_id"])
	assert.NotContains(t, body, "meta")
}

// 契约要求成功与失败都带 request_id。这条最容易在错误路径上漏掉。
func TestErrorAlwaysCarriesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, newRequest(), domain.NotFound("model_not_found", "no such model"))

	var body ErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "req_test", body.RequestID)
	assert.Equal(t, "model_not_found", body.Error.Code)
}

// 错误映射是全系统唯一的翻译点——同一类错误在不同端点返回不同状态码，
// 正是它存在的理由（ARCHITECTURE.md §5.1）。
func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.NotFound("model_not_found", "x"), http.StatusNotFound, "model_not_found"},
		{"conflict", domain.Conflict("slug_taken", "x"), http.StatusConflict, "slug_taken"},
		{"validation", domain.Validation("bad_email", "x", "email"), http.StatusUnprocessableEntity, "bad_email"},
		{"unauthorized", domain.Unauthorized("no_session", "x"), http.StatusUnauthorized, "no_session"},
		{"forbidden", domain.Forbidden("not_owner", "x"), http.StatusForbidden, "not_owner"},
		{"bare sentinel", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"rate limited", domain.ErrRateLimited, http.StatusTooManyRequests, "rate_limited"},
		{"insufficient balance", domain.ErrInsufficient, http.StatusPaymentRequired, "insufficient_balance"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Error(w, newRequest(), tc.err)

			assert.Equal(t, tc.status, w.Code)
			var body ErrorBody
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.code, body.Error.Code)
		})
	}
}

// 内部错误绝不能回显给客户端——它可能含连接串、SQL 片段。
func TestInternalErrorDoesNotLeakDetails(t *testing.T) {
	w := httptest.NewRecorder()
	secret := "pq: password authentication failed for user postgres://admin:hunter2@db"
	Error(w, newRequest(), errors.New(secret))

	assert.NotContains(t, w.Body.String(), "hunter2")
	assert.NotContains(t, w.Body.String(), "postgres://")
}

// 包装过的错误也要能被识别，否则 handler 里加一层 %w 就会掉进 500。
func TestWrappedErrorStillMaps(t *testing.T) {
	w := httptest.NewRecorder()
	wrapped := fmt.Errorf("load model: %w", domain.NotFound("model_not_found", "x"))
	Error(w, newRequest(), wrapped)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestValidationErrorCarriesField(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, newRequest(), domain.Validation("invalid_price", "must be >= 0", "pricing.input_per_million"))

	var body ErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "pricing.input_per_million", body.Error.Field)
}
