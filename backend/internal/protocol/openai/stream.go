package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

// ── 解码：上游 SSE → 规范事件 ────────────────────────────────────

type wireStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type wireStreamDelta struct {
	Role      string               `json:"role,omitempty"`
	Content   *string              `json:"content,omitempty"`
	ToolCalls []wireStreamToolCall `json:"tool_calls,omitempty"`
}

type wireStreamChoice struct {
	Index        int             `json:"index"`
	Delta        wireStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type wireStreamChunk struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Choices []wireStreamChoice `json:"choices"`
	Usage   *wireUsage         `json:"usage,omitempty"`
}

// StreamReader 把上游的 OpenAI 兼容 SSE 流转成规范事件序列。
//
// **块生命周期在这里显式化**（§4.3）：OpenAI 的原生形状只有扁平 delta，
// 工具调用靠 index 累积拼接；这个类型负责把那种隐式状态变成显式的
// BlockStart/BlockDelta/BlockStop，下游（网关的超时判断、日志、未来的
// 跨协议转换）不用重新推导一遍"这段 delta 属于哪个逻辑块"。
type StreamReader struct {
	scanner      *bufio.Scanner
	includeUsage bool

	queue []ir.StreamEvent
	done  bool

	started    bool
	textOpen   bool
	toolBlocks map[int]int // 上游 tool_call.index → 我们的块 index
	blockOrder []int       // 块打开顺序，用于按序关闭
	nextBlock  int

	pendingFinish *string
	pendingUsage  *ir.Usage
	usageSource   ir.UsageSource
	finished      bool
}

// NewStreamReader 包住上游响应体。includeUsage 对应客户端请求里的
// `stream_options.include_usage`——**只有知道客户端要不要 usage，
// 才能判断"finish_reason 之后还要不要再等一个 usage-only chunk"**，
// 否则要么提前收尾丢了 usage，要么无限等一个永远不会来的 chunk。
func NewStreamReader(body io.Reader, includeUsage bool) *StreamReader {
	sc := bufio.NewScanner(body)
	// 工具调用参数可能很长（大段 JSON），默认 64KiB 行缓冲不够。
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &StreamReader{
		scanner: sc, includeUsage: includeUsage,
		toolBlocks: map[int]int{}, nextBlock: 1,
	}
}

// ErrStreamDone 是流正常结束的哨兵，调用方用 errors.Is 判断。
var ErrStreamDone = errors.New("openai: stream done")

// Next 返回下一个规范事件。流正常结束时返回 (EventMessageStop, ErrStreamDone)——
// **调用方仍然可以拿到那个事件**，因为 ErrStreamDone 不是失败，
// 用 EOF 语义传信息容易让调用方漏掉最后一个事件。
func (r *StreamReader) Next() (ir.StreamEvent, error) {
	for {
		if len(r.queue) > 0 {
			ev := r.queue[0]
			r.queue = r.queue[1:]
			if _, ok := ev.(ir.EventMessageStop); ok {
				return ev, ErrStreamDone
			}
			return ev, nil
		}
		if r.done {
			return ir.EventMessageStop{}, ErrStreamDone
		}
		if err := r.pump(); err != nil {
			return nil, err
		}
	}
}

// pump 读一行 SSE 数据并把它翻译成 0 个或多个排队事件。
func (r *StreamReader) pump() error {
	for r.scanner.Scan() {
		line := r.scanner.Bytes()
		if len(line) == 0 {
			continue // SSE 事件分隔的空行
		}
		data, ok := bytes.CutPrefix(line, []byte("data: "))
		if !ok {
			data, ok = bytes.CutPrefix(line, []byte("data:"))
			if !ok {
				continue // 忽略 event:/id:/注释等非 data 行
			}
		}
		data = bytes.TrimSpace(data)
		if string(data) == "[DONE]" {
			r.finish()
			return nil
		}

		var chunk wireStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("openai: invalid stream chunk: %w", err)
		}
		r.consume(chunk)
		if len(r.queue) > 0 {
			return nil
		}
		// 这一行没有产出事件（例如只带 role 的空 delta），继续读下一行。
	}
	if err := r.scanner.Err(); err != nil {
		return fmt.Errorf("openai: read stream: %w", err)
	}
	// 上游连接结束但没有 [DONE]——多数是连接被切断，不是正常收尾。
	// 据实上报，不伪造一个 stop（ARCHITECTURE.md §6.2 第 3 条）。
	if !r.finished {
		r.queue = append(r.queue, ir.EventError{
			Code: "upstream_closed", Message: "upstream closed the stream before [DONE]",
		})
		r.done = true
		return nil
	}
	r.finish()
	return nil
}

