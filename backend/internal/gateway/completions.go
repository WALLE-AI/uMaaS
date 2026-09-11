package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
	"github.com/WALLE-AI/uMaaS/backend/internal/protocol/openai"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
	"github.com/WALLE-AI/uMaaS/backend/internal/requestlog"
)

// UpstreamDoer 是 openaicompat.Driver 用得到的那部分接口，方便测试用假上游替换。
type UpstreamDoer interface {
	BuildRequest(ctx context.Context, req *ir.Request, upstreamModel string) (*http.Request, error)
	Do(httpReq *http.Request) (*http.Response, error)
}

// RequestLogger 是 request_logs 的写入端口。CompletionsHandler 只依赖这个
// 窄接口而不是具体的 *requestlog.Writer，方便测试用一个丢弃实现替换。
type RequestLogger interface {
	Enqueue(requestlog.Entry)
}

// CompletionsHandler 实现 POST /v1/chat/completions（B3 + I4 的多渠道回退）。
//
// 请求进来后：解析模型 → 能力协商（negotiate.go）→ Router.Rank 排出一条
// 候选渠道链 → 依次尝试，遇到"安全窗口内"的失败换下一个候选，遇到
// "已经吐过内容"的失败据实上报并停止（UNIFIED-PROVIDER-INTERFACE.md §5.5/§7）。
type CompletionsHandler struct {
	Models ModelRepository
	Router *Router
	// NewDriver 是驱动工厂而不是一个固定的 Driver：每个候选渠道的
	// Profile 不同（不同的 base_url/凭证），必须按候选临时构造。
	NewDriver func(provider.Profile) UpstreamDoer
	Pricing   *pricing.Resolver
	Logs      RequestLogger

	FirstByteTimeout time.Duration
	StallTimeout     time.Duration
}

