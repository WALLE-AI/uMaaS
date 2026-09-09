//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/testsupport"
)

// 端到端跑一遍管理员的完整生命周期：创建 → 绑定 2FA → 登录 → 会话 → 登出。
// 这条路径横跨 service、repo 与真实数据库，任何一层的类型映射出错都会在这里暴露。
func TestAdminEndToEnd(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()

	cipher, err := platform.NewCipher(make([]byte, 32))
	require.NoError(t, err)
	svc := auth.NewService(
		postgres.NewIdentityRepo(db),
		postgres.NewAdminRepo(db),
		postgres.NewAuditRepo(db),
		cipher,
	)

	email := uniqueEmail("root")
	const password = "admin-password-1"

	enrollment, err := svc.CreateAdmin(ctx, auth.CreateAdminInput{
		Email: email, Name: "Root", Role: domain.AdminSuperAdmin, Password: password,
	})
	require.NoError(t, err)
	assert.False(t, enrollment.Account.TOTPEnabled, "2FA must start disabled until confirmed")

	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)

	// 绑定之前不能登录——全员强制 2FA，不给例外。
	_, err = svc.AdminLogin(ctx, auth.AdminLoginInput{
		Email: email, Password: password, TOTPCode: code,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	require.NoError(t, svc.ConfirmAdminTOTP(ctx, email, password, code))

	res, err := svc.AdminLogin(ctx, auth.AdminLoginInput{
		Email: email, Password: password, TOTPCode: code, IP: "10.0.0.1",
	})
	require.NoError(t, err)
	require.NotEmpty(t, res.Token)

	principal, err := svc.AdminSession(ctx, res.Token)
	require.NoError(t, err)
	assert.Equal(t, domain.AdminSuperAdmin, principal.Account.Role)

	require.NoError(t, svc.AdminLogout(ctx, res.Token))
	_, err = svc.AdminSession(ctx, res.Token)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
}

// TOTP 密钥落库必须是密文。这条在真实数据库上验证一次——
// 内存假实现可能掩盖掉 []byte 列的映射问题。
func TestTOTPSecretStoredEncrypted(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()

	cipher, err := platform.NewCipher(make([]byte, 32))
	require.NoError(t, err)
	repo := postgres.NewAdminRepo(db)
	svc := auth.NewService(nil, repo, postgres.NewAuditRepo(db), cipher)

	email := uniqueEmail("enc")
	enrollment, err := svc.CreateAdmin(ctx, auth.CreateAdminInput{
		Email: email, Name: "Enc", Role: domain.AdminOperator, Password: "admin-password-1",
	})
	require.NoError(t, err)

	var raw []byte
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT totp_secret_enc FROM admin_accounts WHERE id = $1`,
		enrollment.Account.ID).Scan(&raw))

	assert.NotEmpty(t, raw)
	assert.NotContains(t, string(raw), enrollment.Secret,
		"the TOTP secret must never hit disk in plaintext")

	plain, err := cipher.Decrypt(raw)
	require.NoError(t, err)
	assert.Equal(t, enrollment.Secret, string(plain))
}

// 审计写入要能落到带索引的 destructive 列上，且 detail 是合法 jsonb。
func TestAuditLogWritesDestructiveFlag(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	repo := postgres.NewAuditRepo(db)

	require.NoError(t, repo.Append(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorLabel: "root@example.com",
		Action: "pricing.update", Target: "openai/gpt-5.6-sol",
		Destructive: true,
		Detail:      map[string]any{"from": 1.25, "to": 1.5},
		IP:          "10.0.0.1", RequestID: "req_test",
	}))

	var n int
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE destructive AND action = 'pricing.update'`).Scan(&n))
	assert.GreaterOrEqual(t, n, 1)

	// detail 必须是可查询的 jsonb，不是字符串——否则审计页无法按字段筛选。
	var from float64
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT (detail->>'from')::float FROM audit_logs
		 WHERE action = 'pricing.update' ORDER BY id DESC LIMIT 1`).Scan(&from))
	assert.Equal(t, 1.25, from)
}

// 停用的管理员，已签发的会话必须立即失效。
func TestDisabledAdminSessionRejected(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()

	cipher, err := platform.NewCipher(make([]byte, 32))
	require.NoError(t, err)
	repo := postgres.NewAdminRepo(db)
	svc := auth.NewService(nil, repo, postgres.NewAuditRepo(db), cipher)

	email := uniqueEmail("disabled")
	const password = "admin-password-1"
	enrollment, err := svc.CreateAdmin(ctx, auth.CreateAdminInput{
		Email: email, Name: "Dis", Role: domain.AdminOperator, Password: password,
	})
	require.NoError(t, err)
	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.ConfirmAdminTOTP(ctx, email, password, code))

	res, err := svc.AdminLogin(ctx, auth.AdminLoginInput{
		Email: email, Password: password, TOTPCode: code,
	})
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx,
		`UPDATE admin_accounts SET disabled_at = now() WHERE id = $1`, enrollment.Account.ID)
	require.NoError(t, err)

	_, err = svc.AdminSession(ctx, res.Token)
	assert.ErrorIs(t, err, domain.ErrForbidden)
}
