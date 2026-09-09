// Command umaas-migrate 是独立的迁移工具。
//
// 生产环境**不在服务启动时执行迁移**：多副本同时启动会竞争迁移锁，
// 且一次失败的迁移会让整个服务起不来。正确做法是把迁移作为部署流水线的
// 独立步骤，迁移成功后再滚动更新服务（ARCHITECTURE.md §8）。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
)

func main() {
	configPath := flag.String("config", "", "path to config file (optional)")
	timeout := flag.Duration("timeout", 5*time.Minute, "migration timeout")
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		usage()
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	m := store.NewMigrator(cfg.Database.DSN)

	switch cmd {
	case "up":
		if err := m.Up(ctx); err != nil {
			fail(err)
		}
		v, err := m.Version(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Printf("migrated to version %d\n", v)
	case "down":
		if err := m.Down(ctx); err != nil {
			fail(err)
		}
		fmt.Println("rolled back one version")
	case "status":
		if err := m.Status(ctx); err != nil {
			fail(err)
		}
	case "version":
		v, err := m.Version(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Println(v)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `umaas-migrate — schema migrations

usage: umaas-migrate [flags] <up|down|status|version>

  up       migrate to the latest version
  down     roll back one version
  status   print migration status
  version  print current schema version

flags:
`)
	flag.PrintDefaults()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
