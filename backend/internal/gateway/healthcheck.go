package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
)

// HealthCheckRepository 是后台探针循环需要的额外查询——不并进
// RouteRepository，因为那个接口是热路径（每次请求都用），这个是
// 慢路径（一个 ticker，全局共用一次）。
type HealthCheckRepository interface {
	// ChannelsDueForProbe 返回距上次探测已经超过 probe_interval_seconds
	// 的活跃渠道。
	ChannelsDueForProbe(ctx context.Context) ([]DueChannel, error)
	// TouchProbe 记录这次探测发生的时间，并把下一次探测的间隔写回去。
	TouchProbe(ctx context.Context, channelID int64, nextIntervalSeconds int32) error
	// ChannelDriverInfo 取一个渠道的 Profile 与它标记为 probe 的模型
	// （若没有标记，返回 upstreamModel="" 表示这次跳过）。
	ChannelDriverInfo(ctx context.Context, channelID int64) (profile provider.Profile, upstreamModel, billingModel string, err error)
}

// DueChannel 是一个待探测的渠道及其当前的连续失败计数。
type DueChannel struct {
	ChannelID            int64
	ConsecutiveFailures  int32
	ProbeIntervalSeconds int32
}

// probe 退休间隔的形状（UNIFIED-PROVIDER-INTERFACE.md §5.5 末段）：
// 连续失败越多，下一次探测拖得越久，避免对已死的上游持续打无效请求；
// 一次成功立刻把间隔打回底线，因为"已经证明还活着"不需要再谨慎。
const (
	probeIntervalFloor = 60 * time.Second
	probeIntervalCeil  = 30 * time.Minute
)

// nextProbeInterval 是指数退避，上限 30 分钟——**不是"退休后再也不测"**，
// 一个已死的上游有一天恢复了，30 分钟的探测间隔仍然能发现它
// （不像确定性评分那样需要人工解冻，Thompson 采样 + 定期探测两者互补）。
func nextProbeInterval(consecutiveFailures int32) time.Duration {
	interval := probeIntervalFloor
	for i := int32(0); i < consecutiveFailures && interval < probeIntervalCeil; i++ {
		interval *= 2
	}
	if interval > probeIntervalCeil {
		interval = probeIntervalCeil
	}
	return interval
}

// HealthChecker 是 B4 的后台探针循环：定期给到期的渠道发一次探测请求，
// 结果喂进 Beta 后验，并按探针退休规则调整下一次探测的间隔。
type HealthChecker struct {
	Repo      HealthCheckRepository
	Probe     *ProbeService
	NewDriver func(provider.Profile) UpstreamDoer
	Interval  time.Duration // 主循环的 tick 间隔，不是单个渠道的探测间隔
}

// Run 阻塞运行，直到 ctx 取消。调用方应该在一个独立 goroutine 里启动它。
func (h *HealthChecker) Run(ctx context.Context) {
	interval := h.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.tick(ctx)
		}
	}
}

func (h *HealthChecker) tick(ctx context.Context) {
	due, err := h.Repo.ChannelsDueForProbe(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "health checker: list channels due for probe failed", "error", err)
		return
	}
	for _, ch := range due {
		h.probeOne(ctx, ch)
	}
}

func (h *HealthChecker) probeOne(ctx context.Context, ch DueChannel) {
	profile, upstreamModel, billingModel, err := h.Repo.ChannelDriverInfo(ctx, ch.ChannelID)
	if err != nil {
		slog.ErrorContext(ctx, "health checker: load channel failed", "channel_id", ch.ChannelID, "error", err)
		return
	}
	if upstreamModel == "" {
		// 没有标 is_probe 的模型：跳过而不是随便挑一个——随便挑会得到
		// "渠道健康但用户要的那个模型 404"这种最具误导性的结果（§5.6-①）。
		return
	}

	driver := h.NewDriver(profile)
	result := h.Probe.AutoProbe(ctx, ch.ChannelID, driver, upstreamModel, billingModel)

	failures := ch.ConsecutiveFailures
	if result.OverallOK {
		failures = 0
	} else {
		failures++
	}
	nextInterval := int32(nextProbeInterval(failures) / time.Second)
	if err := h.Repo.TouchProbe(ctx, ch.ChannelID, nextInterval); err != nil {
		slog.ErrorContext(ctx, "health checker: touch probe failed", "channel_id", ch.ChannelID, "error", err)
	}
}
