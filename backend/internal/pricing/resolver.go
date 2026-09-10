package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
)

// Key 是一次价目解析的输入。
//
// BillingModel 是 ModelNames.Billing（UNIFIED §5.3），**不是路由用的模型名**。
// 促销别名 gpt-5.6-sol-promo 的 Billing 是它自己（打折价），Resolved/Upstream
// 都是真实模型——用同一个字段的话，要么路由找不到渠道，要么计费算成原价。
type Key struct {
	BillingModel string
	// FallbackBillingModel 是"计费别名没配价时退回模型默认价"这一层（§3.4 最后一级）。
	// 通常是模型的规范 ID（provider/slug）。
	FallbackBillingModel string
	Plan                 string
	WorkspaceID          string
}

// Repository 是价目的持久化端口。实现在 store/postgres。
type Repository interface {
	// ResolveVersion 按 §3.4 的四层优先级返回 at 时刻生效的价目版本。
	// 没有任何一层命中时返回 domain.ErrNotFound。
	ResolveVersion(ctx context.Context, key Key, at time.Time) (*Version, error)
}

// Resolver 是**展示与结算共用的价目入口**。
//
// catalog handler 与将来的 BillingSession 都只能拿到这个类型，
// 谁都不许自己去查 model_prices——这是 M1b 判据的实现方式：
// 用类型强制，而不是靠"注意保持一致"。
type Resolver struct {
	repo Repository
}

func NewResolver(repo Repository) *Resolver { return &Resolver{repo: repo} }

// ForDisplay 解析目录页要展示的价格：**default 档**（§3.6-② 推论）。
//
// 未登录用户也要能看价，所以展示价不带 workspace/plan。
// 某个 workspace 有协议价时，它在 console 里看到的价格与公开目录不同——
// 这是对的，但 UI 上要标明"您的协议价"，否则会被当成 bug 报上来。
func (r *Resolver) ForDisplay(ctx context.Context, billingModel, fallback string, at time.Time) (*Version, error) {
	return r.repo.ResolveVersion(ctx, Key{
		BillingModel:         billingModel,
		FallbackBillingModel: fallback,
	}, at)
}

// ForBilling 解析结算要用的价格：带上 plan 与 workspace 的协议价。
//
// at **必须**是 RequestContext.ReceivedAt 而不是 now()：一次跨越改价时刻的
// 长请求（流式可以跑几分钟）要整体按受理时的价目结算，不能中途换价（§3.3-3）。
func (r *Resolver) ForBilling(ctx context.Context, key Key, receivedAt time.Time) (*Version, error) {
	if receivedAt.IsZero() {
		return nil, fmt.Errorf("pricing: ForBilling requires the request's received_at, not a zero time")
	}
	return r.repo.ResolveVersion(ctx, key, receivedAt)
}

// DecodeVersion 从 to_jsonb(model_prices 行) 还原一个价目版本。
//
// 目录列表要在 SQL 里按价格排序过滤（Go 侧做不到），因此价目版本是随列表一起
// 带回来的。这个函数保证**那条路径与 ResolveVersion 走同一个费率解析器**——
// 两个入口，一份 ParseRates。
func DecodeVersion(raw []byte) (*Version, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var row struct {
		ID            int64           `json:"id"`
		BillingModel  string          `json:"billing_model"`
		ScopeKind     string          `json:"scope_kind"`
		ScopeID       *string         `json:"scope_id"`
		Currency      string          `json:"currency"`
		Rates         json.RawMessage `json:"rates"`
		EffectiveFrom time.Time       `json:"effective_from"`
		Source        string          `json:"source"`
		AppliedBy     string          `json:"applied_by"`
		Note          string          `json:"note"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("pricing: decode price version: %w", err)
	}
	rates, err := ParseRates(row.Rates)
	if err != nil {
		return nil, err
	}
	v := &Version{
		ID: row.ID, BillingModel: row.BillingModel, ScopeKind: row.ScopeKind,
		Currency: row.Currency, Rates: rates, EffectiveFrom: row.EffectiveFrom,
		Source: row.Source, AppliedBy: row.AppliedBy, Note: row.Note,
	}
	if row.ScopeID != nil {
		v.ScopeID = *row.ScopeID
	}
	if v.Currency == "" {
		v.Currency = "USD"
	}
	return v, nil
}

// NotPriced 是"这个计费主体在该时刻没有任何价目"的错误。
//
// 它是 404 而不是 500：draft 状态的模型本来就可以没有价目（§3.6-③），
// 拿它当内部错误会让 admin 的正常工作流刷一屏告警。
func NotPriced(billingModel string) error {
	return domain.NotFound("model_not_priced",
		fmt.Sprintf("no effective price version for billing model %q", billingModel))
}
