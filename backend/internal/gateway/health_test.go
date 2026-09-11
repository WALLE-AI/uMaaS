package gateway

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandSampler_ConvergesToMean(t *testing.T) {
	s := NewRandSampler()
	const alpha, beta = 8.0, 2.0 // 均值 0.8
	var sum float64
	const n = 20000
	for i := 0; i < n; i++ {
		sum += s.BetaSample(alpha, beta)
	}
	mean := sum / n
	assert.InDelta(t, 0.8, mean, 0.02, "large-sample mean of Beta(8,2) should converge to 0.8")
}

func TestRandSampler_StaysInUnitInterval(t *testing.T) {
	s := NewRandSampler()
	for i := 0; i < 5000; i++ {
		v := s.BetaSample(0.5, 3.0)
		require.GreaterOrEqual(t, v, 0.0)
		require.LessOrEqual(t, v, 1.0)
	}
}

// 一个失败几次的渠道必须仍有机会被抽中偏高的可靠性样本——
// 这是 Thompson 采样存在的全部理由：确定性评分会让它永远沉底。
func TestRandSampler_FailedChannelCanStillSampleHigh(t *testing.T) {
	s := NewRandSampler()
	// 3 次成功 2 次失败：后验均值 0.6，但方差还很大，应该能抽到 >0.9 的样本。
	sawHigh := false
	for i := 0; i < 2000; i++ {
		if s.BetaSample(4, 3) > 0.9 {
			sawHigh = true
			break
		}
	}
	assert.True(t, sawHigh, "a channel with only a few failures must still occasionally sample high")
}

func TestValidateCustomWeights_RejectsSumNotOne(t *testing.T) {
	issues := ValidateCustomWeights(RoutingWeights{Reliability: 0.5, Speed: 0.3, Intelligence: 0.3})
	require.NotEmpty(t, issues)
	assert.True(t, issues[0].Blocking)
}

func TestValidateCustomWeights_AcceptsValidSum(t *testing.T) {
	issues := ValidateCustomWeights(RoutingWeights{Reliability: 0.4, Speed: 0.3, Intelligence: 0.3})
	for _, i := range issues {
		assert.False(t, i.Blocking, "a sum of 1.0 must never be blocked")
	}
}

func TestValidateCustomWeights_WarnsButDoesNotBlockLowReliability(t *testing.T) {
	issues := ValidateCustomWeights(RoutingWeights{Reliability: 0, Speed: 0.5, Intelligence: 0.5})
	require.NotEmpty(t, issues)
	for _, i := range issues {
		assert.False(t, i.Blocking, "reliability=0 is the user's explicit choice; it must warn, not block")
	}
}

func TestValidateCustomWeights_RejectsNegative(t *testing.T) {
	issues := ValidateCustomWeights(RoutingWeights{Reliability: 1.2, Speed: -0.2, Intelligence: 0})
	require.NotEmpty(t, issues)
	blocked := false
	for _, i := range issues {
		if i.Blocking {
			blocked = true
		}
	}
	assert.True(t, blocked)
}

func TestResolveWeights_PresetsAndCustom(t *testing.T) {
	w, ok := ResolveWeights("smartest", nil)
	require.True(t, ok)
	assert.InDelta(t, 1.0, w.Reliability+w.Speed+w.Intelligence, 1e-9)

	_, ok = ResolveWeights("custom", nil)
	assert.False(t, ok, "custom without an explicit weight vector must fail, not silently default")

	custom := RoutingWeights{Reliability: 0.6, Speed: 0.2, Intelligence: 0.2}
	got, ok := ResolveWeights("custom", &custom)
	require.True(t, ok)
	assert.Equal(t, custom, got)
}

func TestRateLimitFactor_FullFactorWhenNotLimited(t *testing.T) {
	now := time.Now()
	assert.Equal(t, 1.0, RateLimitFactor(nil, now))
	past := now.Add(-time.Second)
	assert.Equal(t, 1.0, RateLimitFactor(&past, now))
}

