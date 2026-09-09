package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/WALLE-AI/uMaaS/backend/db/migrations"
)

// Migrator 执行 goose 迁移。
//
// goose 需要 database/sql 句柄，而业务代码走 pgx 原生接口——
// 这里用 stdlib.OpenDB 桥接。迁移不在热路径上，多一层无所谓；
// 业务代码不走这条路（ARCHITECTURE.md §2.3）。
type Migrator struct {
	dsn string
}

func NewMigrator(dsn string) *Migrator { return &Migrator{dsn: dsn} }

func (m *Migrator) open() (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(m.dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	return stdlib.OpenDB(*cfg), nil
}

func (m *Migrator) prepare(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	return nil
}

// Up 迁移到最新版本。
func (m *Migrator) Up(ctx context.Context) error {
	db, err := m.open()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := m.prepare(db); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, ".")
}

// Down 回滚一个版本。生产环境慎用——多数迁移的 Down 是有损的。
func (m *Migrator) Down(ctx context.Context) error {
	db, err := m.open()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := m.prepare(db); err != nil {
		return err
	}
	return goose.DownContext(ctx, db, ".")
}

// Version 返回当前 schema 版本。
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	db, err := m.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err := m.prepare(db); err != nil {
		return 0, err
	}
	return goose.GetDBVersionContext(ctx, db)
}

// Status 打印迁移状态。
func (m *Migrator) Status(ctx context.Context) error {
	db, err := m.open()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := m.prepare(db); err != nil {
		return err
	}
	return goose.StatusContext(ctx, db, ".")
}
