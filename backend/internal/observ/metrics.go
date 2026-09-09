package observ

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// HTTPMetrics 记录请求计数与时延。
//
// 两个平面**分开打标**（plane 属性）：控制平面按 CPU 扩容、数据平面按在途连接数
// 扩容（ARCHITECTURE.md §1），把两者的指标混在一起，扩容依据就没法看了。
type HTTPMetrics struct {
	requests metric.Int64Counter
	duration metric.Float64Histogram
	inflight metric.Int64UpDownCounter
}

func NewHTTPMetrics(m metric.Meter) (*HTTPMetrics, error) {
	requests, err := m.Int64Counter("umaas.http.requests",
		metric.WithDescription("HTTP requests handled"))
	if err != nil {
		return nil, err
	}
	duration, err := m.Float64Histogram("umaas.http.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	// 在途请求数是数据平面的扩容依据——它的请求持续几十秒，
	// QPS 与 CPU 都反映不了真实负载。
	inflight, err := m.Int64UpDownCounter("umaas.http.requests.inflight",
		metric.WithDescription("In-flight HTTP requests"))
	if err != nil {
		return nil, err
	}
	return &HTTPMetrics{requests: requests, duration: duration, inflight: inflight}, nil
}

// Middleware 返回按平面打标的中间件。
func (h *HTTPMetrics) Middleware(plane string) func(http.Handler) http.Handler {
	planeAttr := attribute.String("plane", plane)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h == nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			h.inflight.Add(ctx, 1, metric.WithAttributes(planeAttr))
			defer h.inflight.Add(ctx, -1, metric.WithAttributes(planeAttr))

			start := time.Now()
			rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			// 用路由模式而不是原始路径：/models/{provider}/{model} 若按实际路径打标，
			// 上千个模型会产生上千条时间序列，Prometheus 会被基数打爆。
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			attrs := metric.WithAttributes(
				planeAttr,
				attribute.String("method", r.Method),
				attribute.String("route", route),
				attribute.String("status", strconv.Itoa(rec.status)),
			)
			h.requests.Add(ctx, 1, attrs)
			h.duration.Record(ctx, time.Since(start).Seconds(), attrs)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Flush 与 Unwrap 必须转发，否则 SSE 会退化成"慢的非流式"。
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
