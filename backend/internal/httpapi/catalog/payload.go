// Package catalog 是目录七个只读端点的 HTTP 层（B2）。
//
// 这一层只做形状转换与缓存头，业务组装在 internal/catalog。
package catalog

import (
	"time"

	catalogsvc "github.com/WALLE-AI/uMaaS/backend/internal/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

type providerPayload struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	LogoURL    string `json:"logo_url"`
	ModelCount int    `json:"model_count"`
}

// pricingPayload 是契约的 ModelPricing（计价 附 1.2）。
//
// 三态必须能区分，这是整份契约改动的核心：
//
//	字段缺省       = 该模型没有这个计价单元
//	字段为 null    = 价格未知（**不等于免费**，UI 显示「—」而非 $0）
//	字段为数字 0   = 真的免费
//
// 旧契约只有 input/output 两个字段，于是前端只能自己发明价格——
// `App.tsx` 里的 `input × 0.1` 就是这么来的，而各厂商的缓存折扣实际
// 从 10% 到 50% 不等（附 1.1-①）。
type pricingPayload struct {
	Currency            string              `json:"currency"`
	InputPerMillion     *float64            `json:"input_per_million"`
	OutputPerMillion    *float64            `json:"output_per_million"`
	CacheReadPerMillion *float64            `json:"cache_read_per_million,omitempty"`
	CacheWrite          []cacheWritePayload `json:"cache_write,omitempty"`
	ReasoningPerMillion *float64            `json:"reasoning_per_million,omitempty"`
	RequestFixed        *float64            `json:"request_fixed,omitempty"`
	Tiers               []tierPayload       `json:"tiers,omitempty"`
	EffectiveFrom       *string             `json:"effective_from,omitempty"`
}

type cacheWritePayload struct {
	TTL        string  `json:"ttl"`
	PerMillion float64 `json:"per_million"`
}

type tierPayload struct {
	AboveTokens      int64   `json:"above_tokens"`
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// toPricingPayload 把解析好的价目版本转成契约形状。
//
// **契约暴露解析后的有效价格，不暴露内部表达**（附 1.2-1）：
// 存储里 `{"reasoning": {"same_as": "output"}}` 这种写法在这里必须已经
// 变成一个数字，否则每个客户端都要实现一遍解析规则，迟早出现两种算法。
func toPricingPayload(v *pricing.Version) pricingPayload {
	// 没有价目：currency 仍给 USD（契约必填），两个价格为 null。
	// 这正是"未知"与"免费"的区分点。
	if v == nil || v.Rates == nil {
		return pricingPayload{Currency: "USD"}
	}
	p := pricingPayload{Currency: v.Currency}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitInput); ok {
		p.InputPerMillion = usdPtr(nano)
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitOutput); ok {
		p.OutputPerMillion = usdPtr(nano)
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitCacheRead); ok {
		p.CacheReadPerMillion = usdPtr(nano)
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitReasoning); ok {
		p.ReasoningPerMillion = usdPtr(nano)
	}
	for _, cw := range v.Rates.CacheWriteTiers() {
		p.CacheWrite = append(p.CacheWrite, cacheWritePayload{
			TTL: cw.TTL, PerMillion: pricing.PerMillionUSD(cw.PerMillionNano),
		})
	}
	for _, t := range v.Rates.Tiers() {
		p.Tiers = append(p.Tiers, tierPayload{
			AboveTokens:      t.AboveTokens,
			InputPerMillion:  pricing.PerMillionUSD(t.InputPerMillionNano),
			OutputPerMillion: pricing.PerMillionUSD(t.OutputPerMillionNano),
		})
	}
	// effective_from 让 UI 能在改价窗口内标出"价格更新于 X"。
	// catalog 的缓存策略是 max-age=60，用户看到一个价格却按另一个价格
	// 被收费是最难解释的一类工单（附 1.2 末）。
	if !v.EffectiveFrom.IsZero() {
		s := v.EffectiveFrom.UTC().Format(time.RFC3339)
		p.EffectiveFrom = &s
	}
	return p
}

func usdPtr(nano int64) *float64 {
	v := pricing.PerMillionUSD(nano)
	return &v
}

type performancePayload struct {
	ThroughputTokensPerSecond *float64 `json:"throughput_tokens_per_second"`
	QualityScore              *float64 `json:"quality_score"`
}

