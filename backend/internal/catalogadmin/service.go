// Package catalogadmin 是目录与价目的**写入侧**（M1a）。
//
// 它与只读的 internal/catalog 分开，不是为了对称好看，而是因为两者的
// 关注点完全不同：读侧关心形状与缓存，写侧关心**校验、留痕与状态机**。
//
// 一条贯穿本包的判断（计价 §3.6-④）：既然价目由人手工填，"填错"就是常态
// 而非异常，因此**全部校验前移到提交路径**，运行时只做兜底。
// 发布时拦住的成本，远低于运行时处理一个"能被调用却没有价目"的模型——
// 后者只有两种结局：按 0 计费（白送）或直接 500。
package catalogadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

// Model 是 admin 视角的模型（四态全见，web 只见 listed）。
type Model struct {
	ID              int64
	ProviderSlug    string
	ProviderName    string
	Slug            string
	DisplayName     string
	Description     string
	Modalities      []string
	Capabilities    []string
	ContextLength   *int32
	MaxOutputTokens *int32
	Hosting         string
	BillingModel    string
	Status          string
	CanaryPercent   *int32
	CallsLast7d     int64
	ReleasedAt      time.Time
	// Price 为 nil = 还没有价目。draft 允许，listed/canary 不允许。
	Price *pricing.Version
}

// PublicID 是 `provider/model`，也是价目回退时用的模型默认计费键。
func (m Model) PublicID() string { return m.ProviderSlug + "/" + m.Slug }

// UsageWindow 是近 7 日的用量与两本账，供改价预检使用。
//
// CostAvailable 为 false 表示成本侧还没有数据（M2 之前恒为 false）。
// **这个标记必须存在**：把"没有成本数据"显示成"成本为 0"，
// 毛利预览会给出一个 100% 的毛利率，而那正是最需要被质疑的数字。
type UsageWindow struct {
	Calls             int64
	InputTokens       int64
	OutputTokens      int64
	UpstreamCostNano  int64
	ChargedAmountNano int64
	CostAvailable     bool
}

// Repository 是写入侧的持久化端口。
type Repository interface {
	UpsertProvider(ctx context.Context, in ProviderInput) (int64, error)
	UpsertModel(ctx context.Context, providerID int64, in ModelInput) (*Model, error)
	ModelByID(ctx context.Context, id int64) (*Model, error)
	ListModels(ctx context.Context, status string) ([]Model, error)
	SetStatus(ctx context.Context, id int64, status string, canaryPercent *int32) (*Model, error)
	RecentUsage(ctx context.Context, modelID int64) (UsageWindow, error)
	AppendActivity(ctx context.Context, modelID int64, kind, title string) error

	PublishPrice(ctx context.Context, in PricePublication) (*pricing.Version, error)
	PriceHistory(ctx context.Context, billingModel string, limit int32) ([]*pricing.Version, error)
}

type ProviderInput struct {
	Slug        string
	Name        string
	Kind        string
	LogoURL     string
	Description string
}

type ModelInput struct {
	Slug                string
	DisplayName         string
	Description         string
	LogoURL             string
	Modalities          []string
	Capabilities        []string
	ContextLength       *int32
	MaxOutputTokens     *int32
	Architecture        *string
	InputFormats        []string
	OutputFormats       []string
	SupportedParameters []string
	ZeroDataRetention   bool
	OpenWeights         bool
	Hosting             string
	BillingModel        string
	ReleasedAt          time.Time
}

type PricePublication struct {
	BillingModel  string
	ScopeKind     string
	ScopeID       string
	Currency      string
	Rates         json.RawMessage
	EffectiveFrom time.Time
	Source        string
	AppliedBy     string
	Note          string
}

// Service 是 admin 的目录写入服务。
type Service struct {
	repo  Repository
	audit domain.AuditRepository
}

func NewService(repo Repository, audit domain.AuditRepository) *Service {
	return &Service{repo: repo, audit: audit}
}

// Actor 是执行操作的管理员。审计里的 actor 必须能一直解析出人名，
// 因此这里带的是标签而不只是 id（ARCHITECTURE.md §4.5）。
type Actor struct {
	ID        int64
	Label     string
	IP        string
	RequestID string
}

func (s *Service) List(ctx context.Context, status string) ([]Model, error) {
	return s.repo.ListModels(ctx, status)
}

func (s *Service) Get(ctx context.Context, id int64) (*Model, error) {
	return s.repo.ModelByID(ctx, id)
}

