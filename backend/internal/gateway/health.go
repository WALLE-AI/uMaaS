package gateway

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// ChannelHealth 是一个候选渠道的当前健康统计，供评分使用
// （UNIFIED-PROVIDER-INTERFACE.md §5.5）。
type ChannelHealth struct {
	Alpha, Beta         float64 // Beta(alpha, beta) 后验
	LatencyEMAMs        *float64
	ConsecutiveFailures int32
	RateLimitedUntil    *time.Time
}

// Sampler 从渠道的健康后验里抽一个可靠性样本。
//
// **接口化是为了测试确定性**：默认实现用 math/rand/v2 的真随机源，
// 但评分测试需要固定种子重放同一组样本，否则"排序对不对"这类断言
// 会偶发失败——那种偶发失败比没有测试更糟，因为没人敢信任它。
type Sampler interface {
	// BetaSample 抽一个 Beta(alpha, beta) 分布的样本，用作这次决策的
	// 可靠性估计。Thompson 采样的核心就是"每次决策都重新抽一次"，
	// 而不是用后验均值——均值是确定性评分，会导致失败几次后再也选不中。
	BetaSample(alpha, beta float64) float64
}

// RandSampler 是生产环境用的默认实现。
type RandSampler struct{ rng *rand.Rand }

func NewRandSampler() *RandSampler { return &RandSampler{rng: rand.New(rand.NewPCG(0, 0))} }

// BetaSample 用 Gamma 分布的比值法生成 Beta 样本：
// X~Gamma(alpha,1)，Y~Gamma(beta,1) ⟹ X/(X+Y) ~ Beta(alpha,beta)。
// 标准库没有直接的 Beta 分布，这是最简单的正确实现，不引入新依赖。
func (s *RandSampler) BetaSample(alpha, beta float64) float64 {
	x := s.gammaSample(alpha)
	y := s.gammaSample(beta)
	if x+y == 0 {
		return 0.5
	}
	return x / (x + y)
}

