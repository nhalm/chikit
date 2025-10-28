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

func TestConcurrentMultipleKeys(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 10
		concurrency = 50
		numKeys     = 5
	)

	handler := ratelimit.ByIP(st, limit, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var (
		results = make(map[string]*struct {
			allowed atomic.Int64
			denied  atomic.Int64
		})
		mu      sync.Mutex
		wg      sync.WaitGroup
		startCh = make(chan struct{})
	)

	for i := 0; i < numKeys; i++ {
		results[i64ToString(int64(i))] = &struct {
			allowed atomic.Int64
			denied  atomic.Int64
		}{}
	}

	wg.Add(numKeys * concurrency)
	for keyIdx := 0; keyIdx < numKeys; keyIdx++ {
		for reqIdx := 0; reqIdx < concurrency; reqIdx++ {
			keyIdx := keyIdx
			go func() {
				defer wg.Done()

				<-startCh

				req := httptest.NewRequest("GET", "/test", http.NoBody)
				req.RemoteAddr = i64ToString(int64(keyIdx)) + ":1234"
				rr := httptest.NewRecorder()

				handler.ServeHTTP(rr, req)

				mu.Lock()
				result := results[i64ToString(int64(keyIdx))]
				mu.Unlock()

				if rr.Code == http.StatusOK {
					result.allowed.Add(1)
				} else if rr.Code == http.StatusTooManyRequests {
					result.denied.Add(1)
				}
			}()
		}
	}

	close(startCh)
	wg.Wait()

	for key, result := range results {
		allowedCount := result.allowed.Load()
		deniedCount := result.denied.Load()

		if allowedCount != limit {
			t.Errorf("key %s: expected exactly %d allowed requests, got %d", key, limit, allowedCount)
		}

		if deniedCount != concurrency-limit {
			t.Errorf("key %s: expected exactly %d denied requests, got %d", key, concurrency-limit, deniedCount)
		}
	}
}

func TestConcurrentBuilderMultiDimensional(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 20
		concurrency = 50
	)

	handler := ratelimit.NewBuilder(st).
		WithIP().
		WithEndpoint().
		Limit(limit, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []struct {
		name            string
		method          string
		path            string
		ip              string
		expectedAllowed int64
	}{
		{"endpoint1", "GET", "/api/users", "192.168.1.1", limit},
		{"endpoint2", "POST", "/api/users", "192.168.1.1", limit},
		{"endpoint3", "GET", "/api/orders", "192.168.1.1", limit},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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

					req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
					req.RemoteAddr = tc.ip + ":1234"
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

			if allowedCount != tc.expectedAllowed {
				t.Errorf("expected exactly %d allowed requests, got %d", tc.expectedAllowed, allowedCount)
			}

			if deniedCount != concurrency-tc.expectedAllowed {
				t.Errorf("expected exactly %d denied requests, got %d", concurrency-tc.expectedAllowed, deniedCount)
			}
		})
	}
}

func TestConcurrentRaceDetection(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 100
		concurrency = 200
		duration    = 500 * time.Millisecond
	)

	handler := ratelimit.ByIP(st, limit, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var (
		totalRequests atomic.Int64
		stopCh        = make(chan struct{})
		wg            sync.WaitGroup
	)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		i := i
		go func() {
			defer wg.Done()

			for {
				select {
				case <-stopCh:
					return
				default:
					req := httptest.NewRequest("GET", "/test", http.NoBody)
					req.RemoteAddr = i64ToString(int64(i%10)) + ":1234"
					rr := httptest.NewRecorder()

					handler.ServeHTTP(rr, req)
					totalRequests.Add(1)
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stopCh)
	wg.Wait()

	if totalRequests.Load() == 0 {
		t.Error("expected some requests to be processed")
	}
}

func TestConcurrentHeaderBased(t *testing.T) {
	t.Parallel()

	st := store.NewMemory()
	defer st.Close()

	const (
		limit       = 25
		concurrency = 75
	)

	handler := ratelimit.ByHeader(st, "X-API-Key", limit, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	apiKeys := []string{"key-alpha", "key-beta", "key-gamma"}

	for _, apiKey := range apiKeys {
		apiKey := apiKey
		t.Run(apiKey, func(t *testing.T) {
			t.Parallel()

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
					req.Header.Set("X-API-Key", apiKey)
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
		})
	}
}

func i64ToString(n int64) string {
	if n < 0 {
		return "-" + i64ToString(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return i64ToString(n/10) + string(rune('0'+n%10))
}