// UpsertModel 写模型元数据。**不碰 status**：发布与下架是独立动作，
// 因为它们要校验价目完整性，而一次元数据编辑不该顺手把模型发布出去。
func (s *Service) UpsertModel(ctx context.Context, actor Actor, provider ProviderInput, in ModelInput) (*Model, error) {
	if provider.Slug == "" || in.Slug == "" {
		return nil, domain.Validation("slug_required", "provider slug and model slug are required", "slug")
	}
	if in.DisplayName == "" {
		return nil, domain.Validation("name_required", "display_name is required", "display_name")
	}
	if in.Hosting != "cloud" && in.Hosting != "self" {
		return nil, domain.Validation("invalid_hosting", "hosting must be cloud or self", "hosting")
	}
	// 计费键缺省为模型的规范 ID。让它可覆盖是为了促销别名（UNIFIED §5.3），
	// 但**绝不能让它落到空字符串**——那样所有未填的模型会共用一份价目。
	if in.BillingModel == "" {
		in.BillingModel = provider.Slug + "/" + in.Slug
	}
	if in.ReleasedAt.IsZero() {
		in.ReleasedAt = time.Now().UTC()
	}
	if provider.Kind == "" {
		provider.Kind = "cloud"
	}

	providerID, err := s.repo.UpsertProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	m, err := s.repo.UpsertModel(ctx, providerID, in)
	if err != nil {
		return nil, err
	}
	s.record(ctx, actor, "catalog.model.upsert", m.PublicID(), false, map[string]any{
		"billing_model": m.BillingModel, "hosting": m.Hosting,
	})
	return m, nil
}

// ── 改价 ────────────────────────────────────────────────────────

// PriceProposal 是一次改价提案。
type PriceProposal struct {
	ModelID       int64
	ScopeKind     string
	ScopeID       string
	Currency      string
	Rates         json.RawMessage
	EffectiveFrom time.Time
	Note          string
	Source        string
	// Confirm 表示管理员已经看过警告并二次确认。
	// 有 warn 而没有 Confirm 时**拒绝保存**——§3.6-④ 那条"毛利为负必须
	// 二次确认，不能静默保存"，实现上就是这个开关。
	Confirm bool
}

// MarginPreview 是改价的毛利影响预览（§3.3 末、§8-2）。
//
// 光看价格数字的变化不足以判断该不该改：涨 10% 听起来无害，
// 但如果这个模型的毛利率本来就是 -5%，涨 10% 依然是亏的。
type MarginPreview struct {
	WindowDays int `json:"window_days"`
	// DataAvailable 为 false 时下面的数字全部无意义，UI 必须显示"暂无用量数据"
	// 而不是显示一堆 0。
	DataAvailable bool `json:"data_available"`
	// CostAvailable 单独一个标记：可能有用量但还没有成本（M2 之前就是如此）。
	CostAvailable        bool     `json:"cost_available"`
	Calls                int64    `json:"calls"`
	InputTokens          int64    `json:"input_tokens"`
	OutputTokens         int64    `json:"output_tokens"`
	CurrentRevenueNano   int64    `json:"current_revenue_nano"`
	ProjectedRevenueNano int64    `json:"projected_revenue_nano"`
	UpstreamCostNano     int64    `json:"upstream_cost_nano"`
	ProjectedMarginNano  int64    `json:"projected_margin_nano"`
	ProjectedMarginRate  *float64 `json:"projected_margin_rate"`
}

// PreviewResult 是提交前的完整体检结果。
type PreviewResult struct {
	Issues  []pricing.Issue `json:"issues"`
	Margin  MarginPreview   `json:"margin"`
	Blocked bool            `json:"blocked"`
	// NeedsConfirm 为 true 表示有警告，需要管理员二次确认才能保存。
	NeedsConfirm bool `json:"needs_confirm"`
}

