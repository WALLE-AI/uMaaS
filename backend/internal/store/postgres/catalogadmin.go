package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/catalogadmin"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// CatalogAdminRepo 是目录写入侧的实现。
//
// 它复用 PricingRepo 而不是自己再写一遍改价 SQL——**改价只能有一条路径**
// （计价 §3.6-①：没有任何路径能绕过 admin 直接改价，反过来说，
// admin 这条路径也不能有第二份实现）。
type CatalogAdminRepo struct {
	db    *store.DB
	q     *dbgen.Queries
	price *PricingRepo
}

func NewCatalogAdminRepo(db *store.DB) *CatalogAdminRepo {
	return &CatalogAdminRepo{db: db, q: dbgen.New(db.Pool), price: NewPricingRepo(db)}
}

var _ catalogadmin.Repository = (*CatalogAdminRepo)(nil)

func (r *CatalogAdminRepo) UpsertProvider(ctx context.Context, in catalogadmin.ProviderInput) (int64, error) {
	row, err := r.q.UpsertProvider(ctx, dbgen.UpsertProviderParams{
		Slug: in.Slug, Name: in.Name, Kind: in.Kind,
		LogoUrl: in.LogoURL, Description: in.Description,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert provider: %w", err)
	}
	return row.ID, nil
}

func (r *CatalogAdminRepo) UpsertModel(ctx context.Context, providerID int64, in catalogadmin.ModelInput) (*catalogadmin.Model, error) {
	row, err := r.q.UpsertModel(ctx, dbgen.UpsertModelParams{
		ProviderID: providerID, Slug: in.Slug, DisplayName: in.DisplayName,
		Description: in.Description, LogoUrl: in.LogoURL,
		Modalities: nonNilStrings(in.Modalities), Capabilities: nonNilStrings(in.Capabilities),
		ContextLength: in.ContextLength, MaxOutputTokens: in.MaxOutputTokens,
		Architecture:        in.Architecture,
		InputFormats:        nonNilStrings(in.InputFormats),
		OutputFormats:       nonNilStrings(in.OutputFormats),
		SupportedParameters: nonNilStrings(in.SupportedParameters),
		ZeroDataRetention:   in.ZeroDataRetention,
		OpenWeights:         in.OpenWeights,
		Hosting:             in.Hosting,
		BillingModel:        in.BillingModel,
		ReleasedAt:          in.ReleasedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert model: %w", err)
	}
	return r.ModelByID(ctx, row.ID)
}

func (r *CatalogAdminRepo) ModelByID(ctx context.Context, id int64) (*catalogadmin.Model, error) {
	row, err := r.q.GetAdminModelByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("model_not_found", "model does not exist")
		}
		return nil, fmt.Errorf("get admin model: %w", err)
	}
	m := &catalogadmin.Model{
		ID: row.ID, ProviderSlug: row.ProviderSlug, ProviderName: row.ProviderName,
		Slug: row.Slug, DisplayName: row.DisplayName, Description: row.Description,
		Modalities: row.Modalities, Capabilities: row.Capabilities,
		ContextLength: row.ContextLength, MaxOutputTokens: row.MaxOutputTokens,
		Hosting: row.Hosting, BillingModel: row.BillingModel,
		Status: row.Status, CanaryPercent: row.CanaryPercent, ReleasedAt: row.ReleasedAt,
	}
	price, err := r.currentPrice(ctx, m)
	if err != nil {
		return nil, err
	}
	m.Price = price

	usage, err := r.RecentUsage(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	m.CallsLast7d = usage.Calls
	return m, nil
}

// currentPrice 走**与展示、结算完全相同的解析入口**。
// admin 页面上显示的价格若与目录页不一致，运营就会按错误的数字做决策。
func (r *CatalogAdminRepo) currentPrice(ctx context.Context, m *catalogadmin.Model) (*pricing.Version, error) {
	v, err := r.price.ResolveVersion(ctx, pricing.Key{
		BillingModel:         m.BillingModel,
		FallbackBillingModel: m.PublicID(),
	}, nowUTC())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil // draft 模型可以没有价目
		}
		return nil, err
	}
	return v, nil
}

