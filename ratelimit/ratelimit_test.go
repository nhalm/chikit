package ratelimit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nhalm/chikit/ratelimit"
	"github.com/nhalm/chikit/ratelimit/store"
	"github.com/nhalm/chikit/wrapper"
)

func TestSimpleAPI(t *testing.T) {
	tests := []struct {
		name         string
		middleware   func(http.Handler) http.Handler
		setupRequest func(*http.Request)
		limit        int
	}{
		{
			name: "ByIP",
			middleware: func(h http.Handler) http.Handler {
				st := store.NewMemory()
				t.Cleanup(func() { st.Close() })
				return ratelimit.ByIP(st, 2, time.Minute)(h)
			},
			setupRequest: func(r *http.Request) {
				r.RemoteAddr = "192.168.1.1:1234"
			},
			limit: 2,
		},
		{
			name: "ByHeader",
			middleware: func(h http.Handler) http.Handler {
				st := store.NewMemory()
				t.Cleanup(func() { st.Close() })
				return ratelimit.ByHeader(st, "X-API-Key", 3, time.Minute)(h)
			},
			setupRequest: func(r *http.Request) {
				r.Header.Set("X-API-Key", "test-key")
			},
			limit: 3,
		},
		{
			name: "ByEndpoint",
			middleware: func(h http.Handler) http.Handler {
				st := store.NewMemory()
				t.Cleanup(func() { st.Close() })
				return ratelimit.ByEndpoint(st, 2, time.Minute)(h)
			},
			setupRequest: func(_ *http.Request) {},
			limit:        2,
		},
		{
			name: "ByQueryParam",
			middleware: func(h http.Handler) http.Handler {
				st := store.NewMemory()
				t.Cleanup(func() { st.Close() })
				return ratelimit.ByQueryParam(st, "api_key", 3, time.Minute)(h)
			},
			setupRequest: func(r *http.Request) {
				r.URL.RawQuery = "api_key=test-key-123"
			},
			limit: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			tt.setupRequest(req)

			for i := 0; i < tt.limit; i++ {
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
				}
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusTooManyRequests {
				t.Errorf("expected 429, got %d", rr.Code)
			}

			if retry := rr.Header().Get("Retry-After"); retry == "" {
				t.Error("expected Retry-After header")
			}
		})
	}
}

func TestByHeaderMissing(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByHeader(st, "X-API-Key", 1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected request without header to pass, got %d", rr.Code)
	}
}

func TestByQueryParamMissing(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByQueryParam(st, "api_key", 1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected request without query param to pass, got %d", rr.Code)
	}
}

func TestBuilderMultiDimensional(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithIP().
		WithEndpoint().
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("POST", "/api/v1/users", http.NoBody)
	req1.RemoteAddr = "192.168.1.1:1234"

	req2 := httptest.NewRequest("GET", "/api/v1/users", http.NoBody)
	req2.RemoteAddr = "192.168.1.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req1)
		if rr.Code != http.StatusOK {
			t.Errorf("POST request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req1)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("POST: expected 429, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req2)
	if rr.Code != http.StatusOK {
		t.Error("GET request should not be rate limited (different endpoint)")
	}
}

func TestBuilderWithHeader(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithIP().
		WithHeader("X-Tenant-ID").
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "/test", http.NoBody)
	req1.RemoteAddr = "192.168.1.1:1234"
	req1.Header.Set("X-Tenant-ID", "tenant-a")

	req2 := httptest.NewRequest("GET", "/test", http.NoBody)
	req2.RemoteAddr = "192.168.1.1:1234"
	req2.Header.Set("X-Tenant-ID", "tenant-b")

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req1)
		if rr.Code != http.StatusOK {
			t.Errorf("tenant-a request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req1)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("tenant-a: expected 429, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req2)
	if rr.Code != http.StatusOK {
		t.Error("tenant-b should not be rate limited (different tenant)")
	}
}

func TestBuilderWithQueryParam(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithIP().
		WithQueryParam("tenant_id").
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "/test?tenant_id=tenant-a", http.NoBody)
	req1.RemoteAddr = "192.168.1.1:1234"

	req2 := httptest.NewRequest("GET", "/test?tenant_id=tenant-b", http.NoBody)
	req2.RemoteAddr = "192.168.1.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req1)
		if rr.Code != http.StatusOK {
			t.Errorf("tenant-a request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req1)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("tenant-a: expected 429, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req2)
	if rr.Code != http.StatusOK {
		t.Error("tenant-b should not be rate limited (different tenant)")
	}
}

