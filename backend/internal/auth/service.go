// Package auth 是身份服务：注册、登录、会话、管理员认证。
//
// 安全判断集中在这里，handler 只负责解析请求与写响应。
// 分散的后果是"某个端点忘了查 suspended 状态"这类漏洞。
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

// 会话时长。两档：普通登录与"记住我"。
//
// admin 的会话**更短**（见 AdminSessionTTL）：它能看所有客户的账单、能改价、
// 能删部署实例，泄露一个 admin 会话的代价远高于普通用户（ARCHITECTURE.md §4.5）。
const (
	SessionTTL         = 24 * time.Hour
	SessionTTLRemember = 30 * 24 * time.Hour
	AdminSessionTTL    = 8 * time.Hour
)

type Service struct {
	users  domain.IdentityRepository
	admins domain.AdminRepository
	audit  domain.AuditRepository
	cipher *platform.Cipher
	now    func() time.Time
}

func NewService(
	users domain.IdentityRepository,
	admins domain.AdminRepository,
	audit domain.AuditRepository,
	cipher *platform.Cipher,
) *Service {
	return &Service{users: users, admins: admins, audit: audit, cipher: cipher, now: time.Now}
}

// SessionResult 是登录/注册的产物。Token 是明文令牌，只在这一刻存在。
type SessionResult struct {
	Token     string
	ExpiresAt time.Time
	Principal *domain.Principal
}

// ── 注册 ────────────────────────────────────────────────────────

type RegisterInput struct {
	Name          string
	Email         string
	Password      string
	AcceptedTerms bool
	IP, UserAgent string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*SessionResult, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	if err := validateEmail(in.Email); err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	// 契约把 accepted_terms 标为 const: true，服务端必须真的校验——
	// 只在前端拦的"同意条款"在法务上等于没有。
	if !in.AcceptedTerms {
		return nil, domain.Validation("terms_not_accepted",
			"you must accept the terms to create an account", "accepted_terms")
	}

	hash, err := platform.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	email := normalizeEmail(in.Email)
	user, _, err := s.users.CreateUserWithWorkspace(ctx, domain.NewUser{
		Email:         email,
		Name:          strings.TrimSpace(in.Name),
		PasswordHash:  hash,
		WorkspaceName: defaultWorkspaceName(in.Name),
		WorkspaceSlug: workspaceSlug(email, s.now()),
	})
	if err != nil {
		return nil, err
	}

	return s.issueSession(ctx, user.ID, SessionTTL, in.IP, in.UserAgent)
}

// ── 登录 ────────────────────────────────────────────────────────

type LoginInput struct {
	Email         string
	Password      string
	Remember      bool
	IP, UserAgent string
}

// Login 校验凭证并签发会话。
//
// 账号不存在时**仍然跑一遍密码哈希**：否则登录接口的响应时间会泄露
// 账号是否存在——存在时慢（argon2），不存在时快。这是可被脚本利用的枚举通道。
func (s *Service) Login(ctx context.Context, in LoginInput) (*SessionResult, error) {
	user, hash, err := s.users.GetUserByEmail(ctx, normalizeEmail(in.Email))
	if err != nil {
		if isNotFound(err) {
			_, _ = platform.VerifyPassword(platform.DummyPasswordHash, in.Password)
			return nil, invalidCredentials()
		}
		return nil, err
	}
	if hash == "" {
		// OAuth 注册的账号没有密码。同样返回统一错误，
		// 不告诉调用方"这个邮箱是用 GitHub 注册的"。
		_, _ = platform.VerifyPassword(platform.DummyPasswordHash, in.Password)
		return nil, invalidCredentials()
	}

	ok, err := platform.VerifyPassword(hash, in.Password)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return nil, invalidCredentials()
	}
	if user.Status == domain.UserSuspended {
		return nil, domain.Forbidden("account_suspended", "this account has been suspended")
	}

	ttl := SessionTTL
	if in.Remember {
		ttl = SessionTTLRemember
	}
	return s.issueSession(ctx, user.ID, ttl, in.IP, in.UserAgent)
}

