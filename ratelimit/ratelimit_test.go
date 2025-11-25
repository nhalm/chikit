package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nhalm/chikit/ratelimit"
	"github.com/nhalm/chikit/ratelimit/store"
)

func TestByIP(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByIP(st, 2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestByHeader(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByHeader(st, "X-API-Key", 3, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestByEndpoint(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByEndpoint(st, 2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/users", http.NoBody)

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

func TestByQueryParam(t *testing.T) {
	st := store.NewMemory()
	defer st.Close()

	handler := ratelimit.ByQueryParam(st, "api_key", 3, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
