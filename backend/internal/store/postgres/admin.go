package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

type AdminRepo struct {
	db *store.DB
	q  *dbgen.Queries
}

func NewAdminRepo(db *store.DB) *AdminRepo {
	return &AdminRepo{db: db, q: dbgen.New(db.Pool)}
}

var _ domain.AdminRepository = (*AdminRepo)(nil)

func (r *AdminRepo) CreateAdmin(ctx context.Context, in domain.NewAdmin) (*domain.AdminAccount, error) {
	row, err := r.q.CreateAdminAccount(ctx, dbgen.CreateAdminAccountParams{
		Email:         in.Email,
		Name:          in.Name,
		Role:          string(in.Role),
		PasswordHash:  in.PasswordHash,
		TotpSecretEnc: in.TOTPSecret,
		TotpEnabled:   in.TOTPEnabled,
		CreatedBy:     in.CreatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.Conflict("admin_email_taken", "an admin with this email already exists")
		}
		return nil, fmt.Errorf("create admin: %w", err)
	}
	return toDomainAdmin(row), nil
}

// GetAdminByEmail 返回账号、密码哈希与 TOTP 密文。
//
// 三样一起返回而不是分三次查：登录路径上每多一次往返都会被放大到每次登录。
// 密码哈希与 TOTP 密文**不进 domain.AdminAccount**——领域模型里不该有凭证，
// 否则它迟早会被某个 handler 顺手序列化进响应体。
func (r *AdminRepo) GetAdminByEmail(ctx context.Context, email string) (*domain.AdminAccount, string, []byte, error) {
	row, err := r.q.GetAdminByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil, domain.ErrNotFound
		}
		return nil, "", nil, fmt.Errorf("get admin by email: %w", err)
	}
	return toDomainAdmin(row), row.PasswordHash, row.TotpSecretEnc, nil
}

func (r *AdminRepo) CountAdmins(ctx context.Context) (int64, error) {
	n, err := r.q.CountAdmins(ctx)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

func (r *AdminRepo) SetAdminTOTP(ctx context.Context, adminID int64, secretEnc []byte, enabled bool) error {
	if err := r.q.SetAdminTOTP(ctx, dbgen.SetAdminTOTPParams{
		ID: adminID, TotpSecretEnc: secretEnc, TotpEnabled: enabled,
	}); err != nil {
		return fmt.Errorf("set admin totp: %w", err)
	}
	return nil
}

func (r *AdminRepo) TouchAdminActivity(ctx context.Context, adminID int64) error {
	if err := r.q.TouchAdminActivity(ctx, adminID); err != nil {
		return fmt.Errorf("touch admin activity: %w", err)
	}
	return nil
}

func (r *AdminRepo) CreateAdminSession(ctx context.Context, in domain.NewSession) (int64, error) {
	row, err := r.q.CreateAdminSession(ctx, dbgen.CreateAdminSessionParams{
		TokenHash: in.TokenHash,
		AdminID:   in.UserID,
		ExpiresAt: in.ExpiresAt,
		Ip:        nullableAddr(in.IP),
		UserAgent: nullableString(in.UserAgent),
	})
	if err != nil {
		return 0, fmt.Errorf("create admin session: %w", err)
	}
	return row.ID, nil
}

func (r *AdminRepo) LookupAdminSession(ctx context.Context, tokenHash []byte) (*domain.AdminPrincipal, error) {
	row, err := r.q.GetAdminSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
		}
		return nil, fmt.Errorf("get admin session: %w", err)
	}
	if row.RevokedAt != nil || !time.Now().Before(row.ExpiresAt) {
		return nil, domain.Unauthorized("invalid_session", "session is invalid or expired")
	}
	// 停用的管理员，已签发的会话必须立即失效——否则"停用"要等到会话过期才生效，
	// 而那正是最需要它立刻生效的场合。
	if row.DisabledAt != nil {
		return nil, domain.Forbidden("admin_disabled", "this admin account is disabled")
	}

	return &domain.AdminPrincipal{
		Account: domain.AdminAccount{
			ID: row.AdminID, Email: row.Email, Name: row.Name,
			Role: domain.AdminRole(row.Role), TOTPEnabled: row.TotpEnabled,
		},
		SessionID: row.ID,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (r *AdminRepo) RevokeAdminSession(ctx context.Context, tokenHash []byte) error {
	if err := r.q.RevokeAdminSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("revoke admin session: %w", err)
	}
	return nil
}

func toDomainAdmin(a dbgen.AdminAccount) *domain.AdminAccount {
	return &domain.AdminAccount{
		ID: a.ID, Email: a.Email, Name: a.Name,
		Role:         domain.AdminRole(a.Role),
		TOTPEnabled:  a.TotpEnabled,
		LastActiveAt: a.LastActiveAt, DisabledAt: a.DisabledAt,
		CreatedAt: a.CreatedAt,
	}
}

// ── 审计 ────────────────────────────────────────────────────────

type AuditRepo struct {
	q *dbgen.Queries
}

func NewAuditRepo(db *store.DB) *AuditRepo { return &AuditRepo{q: dbgen.New(db.Pool)} }

var _ domain.AuditRepository = (*AuditRepo)(nil)

func (r *AuditRepo) Append(ctx context.Context, e domain.AuditEntry) error {
	detail, err := marshalDetail(e.Detail)
	if err != nil {
		return fmt.Errorf("marshal audit detail: %w", err)
	}
	_, err = r.q.InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
		ActorKind:   string(e.ActorKind),
		ActorID:     e.ActorID,
		ActorLabel:  e.ActorLabel,
		Action:      e.Action,
		Target:      e.Target,
		Destructive: e.Destructive,
		Detail:      detail,
		Ip:          nullableAddr(e.IP),
		RequestID:   nullableString(e.RequestID),
	})
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