func TestBuilderWithCustomKey(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithIP().
		WithCustomKey(func(r *http.Request) string {
			if userID := r.Header.Get("X-User-ID"); userID != "" {
				return "user:" + userID
			}
			return ""
		}).
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "/test", http.NoBody)
	req1.RemoteAddr = "192.168.1.1:1234"
	req1.Header.Set("X-User-ID", "user-123")

	req2 := httptest.NewRequest("GET", "/test", http.NoBody)
	req2.RemoteAddr = "192.168.1.1:1234"
	req2.Header.Set("X-User-ID", "user-456")

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req1)
		if rr.Code != http.StatusOK {
			t.Errorf("user-123 request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req1)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("user-123: expected 429, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req2)
	if rr.Code != http.StatusOK {
		t.Error("user-456 should not be rate limited (different user)")
	}
}

func TestRateLimitHeaders(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByIP(st, 5, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if limit := rr.Header().Get("RateLimit-Limit"); limit != "5" {
		t.Errorf("expected RateLimit-Limit: 5, got %s", limit)
	}

	if remaining := rr.Header().Get("RateLimit-Remaining"); remaining != "4" {
		t.Errorf("expected RateLimit-Remaining: 4, got %s", remaining)
	}

	if reset := rr.Header().Get("RateLimit-Reset"); reset == "" {
		t.Error("expected RateLimit-Reset header")
	}
}

func TestHeaderModes(t *testing.T) {
	tests := []struct {
		name                  string
		mode                  ratelimit.HeaderMode
		wantHeadersOnSuccess  bool
		wantHeadersOnExceeded bool
	}{
		{
			name:                  "HeadersAlways",
			mode:                  ratelimit.HeadersAlways,
			wantHeadersOnSuccess:  true,
			wantHeadersOnExceeded: true,
		},
		{
			name:                  "HeadersOnLimitExceeded",
			mode:                  ratelimit.HeadersOnLimitExceeded,
			wantHeadersOnSuccess:  false,
			wantHeadersOnExceeded: true,
		},
		{
			name:                  "HeadersNever",
			mode:                  ratelimit.HeadersNever,
			wantHeadersOnSuccess:  false,
			wantHeadersOnExceeded: false,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := store.NewMemory()
			defer st.Close()

			handler := ratelimit.NewBuilder(st).
				WithIP().
				WithHeaderMode(tt.mode).
				Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			req.RemoteAddr = fmt.Sprintf("192.168.1.%d:1234", 100+i)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}

			hasHeaders := rr.Header().Get("RateLimit-Limit") != ""
			if hasHeaders != tt.wantHeadersOnSuccess {
				t.Errorf("success response: headers present = %v, want %v", hasHeaders, tt.wantHeadersOnSuccess)
			}

			rr = httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			rr = httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusTooManyRequests {
				t.Fatalf("expected 429, got %d", rr.Code)
			}

			hasHeaders = rr.Header().Get("RateLimit-Limit") != ""
			if hasHeaders != tt.wantHeadersOnExceeded {
				t.Errorf("exceeded response: headers present = %v, want %v", hasHeaders, tt.wantHeadersOnExceeded)
			}
		})
	}
}

func TestBuilder_WithName(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithName("global").
		WithIP().
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	count, err := st.Get(context.Background(), "global:192.168.1.1")
	if err != nil {
		t.Fatalf("failed to get key: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3 for key 'global:192.168.1.1', got %d", count)
	}
}

func TestBuilder_WithName_MultiDimension(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithName("api").
		WithIP().
		WithHeader("X-Tenant-ID").
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"
	req.Header.Set("X-Tenant-ID", "tenant-123")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	count, err := st.Get(context.Background(), "api:192.168.1.1:tenant-123")
	if err != nil {
		t.Fatalf("failed to get key: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 for key 'api:192.168.1.1:tenant-123', got %d", count)
	}
}

