// Package slo provides SLO (Service Level Objective) tracking middleware.
package slo

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Metrics contains SLO tracking data.
type Metrics struct {
	mu sync.RWMutex

	totalRequests int64
	errorRequests int64
	latencies     []time.Duration
	maxLatencies  int
}

// NewMetrics creates a new Metrics tracker.
func NewMetrics(maxLatencies int) *Metrics {
	if maxLatencies <= 0 {
		maxLatencies = 1000
	}
	return &Metrics{
		latencies:    make([]time.Duration, 0, maxLatencies),
		maxLatencies: maxLatencies,
	}
}

// Stats returns current SLO statistics.
type Stats struct {
	TotalRequests int64
	ErrorRequests int64
	ErrorRate     float64
	Availability  float64
	Latency       LatencyStats
}

// LatencyStats contains latency percentile data.
type LatencyStats struct {
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
}

// Stats returns a snapshot of current metrics.
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

// Reset clears all metrics.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests = 0
	m.errorRequests = 0
	m.latencies = m.latencies[:0]
}

// MetricsExporter is called periodically with current metrics.
type MetricsExporter func(Stats)

// TrackConfig configures the Track middleware.
type TrackConfig struct {
	Metrics  *Metrics
	Exporter MetricsExporter
	Interval time.Duration
	ctx      context.Context
}

// Track returns middleware that tracks SLO metrics.
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
func WithExporter(exporter MetricsExporter, interval time.Duration) TrackOption {
	return func(c *TrackConfig) {
		c.Exporter = exporter
		c.Interval = interval
	}
}

// WithContext sets the context for the exporter goroutine.
// When the context is canceled, the exporter stops.
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