func (h *CompletionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := platform.RequestID(r.Context())
	if requestID == "" {
		requestID = platform.NewRequestID()
	}

	req, requestedModel, err := openai.DecodeChatRequest(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	providerSlug, modelSlug, err := splitModelID(requestedModel)
	if err != nil {
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", err.Error())
		return
	}
	resolved, err := h.Models.ResolveCallable(r.Context(), providerSlug, modelSlug)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeOpenAIError(w, http.StatusNotFound, "model_not_found",
				fmt.Sprintf("model %q does not exist or is not available", requestedModel))
			return
		}
		slog.ErrorContext(r.Context(), "resolve model failed", "error", err, "model", requestedModel)
		writeOpenAIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	// 能力协商在发出任何上游请求之前做——没有它，用户把请求从支持
	// 工具调用的模型换到不支持的模型，得到的是上游一个含义不明的 400
	// （UNIFIED §5.4）。这与渠道选择无关，任何渠道都救不了这个组合。
	if negErr := Negotiate(req, ManifestFromCapabilities(resolved.Capabilities)); negErr != nil {
		var ne *NegotiationError
		errors.As(negErr, &ne)
		writeOpenAIError(w, http.StatusUnprocessableEntity, "unsupported_capability", ne.Message)
		return
	}

	candidates, err := h.Router.Rank(r.Context(), resolved.ID, "")
	if err != nil {
		if errors.Is(err, ErrNoChannels) {
			writeOpenAIError(w, http.StatusServiceUnavailable, "no_channels_available",
				fmt.Sprintf("no channel currently serves model %q", requestedModel))
			return
		}
		slog.ErrorContext(r.Context(), "rank channels failed", "error", err, "model", requestedModel)
		writeOpenAIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	rec := &logRecorder{
		requestID: requestID, requestedModel: requestedModel,
		resolved: resolved, start: start,
	}

	if req.Stream {
		h.serveStreamWithFallback(w, r, candidates, req, requestedModel, rec)
		return
	}
	h.serveNonStreamWithFallback(w, r, candidates, req, requestedModel, rec)
}

// splitModelID 解析契约的 `provider/model` 形态（ARCHITECTURE.md §4.2）。
func splitModelID(id string) (provider, model string, err error) {
	i := strings.IndexByte(id, '/')
	if i <= 0 || i == len(id)-1 {
		return "", "", fmt.Errorf("model must be in the form \"provider/model\", got %q", id)
	}
	return id[:i], id[i+1:], nil
}

// ── 非流式路径：可以在候选之间安全回退 ───────────────────────────

// 每个候选渠道的尝试都单独落一条 request_logs（attempt_index 递增，
// is_final 只在真正收尾的那次为 true）——这是 request_logs 从 I3 起就是
// attempt 级表的原因（M2 的迁移注释）：没有它，一次请求先在渠道 A 上
// 失败、又在渠道 B 上成功，运维只能看到最后成功的那条，A 失败的原因、
// 它对 A 的健康后验产生了什么影响，全部无从查起。
func (h *CompletionsHandler) serveNonStreamWithFallback(
	w http.ResponseWriter, r *http.Request, candidates []Candidate,
	req *ir.Request, requestedModel string, rec *logRecorder,
) {
	for i, cand := range candidates {
		rec.attemptIndex = int32(i)
		rec.attemptStart = time.Now()
		rec.channelID, rec.channelName, rec.upstreamModel = &cand.ChannelID, cand.ChannelName, cand.UpstreamModel
		result, resp := h.attemptNonStream(r.Context(), cand, req, requestedModel, rec)
		isLast := i == len(candidates)-1

		if result.success {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if err := openai.EncodeResponse(w, resp, requestedModel); err != nil {
				slog.ErrorContext(r.Context(), "encode response failed", "error", err)
			}
			rec.usage(resp.Usage, resp.UsageSource)
			rec.finish(http.StatusOK, "", "")
			rec.isFinal = true
			h.emitLog(rec)
			return
		}

		final := !result.retry || isLast
		rec.finish(result.outcome.status, result.outcome.code, result.outcome.message)
		rec.isFinal = final
		h.emitLog(rec)
		if final {
			writeOpenAIError(w, result.outcome.status, result.outcome.code, result.outcome.message)
			return
		}
		rec.resetForNextAttempt()
	}
}

func (h *CompletionsHandler) attemptNonStream(
	parentCtx context.Context, cand Candidate, req *ir.Request, requestedModel string, rec *logRecorder,
) (tryResult, *ir.Response) {
	driver := h.NewDriver(cand.Profile)
	attemptStart := time.Now()

	// 非流式没有"部分内容已经吐给客户端"这回事，因此没有 stall 的概念——
	// 一个总预算覆盖整个来回，超过就是超时，不区分阶段。
	ctx, cancel := context.WithTimeout(parentCtx, h.FirstByteTimeout+h.StallTimeout)
	defer cancel()

	httpReq, err := driver.BuildRequest(ctx, req, cand.UpstreamModel)
	if err != nil {
		return tryResult{outcome: retryOutcome{status: http.StatusInternalServerError,
			code: "internal_error", message: "internal server error"}}, nil
	}

	httpResp, err := driver.Do(httpReq)
	if err != nil {
		outcome := classifyAttemptError(err, ctx.Err())
		recordOutcome(parentCtx, h.Router.Repo, cand.ChannelID, tryResult{outcome: outcome})
		return tryResult{retry: outcome.retryable, outcome: outcome}, nil
	}
	defer httpResp.Body.Close()

	resp, err := openai.DecodeResponse(httpResp.Body, requestedModel)
	if err != nil {
		outcome := retryOutcome{status: http.StatusBadGateway, code: "invalid_upstream_response", message: err.Error()}
		recordOutcome(parentCtx, h.Router.Repo, cand.ChannelID, tryResult{outcome: outcome})
		// 上游返回了 2xx 但响应体解析不出来：这通常是"这家其实不是真正
		// 的 OpenAI 兼容"，换渠道有意义，所以标可重试。
		return tryResult{retry: true, outcome: outcome}, nil
	}

	latency := float64(time.Since(attemptStart).Milliseconds())
	result := tryResult{success: true, latencyMs: latency}
	recordOutcome(parentCtx, h.Router.Repo, cand.ChannelID, result)
	return result, resp
}

// ── 流式路径：首字节前可以回退，之后绝不可以 ──────────────────────

func (h *CompletionsHandler) serveStreamWithFallback(
	w http.ResponseWriter, r *http.Request, candidates []Candidate,
	req *ir.Request, requestedModel string, rec *logRecorder,
) {
	for i, cand := range candidates {
		rec.attemptIndex = int32(i)
		rec.attemptStart = time.Now()
		rec.channelID, rec.channelName, rec.upstreamModel = &cand.ChannelID, cand.ChannelName, cand.UpstreamModel
		isLast := i == len(candidates)-1
		written, retry, outcome := h.attemptStream(w, r, cand, req, requestedModel, rec, isLast)
		if written {
			// 已经向客户端写过内容：无论最终是正常结束还是 partial_failure，
			// 都不能再换渠道重试——这就是"流式已发出首个 chunk 后失败，
			// 绝对不可以重试"这条边界（§5.5/§7）。attemptStream 内部
			// 已经完成了收尾（写 [DONE] 或 EventError）与日志记录，
			// 这里直接返回。
			return
		}
		final := !retry || isLast
		if final {
			writeOpenAIError(w, outcome.status, outcome.code, outcome.message)
			return
		}
		rec.resetForNextAttempt()
	}
}

// attemptStream 尝试一个候选渠道的流式请求。written=true 表示已经向
// 客户端写过至少一个字节——从这一刻起调用方绝不能再换渠道，也不能
// 再修改状态码。**每一次尝试都会落一条 request_logs**（is_final 由
// 这次尝试是否终结整条请求决定），不只是最后成功/失败的那一次。
func (h *CompletionsHandler) attemptStream(
	w http.ResponseWriter, r *http.Request, cand Candidate, req *ir.Request, requestedModel string,
	rec *logRecorder, isLastCandidate bool,
) (written bool, retry bool, outcome retryOutcome) {
	driver := h.NewDriver(cand.Profile)
	upstreamCtx, cancelUpstream := context.WithCancel(r.Context())
	defer cancelUpstream()

	httpReq, err := driver.BuildRequest(upstreamCtx, req, cand.UpstreamModel)
	if err != nil {
		return false, false, retryOutcome{status: http.StatusInternalServerError,
			code: "internal_error", message: "internal server error"}
	}

	wd := newWatchdog(cancelUpstream, h.FirstByteTimeout)
	defer wd.stop()

	attemptStart := time.Now()
	httpResp, err := driver.Do(httpReq)
	if err != nil {
		// **首字节前不向下游写任何东西**（UNIFIED §7 第 2 条）：还没碰过
		// w，可以干净地把这次失败归类为"要不要换渠道"。
		o := classifyAttemptError(err, upstreamCtx.Err())
		if wd.timedOutBeforeFirstByte() {
			o = retryOutcome{retryable: true, status: http.StatusGatewayTimeout,
				code: "upstream_timeout", message: "upstream did not respond before first_byte_timeout"}
		}
		recordOutcome(r.Context(), h.Router.Repo, cand.ChannelID, tryResult{outcome: o})
		final := !o.retryable || isLastCandidate
		rec.finish(o.status, o.code, o.message)
		rec.isFinal = final
		h.emitLog(rec)
		return false, o.retryable, o
	}
	defer httpResp.Body.Close()

	reader := openai.NewStreamReader(httpResp.Body, req.StreamUsage)
	var writer *openai.StreamWriter
	var flusher streamFlusher

	for {
		ev, nextErr := reader.Next()
		wd.tick(h.StallTimeout)

		if ev != nil {
			if !written {
				written = true
				rec.ttft(time.Since(rec.attemptStart))
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				writer = openai.NewStreamWriter(w, requestedModel)
				flusher = newStreamFlusher(w)
			}
			if writeErr := writer.Write(ev); writeErr != nil {
				recordOutcome(r.Context(), h.Router.Repo, cand.ChannelID,
					tryResult{success: true, latencyMs: float64(time.Since(attemptStart).Milliseconds())})
				rec.finish(0, "client_disconnect", "client disconnected mid-stream")
				rec.isFinal = true
				h.emitLog(rec)
				return true, false, retryOutcome{}
			}
			flusher.Flush()
			if md, ok := ev.(ir.EventMessageDelta); ok && md.Usage != nil {
				rec.usage(*md.Usage, md.UsageSource)
			}
		}

		if nextErr == nil {
			continue
		}
		if errors.Is(nextErr, openai.ErrStreamDone) {
			recordOutcome(r.Context(), h.Router.Repo, cand.ChannelID,
				tryResult{success: true, latencyMs: float64(time.Since(attemptStart).Milliseconds())})
			rec.finish(http.StatusOK, "", "")
			rec.isFinal = true
			h.emitLog(rec)
			return true, false, retryOutcome{}
		}

		// 读上游出了错：可能是我们自己的 watchdog 触发的，也可能是真实
		// 的网络故障。**据实上报，绝不假装成功**（ARCHITECTURE.md §6.2）。
		o := h.classifyStreamFailure(written, upstreamCtx, wd)
		recordOutcome(r.Context(), h.Router.Repo, cand.ChannelID, tryResult{outcome: o})

		if !written {
			return false, o.retryable, o
		}
		_ = writer.Write(ir.EventError{Code: o.code, Message: o.message})
		flusher.Flush()
		rec.finish(http.StatusOK, "partial_failure", o.message)
		rec.isFinal = true
		h.emitLog(rec)
		return true, false, retryOutcome{}
	}
}

func (h *CompletionsHandler) classifyStreamFailure(written bool, upstreamCtx context.Context, wd *watchdog) retryOutcome {
	if wd.timedOutBeforeFirstByte() || (!written && upstreamCtx.Err() != nil) {
		return retryOutcome{retryable: true, status: http.StatusGatewayTimeout,
			code: "upstream_timeout", message: "upstream stopped responding before sending any content"}
	}
	if upstreamCtx.Err() != nil {
		return retryOutcome{status: http.StatusOK, code: "stall_timeout",
			message: "upstream stopped sending data mid-stream"}
	}
	return retryOutcome{retryable: !written, status: http.StatusBadGateway,
		code: "upstream_error", message: "the upstream connection failed"}
}

// ── 输出 ────────────────────────────────────────────────────────

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := map[string]any{"error": map[string]any{
		"message": message, "type": "invalid_request_error", "code": code,
	}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encode openai error failed", "error", err)
	}
}

