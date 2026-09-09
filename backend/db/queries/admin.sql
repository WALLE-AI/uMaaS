-- 平台管理员。与 users 完全独立的一套（ARCHITECTURE.md §4.5）。

-- name: CreateAdminAccount :one
INSERT INTO admin_accounts (email, name, role, password_hash, totp_secret_enc, totp_enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAdminByEmail :one
SELECT * FROM admin_accounts WHERE lower(email) = lower($1);

-- name: GetAdminByID :one
SELECT * FROM admin_accounts WHERE id = $1;

-- name: CountAdmins :one
SELECT count(*) FROM admin_accounts;

-- name: ListAdmins :many
SELECT * FROM admin_accounts ORDER BY created_at;

-- name: SetAdminTOTP :exec
UPDATE admin_accounts SET totp_secret_enc = $2, totp_enabled = $3 WHERE id = $1;

-- name: TouchAdminActivity :exec
UPDATE admin_accounts SET last_active_at = now() WHERE id = $1;

-- name: DisableAdmin :exec
-- 停用而非删除：审计日志里的 actor 要能一直解析出人名。
UPDATE admin_accounts SET disabled_at = now() WHERE id = $1 AND disabled_at IS NULL;

-- name: CreateAdminSession :one
INSERT INTO admin_sessions (token_hash, admin_id, expires_at, ip, user_agent)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAdminSessionByTokenHash :one
SELECT s.id, s.token_hash, s.admin_id, s.expires_at, s.revoked_at, s.created_at,
       a.email, a.name, a.role, a.totp_enabled, a.disabled_at
FROM admin_sessions s
JOIN admin_accounts a ON a.id = s.admin_id
WHERE s.token_hash = $1;

-- name: RevokeAdminSession :exec
UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllAdminSessions :exec
UPDATE admin_sessions SET revoked_at = now() WHERE admin_id = $1 AND revoked_at IS NULL;

-- name: InsertAuditLog :one
INSERT INTO audit_logs (actor_kind, actor_id, actor_label, action, target, destructive, detail, ip, request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (NOT sqlc.arg(destructive_only)::boolean OR destructive)
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);
