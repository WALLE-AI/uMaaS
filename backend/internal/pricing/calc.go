package pricing

import (
	"errors"
	"fmt"
	"math/bits"
)

// ErrOverflow 表示金额超出 int64 能表达的范围。
//
// 它不该在任何真实请求上出现（int64 上限 ≈ $9.22e9），出现即意味着
// 价目填错了几个数量级——**报错远好于悄悄回绕成一个负数金额**。
var ErrOverflow = errors.New("pricing: amount overflows int64 nano-USD")

const nanoPerMillionDivisor = 1_000_000

// Usage 是一次调用的实际用量。字段与 request_logs 的埋点一一对应（§3.1）。
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	// CacheWriteTTL 选 cache_write 的档（"5m" / "1h"）。空字符串取第一档。
	CacheWriteTTL string
	// ReasoningTokens 单列。部分厂商与 output 同价，那种情况下价目里
	// 写 {"reasoning": {"same_as": "output"}}，而不是在这里合并进 OutputTokens——
	// 合并会让"某厂商改成单列计价"这件事没有改动点。
	ReasoningTokens int64
	// Requests 通常是 1。固定费按它乘。
	Requests int64
}

// LineItem 是账单明细的一行。用量明细页要能逐单元展示，
// 且**未声明的单元显示为 0 而非空**（§3.2）。
type LineItem struct {
	Unit       Unit
	Quantity   int64
	RateNano   int64 // 每百万单位（固定费为每请求）
	AmountNano int64
}

// Charge 计算一次调用的金额（纳美元）。
//
// # 两条舍入纪律（§3.5）
//
//  1. **全程 nano 整数，中间不取整。** 每个计价单元的 tokens × rate 累加进一个
//     128 位中间量，最后**只对请求总额做一次 round-half-up**。
//  2. **不对每个计价单元分别舍入。** 分别舍入会让"分维度求和"与"总额"对不上，
//     这类差异在对账时极其难查。
//
// # 溢出
//
// 危险不在总额（int64 上限 ≈ $9.22e9），而在**中间乘积**：
// 1e7 token × $1000/1M 的极端组合下 tokens × per_million_nano 会溢出 int64。
// 这里用 bits.Mul64 累到 128 位再一次性除，因此该组合能算对；
// 真正超出 int64 表达范围时返回 ErrOverflow 而不是回绕。
func (v *Version) Charge(u Usage) (int64, []LineItem, error) {
	if v == nil || v.Rates == nil {
		return 0, nil, fmt.Errorf("pricing: no price version")
	}
	rs := v.Rates

	inputRate, outputRate := rs.effective(UnitInput, ""), rs.effective(UnitOutput, "")
	inputNano, outputNano := rateNano(inputRate), rateNano(outputRate)

	// 分档价：按**输入 token 数**选档，命中后 input/output 单价整体替换。
	// 只换 input 不换 output 是错的——Gemini 的超长上下文两者都涨。
	for _, t := range rs.Tiers() {
		if u.InputTokens > t.AboveTokens {
			inputNano = t.InputPerMillionNano
			if t.OutputPerMillionNano > 0 {
				outputNano = t.OutputPerMillionNano
			}
		}
	}

	var acc u128
	var items []LineItem

	add := func(unit Unit, qty, rate int64, declared bool) error {
		if !declared {
			return nil
		}
		if qty < 0 || rate < 0 {
			return fmt.Errorf("pricing: negative quantity or rate for unit %q", unit)
		}
		hi, lo := bits.Mul64(uint64(qty), uint64(rate))
		acc = acc.add(u128{hi, lo})
		items = append(items, LineItem{Unit: unit, Quantity: qty, RateNano: rate})
		return nil
	}

	if err := add(UnitInput, u.InputTokens, inputNano, inputRate != nil); err != nil {
		return 0, nil, err
	}
	if err := add(UnitOutput, u.OutputTokens, outputNano, outputRate != nil); err != nil {
		return 0, nil, err
	}
	if r := rs.effective(UnitCacheRead, ""); r != nil {
		if err := add(UnitCacheRead, u.CacheReadTokens, r.PerMillionNano, true); err != nil {
			return 0, nil, err
		}
	}
	if r := rs.effective(UnitCacheWrite, u.CacheWriteTTL); r != nil {
		if err := add(UnitCacheWrite, u.CacheWriteTokens, r.PerMillionNano, true); err != nil {
			return 0, nil, err
		}
	}
	if r := rs.effective(UnitReasoning, ""); r != nil {
		if err := add(UnitReasoning, u.ReasoningTokens, r.PerMillionNano, true); err != nil {
			return 0, nil, err
		}
	}

	// 一次性 round-half-up 到 nano：+ 500000 再整除 1e6。
	total, err := acc.divRoundHalfUp(nanoPerMillionDivisor)
	if err != nil {
		return 0, nil, err
	}

	// 固定费不经过"每百万"这一步，直接按请求数加。
	if r := rs.effective(UnitRequestFixed, ""); r != nil && r.PerRequestNano != 0 {
		reqs := u.Requests
		if reqs <= 0 {
			reqs = 1
		}
		fixed, ok := mulInt64(r.PerRequestNano, reqs)
		if !ok {
			return 0, nil, ErrOverflow
		}
		sum, ok := addInt64(total, fixed)
		if !ok {
			return 0, nil, ErrOverflow
		}
		total = sum
		items = append(items, LineItem{
			Unit: UnitRequestFixed, Quantity: reqs,
			RateNano: r.PerRequestNano, AmountNano: fixed,
		})
	}

	// 明细金额单独算一遍（各自 round-half-up），仅供展示。
	// **总额不是明细之和**——这正是 §3.5 那条"不对每个单元分别舍入"的直接后果，
	// 差异最多 1 nano，但把它写清楚比让对账的人自己发现要好。
	for i := range items {
		if items[i].Unit == UnitRequestFixed {
			continue
		}
		hi, lo := bits.Mul64(uint64(items[i].Quantity), uint64(items[i].RateNano))
		amount, err := (u128{hi, lo}).divRoundHalfUp(nanoPerMillionDivisor)
		if err != nil {
			return 0, nil, err
		}
		items[i].AmountNano = amount
	}

	return total, items, nil
}

