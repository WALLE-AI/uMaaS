package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
	"github.com/WALLE-AI/uMaaS/backend/internal/protocol/openai"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider/openaicompat"
	"github.com/WALLE-AI/uMaaS/backend/internal/requestlog"
)

// ProbeCheck 是连通性测试的一个检查项（UNIFIED-PROVIDER-INTERFACE.md §5.6）。
//
// **返回分项结果，不是一个 bool**：合成一个 `ok` 会把"连通但鉴权失败"
// 与"鉴权通过但模型不存在"这两种完全不同的运维动作在服务端就丢掉，
// 然后指望运维从一句 detail 字符串里读出来。
type ProbeCheck struct {
	Name   string `json:"name"` // connectivity | auth | model | parse
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type ProbeResult struct {
	Channel   int64        `json:"channel_id"`
	Checks    []ProbeCheck `json:"checks"`
	LatencyMs float64      `json:"latency_ms"`
	OverallOK bool         `json:"overall_ok"`
}

// probeRequest 是探测请求的固定形态（§5.6-②），四个参数都不是可选的：
//   - max_tokens=1：最省钱
//   - stream=false：流式会把首字节窗口与 stall 两套超时也拖进来，测试不需要
//   - 短超时：比生产的 firstByteTimeout 更短，测试要快速给结论
//   - 不重试：测的就是这一个渠道，不走回退链
func probeRequest() *ir.Request {
	maxTokens := int64(1)
	return &ir.Request{
		Messages:  []ir.Message{ir.Text(ir.RoleUser, "ping")},
		MaxTokens: &maxTokens,
		Stream:    false,
	}
}

const probeTimeout = 10 * time.Second

// runProbe 是连通性测试与自动健康探针共用的**底层 HTTP 机制**。
//
// 共用的只是"怎么发一次探测请求、怎么把响应分类成四项检查"——这部分
// 是纯 I/O，没有业务判断。**真正有分歧的两件事（写不写健康状态机、
// 记账 origin 是什么）分别在 ManualTest 与 AutoProbe 两个独立函数里做**，
// 不共用一个 isManual 参数：那个参数迟早会被传错（§5.6-④ 末段）。
func runProbe(ctx context.Context, driver UpstreamDoer, upstreamModel string) (ProbeResult, ir.Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	start := time.Now()
	req := probeRequest()
	httpReq, err := driver.BuildRequest(ctx, req, upstreamModel)
	if err != nil {
		return ProbeResult{Checks: []ProbeCheck{
			{Name: "connectivity", Passed: false, Detail: err.Error()},
		}}, ir.Usage{}, err
	}

	httpResp, err := driver.Do(httpReq)
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return classifyProbeError(err, latency), ir.Usage{}, nil
	}
	defer httpResp.Body.Close()

	resp, err := decodeProbeResponse(httpResp)
	if err != nil {
		return ProbeResult{
			LatencyMs: latency,
			Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: true},
				{Name: "model", Passed: true},
				{Name: "parse", Passed: false, Detail: err.Error()},
			},
		}, ir.Usage{}, nil
	}

	return ProbeResult{
		LatencyMs: latency, OverallOK: true,
		Checks: []ProbeCheck{
			{Name: "connectivity", Passed: true},
			{Name: "auth", Passed: true},
			{Name: "model", Passed: true},
			{Name: "parse", Passed: true},
		},
	}, resp.Usage, nil
}

// decodeProbeResponse 复用 protocol/openai 的解析器——协议解析的正确性
// 已经在那个包的测试里覆盖过，这里不重新测一遍，只测"分类成四项检查"
// 这一步自己的逻辑。走一个可替换的变量是为了让探针测试能注入一个假
// 响应，而不必构造一个真实、可被 http.Response.Body 读出来的 io.Reader。
var probeDecodeOverride = openai.DecodeResponse

func decodeProbeResponse(httpResp *http.Response) (*ir.Response, error) {
	return probeDecodeOverride(httpResp.Body, "")
}

