-- +goose Up
-- +goose StatementBegin

-- I2 目录与价目。**这是数据模型不可逆性最高的一个迭代**
-- （IMPLEMENTATION-PLAN.md §3-I2）：`model_prices` 一旦有生产数据，
-- 改形状就不再是"改代码"，而是"数据迁移 + 历史账单重算"。

-- ── 供应商与模型 ──────────────────────────────────────────────

CREATE TABLE providers (
    id          bigserial PRIMARY KEY,
    slug        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    -- cloud = 云端 API；self_hosted = 自建 GPU 供给（SELF-HOSTED §1 的三层供给）。
    kind        text        NOT NULL DEFAULT 'cloud',
    logo_url    text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 文本/对话模型。图像/音频/视频走 media_models（UNIFIED §5.2），
-- 合表意味着大半字段恒为 null，本迭代不建——首版只做 chat + embedding。
CREATE TABLE models (
    id            bigserial PRIMARY KEY,
    provider_id   bigint      NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    -- 模型 ID 对外是 `provider/model`，表里拆两列：契约特意把 HTTP 路径拆成
    -- 两个参数就是为了避开编码斜杠，数据层保持一致（ARCHITECTURE.md §4.2）。
    slug          text        NOT NULL,
    display_name  text        NOT NULL,
    description   text        NOT NULL DEFAULT '',
    logo_url      text        NOT NULL DEFAULT '',

    modalities    text[]      NOT NULL DEFAULT '{text}',
    -- 能力标记，供 /models?capability= 过滤与价目完整性校验使用
    -- （计价 §3.6-④：声明了 cache 却没有 cache_read 费率要被拦住）。
    -- I4 的能力协商需要更结构化的 Capability Manifest（UNIFIED §5.4），
    -- 那是一个独立的 jsonb 列，不要把两者混成一个。
    capabilities  text[]      NOT NULL DEFAULT '{}',

    context_length      int,
    max_output_tokens   int,
    architecture        text,
    input_formats       text[]  NOT NULL DEFAULT '{}',
    output_formats      text[]  NOT NULL DEFAULT '{}',
    supported_parameters text[] NOT NULL DEFAULT '{}',
    zero_data_retention boolean NOT NULL DEFAULT false,
    -- 权重是否公开。榜单的 scope=open-source 就靠它，
    -- 用 capabilities 里塞一个 'open_source' 标记来代替会让"能力"这个维度
    -- 同时承担两种语义，筛选时互相污染。
    open_weights        boolean NOT NULL DEFAULT false,

    -- cloud | self。自建模型的**售价机制与云端完全相同**，
    -- 不同的只是成本侧（计价 §3.6-⑤）。这一列只用于展示与成本归集。
    hosting       text        NOT NULL DEFAULT 'cloud',

    -- ModelNames.Billing（UNIFIED §5.3）：价目表的唯一计费主体键。
    -- 与 provider/slug 分开是为了让促销别名能共用一套价格，
    -- 也让"路由不影响计费"成立（计价 §4.2）。
    billing_model text        NOT NULL,

    -- draft | listed | canary | delisted。管的是**可见性与可调用性**，
    -- 与价目版本正交（计价 §3.6-③）。draft 可以没有价目；
    -- listed/canary 必须有；delisted 的价目**不可删**，历史账单要能重算。
    status        text        NOT NULL DEFAULT 'draft',
    canary_percent int,

    -- 首页精选。放列而不是靠"最近发布的三个"，是因为精选是运营决策。
    featured      boolean     NOT NULL DEFAULT false,
    featured_rank int,

    -- 综合质量分（0–100）。由评测结果回写，没有评测时为 NULL 而不是 0——
    -- "没测过"与"测了 0 分"在 UI 上必须能区分。
    quality_score numeric(5,2),

    released_at   timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider_id, slug)
);

CREATE INDEX models_public_idx    ON models (released_at DESC) WHERE status = 'listed';
CREATE INDEX models_billing_idx   ON models (billing_model);
CREATE INDEX models_featured_idx  ON models (featured_rank) WHERE featured;
CREATE INDEX models_modality_idx  ON models USING gin (modalities);
CREATE INDEX models_capability_idx ON models USING gin (capabilities);

