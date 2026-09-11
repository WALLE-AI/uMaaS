-- 数据平面用到的目录查询（B3）。
--
-- 与 catalog.sql 的只读端点分开：那些服务的是"给人看的目录页"，
-- 这一条服务的是"请求进来时能不能转发"。前者只暴露 listed，
-- 后者要放行 canary——canary 的定义就是"可调用但不公开可见"
-- （BILLING-AND-PRICING.md §3.6-③）。draft/delisted 一律拒绝。

-- name: GetCallableModelByPath :one
SELECT m.id, m.billing_model, m.hosting, m.status, m.slug, m.capabilities
FROM models m
WHERE m.provider_id = (SELECT p.id FROM providers p WHERE p.slug = $1)
  AND m.slug = $2
  AND m.status IN ('listed', 'canary');
