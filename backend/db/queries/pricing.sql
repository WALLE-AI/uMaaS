-- 价目查询（M1 / M1a / M1b）。
--
-- **本文件是价目的唯一 SQL 入口。** catalog 侧只在 catalog.sql 里通过同一个
-- pricing_effective_version() 取默认档价格用于排序过滤，展示与结算的费率解析
-- 都走 internal/pricing 的同一个 Go 函数（计价 §3.6-②）。

-- name: ResolvePriceVersion :one
-- 价目解析的**唯一实现**。四层优先级（计价 §3.4）：
--
--     workspace 专属协议价 > plan 价目 > 计费别名默认价 > 模型默认价
--
-- 每一层是**整体覆盖**，不是字段级合并。字段级合并看起来灵活，
-- 实际会产生"没人能说清这个模型现在多少钱"的价目，排查成本极高。
--
-- at 由调用方给：结算必须传 RequestContext.ReceivedAt 而不是 now()，
-- 否则一次跨越改价时刻的长流会中途换价（§3.3-3）。
SELECT mp.* FROM model_prices mp
WHERE mp.id = coalesce(
    pricing_effective_version(@billing_model,          'workspace', sqlc.narg('workspace_id')::text, @at),
    pricing_effective_version(@billing_model,          'plan',      sqlc.narg('plan')::text,         @at),
    pricing_effective_version(@billing_model,          'default',   NULL,                            @at),
    pricing_effective_version(@fallback_billing_model, 'default',   NULL,                            @at)
);

-- name: GetPriceVersionByID :one
SELECT * FROM model_prices WHERE id = $1;

-- name: ListPriceVersions :many
SELECT * FROM model_prices
WHERE billing_model = $1
ORDER BY effective_from DESC
LIMIT $2;

-- name: ClosePriceVersions :execrows
-- 改价 = 关闭旧版本 + 插入新版本（§3.3-1）。**绝不原地 UPDATE 费率**：
-- 那会让历史账单在改价当天被静默重算成另一个数字。
UPDATE model_prices
SET effective_to = @effective_from
WHERE billing_model = @billing_model
  AND scope_kind = @scope_kind
  AND COALESCE(scope_id, '') = COALESCE(sqlc.narg('scope_id')::text, '')
  AND effective_to IS NULL
  AND effective_from <= @effective_from;

-- name: InsertPriceVersion :one
INSERT INTO model_prices (
    billing_model, scope_kind, scope_id, currency, rates,
    effective_from, source, applied_by, note
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- ── admin 写入口（M1a）─────────────────────────────────────────

-- name: UpsertProvider :one
INSERT INTO providers (slug, name, kind, logo_url, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (slug) DO UPDATE
SET name = excluded.name, kind = excluded.kind,
    logo_url = excluded.logo_url, description = excluded.description,
    updated_at = now()
RETURNING *;

-- name: GetProviderBySlug :one
SELECT * FROM providers WHERE slug = $1;

-- name: UpsertModel :one
INSERT INTO models (
    provider_id, slug, display_name, description, logo_url,
    modalities, capabilities, context_length, max_output_tokens,
    architecture, input_formats, output_formats, supported_parameters,
    zero_data_retention, open_weights, hosting, billing_model, released_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
)
ON CONFLICT (provider_id, slug) DO UPDATE
SET display_name = excluded.display_name,
    description = excluded.description,
    logo_url = excluded.logo_url,
    modalities = excluded.modalities,
    capabilities = excluded.capabilities,
    context_length = excluded.context_length,
    max_output_tokens = excluded.max_output_tokens,
    architecture = excluded.architecture,
    input_formats = excluded.input_formats,
    output_formats = excluded.output_formats,
    supported_parameters = excluded.supported_parameters,
    zero_data_retention = excluded.zero_data_retention,
    open_weights = excluded.open_weights,
    hosting = excluded.hosting,
    billing_model = excluded.billing_model,
    updated_at = now()
-- **status 不在 SET 列表里**：发布/下架是独立的、要校验价目完整性的动作，
-- 不能被一次元数据编辑顺手带过去（计价 §3.6-③）。
RETURNING *;

-- name: GetAdminModelByID :one
SELECT m.*, p.slug AS provider_slug, p.name AS provider_name
FROM models m JOIN providers p ON p.id = m.provider_id
WHERE m.id = $1;

-- name: GetAdminModelByPath :one
SELECT m.*, p.slug AS provider_slug, p.name AS provider_name
FROM models m JOIN providers p ON p.id = m.provider_id
WHERE p.slug = $1 AND m.slug = $2;

-- name: ListAdminModels :many
-- admin 看得到全部四态；web 只看得到 listed。
SELECT m.*, p.slug AS provider_slug, p.name AS provider_name,
       pricing_effective_version(m.billing_model, 'default', NULL, now()) AS price_version_id,
       (SELECT coalesce(sum(s.requests), 0)::bigint FROM model_stats s
         WHERE s.model_id = m.id AND s.day >= (current_date - 7)) AS calls_last_7d
FROM models m JOIN providers p ON p.id = m.provider_id
WHERE sqlc.narg('status')::text IS NULL OR m.status = @status
ORDER BY p.slug, m.slug;

-- name: SetModelStatus :one
UPDATE models
SET status = $2, canary_percent = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountRecentCalls :one
-- "近 7 日有真实调用量"决定改价是否需要二次确认与强制 note（§3.6-⑥）。
-- B3 之前 model_stats 是空的，此时返回 0 是**事实**而不是兜底。
SELECT coalesce(sum(requests), 0)::bigint            AS calls,
       coalesce(sum(input_tokens), 0)::bigint        AS input_tokens,
       coalesce(sum(output_tokens), 0)::bigint       AS output_tokens,
       coalesce(sum(upstream_cost_nano), 0)::bigint  AS upstream_cost_nano,
       coalesce(sum(charged_amount_nano), 0)::bigint AS charged_amount_nano
FROM model_stats
WHERE model_id = $1 AND day >= (current_date - 7);

-- name: InsertModelActivity :exec
INSERT INTO model_activity (model_id, type, title)
VALUES ($1, $2, $3);
