package gateway

import "context"

// ResolvedModel 是"客户端请求的这个模型 ID 对应目录里的哪一条"。
//
// **不含"上游实际叫什么"**——那是 per-渠道的信息（channel_models.
// upstream_model_name，I4 起随 Candidate 一起从 Router.Rank 拿），同一个
// 模型在不同渠道上可能叫不同的名字，不该固化在模型解析这一步。
type ResolvedModel struct {
	ID           int64
	BillingModel string
	Hosting      string
	// Capabilities 供能力协商使用（negotiate.go）——请求所需能力必须是
	// 模型声明能力的子集，否则在发出任何上游请求前就该给出准确错误
	// （UNIFIED-PROVIDER-INTERFACE.md §5.4）。
	Capabilities []string
}

// ModelRepository 是数据平面需要的目录只读端口。
//
// 与 internal/catalog.Repository 分开：那个接口服务"给人看的目录页"，
// 只暴露 listed；这个接口服务"请求能不能被转发"，还要放行 canary
// （BILLING-AND-PRICING.md §3.6-③：canary 是"可调用但不公开可见"）。
type ModelRepository interface {
	// ResolveCallable 返回 id 命中且状态可调用（listed|canary）的模型。
	// 找不到时返回 domain.ErrNotFound，网关据此给客户端一个清晰的
	// model_not_found，而不是把"没有这个模型"和"上游炸了"混在一起。
	ResolveCallable(ctx context.Context, providerSlug, modelSlug string) (*ResolvedModel, error)
}