func (r *CatalogAdminRepo) ListModels(ctx context.Context, status string) ([]catalogadmin.Model, error) {
	var st *string
	if status != "" {
		st = &status
	}
	rows, err := r.q.ListAdminModels(ctx, st)
	if err != nil {
		return nil, fmt.Errorf("list admin models: %w", err)
	}
	out := make([]catalogadmin.Model, 0, len(rows))
	for _, row := range rows {
		m := catalogadmin.Model{
			ID: row.ID, ProviderSlug: row.ProviderSlug, ProviderName: row.ProviderName,
			Slug: row.Slug, DisplayName: row.DisplayName, Description: row.Description,
			Modalities: row.Modalities, Capabilities: row.Capabilities,
			ContextLength: row.ContextLength, MaxOutputTokens: row.MaxOutputTokens,
			Hosting: row.Hosting, BillingModel: row.BillingModel,
			Status: row.Status, CanaryPercent: row.CanaryPercent,
			CallsLast7d: row.CallsLast7d, ReleasedAt: row.ReleasedAt,
		}
		priceID, err := decodeJSONInt64(row.PriceVersionID)
		if err != nil {
			return nil, err
		}
		if priceID != nil {
			v, err := r.priceByID(ctx, *priceID)
			if err != nil {
				return nil, err
			}
			m.Price = v
		}
		out = append(out, m)
	}
	return out, nil
}

// decodeJSONInt64 读 to_jsonb(可空 bigint) 的结果。
//
// ListAdminModels 的 price_version_id 走 to_jsonb 而不是直接 scan 进 *int64：
// sqlc 把 pricing_effective_version() 的返回类型静态推断成 NOT NULL bigint
// （函数签名如此），draft 模型没有价目时会在扫描阶段直接 panic 式报错——
// 这条路径只有在库里真的存在一个没有价目的模型时才会触发，用带价目的种子
// 数据测不出来。
func decodeJSONInt64(raw []byte) (*int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode price_version_id: %w", err)
	}
	return &v, nil
}

func (r *CatalogAdminRepo) priceByID(ctx context.Context, id int64) (*pricing.Version, error) {
	row, err := r.q.GetPriceVersionByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get price version: %w", err)
	}
	return versionFromRow(row)
}

func (r *CatalogAdminRepo) SetStatus(ctx context.Context, id int64, status string, canaryPercent *int32) (*catalogadmin.Model, error) {
	if _, err := r.q.SetModelStatus(ctx, dbgen.SetModelStatusParams{
		ID: id, Status: status, CanaryPercent: canaryPercent,
	}); err != nil {
		return nil, fmt.Errorf("set model status: %w", err)
	}
	return r.ModelByID(ctx, id)
}

func (r *CatalogAdminRepo) RecentUsage(ctx context.Context, modelID int64) (catalogadmin.UsageWindow, error) {
	row, err := r.q.CountRecentCalls(ctx, modelID)
	if err != nil {
		return catalogadmin.UsageWindow{}, fmt.Errorf("recent usage: %w", err)
	}
	return catalogadmin.UsageWindow{
		Calls: row.Calls, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
		UpstreamCostNano: row.UpstreamCostNano, ChargedAmountNano: row.ChargedAmountNano,
		// 成本列在 M2 之前恒为 0。**用 >0 判断而不是用"有没有调用"判断**：
		// 有调用但成本为 0，说明成本侧还没接上，不是"这些调用不花钱"。
		CostAvailable: row.UpstreamCostNano > 0,
	}, nil
}

func (r *CatalogAdminRepo) AppendActivity(ctx context.Context, modelID int64, kind, title string) error {
	if err := r.q.InsertModelActivity(ctx, dbgen.InsertModelActivityParams{
		ModelID: modelID, Type: kind, Title: title,
	}); err != nil {
		return fmt.Errorf("insert model activity: %w", err)
	}
	return nil
}

func (r *CatalogAdminRepo) PublishPrice(ctx context.Context, in catalogadmin.PricePublication) (*pricing.Version, error) {
	return r.price.PublishPrice(ctx, PublishPriceInput{
		BillingModel: in.BillingModel, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID,
		Currency: in.Currency, Rates: in.Rates, EffectiveFrom: in.EffectiveFrom,
		Source: in.Source, AppliedBy: in.AppliedBy, Note: in.Note,
	})
}

func (r *CatalogAdminRepo) PriceHistory(ctx context.Context, billingModel string, limit int32) ([]*pricing.Version, error) {
	return r.price.PriceHistory(ctx, billingModel, limit)
}
