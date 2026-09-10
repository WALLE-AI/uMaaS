-- 目录只读查询（B2）。
--
-- 两条纪律贯穿本文件：
--
--  1. **价格一律通过 pricing_effective_version() 取**，没有任何查询自己写
--     "当前生效版本"的 WHERE 子句（理由见迁移里那个函数的注释，以及 M1b）。
--  2. **可空的联接结果一律用 to_jsonb() 整体带出**。价目与统计都可能不存在，
--     而"没有价目"与"价格为 0"、"没有遥测"与"吞吐为 0"在展示上是完全不同的两件事；
--     摊成一堆列会逼着每一列都取一个哨兵值，那正是 null 被当成 0 的起点。

-- name: ListModels :many
-- 目录列表：过滤 + 排序 + 游标分页，一条 SQL 全包。
--
-- 排序键归一化成 double precision 后乘方向系数（asc=1 / desc=-1），
-- 于是"两种方向 × 四种排序键"塌缩成同一个 `ORDER BY sort_key, id`，
-- 游标比较也退化成一个无 NULL 的二元组比较。
-- 没有价格的模型，排序键取 ±Infinity——契约写死了 **null 恒排在最后，
-- 且 null ≠ 0**；把未定价的模型排成"免费"是最贵的一类展示错误。
WITH params AS (
    SELECT CASE WHEN @direction::text = 'asc' THEN 1 ELSE -1 END AS dir
), base AS (
    SELECT
        m.id,
        m.slug,
        m.display_name,
        m.description,
        m.logo_url,
        m.modalities,
        m.capabilities,
        m.context_length,
        m.quality_score,
        m.released_at,
        m.billing_model,
        m.hosting,
        p.id       AS provider_id,
        p.slug     AS provider_slug,
        p.name     AS provider_name,
        p.logo_url AS provider_logo_url,
        (SELECT count(*) FROM models pm
          WHERE pm.provider_id = p.id AND pm.status = 'listed') AS provider_model_count,
        to_jsonb(pr) AS price_version,
        to_jsonb(st) AS stats,
        params.dir,
        CASE @sort::text
            WHEN 'price'      THEN (pr.rates -> 'input' ->> 'per_million_nano')::double precision
            WHEN 'throughput' THEN st.throughput::double precision
            WHEN 'usage'      THEN st.tokens_30d::double precision
            ELSE extract(epoch FROM m.released_at)
        END AS raw_key
    FROM models m
    JOIN providers p ON p.id = m.provider_id
    CROSS JOIN params
    LEFT JOIN LATERAL (
        SELECT mp.id, mp.rates, mp.currency, mp.effective_from
        FROM model_prices mp
        WHERE mp.id = pricing_effective_version(m.billing_model, 'default', NULL, now())
    ) pr ON true
    LEFT JOIN LATERAL (
        SELECT coalesce(sum(s.tokens), 0)::bigint AS tokens_30d,
               max(s.throughput_tokens_per_second) AS throughput
        FROM model_stats s
        WHERE s.model_id = m.id AND s.day >= (current_date - 30)
    ) st ON true
    -- 目录只暴露 listed。canary 是"可调用但不公开可见"，
    -- delisted 是"不可见也不可新调用"（计价 §3.6-③）。
    WHERE m.status = 'listed'
      AND (sqlc.narg('q')::text IS NULL
           OR m.display_name ILIKE '%' || @q || '%'
           OR m.slug         ILIKE '%' || @q || '%'
           OR p.name         ILIKE '%' || @q || '%')
      AND (sqlc.narg('provider')::text IS NULL OR p.slug = @provider)
      AND (cardinality(@modalities::text[]) = 0 OR m.modalities && @modalities::text[])
      AND (cardinality(@capabilities::text[]) = 0 OR m.capabilities @> @capabilities::text[])
      AND (sqlc.narg('min_context_length')::int IS NULL
           OR m.context_length >= @min_context_length)
      AND (sqlc.narg('zero_data_retention')::boolean IS NULL
           OR m.zero_data_retention = @zero_data_retention)
      -- 价格过滤按纳美元比较。未定价（NULL）的模型在有价格上限时**被排除**：
      -- "未知"不该被当成"便宜"通过筛选。
      AND (sqlc.narg('max_input_price_nano')::bigint IS NULL
           OR (pr.rates -> 'input' ->> 'per_million_nano')::bigint <= @max_input_price_nano)
), keyed AS (
    SELECT base.*,
           (coalesce(raw_key,
                     CASE WHEN dir = 1 THEN 'Infinity' ELSE '-Infinity' END::double precision
            ) * dir)::double precision AS sort_key,
           -- **总数要在游标过滤之前算**。放到最外层的话，翻到第二页时
           -- meta.total 会变成"剩下多少条"，而前端拿它显示"共 N 个模型"。
           count(*) OVER () AS total_count
    FROM base
)
SELECT keyed.id, keyed.slug, keyed.display_name, keyed.description, keyed.logo_url,
       keyed.modalities, keyed.capabilities, keyed.context_length, keyed.quality_score,
       keyed.released_at, keyed.billing_model, keyed.hosting,
       keyed.provider_id, keyed.provider_slug, keyed.provider_name, keyed.provider_logo_url,
       keyed.provider_model_count,
       keyed.price_version, keyed.stats, keyed.sort_key, keyed.total_count
