package requestlog

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePool 记录每次 CopyFrom 调用收到的行数，替代真实 Postgres 连接池。
// 单元测试要验证的是批量写入的**触发条件**（凑够 batch 或到 flush 间隔），
// 不是 SQL 本身对不对——那部分留给集成测试。
type fakePool struct {
	mu    sync.Mutex
	calls [][][]any
}

func (f *fakePool) CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows [][]any
	for src.Next() {
		row, err := src.Values()
		if err != nil {
			return 0, err
		}
		rows = append(rows, row)
	}
	f.calls = append(f.calls, rows)
	return int64(len(rows)), nil
}

func (f *fakePool) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePool) totalRows() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += len(c)
	}
	return n
}

func newTestWriter(pool copyFromer, flush time.Duration, batch int) *Writer {
	w := &Writer{pool: pool, ch: make(chan Entry, channelCapacity), flush: flush, batch: batch, done: make(chan struct{})}
	go w.run()
	return w
}

func TestWriter_FlushesOnBatchSize(t *testing.T) {
	pool := &fakePool{}
	w := newTestWriter(pool, time.Hour, 3) // flush 间隔故意很长，只测 batch 触发

	for i := 0; i < 3; i++ {
		w.Enqueue(Entry{RequestID: "r", StatusCode: 200})
	}

	require.Eventually(t, func() bool { return pool.callCount() == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, 3, pool.totalRows())
}

func TestWriter_FlushesOnTicker(t *testing.T) {
	pool := &fakePool{}
	w := newTestWriter(pool, 20*time.Millisecond, 1000) // batch 很大，只测定时触发

	w.Enqueue(Entry{RequestID: "r", StatusCode: 200})

	require.Eventually(t, func() bool { return pool.callCount() >= 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, pool.totalRows())
}

func TestWriter_CloseFlushesRemainingEntries(t *testing.T) {
	pool := &fakePool{}
	w := newTestWriter(pool, time.Hour, 1000)

	w.Enqueue(Entry{RequestID: "a", StatusCode: 200})
	w.Enqueue(Entry{RequestID: "b", StatusCode: 200})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w.Close(ctx)

	assert.Equal(t, 2, pool.totalRows(), "in-flight entries must not be lost on graceful shutdown")
}

func TestWriter_DropsWhenChannelFullWithoutBlocking(t *testing.T) {
	pool := &fakePool{}
	w := &Writer{pool: pool, ch: make(chan Entry), flush: time.Hour, batch: 1000, done: make(chan struct{})}
	// 不启动 run()：channel 无缓冲且没有消费者，第一次 Enqueue 就必须打满走 default 分支。

	done := make(chan struct{})
	go func() {
		w.Enqueue(Entry{RequestID: "x"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue must never block the caller, even when the channel is full")
	}
	assert.Equal(t, uint64(1), w.Dropped())
}
