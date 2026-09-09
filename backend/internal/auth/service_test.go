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

// 单元测试塞内存假实现，不碰数据库——这正是 domain 保留 Repository 接口的理由
// （ARCHITECTURE.md §3），不是为了"将来换数据库"。

type fakeIdentityRepo struct {
	users    map[string]*storedUser
	sessions map[string]*domain.NewSession
	nextID   int64
}

type storedUser struct {
	user *domain.User
	hash string
}

func newFakeIdentity() *fakeIdentityRepo {
	return &fakeIdentityRepo{
		users:    map[string]*storedUser{},
		sessions: map[string]*domain.NewSession{},
		nextID:   1,
	}
}

func (f *fakeIdentityRepo) CreateUserWithWorkspace(_ context.Context, in domain.NewUser) (*domain.User, *domain.Workspace, error) {
	if _, exists := f.users[in.Email]; exists {
		return nil, nil, domain.Conflict("email_taken", "already exists")
	}
	u := &domain.User{ID: f.nextID, Email: in.Email, Name: in.Name, Status: domain.UserActive}
	f.nextID++
	f.users[in.Email] = &storedUser{user: u, hash: in.PasswordHash}
	return u, &domain.Workspace{ID: 100, Slug: in.WorkspaceSlug}, nil
}

func (f *fakeIdentityRepo) GetUserByEmail(_ context.Context, email string) (*domain.User, string, error) {
	s, ok := f.users[email]
	if !ok {
		return nil, "", domain.ErrNotFound
	}
	return s.user, s.hash, nil
}

func (f *fakeIdentityRepo) GetUserByID(context.Context, int64) (*domain.User, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeIdentityRepo) PrimaryMembership(context.Context, int64) (int64, domain.Role, error) {
	return 100, domain.RoleOwner, nil
}

func (f *fakeIdentityRepo) CreateSession(_ context.Context, in domain.NewSession) (*domain.Session, error) {
	f.sessions[string(in.TokenHash)] = &in
	return &domain.Session{ID: 1, UserID: in.UserID, ExpiresAt: in.ExpiresAt}, nil
}

func (f *fakeIdentityRepo) LookupSession(_ context.Context, hash []byte) (*domain.Principal, error) {
	s, ok := f.sessions[string(hash)]
	if !ok {
		return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
	}
	if time.Now().After(s.ExpiresAt) {
		return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
	}
	for _, u := range f.users {
		if u.user.ID == s.UserID {
			return &domain.Principal{
				User: *u.user, WorkspaceID: 100, Role: domain.RoleOwner,
				SessionID: 1, ExpiresAt: s.ExpiresAt,
			}, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeIdentityRepo) RevokeSession(_ context.Context, hash []byte) error {
	delete(f.sessions, string(hash))
	return nil
}

type fakeAuditRepo struct{ entries []domain.AuditEntry }

func (f *fakeAuditRepo) Append(_ context.Context, e domain.AuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}

func newService(t *testing.T) (*Service, *fakeIdentityRepo, *fakeAdminRepo, *fakeAuditRepo) {
	t.Helper()
	ids := newFakeIdentity()
	admins := newFakeAdmin()
	audit := &fakeAuditRepo{}
	cipher, err := platform.NewCipher(make([]byte, 32))
	require.NoError(t, err)
	return NewService(ids, admins, audit, cipher), ids, admins, audit
}

// ── 注册 ────────────────────────────────────────────────────────

func TestRegisterCreatesSession(t *testing.T) {
	svc, _, _, _ := newService(t)

	res, err := svc.Register(context.Background(), RegisterInput{
		Name: "Ada", Email: "Ada@Example.com", Password: "supersecret",
		AcceptedTerms: true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
	assert.Equal(t, "ada@example.com", res.Principal.User.Email, "email must be normalised")
}

// 契约把 accepted_terms 标为 const: true。只在前端拦等于没拦。
func TestRegisterRequiresAcceptedTerms(t *testing.T) {
	svc, _, _, _ := newService(t)

	_, err := svc.Register(context.Background(), RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret",
		AcceptedTerms: false,
	})
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestRegisterValidation(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		input RegisterInput
		field string
	}{
		{"bad email", RegisterInput{Name: "A", Email: "nope", Password: "supersecret", AcceptedTerms: true}, "email"},
		{"short password", RegisterInput{Name: "A", Email: "a@b.co", Password: "short", AcceptedTerms: true}, "password"},
		{"empty name", RegisterInput{Name: "  ", Email: "a@b.co", Password: "supersecret", AcceptedTerms: true}, "name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Register(ctx, tc.input)
			require.ErrorIs(t, err, domain.ErrValidation)
			var de *domain.Error
			require.ErrorAs(t, err, &de)
			assert.Equal(t, tc.field, de.Field)
		})
	}
}

// 中文名 100 字应当通过：按 rune 计数而不是字节数。
func TestRegisterAcceptsMultibyteNames(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, err := svc.Register(context.Background(), RegisterInput{
		Name: "张三", Email: "zhang@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)
}

// ── 登录 ────────────────────────────────────────────────────────

func TestLoginSuccess(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)

	res, err := svc.Login(ctx, LoginInput{Email: "ada@example.com", Password: "supersecret"})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
}

// 账号不存在与密码错误必须**返回同一个错误**，
// 否则登录接口就是一个免费的账号枚举器。
func TestLoginDoesNotRevealAccountExistence(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)

	_, errUnknown := svc.Login(ctx, LoginInput{Email: "nobody@example.com", Password: "supersecret"})
	_, errWrongPw := svc.Login(ctx, LoginInput{Email: "ada@example.com", Password: "wrongpassword"})

	require.Error(t, errUnknown)
	require.Error(t, errWrongPw)
	assert.Equal(t, errUnknown.Error(), errWrongPw.Error(),
		"unknown account and wrong password must be indistinguishable")
}

func TestLoginRememberExtendsSession(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)

	short, err := svc.Login(ctx, LoginInput{Email: "ada@example.com", Password: "supersecret"})
	require.NoError(t, err)
	long, err := svc.Login(ctx, LoginInput{Email: "ada@example.com", Password: "supersecret", Remember: true})
	require.NoError(t, err)

	assert.True(t, long.ExpiresAt.After(short.ExpiresAt))
}

func TestLoginRejectsSuspendedAccount(t *testing.T) {
	svc, ids, _, _ := newService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)
	ids.users["ada@example.com"].user.Status = domain.UserSuspended

	_, err = svc.Login(ctx, LoginInput{Email: "ada@example.com", Password: "supersecret"})
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestSessionAndLogout(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "supersecret", AcceptedTerms: true,
	})
	require.NoError(t, err)

	principal, err := svc.Session(ctx, res.Token)
	require.NoError(t, err)
	assert.Equal(t, "ada@example.com", principal.User.Email)

	require.NoError(t, svc.Logout(ctx, res.Token))

	_, err = svc.Session(ctx, res.Token)
	require.ErrorIs(t, err, domain.ErrUnauthorized)
}

func TestSessionWithoutTokenIsUnauthorized(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, err := svc.Session(context.Background(), "")
	require.ErrorIs(t, err, domain.ErrUnauthorized)
}

// 登出必须幂等：没有会话时重复调用不应报错，否则前端的"登出"按钮
// 在会话已过期时会弹一个没人能理解的错误。
func TestLogoutIsIdempotent(t *testing.T) {
	svc, _, _, _ := newService(t)
	require.NoError(t, svc.Logout(context.Background(), ""))
}