// ── 128 位无符号累加器 ────────────────────────────────────────
//
// 用 bits 包而不是 big.Int：结算在热路径上，每请求一次堆分配没有必要。
// 128 位足够——单个乘积上限 2^128，而我们的量级离它有 20 个数量级。

type u128 struct{ hi, lo uint64 }

func (a u128) add(b u128) u128 {
	lo, carry := bits.Add64(a.lo, b.lo, 0)
	hi, _ := bits.Add64(a.hi, b.hi, carry)
	return u128{hi, lo}
}

// divRoundHalfUp 计算 round((a + d/2) / d)，结果必须落在 int64 内。
func (a u128) divRoundHalfUp(d uint64) (int64, error) {
	half := d / 2
	lo, carry := bits.Add64(a.lo, half, 0)
	hi, c2 := bits.Add64(a.hi, 0, carry)
	if c2 != 0 {
		return 0, ErrOverflow
	}
	a = u128{hi, lo}

	// bits.Div64 要求 hi < d，否则 panic。先用 hi 除一次把它压下来。
	quoHi, rem := a.hi/d, a.hi%d
	quoLo, _ := bits.Div64(rem, a.lo, d)
	if quoHi != 0 || quoLo > uint64(1<<63-1) {
		return 0, ErrOverflow
	}
	return int64(quoLo), nil
}

func rateNano(r *Rate) int64 {
	if r == nil {
		return 0
	}
	return r.PerMillionNano
}

func mulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	c := a * b
	if c/b != a {
		return 0, false
	}
	return c, true
}

func addInt64(a, b int64) (int64, bool) {
	c := a + b
	if (c > a) != (b > 0) {
		return 0, false
	}
	return c, true
}

// NanoToUSD 把纳美元转成契约里的 USD 数值。
//
// **只在这一步取整，且只取整一次**（§3.5）。请求级、账目级一律用整数 nano，
// 浮点只出现在最外层的 JSON 序列化——这是"浮点累加百万次误差会进账单"的解法。
func NanoToUSD(nano int64) float64 { return float64(nano) / 1e9 }

// PerMillionUSD 把"每百万单位纳美元"转成契约的 `*_per_million`。
func PerMillionUSD(perMillionNano int64) float64 { return float64(perMillionNano) / 1e9 }

// USDPerMillionToNano 是录入方向：admin 填 "0.15"（USD / 1M），存 150000000。
//
// 走 round-half-up 而不是 float 截断：0.15 在二进制浮点里是 0.1499999...，
// 直接 int64(x*1e9) 会存成 149999999，且这个 1 nano 的偏差会出现在**每一次调用**上。
func USDPerMillionToNano(usd float64) int64 {
	if usd < 0 {
		return 0
	}
	return int64(usd*1e9 + 0.5)
}
