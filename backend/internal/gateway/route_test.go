package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
)

type fakeRouteRepo struct {
	candidates []RawCandidate
	policy     PolicyResolution
	err        error
}

func (f *fakeRouteRepo) ListCandidates(ctx context.Context, modelID int64) ([]RawCandidate, error) {
	return f.candidates, f.err
}
func (f *fakeRouteRepo) ResolvePolicy(ctx context.Context, modelID int64, workspaceID string) (PolicyResolution, error) {
	return f.policy, nil
}
func (f *fakeRouteRepo) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64) error {
	return nil
}
func (f *fakeRouteRepo) RecordFailure(ctx context.Context, channelID int64, rateLimited bool, rateLimitSeconds int) error {
	return nil
}

func candidate(id int64, alpha, beta float64) RawCandidate {
	return RawCandidate{
		ChannelID: id, ChannelName: "c", Profile: provider.Profile{Name: "c"},
		UpstreamModel: "m", Health: ChannelHealth{Alpha: alpha, Beta: beta},
	}
}

func TestRouter_NoChannelsReturnsSentinel(t *testing.T) {
	r := &Router{Repo: &fakeRouteRepo{}, Sampler: FixedSampler{}, Now: time.Now}
	_, err := r.Rank(context.Background(), 1, "")
	require.ErrorIs(t, err, ErrNoChannels)
}

func TestRouter_BalancedStrategyOrdersByReliability(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{candidate(1, 1, 9), candidate(2, 9, 1)}, // #2 is far more reliable
		policy:     PolicyResolution{Strategy: "balanced", FallbackDepth: 5},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err)
	require.Len(t, ranked, 2)
	assert.Equal(t, int64(2), ranked[0].ChannelID, "the more reliable channel must rank first")
}

func TestRouter_FallbackDepthTruncates(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{candidate(1, 5, 5), candidate(2, 5, 5), candidate(3, 5, 5)},
		policy:     PolicyResolution{Strategy: "balanced", FallbackDepth: 2},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err)
	assert.Len(t, ranked, 2, "fallback_depth must cap the candidate list")
}

func TestRouter_ZeroFallbackDepthMeansNoCap(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{candidate(1, 5, 5), candidate(2, 5, 5), candidate(3, 5, 5)},
		policy:     PolicyResolution{Strategy: "balanced", FallbackDepth: 0},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err)
	assert.Len(t, ranked, 3)
}

func TestRouter_PriorityStrategyBypassesScoring(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{
			{ChannelID: 1, Priority: 1, Weight: 100, Health: ChannelHealth{Alpha: 1, Beta: 100}}, // 差可靠性但高优先级
			{ChannelID: 2, Priority: 0, Weight: 100, Health: ChannelHealth{Alpha: 100, Beta: 1}},
		},
		policy: PolicyResolution{Strategy: "priority", FallbackDepth: 5},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err)
	require.Len(t, ranked, 2)
	assert.Equal(t, int64(1), ranked[0].ChannelID, "priority strategy must ignore health scores entirely")
}

func TestRouter_CustomStrategyMissingWeightsFallsBackToBalancedInsteadOfFailing(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{candidate(1, 5, 5)},
		policy:     PolicyResolution{Strategy: "custom", Weights: nil, FallbackDepth: 5},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err, "a misconfigured custom policy must not take down the whole request path")
	assert.Len(t, ranked, 1)
}

func TestRouter_StableOrderWhenScoresTie(t *testing.T) {
	repo := &fakeRouteRepo{
		candidates: []RawCandidate{candidate(1, 5, 5), candidate(2, 5, 5), candidate(3, 5, 5)},
		policy:     PolicyResolution{Strategy: "balanced", FallbackDepth: 5},
	}
	r := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
	ranked, err := r.Rank(context.Background(), 1, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, []int64{ranked[0].ChannelID, ranked[1].ChannelID, ranked[2].ChannelID},
		"tied scores must preserve the repository's original order, not shuffle")
}