FROM keyed
WHERE (sqlc.narg('cursor_key')::double precision IS NULL
       OR (keyed.sort_key, keyed.id) > (@cursor_key, @cursor_id::bigint))
ORDER BY keyed.sort_key, keyed.id
LIMIT @row_limit::int;

-- name: GetModelByPath :one
-- 详情页。与 ListModels 取价格的方式**必须一致**——同一个函数，同一个 scope。
SELECT
    m.id, m.slug, m.display_name, m.description, m.logo_url,
    m.modalities, m.capabilities, m.context_length, m.max_output_tokens,
    m.architecture, m.input_formats, m.output_formats, m.supported_parameters,
    m.zero_data_retention, m.hosting, m.billing_model, m.status,
    m.quality_score, m.released_at,
    p.id AS provider_id, p.slug AS provider_slug, p.name AS provider_name,
    p.logo_url AS provider_logo_url,
    (SELECT count(*) FROM models pm
      WHERE pm.provider_id = p.id AND pm.status = 'listed') AS provider_model_count,
    to_jsonb(pr) AS price_version, to_jsonb(st) AS stats
FROM models m
JOIN providers p ON p.id = m.provider_id
LEFT JOIN LATERAL (
    SELECT mp.id, mp.rates, mp.currency, mp.effective_from
    FROM model_prices mp
    WHERE mp.id = pricing_effective_version(m.billing_model, 'default', NULL, now())
) pr ON true
LEFT JOIN LATERAL (
    SELECT coalesce(sum(s.tokens), 0)::bigint AS tokens_30d,
           max(s.throughput_tokens_per_second) AS throughput
    FROM model_stats s
    WHERE s.model_id = m.id AND s.day >= (current_date - 30)
) st ON true
WHERE p.slug = @provider_slug AND m.slug = @model_slug AND m.status = 'listed';

-- name: ListModelSummariesByIDs :many
-- 精选模型、评测优胜者、榜单条目、相关模型都要嵌一个 ModelSummary。
-- 走同一个查询，避免"首页的价格"与"列表页的价格"各查各的。
SELECT
    m.id, m.slug, m.display_name, m.description, m.logo_url,
    m.modalities, m.capabilities, m.context_length, m.quality_score,
    m.released_at, m.billing_model, m.hosting,
    p.id AS provider_id, p.slug AS provider_slug, p.name AS provider_name,
    p.logo_url AS provider_logo_url,
    (SELECT count(*) FROM models pm
      WHERE pm.provider_id = p.id AND pm.status = 'listed') AS provider_model_count,
    to_jsonb(pr) AS price_version, to_jsonb(st) AS stats
