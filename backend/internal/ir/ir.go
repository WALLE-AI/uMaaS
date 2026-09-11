// Package ir 是协议无关的规范表示（Canonical IR，UNIFIED-PROVIDER-INTERFACE.md §4.1）。
//
// 这个包**不认识 HTTP，不认识任何具体协议**，只认识"一次对话请求/响应长什么样"。
// 之所以不直接拿 OpenAI 格式当中间表示：隐式以某一家协议为中枢，会让
// 转到别的协议时被迫先降级到中枢协议、再降级到目标协议，等于降级两次。
//
// I3 只落地 chat 能力与 OpenAI 协议（P1 的判据是"OpenAI SDK 直连可用"）。
// IR 的字段形状已经按四种协议的并集设计（§4.1），是为了 P4/P5（Anthropic/Gemini，
// I9+）接入时不用回来改这个包——但那两个协议的转换器现在不存在。
package ir

import "time"

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentPart 是消息内容的一个片段。多数消息只有一个 text 片段，
// 但多模态输入（image_url）与工具结果都需要多片段表示。
type ContentPart struct {
	Type     string `json:"type"` // text | image_url
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ToolCall 是助手消息里发起的一次工具调用。
//
// Arguments 在流式场景下会跨多个 chunk 拼接（§4.3-1），因此这里存的是
// 拼接完成后的最终 JSON 字符串，而不是已解析的对象——**不能边收边解析**，
// 中间态必然是非法 JSON，解析交给调用方在拿到完整字符串后做。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message 是一条对话消息。
type Message struct {
	Role       Role          `json:"role"`
	Content    []ContentPart `json:"content,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // role=tool 时，对应哪次调用
	Name       string        `json:"name,omitempty"`
}

// Text 是单文本片段消息的便捷构造。绝大多数请求属于这一类，
// 到处手写 []ContentPart{{Type:"text",...}} 只会让调用点变得难读。
func Text(role Role, text string) Message {
	return Message{Role: role, Content: []ContentPart{{Type: "text", Text: text}}}
}

// PlainText 拼接一条消息里全部 text 片段。
// 图片等非文本片段不计入——调用方需要纯文本时（如 tokenizer 估算），
// 这是唯一正确的取法。
func (m Message) PlainText() string {
	var out string
	for _, p := range m.Content {
		if p.Type == "text" {
			out += p.Text
		}
	}
	return out
}

// Tool 是客户端声明的可用工具。
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  []byte `json:"parameters,omitempty"` // JSON Schema，原样透传
}

// Request 是一次 chat 请求的规范表示。
type Request struct {
	Messages    []Message
	Tools       []Tool
	ToolChoice  string // "auto" | "none" | "required" | 具体工具名
	Temperature *float64
	TopP        *float64
	MaxTokens   *int64
	Stream      bool
	// StreamUsage 表示客户端要求在流式响应里带上 usage
	// （OpenAI 的 stream_options.include_usage）。
	StreamUsage bool
	Stop        []string
}

// FinishReason 是响应或流结束的原因。
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	// FinishError 不是任何协议的原生结束原因，是我们自己加的：
	// 中断的流据实上报时用它，绝不能伪装成 FinishStop
	// （ARCHITECTURE.md §6.2 第 3 条："不要假装成功"）。
	FinishError FinishReason = "error"
)

// Usage 统一三种协议的用量口径（§4.3-3：usage 到达时机不同，规范层统一汇总）。
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	// 以下三个此前无处安放（BILLING-AND-PRICING.md §3.1）：
	// 埋点先于价目表存在，现在两者要对齐。
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	ToolCallCount    int64
}

// Response 是非流式响应的规范表示。
type Response struct {
	ID           string
	Model        string // 上游实际返回的模型名，未必等于请求里的
	Message      Message
	FinishReason FinishReason
	Usage        Usage
	// UsageSource 记录 Usage 从哪来（计价 §4.1 的三档可信度）。
	// gateway 落地 request_logs 时直接读这个字段，不再重新判断一遍。
	UsageSource UsageSource
	CreatedAt   time.Time
}

// UsageSource 是用量的可信度分级（BILLING-AND-PRICING.md §4.1）。
type UsageSource string

const (
	// UsageUpstream 是上游返回的真实用量。**以它为准，即使我们认为算错了**，
	// 否则与上游账单的对账永远对不平。
	UsageUpstream   UsageSource = "upstream"
	UsageSelfHosted UsageSource = "self_hosted"
	// UsageEstimated 是上游不返回 usage 时，本地 tokenizer 的估算兜底。
	// 三条纪律见计价 §4.1：不得当精确值计费、偏保守、占比要能监控。
	UsageEstimated UsageSource = "estimated"
)
