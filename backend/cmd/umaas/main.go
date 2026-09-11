// Command umaas 是主二进制，可同时或分别启用控制平面与数据平面。
//
// 两个平面共用一个进程但**不共用 router、中间件链与连接池**（ARCHITECTURE.md §1）。
// 规模上来后同一份代码分开部署，不用改代码——只改配置里的 enabled。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/WALLE-AI/uMaaS/backend/internal/assembly"
	"github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/catalogadmin"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/gateway"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi"
	admincatalogapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/admincatalog"
	authapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/auth"
	catalogapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/observ"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider/openaicompat"
	"github.com/WALLE-AI/uMaaS/backend/internal/requestlog"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres"
)

// version 由构建时注入：-ldflags "-X main.version=$(git describe --tags)"
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// 子命令：`umaas admin create ...`。放在 flag.Parse 之前，
	// 因为子命令有自己的 flag 集合。
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		return runAdminCommand(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		return runSeedCommand(os.Args[2:])
	}

	configPath := flag.String("config", "", "path to config file (optional; env vars still apply)")
	migrate := flag.Bool("migrate", false, "run migrations before starting (dev only)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logger := observ.SetupLogger(cfg.Log)
	logger.Info("starting umaas",
		"version", version,
		"env", cfg.Env,
		"topology", cfg.Topology,
		"control_plane", cfg.ControlPlane.Enabled,
		"data_plane", cfg.DataPlane.Enabled,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 迁移：生产环境走独立的 umaas-migrate，这里只服务开发便利。
	// config.Validate 已经拒绝了 prod + auto_migrate 的组合。
	if *migrate || cfg.Database.AutoMigrate {
		if cfg.Env == "prod" {
			return errors.New("refusing to migrate at startup in prod; run `umaas-migrate up` instead")
		}
		logger.Info("running migrations")
		if err := store.NewMigrator(cfg.Database.DSN).Up(ctx); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// 目录与价目（I2）。**Resolver 只建一个实例**并同时交给展示侧与将来的
	// 结算侧：这是"展示价与计费价同源"的装配落点（BILLING-AND-PRICING.md §3.6-②）。
	pricingRepo := postgres.NewPricingRepo(db)
	resolver := pricing.NewResolver(pricingRepo)
	catalogService := catalog.NewService(postgres.NewCatalogRepo(db))
	auditRepo := postgres.NewAuditRepo(db)
	catalogAdminService := catalogadmin.NewService(postgres.NewCatalogAdminRepo(db), auditRepo)

	services, err := assembly.Build(cfg, assembly.Deps{
		Catalog:      catalogService,
		Pricing:      resolver,
		CatalogAdmin: catalogAdminService,
	})
	if err != nil {
		return err
	}

	// 加密器：TOTP 密钥与上游凭证都靠它。dev 环境允许缺省，
	// 但那样创建管理员会明确失败——比默默用一个空密钥好。
	var cipher *platform.Cipher
	if keyBytes, err := cfg.EncryptionKeyBytes(); err != nil {
		return err
	} else if len(keyBytes) > 0 {
		if cipher, err = platform.NewCipher(keyBytes); err != nil {
			return err
		}
	} else {
		logger.Warn("security.encryption_key is not set; admin creation will fail")
	}

	authService := auth.NewService(
		postgres.NewIdentityRepo(db),
		postgres.NewAdminRepo(db),
		auditRepo,
		cipher,
	)
	authHandler := authapi.New(authService, cfg.Security.SecureCookies)

	var metrics *observ.Metrics
	if cfg.Observ.MetricsEnabled {
		metrics, err = observ.SetupMetrics(ctx, cfg.Observ, version)
		if err != nil {
			return fmt.Errorf("setup metrics: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metrics.Shutdown(shutdownCtx); err != nil {
				logger.Warn("metrics shutdown", "error", err)
			}
		}()
	}

	var httpMetrics *observ.HTTPMetrics
	if metrics != nil {
		httpMetrics = metrics.HTTP
	}

	g, gctx := errgroup.WithContext(ctx)
	var servers []*http.Server

	if cfg.ControlPlane.Enabled {
		srv := newServer(cfg.ControlPlane, httpapi.NewRouter(httpapi.Deps{
			Config:       cfg,
			Services:     services,
			Health:       db.Health,
			Version:      version,
			Auth:         authHandler,
			Catalog:      catalogapi.New(catalogService),
			AdminCatalog: admincatalogapi.New(catalogAdminService),
			Metrics:      httpMetrics,
		}))
		servers = append(servers, srv)
		g.Go(func() error { return serve(gctx, logger, "control-plane", srv) })
	}

	var requestLogs *requestlog.Writer
	if cfg.DataPlane.Enabled {
		// 只在真的要装配数据平面时才要求渠道配置——`umaas-migrate`/`umaas seed`/
		// `umaas admin create` 也走 config.Load，但它们不启动数据平面
		// （config.go 的 ValidateGateway 注释解释了为什么不放进 Validate()）。
		if err := cfg.ValidateGateway(); err != nil {
			return err
		}
		requestLogs = requestlog.NewWriter(db)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requestLogs.Close(shutdownCtx)
		}()

		// I4：渠道来自数据库（provider_profiles/channels），不是启动期配置。
		// Router 每次请求都查一次候选（RoutingRepo 内部无缓存——渠道是慢变
		// 数据，秒级 TTL 缓存是下一步优化，现在先保证正确性）。
		httpClient := &http.Client{}
		newDriver := func(p provider.Profile) gateway.UpstreamDoer { return openaicompat.New(p, httpClient) }
		routingRepo := postgres.NewRoutingRepo(db, cipher)
		channelRouter := gateway.NewChannelRouter(routingRepo)

		srv := newServer(cfg.DataPlane, gateway.NewRouter(gateway.Deps{
			Config: cfg, Services: services, Version: version, Metrics: httpMetrics,
			Completions: &gateway.CompletionsHandler{
				Models: postgres.NewGatewayModelRepo(db), Router: channelRouter,
				NewDriver: newDriver, Pricing: resolver, Logs: requestLogs,
				FirstByteTimeout: cfg.Gateway.Stream.FirstByteTimeout,
				StallTimeout:     cfg.Gateway.Stream.StallTimeout,
			},
		}))
		servers = append(servers, srv)
		g.Go(func() error { return serve(gctx, logger, "data-plane", srv) })

		// B4 的后台健康探针：定期给到期的渠道发一次探测请求，结果喂进
		// Beta 后验。与 X10 的手工测试是**两条独立代码路径**（gateway.ProbeService
		// 的 ManualTest/AutoProbe），不共用一个 isManual 参数。
		healthChecker := &gateway.HealthChecker{
			Repo:      routingRepo,
			Probe:     &gateway.ProbeService{Repo: routingRepo, Logs: requestLogs},
			NewDriver: newDriver,
		}
		g.Go(func() error { healthChecker.Run(gctx); return nil })
	}

	if metrics != nil {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler)
		srv := &http.Server{Addr: cfg.Observ.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, srv)
		g.Go(func() error { return serve(gctx, logger, "metrics", srv) })
	}

	<-gctx.Done()
	logger.Info("shutting down")
	shutdownAll(logger, cfg, servers)

	if err := g.Wait(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("stopped cleanly")
	return nil
}

func newServer(pc config.PlaneConfig, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              pc.Addr,
		Handler:           h,
		ReadHeaderTimeout: pc.ReadHeaderTimeout,
		// WriteTimeout 对数据平面必须是 0——SSE 流会跑几分钟，
		// 整体写超时会在正常的长响应中途砍断连接。config.Validate 已强制。
		WriteTimeout: pc.WriteTimeout,
		IdleTimeout:  pc.IdleTimeout,
	}
}

func serve(ctx context.Context, logger *slog.Logger, name string, srv *http.Server) error {
	logger.Info("listening", "plane", name, "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// shutdownAll 优雅停机。
//
// 数据平面的 ShutdownTimeout 比控制平面长：在途的流式请求可能还要跑几十秒，
// 直接切断等于让用户的请求白费——而那是已经付过上游费用的。
func shutdownAll(logger *slog.Logger, cfg *config.Config, servers []*http.Server) {
	timeout := cfg.ControlPlane.ShutdownTimeout
	if cfg.DataPlane.Enabled && cfg.DataPlane.ShutdownTimeout > timeout {
		timeout = cfg.DataPlane.ShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			logger.Warn("shutdown", "addr", srv.Addr, "error", err)
		}
	}
}
