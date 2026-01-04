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

func TestWithIP(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute, ratelimit.WithIP())
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	if retry := rr.Header().Get("Retry-After"); retry == "" {
		t.Error("expected Retry-After header")
	}
}

func TestWithRealIP(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute, ratelimit.WithRealIP(false))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

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
	})

	t.Run("X-Real-IP", func(t *testing.T) {
		st2 := store.NewMemory()
		defer st2.Close()

		limiter2 := ratelimit.New(st2, 2, time.Minute, ratelimit.WithRealIP(false))
		handler2 := limiter2.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", http.NoBody)
		req.Header.Set("X-Real-IP", "10.0.0.3")

		for i := 0; i < 2; i++ {
			rr := httptest.NewRecorder()
			handler2.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
			}
		}

		rr := httptest.NewRecorder()
		handler2.ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429, got %d", rr.Code)
		}
	})

	t.Run("no_header_skips_ratelimit", func(t *testing.T) {
		st3 := store.NewMemory()
		defer st3.Close()

		limiter3 := ratelimit.New(st3, 1, time.Minute, ratelimit.WithRealIP(false))
		handler3 := limiter3.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("GET", "/test", http.NoBody)
			rr := httptest.NewRecorder()
			handler3.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("request %d: expected 200 (skipped), got %d", i+1, rr.Code)
			}
		}
	})
}

func TestWithRealIP_Required(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithRealIP(true))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request without X-Forwarded-For or X-Real-IP should return 400
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// Request with header should succeed
	req = httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWithHeader(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 3, time.Minute, ratelimit.WithHeader("X-API-Key", false))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("X-API-Key", "test-key")

	for i := 0; i < 3; i++ {
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
}

func TestWithHeader_Required(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithHeader("X-API-Key", true))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request without header should return 400
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if body := rr.Body.String(); body != "Missing required header X-API-Key\n" {
		t.Errorf("unexpected error message: %q", body)
	}

	// Request with header should succeed
	req = httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("X-API-Key", "test-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWithHeader_Required_WithWrapper(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithHeader("X-API-Key", true))
	handler := wrapper.New()(limiter.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	// Request without header should return 400 with JSON error
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"].Code != "bad_request" {
		t.Errorf("expected code 'bad_request', got %s", resp["error"].Code)
	}
}

func TestWithEndpoint(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute, ratelimit.WithEndpoint())
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", http.NoBody)

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
}

func TestWithQueryParam(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 3, time.Minute, ratelimit.WithQueryParam("api_key", false))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?api_key=test-key-123", http.NoBody)

	for i := 0; i < 3; i++ {
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
}

func TestWithQueryParam_Required(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithQueryParam("api_key", true))
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request without query param should return 400
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// Request with query param should succeed
	req = httptest.NewRequest("GET", "/test?api_key=test-key", http.NoBody)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestMultiDimensional(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute,
		ratelimit.WithIP(),
		ratelimit.WithEndpoint(),
	)
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestRateLimitHeaders(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 5, time.Minute, ratelimit.WithIP())
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

			limiter := ratelimit.New(st, 2, time.Minute,
				ratelimit.WithIP(),
				ratelimit.WithHeaderMode(tt.mode),
			)
			handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestWithName(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute,
		ratelimit.WithName("global"),
		ratelimit.WithIP(),
	)
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestWithName_MultiDimension(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 2, time.Minute,
		ratelimit.WithName("api"),
		ratelimit.WithIP(),
		ratelimit.WithHeader("X-Tenant-ID", false),
	)
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestLayeredLimiters(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	globalLimiter := ratelimit.New(st, 5, time.Minute,
		ratelimit.WithName("global"),
		ratelimit.WithIP(),
	)

	endpointLimiter := ratelimit.New(st, 2, time.Minute,
		ratelimit.WithName("endpoint"),
		ratelimit.WithIP(),
		ratelimit.WithEndpoint(),
	)

	handler := globalLimiter.Handler(endpointLimiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestConcurrentSameKey(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 50
		concurrency = 100
	)

	limiter := ratelimit.New(st, limit, time.Minute, ratelimit.WithIP())
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	limiter := ratelimit.New(st, 10, time.Minute, ratelimit.WithIP())
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestNoKeyDimensions_Panics(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when no key dimensions provided")
		}
	}()

	ratelimit.New(st, 1, time.Minute) // Should panic
}

func TestWithRealIP_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		xff         string
		realIP      string
		expectedKey string
	}{
		{
			name:        "xff_single_ip",
			xff:         "10.0.0.1",
			expectedKey: "10.0.0.1",
		},
		{
			name:        "xff_multiple_ips",
			xff:         "10.0.0.1, 10.0.0.2, 10.0.0.3",
			expectedKey: "10.0.0.1",
		},
		{
			name:        "xff_with_spaces",
			xff:         "  10.0.0.1  ,  10.0.0.2  ",
			expectedKey: "10.0.0.1",
		},
		{
			name:        "real_ip_fallback",
			realIP:      "10.0.0.5",
			expectedKey: "10.0.0.5",
		},
		{
			name:        "real_ip_with_spaces",
			realIP:      "  10.0.0.5  ",
			expectedKey: "10.0.0.5",
		},
		{
			name:        "xff_takes_precedence",
			xff:         "10.0.0.1",
			realIP:      "10.0.0.5",
			expectedKey: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := store.NewMemory()
			defer st.Close()

			limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithRealIP(false))
			handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rr.Code)
			}

			count, err := st.Get(context.Background(), tt.expectedKey)
			if err != nil {
				t.Fatalf("failed to get key: %v", err)
			}
			if count != 1 {
				t.Errorf("expected count 1 for key %q, got %d", tt.expectedKey, count)
			}
		})
	}
}

func TestWithWrapper_RateLimited(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	limiter := ratelimit.New(st, 1, time.Minute, ratelimit.WithIP())

	handler := wrapper.New()(limiter.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:1234"

	// First request - should succeed
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rr.Code)
	}

	// Second request - should be rate limited with wrapper error format
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rr.Code)
	}

	// Verify JSON error response format
	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"].Type != "rate_limit_error" {
		t.Errorf("expected error type 'rate_limit_error', got %s", resp["error"].Type)
	}

	// Verify rate limit headers are set
	if rr.Header().Get("RateLimit-Limit") != "1" {
		t.Errorf("expected RateLimit-Limit header")
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header")
	}
}
