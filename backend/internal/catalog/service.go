package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

// Service 组装目录的七个只读端点。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

const (
	defaultLimit  = 20
	maxLimit      = 100
	featuredLimit = 3
	relatedLimit  = 4
	activityLimit = 10
	docsSearchCap = 20
	rankingCap    = 20
)

var validSorts = map[string]bool{
	"released_at": true, "price": true, "throughput": true, "usage": true,
}

// List 实现 GET /models。
func (s *Service) List(ctx context.Context, f ListFilter) (Page, error) {
	if f.Sort == "" {
		f.Sort = "released_at"
	}
	if !validSorts[f.Sort] {
		return Page{}, domain.Validation("invalid_sort",
			"sort must be one of released_at, price, throughput, usage", "sort")
	}
	if f.Direction == "" {
		f.Direction = "desc"
	}
	if f.Direction != "asc" && f.Direction != "desc" {
		return Page{}, domain.Validation("invalid_direction", "direction must be asc or desc", "direction")
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		return Page{}, domain.Validation("invalid_limit", "limit must be between 1 and 100", "limit")
	}
	// 游标里带着排序方式：翻页途中换排序，游标就失去意义了。
	// 静默按新排序从头开始会让用户看到重复条目，报错反而更容易理解。
	if f.Cursor != nil && (f.Cursor.Sort != f.Sort || f.Cursor.Direction != f.Direction) {
		return Page{}, domain.Validation("cursor_sort_mismatch",
			"cursor was issued for a different sort order; drop the cursor when changing sort", "cursor")
	}
	return s.repo.ListModels(ctx, f)
}

// Detail 是 GET /models/{provider}/{model} 的完整载荷。
type Detail struct {
	Model
	BenchmarkScores []BenchmarkScore
	Activity        []Activity
	FAQ             []FAQ
	Related         []Model
}

func (s *Service) Detail(ctx context.Context, providerSlug, modelSlug string) (*Detail, error) {
	m, err := s.repo.GetModel(ctx, providerSlug, modelSlug)
	if err != nil {
		return nil, err
	}

	scores, err := s.repo.ModelBenchmarkScores(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	activity, err := s.repo.ModelActivity(ctx, m.ID, activityLimit)
	if err != nil {
		return nil, err
	}
	faq, err := s.repo.ModelFAQ(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	relatedIDs, err := s.repo.RelatedModelIDs(ctx, m, relatedLimit)
	if err != nil {
		return nil, err
	}
	related, err := s.repo.ModelsByIDs(ctx, relatedIDs)
	if err != nil {
		return nil, err
	}

	return &Detail{
		Model: *m, BenchmarkScores: scores, Activity: activity,
		FAQ: faq, Related: orderByIDs(related, relatedIDs),
	}, nil
}

// Summary 实现 GET /catalog/summary。
type Summary struct {
	Counts
	Featured []Model
}

func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	counts, err := s.repo.Counts(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := s.repo.FeaturedModelIDs(ctx, featuredLimit)
	if err != nil {
		return nil, err
	}
	featured, err := s.repo.ModelsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &Summary{Counts: counts, Featured: orderByIDs(featured, ids)}, nil
}

// ── 评测 ────────────────────────────────────────────────────────

// Winner 是某个指标上的优胜者。
type Winner struct {
	Model        Model
	Value        float64
	DisplayValue string
}

// BenchmarkView 是 BenchmarkSummary 加三个优胜者。
type BenchmarkView struct {
	Benchmark
	Quality *Winner
	Value   *Winner
	Speed   *Winner
}

// BenchmarkDetailView 额外带排好序的结果行。
type BenchmarkDetailView struct {
	BenchmarkView
	Results []BenchmarkResultView
}

type BenchmarkResultView struct {
	BenchmarkResult
	Model Model
}

func (s *Service) Benchmarks(ctx context.Context, category string) ([]BenchmarkView, error) {
	list, err := s.repo.Benchmarks(ctx, category)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []BenchmarkView{}, nil
	}
	ids := make([]int64, 0, len(list))
	for _, b := range list {
		ids = append(ids, b.ID)
	}
	results, err := s.repo.BenchmarkResults(ctx, ids)
	if err != nil {
		return nil, err
	}
	models, err := s.modelsFor(ctx, results)
	if err != nil {
		return nil, err
	}

	byBenchmark := map[int64][]BenchmarkResult{}
	for _, r := range results {
		byBenchmark[r.BenchmarkID] = append(byBenchmark[r.BenchmarkID], r)
	}
	out := make([]BenchmarkView, 0, len(list))
	for _, b := range list {
		out = append(out, buildView(b, byBenchmark[b.ID], models))
	}
	return out, nil
}

func (s *Service) Benchmark(ctx context.Context, slug string) (*BenchmarkDetailView, error) {
	b, err := s.repo.Benchmark(ctx, slug)
	if err != nil {
		return nil, err
	}
	results, err := s.repo.BenchmarkResults(ctx, []int64{b.ID})
	if err != nil {
		return nil, err
	}
	models, err := s.modelsFor(ctx, results)
	if err != nil {
		return nil, err
	}

	view := BenchmarkDetailView{BenchmarkView: buildView(*b, results, models)}
	for _, r := range results {
		m, ok := models[r.ModelID]
		if !ok {
			// 评测过但已下架的模型：跳过而不是塞一个空壳。
			// 详情页出现一行没有名字的模型比少一行更难解释。
			continue
		}
		view.Results = append(view.Results, BenchmarkResultView{BenchmarkResult: r, Model: m})
	}
	return &view, nil
}

func (s *Service) modelsFor(ctx context.Context, results []BenchmarkResult) (map[int64]Model, error) {
	seen := map[int64]bool{}
	var ids []int64
	for _, r := range results {
		if !seen[r.ModelID] {
			seen[r.ModelID] = true
			ids = append(ids, r.ModelID)
		}
	}
	models, err := s.repo.ModelsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Model, len(models))
	for _, m := range models {
		out[m.ID] = m
	}
	return out, nil
}

