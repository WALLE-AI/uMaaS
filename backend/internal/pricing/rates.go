// Package pricing 是价目的**唯一**解析与计算实现（BILLING-AND-PRICING.md §3）。
//
// # 为什么必须只有一份
//
// catalog 页面显示 `$0.15 / 1M`，用户按 `$0.15` 付费。这两个数字若来自不同的
// 解析路径，迟早会不一致，而这类不一致**用户一定先于我们发现**（§3.6-②）。
// 因此：
//
//   - "哪一版价目在某时刻生效" 只有一个定义——SQL 函数 pricing_effective_version()
//   - "这一版价目的费率是多少" 只有一个定义——本包的 ParseRates
//   - "这次调用要收多少钱" 只有一个定义——本包的 Price.Charge
//
// 展示侧与结算侧都只能走这三个入口。IMPLEMENTATION-PLAN.md 的 M1b 判据
// （"同一模型在 web 与结算侧取到同一个 price_version_id"）就是对这条的验收。
package pricing

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Unit 是计价单元（§3.1）。
//
// 这些**不是"以后再加的列"，是价目表的形状**：request_logs 早就埋了
// cache_read / cache_write / tool_call 的 token 数，而它们此前没有任何价格
// 可以对应——埋点跑在价目表前面，说明价目表没被设计过。
type Unit string

const (
	UnitInput      Unit = "input"
	UnitOutput     Unit = "output"
	UnitCacheRead  Unit = "cache_read"
	UnitCacheWrite Unit = "cache_write"
	UnitReasoning  Unit = "reasoning"
	// UnitRequestFixed 是每请求固定费，单位是 per_request_nano。
	UnitRequestFixed Unit = "request_fixed"
	// 以下三个单元本版不计算（首版只做 chat + embedding，UNIFIED §11-B），
	// 但**必须能被解析与校验**：价目录入时填了它们，不该被静默丢弃。
	UnitImagePerUnit   Unit = "image_per_unit"
	UnitAudioPerSecond Unit = "audio_per_second"
	UnitVideoPerSecond Unit = "video_per_second"
	UnitRerankPerDoc   Unit = "rerank_per_doc"
	UnitInputLongCtx   Unit = "input_long_context"
	UnitBatchDiscount  Unit = "batch_discount"
)

// knownUnits 用于拦住拼写错误。未知单元不是"忽略就好"——
// 把 `cache_read` 敲成 `cache-read` 的后果是缓存读取白送，而没有任何报错。
var knownUnits = map[Unit]bool{
	UnitInput: true, UnitOutput: true, UnitCacheRead: true, UnitCacheWrite: true,
	UnitReasoning: true, UnitRequestFixed: true, UnitImagePerUnit: true,
	UnitAudioPerSecond: true, UnitVideoPerSecond: true, UnitRerankPerDoc: true,
	UnitInputLongCtx: true, UnitBatchDiscount: true,
}

// Rate 是一个计价单元的费率。
//
// PerMillionNano 存的是"每百万单位的纳美元数"（§3.5）。用纳美元而不是微美元，
// 是因为 $0.15/1M = 1.5e-7 USD/token，比 1 微美元还小一个数量级——
// 用 micro 逐项相乘再取整，短请求（几十 token 的 embedding、1-token 的健康探针）
// 会被系统性截断到 0，**且偏差方向恒定**。那不是随机误差，是系统性少收。
type Rate struct {
	PerMillionNano int64  `json:"per_million_nano,omitempty"`
	PerRequestNano int64  `json:"per_request_nano,omitempty"`
	PerUnitNano    int64  `json:"per_unit_nano,omitempty"`
	TTL            string `json:"ttl,omitempty"`     // cache_write 的 5m / 1h
	SameAs         Unit   `json:"same_as,omitempty"` // 显式引用另一个单元
	// ThresholdTokens 只有 input_long_context 用得到：超过它就换档。
	ThresholdTokens int64   `json:"threshold_tokens,omitempty"`
	Factor          float64 `json:"factor,omitempty"` // batch_discount 用
}

// Tier 是分档价（长上下文）。选档看**输入 token 数**，
// 命中后 input/output 单价整体替换——Gemini 的超长上下文就是这么算的。
type Tier struct {
	AboveTokens          int64 `json:"above_tokens"`
	InputPerMillionNano  int64 `json:"input_per_million_nano"`
	OutputPerMillionNano int64 `json:"output_per_million_nano"`
}

// Rates 是一份完整价目的费率集合。
//
// 一个单元可以有多条 Rate（cache_write 的 5m / 1h 两档），因此值是切片。
type Rates struct {
	units map[Unit][]Rate
	tiers []Tier
	raw   json.RawMessage
}

// rawRates 是落库形态。每个单元既可以是一个对象，也可以是对象数组——
// Anthropic 的 cache_write 有 5m / 1h 两档不同价，硬塞进单对象就要另起字段名。
type rawRates map[string]json.RawMessage

