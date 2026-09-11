-- +goose Up
-- +goose StatementBegin

-- I3 数据平面 MVP：request_logs（M2，BILLING-AND-PRICING.md §2、§4、§9）。
--
-- **这张表现在改是零成本，等有一个月生产数据就是迁移 + 账单口径解释**
-- （IMPLEMENTATION-PLAN.md 的六条不能违反的顺序约束之一）。因此形状直接按
-- 计价方案定稿的样子建，不留"以后再改"的余地：
--   - upstream_cost_nano / charged_amount_nano 两本账，不是一个 cost 字段
--   - usage_source 三档可信度
--   - is_final 支持 attempt 级记录（I3 单渠道不重试，但字段现在就要有，
--     否则 I4 引入回退链时这是一次迁移）
--   - workspace_id / api_key_id 可空：I3 的数据平面还没有 API Key 鉴权
--     （那是 I5 的 B5），先允许匿名调用落 NULL，鉴权上线后自然收紧

CREATE TABLE request_logs (
    -- Postgres 要求分区表的主键必须包含分区键。id 来自全局序列，
    -- 加上 created_at 只是满足这条约束，不改变"id 事实上全局唯一"这件事。
    id                   bigserial,
    request_id           text        NOT NULL,
    -- attempt 级记录：同一个 request_id 可能有多行（重试），
    -- is_final 标记最终成功的那次。I3 只有单渠道，不重试，
    -- 每个 request_id 现在恒好一行，但**表的形状已经是 attempt 级**。
    attempt_index        int         NOT NULL DEFAULT 0,
    is_final             boolean     NOT NULL DEFAULT true,

    workspace_id         bigint      REFERENCES workspaces(id),
    api_key_id           bigint,

    -- 四个模型名里落库的三个（UNIFIED §5.3）。Resolved 是路由决策，
    -- I3 单渠道直连没有路由可言，因此不落这一列，避免记一个假信息。
    requested_model      text        NOT NULL,
    billing_model        text        NOT NULL,
    upstream_model       text        NOT NULL,

    -- 渠道标识。I3 没有 channels 表（I4 才建），先记 provider profile 名，
    -- I4 接入 channels 后这一列改存 channel_id，同名字段语义自然过渡。
    channel_name         text        NOT NULL,

    prompt_tokens        bigint      NOT NULL DEFAULT 0,
    completion_tokens    bigint      NOT NULL DEFAULT 0,
    cache_read_tokens    bigint      NOT NULL DEFAULT 0,
    cache_write_tokens   bigint      NOT NULL DEFAULT 0,
    reasoning_tokens     bigint      NOT NULL DEFAULT 0,
    tool_call_count      bigint      NOT NULL DEFAULT 0,

    -- 三档可信度（计价 §4.1）：upstream | self_hosted | estimated。
    usage_source         text        NOT NULL,

    -- 两本账（计价 §2）。price_version_id 为空表示这次调用没有解析出
    -- 任何生效价目——I3 还没有预扣/结算（M3 在 I5），这里只是"如果
    -- 现在结算会是多少"的即时计算，不代表真的收了钱。
    price_version_id     bigint      REFERENCES model_prices(id),
    upstream_cost_nano   bigint      NOT NULL DEFAULT 0,
    charged_amount_nano  bigint      NOT NULL DEFAULT 0,
    -- 自建摊销成本在当期结束前是估算值（计价 §4.4）。I3 没有自建供给，
    -- 这一列现在恒为 false，但表形状先留好。
    cost_estimated       boolean     NOT NULL DEFAULT false,

    ttft_ms              int,
    latency_ms           int         NOT NULL,
    status_code          int         NOT NULL,
    -- partial_failure | timeout | upstream_error | client_disconnect | ''（成功）
    error_code           text        NOT NULL DEFAULT '',

    -- agent_framework 现在恒为空——网关还没有这个埋点（ARCHITECTURE.md §6.4）。
    -- 字段先留好，不要用启发式硬凑一个看起来合理的分布。
    agent_framework      text        NOT NULL DEFAULT '',

    created_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 按周分区，与 ARCHITECTURE.md §4.3 的策略一致：这是全系统写入压力
-- 最大的表，且只追加不更新，分区让删除历史数据从 DELETE 变成 DROP TABLE。
-- 开发/测试环境先建当前分区，生产的分区滚动交给部署流水线的定时任务。
CREATE TABLE request_logs_default PARTITION OF request_logs DEFAULT;

CREATE INDEX request_logs_request_id_idx ON request_logs (request_id);
CREATE INDEX request_logs_workspace_created_idx ON request_logs (workspace_id, created_at DESC);
CREATE INDEX request_logs_billing_model_idx ON request_logs (billing_model, created_at DESC);
-- 估算条目要能筛选（计价 §4.1 的纪律：估算占比是运维指标）。
CREATE INDEX request_logs_usage_source_idx ON request_logs (usage_source) WHERE usage_source = 'estimated';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS request_logs;
-- +goose StatementEnd