func (r *StreamReader) consume(chunk wireStreamChunk) {
	if !r.started {
		r.started = true
		r.queue = append(r.queue, ir.EventMessageStart{ID: chunk.ID, Model: chunk.Model, Role: ir.RoleAssistant})
	}
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			if !r.textOpen {
				r.textOpen = true
				r.blockOrder = append(r.blockOrder, 0)
				r.queue = append(r.queue, ir.EventBlockStart{Index: 0, Kind: ir.BlockText})
			}
			r.queue = append(r.queue, ir.EventBlockDelta{Index: 0, Text: *choice.Delta.Content})
		}
		for _, tc := range choice.Delta.ToolCalls {
			idx, ok := r.toolBlocks[tc.Index]
			if !ok {
				idx = r.nextBlock
				r.nextBlock++
				r.toolBlocks[tc.Index] = idx
				r.blockOrder = append(r.blockOrder, idx)
				r.queue = append(r.queue, ir.EventBlockStart{
					Index: idx, Kind: ir.BlockToolUse, ToolName: tc.Function.Name, ToolCallID: tc.ID,
				})
			}
			if tc.Function.Arguments != "" {
				r.queue = append(r.queue, ir.EventBlockDelta{Index: idx, PartialJSON: tc.Function.Arguments})
			}
		}
		if choice.FinishReason != nil {
			for _, idx := range r.blockOrder {
				r.queue = append(r.queue, ir.EventBlockStop{Index: idx})
			}
			r.blockOrder = nil
			r.pendingFinish = choice.FinishReason
		}
	}
	if chunk.Usage != nil {
		u := chunk.Usage.toIR()
		r.pendingUsage = &u
		r.usageSource = ir.UsageUpstream
	}
	// 何时收尾：不要 usage 的话，finish_reason 一到就可以收尾；
	// 要 usage 的话，必须等到 usage 真的来了——否则要么提前丢 usage，
	// 要么（下面 finish() 的兜底）在 [DONE] 时才发现从来没等到。
	if r.pendingFinish != nil && (!r.includeUsage || r.pendingUsage != nil) {
		r.finish()
	}
}

func (r *StreamReader) finish() {
	if r.done {
		return
	}
	r.done = true
	if r.pendingFinish == nil {
		// 连正常的 finish_reason 都没收到就结束了——同样据实上报。
		r.queue = append(r.queue, ir.EventError{
			Code: "upstream_closed", Message: "upstream ended the stream without a finish_reason",
		})
		return
	}
	source := r.usageSource
	if r.pendingUsage == nil {
		// 请求要了 usage 但上游没给（quirk）：兜底为估算，
		// 而不是留一个 nil 让调用方各自猜测（计价 §4.1 的三档口径）。
		source = ir.UsageEstimated
	}
	r.queue = append(r.queue,
		ir.EventMessageDelta{FinishReason: mapFinishReason(r.pendingFinish), Usage: r.pendingUsage, UsageSource: source},
		ir.EventMessageStop{},
	)
}

// ── 编码：规范事件 → 客户端 SSE ──────────────────────────────────

// StreamWriter 把规范事件序列写成 OpenAI 形状的 SSE，供客户端 SDK 解析。
type StreamWriter struct {
	w             io.Writer
	id, model     string
	toolAnnounced map[int]bool // 每个工具调用块的 id/name 只在首次 delta 里给一次
}

func NewStreamWriter(w io.Writer, requestedModel string) *StreamWriter {
	return &StreamWriter{w: w, model: requestedModel, toolAnnounced: map[int]bool{}}
}

