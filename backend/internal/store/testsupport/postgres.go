// Package testsupport 提供集成测试用的真实 Postgres。
//
// 为什么不用 SQLite 跑测试（ARCHITECTURE.md §7）：用 SQLite 测、Postgres 跑生产，
// 等于把方言相关的 bug 全部排除在测试覆盖之外——并发写、事务隔离、类型严格性、
// 时间语义、表分区、COPY 批量写，六项差异里后两项恰恰是我们写入压力最大的路径。
// **测试全绿而生产炸掉**正是这种配置的典型产出。
package testsupport

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
)

const (
	image    = "postgres:17-alpine"
	dbName   = "umaas_test"
	dbUser   = "umaas"
	dbPasswd = "umaas"
)

// Postgres 起一个真实的 Postgres 容器并跑完迁移，返回可用的 DB。
//
// 容器复用（Reuse）打开后，首次几秒、后续毫秒级。为了省这几秒而换来一个
// 测不准的测试套件，不划算。
//
// 环境里没有 Docker 时跳过而不是失败——本地开发机可能没装，
// 但 CI 必须有（Makefile 的 test-integration 会强制）。
func Postgres(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()

	// 已经有一个真实 Postgres 时直接用它（CI 的 service container、
	// 本地已起好的实例）。testcontainers 只是"没有现成库时的兜底"，
	// 不是唯一路径——CI 上重复起容器只是白等。
	if dsn := externalDSN(); dsn != "" {
		return openAndMigrate(t, ctx, dsn)
	}

	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(dbUser),
		tcpostgres.WithPassword(dbPasswd),
		testcontainers.WithReuseByName("umaas-test-postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skipping integration test: cannot start postgres container (%v)", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return openAndMigrate(t, ctx, dsn)
}

// externalDSN 读取外部提供的测试库地址。
// UMAAS_TEST_DSN 优先于 UMAAS_DATABASE__DSN，这样本地跑集成测试时
// 不必冒着连到开发库的风险。
func externalDSN() string {
	if dsn := os.Getenv("UMAAS_TEST_DSN"); dsn != "" {
		return dsn
	}
	return os.Getenv("UMAAS_DATABASE__DSN")
}

func openAndMigrate(t *testing.T, ctx context.Context, dsn string) *store.DB {
	t.Helper()

	if err := store.NewMigrator(dsn).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.Open(ctx, config.DatabaseConfig{DSN: dsn, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