// buildView 算三个优胜者。
//
// **value（性价比）在成本为 0 时不给优胜者，而不是给一个无穷大。**
// 成本为 0 通常意味着这次评测没记账，把它评成"性价比之王"是纯粹的误导。
func buildView(b Benchmark, results []BenchmarkResult, models map[int64]Model) BenchmarkView {
	v := BenchmarkView{Benchmark: b}
	var bestQuality, bestValue, bestSpeed *BenchmarkResult
	var bestValueRatio float64

	for i := range results {
		r := &results[i]
		if _, ok := models[r.ModelID]; !ok {
			continue
		}
		if bestQuality == nil || r.Score > bestQuality.Score {
			bestQuality = r
		}
		if r.CostNano > 0 {
			ratio := r.Score / pricing.NanoToUSD(r.CostNano)
			if bestValue == nil || ratio > bestValueRatio {
				bestValue, bestValueRatio = r, ratio
			}
		}
		if r.DurationMs > 0 && (bestSpeed == nil || r.DurationMs < bestSpeed.DurationMs) {
			bestSpeed = r
		}
	}

	if bestQuality != nil {
		v.Quality = &Winner{models[bestQuality.ModelID], bestQuality.Score,
			fmt.Sprintf("%.1f", bestQuality.Score)}
	}
	if bestValue != nil {
		v.Value = &Winner{models[bestValue.ModelID], bestValueRatio,
			fmt.Sprintf("%.2f 分 / $", bestValueRatio)}
	}
	if bestSpeed != nil {
		v.Speed = &Winner{models[bestSpeed.ModelID], bestSpeed.DurationMs,
			fmt.Sprintf("%.0f ms", bestSpeed.DurationMs)}
	}
	return v
}

// ── 榜单 ────────────────────────────────────────────────────────

// RankingRequest 是 GET /rankings 的参数。
type RankingRequest struct {
	Dimension string
	Modality  string
	Period    string // day | week | month
	Unit      string // absolute | percentage
	Scope     string // all | open-source
}

