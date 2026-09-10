package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/WALLE-AI/uMaaS/backend/internal/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

type CatalogRepo struct {
	db *store.DB
	q  *dbgen.Queries
}

func NewCatalogRepo(db *store.DB) *CatalogRepo {
	return &CatalogRepo{db: db, q: dbgen.New(db.Pool)}
}

var _ catalog.Repository = (*CatalogRepo)(nil)

// statsRow 是 to_jsonb(st) 带回来的统计。指针字段表示"没有遥测"，
// 与"值为 0"是两件事——见 catalog.sql 顶部那条纪律。
type statsRow struct {
	Tokens30d  *int64   `json:"tokens_30d"`
	Throughput *float64 `json:"throughput"`
}

func (r *CatalogRepo) ListModels(ctx context.Context, f catalog.ListFilter) (catalog.Page, error) {
	params := dbgen.ListModelsParams{
		Direction:         f.Direction,
		Sort:              f.Sort,
		Modalities:        nonNilStrings(f.Modalities),
		Capabilities:      nonNilStrings(f.Capabilities),
		MinContextLength:  f.MinContextLength,
		ZeroDataRetention: f.ZeroDataRetention,
		MaxInputPriceNano: f.MaxInputPriceNano,
		// 多取一行用来判断"还有没有下一页"。用 count 判断会在过滤条件复杂时
		// 多跑一遍全表扫描，多取一行是零成本的。
		RowLimit: f.Limit + 1,
	}
	if f.Query != "" {
		params.Q = &f.Query
	}
	if f.Provider != "" {
		params.Provider = &f.Provider
	}
	if f.Cursor != nil {
		key := f.Cursor.SortKey
		params.CursorKey = &key
		params.CursorID = f.Cursor.ID
	}

	rows, err := r.q.ListModels(ctx, params)
	if err != nil {
		return catalog.Page{}, fmt.Errorf("list models: %w", err)
	}

	page := catalog.Page{Models: make([]catalog.Model, 0, len(rows))}
	if len(rows) > 0 {
		page.Total = rows[0].TotalCount
	}
	hasMore := int32(len(rows)) > f.Limit
	if hasMore {
		rows = rows[:f.Limit]
	}
	for _, row := range rows {
		m, err := modelFromListRow(row)
		if err != nil {
			return catalog.Page{}, err
		}
		page.Models = append(page.Models, m)
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = &catalog.Cursor{
			SortKey: last.SortKey, ID: last.ID,
			Sort: f.Sort, Direction: f.Direction,
		}
	}
	return page, nil
}

func (r *CatalogRepo) GetModel(ctx context.Context, providerSlug, modelSlug string) (*catalog.Model, error) {
	row, err := r.q.GetModelByPath(ctx, dbgen.GetModelByPathParams{
		ProviderSlug: providerSlug, ModelSlug: modelSlug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("model_not_found", "model does not exist or is not listed")
		}
		return nil, fmt.Errorf("get model: %w", err)
	}
	price, err := pricing.DecodeVersion(row.PriceVersion)
	if err != nil {
		return nil, err
	}
	stats, err := decodeStats(row.Stats)
	if err != nil {
		return nil, err
	}
	m := &catalog.Model{
		ID: row.ID, Slug: row.Slug, Name: row.DisplayName,
		Description: row.Description, LogoURL: row.LogoUrl,
		Provider: catalog.Provider{
			ID: row.ProviderID, Slug: row.ProviderSlug,
			Name: row.ProviderName, LogoURL: row.ProviderLogoUrl,
			ModelCount: row.ProviderModelCount,
		},
		Modalities: row.Modalities, Capabilities: row.Capabilities,
		ContextLength: row.ContextLength, MaxOutputTokens: row.MaxOutputTokens,
		Architecture: row.Architecture, InputFormats: row.InputFormats,
		OutputFormats: row.OutputFormats, SupportedParameters: row.SupportedParameters,
		ZeroDataRetention: row.ZeroDataRetention, Hosting: row.Hosting,
		BillingModel: row.BillingModel, Status: row.Status,
		QualityScore: numericToFloat(row.QualityScore), ReleasedAt: row.ReleasedAt,
		Price: price,
	}
	applyStats(m, stats)
	return m, nil
}

