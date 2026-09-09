-- +goose Up
-- +goose StatementBegin

-- I1 身份。两套**互不相关**的身份体系（ARCHITECTURE.md §4.5）：
--   users/memberships  = 工作空间成员，权限锚定 workspace_id
--   admin_accounts     = 平台管理员，权限是全局的
-- 合并这两套是很有诱惑力的做法，但每次鉴权都会变成"这个角色是对哪个工作空间说的"，
-- 而正确答案是"跟工作空间无关"。

-- ── 用户与租户 ────────────────────────────────────────────────

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    email         text        NOT NULL,
    name          text        NOT NULL,
    -- OAuth 注册的用户没有密码，此列可空。
    password_hash text,
    avatar_url    text,
    status        text        NOT NULL DEFAULT 'active',  -- active | suspended
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- 邮箱大小写不敏感。不做这一条，Alice@x.com 与 alice@x.com 会是两个账号，
-- 而用户会认为自己"密码错了"。
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE workspaces (
    id                 bigserial PRIMARY KEY,
    name               text        NOT NULL,
    slug               text        NOT NULL UNIQUE,
    plan               text        NOT NULL DEFAULT 'free',
    seats_limit        int         NOT NULL DEFAULT 1,
    -- 金额一律纳美元整数（BILLING-AND-PRICING.md §3.5）。
    -- micro 在单 token 粒度上已经截断，分更不够。
    balance_nano       bigint      NOT NULL DEFAULT 0,
    -- 允许余额为负到这个额度。首版保持 0、不产品化，但列必须存在：
    -- §5.3 的补记路径要求余额可短暂为负。
    credit_limit_nano  bigint      NOT NULL DEFAULT 0,
    monthly_budget_nano bigint,
    -- 预扣估算的输出 token 上限（§5.2）。缺省时不取模型上限，
    -- 否则余额充裕的用户也会被误伤成"余额不足"。
    max_estimate_tokens int        NOT NULL DEFAULT 8192,
    -- 数据驻留：any | on_premise_only（SELF-HOSTED-GPU-OPERATIONS.md §2.1）。
    -- 首版可能没有此类客户，但字段先留好——事后加会牵动路由热路径。
    data_residency     text        NOT NULL DEFAULT 'any',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    user_id      bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id bigint      NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    role         text        NOT NULL,  -- owner | admin | developer | viewer
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);

CREATE INDEX memberships_workspace_idx ON memberships (workspace_id);

-- ── 会话 ──────────────────────────────────────────────────────

CREATE TABLE sessions (
    id          bigserial PRIMARY KEY,
    -- 存的是会话令牌的**哈希**，不是令牌本身。
    -- 数据库泄露不等于会话被劫持（ARCHITECTURE.md §5.3）。
    token_hash  bytea       NOT NULL UNIQUE,
    user_id     bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    ip          inet,
    user_agent  text,
    -- 撤销而非删除：/auth/session 要能区分"没登录"与"会话已被踢下线"。
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE oauth_identities (
    id           bigserial PRIMARY KEY,
    user_id      bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider     text        NOT NULL,  -- github | google
    provider_uid text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_uid)
);

-- ── 平台管理员（与上面完全独立）────────────────────────────────

CREATE TABLE admin_accounts (
    id              bigserial PRIMARY KEY,
    email           text        NOT NULL,
    name            text        NOT NULL,
    role            text        NOT NULL,  -- super_admin | operator | viewer
    password_hash   text        NOT NULL,
    -- 全员强制 2FA（已定 2026-09-08）。密钥加密存储，
    -- 与 channels.credentials_enc 同一套 AES-GCM。
    totp_secret_enc bytea,
    totp_enabled    boolean     NOT NULL DEFAULT false,
    -- 管理员账号只能由管理员创建，没有自助注册入口。
    -- 首个 super_admin 由 CLI 创建，是唯一允许为空的情况。
    created_by      bigint      REFERENCES admin_accounts(id),
    last_active_at  timestamptz,
    -- 停用而非删除：审计日志里的 actor 要能一直解析出人名。
    -- DELETE /admin/system/admins/{id} 的语义是停用。
    disabled_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX admin_accounts_email_key ON admin_accounts (lower(email));

CREATE TABLE admin_sessions (
    id         bigserial PRIMARY KEY,
    token_hash bytea       NOT NULL UNIQUE,
    admin_id   bigint      NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    ip         inet,
    user_agent text,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_sessions_admin_idx ON admin_sessions (admin_id) WHERE revoked_at IS NULL;

-- ── 审计 ──────────────────────────────────────────────────────

CREATE TABLE audit_logs (
    id          bigserial PRIMARY KEY,
    actor_kind  text        NOT NULL,  -- admin | user | system
    actor_id    bigint,
    actor_label text        NOT NULL,  -- 冗余存人名/邮箱：账号停用后仍要能读懂
    action      text        NOT NULL,  -- 如 admin.login / pricing.update
    target      text        NOT NULL,
    -- 独立字段而非从 action 字符串推断：审计页支持"仅看破坏性操作"
    -- 一键筛选，这个筛选要走索引（ARCHITECTURE.md §4.4）。
    destructive boolean     NOT NULL DEFAULT false,
    detail      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip          inet,
    request_id  text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_created_idx ON audit_logs (created_at DESC);
CREATE INDEX audit_logs_destructive_idx ON audit_logs (created_at DESC) WHERE destructive;
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_kind, actor_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS admin_sessions;
DROP TABLE IF EXISTS admin_accounts;
DROP TABLE IF EXISTS oauth_identities;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