type RankingEntry struct {
	Rank         int
	Key          string
	Label        string
	Value        float64
	DisplayValue string
	Model        *Model
}

type RankingView struct {
	Dimension   string
	Title       string
	Description string
	Entries     []RankingEntry
	UpdatedAt   time.Time
}

// rankingSpec 把 13 个维度映射到有限的几种数据来源。
//
// **没有数据来源的维度返回空 entries，并在 description 里写明原因。**
// 这是 ARCHITECTURE.md §6.4 已经立过的规矩（宁可让页面保持空状态，
// 也不要假数据，因为假数据会被拿去做决策）在榜单上的兑现——
// 用量类维度要等 B3 的请求日志，应用类维度还要等 agent_framework 埋点。
type rankingSpec struct {
	title  string
	source string // usage | quality | throughput | context_length | session_cost | none
	// pending 是"暂无数据来源"的说明，非空即表示该维度当前恒为空。
	pending       string
	metric        string
	lowerIsBetter bool
}

var rankingSpecs = map[string]rankingSpec{
	"top-models":     {title: "最受欢迎的模型", source: "usage"},
	"market-share":   {title: "用量份额", source: "usage"},
	"leaderboard":    {title: "综合能力榜", source: "quality", metric: "quality"},
	"benchmarks":     {title: "评测得分榜", source: "quality", metric: "quality"},
	"speed":          {title: "吞吐最快", source: "throughput", metric: "throughput"},
	"context-length": {title: "上下文最长", source: "context_length", metric: "context_length"},
	"session-cost":   {title: "单次会话成本最低", source: "session_cost", metric: "price_input", lowerIsBetter: true},
	"task":           {title: "任务榜", pending: "任务维度需要请求日志上的任务标签，等 B3 的数据平面埋点"},
	"languages":      {title: "自然语言榜", pending: "语言维度需要请求内容的语言识别，尚未埋点"},
	"programming":    {title: "编程语言榜", pending: "编程语言维度需要请求内容的语言识别，尚未埋点"},
	"tool-calls":     {title: "工具调用榜", pending: "工具调用数已在 request_logs 里预留字段，等 B3 落地"},
	"images":         {title: "图像模型榜", pending: "图像模型走 media_models，首版只做 chat + embedding"},
	"apps":           {title: "应用榜", pending: "应用维度依赖 agent_framework 埋点（ARCHITECTURE.md §6.4）"},
}

// sessionBasket 是"一次典型会话"的口径：10k 输入 + 2k 输出。
//
// **这个数字必须写在响应的 description 里。** 会话成本没有行业标准口径，
// 不说明篮子的构成，两个平台的"会话成本"根本不可比，而用户会拿它做选型。
const (
	sessionInputTokens  = 10_000
	sessionOutputTokens = 2_000
)