FROM models m
JOIN providers p ON p.id = m.provider_id
LEFT JOIN LATERAL (
    SELECT mp.id, mp.rates, mp.currency, mp.effective_from
    FROM model_prices mp
    WHERE mp.id = pricing_effective_version(m.billing_model, 'default', NULL, now())
) pr ON true
LEFT JOIN LATERAL (
    SELECT coalesce(sum(s.tokens), 0)::bigint AS tokens_30d,
           max(s.throughput_tokens_per_second) AS throughput
    FROM model_stats s
    WHERE s.model_id = m.id AND s.day >= (current_date - 30)
) st ON true
WHERE m.id = ANY(@ids::bigint[]) AND m.status = 'listed';

-- name: ListFeaturedModelIDs :many
SELECT id FROM models
WHERE status = 'listed' AND featured
ORDER BY featured_rank NULLS LAST, released_at DESC
LIMIT @row_limit::int;

-- name: ListRelatedModelIDs :many
-- 同厂商优先，其次同能力。**不做"你可能还喜欢"式的推荐**：
-- 那需要行为数据，而我们现在没有，猜出来的相关性会被当成事实。
SELECT m.id FROM models m
WHERE m.status = 'listed' AND m.id <> @model_id
ORDER BY (m.provider_id = @provider_id) DESC,
         (m.capabilities && @capabilities::text[]) DESC,
         m.released_at DESC
LIMIT @row_limit::int;

-- name: CatalogCounts :one
SELECT
    (SELECT count(*) FROM models WHERE status = 'listed')                    AS model_count,
    (SELECT count(DISTINCT provider_id) FROM models WHERE status = 'listed') AS provider_count,
    (SELECT coalesce(sum(tokens), 0)::bigint FROM model_stats
      WHERE day >= (current_date - 30))                                      AS monthly_tokens,
    -- 网关 p50：按 tokens 加权平均。B3 之前 model_stats 是空的，
    -- 这里返回 NULL 而不是 0——**没有遥测**与**零延迟**必须能区分。
    (SELECT to_jsonb(sum(p50_latency_ms * tokens) / nullif(sum(tokens), 0))
       FROM model_stats WHERE day >= (current_date - 7)
        AND p50_latency_ms IS NOT NULL)                                      AS gateway_latency_ms_p50,
    (SELECT to_jsonb(max(day)) FROM model_stats)                             AS stats_updated_at;

-- name: ListModelActivity :many
SELECT id, type, title, occurred_at FROM model_activity
WHERE model_id = @model_id ORDER BY occurred_at DESC LIMIT @row_limit::int;

-- name: ListModelFAQ :many
SELECT question, answer FROM model_faq WHERE model_id = $1 ORDER BY position, id;

-- name: ListModelBenchmarkScores :many
SELECT b.slug AS benchmark_slug, b.name AS benchmark_name, r.score, r.percentile
FROM benchmark_results r
JOIN benchmarks b ON b.id = r.benchmark_id
WHERE r.model_id = $1
ORDER BY b.name;

-- ── 评测 ──────────────────────────────────────────────────────

-- name: ListBenchmarks :many
SELECT b.id, b.slug, b.name, b.category, b.description, b.run_count, b.last_run_at,
       (SELECT count(*) FROM benchmark_results r WHERE r.benchmark_id = b.id) AS evaluated_model_count
FROM benchmarks b
WHERE sqlc.narg('category')::text IS NULL OR b.category = @category
ORDER BY b.name;

-- name: GetBenchmarkBySlug :one
SELECT b.id, b.slug, b.name, b.category, b.description, b.methodology,
       b.configuration, b.run_count, b.last_run_at,
       (SELECT count(*) FROM benchmark_results r WHERE r.benchmark_id = b.id) AS evaluated_model_count
