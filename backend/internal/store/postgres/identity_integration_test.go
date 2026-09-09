//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/testsupport"
)

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

func TestCreateUserWithWorkspace(t *testing.T) {
	db := testsupport.Postgres(t)
	repo := postgres.NewIdentityRepo(db)
	ctx := context.Background()

	email := uniqueEmail("ada")
	user, ws, err := repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: email, Name: "Ada", PasswordHash: "$argon2id$fake",
		WorkspaceName: "Ada's Workspace", WorkspaceSlug: uniqueSlug("ada"),
	})
	require.NoError(t, err)
	assert.NotZero(t, user.ID)
	assert.NotZero(t, ws.ID)

	wsID, role, err := repo.PrimaryMembership(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, ws.ID, wsID)
	assert.Equal(t, domain.RoleOwner, role, "the creator must be the workspace owner")
}

// 邮箱大小写不敏感。不做这一条，Alice@x.com 与 alice@x.com 会是两个账号，
// 而用户会认为自己"密码错了"。
func TestEmailUniquenessIsCaseInsensitive(t *testing.T) {
	db := testsupport.Postgres(t)
	repo := postgres.NewIdentityRepo(db)
	ctx := context.Background()

	email := uniqueEmail("case")
	_, _, err := repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: email, Name: "A", WorkspaceSlug: uniqueSlug("a"),
	})
	require.NoError(t, err)

	_, _, err = repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: upper(email), Name: "B", WorkspaceSlug: uniqueSlug("b"),
	})
	require.ErrorIs(t, err, domain.ErrConflict)
}

// 三张表必须一起写成功或一起失败：只建了用户而没有工作空间的账号，
// 登录后每个页面都会报"找不到工作空间"，且没有任何界面能修复它。
func TestCreateUserRollsBackOnConflict(t *testing.T) {
	db := testsupport.Postgres(t)
	repo := postgres.NewIdentityRepo(db)
	ctx := context.Background()

	slug := uniqueSlug("dup")
	email1, email2 := uniqueEmail("tx1"), uniqueEmail("tx2")

	_, _, err := repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: email1, Name: "A", WorkspaceSlug: slug,
	})
	require.NoError(t, err)

	// 第二次用同一个 slug：工作空间创建会失败，用户也必须不存在。
	_, _, err = repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: email2, Name: "B", WorkspaceSlug: slug,
	})
	require.ErrorIs(t, err, domain.ErrConflict)

	_, _, err = repo.GetUserByEmail(ctx, email2)
	assert.ErrorIs(t, err, domain.ErrNotFound, "the user row must have been rolled back")
}

func TestSessionLifecycle(t *testing.T) {
	db := testsupport.Postgres(t)
	repo := postgres.NewIdentityRepo(db)
	ctx := context.Background()

	user, _, err := repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: uniqueEmail("sess"), Name: "Ada", WorkspaceSlug: uniqueSlug("sess"),
	})
	require.NoError(t, err)

	token, hash, err := platform.NewSessionToken()
	require.NoError(t, err)
	_, err = repo.CreateSession(ctx, domain.NewSession{
		TokenHash: hash, UserID: user.ID,
		ExpiresAt: time.Now().Add(time.Hour), IP: "127.0.0.1", UserAgent: "test",
	})
	require.NoError(t, err)

	principal, err := repo.LookupSession(ctx, platform.HashSessionToken(token))
	require.NoError(t, err)
	assert.Equal(t, user.ID, principal.User.ID)

	require.NoError(t, repo.RevokeSession(ctx, hash))

	_, err = repo.LookupSession(ctx, hash)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
}

// 过期会话与被撤销的会话，对外都是同一个错误——
// 告诉攻击者"这个令牌曾经有效，只是过期了"没有任何好处。
func TestExpiredSessionIsRejected(t *testing.T) {
	db := testsupport.Postgres(t)
	repo := postgres.NewIdentityRepo(db)
	ctx := context.Background()

	user, _, err := repo.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email: uniqueEmail("exp"), Name: "Ada", WorkspaceSlug: uniqueSlug("exp"),
	})
	require.NoError(t, err)

	_, hash, err := platform.NewSessionToken()
	require.NoError(t, err)
	_, err = repo.CreateSession(ctx, domain.NewSession{
		TokenHash: hash, UserID: user.ID, ExpiresAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)

	_, err = repo.LookupSession(ctx, hash)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
}

func uniqueSlug(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}
