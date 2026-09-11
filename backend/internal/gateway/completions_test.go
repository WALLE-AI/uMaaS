package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider/openaicompat"
	"github.com/WALLE-AI/uMaaS/backend/internal/requestlog"
)

// ── 测试用假目录/路由 ────────────────────────────────────────────

type fakeModels struct {
	billingModel string
	capabilities []string
	found        bool
}

func (f *fakeModels) ResolveCallable(ctx context.Context, providerSlug, modelSlug string) (*ResolvedModel, error) {
	if !f.found {
		return nil, domain.NotFound("model_not_found", "not found")
	}
	return &ResolvedModel{ID: 1, BillingModel: f.billingModel, Capabilities: f.capabilities}, nil
}

type discardLogger struct{}

func (discardLogger) Enqueue(requestlog.Entry) {}

func nopWriter(t *testing.T) RequestLogger {
	t.Helper()
	return discardLogger{}
}

// fakeRepoRecordingCalls 包一层 fakeRouteRepo，记录 RecordSuccess/RecordFailure
// 被调用了多少次、对哪个渠道——回退测试要断言"确实换过渠道"。
type fakeRepoRecordingCalls struct {
	fakeRouteRepo
	successCalls atomic.Int64
	failureCalls atomic.Int64
	lastFailedID atomic.Int64
}

func (f *fakeRepoRecordingCalls) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64) error {
	f.successCalls.Add(1)
	return nil
}
func (f *fakeRepoRecordingCalls) RecordFailure(ctx context.Context, channelID int64, rateLimited bool, rateLimitSeconds int) error {
	f.failureCalls.Add(1)
	f.lastFailedID.Store(channelID)
	return nil
}

func singleChannelHandler(t *testing.T, upstream *httptest.Server) *CompletionsHandler {
	t.Helper()
	client := upstream.Client()
	repo := &fakeRepoRecordingCalls{fakeRouteRepo: fakeRouteRepo{
		candidates: []RawCandidate{{
			ChannelID: 1, ChannelName: "c1", Profile: provider.Profile{Name: "c1", BaseURL: upstream.URL},
			UpstreamModel: "gpt-x", Health: ChannelHealth{Alpha: 1, Beta: 1},
		}},
		policy: PolicyResolution{Strategy: "balanced", FallbackDepth: 5},
	}}
	router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	return &CompletionsHandler{
		Models: &fakeModels{found: true, billingModel: "openai/gpt-x"},
		Router: router,
		NewDriver: func(p provider.Profile) UpstreamDoer {
			return openaicompat.New(p, client)
		},
		Logs:             nopWriter(t),
		FirstByteTimeout: 200 * time.Millisecond,
		StallTimeout:     150 * time.Millisecond,
	}
}

func chatBody(model string, stream bool) io.Reader {
	body := map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"stream":   stream,
	}
	b, _ := json.Marshal(body)
	return strings.NewReader(string(b))
}

// ── 正常流式：分块到达，逐块转发 ──────────────────────────────────

func TestCompletions_StreamHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, chunk := range []string{
			`{"id":"1","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":null}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", true))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"content":"Hel"`)
	assert.Contains(t, body, `"content":"lo"`)
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
	assert.Contains(t, body, `"model":"openai/gpt-x"`, "response must echo the client's requested model id")
}

// ── 非流式 ──────────────────────────────────────────────────────

func TestCompletions_NonStreamHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "choices": []map[string]any{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "hi there"},
			}},
			"usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hi there")
}

// ── 模型不存在 ──────────────────────────────────────────────────

func TestCompletions_UnknownModelReturns404WithOpenAIShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call upstream for an unresolvable model")
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.Models = &fakeModels{found: false}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("nope/nope", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "error")
	assert.NotContains(t, body, "data", "gateway responses must never be wrapped in the control-plane envelope")
}

// ── 能力协商：模型不支持工具调用时在发出任何上游请求前就拒绝 ─────────

func TestCompletions_NegotiationRejectsUnsupportedTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call upstream when capability negotiation already failed")
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.Models = &fakeModels{found: true, billingModel: "openai/gpt-x"} // no "tools" capability
	body := map[string]any{
		"model":    "openai/gpt-x",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"tools":    []map[string]any{{"type": "function", "function": map[string]string{"name": "f"}}},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(b)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestCompletions_NegotiationAllowsToolsWhenSupported(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "choices": []map[string]any{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.Models = &fakeModels{found: true, billingModel: "openai/gpt-x", capabilities: []string{"tools"}}
	body := map[string]any{
		"model":    "openai/gpt-x",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"tools":    []map[string]any{{"type": "function", "function": map[string]string{"name": "f"}}},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(b)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ── 无可用渠道 ──────────────────────────────────────────────────

func TestCompletions_NoChannelsReturns503(t *testing.T) {
	repo := &fakeRouteRepo{candidates: nil}
	router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	h := &CompletionsHandler{
		Models: &fakeModels{found: true, billingModel: "openai/gpt-x"}, Router: router,
		NewDriver: func(p provider.Profile) UpstreamDoer { return nil },
		Logs:      nopWriter(t), FirstByteTimeout: time.Second, StallTimeout: time.Second,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// ── 回退：第一个渠道 5xx，换第二个渠道成功 ─────────────────────────

func TestCompletions_NonStreamFallsBackToSecondChannelOn5xx(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "choices": []map[string]any{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "from good channel"},
			}},
		})
	}))
	defer good.Close()

	repo := &fakeRepoRecordingCalls{fakeRouteRepo: fakeRouteRepo{
		candidates: []RawCandidate{
			{ChannelID: 1, ChannelName: "bad", Profile: provider.Profile{Name: "bad", BaseURL: bad.URL}, UpstreamModel: "m"},
			{ChannelID: 2, ChannelName: "good", Profile: provider.Profile{Name: "good", BaseURL: good.URL}, UpstreamModel: "m"},
		},
		policy: PolicyResolution{Strategy: "priority", FallbackDepth: 5},
	}}
	router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	h := &CompletionsHandler{
		Models: &fakeModels{found: true, billingModel: "m"}, Router: router,
		NewDriver: func(p provider.Profile) UpstreamDoer {
			return openaicompat.New(p, http.DefaultClient)
		},
		Logs: nopWriter(t), FirstByteTimeout: 2 * time.Second, StallTimeout: 2 * time.Second,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/m", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "from good channel")
	assert.Equal(t, int64(1), repo.failureCalls.Load(), "the failing channel must be recorded as a failure")
	assert.Equal(t, int64(1), repo.successCalls.Load(), "the succeeding channel must be recorded as a success")
}

