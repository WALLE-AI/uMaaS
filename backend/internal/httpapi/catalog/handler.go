package catalog

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	catalogsvc "github.com/WALLE-AI/uMaaS/backend/internal/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/response"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

type Handler struct {
	svc *catalogsvc.Service
}

func New(svc *catalogsvc.Service) *Handler { return &Handler{svc: svc} }

// Routes 挂载目录的公开端点。**全部不需要会话**——
// 未登录用户也要能浏览模型与价格（计价 §3.6-② 推论）。
func (h *Handler) Routes(r chi.Router) {
	r.Get("/catalog/summary", h.summary)
	r.Get("/models", h.listModels)
	r.Get("/models/{provider}/{model}", h.modelDetail)
	r.Get("/benchmarks", h.listBenchmarks)
	r.Get("/benchmarks/{slug}", h.benchmarkDetail)
	r.Get("/rankings", h.ranking)
	r.Get("/docs/navigation", h.docsNavigation)
	r.Get("/docs/search", h.docsSearch)
	r.Get("/docs/{slug}", h.docsPage)
}

// 缓存策略照抄契约 README 的表。放在这里而不是反代上，是因为
// 每类资源的新鲜度要求来自业务语义，反代配置里读不出这个理由。
const (
	cacheCatalog     = "public, max-age=60, stale-while-revalidate=300"
	cacheModelDetail = "public, max-age=30, stale-while-revalidate=120"
	cacheBenchmarks  = "public, max-age=300, stale-while-revalidate=3600"
	cacheRankings    = "public, max-age=300, stale-while-revalidate=900"
	cacheDocs        = "public, max-age=300"
)

// ── /catalog/summary ────────────────────────────────────────────

type summaryPayload struct {
	ModelCount    int64 `json:"model_count"`
	ProviderCount int64 `json:"provider_count"`
	MonthlyTokens int64 `json:"monthly_tokens"`
	// null = 还没有网关遥测（B3 之前恒为 null），不是 0 毫秒。
	GatewayLatencyMsP50 *float64              `json:"gateway_latency_ms_p50"`
	FeaturedModels      []modelSummaryPayload `json:"featured_models"`
	UpdatedAt           string                `json:"updated_at"`
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Summary(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", cacheCatalog)
	response.OK(w, r, summaryPayload{
		ModelCount: s.ModelCount, ProviderCount: s.ProviderCount,
		MonthlyTokens: s.MonthlyTokens, GatewayLatencyMsP50: s.GatewayLatencyMsP50,
		FeaturedModels: toSummaries(s.Featured),
		UpdatedAt:      s.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// ── /models ─────────────────────────────────────────────────────

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := catalogsvc.ListFilter{
		Query:        strings.TrimSpace(q.Get("q")),
		Provider:     q.Get("provider"),
		Modalities:   q["modality"],
		Capabilities: q["capability"],
		Sort:         q.Get("sort"),
		Direction:    q.Get("direction"),
	}

	if v := q.Get("min_context_length"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			response.Error(w, r, domain.Validation("invalid_min_context_length",
				"min_context_length must be a positive integer", "min_context_length"))
			return
		}
		n32 := int32(n)
		f.MinContextLength = &n32
	}
	// max_input_price 的单位是 USD / 1M（契约），内部一律纳美元。
	// 转换只在这一处发生——多处转换就是多处四舍五入分歧。
	if v := q.Get("max_input_price"); v != "" {
		usd, err := strconv.ParseFloat(v, 64)
		if err != nil || usd < 0 {
			response.Error(w, r, domain.Validation("invalid_max_input_price",
				"max_input_price must be a non-negative number of USD per 1M tokens", "max_input_price"))
			return
		}
		nano := pricing.USDPerMillionToNano(usd)
		f.MaxInputPriceNano = &nano
	}
	if v := q.Get("zero_data_retention"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			response.Error(w, r, domain.Validation("invalid_zero_data_retention",
				"zero_data_retention must be true or false", "zero_data_retention"))
			return
		}
		f.ZeroDataRetention = &b
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			response.Error(w, r, domain.Validation("invalid_limit",
				"limit must be between 1 and 100", "limit"))
			return
		}
		f.Limit = int32(n)
	}
	if v := q.Get("cursor"); v != "" {
		c, err := decodeCursor(v)
		if err != nil {
			response.Error(w, r, err)
			return
		}
		f.Cursor = c
	}

	page, err := h.svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	meta := &response.Meta{}
	total := int(page.Total)
	meta.Total = &total
	if page.NextCursor != nil {
		encoded, err := encodeCursor(page.NextCursor)
		if err != nil {
			response.Error(w, r, err)
			return
		}
		meta.NextCursor = &encoded
	}
	w.Header().Set("Cache-Control", cacheCatalog)
	response.JSON(w, r, http.StatusOK, toSummaries(page.Models), meta)
}