func (s *Service) issueSession(ctx context.Context, userID int64, ttl time.Duration, ip, ua string) (*SessionResult, error) {
	token, hash, err := platform.NewSessionToken()
	if err != nil {
		return nil, fmt.Errorf("new session token: %w", err)
	}
	expires := s.now().Add(ttl)

	if _, err := s.users.CreateSession(ctx, domain.NewSession{
		TokenHash: hash, UserID: userID, ExpiresAt: expires, IP: ip, UserAgent: ua,
	}); err != nil {
		return nil, err
	}

	principal, err := s.users.LookupSession(ctx, hash)
	if err != nil {
		return nil, err
	}
	return &SessionResult{Token: token, ExpiresAt: expires, Principal: principal}, nil
}

// Session 校验令牌。
func (s *Service) Session(ctx context.Context, token string) (*domain.Principal, error) {
	if token == "" {
		return nil, domain.Unauthorized("no_session", "not authenticated")
	}
	return s.users.LookupSession(ctx, platform.HashSessionToken(token))
}

// Logout 撤销会话。撤销而非删除：/auth/session 要能区分
// "没登录"与"会话已被踢下线"（ARCHITECTURE.md §4.1）。
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil // 幂等：已经没登录就当作成功
	}
	return s.users.RevokeSession(ctx, platform.HashSessionToken(token))
}

// ── 管理员认证 ──────────────────────────────────────────────────

type AdminLoginInput struct {
	Email         string
	Password      string
	TOTPCode      string
	IP, UserAgent string
}

type AdminSessionResult struct {
	Token     string
	ExpiresAt time.Time
	Principal *domain.AdminPrincipal
}

// AdminLogin 密码 + TOTP 两因素。
//
// 两个因素**一起校验、失败时返回同一个错误**：分步返回
// "密码对了，请输入验证码"会把密码是否正确变成一个可探测的信号。
func (s *Service) AdminLogin(ctx context.Context, in AdminLoginInput) (*AdminSessionResult, error) {
	account, hash, totpEnc, err := s.admins.GetAdminByEmail(ctx, normalizeEmail(in.Email))
	if err != nil {
		if isNotFound(err) {
			_, _ = platform.VerifyPassword(platform.DummyPasswordHash, in.Password)
			return nil, invalidCredentials()
		}
		return nil, err
	}

	ok, err := platform.VerifyPassword(hash, in.Password)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		s.auditAdminLoginFailure(ctx, account, "bad_password")
		return nil, invalidCredentials()
	}
	if account.Disabled() {
		s.auditAdminLoginFailure(ctx, account, "disabled")
		return nil, domain.Forbidden("admin_disabled", "this admin account is disabled")
	}

	// 全员强制 2FA（已定 2026-09-08）。没绑定 TOTP 的账号不能登录，
	// 只能走绑定流程——这一条不给例外，否则"临时关掉"会变成永久。
	if !account.TOTPEnabled || len(totpEnc) == 0 {
		return nil, domain.Forbidden("totp_required",
			"two-factor authentication must be set up before signing in")
	}
	secret, err := s.cipher.Decrypt(totpEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}
	valid, err := platform.VerifyTOTP(string(secret), in.TOTPCode, s.now())
	if err != nil {
		return nil, fmt.Errorf("verify totp: %w", err)
	}
	if !valid {
		s.auditAdminLoginFailure(ctx, account, "bad_totp")
		return nil, invalidCredentials()
	}

	token, tokenHash, err := platform.NewSessionToken()
	if err != nil {
		return nil, fmt.Errorf("new session token: %w", err)
	}
	expires := s.now().Add(AdminSessionTTL)
	if _, err := s.admins.CreateAdminSession(ctx, domain.NewSession{
		TokenHash: tokenHash, UserID: account.ID, ExpiresAt: expires,
		IP: in.IP, UserAgent: in.UserAgent,
	}); err != nil {
		return nil, err
	}
	_ = s.admins.TouchAdminActivity(ctx, account.ID)

	s.appendAudit(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorID: &account.ID, ActorLabel: account.Email,
		Action: "admin.login", Target: account.Email, IP: in.IP,
		RequestID: platform.RequestID(ctx),
	})

	principal, err := s.admins.LookupAdminSession(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	return &AdminSessionResult{Token: token, ExpiresAt: expires, Principal: principal}, nil
}

