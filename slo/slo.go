// Package slo provides SLO (Service Level Objective) tracking middleware.
//
// The package emits per-request metrics via callback, following the four golden signals
// from the Google SRE handbook: Latency, Traffic, and Errors. The callback-based design
// works correctly in horizontally scaled environments by delegating aggregation to your
// observability stack (Prometheus, Datadog, OpenTelemetry, etc.).
//
// Basic usage:
//
//	onMetric := func(ctx context.Context, m slo.Metric) {
//		prometheus.ObserveLatency(m.Method, m.Route, m.StatusCode, m.Duration)
//		prometheus.IncRequestCounter(m.Method, m.Route, m.StatusCode)
//	}
//	r.Use(slo.New(onMetric))
//
// Per-route tracking:
//
//	r.Route("/api/v1", func(r chi.Router) {
//		r.Use(slo.New(onMetric))
//		r.Get("/users", listUsers)
//	})
//
// The middleware captures structured metrics per request including route pattern from Chi,
// HTTP method, status code, and request duration. Metrics are emitted after the request
// completes, allowing you to implement custom aggregation, filtering, or export logic.
package slo

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
)

// Metric contains structured data for a single request.
// All fields are populated after the request completes.
type Metric struct {
	// Method is the HTTP method (GET, POST, etc.)
	Method string

	// Route is the Chi route pattern (e.g., "/api/users/{id}")
	// Falls back to URL path if Chi route context is unavailable
	Route string

	// StatusCode is the HTTP status code returned to the client
	StatusCode int

	// Duration is the total request processing time
	Duration time.Duration
}

// Option configures the SLO tracking middleware.
type Option func(*config)

type config struct {
	onMetric func(context.Context, Metric)
}

// New returns middleware that emits metrics for each request via callback.
// The callback receives the request context and structured metric data after
// the request completes. This design allows integration with any observability
// system without forcing specific client libraries.
//
// The middleware captures:
//   - Latency: Request duration from start to completion
//   - Traffic: Request count (via callback invocation)
//   - Errors: Status codes >= 500 indicate errors
//
// Example with Prometheus:
//
//	var (
//		requestDuration = prometheus.NewHistogramVec(
//			prometheus.HistogramOpts{
//				Name: "http_server_request_duration_seconds",
//				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
//			},
//			[]string{"method", "route", "status_code"},
//		)
//		requestsTotal = prometheus.NewCounterVec(
//			prometheus.CounterOpts{Name: "http_server_requests_total"},
//			[]string{"method", "route", "status_code"},
//		)
//	)
//
//	onMetric := func(ctx context.Context, m slo.Metric) {
//		labels := prometheus.Labels{
//			"method": m.Method,
//			"route": m.Route,
//			"status_code": strconv.Itoa(m.StatusCode),
//		}
//		requestDuration.With(labels).Observe(m.Duration.Seconds())
//		requestsTotal.With(labels).Inc()
//	}
//	r.Use(slo.New(onMetric))
func New(onMetric func(context.Context, Metric), opts ...Option) func(http.Handler) http.Handler {
	cfg := &config{onMetric: onMetric}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w}
			rw.statusCode.Store(http.StatusOK)

			defer func() {
				if err := recover(); err != nil {
					rw.statusCode.Store(http.StatusInternalServerError)

					route := r.URL.Path
					if rctx := chi.RouteContext(r.Context()); rctx != nil {
						if pattern := rctx.RoutePattern(); pattern != "" {
							route = pattern
						}
					}

					metric := Metric{
						Method:     r.Method,
						Route:      route,
						StatusCode: int(rw.statusCode.Load()),
						Duration:   time.Since(start),
					}

					cfg.onMetric(r.Context(), metric)
					panic(err)
				}
			}()

			next.ServeHTTP(rw, r)

			route := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					route = pattern
				}
			}

			metric := Metric{
				Method:     r.Method,
				Route:      route,
				StatusCode: int(rw.statusCode.Load()),
				Duration:   time.Since(start),
			}

			cfg.onMetric(r.Context(), metric)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode  atomic.Int32
	wroteHeader atomic.Bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader.CompareAndSwap(false, true) {
		rw.statusCode.Store(int32(code))
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.wroteHeader.CompareAndSwap(false, true) {
		rw.statusCode.Store(http.StatusOK)
		rw.ResponseWriter.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijacking not supported")
}

// Compile-time interface checks
var (
	_ http.Flusher  = (*responseWriter)(nil)
	_ http.Hijacker = (*responseWriter)(nil)
)