// classifyProbeError 把驱动层错误映射到四项检查（§5.6-② 的表）。
//
// **402 与 403 分开处理**：402 是"额度问题但凭证正常"，运维该去充值；
// 403 的结论不确定，运维不该轻举妄动地换 key（§2.3 的三分法在这里复用）。
func classifyProbeError(err error, latencyMs float64) ProbeResult {
	var upstreamErr *openaicompat.UpstreamError
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.StatusCode {
		case http.StatusUnauthorized:
			return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: false, Detail: "401: credentials are invalid, rotate the key"},
			}}
		case http.StatusPaymentRequired:
			return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: false, Detail: "402: credentials are valid but out of quota, top up"},
			}}
		case http.StatusForbidden:
			return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: false, Detail: "403: inconclusive, do not rotate the key blindly"},
			}}
		case http.StatusNotFound:
			return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: true},
				{Name: "model", Passed: false, Detail: "404: check channel_models.upstream_model_name against the upstream's actual name"},
			}}
		default:
			return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
				{Name: "connectivity", Passed: true},
				{Name: "auth", Passed: true},
				{Name: "model", Passed: true},
				{Name: "parse", Passed: false, Detail: upstreamErr.Error()},
			}}
		}
	}
	return ProbeResult{LatencyMs: latencyMs, Checks: []ProbeCheck{
		{Name: "connectivity", Passed: false, Detail: err.Error()},
	}}
}

// ProbeService 组装 X10（手工测试）与 B4（自动探针）两条独立路径。
//
// 两个方法都接受已经建好的 driver（调用方按候选渠道的 Profile 构造），
// 不在这里持有一个驱动工厂——探测总是针对一个**已经确定**的渠道，
// 不需要 Router 那套评分与候选筛选。
type ProbeService struct {
	Repo RouteRepository
	Logs RequestLogger
}

// ManualTest 实现 X10：admin 主动发起的连通性测试。
//
// **结果只返回给操作者与审计日志，绝不写入健康状态机**——手工测试的
// 时机通常是"已经怀疑这个渠道有问题"，把这些样本喂进 Beta 后验等于
// 让运维的怀疑自我实现；且连点几次测试按钮就能把一个健康渠道的
// 后验拉低，改变真实流量的去向，一个只读诊断动作不该有这种副作用
// （§5.6-④）。因此这个函数**不调用 Repo.RecordSuccess/RecordFailure**。
func (s *ProbeService) ManualTest(ctx context.Context, channelID int64, driver UpstreamDoer, upstreamModel, billingModel string) (ProbeResult, error) {
	result, usage, err := runProbe(ctx, driver, upstreamModel)
	result.Channel = channelID
	if err != nil {
		return result, err
	}
	// 费用可见但不进任何客户账单：workspace_id=NULL、charged_amount=0，
	// upstream_cost 照实记（计价方案 §4.3 最后一行）。
	s.emitProbeLog(channelID, upstreamModel, billingModel, "admin_probe", usage, result)
	return result, nil
}

// AutoProbe 实现 B4 的自动健康探针：**会**写入健康状态机，也会更新
// 探针退休间隔（连续失败越多，下一次探测拖得越久，避免对已死的上游
// 持续打无效请求）。
func (s *ProbeService) AutoProbe(ctx context.Context, channelID int64, driver UpstreamDoer, upstreamModel, billingModel string) ProbeResult {
	result, usage, err := runProbe(ctx, driver, upstreamModel)
	result.Channel = channelID
	if err != nil || !result.OverallOK {
		_ = s.Repo.RecordFailure(ctx, channelID, false, rateLimitBackoffSeconds)
	} else {
		_ = s.Repo.RecordSuccess(ctx, channelID, result.LatencyMs)
	}
	s.emitProbeLog(channelID, upstreamModel, billingModel, "health_probe", usage, result)
	return result
}

func (s *ProbeService) emitProbeLog(channelID int64, upstreamModel, billingModel, origin string, usage ir.Usage, result ProbeResult) {
	if s.Logs == nil {
		return
	}
	status := int32(http.StatusOK)
	errorCode := ""
	if !result.OverallOK {
		status = http.StatusBadGateway
		for _, c := range result.Checks {
			if !c.Passed {
				errorCode = c.Name + "_check_failed"
				break
			}
		}
	}
	s.Logs.Enqueue(requestlog.Entry{
		RequestID: "probe-" + time.Now().UTC().Format(time.RFC3339Nano),
		IsFinal:   true, Origin: origin,
		BillingModel: billingModel, UpstreamModel: upstreamModel,
		ChannelID: &channelID,
		// origin != user 时 charged_amount 恒为 0——探测不属于任何客户，
		// 但 upstream_cost 照实记，否则渠道测试的费用会变成上游账单里
		// 对不上的一块（计价方案 §11-F 的差异率监控会因此持续误报）。
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		UsageSource: string(ir.UsageUpstream),
		LatencyMs:   int32(result.LatencyMs), StatusCode: status, ErrorCode: errorCode,
	})
}