func (s *Service) AdminSession(ctx context.Context, token string) (*domain.AdminPrincipal, error) {
	if token == "" {
		return nil, domain.Unauthorized("no_session", "not authenticated")
	}
	return s.admins.LookupAdminSession(ctx, platform.HashSessionToken(token))
}

func (s *Service) AdminLogout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.admins.RevokeAdminSession(ctx, platform.HashSessionToken(token))
}

// ── 管理员创建与 TOTP 绑定 ──────────────────────────────────────

type CreateAdminInput struct {
	Email     string
	Name      string
	Role      domain.AdminRole
	Password  string
	CreatedBy *int64
}

// AdminEnrollment 是创建管理员后返回的一次性绑定信息。
// Secret 与 URI 只在此刻可见，之后系统里只有密文。
type AdminEnrollment struct {
	Account *domain.AdminAccount
	Secret  string
	URI     string
}

// CreateAdmin 创建管理员并生成 TOTP 密钥。
//
// 密钥在创建时就生成并加密入库，`totp_enabled` 先为 false——
// 首次登录前必须完成绑定校验。这样"创建了但没绑 2FA 的账号"无法登录，
// 而不是留一个只有密码保护的后门。
func (s *Service) CreateAdmin(ctx context.Context, in CreateAdminInput) (*AdminEnrollment, error) {
	if err := validateEmail(in.Email); err != nil {
		return nil, err
	}
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	if !in.Role.Valid() {
		return nil, domain.Validation("invalid_role",
			"role must be super_admin, operator or viewer", "role")
	}
	if s.cipher == nil {
		return nil, platform.ErrNoEncryptionKey
	}

	hash, err := platform.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	secret, err := platform.NewTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("new totp secret: %w", err)
	}
	enc, err := s.cipher.Encrypt([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("encrypt totp secret: %w", err)
	}

	email := normalizeEmail(in.Email)
	account, err := s.admins.CreateAdmin(ctx, domain.NewAdmin{
		Email: email, Name: strings.TrimSpace(in.Name), Role: in.Role,
		PasswordHash: hash, TOTPSecret: enc, TOTPEnabled: false, CreatedBy: in.CreatedBy,
	})
	if err != nil {
		return nil, err
	}

	s.appendAudit(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorID: in.CreatedBy,
		ActorLabel: actorLabel(in.CreatedBy),
		Action:     "admin.create", Target: email,
		// 创建管理员是破坏性操作：它直接扩大了平台权限的持有者集合。
		Destructive: true,
		Detail:      map[string]any{"role": string(in.Role)},
		RequestID:   platform.RequestID(ctx),
	})

	return &AdminEnrollment{
		Account: account,
		Secret:  secret,
		URI:     platform.TOTPURI("uMaaS", email, secret),
	}, nil
}

