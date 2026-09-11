package ir

// StreamEvent 是规范化的流式事件（UNIFIED-PROVIDER-INTERFACE.md §4.3）。
//
// 取三种协议流式语义的并集：块生命周期显式化（Anthropic 的形状），
// 因为"从有块结构降级到无块结构"容易，反向很难——OpenAI 的扁平
// `choices[].delta` 转成这个形状不损失信息，但反过来会。
type StreamEvent interface{ isStreamEvent() }

// BlockKind 是内容块的类型。
type BlockKind string

const (
	BlockText     BlockKind = "text"
	BlockToolUse  BlockKind = "tool_use"
	BlockThinking BlockKind = "thinking"
)

type EventMessageStart struct {
	ID    string
	Model string
	Role  Role
}

type EventBlockStart struct {
	Index int
	Kind  BlockKind
	// ToolName / ToolCallID 只在 Kind == BlockToolUse 时有值。
	ToolName   string
	ToolCallID string
}

// EventBlockDelta 是块内的增量。Text 与 PartialJSON 互斥：
// 文本块用 Text，工具调用块用 PartialJSON（跨多个事件拼接，§4.3-1）。
type EventBlockDelta struct {
	Index       int
	Text        string
	PartialJSON string
}

type EventBlockStop struct{ Index int }

// EventMessageDelta 携带结束原因与 usage。usage 统一在这里汇总
// （§4.3-3），不管上游实际是在哪个 chunk 给的。
type EventMessageDelta struct {
	FinishReason FinishReason
	Usage        *Usage
	UsageSource  UsageSource
}

type EventMessageStop struct{}

// EventError 是流中途失败的据实上报（ARCHITECTURE.md §6.2 第 2/3 条）。
//
// **它不是 EventMessageStop 的变体**：调用方必须能用类型区分"正常结束"
// 与"中断"，否则很容易在下游写出"收到 stop 就当成功"的代码，
// 而那正是"假装 finish_reason: stop"这个反模式的根源。
type EventError struct {
	Code    string
	Message string
	// Retryable 表示这次失败发生在首字节之前（安全窗口内）。
	// I3 只有单渠道，不做重试，但字段现在就留好——I4 的 P3 会用它
	// 决定要不要换渠道重试，而不是重新判断一遍"是不是还没吐字节"。
	Retryable bool
}

func (EventMessageStart) isStreamEvent() {}
func (EventBlockStart) isStreamEvent()   {}
func (EventBlockDelta) isStreamEvent()   {}
func (EventBlockStop) isStreamEvent()    {}
func (EventMessageDelta) isStreamEvent() {}
func (EventMessageStop) isStreamEvent()  {}
func (EventError) isStreamEvent()        {}