func (h *Handler) modelDetail(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Detail(r.Context(), chi.URLParam(r, "provider"), chi.URLParam(r, "model"))
	if err != nil {
		response.Error(w, r, err)
		return
	}

	payload := modelDetailPayload{
		modelSummaryPayload: toSummary(d.Model),
		Architecture:        d.Architecture,
		InputFormats:        emptyIfNil(d.InputFormats),
		OutputFormats:       emptyIfNil(d.OutputFormats),
		SupportedParameters: emptyIfNil(d.SupportedParameters),
		// endpoints / uptime / top_apps 现在**必然是空的**，这是有意的：
		//   - endpoints 与 uptime 来自 channels 与健康探针（I4 的 P2/B4）
		//   - top_apps 依赖 agent_framework 埋点（ARCHITECTURE.md §6.4）
		// 与其编三条假记录，不如让页面显示空状态——假数据会被拿去做选型决策。
		Endpoints:       []endpointPayload{},
		Uptime:          []timeSeriesPoint{},
		TopApps:         []appUsagePayload{},
		BenchmarkScores: make([]benchmarkScorePayload, 0, len(d.BenchmarkScores)),
		RecentActivity:  make([]activityPayload, 0, len(d.Activity)),
		FAQ:             make([]faqPayload, 0, len(d.FAQ)),
		RelatedModels:   toSummaries(d.Related),
	}
	for _, s := range d.BenchmarkScores {
		payload.BenchmarkScores = append(payload.BenchmarkScores, benchmarkScorePayload{
			BenchmarkID: s.BenchmarkSlug, BenchmarkName: s.BenchmarkName,
			Score: s.Score, Percentile: s.Percentile,
		})
	}
	for _, a := range d.Activity {
		payload.RecentActivity = append(payload.RecentActivity, activityPayload{
			ID: strconv.FormatInt(a.ID, 10), Type: a.Type, Title: a.Title,
			OccurredAt: a.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	for _, f := range d.FAQ {
		payload.FAQ = append(payload.FAQ, faqPayload{Question: f.Question, Answer: f.Answer})
	}

	w.Header().Set("Cache-Control", cacheModelDetail)
	response.OK(w, r, payload)
}

// ── /benchmarks ─────────────────────────────────────────────────

type winnerPayload struct {
	Model        modelSummaryPayload `json:"model"`
	Value        float64             `json:"value"`
	DisplayValue string              `json:"display_value"`
}

type winnersPayload struct {
	Quality *winnerPayload `json:"quality,omitempty"`
	Value   *winnerPayload `json:"value,omitempty"`
	Speed   *winnerPayload `json:"speed,omitempty"`
}

type benchmarkSummaryPayload struct {
	ID                  string         `json:"id"`
	Slug                string         `json:"slug"`
	Name                string         `json:"name"`
	Category            string         `json:"category"`
	Description         string         `json:"description"`
	EvaluatedModelCount int64          `json:"evaluated_model_count"`
	RunCount            int32          `json:"run_count"`
	LastRunAt           *string        `json:"last_run_at"`
	Winners             winnersPayload `json:"winners"`
}

type benchmarkDetailPayload struct {
	benchmarkSummaryPayload
	Methodology   string                `json:"methodology"`
	Configuration json.RawMessage       `json:"configuration"`
	Results       []benchmarkRowPayload `json:"results"`
}

type benchmarkRowPayload struct {
	Rank        int64               `json:"rank"`
	Model       modelSummaryPayload `json:"model"`
	Score       float64             `json:"score"`
	CostUSD     float64             `json:"cost_usd"`
	DurationMs  float64             `json:"duration_ms"`
	SampleCount int32               `json:"sample_count"`
}

func (h *Handler) listBenchmarks(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	views, err := h.svc.Benchmarks(r.Context(), category)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]benchmarkSummaryPayload, 0, len(views))
	for _, v := range views {
		out = append(out, toBenchmarkSummary(v))
	}
	w.Header().Set("Cache-Control", cacheBenchmarks)
	response.OK(w, r, out)
}

func (h *Handler) benchmarkDetail(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Benchmark(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	payload := benchmarkDetailPayload{
		benchmarkSummaryPayload: toBenchmarkSummary(v.BenchmarkView),
		Methodology:             v.Methodology,
		Configuration:           json.RawMessage(v.Configuration),
		Results:                 make([]benchmarkRowPayload, 0, len(v.Results)),
	}
	if len(payload.Configuration) == 0 {
		payload.Configuration = json.RawMessage("{}")
	}
	for _, row := range v.Results {
		payload.Results = append(payload.Results, benchmarkRowPayload{
			Rank: row.Rank, Model: toSummary(row.Model), Score: row.Score,
			// 评测成本按 nano 存、按 USD 出——**只在这一步取整一次**（§3.5）。
			CostUSD:     pricing.NanoToUSD(row.CostNano),
			DurationMs:  row.DurationMs,
			SampleCount: row.SampleCount,
		})
	}
	w.Header().Set("Cache-Control", cacheBenchmarks)
	response.OK(w, r, payload)
}

func toBenchmarkSummary(v catalogsvc.BenchmarkView) benchmarkSummaryPayload {
	p := benchmarkSummaryPayload{
		ID: v.Slug, Slug: v.Slug, Name: v.Name, Category: v.Category,
		Description: v.Description, EvaluatedModelCount: v.EvaluatedModelCount,
		RunCount: v.RunCount,
	}
	if v.LastRunAt != nil {
		s := v.LastRunAt.UTC().Format(time.RFC3339)
		p.LastRunAt = &s
	}
	p.Winners = winnersPayload{
		Quality: toWinner(v.Quality), Value: toWinner(v.Value), Speed: toWinner(v.Speed),
	}
	return p
}

func toWinner(w *catalogsvc.Winner) *winnerPayload {
	if w == nil {
		return nil
	}
	return &winnerPayload{Model: toSummary(w.Model), Value: w.Value, DisplayValue: w.DisplayValue}
}

// ── /rankings ───────────────────────────────────────────────────

type rankingEntryPayload struct {
	Rank          int                  `json:"rank"`
	Key           string               `json:"key"`
	Label         string               `json:"label"`
	Value         float64              `json:"value"`
	DisplayValue  string               `json:"display_value"`
	ChangePercent *float64             `json:"change_percent"`
	Model         *modelSummaryPayload `json:"model,omitempty"`
}

type rankingPayload struct {
	Dimension   string                `json:"dimension"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Entries     []rankingEntryPayload `json:"entries"`
	UpdatedAt   string                `json:"updated_at"`
}

func (h *Handler) ranking(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dimension := q.Get("dimension")
	if dimension == "" {
		response.Error(w, r, domain.Validation("dimension_required", "dimension is required", "dimension"))
		return
	}
	view, err := h.svc.Ranking(r.Context(), catalogsvc.RankingRequest{
		Dimension: dimension, Modality: q.Get("modality"),
		Period: q.Get("period"), Unit: q.Get("unit"), Scope: q.Get("scope"),
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	payload := rankingPayload{
		Dimension: view.Dimension, Title: view.Title, Description: view.Description,
		Entries:   make([]rankingEntryPayload, 0, len(view.Entries)),
		UpdatedAt: view.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, e := range view.Entries {
		entry := rankingEntryPayload{
			Rank: e.Rank, Key: e.Key, Label: e.Label,
			Value: e.Value, DisplayValue: e.DisplayValue,
			// change_percent 需要上一周期的快照做对比，而 model_stats 现在
			// 只有当期。给 null 而不是 0：0 的意思是"没有变化"，那是另一回事。
			ChangePercent: nil,
		}
		if e.Model != nil {
			s := toSummary(*e.Model)
			entry.Model = &s
		}
		payload.Entries = append(payload.Entries, entry)
	}
	w.Header().Set("Cache-Control", cacheRankings)
	response.OK(w, r, payload)
}

// ── /docs ───────────────────────────────────────────────────────

type docsNavItemPayload struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Badge string `json:"badge,omitempty"`
}

type docsNavGroupPayload struct {
	Title string               `json:"title"`
	Items []docsNavItemPayload `json:"items"`
}

func (h *Handler) docsNavigation(w http.ResponseWriter, r *http.Request) {
	groups, err := h.svc.DocsNavigation(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]docsNavGroupPayload, 0, len(groups))
	for _, g := range groups {
		items := make([]docsNavItemPayload, 0, len(g.Items))
		for _, i := range g.Items {
			items = append(items, docsNavItemPayload{Slug: i.Slug, Title: i.Title, Badge: i.Badge})
		}
		out = append(out, docsNavGroupPayload{Title: g.Title, Items: items})
	}
	w.Header().Set("Cache-Control", cacheDocs)
	response.OK(w, r, out)
}

type docsHeadingPayload struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Level int    `json:"level"`
}

type docsPagePayload struct {
	Slug            string               `json:"slug"`
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	BodyMarkdown    string               `json:"body_markdown"`
	TableOfContents []docsHeadingPayload `json:"table_of_contents"`
	UpdatedAt       string               `json:"updated_at"`
}

func (h *Handler) docsPage(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.DocsPageBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	payload := docsPagePayload{
		Slug: page.Slug, Title: page.Title, Description: page.Description,
		BodyMarkdown:    page.BodyMarkdown,
		TableOfContents: make([]docsHeadingPayload, 0, len(page.TableOfContents)),
		UpdatedAt:       page.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, hd := range page.TableOfContents {
		payload.TableOfContents = append(payload.TableOfContents,
			docsHeadingPayload{ID: hd.ID, Title: hd.Title, Level: hd.Level})
	}

	// docs 内容很少变而前端请求很频繁，ETag 让绝大多数请求变成 304
	// （契约 README 的缓存表专门给 docs 标了 ETag）。
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(page.UpdatedAt.String()+page.Slug)))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheDocs)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	response.OK(w, r, payload)
}

type docsSearchHitPayload struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

func (h *Handler) docsSearch(w http.ResponseWriter, r *http.Request) {
	hits, err := h.svc.SearchDocs(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]docsSearchHitPayload, 0, len(hits))
	for _, hit := range hits {
		out = append(out, docsSearchHitPayload{Slug: hit.Slug, Title: hit.Title, Excerpt: hit.Excerpt})
	}
	// 搜索结果不缓存：查询空间无限，缓存命中率接近 0 而缓存条目会爆。
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, r, out)
}

// ── 游标编解码 ──────────────────────────────────────────────────

// encodeCursor 生成不透明游标。base64 而不是明文，是为了让"游标是实现细节"
// 这件事在类型上就成立——客户端一旦开始拼游标，我们就再也改不了排序实现。
func encodeCursor(c *catalogsvc.Cursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(s string) (*catalogsvc.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, domain.Validation("invalid_cursor", "cursor is not a valid pagination cursor", "cursor")
	}
	var c catalogsvc.Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, domain.Validation("invalid_cursor", "cursor is not a valid pagination cursor", "cursor")
	}
	return &c, nil
}