FROM benchmarks b WHERE b.slug = $1;

-- name: ListBenchmarkResults :many
-- rank 在查询里算：让 Go 侧再排一遍等于给"两处排序不一致"留位置。
SELECT r.benchmark_id, r.model_id, r.score, r.percentile, r.cost_nano,
       r.duration_ms, r.sample_count,
       rank() OVER (PARTITION BY r.benchmark_id ORDER BY r.score DESC) AS rank
FROM benchmark_results r
WHERE r.benchmark_id = ANY(@benchmark_ids::bigint[])
ORDER BY r.benchmark_id, r.score DESC;

-- ── 榜单 ──────────────────────────────────────────────────────

-- name: RankModelsByUsage :many
SELECT m.id, coalesce(sum(s.tokens), 0)::bigint AS value
FROM models m
JOIN model_stats s ON s.model_id = m.id AND s.day >= (current_date - @days::int)
WHERE m.status = 'listed'
  AND (cardinality(@modalities::text[]) = 0 OR m.modalities && @modalities::text[])
  AND (@open_weights_only::boolean = false OR m.open_weights)
GROUP BY m.id
HAVING coalesce(sum(s.tokens), 0) > 0
ORDER BY value DESC
LIMIT @row_limit::int;

-- name: RankModelsByColumn :many
-- 质量分 / 吞吐 / 上下文长度 / 输入单价，四个维度共用一条查询。
-- 拆成四条会让"某个维度悄悄换了口径"变得极难发现。
SELECT m.id,
       to_jsonb(
           CASE @metric::text
               WHEN 'quality'        THEN m.quality_score::double precision
               WHEN 'context_length' THEN m.context_length::double precision
               WHEN 'throughput'     THEN st.throughput::double precision
               WHEN 'price_input'    THEN (pr.rates -> 'input' ->> 'per_million_nano')::double precision
           END
       ) AS value
FROM models m
LEFT JOIN LATERAL (
    SELECT mp.rates FROM model_prices mp
    WHERE mp.id = pricing_effective_version(m.billing_model, 'default', NULL, now())
) pr ON true
LEFT JOIN LATERAL (
    SELECT max(s.throughput_tokens_per_second) AS throughput
    FROM model_stats s
    WHERE s.model_id = m.id AND s.day >= (current_date - 30)
) st ON true
WHERE m.status = 'listed'
  AND (cardinality(@modalities::text[]) = 0 OR m.modalities && @modalities::text[])
  AND (@open_weights_only::boolean = false OR m.open_weights)
ORDER BY (CASE @metric::text
              WHEN 'quality'        THEN m.quality_score::double precision
              WHEN 'context_length' THEN m.context_length::double precision
              WHEN 'throughput'     THEN st.throughput::double precision
              WHEN 'price_input'    THEN (pr.rates -> 'input' ->> 'per_million_nano')::double precision
          END) DESC NULLS LAST
LIMIT @row_limit::int;

-- ── 文档 ──────────────────────────────────────────────────────

-- name: ListDocsNavigation :many
SELECT slug, group_title, group_position, title, badge
FROM docs_pages ORDER BY group_position, position, title;

-- name: GetDocsPage :one
SELECT slug, title, description, body_markdown, updated_at
FROM docs_pages WHERE slug = $1;

-- name: SearchDocs :many
-- ILIKE 而不是 to_tsvector：默认的分词配置对中文正文只会切出整段，
-- 装 pg_jieba/zhparser 又是一个部署依赖。目录规模下 ILIKE 足够，
-- 换成全文检索时这里是唯一的改动点。
SELECT slug, title, description, body_markdown
FROM docs_pages
WHERE title ILIKE '%' || @q || '%'
   OR description ILIKE '%' || @q || '%'
   OR body_markdown ILIKE '%' || @q || '%'
ORDER BY (title ILIKE '%' || @q || '%') DESC, group_position, position
LIMIT @row_limit::int;
