// Package ratelimit provides flexible rate limiting middleware for Chi and standard http.Handler.
package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nhalm/chikit/ratelimit/store"
	"github.com/nhalm/chikit/wrapper"
)

// HeaderMode controls when rate limit headers are included in responses.
type HeaderMode int

const (
	HeadersAlways HeaderMode = iota
	HeadersOnLimitExceeded
	HeadersNever
)

// KeyFunc extracts a rate limiting key from an HTTP request.
type KeyFunc func(*http.Request) string

// Limiter implements rate limiting middleware.
type Limiter struct {
	store      store.Store
	limit      int64
	window     time.Duration
	keyFn      KeyFunc
	headerMode HeaderMode
}

// New creates a new rate limiter with the given store, limit, and window.
func New(st store.Store, limit int, window time.Duration, keyFn KeyFunc) *Limiter {
	return &Limiter{
		store:      st,
		limit:      int64(limit),
		window:     window,
		keyFn:      keyFn,
		headerMode: HeadersAlways,
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
			wrapper.SetError(r, wrapper.ErrInternal.With("Rate limit check failed"))
			return
		}

		remaining := max(0, l.limit-count)
		resetTime := time.Now().Add(ttl).Unix()
		exceeded := count > l.limit

		shouldSetHeaders := l.headerMode == HeadersAlways || (l.headerMode == HeadersOnLimitExceeded && exceeded)

		if shouldSetHeaders {
			wrapper.SetHeader(r, "RateLimit-Limit", strconv.FormatInt(l.limit, 10))
			wrapper.SetHeader(r, "RateLimit-Remaining", strconv.FormatInt(remaining, 10))
			wrapper.SetHeader(r, "RateLimit-Reset", strconv.FormatInt(resetTime, 10))
		}

		if exceeded {
			if shouldSetHeaders {
				wrapper.SetHeader(r, "Retry-After", strconv.Itoa(int(ttl.Seconds())))
			}
			wrapper.SetError(r, wrapper.ErrRateLimited)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ByIP creates IP-based rate limiting middleware.
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

// ByHeader creates header-based rate limiting middleware.
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

// ByEndpoint creates endpoint-based rate limiting middleware.
func ByEndpoint(st store.Store, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		return "endpoint:" + r.Method + ":" + r.URL.Path
	})
	return limiter.Handler
}

// ByQueryParam creates query parameter-based rate limiting middleware.
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

// Builder provides a fluent API for building complex multi-dimensional rate limiters.
type Builder struct {
	store      store.Store
	name       string
	limit      int
	window     time.Duration
	keyFns     []KeyFunc
	headerMode HeaderMode
}

// NewBuilder creates a new rate limiter builder.
func NewBuilder(st store.Store) *Builder {
	return &Builder{
		store:      st,
		keyFns:     make([]KeyFunc, 0),
		headerMode: HeadersAlways,
	}
}

// WithName sets an identifier for this rate limiter, prepended to all keys.
func (b *Builder) WithName(name string) *Builder {
	b.name = name
	return b
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

// WithHeaderMode configures when rate limit headers are included in responses.
func (b *Builder) WithHeaderMode(mode HeaderMode) *Builder {
	b.headerMode = mode
	return b
}

// Limit sets the rate limit and window, returning the middleware handler.
func (b *Builder) Limit(limit int, window time.Duration) func(http.Handler) http.Handler {
	b.limit = limit
	b.window = window

	keyFn := func(r *http.Request) string {
		parts := make([]string, 0, len(b.keyFns)+1)

		if b.name != "" {
			parts = append(parts, b.name)
		}

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
	limiter.headerMode = b.headerMode
	return limiter.Handler
}
