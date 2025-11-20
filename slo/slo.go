// Package slo provides SLO (Service Level Objective) tracking middleware.
//
// The package tracks key SLO metrics including request count, error rate, availability,
// and latency percentiles (p50, p95, p99). Metrics can be exported periodically to
// monitoring systems like Prometheus, Datadog, or custom backends.
//
// Basic usage:
//
//	metrics := slo.NewMetrics(1000)
//	r.Use(slo.Track(metrics))
//	stats := metrics.Stats() // Get current metrics
//
// With periodic export:
//
//	exporter := func(stats slo.Stats) {
//		prometheus.RecordAvailability(stats.Availability)
//		prometheus.RecordLatency(stats.Latency.P99)
//	}
//	r.Use(slo.Track(metrics, slo.WithExporter(exporter, 60*time.Second)))
//
// The middleware considers responses with status >= 500 as errors when calculating
// availability and error rate. All other status codes are considered successful.
package slo

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Metrics contains SLO tracking data with thread-safe access.
// Maintains a bounded buffer of recent latencies to calculate percentiles.
type Metrics struct {
	mu sync.RWMutex

	totalRequests int64
	errorRequests int64
	latencies     []time.Duration
	maxLatencies  int
}

// NewMetrics creates a new Metrics tracker with a bounded latency buffer.
// The maxLatencies parameter determines how many recent latency samples to keep
// for percentile calculations. Larger values provide more accurate percentiles
// but use more memory. When the buffer is full, the oldest latency is discarded.
//
// A value of 1000 is suitable for most applications. Set to 0 to use the default of 1000.
func NewMetrics(maxLatencies int) *Metrics {
	if maxLatencies <= 0 {
		maxLatencies = 1000
	}
	return &Metrics{
		latencies:    make([]time.Duration, 0, maxLatencies),
		maxLatencies: maxLatencies,
	}
}

// Stats returns current SLO statistics as a snapshot.
type Stats struct {
	// TotalRequests is the total number of requests processed
	TotalRequests int64

	// ErrorRequests is the number of requests that resulted in errors (status >= 500)
	ErrorRequests int64

	// ErrorRate is the ratio of error requests to total requests (0.0 to 1.0)
	ErrorRate float64

	// Availability is the ratio of successful requests to total requests (0.0 to 1.0)
	// Calculated as (total - errors) / total
	Availability float64

	// Latency contains percentile statistics for request duration
	Latency LatencyStats
}

// LatencyStats contains latency percentile data.
type LatencyStats struct {
	// P50 is the median latency (50th percentile)
	P50 time.Duration

	// P95 is the 95th percentile latency
	P95 time.Duration

	// P99 is the 99th percentile latency
	P99 time.Duration
}

// Stats returns a snapshot of current metrics.
// This method is thread-safe and can be called concurrently with request processing.
func (m *Metrics) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := float64(m.totalRequests)
	errors := float64(m.errorRequests)

	errorRate := 0.0
	if total > 0 {
		errorRate = errors / total
	}

	availability := 0.0
	if total > 0 {
		availability = (total - errors) / total
	}

	return Stats{
		TotalRequests: m.totalRequests,
		ErrorRequests: m.errorRequests,
		ErrorRate:     errorRate,
		Availability:  availability,
		Latency:       m.calculateLatencyStats(),
	}
}

func (m *Metrics) calculateLatencyStats() LatencyStats {
	if len(m.latencies) == 0 {
		return LatencyStats{}
	}

	sorted := make([]time.Duration, len(m.latencies))
	copy(sorted, m.latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	return LatencyStats{
		P50: percentile(sorted, 0.50),
		P95: percentile(sorted, 0.95),
		P99: percentile(sorted, 0.99),
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)) * p)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func (m *Metrics) record(latency time.Duration, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests++
	if isError {
		m.errorRequests++
	}

	if len(m.latencies) >= m.maxLatencies {
		m.latencies = m.latencies[1:]
	}
	m.latencies = append(m.latencies, latency)
}

// Reset clears all metrics, setting counters to zero and clearing latency buffer.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests = 0
	m.errorRequests = 0
	m.latencies = m.latencies[:0]
}

// MetricsExporter is called periodically with current metrics.
// Implement this to export metrics to your monitoring system (Prometheus, Datadog, etc.).
type MetricsExporter func(Stats)

// TrackConfig configures the Track middleware.
type TrackConfig struct {
	// Metrics is the metrics instance to record to
	Metrics *Metrics

	// Exporter is called periodically with current metrics (optional)
	Exporter MetricsExporter

	// Interval is how often to call the exporter (default: 60 seconds)
	Interval time.Duration

	// ctx controls the lifecycle of the exporter goroutine
	ctx context.Context
}

// Track returns middleware that tracks SLO metrics for each request.
// Records request latency and tracks errors (responses with status >= 500).
// If an exporter is configured, it will be called periodically in a background goroutine.
//
// The exporter goroutine is started when the middleware is created and runs until
// the context (set via WithContext) is canceled. If no context is provided, the
// exporter runs indefinitely. The goroutine is cleaned up when the context is canceled,
// so always use WithContext(ctx) in production to ensure proper shutdown.
//
// Example:
//
//	metrics := slo.NewMetrics(1000)
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	exporter := func(stats slo.Stats) {
//		log.Printf("Availability: %.2f%%, P99: %v", stats.Availability*100, stats.Latency.P99)
//	}
//
//	r.Use(slo.Track(metrics,
//		slo.WithExporter(exporter, 60*time.Second),
//		slo.WithContext(ctx)))
func Track(metrics *Metrics, opts ...TrackOption) func(http.Handler) http.Handler {
	config := TrackConfig{
		Metrics:  metrics,
		Exporter: nil,
		Interval: 60 * time.Second,
		ctx:      context.Background(),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Exporter != nil && config.Interval > 0 {
		go func() {
			ticker := time.NewTicker(config.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-config.ctx.Done():
					return
				case <-ticker.C:
					config.Exporter(config.Metrics.Stats())
				}
			}
		}()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			latency := time.Since(start)
			isError := rw.statusCode >= 500

			config.Metrics.record(latency, isError)
		})
	}
}

// TrackOption configures Track middleware.
type TrackOption func(*TrackConfig)

// WithExporter sets a metrics exporter that is called periodically.
// The exporter function receives the current stats and can export them to
// monitoring systems, write to logs, or perform any other action.
func WithExporter(exporter MetricsExporter, interval time.Duration) TrackOption {
	return func(c *TrackConfig) {
		c.Exporter = exporter
		c.Interval = interval
	}
}

// WithContext sets the context for the exporter goroutine lifecycle.
// When the context is canceled, the exporter goroutine stops. Use this in production
// to ensure proper cleanup during application shutdown.
//
// Example:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel() // Stops the exporter on shutdown
//	r.Use(slo.Track(metrics, slo.WithContext(ctx)))
func WithContext(ctx context.Context) TrackOption {
	return func(c *TrackConfig) {
		c.ctx = ctx
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
