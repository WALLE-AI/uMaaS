-- 供给与路由（I4：P2/P3/B4/X9/X10）。

-- name: UpsertProviderProfile :one
INSERT INTO provider_profiles (slug, name, protocol, base_url, auth_scheme, auth_header, quirks)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (slug) DO UPDATE
SET name = excluded.name, protocol = excluded.protocol, base_url = excluded.base_url,
    auth_scheme = excluded.auth_scheme, auth_header = excluded.auth_header,
    quirks = excluded.quirks, updated_at = now()
RETURNING *;

-- name: CreateChannel :one
INSERT INTO channels (provider_profile_id, name, base_url_override, credentials_enc,
                      region, priority, weight, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpsertChannelModel :exec
INSERT INTO channel_models (channel_id, model_id, upstream_model_name, enabled,
                            priority_override, weight_override, is_probe)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (channel_id, model_id) DO UPDATE
SET upstream_model_name = excluded.upstream_model_name, enabled = excluded.enabled,
    priority_override = excluded.priority_override, weight_override = excluded.weight_override,
    is_probe = excluded.is_probe;

-- name: ListCandidateChannels :many
-- 一个模型的全部候选渠道（B3/P3 的路由热路径）。
-- 复合索引 (model_id, enabled) 已经建在 channel_models 上；渠道状态与
-- 限流窗口在这里一并过滤，避免路由代码里再判断一次。
SELECT
    c.id AS channel_id, c.name AS channel_name, c.provider_profile_id,
    c.priority, c.weight, c.health_alpha, c.health_beta, c.latency_ema_ms,
    c.rate_limited_until, c.consecutive_failures, c.probe_interval_seconds, c.last_probe_at,
    c.credentials_enc, c.base_url_override,
    p.slug AS provider_slug, p.protocol, p.base_url AS provider_base_url,
    p.auth_scheme, p.auth_header, p.quirks,
    cm.upstream_model_name, cm.priority_override, cm.weight_override
FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
JOIN provider_profiles p ON p.id = c.provider_profile_id
WHERE cm.model_id = $1 AND cm.enabled = true AND c.status = 'active';

-- name: GetProbeChannelModel :one
-- 手工连通性测试选模型的优先级第二档：该渠道标了 is_probe 的模型
-- （UNIFIED §5.6-①）。第一档"admin 显式指定"与第三档"近 7 日调用量最高"
-- 由调用方在 Go 侧决定，不是每一档都值得写成 SQL。
SELECT cm.model_id, cm.upstream_model_name, m.billing_model
FROM channel_models cm
JOIN models m ON m.id = cm.model_id
WHERE cm.channel_id = $1 AND cm.is_probe = true AND cm.enabled = true
LIMIT 1;

-- name: GetChannelByID :one
SELECT c.*, p.slug AS provider_slug, p.protocol, p.base_url AS provider_base_url,
       p.auth_scheme, p.auth_header, p.quirks
FROM channels c JOIN provider_profiles p ON p.id = c.provider_profile_id
WHERE c.id = $1;

-- name: RecordChannelSuccess :exec
-- Beta 后验更新：一次成功 = alpha+1。同时把限流窗口清掉、连续失败清零、
-- 更新延迟 EMA（平滑系数 0.3，足够快地反映最近状况又不被单次抖动带偏）。
UPDATE channels
SET health_alpha = health_alpha + 1,
    consecutive_failures = 0,
    latency_ema_ms = CASE
        WHEN latency_ema_ms IS NULL THEN sqlc.arg(latency_ms)::double precision
        ELSE latency_ema_ms * 0.7 + sqlc.arg(latency_ms)::double precision * 0.3
    END,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordChannelFailure :exec
-- Beta 后验更新：一次失败 = beta+1。429 时额外设置限流窗口
-- （rateLimitFactor 的输入，UNIFIED §5.5 的乘性护栏）。
UPDATE channels
SET health_beta = health_beta + 1,
    consecutive_failures = consecutive_failures + 1,
    rate_limited_until = CASE WHEN sqlc.arg(rate_limited)::boolean
                               THEN now() + (sqlc.arg(rate_limit_seconds)::int || ' seconds')::interval
                               ELSE rate_limited_until END,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: TouchChannelProbe :exec
-- 探针退休（§5.5 末段）：连续失败越多，下一次自动探测的间隔越长，
-- 上限由调用方算好传入——避免对已死的上游持续打无效请求。
UPDATE channels
SET last_probe_at = now(), probe_interval_seconds = $2
WHERE id = $1;

-- name: ListChannelsDueForProbe :many
SELECT id, consecutive_failures, probe_interval_seconds
FROM channels
WHERE status = 'active'
  AND (last_probe_at IS NULL OR last_probe_at < now() - (probe_interval_seconds || ' seconds')::interval);

-- ── 路由策略（X9）───────────────────────────────────────────────

-- name: ResolveRoutingPolicy :one
-- 解析顺序 model > workspace > global，整体覆盖（计价 §3.4 同一原则）。
--
-- **不能写成 `(scope_kind, scope_id) IN ((...), (...), ('global', NULL))`**：
-- SQL 的行比较里只要有一个分量是 NULL，整行比较结果就是 UNKNOWN 而不是
-- TRUE——即使目标行的 scope_id 也是 NULL。用这种写法时 scope_kind='global'
-- 的策略永远匹配不到，会静默退回调用方的默认值（这里是 balanced），
-- 而不是报错，因此第一次真实验证时才会暴露。改用显式的 OR + IS NOT
-- DISTINCT FROM，它对 NULL 的语义是"两边都是 NULL 时也算相等"。
SELECT * FROM routing_policies
WHERE (scope_kind = 'model'     AND scope_id IS NOT DISTINCT FROM sqlc.narg('model_id')::text)
   OR (scope_kind = 'workspace' AND scope_id IS NOT DISTINCT FROM sqlc.narg('workspace_id')::text)
   OR (scope_kind = 'global'    AND scope_id IS NULL)
ORDER BY CASE scope_kind WHEN 'model' THEN 0 WHEN 'workspace' THEN 1 ELSE 2 END
LIMIT 1;

-- name: UpsertRoutingPolicy :one
INSERT INTO routing_policies (name, scope_kind, scope_id, strategy, weights, fallback_depth, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (scope_kind, COALESCE(scope_id, '')) DO UPDATE
SET name = excluded.name, strategy = excluded.strategy, weights = excluded.weights,
    fallback_depth = excluded.fallback_depth, updated_by = excluded.updated_by, updated_at = now()
RETURNING *;