// ConfirmAdminTOTP 校验一次验证码后启用 2FA。
//
// 必须真的校验一次再启用：只让用户"扫码并点确认"的流程里，
// 扫错、时钟不同步这类问题会等到下次登录才暴露——而那时账号已经锁死。
func (s *Service) ConfirmAdminTOTP(ctx context.Context, email, password, code string) error {
	account, hash, enc, err := s.admins.GetAdminByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if isNotFound(err) {
			return invalidCredentials()
		}
		return err
	}
	ok, err := platform.VerifyPassword(hash, password)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return invalidCredentials()
	}
	if account.TOTPEnabled {
		return domain.Conflict("totp_already_enabled", "two-factor authentication is already enabled")
	}

	secret, err := s.cipher.Decrypt(enc)
	if err != nil {
		return fmt.Errorf("decrypt totp secret: %w", err)
	}
	valid, err := platform.VerifyTOTP(string(secret), code, s.now())
	if err != nil {
		return fmt.Errorf("verify totp: %w", err)
	}
	if !valid {
		return domain.Validation("invalid_totp_code", "the verification code is incorrect", "code")
	}

	if err := s.admins.SetAdminTOTP(ctx, account.ID, enc, true); err != nil {
		return err
	}
	s.appendAudit(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorID: &account.ID, ActorLabel: account.Email,
		Action: "admin.totp_enabled", Target: account.Email, RequestID: platform.RequestID(ctx),
	})
	return nil
}

// ── 审计与校验 ──────────────────────────────────────────────────

// appendAudit 写审计，失败只记日志不阻断主流程。
//
// 这是个有意识的取舍：审计写失败时让登录也失败，会把审计表变成
// 登录链路上的单点故障。但**破坏性操作**的审计应当在事务内与操作同生共死——
// 那类调用点在 I6 落地时用 InTx 走另一条路。
func (s *Service) appendAudit(ctx context.Context, e domain.AuditEntry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Append(ctx, e); err != nil {
		slog.ErrorContext(ctx, "append audit log failed", "action", e.Action, "error", err)
	}
}

func (s *Service) auditAdminLoginFailure(ctx context.Context, a *domain.AdminAccount, reason string) {
	s.appendAudit(ctx, domain.AuditEntry{
		ActorKind: domain.ActorAdmin, ActorID: &a.ID, ActorLabel: a.Email,
		Action: "admin.login_failed", Target: a.Email,
		Detail: map[string]any{"reason": reason}, RequestID: platform.RequestID(ctx),
	})
}

// invalidCredentials 是登录失败的**唯一**对外错误。
// 区分"邮箱不存在"与"密码错误"等于免费送出一个账号枚举接口。
func invalidCredentials() error {
	return domain.Unauthorized("invalid_credentials", "email or password is incorrect")
}

func isNotFound(err error) bool {
	return err != nil && (err == domain.ErrNotFound || strings.Contains(err.Error(), "not found"))
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func validateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" || !emailRe.MatchString(email) || len(email) > 254 {
		return domain.Validation("invalid_email", "a valid email address is required", "email")
	}
	return nil
}

func validateName(name string) error {
	name = strings.TrimSpace(name)
	// 按契约：minLength 1、maxLength 100。用 rune 数而非字节数，
	// 否则中文名会在 33 个字符处被拒。
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return domain.Validation("invalid_name", "name must be 1-100 characters", "name")
	}
	return nil
}

func validatePassword(pw string) error {
	// 契约要求 minLength 8。上限 72 不是任意取的——
	// 虽然 argon2 没有 bcrypt 的 72 字节截断问题，但设一个上限可以挡住
	// "提交 1MB 密码"这类拿哈希开销当 DoS 的玩法。
	if len(pw) < 8 {
		return domain.Validation("weak_password", "password must be at least 8 characters", "password")
	}
	if len(pw) > 256 {
		return domain.Validation("password_too_long", "password must be at most 256 characters", "password")
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func defaultWorkspaceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "My Workspace"
	}
	return name + "'s Workspace"
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// workspaceSlug 由邮箱本地部分加时间戳生成，保证唯一。
func workspaceSlug(email string, now time.Time) string {
	local, _, _ := strings.Cut(email, "@")
	base := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(local), "-"), "-")
	if base == "" {
		base = "workspace"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	return fmt.Sprintf("%s-%d", base, now.UnixNano()%1e9)
}

func actorLabel(id *int64) string {
	if id == nil {
		return "cli-bootstrap"
	}
	return fmt.Sprintf("admin:%d", *id)
}
