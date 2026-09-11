package gateway

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
)

// ErrNoChannels 表示一个模型在目录里存在，但没有任何渠道能提供它。
//
// 与"模型不存在"是不同的错误：前者是目录问题（B2/M1a 该管的事），
// 后者是供给问题——本次请求应该收到一个明确说"暂无可用渠道"的错误，
// 而不是被 §5.5 的评分空转成一个模糊的 500。
var ErrNoChannels = errors.New("gateway: no channel serves this model")

// Candidate 是一次排序后的候选渠道：已经带着可以直接拿去建请求的
// provider.Profile，调用方不需要再去拼装鉴权信息。
type Candidate struct {
	ChannelID     int64
	ChannelName   string
	Profile       provider.Profile
	UpstreamModel string
	Score         Score
}

// PolicyResolution 是路由策略解析结果（X9）。
type PolicyResolution struct {
	Strategy      string
	Weights       *RoutingWeights // 仅 strategy=custom 时非空
	FallbackDepth int
}

// RouteRepository 是路由引擎需要的持久化端口。
type RouteRepository interface {
	ListCandidates(ctx context.Context, modelID int64) ([]RawCandidate, error)
	ResolvePolicy(ctx context.Context, modelID int64, workspaceID string) (PolicyResolution, error)
	RecordSuccess(ctx context.Context, channelID int64, latencyMs float64) error
	RecordFailure(ctx context.Context, channelID int64, rateLimited bool, rateLimitSeconds int) error
}

// RawCandidate 是仓储层返回的原始候选，还没有排序也没有解密凭证之外的处理
// ——Profile 已经在仓储层组装好（含解密后的凭证），路由引擎只关心健康统计。
type RawCandidate struct {
	ChannelID     int64
	ChannelName   string
	Profile       provider.Profile
	UpstreamModel string
	Priority      int32
	Weight        int32
	Health        ChannelHealth
}

// Router 实现 P3 的渠道选择：能力协商之后，把候选渠道按策略排出一条
// 回退链。**策略引擎唯一**：balanced/smartest/fastest/reliable/custom
// 都走同一个 ScoreChannel，priority 是唯一的例外（退回手工链，不参与
// 评分，UNIFIED §5.5 的策略表最后一行）。
type Router struct {
	Repo    RouteRepository
	Sampler Sampler
	Now     func() time.Time // 测试注入固定时间
}

func NewChannelRouter(repo RouteRepository) *Router {
	return &Router{Repo: repo, Sampler: NewRandSampler(), Now: time.Now}
}

// Rank 返回按策略排好序、截断到 fallback_depth 的候选渠道列表。
//
// 空列表配 ErrNoChannels；调用方（attempt.go）依次尝试，直到成功或
// 用尽列表——回退的边界条件（哪些失败允许换渠道）不在这里判断，
// 那是每次尝试之后才知道的事，属于 attempt 循环的职责。
func (r *Router) Rank(ctx context.Context, modelID int64, workspaceID string) ([]Candidate, error) {
	raw, err := r.Repo.ListCandidates(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrNoChannels
	}

	policy, err := r.Repo.ResolvePolicy(ctx, modelID, workspaceID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	var candidates []Candidate
	if policy.Strategy == "priority" {
		candidates = rankByPriority(raw)
	} else {
		weights, ok := ResolveWeights(policy.Strategy, policy.Weights)
		if !ok {
			// custom 缺权重是配置错误（X9 的写入校验本该拦住），
			// 退回 balanced 而不是让整条请求路径炸掉——路由策略配错
			// 不该让用户的请求跟着遭殃，这条防线比"报错更彻底"更重要。
			weights = presetWeights["balanced"]
		}
		candidates = rankByScore(raw, weights, r.Sampler, now)
	}

	depth := policy.FallbackDepth
	if depth <= 0 || depth > len(candidates) {
		depth = len(candidates)
	}
	return candidates[:depth], nil
}

func rankByPriority(raw []RawCandidate) []Candidate {
	sorted := append([]RawCandidate(nil), raw...)
	// 稳定排序：评分/优先级并列时保持仓储层返回的顺序，而不是每次请求
	// 都洗一遍牌——那会让日志里的"选中了哪个渠道"变得无法预测，
	// 排障时不必要地增加噪音。
	slices.SortStableFunc(sorted, func(a, b RawCandidate) int {
		if a.Priority != b.Priority {
			return int(b.Priority) - int(a.Priority)
		}
		return int(b.Weight) - int(a.Weight)
	})
	out := make([]Candidate, 0, len(sorted))
	for _, c := range sorted {
		out = append(out, Candidate{
			ChannelID: c.ChannelID, ChannelName: c.ChannelName,
			Profile: c.Profile, UpstreamModel: c.UpstreamModel,
		})
	}
	return out
}

func rankByScore(raw []RawCandidate, weights RoutingWeights, sampler Sampler, now time.Time) []Candidate {
	out := make([]Candidate, 0, len(raw))
	for _, c := range raw {
		out = append(out, Candidate{
			ChannelID: c.ChannelID, ChannelName: c.ChannelName,
			Profile: c.Profile, UpstreamModel: c.UpstreamModel,
			Score: ScoreChannel(c.Health, weights, sampler, now),
		})
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		switch {
		case a.Score.Effective > b.Score.Effective:
			return -1
		case a.Score.Effective < b.Score.Effective:
			return 1
		default:
			return 0
		}
	})
	return out
}