// streamFlusher 包一层 http.ResponseController。
//
// 用 ResponseController 而不是类型断言 http.Flusher：前者是 Go 1.20+
// 推荐的方式，对经过若干层中间件包装的 ResponseWriter 更稳妥
// （ARCHITECTURE.md §6.1）。**禁止在中间做缓冲**：任何一层不 flush
// 就会把流式变成"慢的非流式"。
type streamFlusher struct{ rc *http.ResponseController }

func (f streamFlusher) Flush() { _ = f.rc.Flush() }

func newStreamFlusher(w http.ResponseWriter) streamFlusher {
	return streamFlusher{rc: http.NewResponseController(w)}
}

// ── 日志记录（M2）──────────────────────────────────────────────

// logRecorder 攒齐一条 request_logs 记录需要的字段，在请求结束时统一提交。
type logRecorder struct {
	requestID      string
	requestedModel string
	resolved       *ResolvedModel
	channelID      *int64
	channelName    string
	upstreamModel  string
	start          time.Time

	// attemptIndex/isFinal 支持一次请求跨多个候选渠道产生多条
	// request_logs（M2 的 attempt 级设计）。attemptStart 是这一次
	// 尝试自己的起点，与 start（整条请求的起点）分开——多候选回退时，
	// latency_ms 应该反映"这一次尝试花了多久"，不是"从请求进来到现在"。
	attemptIndex int32
	isFinal      bool
	attemptStart time.Time

	ttftMs *int32
	u      ir.Usage
	source ir.UsageSource

	status    int
	errorCode string
	errorMsg  string
}

