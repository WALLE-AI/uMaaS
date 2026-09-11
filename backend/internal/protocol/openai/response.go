package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

type wireUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
}

func (u *wireUsage) toIR() ir.Usage {
	if u == nil {
		return ir.Usage{}
	}
	out := ir.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
	if u.PromptTokensDetails != nil {
		out.CacheReadTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

func fromIRUsage(u ir.Usage) *wireUsage {
	w := &wireUsage{
		PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
		TotalTokens: u.PromptTokens + u.CompletionTokens,
	}
	if u.CacheReadTokens > 0 {
		w.PromptTokensDetails = &struct {
			CachedTokens int64 `json:"cached_tokens"`
		}{CachedTokens: u.CacheReadTokens}
	}
	if u.ReasoningTokens > 0 {
		w.CompletionTokensDetails = &struct {
			ReasoningTokens int64 `json:"reasoning_tokens"`
		}{ReasoningTokens: u.ReasoningTokens}
	}
	return w
}

type wireChoice struct {
	Index        int         `json:"index"`
	Message      wireMessage `json:"message"`
	FinishReason *string     `json:"finish_reason"`
}

type wireChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage,omitempty"`
}

// DecodeResponse 解析上游的非流式响应体。
//
// 只取第一个 choice：网关目前只请求 n=1（多候选不在 I3 范围内），
// 上游即使返回多个也只应该发生在客户端显式要求 n>1 时——那种情况
// 现在按错误处理更安全，而不是悄悄丢弃后续候选却让调用方以为完整。
func DecodeResponse(r io.Reader, requestedModel string) (*ir.Response, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("openai: read response body: %w", err)
	}
	var wire wireChatResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("openai: invalid response body: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, fmt.Errorf("openai: response has no choices")
	}
	choice := wire.Choices[0]

	msg := ir.Message{Role: ir.RoleAssistant}
	parts, err := decodeContent(choice.Message.Content)
	if err != nil {
		return nil, err
	}
	msg.Content = parts
	for _, tc := range choice.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ir.ToolCall{
			ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
		})
	}

	resp := &ir.Response{
		ID: wire.ID, Model: wire.Model, Message: msg,
		FinishReason: mapFinishReason(choice.FinishReason),
		CreatedAt:    time.Unix(wire.Created, 0).UTC(),
	}
	if wire.Usage != nil {
		resp.Usage = wire.Usage.toIR()
		resp.UsageSource = ir.UsageUpstream
	} else {
		resp.UsageSource = ir.UsageEstimated
	}
	if len(msg.ToolCalls) > 0 {
		resp.Usage.ToolCallCount = int64(len(msg.ToolCalls))
	}
	return resp, nil
}

func mapFinishReason(s *string) ir.FinishReason {
	if s == nil {
		return ""
	}
	switch *s {
	case "stop":
		return ir.FinishStop
	case "length":
		return ir.FinishLength
	case "tool_calls":
		return ir.FinishToolCalls
	case "content_filter":
		return ir.FinishContentFilter
	default:
		return ir.FinishReason(*s)
	}
}

func finishReasonWire(f ir.FinishReason) *string {
	if f == "" {
		return nil
	}
	s := string(f)
	// FinishError 不是 OpenAI 协议认识的原因；出口协议里它只能出现在
	// 我们自己构造的错误载荷里，不会走到这个函数——防御性地映射成
	// "stop" 会正是 ARCHITECTURE.md §6.2 明确禁止的"假装成功"，
	// 所以宁可让调用方在这里 panic 也不悄悄兼容。
	if f == ir.FinishError {
		panic("openai: FinishError must never be encoded as a normal finish_reason")
	}
	return &s
}

// EncodeResponse 把 IR 编回 OpenAI 形状，写给客户端。
func EncodeResponse(w io.Writer, resp *ir.Response, requestedModel string) error {
	wire := wireChatResponse{
		ID: resp.ID, Object: "chat.completion", Created: resp.CreatedAt.Unix(),
		// **模型字段回显客户端请求里写的原文**（UNIFIED §5.3 的 Requested 用途之一），
		// 不是上游实际服务的模型——OpenAI SDK 的使用者按请求里的名字核对响应。
		Model: requestedModel,
	}
	choice := wireChoice{Index: 0, FinishReason: finishReasonWire(resp.FinishReason)}
	choice.Message.Role = string(ir.RoleAssistant)
	if len(resp.Message.Content) == 1 && resp.Message.Content[0].Type == "text" {
		b, err := json.Marshal(resp.Message.Content[0].Text)
		if err != nil {
			return err
		}
		choice.Message.Content = b
	}
	for _, tc := range resp.Message.ToolCalls {
		wtc := wireToolCall{ID: tc.ID, Type: "function"}
		wtc.Function.Name = tc.Name
		wtc.Function.Arguments = tc.Arguments
		choice.Message.ToolCalls = append(choice.Message.ToolCalls, wtc)
	}
	wire.Choices = []wireChoice{choice}
	wire.Usage = fromIRUsage(resp.Usage)

	return json.NewEncoder(w).Encode(wire)
}