// Write 把一个规范事件编码成一行或多行 `data: ...\n\n`。
//
// 调用方负责在每次 Write 后 flush——本类型不认识 http.ResponseWriter，
// 保持这一层职责单一（ARCHITECTURE.md §6.1 的流式约束在网关层执行）。
func (sw *StreamWriter) Write(ev ir.StreamEvent) error {
	switch e := ev.(type) {
	case ir.EventMessageStart:
		sw.id = e.ID
		if e.Model != "" {
			// 不覆盖 model：出口必须回显客户端请求里写的原文，
			// 而不是上游内部的真实模型名（同 EncodeResponse 的理由）。
		}
		return sw.emit(wireStreamChunk{
			ID: sw.id, Model: sw.model,
			Choices: []wireStreamChoice{{Delta: wireStreamDelta{Role: "assistant"}}},
		})
	case ir.EventBlockStart:
		if e.Kind == ir.BlockToolUse {
			return sw.emit(wireStreamChunk{ID: sw.id, Model: sw.model, Choices: []wireStreamChoice{{
				Delta: wireStreamDelta{ToolCalls: []wireStreamToolCall{sw.announceTool(e)}},
			}}})
		}
		return nil // 文本块的开始不对应任何 OpenAI chunk，首个文本 delta 自然开始
	case ir.EventBlockDelta:
		if e.Text != "" {
			text := e.Text
			return sw.emit(wireStreamChunk{ID: sw.id, Model: sw.model, Choices: []wireStreamChoice{{
				Delta: wireStreamDelta{Content: &text},
			}}})
		}
		if e.PartialJSON != "" {
			tc := wireStreamToolCall{Index: e.Index}
			tc.Function.Arguments = e.PartialJSON
			return sw.emit(wireStreamChunk{ID: sw.id, Model: sw.model, Choices: []wireStreamChoice{{
				Delta: wireStreamDelta{ToolCalls: []wireStreamToolCall{tc}},
			}}})
		}
		return nil
	case ir.EventBlockStop:
		return nil // OpenAI 没有块结束的原生表示，finish_reason 一并带走
	case ir.EventMessageDelta:
		finish := finishReasonWire(e.FinishReason)
		chunk := wireStreamChunk{ID: sw.id, Model: sw.model, Choices: []wireStreamChoice{{
			Delta: wireStreamDelta{}, FinishReason: finish,
		}}}
		if err := sw.emit(chunk); err != nil {
			return err
		}
		if e.Usage != nil {
			return sw.emit(wireStreamChunk{ID: sw.id, Model: sw.model, Choices: []wireStreamChoice{}, Usage: fromIRUsage(*e.Usage)})
		}
		return nil
	case ir.EventMessageStop:
		_, err := io.WriteString(sw.w, "data: [DONE]\n\n")
		return err
	case ir.EventError:
		// **不是任何 OpenAI 原生事件**：中断时据实上报，绝不假装
		// finish_reason: stop（ARCHITECTURE.md §6.2 第 3 条）。
		// 用一个 error 载荷 + [DONE] 收尾，是社区里 OpenAI 兼容网关
		// 报告流中错误的通行做法——客户端 SDK 至少能读到 error 字段，
		// 而不是把截断的正文当成完整答案。
		payload := map[string]any{"error": map[string]any{
			"message": e.Message, "type": "upstream_error", "code": e.Code,
		}}
		if err := sw.emitRaw(payload); err != nil {
			return err
		}
		_, err := io.WriteString(sw.w, "data: [DONE]\n\n")
		return err
	default:
		return fmt.Errorf("openai: unknown stream event %T", ev)
	}
}

func (sw *StreamWriter) announceTool(e ir.EventBlockStart) wireStreamToolCall {
	tc := wireStreamToolCall{Index: e.Index, ID: e.ToolCallID}
	tc.Function.Name = e.ToolName
	sw.toolAnnounced[e.Index] = true
	return tc
}

func (sw *StreamWriter) emit(chunk wireStreamChunk) error {
	chunk.ID = sw.id
	return sw.emitRaw(chunk)
}

func (sw *StreamWriter) emitRaw(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("openai: encode stream chunk: %w", err)
	}
	if _, err := sw.w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := sw.w.Write(b); err != nil {
		return err
	}
	_, err = io.WriteString(sw.w, "\n\n")
	return err
}
