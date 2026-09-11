// Package openai 是 OpenAI Chat Completions 协议与规范 IR 之间的转换器
// （UNIFIED-PROVIDER-INTERFACE.md §4.1 混合拓扑里的"高频路径直接转换器"）。
//
// I3 的范围只有 chat，含流式。这条路径两端都是 OpenAI 形状——
// 客户端说 OpenAI，约 30 家上游本身就是 OpenAI 兼容（§4.4）——
// 因此这里的转换在字段层面接近恒等，真正的价值在于：
//  1. 把"协议怎么长"和"业务怎么处理"解耦，为 I9+ 的 Anthropic/Gemini
//     转换器让出位置，而不需要回来重写这一层；
//  2. 流式的块生命周期显式化（§4.3），比 OpenAI 原生的扁平 delta 更适合
//     在网关层做超时判断、日志记录与（未来）跨协议转换。
package openai

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

// wireMessage 是请求体里一条消息的原始形状。Content 既可能是字符串，
// 也可能是多模态片段数组——OpenAI 两种都接受，这里都要能解析。
type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []wireToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// wireStreamOptions 只有一个字段有意义，但 OpenAI 把它包了一层对象。
type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireChatRequest struct {
	Model         string             `json:"model"`
	Messages      []wireMessage      `json:"messages"`
	Tools         []wireTool         `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	MaxTokens     *int64             `json:"max_tokens,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StreamOptions *wireStreamOptions `json:"stream_options,omitempty"`
	Stop          json.RawMessage    `json:"stop,omitempty"`
}

// DecodeChatRequest 解析客户端请求体，返回规范 IR 与客户端写的原始模型名
// （UNIFIED §5.3 的 ModelNames.Requested）。
func DecodeChatRequest(r io.Reader) (*ir.Request, string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 8<<20)) // 8MiB 上限：防止畸形超大请求体
	if err != nil {
		return nil, "", fmt.Errorf("openai: read request body: %w", err)
	}

	var wire wireChatRequest
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, "", fmt.Errorf("openai: invalid request body: %w", err)
	}
	if wire.Model == "" {
		return nil, "", fmt.Errorf("openai: model is required")
	}
	if len(wire.Messages) == 0 {
		return nil, "", fmt.Errorf("openai: messages must not be empty")
	}

	req := &ir.Request{
		Temperature: wire.Temperature,
		TopP:        wire.TopP,
		MaxTokens:   wire.MaxTokens,
		Stream:      wire.Stream,
	}
	if wire.StreamOptions != nil {
		req.StreamUsage = wire.StreamOptions.IncludeUsage
	}
	if len(wire.Stop) > 0 {
		req.Stop, err = decodeStop(wire.Stop)
		if err != nil {
			return nil, "", err
		}
	}
	if len(wire.ToolChoice) > 0 {
		req.ToolChoice, err = decodeToolChoice(wire.ToolChoice)
		if err != nil {
			return nil, "", err
		}
	}

	for _, t := range wire.Tools {
		req.Tools = append(req.Tools, ir.Tool{
			Name: t.Function.Name, Description: t.Function.Description,
			Parameters: t.Function.Parameters,
		})
	}

	for _, m := range wire.Messages {
		msg := ir.Message{Role: ir.Role(m.Role), Name: m.Name, ToolCallID: m.ToolCallID}
		parts, err := decodeContent(m.Content)
		if err != nil {
			return nil, "", fmt.Errorf("openai: message content: %w", err)
		}
		msg.Content = parts
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ir.ToolCall{
				ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
			})
		}
		req.Messages = append(req.Messages, msg)
	}

	return req, wire.Model, nil
}

func decodeContent(raw json.RawMessage) ([]ir.ContentPart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// 字符串内容是绝大多数请求的形状，单独走一条快路径。
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []ir.ContentPart{{Type: "text", Text: s}}, nil
	}
	var parts []wireContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	out := make([]ir.ContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, ir.ContentPart{Type: "text", Text: p.Text})
		case "image_url":
			out = append(out, ir.ContentPart{Type: "image_url", ImageURL: p.ImageURL.URL})
		default:
			// 不认识的片段类型透传为 text 空占位会丢信息也会误导；
			// 直接跳过并让上游自己决定要不要报错，比我们猜一个类型更安全。
		}
	}
	return out, nil
}

func decodeStop(raw json.RawMessage) ([]string, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("openai: invalid stop: %w", err)
		}
		return []string{s}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("openai: invalid stop: %w", err)
	}
	return list, nil
}

func decodeToolChoice(raw json.RawMessage) (string, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("openai: invalid tool_choice: %w", err)
		}
		return s, nil
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("openai: invalid tool_choice: %w", err)
	}
	return obj.Function.Name, nil
}

// EncodeUpstreamRequest 把 IR 编回 OpenAI 形状的请求体，发给上游。
//
// I3 只对接 OpenAI 兼容上游（§4.4 约 30 家），因此这一步在字段层面接近
// 恒等——真正的价值在于 upstreamModel 覆盖：客户端说的模型名与上游
// 实际要接受的名字可能不同（UNIFIED §5.3 的 Upstream 名）。
func EncodeUpstreamRequest(req *ir.Request, upstreamModel string) ([]byte, error) {
	wire := wireChatRequest{
		Model: upstreamModel, Temperature: req.Temperature, TopP: req.TopP,
		MaxTokens: req.MaxTokens, Stream: req.Stream,
	}
	if req.Stream && req.StreamUsage {
		wire.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	}
	if len(req.Stop) == 1 {
		b, err := json.Marshal(req.Stop[0])
		if err != nil {
			return nil, err
		}
		wire.Stop = b
	} else if len(req.Stop) > 1 {
		b, err := json.Marshal(req.Stop)
		if err != nil {
			return nil, err
		}
		wire.Stop = b
	}
	if req.ToolChoice != "" {
		b, err := json.Marshal(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		wire.ToolChoice = b
	}
	for _, t := range req.Tools {
		wt := wireTool{Type: "function"}
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Parameters
		wire.Tools = append(wire.Tools, wt)
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Name: m.Name, ToolCallID: m.ToolCallID}
		if len(m.Content) == 1 && m.Content[0].Type == "text" {
			b, err := json.Marshal(m.Content[0].Text)
			if err != nil {
				return nil, err
			}
			wm.Content = b
		} else if len(m.Content) > 0 {
			parts := make([]wireContentPart, 0, len(m.Content))
			for _, p := range m.Content {
				wp := wireContentPart{Type: p.Type, Text: p.Text}
				wp.ImageURL.URL = p.ImageURL
				parts = append(parts, wp)
			}
			b, err := json.Marshal(parts)
			if err != nil {
				return nil, err
			}
			wm.Content = b
		}
		for _, tc := range m.ToolCalls {
			wtc := wireToolCall{ID: tc.ID, Type: "function"}
			wtc.Function.Name = tc.Name
			wtc.Function.Arguments = tc.Arguments
			wm.ToolCalls = append(wm.ToolCalls, wtc)
		}
		wire.Messages = append(wire.Messages, wm)
	}
	return json.Marshal(wire)
}
