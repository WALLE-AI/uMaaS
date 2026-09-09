//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/store/testsupport"
)

// 集成测试跑在真实 Postgres 上（testcontainers），与生产同构。
// 用 build tag 隔开：`go test ./...` 不会碰它，`make test-integration` 才跑。

func TestMigrationsApplyCleanly(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()

	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM platform_meta WHERE key = 'schema'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestHealthOnLiveDatabase(t *testing.T) {
	db := testsupport.Postgres(t)
	require.NoError(t, db.Health(context.Background()))
}

// 事务封装：出错必须回滚。这条在 SQLite 上测不出真实语义——
// 它的隔离级别实质接近串行化（ARCHITECTURE.md §7）。
func TestInTxRollsBackOnError(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()

	_, err := db.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS tx_probe (id int PRIMARY KEY)`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Pool.Exec(ctx, `DROP TABLE IF EXISTS tx_probe`) })

	sentinel := assert.AnError
	err = db.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tx_probe (id) VALUES (1)`); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM tx_probe`).Scan(&n))
	assert.Zero(t, n, "transaction must have rolled back")
}
