-- +goose Up
-- +goose StatementBegin

-- I0 只建骨架所需的最小集合：一张迁移自检表 + 后续迭代要用的枚举与扩展。
-- 业务表按迭代逐步加入（I1 身份、I2 目录与价目、I3 请求日志…），
-- 每次一个迁移文件，不回头改已发布的迁移。

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 平台自检表：记录本二进制的构建信息与最近一次启动。
-- 它的实际用途是让 /readyz 能区分"数据库连得上"与"schema 是这个版本的"。
CREATE TABLE IF NOT EXISTS platform_meta (
    key         text PRIMARY KEY,
    value       jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO platform_meta (key, value)
VALUES ('schema', '{"initialized_by": "I0", "iteration": "I0"}'::jsonb)
ON CONFLICT (key) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS platform_meta;
-- +goose StatementEnd
