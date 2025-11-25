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
//	r.Use(slo.Track(onMetric))
//
// Per-route tracking:
//
//	r.Route("/api/v1", func(r chi.Router) {
//		r.Use(slo.Track(onMetric))
//		r.Get("/users", listUsers)
//	})
//
// The middleware captures structured metrics per request including route pattern from Chi,
// HTTP method, status code, and request duration. Metrics are emitted after the request
// completes, allowing you to implement custom aggregation, filtering, or export logic.
package slo

import (
	"context"
	"net/http"
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

// Track returns middleware that emits metrics for each request via callback.
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
//	r.Use(slo.Track(onMetric))
func Track(onMetric func(context.Context, Metric)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			defer func() {
				if err := recover(); err != nil {
					rw.statusCode = http.StatusInternalServerError

					route := r.URL.Path
					if rctx := chi.RouteContext(r.Context()); rctx != nil {
						if pattern := rctx.RoutePattern(); pattern != "" {
							route = pattern
						}
					}

					metric := Metric{
						Method:     r.Method,
						Route:      route,
						StatusCode: rw.statusCode,
						Duration:   time.Since(start),
					}

					onMetric(r.Context(), metric)
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
				StatusCode: rw.statusCode,
				Duration:   time.Since(start),
			}

			onMetric(r.Context(), metric)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
