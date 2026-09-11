package gateway

// ModelNames 是四个模型名的分离（UNIFIED-PROVIDER-INTERFACE.md §5.3）。
//
// 合并任意两个都会在某个场景出错——一个具体例子：促销别名
// `gpt-5.6-sol-promo` 的 Billing 是它自己（打折价），Resolved 和
// Upstream 都是真实模型。若用同一个字段，要么路由找不到渠道，
// 要么计费算成原价。**Billing 绝不参与路由**，反过来"路由不参与计费"
// 同样必须成立（BILLING-AND-PRICING.md §4.2）。
type ModelNames struct {
	// Requested 是客户端写的原文。用途：日志、错误消息、响应体里的
	// model 字段回显（EncodeResponse 已经在这么做）。
	Requested string
	// Resolved 是解析别名/版本后的规范 ID，这里就是目录里的 `provider/slug`。
	// 用途：渠道选择、能力协商。
	Resolved string
	// Billing 是计费身份，可以是虚拟别名。用途：仅计价，绝不参与路由。
	Billing string
	// Upstream 在 I3 恒等于 Resolved 的 slug 部分；I4 起改成
	// channel_models.upstream_model_name——同一个模型在不同渠道上
	// 可能叫不同的名字。
	Upstream string
}
