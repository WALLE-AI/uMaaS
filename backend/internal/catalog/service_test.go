package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
)

type stubRepo struct {
	page Page
}

func (s *stubRepo) ListModels(ctx context.Context, f ListFilter) (Page, error) { return s.page, nil }
func (s *stubRepo) GetModel(ctx context.Context, providerSlug, modelSlug string) (*Model, error) {
	return nil, domain.NotFound("model_not_found", "not found")
}
func (s *stubRepo) ModelsByIDs(ctx context.Context, ids []int64) ([]Model, error) { return nil, nil }
func (s *stubRepo) FeaturedModelIDs(ctx context.Context, limit int32) ([]int64, error) {
	return nil, nil
}
func (s *stubRepo) RelatedModelIDs(ctx context.Context, m *Model, limit int32) ([]int64, error) {
	return nil, nil
}
func (s *stubRepo) Counts(ctx context.Context) (Counts, error) { return Counts{}, nil }
func (s *stubRepo) ModelActivity(ctx context.Context, modelID int64, limit int32) ([]Activity, error) {
	return nil, nil
}
func (s *stubRepo) ModelFAQ(ctx context.Context, modelID int64) ([]FAQ, error) { return nil, nil }
func (s *stubRepo) ModelBenchmarkScores(ctx context.Context, modelID int64) ([]BenchmarkScore, error) {
	return nil, nil
}
func (s *stubRepo) Benchmarks(ctx context.Context, category string) ([]Benchmark, error) {
	return nil, nil
}
func (s *stubRepo) Benchmark(ctx context.Context, slug string) (*Benchmark, error) {
	return nil, domain.NotFound("benchmark_not_found", "not found")
}
func (s *stubRepo) BenchmarkResults(ctx context.Context, ids []int64) ([]BenchmarkResult, error) {
	return nil, nil
}
func (s *stubRepo) RankByUsage(ctx context.Context, days int32, modalities []string, openOnly bool, limit int32) ([]RankedModel, error) {
	return nil, nil
}
func (s *stubRepo) RankByColumn(ctx context.Context, metric string, modalities []string, openOnly bool, limit int32) ([]RankedModel, error) {
	return nil, nil
}
func (s *stubRepo) DocsNavigation(ctx context.Context) ([]DocsNavItem, error) { return nil, nil }
func (s *stubRepo) DocsPage(ctx context.Context, slug string) (*DocsPage, error) {
	return nil, domain.NotFound("docs_page_not_found", "not found")
}
func (s *stubRepo) SearchDocs(ctx context.Context, q string, limit int32) ([]DocsMatch, error) {
	return nil, nil
}

func TestList_RejectsInvalidSort(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.List(context.Background(), ListFilter{Sort: "bogus"})
	require.Error(t, err)
}

func TestList_RejectsCursorSortMismatch(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.List(context.Background(), ListFilter{
		Sort: "price", Direction: "asc",
		Cursor: &Cursor{Sort: "usage", Direction: "asc"},
	})
	require.Error(t, err, "changing sort mid-pagination must not silently restart from page one")
}

func TestList_DefaultsAndAppliesLimit(t *testing.T) {
	svc := NewService(&stubRepo{page: Page{Total: 3}})
	page, err := svc.List(context.Background(), ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), page.Total)
}

func TestList_RejectsLimitOutOfRange(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.List(context.Background(), ListFilter{Limit: 500})
	require.Error(t, err)
}

func TestRanking_UnknownDimensionRejected(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.Ranking(context.Background(), RankingRequest{Dimension: "not-a-real-dimension"})
	require.Error(t, err)
}

func TestRanking_PendingDimensionReturnsEmptyWithExplanation(t *testing.T) {
	svc := NewService(&stubRepo{})
	view, err := svc.Ranking(context.Background(), RankingRequest{Dimension: "apps"})
	require.NoError(t, err)
	assert.Empty(t, view.Entries)
	assert.NotEmpty(t, view.Description, "a pending dimension must explain why it's empty, not just return []")
}

func TestSearchDocs_RejectsShortQuery(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.SearchDocs(context.Background(), "a")
	require.Error(t, err)
}

func TestExtractHeadings_SkipsCodeFences(t *testing.T) {
	body := "# Title\n\n```\n# not a heading\n```\n\n## Real heading\n"
	headings := extractHeadings(body)
	require.Len(t, headings, 2)
	assert.Equal(t, "Title", headings[0].Title)
	assert.Equal(t, 1, headings[0].Level)
	assert.Equal(t, "Real heading", headings[1].Title)
	assert.Equal(t, 2, headings[1].Level)
}

func TestOrderByIDs_PreservesCallerOrder(t *testing.T) {
	models := []Model{{ID: 3}, {ID: 1}, {ID: 2}}
	ordered := orderByIDs(models, []int64{2, 3, 1})
	require.Len(t, ordered, 3)
	assert.Equal(t, []int64{2, 3, 1}, []int64{ordered[0].ID, ordered[1].ID, ordered[2].ID})
}
