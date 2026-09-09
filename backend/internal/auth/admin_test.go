package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

type storedAdmin struct {
	account *domain.AdminAccount
	hash    string
	totpEnc []byte
}

type fakeAdminRepo struct {
	admins   map[string]*storedAdmin
	sessions map[string]int64
	nextID   int64
}

func newFakeAdmin() *fakeAdminRepo {
	return &fakeAdminRepo{
		admins:   map[string]*storedAdmin{},
		sessions: map[string]int64{},
		nextID:   1,
	}
}

func (f *fakeAdminRepo) CreateAdmin(_ context.Context, in domain.NewAdmin) (*domain.AdminAccount, error) {
	if _, exists := f.admins[in.Email]; exists {
		return nil, domain.Conflict("admin_email_taken", "exists")
	}
	a := &domain.AdminAccount{
		ID: f.nextID, Email: in.Email, Name: in.Name,
		Role: in.Role, TOTPEnabled: in.TOTPEnabled,
	}
	f.nextID++
	f.admins[in.Email] = &storedAdmin{account: a, hash: in.PasswordHash, totpEnc: in.TOTPSecret}
	return a, nil
}

func (f *fakeAdminRepo) GetAdminByEmail(_ context.Context, email string) (*domain.AdminAccount, string, []byte, error) {
	s, ok := f.admins[email]
	if !ok {
		return nil, "", nil, domain.ErrNotFound
	}
	return s.account, s.hash, s.totpEnc, nil
}

func (f *fakeAdminRepo) CountAdmins(context.Context) (int64, error) {
	return int64(len(f.admins)), nil
}

func (f *fakeAdminRepo) SetAdminTOTP(_ context.Context, id int64, enc []byte, enabled bool) error {
	for _, s := range f.admins {
		if s.account.ID == id {
			s.totpEnc = enc
			s.account.TOTPEnabled = enabled
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeAdminRepo) TouchAdminActivity(context.Context, int64) error { return nil }

func (f *fakeAdminRepo) CreateAdminSession(_ context.Context, in domain.NewSession) (int64, error) {
	f.sessions[string(in.TokenHash)] = in.UserID
	return 1, nil
}

func (f *fakeAdminRepo) LookupAdminSession(_ context.Context, hash []byte) (*domain.AdminPrincipal, error) {
	id, ok := f.sessions[string(hash)]
	if !ok {
		return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
	}
	for _, s := range f.admins {
		if s.account.ID == id {
			if s.account.Disabled() {
				return nil, domain.Forbidden("admin_disabled", "disabled")
			}
			return &domain.AdminPrincipal{Account: *s.account, SessionID: 1,
				ExpiresAt: time.Now().Add(AdminSessionTTL)}, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAdminRepo) RevokeAdminSession(_ context.Context, hash []byte) error {
	delete(f.sessions, string(hash))
	return nil
}

// ── 测试 ────────────────────────────────────────────────────────

func enrollAdmin(t *testing.T, svc *Service, role domain.AdminRole) (*AdminEnrollment, string) {
	t.Helper()
	const password = "admin-password-1"
	enrollment, err := svc.CreateAdmin(context.Background(), CreateAdminInput{
		Email: "root@example.com", Name: "Root", Role: role, Password: password,
	})
	require.NoError(t, err)
	return enrollment, password
}

// 新建的管理员**必须先绑定 2FA 才能登录**。
// 这条不给例外，否则"临时关掉"会变成永久。
func TestAdminCannotLoginBeforeTOTPEnrollment(t *testing.T) {
	svc, _, _, _ := newService(t)
	enrollment, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)

	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)

	_, err = svc.AdminLogin(context.Background(), AdminLoginInput{
		Email: "root@example.com", Password: password, TOTPCode: code,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, "totp_required", de.Code)
}

func TestAdminLoginAfterEnrollment(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	enrollment, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)

	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.ConfirmAdminTOTP(ctx, "root@example.com", password, code))

	res, err := svc.AdminLogin(ctx, AdminLoginInput{
		Email: "root@example.com", Password: password, TOTPCode: code,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
	assert.Equal(t, domain.AdminSuperAdmin, res.Principal.Account.Role)
	// admin 会话比用户会话短得多。
	assert.True(t, res.ExpiresAt.Before(time.Now().Add(SessionTTL)))
}

// 密码对、验证码错，与密码错，必须返回同一个错误——
// 分步返回"密码对了，请输入验证码"会把密码正确性变成可探测信号。
func TestAdminLoginHidesWhichFactorFailed(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	enrollment, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)
	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.ConfirmAdminTOTP(ctx, "root@example.com", password, code))

	_, errBadPassword := svc.AdminLogin(ctx, AdminLoginInput{
		Email: "root@example.com", Password: "wrong-password", TOTPCode: code,
	})
	_, errBadCode := svc.AdminLogin(ctx, AdminLoginInput{
		Email: "root@example.com", Password: password, TOTPCode: "000000",
	})

	require.Error(t, errBadPassword)
	require.Error(t, errBadCode)
	assert.Equal(t, errBadPassword.Error(), errBadCode.Error())
}

// 登录失败要留审计——这是发现暴力破解的唯一线索。
func TestAdminLoginFailureIsAudited(t *testing.T) {
	svc, _, _, audit := newService(t)
	ctx := context.Background()
	enrollment, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)
	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.ConfirmAdminTOTP(ctx, "root@example.com", password, code))

	_, _ = svc.AdminLogin(ctx, AdminLoginInput{
		Email: "root@example.com", Password: "wrong-password", TOTPCode: code,
	})

	var found bool
	for _, e := range audit.entries {
		if e.Action == "admin.login_failed" {
			found = true
			assert.Equal(t, "bad_password", e.Detail["reason"])
		}
	}
	assert.True(t, found, "failed admin logins must be audited")
}

