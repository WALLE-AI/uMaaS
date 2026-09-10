-- 开发种子数据的写入（`umaas seed catalog`）。
--
-- **这些查询只服务开发环境**，seed 命令在 env=prod 时直接拒绝执行。
-- 它们写的都是"展示层属性"（精选位、质量分、评测结果、文档），
-- 唯独不写价目——价目只能走 pricing.sql 的那条路径（计价 §3.6-①）。

-- name: SetModelPresentation :exec
UPDATE models
SET featured = $2, featured_rank = $3, quality_score = $4, updated_at = now()
WHERE id = $1;

-- name: UpsertBenchmark :one
INSERT INTO benchmarks (slug, name, category, description, methodology,
                        configuration, run_count, last_run_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (slug) DO UPDATE
SET name = excluded.name, category = excluded.category,
    description = excluded.description, methodology = excluded.methodology,
    configuration = excluded.configuration, run_count = excluded.run_count,
    last_run_at = excluded.last_run_at
RETURNING id;

-- name: UpsertBenchmarkResult :exec
INSERT INTO benchmark_results (benchmark_id, model_id, score, percentile,
                               cost_nano, duration_ms, sample_count)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (benchmark_id, model_id) DO UPDATE
SET score = excluded.score, percentile = excluded.percentile,
    cost_nano = excluded.cost_nano, duration_ms = excluded.duration_ms,
    sample_count = excluded.sample_count, run_at = now();

-- name: UpsertModelFAQ :exec
INSERT INTO model_faq (model_id, question, answer, position)
SELECT $1, $2, $3, $4
WHERE NOT EXISTS (
    SELECT 1 FROM model_faq WHERE model_id = $1 AND question = $2
);

-- name: UpsertDocsPage :exec
INSERT INTO docs_pages (slug, group_title, group_position, position,
                        title, description, badge, body_markdown, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (slug) DO UPDATE
SET group_title = excluded.group_title, group_position = excluded.group_position,
    position = excluded.position, title = excluded.title,
    description = excluded.description, badge = excluded.badge,
    body_markdown = excluded.body_markdown, updated_at = now();
