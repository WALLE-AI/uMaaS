package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// PricingRepo 是价目的持久化实现。
//
// 它同时服务展示侧与结算侧，**这是有意的**：两条路径共用一个仓储、一个
// SQL 函数、一个费率解析器，才能兑现"展示价与计费价必须是同一条记录"
// （BILLING-AND-PRICING.md §3.6-②）。
type PricingRepo struct {
	db *store.DB
	q  *dbgen.Queries
}

func NewPricingRepo(db *store.DB) *PricingRepo {
	return &PricingRepo{db: db, q: dbgen.New(db.Pool)}
}

var _ pricing.Repository = (*PricingRepo)(nil)

func (r *PricingRepo) ResolveVersion(ctx context.Context, key pricing.Key, at time.Time) (*pricing.Version, error) {
	fallback := key.FallbackBillingModel
	if fallback == "" {
		fallback = key.BillingModel
	}
	params := dbgen.ResolvePriceVersionParams{
		BillingModel:         key.BillingModel,
		FallbackBillingModel: fallback,
		At:                   at,
	}
	if key.Plan != "" {
		params.Plan = &key.Plan
	}
	if key.WorkspaceID != "" {
		params.WorkspaceID = &key.WorkspaceID
	}

	row, err := r.q.ResolvePriceVersion(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pricing.NotPriced(key.BillingModel)
		}
		return nil, fmt.Errorf("resolve price version: %w", err)
	}
	return versionFromRow(row)
}

func versionFromRow(row dbgen.ModelPrice) (*pricing.Version, error) {
	rates, err := pricing.ParseRates(row.Rates)
	if err != nil {
		return nil, err
	}
	v := &pricing.Version{
		ID: row.ID, BillingModel: row.BillingModel, ScopeKind: row.ScopeKind,
		Currency: row.Currency, Rates: rates, EffectiveFrom: row.EffectiveFrom,
		Source: row.Source, AppliedBy: row.AppliedBy, Note: row.Note,
	}
	if row.ScopeID != nil {
		v.ScopeID = *row.ScopeID
	}
	return v, nil
}

// ── admin 写入口（M1a）─────────────────────────────────────────

// PublishPriceInput 是一次改价。
type PublishPriceInput struct {
	BillingModel  string
	ScopeKind     string
	ScopeID       string
	Currency      string
	Rates         json.RawMessage
	EffectiveFrom time.Time
	Source        string
	AppliedBy     string
	Note          string
}

// PublishPrice 关闭旧版本并插入新版本，**在一个事务里**。
//
// 分成两条语句执行而不加事务的话，中间失败会留下一个"没有任何版本生效"的
// 窗口——那期间进来的请求算不出钱。窗口只有几毫秒，但它在生产上一定会被撞上。
func (r *PricingRepo) PublishPrice(ctx context.Context, in PublishPriceInput) (*pricing.Version, error) {
	var out *pricing.Version
	err := r.db.InTx(ctx, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		// effective_to 是可空列，因此生成的参数是指针。传的是新版本的生效时刻：
		// 旧版本在新版本生效的那一刻**恰好**结束，中间不留缝也不重叠。
		effectiveFrom := in.EffectiveFrom
		closeParams := dbgen.ClosePriceVersionsParams{
			EffectiveFrom: &effectiveFrom,
			BillingModel:  in.BillingModel,
			ScopeKind:     in.ScopeKind,
		}
		if in.ScopeID != "" {
			closeParams.ScopeID = &in.ScopeID
		}
		if _, err := q.ClosePriceVersions(ctx, closeParams); err != nil {
			return fmt.Errorf("close price versions: %w", err)
		}

		insert := dbgen.InsertPriceVersionParams{
			BillingModel:  in.BillingModel,
			ScopeKind:     in.ScopeKind,
			Currency:      in.Currency,
			Rates:         in.Rates,
			EffectiveFrom: in.EffectiveFrom,
			Source:        in.Source,
			AppliedBy:     in.AppliedBy,
			Note:          in.Note,
		}
		if in.ScopeID != "" {
			insert.ScopeID = &in.ScopeID
		}
		row, err := q.InsertPriceVersion(ctx, insert)
		if err != nil {
			if isUniqueViolation(err) {
				// 同一个生效时刻已经有一版了。这通常是重复提交，
				// 报冲突比覆盖安全——覆盖等于原地改价。
				return domain.Conflict("price_version_exists",
					"a price version with this effective_from already exists")
			}
			return fmt.Errorf("insert price version: %w", err)
		}
		out, err = versionFromRow(row)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PriceHistory 返回某个计费主体的版本历史，供 admin 审阅与客服追溯。
func (r *PricingRepo) PriceHistory(ctx context.Context, billingModel string, limit int32) ([]*pricing.Version, error) {
	rows, err := r.q.ListPriceVersions(ctx, dbgen.ListPriceVersionsParams{
		BillingModel: billingModel, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("price history: %w", err)
	}
	out := make([]*pricing.Version, 0, len(rows))
	for _, row := range rows {
		v, err := versionFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// nowUTC 集中一处取当前时间。
//
// admin 页面上"当前生效的价目"取的是 now()，而结算取的是 received_at——
// 两者不同是**有意的**（§3.3-3），把它们写成同一个字面量 time.Now() 会让
// 这个区别在代码里消失，下一个人很容易顺手把结算路径也改成 now()。
func nowUTC() time.Time { return time.Now().UTC() }
