package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/gateway"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// GatewayModelRepo 实现数据平面的目录只读查询（B3）。
type GatewayModelRepo struct {
	q *dbgen.Queries
}

func NewGatewayModelRepo(db *store.DB) *GatewayModelRepo {
	return &GatewayModelRepo{q: dbgen.New(db.Pool)}
}

var _ gateway.ModelRepository = (*GatewayModelRepo)(nil)

func (r *GatewayModelRepo) ResolveCallable(ctx context.Context, providerSlug, modelSlug string) (*gateway.ResolvedModel, error) {
	row, err := r.q.GetCallableModelByPath(ctx, dbgen.GetCallableModelByPathParams{
		Slug: providerSlug, Slug_2: modelSlug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("model_not_found", "model does not exist or is not callable")
		}
		return nil, fmt.Errorf("resolve callable model: %w", err)
	}
	return &gateway.ResolvedModel{
		ID: row.ID, BillingModel: row.BillingModel,
		Hosting: row.Hosting, Capabilities: row.Capabilities,
	}, nil
}