func (s *Service) Ranking(ctx context.Context, req RankingRequest) (*RankingView, error) {
	spec, ok := rankingSpecs[req.Dimension]
	if !ok {
		return nil, domain.Validation("invalid_dimension", "unknown ranking dimension", "dimension")
	}
	days := int32(7)
	switch req.Period {
	case "", "week":
		days = 7
	case "day":
		days = 1
	case "month":
		days = 30
	default:
		return nil, domain.Validation("invalid_period", "period must be day, week or month", "period")
	}
	if req.Unit != "" && req.Unit != "absolute" && req.Unit != "percentage" {
		return nil, domain.Validation("invalid_unit", "unit must be absolute or percentage", "unit")
	}
	if req.Scope != "" && req.Scope != "all" && req.Scope != "open-source" {
		return nil, domain.Validation("invalid_scope", "scope must be all or open-source", "scope")
	}

	view := &RankingView{
		Dimension: req.Dimension, Title: spec.title,
		Entries: []RankingEntry{}, UpdatedAt: time.Now().UTC(),
	}
	if spec.pending != "" {
		view.Description = spec.pending
		return view, nil
	}

	var modalities []string
	if req.Modality != "" {
		modalities = []string{req.Modality}
	}
	openOnly := req.Scope == "open-source"

	var ranked []RankedModel
	var err error
	switch spec.source {
	case "usage":
		ranked, err = s.repo.RankByUsage(ctx, days, modalities, openOnly, rankingCap)
		view.Description = fmt.Sprintf("按最近 %d 天的 token 用量排序。", days)
	case "session_cost":
		ranked, err = s.repo.RankByColumn(ctx, spec.metric, modalities, openOnly, rankingCap)
		view.Description = fmt.Sprintf("一次典型会话（%d 输入 + %d 输出 token）的价格，按公开档价目计算。",
			sessionInputTokens, sessionOutputTokens)
	default:
		ranked, err = s.repo.RankByColumn(ctx, spec.metric, modalities, openOnly, rankingCap)
		view.Description = rankDescription(spec.source)
	}
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(ranked))
	for _, r := range ranked {
		if r.Value != nil || spec.source == "session_cost" {
			ids = append(ids, r.ModelID)
		}
	}
	models, err := s.repo.ModelsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Model, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	entries := make([]RankingEntry, 0, len(ranked))
	var total float64
	for _, r := range ranked {
		m, ok := byID[r.ModelID]
		if !ok {
			continue
		}
		value, display, ok := rankValue(spec, m, r)
		if !ok {
			continue
		}
		total += value
		model := m
		entries = append(entries, RankingEntry{
			Key: modelPublicID(m), Label: m.Name, Value: value,
			DisplayValue: display, Model: &model,
		})
	}

	if spec.lowerIsBetter {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Value < entries[j].Value })
	}
	// percentage 只对可加总的量有意义（用量份额）。对"上下文长度"求百分比
	// 是无意义的数字，因此这里只在 usage 源上应用。
	if req.Unit == "percentage" && spec.source == "usage" && total > 0 {
		for i := range entries {
			pct := entries[i].Value / total * 100
			entries[i].Value = pct
			entries[i].DisplayValue = fmt.Sprintf("%.1f%%", pct)
		}
	}
	for i := range entries {
		entries[i].Rank = i + 1
	}
	view.Entries = entries
	return view, nil
}

func rankDescription(source string) string {
	switch source {
	case "quality":
		return "按公开评测的综合质量分排序。未参与评测的模型不出现在榜上。"
	case "throughput":
		return "按最近 30 天观测到的吞吐（tokens / 秒）排序。"
	case "context_length":
		return "按模型声明的上下文窗口排序。"
	}
	return ""
}

// rankValue 把一行排名换算成展示值。
//
// session_cost 这一档是唯一需要真正算钱的：它走 pricing.Version.Charge，
// **与结算是同一个函数**——否则"榜单说最便宜"与"账单上最便宜"可以是两个模型。
func rankValue(spec rankingSpec, m Model, r RankedModel) (float64, string, bool) {
	switch spec.source {
	case "usage":
		if r.Value == nil {
			return 0, "", false
		}
		return *r.Value, formatTokens(*r.Value), true
	case "quality":
		if r.Value == nil {
			return 0, "", false
		}
		return *r.Value, fmt.Sprintf("%.1f", *r.Value), true
	case "throughput":
		if r.Value == nil {
			return 0, "", false
		}
		return *r.Value, fmt.Sprintf("%.0f t/s", *r.Value), true
	case "context_length":
		if r.Value == nil {
			return 0, "", false
		}
		return *r.Value, formatTokens(*r.Value), true
	case "session_cost":
		if m.Price == nil {
			return 0, "", false // 未定价 ≠ 免费，不进榜
		}
		nano, _, err := m.Price.Charge(pricing.Usage{
			InputTokens: sessionInputTokens, OutputTokens: sessionOutputTokens, Requests: 1,
		})
		if err != nil {
			return 0, "", false
		}
		usd := pricing.NanoToUSD(nano)
		return usd, fmt.Sprintf("$%.4f", usd), true
	}
	return 0, "", false
}