// **回归测试**：真实 E2E 验证时发现，最初的实现只在整条请求收尾时写一条
// request_logs，中间失败的渠道尝试完全没有落库——只更新了健康后验，
// 运维排查"为什么这次打到了第二个渠道"时看不到第一个渠道为什么失败。
// request_logs 从 I3 起就是 attempt 级表（M2），这里锁住"每次尝试都要
// 落一条日志，is_final 只在真正收尾的那次为 true"这条约定。
func TestCompletions_LogsOneEntryPerAttempt(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "choices": []map[string]any{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer good.Close()

	var logged []requestlog.Entry
	logger := recordingLogger{entries: &logged}

	repo := &fakeRouteRepo{
		candidates: []RawCandidate{
			{ChannelID: 1, ChannelName: "bad", Profile: provider.Profile{Name: "bad", BaseURL: bad.URL}, UpstreamModel: "m"},
			{ChannelID: 2, ChannelName: "good", Profile: provider.Profile{Name: "good", BaseURL: good.URL}, UpstreamModel: "m"},
		},
		policy: PolicyResolution{Strategy: "priority", FallbackDepth: 5},
	}
	router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	h := &CompletionsHandler{
		Models: &fakeModels{found: true, billingModel: "m"}, Router: router,
		NewDriver: func(p provider.Profile) UpstreamDoer {
			return openaicompat.New(p, http.DefaultClient)
		},
		Logs: &logger, FirstByteTimeout: 2 * time.Second, StallTimeout: 2 * time.Second,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/m", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, logged, 2, "one attempt against the failing channel and one against the succeeding channel")

	assert.Equal(t, int32(0), logged[0].AttemptIndex)
	assert.False(t, logged[0].IsFinal, "the failed first attempt must not be marked final")
	require.NotNil(t, logged[0].ChannelID)
	assert.Equal(t, int64(1), *logged[0].ChannelID)
	assert.Equal(t, int32(http.StatusInternalServerError), logged[0].StatusCode)

	assert.Equal(t, int32(1), logged[1].AttemptIndex)
	assert.True(t, logged[1].IsFinal, "the succeeding attempt must be marked final")
	require.NotNil(t, logged[1].ChannelID)
	assert.Equal(t, int64(2), *logged[1].ChannelID)
	assert.Equal(t, int32(http.StatusOK), logged[1].StatusCode)
}

type recordingLogger struct {
	entries *[]requestlog.Entry
}

func (r *recordingLogger) Enqueue(e requestlog.Entry) { *r.entries = append(*r.entries, e) }

// 4xx 参数错误不应该换渠道——两个渠道都会得到同样的错误，浪费一次上游调用。
func TestCompletions_NonStreamDoesNotRetryOn400(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer upstream.Close()

	repo := &fakeRepoRecordingCalls{fakeRouteRepo: fakeRouteRepo{
		candidates: []RawCandidate{
			{ChannelID: 1, ChannelName: "a", Profile: provider.Profile{Name: "a", BaseURL: upstream.URL}, UpstreamModel: "m"},
			{ChannelID: 2, ChannelName: "b", Profile: provider.Profile{Name: "b", BaseURL: upstream.URL}, UpstreamModel: "m"},
		},
		policy: PolicyResolution{Strategy: "priority", FallbackDepth: 5},
	}}
	router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	h := &CompletionsHandler{
		Models: &fakeModels{found: true, billingModel: "m"}, Router: router,
		NewDriver: func(p provider.Profile) UpstreamDoer {
			return openaicompat.New(p, http.DefaultClient)
		},
		Logs: nopWriter(t), FirstByteTimeout: 2 * time.Second, StallTimeout: 2 * time.Second,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/m", false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, int32(1), calls.Load(), "a 400 must not be retried against another channel")
}

// ── 首字节超时：安全窗口内，还没写任何东西给客户端 ─────────────────

func TestCompletions_FirstByteTimeout(t *testing.T) {
	block := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // 永远不响应，直到测试结束
	}))
	defer upstream.Close()
	defer close(block)

	h := singleChannelHandler(t, upstream)
	h.FirstByteTimeout = 50 * time.Millisecond
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", true))
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after first-byte timeout")
	}

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "error")
}

// ── stall 超时：首字节之后卡住，必须据实上报而不是假装 stop ─────────

func TestCompletions_StallTimeoutAfterFirstByte(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.FirstByteTimeout = 2 * time.Second
	h.StallTimeout = 80 * time.Millisecond
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", true))
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return after stall timeout")
	}

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "partial")
	assert.Contains(t, body, `"upstream_error"`, "a stalled stream must surface an explicit error event")
	assert.NotContains(t, body, `"finish_reason":"stop"`,
		"a stalled stream must never be reported as a normal stop (ARCHITECTURE.md §6.2)")
	assert.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
}

// ── 客户端断开：必须取消上游请求，不能让它继续跑并计费 ──────────────

func TestCompletions_ClientDisconnectCancelsUpstream(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.FirstByteTimeout = 5 * time.Second
	h.StallTimeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", true)).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("client disconnect must propagate to the upstream request context")
	}
}

// ── 上游首字节前就返回非 2xx：安全窗口内的失败 ─────────────────────

func TestCompletions_UpstreamErrorBeforeFirstByte(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/gpt-x", true))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "error")
}

// ── 每个 SSE chunk 都是独立的一次 flush（不是攒到最后一次性吐出）───

func TestCompletions_FlushesEachChunkIndependently(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		<-release
		fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	h := singleChannelHandler(t, upstream)
	h.FirstByteTimeout = 5 * time.Second
	h.StallTimeout = 5 * time.Second

	server := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer server.Close()

	resp, err := http.Post(server.URL, "application/json", chatBody("openai/gpt-x", true))
	require.NoError(t, err)
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	_, err = readUntilData(reader)
	require.NoError(t, err)
	line, err := readUntilData(reader)
	require.NoError(t, err)
	assert.Contains(t, line, "first",
		"the first chunk must reach the client before the upstream sends the rest — "+
			"if this hangs until timeout, some layer is buffering the whole stream")
	close(release)
}

func readUntilData(r *bufio.Reader) (string, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(line, "data: ") {
			return line, nil
		}
	}
}
