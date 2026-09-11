package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
)

func TestNextProbeInterval_GrowsWithConsecutiveFailures(t *testing.T) {
	a := nextProbeInterval(0)
	b := nextProbeInterval(3)
	c := nextProbeInterval(3)
	assert.Equal(t, probeIntervalFloor, a)
	assert.Greater(t, b, a, "more consecutive failures must widen the probe interval")
	assert.Equal(t, b, c, "the backoff must be deterministic given the same failure count")
}

func TestNextProbeInterval_CapsAtCeiling(t *testing.T) {
	interval := nextProbeInterval(1000)
	assert.LessOrEqual(t, interval, probeIntervalCeil,
		"an already-dead upstream must not be probed less than once per ceiling window — "+
			"it needs a chance to be discovered when it recovers")
}

type fakeHealthCheckRepo struct {
	due       []DueChannel
	touched   map[int64]int32
	skipModel bool
}

func (f *fakeHealthCheckRepo) ChannelsDueForProbe(ctx context.Context) ([]DueChannel, error) {
	return f.due, nil
}
func (f *fakeHealthCheckRepo) TouchProbe(ctx context.Context, channelID int64, nextIntervalSeconds int32) error {
	if f.touched == nil {
		f.touched = map[int64]int32{}
	}
	f.touched[channelID] = nextIntervalSeconds
	return nil
}
func (f *fakeHealthCheckRepo) ChannelDriverInfo(ctx context.Context, channelID int64) (provider.Profile, string, string, error) {
	if f.skipModel {
		return provider.Profile{}, "", "", nil
	}
	return provider.Profile{Name: "c"}, "gpt-x", "openai/gpt-x", nil
}

func TestHealthChecker_SkipsChannelWithoutProbeModel(t *testing.T) {
	repo := &fakeHealthCheckRepo{due: []DueChannel{{ChannelID: 1}}, skipModel: true}
	called := false
	hc := &HealthChecker{
		Repo:      repo,
		Probe:     &ProbeService{Repo: &fakeRouteRepo{}, Logs: discardLogger{}},
		NewDriver: func(p provider.Profile) UpstreamDoer { called = true; return &fakeUpstreamDoer{} },
	}
	hc.tick(context.Background())
	assert.False(t, called, "a channel with no probe model must be skipped, not probed with a guessed model")
	assert.Empty(t, repo.touched, "skipped channels must not have their probe interval touched")
}

func TestHealthChecker_TouchesIntervalAfterProbe(t *testing.T) {
	repo := &fakeHealthCheckRepo{due: []DueChannel{{ChannelID: 1, ConsecutiveFailures: 0}}}
	hc := &HealthChecker{
		Repo:  repo,
		Probe: &ProbeService{Repo: &fakeRouteRepo{}, Logs: discardLogger{}},
		NewDriver: func(p provider.Profile) UpstreamDoer {
			return &fakeUpstreamDoer{buildErr: assertErr("boom")}
		},
	}
	hc.tick(context.Background())
	require.Contains(t, repo.touched, int64(1))
	assert.GreaterOrEqual(t, repo.touched[1], int32(probeIntervalFloor/time.Second))
}