// ParseRates 把 model_prices.rates 解析成可计算的费率集合。
//
// 三条语义在这里定死：
//
//   - **`same_as` 是显式的**（§3.2）。"推理 token 与 output 同价"要写出来，
//     而不是靠代码里的默认分支——否则某个厂商改成单列计价时，改动点找不到。
//   - **未声明的计价单元 = 不计费**，且用量明细里显示为 0 而非空。
//     "没配价"与"免费"在类型上要能区分：前者由 Validate 在提交时拦住。
//   - **未知单元名一律报错**，不做宽容解析。
func ParseRates(raw []byte) (*Rates, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("pricing: empty rates")
	}
	var rr rawRates
	if err := json.Unmarshal(raw, &rr); err != nil {
		return nil, fmt.Errorf("pricing: decode rates: %w", err)
	}

	out := &Rates{units: map[Unit][]Rate{}, raw: append(json.RawMessage(nil), raw...)}
	for name, value := range rr {
		if name == "tiers" {
			if err := json.Unmarshal(value, &out.tiers); err != nil {
				return nil, fmt.Errorf("pricing: decode tiers: %w", err)
			}
			sort.Slice(out.tiers, func(i, j int) bool {
				return out.tiers[i].AboveTokens < out.tiers[j].AboveTokens
			})
			continue
		}
		unit := Unit(name)
		if !knownUnits[unit] {
			return nil, fmt.Errorf("pricing: unknown pricing unit %q", name)
		}
		var list []Rate
		if len(value) > 0 && value[0] == '[' {
			if err := json.Unmarshal(value, &list); err != nil {
				return nil, fmt.Errorf("pricing: decode unit %q: %w", name, err)
			}
		} else {
			var one Rate
			if err := json.Unmarshal(value, &one); err != nil {
				return nil, fmt.Errorf("pricing: decode unit %q: %w", name, err)
			}
			list = []Rate{one}
		}
		for _, r := range list {
			if r.SameAs != "" && !knownUnits[r.SameAs] {
				return nil, fmt.Errorf("pricing: unit %q references unknown unit %q", name, r.SameAs)
			}
			if r.PerMillionNano < 0 || r.PerRequestNano < 0 || r.PerUnitNano < 0 {
				return nil, fmt.Errorf("pricing: unit %q has a negative rate", name)
			}
		}
		out.units[unit] = list
	}

	// input_long_context 是单档 tier 的语法糖（§3.2 的 jsonc 示例用的就是它）。
	// 归一化到 tiers 上，让计算路径只有一条。
	if list, ok := out.units[UnitInputLongCtx]; ok && len(list) > 0 {
		base := out.effective(UnitOutput, "")
		var outputNano int64
		if base != nil {
			outputNano = base.PerMillionNano
		}
		for _, r := range list {
			out.tiers = append(out.tiers, Tier{
				AboveTokens:          r.ThresholdTokens,
				InputPerMillionNano:  r.PerMillionNano,
				OutputPerMillionNano: outputNano,
			})
		}
		sort.Slice(out.tiers, func(i, j int) bool {
			return out.tiers[i].AboveTokens < out.tiers[j].AboveTokens
		})
	}

	if err := out.checkSameAsCycles(); err != nil {
		return nil, err
	}
	return out, nil
}

func (rs *Rates) checkSameAsCycles() error {
	for unit := range rs.units {
		seen := map[Unit]bool{}
		cur := unit
		for {
			list := rs.units[cur]
			if len(list) == 0 || list[0].SameAs == "" {
				break
			}
			if seen[cur] {
				return fmt.Errorf("pricing: same_as cycle at unit %q", cur)
			}
			seen[cur] = true
			cur = list[0].SameAs
		}
	}
	return nil
}

// effective 解析 same_as 后返回真正生效的费率。ttl 为空表示不挑档。
func (rs *Rates) effective(unit Unit, ttl string) *Rate {
	for hops := 0; hops < 8; hops++ {
		list := rs.units[unit]
		if len(list) == 0 {
			return nil
		}
		chosen := &list[0]
		if ttl != "" {
			for i := range list {
				if list[i].TTL == ttl {
					chosen = &list[i]
					break
				}
			}
		}
		if chosen.SameAs == "" {
			return chosen
		}
		unit = chosen.SameAs
		ttl = ""
	}
	return nil
}

// Has 报告某个计价单元是否被声明。**未声明 ≠ 0**：
// 前者是"这个模型没有这个计价单元"，后者是"免费"（§3.2）。
func (rs *Rates) Has(unit Unit) bool { return len(rs.units[unit]) > 0 }

// PerMillionNano 返回某单元解析后的每百万单位纳美元数，第二个返回值为
// false 表示该单元未声明。契约侧据此区分「—」与「$0.00」。
func (rs *Rates) PerMillionNano(unit Unit) (int64, bool) {
	r := rs.effective(unit, "")
	if r == nil {
		return 0, false
	}
	return r.PerMillionNano, true
}

// CacheWriteTiers 返回 cache_write 的所有档位（Anthropic 的 5m / 1h）。
func (rs *Rates) CacheWriteTiers() []Rate {
	list := rs.units[UnitCacheWrite]
	out := make([]Rate, 0, len(list))
	for _, r := range list {
		if r.SameAs != "" {
			if eff := rs.effective(UnitCacheWrite, r.TTL); eff != nil {
				clone := *eff
				clone.TTL = r.TTL
				out = append(out, clone)
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// Tiers 返回按 above_tokens 升序的分档价。
func (rs *Rates) Tiers() []Tier { return append([]Tier(nil), rs.tiers...) }

// Units 返回已声明的单元名，供校验与用量明细使用。
func (rs *Rates) Units() []Unit {
	out := make([]Unit, 0, len(rs.units))
	for u := range rs.units {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Raw 返回原始 jsonb，用于回写与审计留痕。
func (rs *Rates) Raw() json.RawMessage { return rs.raw }

// Version 是一条价目版本，也是"按哪一版算的"这个问题的答案。
//
// ID 就是 request_logs.price_version_id 要固化的值——没有这一列，
// 对账时无法回答"上个月这笔为什么是这个数"。这是审计的地板，不是可选项（§3.3-2）。
type Version struct {
	ID            int64
	BillingModel  string
	ScopeKind     string
	ScopeID       string
	Currency      string
	Rates         *Rates
	EffectiveFrom time.Time
	Source        string
	AppliedBy     string
	Note          string
}
