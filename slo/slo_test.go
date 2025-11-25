package slo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/chikit/slo"
)

func TestTrack_Basic(t *testing.T) {
	var capturedMetric slo.Metric
	var callCount int

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
		callCount++
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if callCount != 1 {
		t.Errorf("expected callback called once, got %d", callCount)
	}

	if capturedMetric.Method != "GET" {
		t.Errorf("expected method GET, got %s", capturedMetric.Method)
	}

	if capturedMetric.Route != "/test" {
		t.Errorf("expected route /test, got %s", capturedMetric.Route)
	}

	if capturedMetric.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", capturedMetric.StatusCode)
	}

	if capturedMetric.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", capturedMetric.Duration)
	}
}

func TestTrack_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"400 Bad Request", http.StatusBadRequest},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedMetric slo.Metric

			onMetric := func(_ context.Context, m slo.Metric) {
				capturedMetric = m
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := slo.Track(onMetric)
			tracked := middleware(handler)

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			rec := httptest.NewRecorder()
			tracked.ServeHTTP(rec, req)

			if capturedMetric.StatusCode != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, capturedMetric.StatusCode)
			}
		})
	}
}

func TestTrack_DefaultStatusCode(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if capturedMetric.StatusCode != http.StatusOK {
		t.Errorf("expected default status 200, got %d", capturedMetric.StatusCode)
	}
}

func TestTrack_HTTPMethods(t *testing.T) {
	methods := []string{"GET", "POST", "DELETE"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var capturedMetric slo.Metric

			onMetric := func(_ context.Context, m slo.Metric) {
				capturedMetric = m
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := slo.Track(onMetric)
			tracked := middleware(handler)

			req := httptest.NewRequest(method, "/test", http.NoBody)
			rec := httptest.NewRecorder()
			tracked.ServeHTTP(rec, req)

			if capturedMetric.Method != method {
				t.Errorf("expected method %s, got %s", method, capturedMetric.Method)
			}
		})
	}
}

func TestTrack_LatencyTracking(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if capturedMetric.Duration < 50*time.Millisecond {
		t.Errorf("expected duration >= 50ms, got %v", capturedMetric.Duration)
	}

	if capturedMetric.Duration > 100*time.Millisecond {
		t.Errorf("expected duration < 100ms (with tolerance), got %v", capturedMetric.Duration)
	}
}

func TestTrack_ChiRoutePattern(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	r := chi.NewRouter()
	r.Use(slo.Track(onMetric))
	r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/users/123", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if capturedMetric.Route != "/users/{id}" {
		t.Errorf("expected route pattern /users/{id}, got %s", capturedMetric.Route)
	}
}

func TestTrack_ChiNestedRoutes(t *testing.T) {
	var capturedMetrics []slo.Metric
	var mu sync.Mutex

	onMetric := func(_ context.Context, m slo.Metric) {
		mu.Lock()
		capturedMetrics = append(capturedMetrics, m)
		mu.Unlock()
	}

	r := chi.NewRouter()
	r.Use(slo.Track(onMetric))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/users", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		path            string
		expectedPattern string
	}{
		{"/api/v1/users", "/api/v1/users"},
		{"/api/v1/users/123", "/api/v1/users/{id}"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, http.NoBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	if len(capturedMetrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(capturedMetrics))
	}

	for i, tt := range tests {
		if capturedMetrics[i].Route != tt.expectedPattern {
			t.Errorf("request %d: expected pattern %s, got %s", i, tt.expectedPattern, capturedMetrics[i].Route)
		}
	}
}

