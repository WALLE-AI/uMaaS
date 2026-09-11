package gateway

import (
	"context"
	"sync/atomic"
	"time"
)

// watchdog 实现 UNIFIED-PROVIDER-INTERFACE.md §7 的两段流式超时。
//
// 只设首字节超时不够：流已经开始、上游中途卡住不再吐字节时，首字节超时
// 早已失效，没有 stall 超时就会一直挂到客户端自己放弃。两段超时用
// **同一个可重置的定时器**实现：构造时按 firstByteTimeout 启动，每收到
// 一个事件就把它重置为 stallTimeout。这个切换本身就编码了"从安全窗口
// 进入非安全窗口"这件事，不需要单独一个布尔量去追踪"是不是已经过了首字节"。
//
// **两个布尔量都必须是原子的**：定时器触发的回调运行在独立的 goroutine
// 里，与调用 tick() 的主 goroutine 天然并发——这不是可以靠"调用顺序"
// 避免的竞态，是这个类型存在的意义本身（提前打断一个正在阻塞的调用）。
type watchdog struct {
	timer          *time.Timer
	gotFirstEvent  atomic.Bool
	firedFirstByte atomic.Bool
}

// newWatchdog 启动首字节计时器。cancel 是超时后要调用的函数——
// 通常是上游请求 context 的 CancelFunc，触发后底层的 Read 会因为
// ctx 被取消而返回错误，从而让调用方的读循环感知到超时。
func newWatchdog(cancel context.CancelFunc, firstByteTimeout time.Duration) *watchdog {
	w := &watchdog{}
	w.timer = time.AfterFunc(firstByteTimeout, func() {
		if !w.gotFirstEvent.Load() {
			w.firedFirstByte.Store(true)
		}
		cancel()
	})
	return w
}

// tick 在每次成功读到一个事件后调用，把计时器重置为 stallTimeout。
func (w *watchdog) tick(stallTimeout time.Duration) {
	w.gotFirstEvent.Store(true)
	w.timer.Reset(stallTimeout)
}

// stop 必须在读循环退出时调用（正常结束或出错都要），
// 否则计时器会在已经不需要它的请求上继续挂着，白占一个 goroutine 直到触发。
func (w *watchdog) stop() { w.timer.Stop() }

// timedOutBeforeFirstByte 报告触发时是否还没收到任何上游事件。
// 只有在调用方确认这次失败确实是这个 watchdog 触发的之后才有意义
// （通常用 errors.Is(err, context.Canceled) 或检查上游 ctx.Err() 来确认）。
func (w *watchdog) timedOutBeforeFirstByte() bool { return w.firedFirstByte.Load() }
