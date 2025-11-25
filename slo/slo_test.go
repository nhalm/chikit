package slo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nhalm/chikit/slo"
)

func TestTrack_RequestCounting(t *testing.T) {
	metrics := slo.NewMetrics(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(metrics)
	tracked := middleware(handler)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", http.NoBody)
		rec := httptest.NewRecorder()
		tracked.ServeHTTP(rec, req)
	}

	stats := metrics.Stats()
	if stats.TotalRequests != 10 {
		t.Errorf("expected 10 total requests, got %d", stats.TotalRequests)
	}
	if stats.ErrorRequests != 0 {
		t.Errorf("expected 0 error requests, got %d", stats.ErrorRequests)
	}
}

func TestTrack_ErrorCounting(t *testing.T) {
	metrics := slo.NewMetrics(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := slo.Track(metrics)
	tracked := middleware(handler)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", http.NoBody)
		rec := httptest.NewRecorder()
		tracked.ServeHTTP(rec, req)
	}

	stats := metrics.Stats()
	if stats.TotalRequests != 5 {
		t.Errorf("expected 5 total requests, got %d", stats.TotalRequests)
	}
	if stats.ErrorRequests != 5 {
		t.Errorf("expected 5 error requests, got %d", stats.ErrorRequests)
	}
	if stats.ErrorRate != 1.0 {
		t.Errorf("expected error rate 1.0, got %.2f", stats.ErrorRate)
	}
}

func TestTrack_MixedRequests(t *testing.T) {
	metrics := slo.NewMetrics(100)

	successHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	errorHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := slo.Track(metrics)

	for i := 0; i < 8; i++ {
		req := httptest.NewRequest("GET", "/", http.NoBody)
		rec := httptest.NewRecorder()
		middleware(successHandler).ServeHTTP(rec, req)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", http.NoBody)
		rec := httptest.NewRecorder()
		middleware(errorHandler).ServeHTTP(rec, req)
	}

	stats := metrics.Stats()
	if stats.TotalRequests != 10 {
		t.Errorf("expected 10 total requests, got %d", stats.TotalRequests)
	}
	if stats.ErrorRequests != 2 {
		t.Errorf("expected 2 error requests, got %d", stats.ErrorRequests)
	}
	if stats.ErrorRate != 0.2 {
		t.Errorf("expected error rate 0.2, got %.2f", stats.ErrorRate)
	}
	if stats.Availability != 0.8 {
		t.Errorf("expected availability 0.8, got %.2f", stats.Availability)
	}
}

func TestTrack_LatencyTracking(t *testing.T) {
	metrics := slo.NewMetrics(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(metrics)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	stats := metrics.Stats()
	if stats.Latency.P50 < 10*time.Millisecond {
		t.Errorf("expected P50 latency >= 10ms, got %v", stats.Latency.P50)
	}
}

func TestMetrics_Reset(t *testing.T) {
	metrics := slo.NewMetrics(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(metrics)
	tracked := middleware(handler)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", http.NoBody)
		rec := httptest.NewRecorder()
		tracked.ServeHTTP(rec, req)
	}

	metrics.Reset()

	stats := metrics.Stats()
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", stats.TotalRequests)
	}
	if stats.ErrorRequests != 0 {
		t.Errorf("expected 0 error requests after reset, got %d", stats.ErrorRequests)
	}
}

func TestTrack_ContextCancellation(t *testing.T) {
	metrics := slo.NewMetrics(100)

	var exportCalled atomic.Int64
	exporter := func(_ slo.Stats) {
		exportCalled.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(
		metrics,
		slo.WithExporter(exporter, 50*time.Millisecond),
		slo.WithContext(ctx),
	)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	time.Sleep(120 * time.Millisecond)

	beforeCancel := exportCalled.Load()
	if beforeCancel < 1 {
		t.Fatalf("expected at least 1 export before cancel, got %d", beforeCancel)
	}

	cancel()
	time.Sleep(120 * time.Millisecond)

	afterCancel := exportCalled.Load()
	if afterCancel != beforeCancel {
		t.Errorf("expected no more exports after cancel, before=%d after=%d", beforeCancel, afterCancel)
	}
}
