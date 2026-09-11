-- +goose Up
-- +goose StatementBegin

-- I4 供给与路由：provider_profiles / channels / channel_models / routing_policies
-- （UNIFIED-PROVIDER-INTERFACE.md §4.4、§5.2、§5.5）。
--
-- **这是 P2 判据的数据面基础**：新增一个 OpenAI 兼容厂商应当是数据库里一行，
-- 不是一个 Go 文件。provider_profiles 存协议族与鉴权形态，channels 存
-- 这个协议族的一个具体接入实例（可以有多个），channel_models 是路由索引，
-- 用正规化关联表取代 new-api 的逗号分隔字符串（§5.2 的对比表）。

-- ── 供应商协议档案（L4 传输层）────────────────────────────────

CREATE TABLE provider_profiles (
    id           bigserial PRIMARY KEY,
    slug         text        NOT NULL UNIQUE,
    name         text        NOT NULL,
    -- openai_compat 覆盖约七成厂商（§4.4）；anthropic/gemini 留给 I9+。
    protocol     text        NOT NULL DEFAULT 'openai_compat',
    base_url     text        NOT NULL,
    -- bearer | header。查询鉴权（aws_sigv4/gcp_oauth）不在 I4 范围。
    auth_scheme  text        NOT NULL DEFAULT 'bearer',
    auth_header  text        NOT NULL DEFAULT '',
    -- 与标准协议的偏离项（Quirks）：不支持 stream usage、必填 max_tokens 等。
    -- 绝大多数厂商只需要设一两个字段——这正是"零代码接入"能成立的关键。
    quirks       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- ── 渠道：一个协议档案的具体接入实例 ────────────────────────────

CREATE TABLE channels (
    id                  bigserial PRIMARY KEY,
    provider_profile_id bigint      NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    name                text        NOT NULL,
    base_url_override   text,
    -- 上游密钥加密存储（AES-GCM），与 admin_accounts.totp_secret_enc 同一套。
    credentials_enc     bytea       NOT NULL,
    region              text        NOT NULL DEFAULT '',
    priority            int         NOT NULL DEFAULT 0,
    weight              int         NOT NULL DEFAULT 100,
    -- active | disabled。人工下线一个渠道不是"权重设成 0"，
    -- 是状态语义——0 权重在评分公式里还是会被选中（只是概率低）。
    status              text        NOT NULL DEFAULT 'active',

    -- ── 健康状态（B4）────────────────────────────────────────
    -- Beta(alpha, beta) 后验，Thompson 采样直接从这两个参数抽样。
    -- 用确定性成功率会有硬伤：失败几次后分数变低就再也选不中，
    -- 永远没机会证明自己已经恢复（UNIFIED §5.5）。
    health_alpha        double precision NOT NULL DEFAULT 1,
    health_beta         double precision NOT NULL DEFAULT 1,
    -- 延迟的指数移动平均（毫秒），供 speed 因子归一化。
    latency_ema_ms       double precision,
    consecutive_failures int        NOT NULL DEFAULT 0,
    -- 429 之后短暂压低该渠道（rateLimitFactor 的输入），而不是直接拉黑：
    -- 限流通常几十秒后自愈，拉黑要靠人工恢复，代价不对等。
    rate_limited_until   timestamptz,
    -- 探针退休：连续失败的渠道拉长探测间隔，避免对已死的上游
    -- 持续打无效请求（§5.5 最后一段）。
    probe_interval_seconds int      NOT NULL DEFAULT 60,
    last_probe_at        timestamptz,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX channels_provider_profile_idx ON channels (provider_profile_id);
CREATE INDEX channels_active_idx ON channels (status) WHERE status = 'active';

-- ── 路由索引：正规化关联表，取代逗号分隔字符串 ───────────────────

CREATE TABLE channel_models (
    channel_id           bigint  NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    model_id             bigint  NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    -- 上游实际叫什么，取代 ModelMapping JSON（§5.2）。
    upstream_model_name  text    NOT NULL,
    enabled              boolean NOT NULL DEFAULT true,
    priority_override    int,
    weight_override       int,
    -- 手工连通性测试（X10）没有显式指定模型时，优先探这个模型
    -- ——比"随便挑一个模型"更能代表真实可用性（§5.6-①）。
    is_probe             boolean NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, model_id)
);

CREATE INDEX channel_models_model_idx ON channel_models (model_id, enabled);

-- ── 路由策略（X9）────────────────────────────────────────────

CREATE TABLE routing_policies (
    id             bigserial PRIMARY KEY,
    name           text        NOT NULL,
    -- global | workspace | model。解析顺序 model > workspace > global，
    -- 整体覆盖不做字段级合并——与计价方案 §3.4 同一条原则。
    scope_kind     text        NOT NULL DEFAULT 'global',
    scope_id       text,
    -- balanced | smartest | fastest | reliable | custom | priority。
    strategy       text        NOT NULL DEFAULT 'balanced',
    -- {reliability, speed, intelligence}，仅 strategy=custom 时使用。
    weights        jsonb,
    fallback_depth int         NOT NULL DEFAULT 3,
    updated_by     text        NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX routing_policies_scope_key
    ON routing_policies (scope_kind, COALESCE(scope_id, ''));

-- request_logs 需要区分"谁发起的这次调用"，才能让手工渠道测试与自动
-- 健康探针的费用可见、但不污染任何客户账单（计价方案 §4.3 最后一行）。
ALTER TABLE request_logs ADD COLUMN origin text NOT NULL DEFAULT 'user';
-- I3 单渠道直连时 channel_name 记的是 provider profile 名；
-- I4 起改记 channel_id，语义自然过渡到"这次调用实际打到了哪个渠道"。
ALTER TABLE request_logs ADD COLUMN channel_id bigint REFERENCES channels(id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs DROP COLUMN IF EXISTS channel_id;
ALTER TABLE request_logs DROP COLUMN IF EXISTS origin;
DROP TABLE IF EXISTS routing_policies;
DROP TABLE IF EXISTS channel_models;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS provider_profiles;
-- +goose StatementEnd