// 创建管理员必须标 destructive：它直接扩大了平台权限的持有者集合。
func TestCreateAdminIsAuditedAsDestructive(t *testing.T) {
	svc, _, _, audit := newService(t)
	enrollAdmin(t, svc, domain.AdminSuperAdmin)

	require.NotEmpty(t, audit.entries)
	var found bool
	for _, e := range audit.entries {
		if e.Action == "admin.create" {
			found = true
			assert.True(t, e.Destructive, "admin.create must be flagged destructive")
		}
	}
	assert.True(t, found)
}

// TOTP 密钥必须加密入库，绝不明文。
func TestTOTPSecretIsEncryptedAtRest(t *testing.T) {
	svc, _, admins, _ := newService(t)
	enrollment, _ := enrollAdmin(t, svc, domain.AdminSuperAdmin)

	stored := admins.admins["root@example.com"].totpEnc
	require.NotEmpty(t, stored)
	assert.NotContains(t, string(stored), enrollment.Secret)
}

// 绑定必须真的校验一次验证码再启用：只让用户"扫码点确认"的流程里，
// 扫错或时钟不同步会等到下次登录才暴露，而那时账号已经锁死。
func TestConfirmTOTPRejectsWrongCode(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)

	err := svc.ConfirmAdminTOTP(context.Background(), "root@example.com", password, "000000")
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestConfirmTOTPRequiresCorrectPassword(t *testing.T) {
	svc, _, _, _ := newService(t)
	enrollment, _ := enrollAdmin(t, svc, domain.AdminSuperAdmin)
	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)

	err = svc.ConfirmAdminTOTP(context.Background(), "root@example.com", "wrong-password", code)
	require.ErrorIs(t, err, domain.ErrUnauthorized)
}

func TestDisabledAdminCannotUseExistingSession(t *testing.T) {
	svc, _, admins, _ := newService(t)
	ctx := context.Background()
	enrollment, password := enrollAdmin(t, svc, domain.AdminSuperAdmin)
	code, err := platform.TOTPCode(enrollment.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.ConfirmAdminTOTP(ctx, "root@example.com", password, code))

	res, err := svc.AdminLogin(ctx, AdminLoginInput{
		Email: "root@example.com", Password: password, TOTPCode: code,
	})
	require.NoError(t, err)

	// 停用后，已签发的会话必须立即失效——否则"停用"要等会话过期才生效，
	// 而那正是最需要它立刻生效的场合。
	now := time.Now()
	admins.admins["root@example.com"].account.DisabledAt = &now

	_, err = svc.AdminSession(ctx, res.Token)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestAdminRolePermissions(t *testing.T) {
	assert.True(t, domain.AdminSuperAdmin.CanWrite())
	assert.True(t, domain.AdminOperator.CanWrite())
	assert.False(t, domain.AdminViewer.CanWrite(), "viewer must be read-only")

	assert.True(t, domain.AdminSuperAdmin.CanManageAdmins())
	assert.False(t, domain.AdminOperator.CanManageAdmins(),
		"only super_admin may create or disable admins")
}

func TestCreateAdminRejectsInvalidRole(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, err := svc.CreateAdmin(context.Background(), CreateAdminInput{
		Email: "x@example.com", Name: "X", Role: "root", Password: "admin-password-1",
	})
	require.ErrorIs(t, err, domain.ErrValidation)
}
