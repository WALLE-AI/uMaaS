package gateway

import (
	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

// Manifest 是一个模型声明自己支持什么（UNIFIED-PROVIDER-INTERFACE.md §5.4）。
//
// I4 只落地能覆盖目录 capabilities 字段（I2 已有）的最小集合——
// 完整的 Capability Manifest（含 max_images、strict_schema 等细粒度字段）
// 要等上游 /models 同步接进来才有数据源，现在手填 1000 个模型的这些
// 字段不现实。**能力协商的价值不在字段多，在于"请求要的能力 ∩ 模型
// 声明的能力"这次相减动作本身**——先把这个动作做对，字段可以逐步加。
type Manifest struct {
	Streaming bool
	Tools     bool
	Vision    bool
	Reasoning bool
}

// ManifestFromCapabilities 把 models.capabilities（I2 的 text[]）翻译成
// Manifest。streaming 恒为 true——I3/I4 还没有遇到不支持流式的 OpenAI
// 兼容上游，真遇到时应该是 Quirks 的一个字段，而不是从 capabilities 猜。
func ManifestFromCapabilities(capabilities []string) Manifest {
	m := Manifest{Streaming: true}
	for _, c := range capabilities {
		switch c {
		case "tools":
			m.Tools = true
		case "vision":
			m.Vision = true
		case "reasoning":
			m.Reasoning = true
		}
	}
	return m
}

// NegotiationError 是能力协商失败的结果。
//
// **协商层存在的全部意义**：没有它，用户把请求从支持工具调用的模型 A
// 换到不支持的模型 B，得到的是上游一个含义不明的 400；有协商层则能
// 在发出请求前给出准确错误（UNIFIED §5.4）。
type NegotiationError struct {
	Capability string
	Message    string
}

func (e *NegotiationError) Error() string { return e.Message }

// Negotiate 检查请求所需能力是否被模型声明支持。
//
// 只做**硬性拒绝**，不做降级模拟（如"目标不支持 JSON mode 就用 prompt
// 模拟"）——那是协议转换层的 Loss Policy（UNIFIED §4.2），要等 P4/P5
// 的跨协议转换器接入后才有意义。I4 只有 OpenAI→OpenAI 兼容一条高频
// 路径，字段形状本来就一致，唯一会真实冲突的是**模型能力**而不是协议差异。
func Negotiate(req *ir.Request, m Manifest) error {
	if len(req.Tools) > 0 && !m.Tools {
		return &NegotiationError{Capability: "tools",
			Message: "the requested model does not support tool calling"}
	}
	if !m.Vision {
		for _, msg := range req.Messages {
			for _, part := range msg.Content {
				if part.Type == "image_url" {
					return &NegotiationError{Capability: "vision",
						Message: "the requested model does not support image input"}
				}
			}
		}
	}
	if req.Stream && !m.Streaming {
		return &NegotiationError{Capability: "streaming",
			Message: "the requested model does not support streaming responses"}
	}
	return nil
}