func (r *CatalogRepo) ModelsByIDs(ctx context.Context, ids []int64) ([]catalog.Model, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.q.ListModelSummariesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("models by ids: %w", err)
	}
	out := make([]catalog.Model, 0, len(rows))
	for _, row := range rows {
		price, err := pricing.DecodeVersion(row.PriceVersion)
		if err != nil {
			return nil, err
		}
		stats, err := decodeStats(row.Stats)
		if err != nil {
			return nil, err
		}
		m := catalog.Model{
			ID: row.ID, Slug: row.Slug, Name: row.DisplayName,
			Description: row.Description, LogoURL: row.LogoUrl,
			Provider: catalog.Provider{
				ID: row.ProviderID, Slug: row.ProviderSlug,
				Name: row.ProviderName, LogoURL: row.ProviderLogoUrl,
				ModelCount: row.ProviderModelCount,
			},
			Modalities: row.Modalities, Capabilities: row.Capabilities,
			ContextLength: row.ContextLength,
			QualityScore:  numericToFloat(row.QualityScore),
			ReleasedAt:    row.ReleasedAt, Hosting: row.Hosting,
			BillingModel: row.BillingModel, Status: "listed", Price: price,
		}
		applyStats(&m, stats)
		out = append(out, m)
	}
	return out, nil
}

func (r *CatalogRepo) FeaturedModelIDs(ctx context.Context, limit int32) ([]int64, error) {
	ids, err := r.q.ListFeaturedModelIDs(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("featured models: %w", err)
	}
	return ids, nil
}

func (r *CatalogRepo) RelatedModelIDs(ctx context.Context, m *catalog.Model, limit int32) ([]int64, error) {
	ids, err := r.q.ListRelatedModelIDs(ctx, dbgen.ListRelatedModelIDsParams{
		ModelID: m.ID, ProviderID: m.Provider.ID,
		Capabilities: nonNilStrings(m.Capabilities), RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("related models: %w", err)
	}
	return ids, nil
}

func (r *CatalogRepo) Counts(ctx context.Context) (catalog.Counts, error) {
	row, err := r.q.CatalogCounts(ctx)
	if err != nil {
		return catalog.Counts{}, fmt.Errorf("catalog counts: %w", err)
	}
	counts := catalog.Counts{
		ModelCount: row.ModelCount, ProviderCount: row.ProviderCount,
		MonthlyTokens: row.MonthlyTokens, UpdatedAt: time.Now().UTC(),
	}
	if v, err := decodeJSONFloat(row.GatewayLatencyMsP50); err != nil {
		return catalog.Counts{}, err
	} else {
		counts.GatewayLatencyMsP50 = v
	}
	return counts, nil
}

func (r *CatalogRepo) ModelActivity(ctx context.Context, modelID int64, limit int32) ([]catalog.Activity, error) {
	rows, err := r.q.ListModelActivity(ctx, dbgen.ListModelActivityParams{
		ModelID: modelID, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("model activity: %w", err)
	}
	out := make([]catalog.Activity, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.Activity{
			ID: row.ID, Type: row.Type, Title: row.Title, OccurredAt: row.OccurredAt,
		})
	}
	return out, nil
}

func (r *CatalogRepo) ModelFAQ(ctx context.Context, modelID int64) ([]catalog.FAQ, error) {
	rows, err := r.q.ListModelFAQ(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("model faq: %w", err)
	}
	out := make([]catalog.FAQ, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.FAQ{Question: row.Question, Answer: row.Answer})
	}
	return out, nil
}

