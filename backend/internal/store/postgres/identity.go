// Package postgres 用 sqlc 生成的查询实现 domain 的 Repository 接口。
//
// 这一层只做两件事：调用生成代码、把行结构翻译成领域模型。
// 业务判断不放这里——它属于 service 层。
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// uniqueViolation 是 Postgres 的唯一约束冲突码。
const uniqueViolation = "23505"

type IdentityRepo struct {
	db *store.DB
	q  *dbgen.Queries
}

func NewIdentityRepo(db *store.DB) *IdentityRepo {
	return &IdentityRepo{db: db, q: dbgen.New(db.Pool)}
}

var _ domain.IdentityRepository = (*IdentityRepo)(nil)

// CreateUserWithWorkspace 在一个事务里建用户、建工作空间、建 owner 成员关系。
//
// 三者必须原子：只建了用户而没有工作空间的账号，登录后会在每个页面上
// 报"找不到工作空间"，且没有任何界面能修复它。
func (r *IdentityRepo) CreateUserWithWorkspace(ctx context.Context, in domain.NewUser) (*domain.User, *domain.Workspace, error) {
	var user *domain.User
	var ws *domain.Workspace

	err := r.db.InTx(ctx, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		var pwHash *string
		if in.PasswordHash != "" {
			pwHash = &in.PasswordHash
		}
		var avatar *string
		if in.AvatarURL != "" {
			avatar = &in.AvatarURL
		}

		u, err := q.CreateUser(ctx, dbgen.CreateUserParams{
			Email:        in.Email,
			Name:         in.Name,
			PasswordHash: pwHash,
			AvatarUrl:    avatar,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return domain.Conflict("email_taken", "an account with this email already exists")
			}
			return fmt.Errorf("create user: %w", err)
		}

		w, err := q.CreateWorkspace(ctx, dbgen.CreateWorkspaceParams{
			Name: in.WorkspaceName,
			Slug: in.WorkspaceSlug,
			Plan: "free",
		})
		if err != nil {
			if isUniqueViolation(err) {
				return domain.Conflict("workspace_slug_taken", "workspace slug already exists")
			}
			return fmt.Errorf("create workspace: %w", err)
		}

		if err := q.CreateMembership(ctx, dbgen.CreateMembershipParams{
			UserID:      u.ID,
			WorkspaceID: w.ID,
			Role:        string(domain.RoleOwner),
		}); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}

		user = toDomainUser(u)
		ws = &domain.Workspace{
			ID: w.ID, Name: w.Name, Slug: w.Slug, Plan: w.Plan,
			OwnerID: u.ID, CreatedAt: w.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return user, ws, nil
}

func (r *IdentityRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, string, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", domain.ErrNotFound
		}
		return nil, "", fmt.Errorf("get user by email: %w", err)
	}
	var hash string
	if row.PasswordHash != nil {
		hash = *row.PasswordHash
	}
	return toDomainUser(row), hash, nil
}

func (r *IdentityRepo) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return toDomainUser(row), nil
}

// PrimaryMembership 返回用户的主工作空间。
//
// 首版：取最早创建的那个。多工作空间切换是后续迭代的事，
// 但接口先按"总是有一个当前工作空间"设计，避免上层到处判空。
func (r *IdentityRepo) PrimaryMembership(ctx context.Context, userID int64) (int64, domain.Role, error) {
	rows, err := r.q.ListMembershipsByUser(ctx, userID)
	if err != nil {
		return 0, "", fmt.Errorf("list memberships: %w", err)
	}
	if len(rows) == 0 {
		return 0, "", domain.NotFound("no_workspace", "user has no workspace membership")
	}
	return rows[0].WorkspaceID, domain.Role(rows[0].Role), nil
}

func (r *IdentityRepo) CreateSession(ctx context.Context, in domain.NewSession) (*domain.Session, error) {
	row, err := r.q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: in.TokenHash,
		UserID:    in.UserID,
		ExpiresAt: in.ExpiresAt,
		Ip:        nullableAddr(in.IP),
		UserAgent: nullableString(in.UserAgent),
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &domain.Session{
		ID: row.ID, UserID: row.UserID, ExpiresAt: row.ExpiresAt,
		RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt,
	}, nil
}

// LookupSession 校验会话并返回调用方身份。
//
// 过期与撤销分开判断，但**对外都是同一个错误**：告诉攻击者
// "这个令牌曾经有效，只是过期了"没有任何好处。
func (r *IdentityRepo) LookupSession(ctx context.Context, tokenHash []byte) (*domain.Principal, error) {
	row, err := r.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	if row.RevokedAt != nil || !time.Now().Before(row.ExpiresAt) {
		return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
	}
	if domain.UserStatus(row.Status) == domain.UserSuspended {
		return nil, domain.Forbidden("account_suspended", "this account has been suspended")
	}

	wsID, role, err := r.PrimaryMembership(ctx, row.UserID)
	if err != nil {
		return nil, err
	}

	var avatar string
	if row.AvatarUrl != nil {
		avatar = *row.AvatarUrl
	}
	return &domain.Principal{
		User: domain.User{
			ID: row.UserID, Email: row.Email, Name: row.Name,
			AvatarURL: avatar, Status: domain.UserStatus(row.Status),
		},
		WorkspaceID: wsID,
		Role:        role,
		SessionID:   row.ID,
		ExpiresAt:   row.ExpiresAt,
	}, nil
}

func (r *IdentityRepo) RevokeSession(ctx context.Context, tokenHash []byte) error {
	if err := r.q.RevokeSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// ── 辅助 ────────────────────────────────────────────────────────

func toDomainUser(u dbgen.User) *domain.User {
	var avatar string
	if u.AvatarUrl != nil {
		avatar = *u.AvatarUrl
	}
	return &domain.User{
		ID: u.ID, Email: u.Email, Name: u.Name,
		AvatarURL: avatar, Status: domain.UserStatus(u.Status), CreatedAt: u.CreatedAt,
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableAddr 把字符串 IP 转成 inet 列需要的类型。
//
// 解析失败时返回 nil 而不是报错：IP 只用于审计与排查，
// 一个畸形的 X-Real-IP 不该让登录失败。
func nullableAddr(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return &addr
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func marshalDetail(detail map[string]any) ([]byte, error) {
	if detail == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(detail)
}
