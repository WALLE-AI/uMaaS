package gateway

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

// fakeUpstreamDoer 是一个可编程的 UpstreamDoer，用来驱动 runProbe 的分支
// 而不需要起一个真实的 httptest.Server——探测逻辑本身不关心 HTTP 传输层
// 的细节，那部分由 completions_test.go 和 openaicompat 覆盖过了。
type fakeUpstreamDoer struct {
	buildErr error
	doErr    error
	resp     *http.Response
}

func (f *fakeUpstreamDoer) BuildRequest(ctx context.Context, req *ir.Request, upstreamModel string) (*http.Request, error) {
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
}

func (f *fakeUpstreamDoer) Do(httpReq *http.Request) (*http.Response, error) {
	if f.doErr != nil {
		return nil, f.doErr
	}
	return f.resp, nil
}

func TestProbeRequest_UsesFixedShape(t *testing.T) {
	req := probeRequest()
	require.NotNil(t, req.MaxTokens)
	assert.Equal(t, int64(1), *req.MaxTokens, "probe requests must always cap max_tokens at 1")
	assert.False(t, req.Stream, "probe requests must never be streaming")
}

type fakeRouteRepoForProbe struct {
	fakeRouteRepo
	successCalled, failureCalled bool
}

func (f *fakeRouteRepoForProbe) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64) error {
	f.successCalled = true
	return nil
}
func (f *fakeRouteRepoForProbe) RecordFailure(ctx context.Context, channelID int64, rateLimited bool, rateLimitSeconds int) error {
	f.failureCalled = true
	return nil
}

// **这是 X10 判据里最容易做错、也最重要的一条**：手工测试绝不能碰健康状态机。
func TestProbeService_ManualTestNeverTouchesHealthState(t *testing.T) {
	repo := &fakeRouteRepoForProbe{}
	svc := &ProbeService{Repo: repo, Logs: discardLogger{}}

	driver := &fakeUpstreamDoer{buildErr: assertErr("connection refused")}
	_, _ = svc.ManualTest(context.Background(), 1, driver, "m", "billing/m")

	assert.False(t, repo.successCalled, "manual test must never call RecordSuccess")
	assert.False(t, repo.failureCalled, "manual test must never call RecordFailure")
}

// 反过来，自动探针必须写入健康状态机——否则渠道劣化永远不会被 B4 感知到。
func TestProbeService_AutoProbeRecordsFailureOnError(t *testing.T) {
	repo := &fakeRouteRepoForProbe{}
	svc := &ProbeService{Repo: repo, Logs: discardLogger{}}

	driver := &fakeUpstreamDoer{buildErr: assertErr("connection refused")}
	svc.AutoProbe(context.Background(), 1, driver, "m", "billing/m")

	assert.True(t, repo.failureCalled, "auto probe must record failures so B4's downgrade can react")
	assert.False(t, repo.successCalled)
}

func TestProbeService_AutoProbeRecordsSuccessOnHealthyResponse(t *testing.T) {
	repo := &fakeRouteRepoForProbe{}
	svc := &ProbeService{Repo: repo, Logs: discardLogger{}}
	origDecode := probeDecodeOverride
	defer func() { probeDecodeOverride = origDecode }()
	probeDecodeOverride = func(io.Reader, string) (*ir.Response, error) {
		return &ir.Response{Usage: ir.Usage{PromptTokens: 5, CompletionTokens: 1}}, nil
	}

	driver := &fakeUpstreamDoer{resp: &http.Response{StatusCode: 200, Body: http.NoBody}}
	result := svc.AutoProbe(context.Background(), 1, driver, "m", "billing/m")

	assert.True(t, repo.successCalled)
	assert.False(t, repo.failureCalled)
	assert.True(t, result.OverallOK)
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