func formatTokens(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fK", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}

// ── 文档 ────────────────────────────────────────────────────────

type DocsGroup struct {
	Title string
	Items []DocsNavItem
}

func (s *Service) DocsNavigation(ctx context.Context) ([]DocsGroup, error) {
	items, err := s.repo.DocsNavigation(ctx)
	if err != nil {
		return nil, err
	}
	var groups []DocsGroup
	for _, item := range items {
		if n := len(groups); n > 0 && groups[n-1].Title == item.GroupTitle {
			groups[n-1].Items = append(groups[n-1].Items, item)
			continue
		}
		groups = append(groups, DocsGroup{Title: item.GroupTitle, Items: []DocsNavItem{item}})
	}
	return groups, nil
}

// Heading 是正文里抽出的目录项。
type Heading struct {
	ID    string
	Title string
	Level int
}

type DocsPageView struct {
	DocsPage
	TableOfContents []Heading
}

func (s *Service) DocsPageBySlug(ctx context.Context, slug string) (*DocsPageView, error) {
	page, err := s.repo.DocsPage(ctx, slug)
	if err != nil {
		return nil, err
	}
	return &DocsPageView{DocsPage: *page, TableOfContents: extractHeadings(page.BodyMarkdown)}, nil
}

type DocsSearchHit struct {
	Slug    string
	Title   string
	Excerpt string
}

func (s *Service) SearchDocs(ctx context.Context, q string) ([]DocsSearchHit, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, domain.Validation("query_too_short", "q must be at least 2 characters", "q")
	}
	matches, err := s.repo.SearchDocs(ctx, q, docsSearchCap)
	if err != nil {
		return nil, err
	}
	hits := make([]DocsSearchHit, 0, len(matches))
	for _, m := range matches {
		hits = append(hits, DocsSearchHit{Slug: m.Slug, Title: m.Title, Excerpt: excerpt(m, q)})
	}
	return hits, nil
}

// excerpt 优先给命中关键词的正文片段，正文没命中时退回描述。
// 给一段与搜索词无关的开头文字，用户看不出为什么这条会被搜出来。
func excerpt(m DocsMatch, q string) string {
	body := m.Body
	idx := strings.Index(strings.ToLower(body), strings.ToLower(q))
	if idx < 0 {
		if m.Description != "" {
			return m.Description
		}
		return truncateRunes(body, 160)
	}
	runes := []rune(body)
	start := len([]rune(body[:idx]))
	from := max(0, start-60)
	to := min(len(runes), start+120)
	out := strings.TrimSpace(string(runes[from:to]))
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

// extractHeadings 从 Markdown 里抽 ## / ### 标题生成目录。
//
// 用行首匹配而不是引一个 Markdown 解析库：目录只需要标题层级，
// 为此拉一个依赖并不划算，且解析库的锚点生成规则未必与前端渲染一致。
func extractHeadings(body string) []Heading {
	out := []Heading{}
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			// 代码块里的 # 是注释不是标题。不处理围栏的话，
			// 一段 shell 示例会在目录里冒出好几个条目。
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level < 1 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
			continue
		}
		title := strings.TrimSpace(trimmed[level:])
		if title == "" {
			continue
		}
		out = append(out, Heading{ID: slugifyHeading(title), Title: title, Level: level})
	}
	return out
}

func slugifyHeading(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ── 辅助 ────────────────────────────────────────────────────────

// orderByIDs 恢复调用方给的顺序。
//
// ModelsByIDs 用 `= ANY($1)` 查，返回顺序由 Postgres 决定——
// 精选模型的展示顺序是运营排的，被数据库的物理顺序打乱是个静默的产品缺陷。
func orderByIDs(models []Model, ids []int64) []Model {
	byID := make(map[int64]Model, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}
	out := make([]Model, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out
}

func modelPublicID(m Model) string { return m.Provider.Slug + "/" + m.Slug }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
