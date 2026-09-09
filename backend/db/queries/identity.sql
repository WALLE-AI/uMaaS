-- 手写 SQL，sqlc 生成类型安全的 Go（ARCHITECTURE.md §2.2）。
-- 这个系统里最有价值的查询恰恰是最复杂的那些，ORM 在那里帮不上忙。

-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, avatar_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (name, slug, plan)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetWorkspaceByID :one
SELECT * FROM workspaces WHERE id = $1;

-- name: CreateMembership :exec
INSERT INTO memberships (user_id, workspace_id, role)
VALUES ($1, $2, $3);

-- name: ListMembershipsByUser :many
SELECT m.user_id, m.workspace_id, m.role, m.created_at,
       w.name AS workspace_name, w.slug AS workspace_slug
FROM memberships m
JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY m.created_at;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, ip, user_agent)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- 一次查询同时拿到会话与用户：会话校验在每个请求上都要跑，
-- 分两次查等于把控制平面的 QPS 乘以 2。
SELECT s.id, s.token_hash, s.user_id, s.expires_at, s.revoked_at, s.created_at,
       u.email, u.name, u.avatar_url, u.status
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < now() - INTERVAL '30 days';

-- name: UpsertOAuthIdentity :exec
INSERT INTO oauth_identities (user_id, provider, provider_uid)
VALUES ($1, $2, $3)
ON CONFLICT (provider, provider_uid) DO NOTHING;

-- name: GetUserByOAuthIdentity :one
SELECT u.* FROM users u
JOIN oauth_identities oi ON oi.user_id = u.id
WHERE oi.provider = $1 AND oi.provider_uid = $2;