func TestBuilder_LayeredLimiters(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	globalLimiter := ratelimit.NewBuilder(st).
		WithName("global").
		WithIP().
		Limit(5, time.Minute)

	endpointLimiter := ratelimit.NewBuilder(st).
		WithName("endpoint").
		WithIP().
		WithEndpoint().
		Limit(2, time.Minute)

	handler := globalLimiter(endpointLimiter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/api/users", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 from endpoint limiter, got %d", rr.Code)
	}

	globalCount, err := st.Get(context.Background(), "global:192.168.1.1")
	if err != nil {
		t.Fatalf("failed to get global key: %v", err)
	}
	if globalCount != 3 {
		t.Errorf("expected global count 3, got %d", globalCount)
	}

	endpointCount, err := st.Get(context.Background(), "endpoint:192.168.1.1:GET:/api/users")
	if err != nil {
		t.Fatalf("failed to get endpoint key: %v", err)
	}
	if endpointCount != 3 {
		t.Errorf("expected endpoint count 3, got %d", endpointCount)
	}
}

func TestBuilder_WithName_Empty(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.NewBuilder(st).
		WithName("").
		WithIP().
		Limit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	count, err := st.Get(context.Background(), "192.168.1.1")
	if err != nil {
		t.Fatalf("failed to get key: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 for key '192.168.1.1', got %d", count)
	}
}

func TestConcurrentSameKey(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 50
		concurrency = 100
	)

	handler := ratelimit.ByIP(st, limit, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var (
		allowed atomic.Int64
		denied  atomic.Int64
		wg      sync.WaitGroup
		startCh = make(chan struct{})
	)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()

			<-startCh

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			req.RemoteAddr = "192.168.1.1:1234"
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code == http.StatusOK {
				allowed.Add(1)
			} else if rr.Code == http.StatusTooManyRequests {
				denied.Add(1)
			}
		}()
	}

	close(startCh)
	wg.Wait()

	allowedCount := allowed.Load()
	deniedCount := denied.Load()

	if allowedCount != limit {
		t.Errorf("expected exactly %d allowed requests, got %d", limit, allowedCount)
	}

	if deniedCount != concurrency-limit {
		t.Errorf("expected exactly %d denied requests, got %d", concurrency-limit, deniedCount)
	}

	if allowedCount+deniedCount != concurrency {
		t.Errorf("total requests should be %d, got %d", concurrency, allowedCount+deniedCount)
	}
}

type errorStore struct{}

func (e *errorStore) Increment(_ context.Context, _ string, _ time.Duration) (int64, time.Duration, error) {
	return 0, 0, errors.New("storage backend unavailable")
}

func (e *errorStore) Get(_ context.Context, _ string) (int64, error) {
	return 0, errors.New("storage backend unavailable")
}

func (e *errorStore) Reset(_ context.Context, _ string) error {
	return errors.New("storage backend unavailable")
}

func (e *errorStore) Close() error {
	return nil
}

func TestRateLimit_StoreError(t *testing.T) {
	st := &errorStore{}

	handler := ratelimit.ByIP(st, 10, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	body := rr.Body.String()
	if body != "Rate limit check failed\n" {
		t.Errorf("expected error message 'Rate limit check failed', got %q", body)
	}
}

func TestRateLimit_WithWrapper_Exceeded(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	chain := wrapper.Handler()(ratelimit.ByIP(st, 2, time.Minute)(handler))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		chain.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "rate_limit_error" {
		t.Errorf("expected error type rate_limit_error, got %s", resp["error"].Type)
	}
	if resp["error"].Code != "limit_exceeded" {
		t.Errorf("expected code limit_exceeded, got %s", resp["error"].Code)
	}
}

func TestRateLimit_WithWrapper_Headers(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})

	chain := wrapper.Handler()(ratelimit.ByIP(st, 5, time.Minute)(handler))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()

	chain.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	if limit := rr.Header().Get("RateLimit-Limit"); limit != "5" {
		t.Errorf("expected RateLimit-Limit: 5, got %s", limit)
	}

	if remaining := rr.Header().Get("RateLimit-Remaining"); remaining != "4" {
		t.Errorf("expected RateLimit-Remaining: 4, got %s", remaining)
	}

	if reset := rr.Header().Get("RateLimit-Reset"); reset == "" {
		t.Error("expected RateLimit-Reset header")
	}
}

func TestRateLimit_WithWrapper_StoreError(t *testing.T) {
	st := &errorStore{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	chain := wrapper.Handler()(ratelimit.ByIP(st, 10, time.Minute)(handler))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()

	chain.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "internal_error" {
		t.Errorf("expected error type internal_error, got %s", resp["error"].Type)
	}
	if resp["error"].Message != "Rate limit check failed" {
		t.Errorf("expected message 'Rate limit check failed', got %s", resp["error"].Message)
	}
}