// resetForNextAttempt 清掉只属于上一次尝试的字段，避免一个换渠道后的
// 新尝试继承上一次的 usage/ttft——那会让日志显示"这次尝试收到了它
// 从未收到过的 token"。requestID/requestedModel/resolved/start 这些
// 整条请求级别的字段不动。
func (r *logRecorder) resetForNextAttempt() {
	r.ttftMs = nil
	r.u = ir.Usage{}
	r.source = ""
	r.status = 0
	r.errorCode = ""
	r.errorMsg = ""
}

func (r *logRecorder) ttft(d time.Duration) {
	ms := int32(d.Milliseconds())
	r.ttftMs = &ms
}

func (r *logRecorder) usage(u ir.Usage, source ir.UsageSource) {
	r.u = u
	r.source = source
}

func (r *logRecorder) finish(status int, errorCode, errorMsg string) {
	r.status = status
	r.errorCode = errorCode
	r.errorMsg = errorMsg
}

// emitLog 把攒好的记录交给异步写入器。
//
// 成本/售价的计算走**与结算完全相同的 pricing.Version.Charge**——I3/I4 还
// 没有预扣/结算（M3 在 I5），这里算出来的钱不会真的从任何人账户划走，
// 纯粹是为了 request_logs 从第一天起就有真实数字可查（M2 的判据）。
func (h *CompletionsHandler) emitLog(rec *logRecorder) {
	source := rec.source
	if source == "" {
		source = ir.UsageEstimated
	}
	entry := requestlog.Entry{
		RequestID: rec.requestID, AttemptIndex: rec.attemptIndex, IsFinal: rec.isFinal,
		RequestedModel: rec.requestedModel, Origin: "user",
		PromptTokens: rec.u.PromptTokens, CompletionTokens: rec.u.CompletionTokens,
		CacheReadTokens: rec.u.CacheReadTokens, CacheWriteTokens: rec.u.CacheWriteTokens,
		ReasoningTokens: rec.u.ReasoningTokens, ToolCallCount: rec.u.ToolCallCount,
		UsageSource: string(source),
		TTFTMs:      rec.ttftMs,
		LatencyMs:   int32(time.Since(rec.attemptStart).Milliseconds()),
		StatusCode:  int32(rec.status), ErrorCode: rec.errorCode,
		ChannelID: rec.channelID, ChannelName: rec.channelName, UpstreamModel: rec.upstreamModel,
	}
	if rec.resolved != nil {
		entry.BillingModel = rec.resolved.BillingModel
		entry.CostEstimated = rec.resolved.Hosting == "self"
	} else {
		entry.BillingModel = rec.requestedModel
	}

	if h.Pricing != nil && rec.resolved != nil && (entry.PromptTokens > 0 || entry.CompletionTokens > 0) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		version, err := h.Pricing.ForBilling(ctx, pricing.Key{
			BillingModel: rec.resolved.BillingModel, FallbackBillingModel: rec.requestedModel,
		}, rec.start)
		cancel()
		if err == nil {
			nano, _, chargeErr := version.Charge(pricing.Usage{
				InputTokens: entry.PromptTokens, OutputTokens: entry.CompletionTokens,
				CacheReadTokens: entry.CacheReadTokens, CacheWriteTokens: entry.CacheWriteTokens,
				ReasoningTokens: entry.ReasoningTokens, Requests: 1,
			})
			if chargeErr == nil {
				id := version.ID
				entry.PriceVersionID = &id
				entry.ChargedAmountNano = nano
				// I4 还没有渠道成本归集（那要等 I7/I8 的自建摊销与云端账单
				// 对账），上游成本先按售价的近似值兜底——这不是账单口径，
				// 只是让分析页不至于对每条记录都显示"暂无成本"。
				entry.UpstreamCostNano = nano
			}
		}
	}

	h.Logs.Enqueue(entry)
}