-- ── 价目（计价方案 §3.2）───────────────────────────────────────
--
-- 价格**不放在 models 表上**：变化频率远高于模型元数据、同一模型可以有多套
-- 价格（计费别名/协议价）、且价格必须版本化而元数据不必。

CREATE TABLE model_prices (
    id             bigserial PRIMARY KEY,
    billing_model  text        NOT NULL,
    -- default | plan | workspace，解析顺序见 §3.4。
    scope_kind     text        NOT NULL DEFAULT 'default',
    scope_id       text,
    currency       text        NOT NULL DEFAULT 'USD',
    -- 计价单元 → 费率。整数纳美元（§3.5）：
    -- $0.15/1M = 1.5e-7 USD/token，比 1 微美元还小一个数量级，
    -- 用 micro 会把短请求系统性截断到 0，且偏差方向恒定。
    rates          jsonb       NOT NULL,
    effective_from timestamptz NOT NULL DEFAULT now(),
    -- NULL = 当前生效。改价是"关闭旧版本 + 插入新版本"，绝不原地 UPDATE，
    -- 否则历史账单会被静默改掉（§3.3）。
    effective_to   timestamptz,
    -- admin（唯一写入口）| upstream_diff_accepted。
    -- 上游同步只产出建议，没有任何路径能绕过 admin 直接改价（§3.6-①）。
    source         text        NOT NULL DEFAULT 'admin',
    applied_by     text        NOT NULL,
    note           text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX model_prices_version_key
    ON model_prices (billing_model, scope_kind, COALESCE(scope_id, ''), effective_from);
CREATE INDEX model_prices_current_idx
    ON model_prices (billing_model, effective_from DESC) WHERE effective_to IS NULL;

-- pricing_effective_version 是"哪一版价目在 at 时刻对该计费主体生效"的
-- **唯一定义**。
--
-- 它存在的理由就是 IMPLEMENTATION-PLAN.md 里 M1b 那一条：catalog 的展示价
-- 与结算价若来自两条查询路径，不一致时**用户一定先于我们发现**。目录列表要在
-- SQL 里按价格排序/过滤（Go 侧做不到），结算要在 Go 侧按 received_at 解析——
-- 两个场景都只能调这个函数，谁都不许自己写一遍 WHERE 子句。
--
-- 注意 at 参数：一次跨越改价时刻的长请求（流式可以跑几分钟）必须整体按
-- **受理时**的价目结算，所以这里绝不能写死 now()（§3.3-3）。
CREATE FUNCTION pricing_effective_version(
    p_billing_model text,
    p_scope_kind    text,
    p_scope_id      text,
    p_at            timestamptz
) RETURNS bigint
LANGUAGE sql STABLE AS $$
    SELECT id FROM model_prices
    WHERE billing_model = p_billing_model
      AND scope_kind    = p_scope_kind
      AND COALESCE(scope_id, '') = COALESCE(p_scope_id, '')
      AND effective_from <= p_at
      AND (effective_to IS NULL OR effective_to > p_at)
    ORDER BY effective_from DESC
    LIMIT 1;
$$;

-- ── 统计（物化聚合）───────────────────────────────────────────
--
-- 分析页要查"近 30 天按厂商分组的 tokens"，直接扫 request_logs 在千万行级别
-- 会退化（ARCHITECTURE.md §4.3）。B3 落地前这张表是空的，
-- 于是目录页的用量/吞吐显示为 0 与 NULL——**空状态，不是假数据**。
CREATE TABLE model_stats (
    model_id       bigint  NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    day            date    NOT NULL,
    requests       bigint  NOT NULL DEFAULT 0,
    tokens         bigint  NOT NULL DEFAULT 0,
    -- 输入/输出分开存，而不是只存一个 tokens 合计。
    -- **改价预检需要它**（计价 §3.3）：新价 × 近 7 日用量要分单元乘，
    -- 只有合计就只能假设一个输入输出比例，而那个假设会被当成事实读。
    input_tokens   bigint  NOT NULL DEFAULT 0,
    output_tokens  bigint  NOT NULL DEFAULT 0,
    -- 成本与售价是两本账（§2），聚合层同样要分开。
    -- M2 之前这两列是 0，改价预检据此报告"暂无成本数据"而不是报告零成本。
    upstream_cost_nano  bigint NOT NULL DEFAULT 0,
    charged_amount_nano bigint NOT NULL DEFAULT 0,
    p50_latency_ms numeric(10,2),
    p95_latency_ms numeric(10,2),
    throughput_tokens_per_second numeric(10,2),
    PRIMARY KEY (model_id, day)
);

CREATE INDEX model_stats_day_idx ON model_stats (day DESC);

-- ── 评测 ──────────────────────────────────────────────────────

CREATE TABLE benchmarks (
    id            bigserial PRIMARY KEY,
    slug          text        NOT NULL UNIQUE,
    name          text        NOT NULL,
    -- agent | reasoning | search | coding | multimodal（契约的枚举）
    category      text        NOT NULL,
    description   text        NOT NULL DEFAULT '',
    methodology   text        NOT NULL DEFAULT '',
    configuration jsonb       NOT NULL DEFAULT '{}'::jsonb,
    run_count     int         NOT NULL DEFAULT 0,
    last_run_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE benchmark_results (
    id            bigserial PRIMARY KEY,
    benchmark_id  bigint      NOT NULL REFERENCES benchmarks(id) ON DELETE CASCADE,
    model_id      bigint      NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    score         numeric(10,4) NOT NULL,
    percentile    numeric(5,2),
    -- 评测花了多少钱。纳美元，与 request_logs 的两本账同单位（§3.5）。
    cost_nano     bigint      NOT NULL DEFAULT 0,
    duration_ms   numeric(12,2) NOT NULL DEFAULT 0,
    sample_count  int         NOT NULL DEFAULT 1,
    run_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (benchmark_id, model_id)
);

CREATE INDEX benchmark_results_model_idx ON benchmark_results (model_id);

-- ── 模型详情页的几块内容 ───────────────────────────────────────

-- 契约的 ActivityItem。type: status | pricing | release | routing。
-- **改价会在这里留一条**——用户问"为什么这个月贵了"时，
-- 详情页上就能自证，不必等客服去翻审计日志。
CREATE TABLE model_activity (
    id          bigserial PRIMARY KEY,
    model_id    bigint      NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    type        text        NOT NULL,
    title       text        NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX model_activity_model_idx ON model_activity (model_id, occurred_at DESC);

CREATE TABLE model_faq (
    id       bigserial PRIMARY KEY,
    model_id bigint NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    question text   NOT NULL,
    answer   text   NOT NULL,
    position int    NOT NULL DEFAULT 0
);

CREATE INDEX model_faq_model_idx ON model_faq (model_id, position);

-- ── 文档 ──────────────────────────────────────────────────────

CREATE TABLE docs_pages (
    id             bigserial PRIMARY KEY,
    slug           text        NOT NULL UNIQUE,
    group_title    text        NOT NULL,
    group_position int         NOT NULL DEFAULT 0,
    position       int         NOT NULL DEFAULT 0,
    title          text        NOT NULL,
    description    text        NOT NULL DEFAULT '',
    badge          text        NOT NULL DEFAULT '',
    body_markdown  text        NOT NULL DEFAULT '',
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX docs_pages_nav_idx ON docs_pages (group_position, position);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS docs_pages;
DROP TABLE IF EXISTS model_faq;
DROP TABLE IF EXISTS model_activity;
DROP TABLE IF EXISTS benchmark_results;
DROP TABLE IF EXISTS benchmarks;
DROP TABLE IF EXISTS model_stats;
DROP FUNCTION IF EXISTS pricing_effective_version(text, text, text, timestamptz);
DROP TABLE IF EXISTS model_prices;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS providers;
-- +goose StatementEnd
