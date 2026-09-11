// Package requestlog 实现 request_logs 的异步批量写入
// （ARCHITECTURE.md §6.3、BILLING-AND-PRICING.md §4.3 的 attempt 级记录）。
//
// 一条纪律：**这条路径绝不能拖慢请求**。配额/余额扣减要强一致，走 Redis
// 原子裁决（M3，I5 才有）；而日志只是弱一致——异步 channel + 批量写入，
// 进程被 kill 时最多丢一个 flush 间隔的日志。这是刻意接受的权衡，
// 不会因此产生坏账，因为余额从不经过这条路径。
package requestlog

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/store"
)

// Entry 是一条 attempt 级请求日志，字段对应 request_logs 的列
// （M2，BILLING-AND-PRICING.md §9）。
type Entry struct {
	RequestID      string
	AttemptIndex   int32
	IsFinal        bool
	WorkspaceID    *int64
	APIKeyID       *int64
	RequestedModel string
	BillingModel   string
	UpstreamModel  string
	ChannelName    string
	// ChannelID 是 I4 起真正的路由标识；ChannelName 在没有渠道命中时
	// （如模型解析失败前就出错的极端路径）仍可以留空。
	ChannelID *int64
	// Origin 区分"谁发起的这次调用"（BILLING-AND-PRICING.md §4.3 最后一行）：
	// user | admin_probe | health_probe。手工渠道测试与自动健康探针都会
	// 产生真实上游消耗，需要可见但绝不能算进任何客户账单。
	Origin string

	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	ToolCallCount    int64
	UsageSource      string // upstream | self_hosted | estimated

	PriceVersionID    *int64
	UpstreamCostNano  int64
	ChargedAmountNano int64
	CostEstimated     bool

	TTFTMs     *int32
	LatencyMs  int32
	StatusCode int32
	ErrorCode  string

	AgentFramework string
	CreatedAt      time.Time
}

// 默认参数取自 ARCHITECTURE.md §6.3："flush 间隔 1–5s"。
// batchSize 是第二个触发条件——高 QPS 下不必等满 flushInterval 才写。
const (
	defaultFlushInterval = 2 * time.Second
	defaultBatchSize     = 500
	// channelCapacity 决定"最多能攒多少条日志等待落库"。
	// 打满后 Enqueue 直接丢弃并计数告警，而不是阻塞调用方——
	// 阻塞请求路径去等日志写完，代价远高于丢几条日志。
	channelCapacity = 4096
)

// Writer 是 request_logs 的唯一写入口。
type Writer struct {
	pool  copyFromer
	ch    chan Entry
	flush time.Duration
	batch int
	done  chan struct{}

	// Enqueue 会被数据平面的多个请求 goroutine 并发调用，因此这个计数器
	// 必须是原子的——早先按普通 uint64 写的版本在并发下是一个真实的数据竞争。
	dropped atomic.Uint64
}

// copyFromer 是 pgxpool.Pool 用得到的那部分接口，方便测试用假实现替换。
type copyFromer interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

func NewWriter(db *store.DB) *Writer {
	w := &Writer{
		pool: db.Pool, ch: make(chan Entry, channelCapacity),
		flush: defaultFlushInterval, batch: defaultBatchSize,
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Enqueue 提交一条日志，从不阻塞。
//
// 打满的 channel 意味着数据库或网络出了问题——那种时候更不能让
// 推理请求跟着日志一起卡住。丢弃的条数计入 Dropped()，接入
// Prometheus 后应该对它告警（它本身就是"日志管道堵了"的信号）。
func (w *Writer) Enqueue(e Entry) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	select {
	case w.ch <- e:
	default:
		n := w.dropped.Add(1)
		slog.Warn("request log dropped: writer channel full",
			"request_id", e.RequestID, "dropped_total", n)
	}
}

// Close 停止接收新日志并把已缓冲的写完。
//
// 优雅停机时必须调用它，否则进程退出前那最后几百毫秒的日志会白丢——
// 这不在"刻意接受的权衡"范围内，是能避免的损失。
func (w *Writer) Close(ctx context.Context) {
	close(w.ch)
	select {
	case <-w.done:
	case <-ctx.Done():
		slog.Warn("request log writer close timed out; some entries may be lost")
	}
}

func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.flush)
	defer ticker.Stop()

	buf := make([]Entry, 0, w.batch)
	flushBuf := func() {
		if len(buf) == 0 {
			return
		}
		if err := w.writeBatch(buf); err != nil {
			// 写失败了也不重试、不阻塞——重试会占用 goroutine 让新日志继续堆积。
			// 这批数据丢了，与"刻意接受的丢失窗口"是同一类风险，只是触发原因
			// 从"进程被杀"变成了"这次 COPY 失败"，后果同样有界。
			slog.Error("request log batch write failed", "error", err, "batch_size", len(buf))
		}
		buf = buf[:0]
	}

	for {
		select {
		case e, ok := <-w.ch:
			if !ok {
				flushBuf()
				return
			}
			buf = append(buf, e)
			if len(buf) >= w.batch {
				flushBuf()
			}
		case <-ticker.C:
			flushBuf()
		}
	}
}

var columns = []string{
	"request_id", "attempt_index", "is_final", "workspace_id", "api_key_id",
	"requested_model", "billing_model", "upstream_model", "channel_name", "channel_id", "origin",
	"prompt_tokens", "completion_tokens", "cache_read_tokens", "cache_write_tokens",
	"reasoning_tokens", "tool_call_count", "usage_source",
	"price_version_id", "upstream_cost_nano", "charged_amount_nano", "cost_estimated",
	"ttft_ms", "latency_ms", "status_code", "error_code", "agent_framework", "created_at",
}

func (w *Writer) writeBatch(entries []Entry) error {
	// flush 间隔以秒计，用独立的短超时而不是 context.Background()：
	// 数据库卡住时这个 goroutine 也不该无限期挂着。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows := make([][]any, 0, len(entries))
	for _, e := range entries {
		origin := e.Origin
		if origin == "" {
			origin = "user"
		}
		rows = append(rows, []any{
			e.RequestID, e.AttemptIndex, e.IsFinal, e.WorkspaceID, e.APIKeyID,
			e.RequestedModel, e.BillingModel, e.UpstreamModel, e.ChannelName, e.ChannelID, origin,
			e.PromptTokens, e.CompletionTokens, e.CacheReadTokens, e.CacheWriteTokens,
			e.ReasoningTokens, e.ToolCallCount, e.UsageSource,
			e.PriceVersionID, e.UpstreamCostNano, e.ChargedAmountNano, e.CostEstimated,
			e.TTFTMs, e.LatencyMs, e.StatusCode, e.ErrorCode, e.AgentFramework, e.CreatedAt,
		})
	}
	_, err := w.pool.CopyFrom(ctx, pgx.Identifier{"request_logs"}, columns, pgx.CopyFromRows(rows))
	return err
}

// Dropped 返回自启动以来因 channel 打满而丢弃的日志条数。
func (w *Writer) Dropped() uint64 { return w.dropped.Load() }
