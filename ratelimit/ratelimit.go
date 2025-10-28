package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nhalm/chikit/ratelimit/store"
)

// KeyFunc extracts a rate limiting key from an HTTP request.
type KeyFunc func(*http.Request) string

// Limiter implements rate limiting middleware.
type Limiter struct {
	store  store.Store
	limit  int64
	window time.Duration
	keyFn  KeyFunc
}

// New creates a new rate limiter with the given store, limit, and window.
// The keyFn determines what to rate limit by (IP, header, etc.).
func New(st store.Store, limit int, window time.Duration, keyFn KeyFunc) *Limiter {
	return &Limiter{
		store:  st,
		limit:  int64(limit),
		window: window,
		keyFn:  keyFn,
	}
}

// Handler returns the rate limiting middleware.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.keyFn(r)
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		count, ttl, err := l.store.Increment(ctx, key, l.window)
		if err != nil {
			http.Error(w, "Rate limit check failed", http.StatusInternalServerError)
			return
		}

		remaining := max(0, l.limit-count)

		resetTime := time.Now().Add(ttl).Unix()

		w.Header().Set("RateLimit-Limit", strconv.FormatInt(l.limit, 10))
		w.Header().Set("RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		w.Header().Set("RateLimit-Reset", strconv.FormatInt(resetTime, 10))

		if count > l.limit {
			w.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ByIP creates a simple IP-based rate limiter.
func ByIP(st store.Store, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		return "ip:" + ip
	})
	return limiter.Handler
}

// ByHeader creates a simple header-based rate limiter.
func ByHeader(st store.Store, header string, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		val := r.Header.Get(header)
		if val == "" {
			return ""
		}
		return fmt.Sprintf("header:%s:%s", header, val)
	})
	return limiter.Handler
}

// ByEndpoint creates a simple endpoint-based rate limiter.
func ByEndpoint(st store.Store, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		return "endpoint:" + r.Method + ":" + r.URL.Path
	})
	return limiter.Handler
}

// ByQueryParam creates a simple query parameter-based rate limiter.
func ByQueryParam(st store.Store, param string, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		val := r.URL.Query().Get(param)
		if val == "" {
			return ""
		}
		return fmt.Sprintf("query:%s:%s", param, val)
	})
	return limiter.Handler
}

// Builder provides a fluent API for building complex rate limiters.
type Builder struct {
	store  store.Store
	limit  int
	window time.Duration
	keyFns []KeyFunc
}

// NewBuilder creates a new rate limiter builder.
func NewBuilder(st store.Store) *Builder {
	return &Builder{
		store:  st,
		keyFns: make([]KeyFunc, 0),
	}
}

// WithIP adds IP address to the rate limiting key.
func (b *Builder) WithIP() *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return ip
	})
	return b
}

// WithEndpoint adds endpoint (method + path) to the rate limiting key.
func (b *Builder) WithEndpoint() *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		return r.Method + ":" + r.URL.Path
	})
	return b
}

// WithHeader adds a header value to the rate limiting key.
func (b *Builder) WithHeader(header string) *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		return r.Header.Get(header)
	})
	return b
}

// WithQueryParam adds a query parameter value to the rate limiting key.
func (b *Builder) WithQueryParam(param string) *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		return r.URL.Query().Get(param)
	})
	return b
}

// WithCustomKey adds a custom key function to the rate limiting key.
func (b *Builder) WithCustomKey(fn KeyFunc) *Builder {
	b.keyFns = append(b.keyFns, fn)
	return b
}

// Limit sets the rate limit and window, returning the middleware handler.
func (b *Builder) Limit(limit int, window time.Duration) func(http.Handler) http.Handler {
	b.limit = limit
	b.window = window

	keyFn := func(r *http.Request) string {
		parts := make([]string, 0, len(b.keyFns))
		for _, fn := range b.keyFns {
			if part := fn(r); part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, ":")
	}

	limiter := New(b.store, limit, window, keyFn)
	return limiter.Handler
}
