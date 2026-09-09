// Package observ 是可观测底座：结构化日志、OpenTelemetry 指标、Prometheus 暴露。
//
// admin 的 GPU 监控页已按 Prometheus 指标设计（ARCHITECTURE.md §2.5），
// 因此指标必须能被 Prometheus 抓取。request_id 贯穿日志与 trace，
// 是拆分后排查问题的唯一抓手（SERVICE-DECOMPOSITION.md §9）。
package observ

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
)

// SetupLogger 装配全局 slog。
func SetupLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(&contextHandler{Handler: handler})
	slog.SetDefault(logger)
	return logger
}

// contextHandler 自动把 context 里的 request_id 带进每条日志。
//
// 不这么做就得在每个 slog 调用点手写 "request_id", platform.RequestID(ctx)——
// 漏一处，那条日志就串不进链路，而这类遗漏只有在排查故障时才会发现。
type contextHandler struct{ slog.Handler }

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := platform.RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

// Metrics 持有指标 provider 与它的 HTTP 暴露端点。
type Metrics struct {
	provider *sdkmetric.MeterProvider
	Handler  http.Handler
	HTTP     *HTTPMetrics
}

// SetupMetrics 装配 OpenTelemetry MeterProvider 并接上 Prometheus exporter。
func SetupMetrics(ctx context.Context, cfg config.ObservConfig, version string) (*Metrics, error) {
	// 用显式的注册表，而不是 prometheus 的全局默认值。
	//
	// otelprom.New() 默认注册到**自己的**注册表，而 promhttp.Handler() 服务的是
	// 全局默认注册表——两者不是同一个，结果是 /metrics 里只有 go_* 运行时指标，
	// 我们自己的一条都没有。这个坑由 I1 的冒烟测试抓到（"/metrics 可抓"的判据
	// 当时只检查了有没有 # HELP，被 go_* 蒙混过关，因此判据也一并收紧）。
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}
	// 用 resource.New 而不是 Merge(Default(), NewWithAttributes(semconv.SchemaURL, ...))：
	// 后者要求手写的 semconv 版本与 SDK 内置的完全一致，版本一升级就会
	// "conflicting Schema URL" 直接让服务起不来——这个坑由 I0 的冒烟测试抓到。
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", cfg.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithResource(res))
	otel.SetMeterProvider(provider)

	meter := provider.Meter("umaas")

	// build_info 是个恒为 1 的计数器，作用是让 /metrics 至少有一条序列，
	// 且能在监控里回答"线上跑的是哪个版本"——发布回滚时这是第一个要看的东西。
	buildInfo, err := meter.Int64Counter("umaas.build.info",
		metric.WithDescription("Build information; always 1"))
	if err != nil {
		return nil, fmt.Errorf("create build info metric: %w", err)
	}
	buildInfo.Add(ctx, 1, metric.WithAttributes(
		attribute.String("version", version),
		attribute.String("service", cfg.ServiceName),
	))

	httpMetrics, err := NewHTTPMetrics(meter)
	if err != nil {
		return nil, fmt.Errorf("create http metrics: %w", err)
	}

	return &Metrics{
		provider: provider,
		Handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		HTTP:     httpMetrics,
	}, nil
}

func (m *Metrics) Shutdown(ctx context.Context) error {
	if m == nil || m.provider == nil {
		return nil
	}
	return m.provider.Shutdown(ctx)
}
