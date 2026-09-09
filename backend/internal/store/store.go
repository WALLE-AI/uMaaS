// Package store 是持久化实现的入口：连接池、事务封装、健康检查。
//
// 生成的 sqlc 代码将放在 store/postgres/，业务代码只依赖 domain 的 Repository 接口
// （ARCHITECTURE.md §2.2）。保留接口层不是为了换数据库——那是很少兑现的承诺——
// 而是为了单元测试能塞内存假实现。
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/WALLE-AI/uMaaS/backend/internal/config"
)

// DB 包住连接池。用 pgx 原生接口而不套 database/sql：
// COPY（批量写用量日志）与二进制协议都在热路径上（ARCHITECTURE.md §2.3）。
type DB struct {
	Pool *pgxpool.Pool
}

// Open 建立连接池并做一次连通性验证。
func Open(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}

	// PgBouncer 的 transaction 模式不支持 prepared statement 缓存。
	// Supabase 默认给的就是这个地址（6543 端口）——不关掉缓存会在运行时
	// 报 "prepared statement already exists"，且现象随并发飘忽（ARCHITECTURE.md §2.4）。
	if cfg.PgBouncerMode {
		poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
		poolCfg.ConnConfig.StatementCacheCapacity = 0
		poolCfg.ConnConfig.DescriptionCacheCapacity = 0
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// Health 供 /readyz 使用。区别于 /healthz：后者只表示进程活着。
func (db *DB) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return db.Pool.Ping(ctx)
}

// InTx 在一个事务里跑 fn，出错回滚。
//
// 注意：**跨模块调用不共享事务**（SERVICE-DECOMPOSITION.md §5-规则二）。
// 这个封装只服务模块内部；把 tx 传出模块边界会让"本地实现"与"远程实现"的
// 事务语义产生隐藏差异，切拓扑时静默变成最终一致。
func (db *DB) InTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// Rollback 在已提交的事务上是 no-op，安全。
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
