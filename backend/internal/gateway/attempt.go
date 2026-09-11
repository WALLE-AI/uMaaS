package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider/openaicompat"
)

// retryOutcome 是一次尝试失败后的分类结果，决定 attempt 循环要不要
// 换下一个候选渠道（UNIFIED-PROVIDER-INTERFACE.md §5.5 的边界条件表）。
type retryOutcome struct {
	retryable   bool
	rateLimited bool
	status      int
	code        string
	message     string
}

// classifyAttemptError 把驱动层的错误分类成"可以换渠道重试"还是"直接返回"。
//
// 边界条件表（§5.5）：
//
//	连接失败 / DNS / 超时（未发出）    ✅ 可以
//	上游 5xx                          ✅ 可以
//	上游 429                          ✅ 换渠道，且给当前渠道降权
//	上游 4xx（参数错误）                ❌ 换了也一样错，直接返回
//	流式已发出首个 chunk 后失败         ❌ 绝对不可以（不在这个函数的职责内，
//	                                     由调用方在写出第一个字节之后就不再
//	                                     调用这个函数——见 completions.go）
//
// **本函数比原表多做一步区分**：401/403/404 虽然是 4xx，但通常是
// "这个渠道的凭证或上游模型映射配错了"，而不是"客户端的请求本身有问题"
// ——换一个渠道很可能就通，直接返回等于让一个渠道配置错误变成用户可见
// 的故障。真正"换了也一样错"的是 400/422 这类请求体本身不合法的错误。
func classifyAttemptError(err error, ctxErr error) retryOutcome {
	var upstreamErr *openaicompat.UpstreamError
	if errors.As(err, &upstreamErr) {
		status := upstreamErr.StatusCode
		switch {
		case status == http.StatusTooManyRequests:
			return retryOutcome{retryable: true, rateLimited: true, status: status,
				code: "rate_limited", message: upstreamErr.Error()}
		case status >= 500:
			return retryOutcome{retryable: true, status: status,
				code: "upstream_error", message: upstreamErr.Error()}
		case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
			return retryOutcome{retryable: true, status: status,
				code: "channel_misconfigured", message: upstreamErr.Error()}
		default:
			// 400/422 等：请求体本身的问题，换渠道也一样错。
			return retryOutcome{retryable: false, status: status,
				code: "invalid_request_error", message: upstreamErr.Error()}
		}
	}
	if ctxErr != nil {
		return retryOutcome{retryable: true, status: http.StatusGatewayTimeout,
			code: "upstream_timeout", message: "upstream did not respond before first_byte_timeout"}
	}
	// 连接失败 / DNS：未发出，安全窗口内，可以换渠道。
	return retryOutcome{retryable: true, status: http.StatusBadGateway,
		code: "upstream_unreachable", message: err.Error()}
}

// rateLimitBackoffSeconds 是 429 时压低渠道评分的窗口。60s 与
// RateLimitFactor 的 rampWindow 保持一致——两处如果不一致，评分在
// "刚好过了限流窗口"那一刻的行为会和记账的限流窗口对不上。
const rateLimitBackoffSeconds = 60

// tryCandidate 是一次尝试的返回结果，供 attempt 循环判断下一步。
type tryResult struct {
	success bool
	// retry 为 true 时调用方应当继续下一个候选；为 false 时必须停止
	// （或者是成功，或者是不可重试的失败，或者是已经开始向客户端写内容）。
	retry     bool
	outcome   retryOutcome
	latencyMs float64
}

func recordOutcome(ctx context.Context, repo RouteRepository, channelID int64, result tryResult) {
	if result.success {
		_ = repo.RecordSuccess(ctx, channelID, result.latencyMs)
		return
	}
	_ = repo.RecordFailure(ctx, channelID, result.outcome.rateLimited, rateLimitBackoffSeconds)
}