func (r *CatalogRepo) ModelBenchmarkScores(ctx context.Context, modelID int64) ([]catalog.BenchmarkScore, error) {
	rows, err := r.q.ListModelBenchmarkScores(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("model benchmark scores: %w", err)
	}
	out := make([]catalog.BenchmarkScore, 0, len(rows))
	for _, row := range rows {
		score := catalog.BenchmarkScore{
			BenchmarkSlug: row.BenchmarkSlug, BenchmarkName: row.BenchmarkName,
			Percentile: numericToFloat(row.Percentile),
		}
		if v := numericToFloat(row.Score); v != nil {
			score.Score = *v
		}
		out = append(out, score)
	}
	return out, nil
}

func (r *CatalogRepo) Benchmarks(ctx context.Context, category string) ([]catalog.Benchmark, error) {
	var cat *string
	if category != "" {
		cat = &category
	}
	rows, err := r.q.ListBenchmarks(ctx, cat)
	if err != nil {
		return nil, fmt.Errorf("list benchmarks: %w", err)
	}
	out := make([]catalog.Benchmark, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.Benchmark{
			ID: row.ID, Slug: row.Slug, Name: row.Name, Category: row.Category,
			Description: row.Description, RunCount: row.RunCount,
			LastRunAt: row.LastRunAt, EvaluatedModelCount: row.EvaluatedModelCount,
		})
	}
	return out, nil
}

func (r *CatalogRepo) Benchmark(ctx context.Context, slug string) (*catalog.Benchmark, error) {
	row, err := r.q.GetBenchmarkBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("benchmark_not_found", "benchmark does not exist")
		}
		return nil, fmt.Errorf("get benchmark: %w", err)
	}
	return &catalog.Benchmark{
		ID: row.ID, Slug: row.Slug, Name: row.Name, Category: row.Category,
		Description: row.Description, Methodology: row.Methodology,
		Configuration: row.Configuration, RunCount: row.RunCount,
		LastRunAt: row.LastRunAt, EvaluatedModelCount: row.EvaluatedModelCount,
	}, nil
}

func (r *CatalogRepo) BenchmarkResults(ctx context.Context, ids []int64) ([]catalog.BenchmarkResult, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.q.ListBenchmarkResults(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("benchmark results: %w", err)
	}
	out := make([]catalog.BenchmarkResult, 0, len(rows))
	for _, row := range rows {
		res := catalog.BenchmarkResult{
			BenchmarkID: row.BenchmarkID, ModelID: row.ModelID, Rank: row.Rank,
			Percentile: numericToFloat(row.Percentile),
			CostNano:   row.CostNano, SampleCount: row.SampleCount,
		}
		if v := numericToFloat(row.Score); v != nil {
			res.Score = *v
		}
		if v := numericToFloat(row.DurationMs); v != nil {
			res.DurationMs = *v
		}
		out = append(out, res)
	}
	return out, nil
}