func TestRateLimitFactor_SuppressesRecentlyLimitedChannel(t *testing.T) {
	now := time.Now()
	until := now.Add(50 * time.Second)
	factor := RateLimitFactor(&until, now)
	assert.Less(t, factor, 1.0)
	assert.GreaterOrEqual(t, factor, rampFloor)
}

// 乘性护栏不能反转健康渠道之间的相对排序——只应该压低被限流的那一个。
func TestRateLimitFactor_DoesNotInvertRelativeOrder(t *testing.T) {
	now := time.Now()
	sampler := FixedSampler{}
	weights := presetWeights["balanced"]

	healthyA := ChannelHealth{Alpha: 9, Beta: 1} // 高可靠性，未限流
	limitedB := ChannelHealth{Alpha: 9, Beta: 1, RateLimitedUntil: timePtr(now.Add(30 * time.Second))}

	scoreA := ScoreChannel(healthyA, weights, sampler, now)
	scoreB := ScoreChannel(limitedB, weights, sampler, now)

	assert.Equal(t, scoreA.Base, scoreB.Base, "base score must be identical before the guardrail is applied")
	assert.Less(t, scoreB.Effective, scoreA.Effective, "a rate-limited channel must score lower after the guardrail")
}

func TestScoreChannel_UnknownLatencyIsNeutralNotPunitive(t *testing.T) {
	now := time.Now()
	sampler := FixedSampler{}
	weights := RoutingWeights{Reliability: 0, Speed: 1, Intelligence: 0}

	unknown := ScoreChannel(ChannelHealth{Alpha: 1, Beta: 1}, weights, sampler, now)
	slow := ScoreChannel(ChannelHealth{Alpha: 1, Beta: 1, LatencyEMAMs: floatPtr(4900)}, weights, sampler, now)
	fast := ScoreChannel(ChannelHealth{Alpha: 1, Beta: 1, LatencyEMAMs: floatPtr(100)}, weights, sampler, now)

	assert.Greater(t, unknown.Speed, slow.Speed, "an unmeasured channel must not be treated as if it were slow")
	assert.Less(t, unknown.Speed, fast.Speed)
	assert.InDelta(t, 0.5, unknown.Speed, 1e-9, "unknown latency must map to the neutral midpoint")
}

func TestScoreChannel_ReliabilityWeightDominatesWhenSetHigh(t *testing.T) {
	now := time.Now()
	sampler := FixedSampler{}
	reliableStrategy := presetWeights["reliable"]

	unreliableButFast := ChannelHealth{Alpha: 1, Beta: 9, LatencyEMAMs: floatPtr(50)}
	reliableButSlow := ChannelHealth{Alpha: 9, Beta: 1, LatencyEMAMs: floatPtr(4900)}

	a := ScoreChannel(unreliableButFast, reliableStrategy, sampler, now)
	b := ScoreChannel(reliableButSlow, reliableStrategy, sampler, now)

	assert.Greater(t, b.Effective, a.Effective,
		"under the 'reliable' strategy, a slow-but-reliable channel must outrank a fast-but-unreliable one")
}

func floatPtr(f float64) *float64    { return &f }
func timePtr(t time.Time) *time.Time { return &t }

func TestFixedSampler_ReturnsPosteriorMean(t *testing.T) {
	s := FixedSampler{}
	assert.InDelta(t, 0.8, s.BetaSample(8, 2), 1e-9)
	assert.Equal(t, 0.5, s.BetaSample(0, 0))
}

func TestGammaSample_ShapeLessThanOne(t *testing.T) {
	s := NewRandSampler()
	// shape<1 走 boost 分支；只验证不会 panic 且落在合理正数范围。
	for i := 0; i < 1000; i++ {
		v := s.gammaSample(0.3)
		require.False(t, math.IsNaN(v))
		require.GreaterOrEqual(t, v, 0.0)
	}
}