// PreviewPrice 只做体检，不写库。
func (s *Service) PreviewPrice(ctx context.Context, p PriceProposal) (*PreviewResult, *Model, *pricing.Rates, error) {
	m, err := s.repo.ModelByID(ctx, p.ModelID)
	if err != nil {
		return nil, nil, nil, err
	}
	rates, err := pricing.ParseRates(p.Rates)
	if err != nil {
		return nil, nil, nil, domain.Validation("invalid_rates", err.Error(), "rates")
	}
	usage, err := s.repo.RecentUsage(ctx, m.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	issues := pricing.Validate(pricing.ValidateInput{
		Rates:           rates,
		Capabilities:    m.Capabilities,
		Hosting:         m.Hosting,
		Note:            p.Note,
		RecentCallCount: usage.Calls,
	})

	margin := s.margin(&pricing.Version{Rates: rates}, usage)
	if margin.DataAvailable && margin.CostAvailable && margin.ProjectedMarginNano < 0 {
		issues = append(issues, pricing.Issue{
			Severity: pricing.SeverityWarn, Code: "negative_margin",
			Message: fmt.Sprintf("按近 7 日用量测算，新价的毛利为 $%.4f（负）",
				pricing.NanoToUSD(margin.ProjectedMarginNano)),
			Field: "rates",
		})
	}

	res := &PreviewResult{
		Issues: issues, Margin: margin,
		Blocked: pricing.HasErrors(issues),
	}
	for _, i := range issues {
		if i.Severity == pricing.SeverityWarn {
			res.NeedsConfirm = true
		}
	}
	return res, m, rates, nil
}

const marginWindowDays = 7

func (s *Service) margin(v *pricing.Version, u UsageWindow) MarginPreview {
	m := MarginPreview{
		WindowDays:         marginWindowDays,
		DataAvailable:      u.Calls > 0,
		CostAvailable:      u.CostAvailable,
		Calls:              u.Calls,
		InputTokens:        u.InputTokens,
		OutputTokens:       u.OutputTokens,
		CurrentRevenueNano: u.ChargedAmountNano,
		UpstreamCostNano:   u.UpstreamCostNano,
	}
	if !m.DataAvailable {
		return m
	}
	// 用**同一个 Charge 函数**测算，而不是在这里写一遍乘法：
	// 预览与实际收费必须是同一套算法，否则预览就是安慰剂。
	projected, _, err := v.Charge(pricing.Usage{
		InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, Requests: u.Calls,
	})
	if err != nil {
		return m
	}
	m.ProjectedRevenueNano = projected
	if u.CostAvailable {
		m.ProjectedMarginNano = projected - u.UpstreamCostNano
		if projected > 0 {
			rate := float64(m.ProjectedMarginNano) / float64(projected)
			m.ProjectedMarginRate = &rate
		}
	}
	return m
}

// PublishPrice 校验通过后写入新版本。
//
// **改价标 destructive = true**（§3.6-⑥）。它不删除任何数据，但它直接改变收入，
// 是审计时最需要一眼看到的一类操作。
func (s *Service) PublishPrice(ctx context.Context, actor Actor, p PriceProposal) (*pricing.Version, *PreviewResult, error) {
	preview, m, _, err := s.PreviewPrice(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	if preview.Blocked {
		return nil, preview, domain.Validation("price_validation_failed",
			"price submission has blocking issues", "rates")
	}
	if preview.NeedsConfirm && !p.Confirm {
		return nil, preview, domain.Validation("confirmation_required",
			"price submission has warnings; resubmit with confirm=true after reviewing them", "confirm")
	}

	scopeKind := p.ScopeKind
	if scopeKind == "" {
		scopeKind = "default"
	}
	switch scopeKind {
	case "default", "plan", "workspace":
	default:
		return nil, preview, domain.Validation("invalid_scope_kind",
			"scope_kind must be default, plan or workspace", "scope_kind")
	}
	if scopeKind != "default" && p.ScopeID == "" {
		return nil, preview, domain.Validation("scope_id_required",
			"scope_id is required for plan and workspace prices", "scope_id")
	}
	effectiveFrom := p.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = time.Now().UTC()
	}
	source := p.Source
	if source == "" {
		source = "admin"
	}
	if source != "admin" && source != "upstream_diff_accepted" {
		return nil, preview, domain.Validation("invalid_source",
			"source must be admin or upstream_diff_accepted", "source")
	}
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}

	version, err := s.repo.PublishPrice(ctx, PricePublication{
		BillingModel: m.BillingModel, ScopeKind: scopeKind, ScopeID: p.ScopeID,
		Currency: currency, Rates: p.Rates, EffectiveFrom: effectiveFrom,
		Source: source, AppliedBy: actor.Label, Note: p.Note,
	})
	if err != nil {
		return nil, preview, err
	}

	// 详情页的 Activity 里留一条。用户问"为什么这个月贵了"时，
	// 页面上就能自证，不必等客服去翻审计日志。
	if scopeKind == "default" {
		_ = s.repo.AppendActivity(ctx, m.ID, "pricing", "价格已更新")
	}
	s.record(ctx, actor, "pricing.update", m.PublicID(), true, map[string]any{
		"price_version_id": version.ID,
		"billing_model":    version.BillingModel,
		"scope_kind":       version.ScopeKind,
		"scope_id":         version.ScopeID,
		"effective_from":   version.EffectiveFrom,
		"note":             p.Note,
		"calls_last_7d":    preview.Margin.Calls,
	})
	return version, preview, nil
}