func TestTrack_PerRouteMiddleware(t *testing.T) {
	var globalMetrics []slo.Metric
	var apiMetrics []slo.Metric
	var mu sync.Mutex

	onGlobal := func(_ context.Context, m slo.Metric) {
		mu.Lock()
		globalMetrics = append(globalMetrics, m)
		mu.Unlock()
	}

	onAPI := func(_ context.Context, m slo.Metric) {
		mu.Lock()
		apiMetrics = append(apiMetrics, m)
		mu.Unlock()
	}

	r := chi.NewRouter()
	r.Use(slo.Track(onGlobal))

	r.Get("/public", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(slo.Track(onAPI))
		r.Get("/users", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest("GET", "/public", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	req = httptest.NewRequest("GET", "/api/users", http.NoBody)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if len(globalMetrics) != 2 {
		t.Errorf("expected 2 global metrics, got %d", len(globalMetrics))
	}

	if len(apiMetrics) != 1 {
		t.Errorf("expected 1 API metric, got %d", len(apiMetrics))
	}

	if apiMetrics[0].Route != "/api/users" {
		t.Errorf("expected API route /api/users, got %s", apiMetrics[0].Route)
	}
}

func TestTrack_MultipleRequests(t *testing.T) {
	var metrics []slo.Metric
	var mu sync.Mutex

	onMetric := func(_ context.Context, m slo.Metric) {
		mu.Lock()
		metrics = append(metrics, m)
		mu.Unlock()
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		tracked.ServeHTTP(rec, req)
	}

	if len(metrics) != 10 {
		t.Errorf("expected 10 metrics, got %d", len(metrics))
	}
}

func TestTrack_ContextPropagation(t *testing.T) {
	type contextKey string
	const testKey contextKey = "test-key"

	var capturedCtx context.Context

	onMetric := func(ctx context.Context, _ slo.Metric) {
		capturedCtx = ctx
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), testKey, "test-value")
		*r = *r.WithContext(ctx)
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if capturedCtx == nil {
		t.Fatal("expected context to be propagated")
	}

	if val := capturedCtx.Value(testKey); val != "test-value" {
		t.Errorf("expected context value 'test-value', got %v", val)
	}
}

func TestTrack_ConcurrentRequests(t *testing.T) {
	var metrics []slo.Metric
	var mu sync.Mutex

	onMetric := func(_ context.Context, m slo.Metric) {
		mu.Lock()
		metrics = append(metrics, m)
		mu.Unlock()
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	const concurrency = 50
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", http.NoBody)
			rec := httptest.NewRecorder()
			tracked.ServeHTTP(rec, req)
		}()
	}

	wg.Wait()

	if len(metrics) != concurrency {
		t.Errorf("expected %d metrics, got %d", concurrency, len(metrics))
	}

	for i, m := range metrics {
		if m.Duration <= 0 {
			t.Errorf("metric %d: expected positive duration, got %v", i, m.Duration)
		}
		if m.StatusCode != http.StatusOK {
			t.Errorf("metric %d: expected status 200, got %d", i, m.StatusCode)
		}
	}
}

func TestTrack_ErrorClassification(t *testing.T) {
	var successCount, clientErrorCount, serverErrorCount int

	onMetric := func(_ context.Context, m slo.Metric) {
		switch {
		case m.StatusCode >= 500:
			serverErrorCount++
		case m.StatusCode >= 400:
			clientErrorCount++
		default:
			successCount++
		}
	}

	middleware := slo.Track(onMetric)

	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	for _, status := range statuses {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})
		tracked := middleware(handler)

		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		tracked.ServeHTTP(rec, req)
	}

	if successCount != 2 {
		t.Errorf("expected 2 success responses, got %d", successCount)
	}
	if clientErrorCount != 2 {
		t.Errorf("expected 2 client errors, got %d", clientErrorCount)
	}
	if serverErrorCount != 2 {
		t.Errorf("expected 2 server errors, got %d", serverErrorCount)
	}
}

func TestTrack_NoChiContext(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/some/path", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if capturedMetric.Route != "/some/path" {
		t.Errorf("expected fallback to URL path /some/path, got %s", capturedMetric.Route)
	}
}

func TestTrack_Panic(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	})

	middleware := slo.Track(onMetric)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to be re-raised")
		}
	}()

	tracked.ServeHTTP(rec, req)

	if capturedMetric.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500 on panic, got %d", capturedMetric.StatusCode)
	}

	if capturedMetric.Method != "GET" {
		t.Errorf("expected method GET, got %s", capturedMetric.Method)
	}

	if capturedMetric.Route != "/test" {
		t.Errorf("expected route /test, got %s", capturedMetric.Route)
	}

	if capturedMetric.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", capturedMetric.Duration)
	}
}

func TestTrack_PanicWithChiRoute(t *testing.T) {
	var capturedMetric slo.Metric

	onMetric := func(_ context.Context, m slo.Metric) {
		capturedMetric = m
	}

	r := chi.NewRouter()
	r.Use(slo.Track(onMetric))
	r.Get("/users/{id}", func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/users/123", http.NoBody)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to be re-raised")
		}
	}()

	r.ServeHTTP(rec, req)

	if capturedMetric.Route != "/users/{id}" {
		t.Errorf("expected route pattern /users/{id}, got %s", capturedMetric.Route)
	}

	if capturedMetric.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500 on panic, got %d", capturedMetric.StatusCode)
	}
}

func TestTrack_CallbackPanic(t *testing.T) {
	panicCallback := func(_ context.Context, _ slo.Metric) {
		panic("callback panic")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := slo.Track(panicCallback)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate, but it did not")
		} else if r != "callback panic" {
			t.Errorf("expected panic message 'callback panic', got %v", r)
		}
	}()

	tracked.ServeHTTP(rec, req)
}