func (r *CatalogRepo) RankByUsage(ctx context.Context, days int32, modalities []string, openOnly bool, limit int32) ([]catalog.RankedModel, error) {
	rows, err := r.q.RankModelsByUsage(ctx, dbgen.RankModelsByUsageParams{
		Days: days, Modalities: nonNilStrings(modalities),
		OpenWeightsOnly: openOnly, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("rank by usage: %w", err)
	}
	out := make([]catalog.RankedModel, 0, len(rows))
	for _, row := range rows {
		v := float64(row.Value)
		out = append(out, catalog.RankedModel{ModelID: row.ID, Value: &v})
	}
	return out, nil
}

func (r *CatalogRepo) RankByColumn(ctx context.Context, metric string, modalities []string, openOnly bool, limit int32) ([]catalog.RankedModel, error) {
	rows, err := r.q.RankModelsByColumn(ctx, dbgen.RankModelsByColumnParams{
		Metric: metric, Modalities: nonNilStrings(modalities),
		OpenWeightsOnly: openOnly, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("rank by column: %w", err)
	}
	out := make([]catalog.RankedModel, 0, len(rows))
	for _, row := range rows {
		v, err := decodeJSONFloat(row.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, catalog.RankedModel{ModelID: row.ID, Value: v})
	}
	return out, nil
}

func (r *CatalogRepo) DocsNavigation(ctx context.Context) ([]catalog.DocsNavItem, error) {
	rows, err := r.q.ListDocsNavigation(ctx)
	if err != nil {
		return nil, fmt.Errorf("docs navigation: %w", err)
	}
	out := make([]catalog.DocsNavItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.DocsNavItem{
			Slug: row.Slug, GroupTitle: row.GroupTitle,
			GroupPosition: row.GroupPosition, Title: row.Title, Badge: row.Badge,
		})
	}
	return out, nil
}

func (r *CatalogRepo) DocsPage(ctx context.Context, slug string) (*catalog.DocsPage, error) {
	row, err := r.q.GetDocsPage(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("docs_page_not_found", "documentation page does not exist")
		}
		return nil, fmt.Errorf("get docs page: %w", err)
	}
	return &catalog.DocsPage{
		Slug: row.Slug, Title: row.Title, Description: row.Description,
		BodyMarkdown: row.BodyMarkdown, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *CatalogRepo) SearchDocs(ctx context.Context, q string, limit int32) ([]catalog.DocsMatch, error) {
	rows, err := r.q.SearchDocs(ctx, dbgen.SearchDocsParams{Q: &q, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("search docs: %w", err)
	}
	out := make([]catalog.DocsMatch, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.DocsMatch{
			Slug: row.Slug, Title: row.Title,
			Description: row.Description, Body: row.BodyMarkdown,
		})
	}
	return out, nil
}

// ── 行 → 领域模型 ───────────────────────────────────────────────

func modelFromListRow(row dbgen.ListModelsRow) (catalog.Model, error) {
	price, err := pricing.DecodeVersion(row.PriceVersion)
	if err != nil {
		return catalog.Model{}, err
	}
	stats, err := decodeStats(row.Stats)
	if err != nil {
		return catalog.Model{}, err
	}
	m := catalog.Model{
		ID: row.ID, Slug: row.Slug, Name: row.DisplayName,
		Description: row.Description, LogoURL: row.LogoUrl,
		Provider: catalog.Provider{
			ID: row.ProviderID, Slug: row.ProviderSlug,
			Name: row.ProviderName, LogoURL: row.ProviderLogoUrl,
			ModelCount: row.ProviderModelCount,
		},
		Modalities: row.Modalities, Capabilities: row.Capabilities,
		ContextLength: row.ContextLength,
		QualityScore:  numericToFloat(row.QualityScore),
		ReleasedAt:    row.ReleasedAt, BillingModel: row.BillingModel,
		Hosting: row.Hosting, Status: "listed", Price: price,
	}
	applyStats(&m, stats)
	return m, nil
}

func applyStats(m *catalog.Model, s *statsRow) {
	if s == nil {
		return
	}
	if s.Tokens30d != nil {
		m.Tokens30d = *s.Tokens30d
	}
	m.Throughput = s.Throughput
}

func decodeStats(raw []byte) (*statsRow, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s statsRow
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode model stats: %w", err)
	}
	return &s, nil
}

// decodeJSONFloat 读 to_jsonb(数值) 的结果。用 jsonb 兜住可空聚合值，
// 是因为 pgx 把 NULL 扫进 *float64 会直接报错，而"没有数据"是这里的常态。
func decodeJSONFloat(raw []byte) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode numeric: %w", err)
	}
	return &v, nil
}

// numericToFloat 把 pgtype.Numeric 转成 *float64，NULL 保持为 nil。
//
// 这个转换必须保留"无值"：quality_score 为 NULL 是"没测过"，
// 转成 0 会让一个没参加评测的模型在榜单上显示成 0 分垫底。
func numericToFloat(n pgtype.Numeric) *float64 {
	if !n.Valid || n.NaN {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

// nonNilStrings 保证传给 Postgres 的是空数组而不是 NULL。
// `cardinality(NULL) = 0` 是 NULL 而不是 true，会让整个过滤条件失效。
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
