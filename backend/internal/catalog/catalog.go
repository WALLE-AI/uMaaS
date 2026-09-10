// Package catalog 是模型目录的只读读取侧（B2）。
//
// 它只做一件事：把库里的模型、价目、统计、评测、文档组装成契约要的形状。
// **写入侧全在 admin**（计价 §3.6-①：上游同步只产出建议，没有任何路径能
// 绕过 admin 直接改价），所以这个包里没有任何 Create/Update。
//
// 目录页的价格与结算价格来自同一个 pricing.Version——本包不认识费率的内部表达，
// 拿到的已经是解析好的版本对象（计价 §3.6-②）。
package catalog

import (
	"context"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

// Provider 是契约的 ProviderSummary（不含 model_count，那是列表侧的聚合）。
type Provider struct {
	ID         int64
	Slug       string
	Name       string
	LogoURL    string
	ModelCount int64
}

// Model 是目录里的一个模型。
//
// 可空的字段一律用指针：契约把 null 与 0 区分得很死（`context_length`、
// `quality_score`、`throughput` 都是 [number,'null']），而**null 被当成 0
// 正是这次契约改动要修掉的那一类 bug**（计价 附 1.1-③）。
type Model struct {
	ID                  int64
	Slug                string
	Name                string
	Description         string
	LogoURL             string
	Provider            Provider
	Modalities          []string
	Capabilities        []string
	ContextLength       *int32
	MaxOutputTokens     *int32
	Architecture        *string
	InputFormats        []string
	OutputFormats       []string
	SupportedParameters []string
	ZeroDataRetention   bool
	Hosting             string
	BillingModel        string
	Status              string
	QualityScore        *float64
	ReleasedAt          time.Time

	// Price 为 nil 表示**没有价目**，不是"免费"。契约里前者是 null、后者是 0，
	// UI 必须显示为「—」而非 $0。
	Price *pricing.Version

	Tokens30d  int64
	Throughput *float64
}

// PublicID 是契约里的模型 ID：`provider/model`。
func (m Model) PublicID() string { return m.Provider.Slug + "/" + m.Slug }

// ListFilter 是 GET /models 的全部查询参数。
type ListFilter struct {
	Query             string
	Provider          string
	Modalities        []string
	Capabilities      []string
	MinContextLength  *int32
	MaxInputPriceNano *int64
	ZeroDataRetention *bool
	Sort              string // released_at | price | throughput | usage
	Direction         string // asc | desc
	Cursor            *Cursor
	Limit             int32
}

// Cursor 是不透明游标的内容。
//
// **必须包含 id 作为决胜字段**（ARCHITECTURE.md §5.2）：排序键相同的行
// （同一天发布、同样的价格）在翻页时会重复或遗漏，而这类 bug 只在
// 数据量上来之后才显形。
type Cursor struct {
	SortKey   float64 `json:"k"`
	ID        int64   `json:"i"`
	Sort      string  `json:"s"`
	Direction string  `json:"d"`
}

// Page 是一页模型加上翻页所需的信息。
type Page struct {
	Models     []Model
	Total      int64
	NextCursor *Cursor
}

// Counts 支撑 /catalog/summary。
type Counts struct {
	ModelCount    int64
	ProviderCount int64
	MonthlyTokens int64
	// GatewayLatencyMsP50 为 nil = **还没有遥测**，不是 0 毫秒。
	// B3 落地前它一直是 nil，这是事实而不是缺陷。
	GatewayLatencyMsP50 *float64
	UpdatedAt           time.Time
}

type Activity struct {
	ID         int64
	Type       string
	Title      string
	OccurredAt time.Time
}

type FAQ struct{ Question, Answer string }

type BenchmarkScore struct {
	BenchmarkSlug string
	BenchmarkName string
	Score         float64
	Percentile    *float64
}

type Benchmark struct {
	ID                  int64
	Slug                string
	Name                string
	Category            string
	Description         string
	Methodology         string
	Configuration       []byte
	RunCount            int32
	LastRunAt           *time.Time
	EvaluatedModelCount int64
}

type BenchmarkResult struct {
	BenchmarkID int64
	ModelID     int64
	Rank        int64
	Score       float64
	Percentile  *float64
	CostNano    int64
	DurationMs  float64
	SampleCount int32
}

type DocsNavItem struct {
	Slug          string
	GroupTitle    string
	GroupPosition int32
	Title         string
	Badge         string
}

type DocsPage struct {
	Slug         string
	Title        string
	Description  string
	BodyMarkdown string
	UpdatedAt    time.Time
}

type DocsMatch struct {
	Slug        string
	Title       string
	Description string
	Body        string
}

// RankedModel 是榜单的一行。Value 为 nil 表示该模型在该维度上没有数据，
// 调用方要把它剔掉而不是当成 0——"没测过"排在最后一名是错的展示。
type RankedModel struct {
	ModelID int64
	Value   *float64
}

// Repository 是目录的读取端口。实现在 store/postgres。
//
// 接口大是因为目录页本身就是七个端点；把它拆成七个接口只会让装配层更啰嗦。
// 真正要守住的是**方向**：catalog 依赖它，它不依赖 catalog。
type Repository interface {
	ListModels(ctx context.Context, f ListFilter) (Page, error)
	GetModel(ctx context.Context, providerSlug, modelSlug string) (*Model, error)
	ModelsByIDs(ctx context.Context, ids []int64) ([]Model, error)
	FeaturedModelIDs(ctx context.Context, limit int32) ([]int64, error)
	RelatedModelIDs(ctx context.Context, m *Model, limit int32) ([]int64, error)
	Counts(ctx context.Context) (Counts, error)

	ModelActivity(ctx context.Context, modelID int64, limit int32) ([]Activity, error)
	ModelFAQ(ctx context.Context, modelID int64) ([]FAQ, error)
	ModelBenchmarkScores(ctx context.Context, modelID int64) ([]BenchmarkScore, error)

	Benchmarks(ctx context.Context, category string) ([]Benchmark, error)
	Benchmark(ctx context.Context, slug string) (*Benchmark, error)
	BenchmarkResults(ctx context.Context, benchmarkIDs []int64) ([]BenchmarkResult, error)

	RankByUsage(ctx context.Context, days int32, modalities []string, openWeightsOnly bool, limit int32) ([]RankedModel, error)
	RankByColumn(ctx context.Context, metric string, modalities []string, openWeightsOnly bool, limit int32) ([]RankedModel, error)

	DocsNavigation(ctx context.Context) ([]DocsNavItem, error)
	DocsPage(ctx context.Context, slug string) (*DocsPage, error)
	SearchDocs(ctx context.Context, q string, limit int32) ([]DocsMatch, error)
}