// gammaSample 实现 Marsaglia-Tsang 方法（要求 shape ≥ 1；alpha/beta < 1 时
// 用 boost 技巧转换）。这是教科书算法，不是这里的创新点——正确性靠
// TestRandSampler_ConvergesToMean 用大样本统计量验证，而不是逐行审代码。
func (s *RandSampler) gammaSample(shape float64) float64 {
	if shape < 1 {
		u := s.rng.Float64()
		return s.gammaSample(shape+1) * math.Pow(u, 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		var x, v float64
		for {
			x = s.rng.NormFloat64()
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := s.rng.Float64()
		if u < 1-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

// FixedSampler 是测试用的确定性替身：每次 BetaSample 都返回后验均值
// alpha/(alpha+beta)，把"排序稳不稳定"和"随机数对不对"这两件事分开测。
type FixedSampler struct{}

func (FixedSampler) BetaSample(alpha, beta float64) float64 {
	if alpha+beta == 0 {
		return 0.5
	}
	return alpha / (alpha + beta)
}

// RoutingWeights 是凸组合的三个维度权重，和必须为 1（UNIFIED §5.5）。
type RoutingWeights struct {
	Reliability  float64 `json:"reliability"`
	Speed        float64 `json:"speed"`
	Intelligence float64 `json:"intelligence"`
}

// presetWeights 是四个内置策略的权重表（UNIFIED §5.5 的表）。
//
// **`smartest`/`fastest` 仍保留 0.35 的可靠性权重**——这不是随手抄表，
// 是编码了"一个聪明但总失败的模型不该赢，一个快但坏掉的模型也不该赢"
// 这条判断。改这几个数字前先想清楚是不是真的要推翻这条判断。
var presetWeights = map[string]RoutingWeights{
	"balanced": {Reliability: 0.50, Speed: 0.25, Intelligence: 0.25},
	"smartest": {Reliability: 0.35, Speed: 0.10, Intelligence: 0.55},
	"fastest":  {Reliability: 0.35, Speed: 0.55, Intelligence: 0.10},
	"reliable": {Reliability: 0.70, Speed: 0.15, Intelligence: 0.15},
}

// ResolveWeights 把策略名解析成权重向量。custom 用调用方传入的 weights；
// priority 不参与评分（调用方应该走手工优先级链，不调用这个函数）。
func ResolveWeights(strategy string, custom *RoutingWeights) (RoutingWeights, bool) {
	if strategy == "custom" {
		if custom == nil {
			return RoutingWeights{}, false
		}
		return *custom, true
	}
	w, ok := presetWeights[strategy]
	return w, ok
}

const weightSumEpsilon = 1e-6

// ValidateCustomWeights 校验 custom 权重的和为 1（凸组合的前提）。
//
// **写入时校验，不要等到评分时才发现**——那时错误会表现为"排序看起来
// 有点怪"，极难归因（UNIFIED §5.5，X9 的判据）。reliability 建议下限
// 0.30 只是警告：允许用户把它设成 0，等于允许他们把"聪明但不可靠的
// 模型不该赢"这条判断关掉，这是他们的选择，不该被硬性拦住。
type WeightIssue struct {
	Blocking bool
	Message  string
}

// HasBlockingWeightIssue 与 SummarizeWeightIssues 是写入口（store/postgres
// 的 UpsertRoutingPolicy）判断"能不能保存"用的小工具，避免每个调用方
// 各自重新实现一遍"遍历 issues 找 Blocking"这段逻辑。
func HasBlockingWeightIssue(issues []WeightIssue) bool {
	for _, i := range issues {
		if i.Blocking {
			return true
		}
	}
	return false
}

func SummarizeWeightIssues(issues []WeightIssue) string {
	var out string
	for i, issue := range issues {
		if i > 0 {
			out += "; "
		}
		out += issue.Message
	}
	return out
}

func ValidateCustomWeights(w RoutingWeights) []WeightIssue {
	var issues []WeightIssue
	sum := w.Reliability + w.Speed + w.Intelligence
	if math.Abs(sum-1.0) > weightSumEpsilon {
		issues = append(issues, WeightIssue{Blocking: true,
			Message: fmt.Sprintf("custom 权重之和必须为 1（凸组合的前提），当前为 %.4f", sum)})
	}
	if w.Reliability < 0 || w.Speed < 0 || w.Intelligence < 0 {
		issues = append(issues, WeightIssue{Blocking: true, Message: "权重不能为负"})
	}
	if w.Reliability < 0.30 {
		issues = append(issues, WeightIssue{Blocking: false,
			Message: "reliability 低于建议下限 0.30：一个聪明但总失败的模型可能因此排到前面"})
	}
	return issues
}

// ── 乘性护栏 ────────────────────────────────────────────────────

const (
	// headroomRampStart/Floor 是两个护栏共用的斜坡形状（UNIFIED §5.5），
	// 防止 headroomFactor 与 rateLimitFactor 两处阈值各自漂移。
	rampFloor = 0.1
)

// RateLimitFactor 实现"正在返回 429 的渠道被压低"这条乘性护栏。
//
// 是乘性而不是加性：不改变健康渠道之间的相对排序，只在某个渠道变
// 危险时把它单独拉下来。若改成加性惩罚，会连带扰动所有候选的相对次序。
func RateLimitFactor(rateLimitedUntil *time.Time, now time.Time) float64 {
	if rateLimitedUntil == nil || !now.Before(*rateLimitedUntil) {
		return 1
	}
	remaining := rateLimitedUntil.Sub(now)
	const rampWindow = 60 * time.Second
	if remaining >= rampWindow {
		return rampFloor
	}
	// 线性从 floor 爬回 1，剩余时间越短越快恢复到满分。
	return rampFloor + (1-rampFloor)*(1-float64(remaining)/float64(rampWindow))
}

// Score 是一个候选渠道的最终评分与其构成，供排序与可解释性排障使用。
type Score struct {
	ChannelID                        int64
	Base                             float64 // 凸组合，∈[0,1]
	Effective                        float64 // base × 全部乘性护栏
	Reliability, Speed, Intelligence float64
}

// ScoreChannel 实现 §5.5 的评分公式：
//
//	base      = w_rel·reliability + w_speed·speed + w_intel·intelligence
//	effective = base × rateLimitFactor
//
// intelligence 现在恒为一个固定基线（0.5）：真正的"智能分"需要模型
// 评测数据按渠道聚合，那是 I7/I8 自建供给接进来后 costFactor/评测体系
// 成熟时的事。**不伪造一个看起来有梯度的数字**——恒定基线在数学上
// 等价于"这一维暂不参与区分"，比编一个虚假分数更诚实。
func ScoreChannel(h ChannelHealth, weights RoutingWeights, sampler Sampler, now time.Time) Score {
	reliability := sampler.BetaSample(h.Alpha, h.Beta)
	speed := speedFactor(h.LatencyEMAMs)
	const intelligenceBaseline = 0.5

	base := weights.Reliability*reliability + weights.Speed*speed + weights.Intelligence*intelligenceBaseline
	effective := base * RateLimitFactor(h.RateLimitedUntil, now)

	return Score{
		Base: base, Effective: effective,
		Reliability: reliability, Speed: speed, Intelligence: intelligenceBaseline,
	}
}

// speedFactor 把延迟 EMA 归一化到 [0,1]。**未知即不表态**：一个新接入、
// 还没有统计数据的渠道不应该因为"看起来慢"被判死刑，给中性值 0.5
// 而不是 0 或 1（UNIFIED §5.5 的"未知即不表态"原则，用在这里同样成立）。
func speedFactor(latencyMs *float64) float64 {
	if latencyMs == nil {
		return 0.5
	}
	const (
		fastMs = 200.0  // 200ms 以内算满分
		slowMs = 5000.0 // 5s 以上算 0 分
	)
	l := *latencyMs
	if l <= fastMs {
		return 1
	}
	if l >= slowMs {
		return 0
	}
	return 1 - (l-fastMs)/(slowMs-fastMs)
}
