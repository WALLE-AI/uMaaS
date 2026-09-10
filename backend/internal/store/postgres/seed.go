package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// SeedRepo 写开发种子数据里**价目之外**的部分。
//
// 它不提供任何写价目的方法，这是有意的：价目只能经由 catalogadmin.Service，
// 那条路径上挂着 M1a 的全部提交校验（计价 §3.6-④）。
type SeedRepo struct {
	q *dbgen.Queries
}

func NewSeedRepo(db *store.DB) *SeedRepo { return &SeedRepo{q: dbgen.New(db.Pool)} }

func (r *SeedRepo) SetPresentation(ctx context.Context, modelID int64, featured bool, rank *int32, quality *float64) error {
	if err := r.q.SetModelPresentation(ctx, dbgen.SetModelPresentationParams{
		ID: modelID, Featured: featured, FeaturedRank: rank,
		QualityScore: floatToNumeric(quality),
	}); err != nil {
		return fmt.Errorf("set model presentation: %w", err)
	}
	return nil
}

type SeedBenchmark struct {
	Slug          string
	Name          string
	Category      string
	Description   string
	Methodology   string
	Configuration json.RawMessage
	RunCount      int32
	LastRunAt     time.Time
}

func (r *SeedRepo) UpsertBenchmark(ctx context.Context, in SeedBenchmark) (int64, error) {
	cfg := in.Configuration
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	var lastRun *time.Time
	if !in.LastRunAt.IsZero() {
		t := in.LastRunAt
		lastRun = &t
	}
	id, err := r.q.UpsertBenchmark(ctx, dbgen.UpsertBenchmarkParams{
		Slug: in.Slug, Name: in.Name, Category: in.Category,
		Description: in.Description, Methodology: in.Methodology,
		Configuration: cfg, RunCount: in.RunCount, LastRunAt: lastRun,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert benchmark: %w", err)
	}
	return id, nil
}

type SeedBenchmarkResult struct {
	BenchmarkID int64
	ModelID     int64
	Score       float64
	Percentile  float64
	CostNano    int64
	DurationMs  float64
	SampleCount int32
}

func (r *SeedRepo) UpsertBenchmarkResult(ctx context.Context, in SeedBenchmarkResult) error {
	if err := r.q.UpsertBenchmarkResult(ctx, dbgen.UpsertBenchmarkResultParams{
		BenchmarkID: in.BenchmarkID, ModelID: in.ModelID,
		Score:       floatToNumeric(&in.Score),
		Percentile:  floatToNumeric(&in.Percentile),
		CostNano:    in.CostNano,
		DurationMs:  floatToNumeric(&in.DurationMs),
		SampleCount: in.SampleCount,
	}); err != nil {
		return fmt.Errorf("upsert benchmark result: %w", err)
	}
	return nil
}

func (r *SeedRepo) UpsertFAQ(ctx context.Context, modelID int64, question, answer string, position int32) error {
	if err := r.q.UpsertModelFAQ(ctx, dbgen.UpsertModelFAQParams{
		ModelID: modelID, Question: question, Answer: answer, Position: position,
	}); err != nil {
		return fmt.Errorf("upsert model faq: %w", err)
	}
	return nil
}

type SeedDocsPage struct {
	Slug          string
	GroupTitle    string
	GroupPosition int32
	Position      int32
	Title         string
	Description   string
	Badge         string
	Body          string
}

func (r *SeedRepo) UpsertDocsPage(ctx context.Context, in SeedDocsPage) error {
	if err := r.q.UpsertDocsPage(ctx, dbgen.UpsertDocsPageParams{
		Slug: in.Slug, GroupTitle: in.GroupTitle, GroupPosition: in.GroupPosition,
		Position: in.Position, Title: in.Title, Description: in.Description,
		Badge: in.Badge, BodyMarkdown: in.Body,
	}); err != nil {
		return fmt.Errorf("upsert docs page: %w", err)
	}
	return nil
}

// floatToNumeric 把可空的 float64 转成 numeric 列需要的类型。
//
// 走 big.Rat 的字符串形式而不是 SetFloat64：后者在 92.4 这种值上会带出
// 二进制浮点的尾巴，落库变成 92.400000000000006。质量分不是钱，
// 但一个显示成 92.400000000000006 的分数一样会被当成 bug 报上来。
func floatToNumeric(v *float64) pgtype.Numeric {
	if v == nil {
		return pgtype.Numeric{}
	}
	var n pgtype.Numeric
	if err := n.Scan(new(big.Rat).SetFloat64(*v).FloatString(4)); err != nil {
		return pgtype.Numeric{}
	}
	return n
}
