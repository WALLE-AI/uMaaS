package domain

import (
	"context"
	"time"
)

// ── 工作空间成员 ────────────────────────────────────────────────

type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleDeveloper, RoleViewer:
		return true
	}
	return false
}

type UserStatus string

const (
	UserActive    UserStatus = "active"
	UserSuspended UserStatus = "suspended"
)

type User struct {
	ID        int64
	Email     string
	Name      string
	AvatarURL string
	Status    UserStatus
	CreatedAt time.Time
}

type Workspace struct {
	ID        int64
	Name      string
	Slug      string
	Plan      string
	OwnerID   int64
	CreatedAt time.Time
}

// Session 是浏览器会话。
//
// 明文令牌只在创建那一刻存在于内存里，之后系统任何地方都拿不到它——
// 这是"数据库泄露不等于会话被劫持"的实现前提。
type Session struct {
	ID        int64
	UserID    int64
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s *Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

// Principal 是已认证的调用方，贯穿控制平面的请求处理。
type Principal struct {
	User        User
	WorkspaceID int64
	Role        Role
	SessionID   int64
	ExpiresAt   time.Time
}

// ── 平台管理员（与上面无关）──────────────────────────────────────

type AdminRole string

const (
	AdminSuperAdmin AdminRole = "super_admin"
	AdminOperator   AdminRole = "operator"
	AdminViewer     AdminRole = "viewer"
)

func (r AdminRole) Valid() bool {
	switch r {
	case AdminSuperAdmin, AdminOperator, AdminViewer:
		return true
	}
	return false
}

// CanWrite 表达一条产品判断：viewer 只读。
// 放在领域层而不是每个 handler 里各写一遍——后者迟早会漏掉一个端点。
func (r AdminRole) CanWrite() bool {
	return r == AdminSuperAdmin || r == AdminOperator
}

// CanManageAdmins 只有 super_admin 能增删管理员。
func (r AdminRole) CanManageAdmins() bool { return r == AdminSuperAdmin }

type AdminAccount struct {
	ID           int64
	Email        string
	Name         string
	Role         AdminRole
	TOTPEnabled  bool
	LastActiveAt *time.Time
	DisabledAt   *time.Time
	CreatedAt    time.Time
}

func (a *AdminAccount) Disabled() bool { return a.DisabledAt != nil }

// AdminPrincipal 是已认证的平台管理员。
type AdminPrincipal struct {
	Account   AdminAccount
	SessionID int64
	ExpiresAt time.Time
}

// ── 审计 ────────────────────────────────────────────────────────

type ActorKind string

const (
	ActorAdmin  ActorKind = "admin"
	ActorUser   ActorKind = "user"
	ActorSystem ActorKind = "system"
)

// AuditEntry 是一条审计记录。
//
// Destructive 是独立字段而非从 Action 推断：审计页支持"仅看破坏性操作"
// 一键筛选，这个筛选要走索引（ARCHITECTURE.md §4.4）。
type AuditEntry struct {
	ActorKind   ActorKind
	ActorID     *int64
	ActorLabel  string
	Action      string
	Target      string
	Destructive bool
	Detail      map[string]any
	IP          string
	RequestID   string
}

// ── Repository 接口 ─────────────────────────────────────────────
//
// 业务代码只依赖这些接口，实现在 store/postgres。保留接口层不是为了换数据库，
// 而是为了单元测试能塞内存假实现（ARCHITECTURE.md §3）。

type IdentityRepository interface {
	CreateUserWithWorkspace(ctx context.Context, in NewUser) (*User, *Workspace, error)
	GetUserByEmail(ctx context.Context, email string) (*User, string, error) // 返回用户与密码哈希
	GetUserByID(ctx context.Context, id int64) (*User, error)
	PrimaryMembership(ctx context.Context, userID int64) (int64, Role, error)

	CreateSession(ctx context.Context, in NewSession) (*Session, error)
	LookupSession(ctx context.Context, tokenHash []byte) (*Principal, error)
	RevokeSession(ctx context.Context, tokenHash []byte) error
}

type NewUser struct {
	Email         string
	Name          string
	PasswordHash  string
	AvatarURL     string
	WorkspaceName string
	WorkspaceSlug string
}

type NewSession struct {
	TokenHash []byte
	UserID    int64
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

type AdminRepository interface {
	CreateAdmin(ctx context.Context, in NewAdmin) (*AdminAccount, error)
	GetAdminByEmail(ctx context.Context, email string) (*AdminAccount, string, []byte, error) // 账号、密码哈希、TOTP 密文
	CountAdmins(ctx context.Context) (int64, error)
	SetAdminTOTP(ctx context.Context, adminID int64, secretEnc []byte, enabled bool) error
	TouchAdminActivity(ctx context.Context, adminID int64) error

	CreateAdminSession(ctx context.Context, in NewSession) (int64, error)
	LookupAdminSession(ctx context.Context, tokenHash []byte) (*AdminPrincipal, error)
	RevokeAdminSession(ctx context.Context, tokenHash []byte) error
}

type NewAdmin struct {
	Email        string
	Name         string
	Role         AdminRole
	PasswordHash string
	TOTPSecret   []byte
	TOTPEnabled  bool
	CreatedBy    *int64
}

type AuditRepository interface {
	Append(ctx context.Context, e AuditEntry) error
}