func (s *Service) PriceHistory(ctx context.Context, modelID int64, limit int32) ([]*pricing.Version, error) {
	m, err := s.repo.ModelByID(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.PriceHistory(ctx, m.BillingModel, limit)
}

// ── 状态机（§3.6-③）───────────────────────────────────────────
//
//	状态       web 可见   可调用   价目要求
//	draft      否         否       可以没有价目
//	canary     否         是       **必须有价目**——有调用就有计费
//	listed     是         是       **必须有价目**
//	delisted   否         否       **价目不可删**，历史账单要能重算

// Publish 把模型发布为 listed 或 canary。
func (s *Service) Publish(ctx context.Context, actor Actor, modelID int64, status string, canaryPercent *int32) (*Model, error) {
	if status != "listed" && status != "canary" {
		return nil, domain.Validation("invalid_status",
			"publish target must be listed or canary", "status")
	}
	m, err := s.repo.ModelByID(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if status == "canary" && (canaryPercent == nil || *canaryPercent <= 0 || *canaryPercent > 100) {
		return nil, domain.Validation("invalid_canary_percent",
			"canary_percent must be between 1 and 100", "canary_percent")
	}

	// **这是 M1a 的核心判据**：无价目的模型不能被发布为 listed/canary。
	// 一个能被调用却没有价目的模型，请求进来时只有两种结局——
	// 按 0 计费（白送）或直接 500。两种都不可接受。
	if m.Price == nil {
		return nil, domain.Validation("model_not_priced",
			"模型没有生效的价目，不能发布。请先提交价目（计价方案 §3.6-③）", "status")
	}
	issues := pricing.Validate(pricing.ValidateInput{
		Rates: m.Price.Rates, Capabilities: m.Capabilities, Hosting: m.Hosting,
	})
	if pricing.HasErrors(issues) {
		return nil, &domain.Error{
			Kind: domain.ErrValidation, Code: "price_incomplete",
			Message: fmt.Sprintf("现有价目未通过完整性校验：%s", summarize(issues)),
			Field:   "status",
		}
	}

	updated, err := s.repo.SetStatus(ctx, modelID, status, canaryPercent)
	if err != nil {
		return nil, err
	}
	_ = s.repo.AppendActivity(ctx, modelID, "status", "模型已发布为 "+status)
	// 发布改变可调用性与收入，属于破坏性操作。
	s.record(ctx, actor, "catalog.model.publish", m.PublicID(), true, map[string]any{
		"from": m.Status, "to": status, "price_version_id": m.Price.ID,
	})
	return updated, nil
}

// Delist 下架。**只关闭可见性与新调用，绝不删除价目版本**——
// 那是 §3.3 版本化的直接推论：历史账单要能重算。
func (s *Service) Delist(ctx context.Context, actor Actor, modelID int64, reason string) (*Model, error) {
	m, err := s.repo.ModelByID(ctx, modelID)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.SetStatus(ctx, modelID, "delisted", nil)
	if err != nil {
		return nil, err
	}
	_ = s.repo.AppendActivity(ctx, modelID, "status", "模型已下架")
	s.record(ctx, actor, "catalog.model.delist", m.PublicID(), true, map[string]any{
		"from": m.Status, "reason": reason,
	})
	return updated, nil
}

func (s *Service) record(ctx context.Context, actor Actor, action, target string, destructive bool, detail map[string]any) {
	if s.audit == nil {
		return
	}
	id := actor.ID
	// 审计写失败不该让业务失败——但也不能静默：Append 内部会记日志。
	_ = s.audit.Append(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorID: &id, ActorLabel: actor.Label,
		Action: action, Target: target, Destructive: destructive,
		Detail: detail, IP: actor.IP, RequestID: actor.RequestID,
	})
}

func summarize(issues []pricing.Issue) string {
	var parts []string
	for _, i := range issues {
		if i.Severity == pricing.SeverityError {
			parts = append(parts, i.Message)
		}
	}
	return strings.Join(parts, "；")
}
