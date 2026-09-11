// Package openaicompat 是一个驱动服务约 30 家 OpenAI 兼容厂商的落点
// （UNIFIED-PROVIDER-INTERFACE.md §4.4/§8）。I3 只用它对接单一配置的上游。
package openaicompat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
	"github.com/WALLE-AI/uMaaS/backend/internal/protocol/openai"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
)

// Driver 按能力实现 UNIFIED §8 的接口切分：这里只实现 chat，
// 因此只需要 BuildRequest + ParseChat/ParseStream 两组方法，
// 不需要伪造 ConvertImageRequest 之类用不到的方法。
type Driver struct {
	profile provider.Profile
	client  *http.Client
}

func New(profile provider.Profile, client *http.Client) *Driver {
	return &Driver{profile: profile, client: client}
}

func (d *Driver) Profile() provider.Profile { return d.profile }

// BuildRequest 把 IR 编成一次到上游的 *http.Request。
//
// **不在这里发送**：调用方（网关）需要先建好请求再决定怎么读响应
// （流式/非流式的读取方式完全不同），职责必须分开。
func (d *Driver) BuildRequest(ctx context.Context, req *ir.Request, upstreamModel string) (*http.Request, error) {
	if req.Stream && !d.profile.Quirks.NoStreamUsage {
		req.StreamUsage = true
	} else if d.profile.Quirks.NoStreamUsage {
		req.StreamUsage = false
	}
	if d.profile.Quirks.MaxTokensRequired && req.MaxTokens == nil {
		def := int64(4096)
		req.MaxTokens = &def
	}

	body, err := openai.EncodeUpstreamRequest(req, upstreamModel)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.profile.BaseURL+provider.ChatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openaicompat: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	switch d.profile.AuthScheme {
	case provider.AuthBearer:
		httpReq.Header.Set("Authorization", "Bearer "+d.profile.APIKey)
	case provider.AuthHeader:
		httpReq.Header.Set(d.profile.AuthHeader, d.profile.APIKey)
	}
	return httpReq, nil
}

// Do 发出请求。它只做发送——**不读响应体**，因为流式与非流式的读取
// 策略（是否要两段超时、要不要立刻整体读完）完全不同，属于调用方
// （网关）该决定的事，不该塞进驱动里变成一个隐藏分支。
func (d *Driver) Do(httpReq *http.Request) (*http.Response, error) {
	resp, err := d.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: body}
	}
	return resp, nil
}

// UpstreamError 是上游返回非 2xx 时的错误。保留状态码与截断的响应体，
// 供网关翻译成客户端可读的错误，而不是把 500 一律说成"内部错误"——
// 上游的 429/401 对客户端是有意义的信息。
type UpstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("openaicompat: upstream returned %d: %s", e.StatusCode, truncate(e.Body, 500))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