type modelSummaryPayload struct {
	ID             string             `json:"id"`
	Slug           string             `json:"slug"`
	Name           string             `json:"name"`
	Provider       providerPayload    `json:"provider"`
	Description    string             `json:"description"`
	LogoURL        string             `json:"logo_url"`
	Modalities     []string           `json:"modalities"`
	Capabilities   []string           `json:"capabilities"`
	ContextLength  *int32             `json:"context_length"`
	Pricing        pricingPayload     `json:"pricing"`
	Performance    performancePayload `json:"performance"`
	UsageTokens30d int64              `json:"usage_tokens_30d"`
	ReleasedAt     string             `json:"released_at"`
}

func toSummary(m catalogsvc.Model) modelSummaryPayload {
	return modelSummaryPayload{
		ID:   m.PublicID(),
		Slug: m.Slug,
		Name: m.Name,
		Provider: providerPayload{
			ID: m.Provider.Slug, Name: m.Provider.Name,
			Slug: m.Provider.Slug, LogoURL: m.Provider.LogoURL,
			ModelCount: int(m.Provider.ModelCount),
		},
		Description:   m.Description,
		LogoURL:       m.LogoURL,
		Modalities:    emptyIfNil(m.Modalities),
		Capabilities:  emptyIfNil(m.Capabilities),
		ContextLength: m.ContextLength,
		Pricing:       toPricingPayload(m.Price),
		Performance: performancePayload{
			ThroughputTokensPerSecond: m.Throughput,
			QualityScore:              m.QualityScore,
		},
		UsageTokens30d: m.Tokens30d,
		ReleasedAt:     m.ReleasedAt.UTC().Format(time.RFC3339),
	}
}

func toSummaries(models []catalogsvc.Model) []modelSummaryPayload {
	out := make([]modelSummaryPayload, 0, len(models))
	for _, m := range models {
		out = append(out, toSummary(m))
	}
	return out
}

type modelDetailPayload struct {
	modelSummaryPayload
	Architecture        *string                 `json:"architecture"`
	InputFormats        []string                `json:"input_formats"`
	OutputFormats       []string                `json:"output_formats"`
	SupportedParameters []string                `json:"supported_parameters"`
	Endpoints           []endpointPayload       `json:"endpoints"`
	Uptime              []timeSeriesPoint       `json:"uptime"`
	BenchmarkScores     []benchmarkScorePayload `json:"benchmark_scores"`
	TopApps             []appUsagePayload       `json:"top_apps"`
	RecentActivity      []activityPayload       `json:"recent_activity"`
	FAQ                 []faqPayload            `json:"faq"`
	RelatedModels       []modelSummaryPayload   `json:"related_models"`
}

// endpointPayload 的价格字段已按 附 1.1-② 改名。
//
// 旧名字 `input_per_million` 字面读法是"不同端点价格不同、用户按选中的端点付费"，
// 那是 OpenRouter 的模型，**不是本方案的模型**——售价锚定 Billing 名、
// 不随路由波动（§4.2）。留着它有真实价值（用户能看到我们把请求路由到了更便宜的
// 供给），但名字必须写明它不是计费依据。
type endpointPayload struct {
	ID                          string          `json:"id"`
	Provider                    providerPayload `json:"provider"`
	Region                      string          `json:"region"`
	LatencyMsP50                float64         `json:"latency_ms_p50"`
	LatencyMsP95                float64         `json:"latency_ms_p95"`
	ThroughputTokensPerSecond   float64         `json:"throughput_tokens_per_second"`
	UpstreamListPriceInputPerM  *float64        `json:"upstream_list_price_input_per_million,omitempty"`
	UpstreamListPriceOutputPerM *float64        `json:"upstream_list_price_output_per_million,omitempty"`
	Uptime30d                   float64         `json:"uptime_30d"`
	Status                      string          `json:"status"`
	SupportsZeroDataRetention   bool            `json:"supports_zero_data_retention"`
}

type timeSeriesPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type benchmarkScorePayload struct {
	BenchmarkID   string   `json:"benchmark_id"`
	BenchmarkName string   `json:"benchmark_name"`
	Score         float64  `json:"score"`
	Percentile    *float64 `json:"percentile,omitempty"`
}

type appUsagePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LogoURL     string `json:"logo_url,omitempty"`
	UsageTokens int64  `json:"usage_tokens"`
}

type activityPayload struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	OccurredAt string `json:"occurred_at"`
}

type faqPayload struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
