// Package ratelimit provides flexible rate limiting middleware for Chi and standard http.Handler.
//
// The package offers two API styles: a simple API for common use cases (ByIP, ByHeader, etc.)
// and a fluent Builder API for complex multi-dimensional rate limiting scenarios. All middleware
// sets standard rate limit headers (RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset) and
// returns 429 (Too Many Requests) when limits are exceeded.
//
// Simple API example:
//
//	store := store.NewMemory()
//	defer store.Close()
//	r.Use(ratelimit.ByIP(store, 100, time.Minute))
//
// Advanced multi-dimensional example:
//
//	limiter := ratelimit.NewBuilder(store).
//		WithIP().
//		WithHeader("X-Tenant-ID").
//		Limit(100, time.Minute)
//	r.Use(limiter)
//
// Rate limiting is automatically skipped when the key function returns an empty string.
// For distributed deployments (Kubernetes), use the Redis store. The in-memory store
// is only suitable for single-instance deployments and development.
package ratelimit

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nhalm/chikit/ratelimit/store"
	"github.com/nhalm/chikit/wrapper"
)

// HeaderMode controls when rate limit headers are included in responses.
// This configuration is only available through the Builder API.
type HeaderMode int

const (
	// HeadersAlways includes rate limit headers on all responses (default).
	// Headers: RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset
	// On 429: Also includes Retry-After
	HeadersAlways HeaderMode = iota

	// HeadersOnLimitExceeded includes rate limit headers only on 429 responses.
	// Headers on 429: RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, Retry-After
	HeadersOnLimitExceeded

	// HeadersNever never includes rate limit headers in any response.
	// Use this when you want rate limiting without exposing limits to clients.
	HeadersNever
)

// KeyFunc extracts a rate limiting key from an HTTP request.
// Returning an empty string skips rate limiting for that request.
type KeyFunc func(*http.Request) string

// Limiter implements rate limiting middleware.
type Limiter struct {
	store      store.Store
	limit      int64
	window     time.Duration
	keyFn      KeyFunc
	headerMode HeaderMode
}

// Option configures a Limiter.
type Option func(*Limiter)

// WithHeaderMode configures when rate limit headers are included in responses.
func WithHeaderMode(mode HeaderMode) Option {
	return func(l *Limiter) {
		l.headerMode = mode
	}
}

// New creates a new rate limiter with the given store, limit, and window.
// The keyFn determines what to rate limit by (IP, header, etc.).
// Returns 429 (Too Many Requests) when the limit is exceeded, with standard
// rate limit headers and a Retry-After header indicating seconds until reset.
// Returns 500 (Internal Server Error) if the store operation fails.
//
// If keyFn returns an empty string, rate limiting is skipped for that request.
//
// Options:
//   - WithHeaderMode: Configure header visibility (default: HeadersAlways)
func New(st store.Store, limit int, window time.Duration, keyFn KeyFunc, opts ...Option) *Limiter {
	l := &Limiter{
		store:      st,
		limit:      int64(limit),
		window:     window,
		keyFn:      keyFn,
		headerMode: HeadersAlways,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Handler returns the rate limiting middleware.
// Sets the following headers based on header mode:
//   - RateLimit-Limit: The rate limit ceiling for the current window
//   - RateLimit-Remaining: Number of requests remaining in the current window
//   - RateLimit-Reset: Unix timestamp when the current window resets
//   - Retry-After: (only when limited) Seconds until the window resets
//
// These headers follow the IETF draft-ietf-httpapi-ratelimit-headers specification.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.keyFn(r)
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		useWrapper := wrapper.HasState(ctx)

		count, ttl, err := l.store.Increment(ctx, key, l.window)
		if err != nil {
			if useWrapper {
				wrapper.SetError(r, wrapper.ErrInternal.With("Rate limit check failed"))
			} else {
				http.Error(w, "Rate limit check failed", http.StatusInternalServerError)
			}
			return
		}

		remaining := max(0, l.limit-count)
		resetTime := time.Now().Add(ttl).Unix()
		exceeded := count > l.limit

		shouldSetHeaders := l.headerMode == HeadersAlways || (l.headerMode == HeadersOnLimitExceeded && exceeded)

		if shouldSetHeaders {
			if useWrapper {
				wrapper.SetHeader(r, "RateLimit-Limit", strconv.FormatInt(l.limit, 10))
				wrapper.SetHeader(r, "RateLimit-Remaining", strconv.FormatInt(remaining, 10))
				wrapper.SetHeader(r, "RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			} else {
				w.Header().Set("RateLimit-Limit", strconv.FormatInt(l.limit, 10))
				w.Header().Set("RateLimit-Remaining", strconv.FormatInt(remaining, 10))
				w.Header().Set("RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			}
		}

		if exceeded {
			if shouldSetHeaders {
				if useWrapper {
					wrapper.SetHeader(r, "Retry-After", strconv.Itoa(int(ttl.Seconds())))
				} else {
					w.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
				}
			}
			if useWrapper {
				wrapper.SetError(r, wrapper.ErrRateLimited)
			} else {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ByIP creates IP-based rate limiting middleware.
// Uses the RemoteAddr from the request to determine the client IP.
// The generated key format is "ip:<address>".
//
// Example:
//
//	store := store.NewMemory()
//	r.Use(ratelimit.ByIP(store, 100, time.Minute)) // 100 requests per minute per IP
func ByIP(st store.Store, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		var b strings.Builder
		b.Grow(3 + len(ip))
		b.WriteString("ip:")
		b.WriteString(ip)
		return b.String()
	})
	return limiter.Handler
}

// ByHeader creates header-based rate limiting middleware.
// If the specified header is missing, rate limiting is skipped for that request.
// The generated key format is "header:<name>:<value>".
//
// Example:
//
//	r.Use(ratelimit.ByHeader(store, "X-API-Key", 1000, time.Hour))
func ByHeader(st store.Store, header string, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		val := r.Header.Get(header)
		if val == "" {
			return ""
		}
		var b strings.Builder
		b.Grow(7 + len(header) + 1 + len(val))
		b.WriteString("header:")
		b.WriteString(header)
		b.WriteByte(':')
		b.WriteString(val)
		return b.String()
	})
	return limiter.Handler
}

// ByEndpoint creates endpoint-based rate limiting middleware.
// Combines the HTTP method and URL path to create a unique key per endpoint.
// The generated key format is "endpoint:<method>:<path>".
//
// Example:
//
//	r.Use(ratelimit.ByEndpoint(store, 50, time.Minute)) // 50 req/min per endpoint
func ByEndpoint(st store.Store, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		var b strings.Builder
		b.Grow(9 + len(r.Method) + 1 + len(r.URL.Path))
		b.WriteString("endpoint:")
		b.WriteString(r.Method)
		b.WriteByte(':')
		b.WriteString(r.URL.Path)
		return b.String()
	})
	return limiter.Handler
}

// ByQueryParam creates query parameter-based rate limiting middleware.
// If the specified query parameter is missing, rate limiting is skipped for that request.
// The generated key format is "query:<param>:<value>".
//
// Example:
//
//	r.Use(ratelimit.ByQueryParam(store, "api_key", 500, time.Hour))
func ByQueryParam(st store.Store, param string, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := New(st, limit, window, func(r *http.Request) string {
		val := r.URL.Query().Get(param)
		if val == "" {
			return ""
		}
		var b strings.Builder
		b.Grow(6 + len(param) + 1 + len(val))
		b.WriteString("query:")
		b.WriteString(param)
		b.WriteByte(':')
		b.WriteString(val)
		return b.String()
	})
	return limiter.Handler
}

// Builder provides a fluent API for building complex multi-dimensional rate limiters.
// Each With* method adds a dimension to the rate limiting key. All dimensions are
// combined with ":" as a separator. If any dimension returns an empty string,
// rate limiting is skipped for that request.
//
// Example:
//
//	limiter := ratelimit.NewBuilder(store).
//		WithIP().
//		WithHeader("X-Tenant-ID").
//		WithEndpoint().
//		Limit(100, time.Minute)
//	r.Use(limiter)
//
// This creates a composite key like "192.168.1.1:tenant-abc:GET:/api/users"
// allowing fine-grained rate limiting per IP, tenant, and endpoint combination.
type Builder struct {
	store      store.Store
	name       string
	limit      int
	window     time.Duration
	keyFns     []KeyFunc
	headerMode HeaderMode
}

// NewBuilder creates a new rate limiter builder.
// Use With* methods to add dimensions, then call Limit to build the middleware.
func NewBuilder(st store.Store) *Builder {
	return &Builder{
		store:      st,
		keyFns:     make([]KeyFunc, 0),
		headerMode: HeadersAlways,
	}
}

// WithName sets an identifier for this rate limiter, prepended to all keys.
// Use to prevent key collisions when layering multiple rate limiters.
func (b *Builder) WithName(name string) *Builder {
	b.name = name
	return b
}

// WithIP adds IP address to the rate limiting key.
// Extracts the IP from RemoteAddr, removing the port if present.
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
// The key component format is "<method>:<path>".
func (b *Builder) WithEndpoint() *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		var sb strings.Builder
		sb.Grow(len(r.Method) + 1 + len(r.URL.Path))
		sb.WriteString(r.Method)
		sb.WriteByte(':')
		sb.WriteString(r.URL.Path)
		return sb.String()
	})
	return b
}

// WithHeader adds a header value to the rate limiting key.
// If the header is missing, returns an empty string which causes
// rate limiting to be skipped for that request.
func (b *Builder) WithHeader(header string) *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		return r.Header.Get(header)
	})
	return b
}

// WithQueryParam adds a query parameter value to the rate limiting key.
// If the parameter is missing, returns an empty string which causes
// rate limiting to be skipped for that request.
func (b *Builder) WithQueryParam(param string) *Builder {
	b.keyFns = append(b.keyFns, func(r *http.Request) string {
		return r.URL.Query().Get(param)
	})
	return b
}

// WithCustomKey adds a custom key function to the rate limiting key.
// The function should return an empty string to skip rate limiting for a request.
// Useful for custom extraction logic or computed values.
func (b *Builder) WithCustomKey(fn KeyFunc) *Builder {
	b.keyFns = append(b.keyFns, fn)
	return b
}

// WithHeaderMode configures when rate limit headers are included in responses.
// This follows the IETF draft-ietf-httpapi-ratelimit-headers specification.
//
// Available modes:
//   - HeadersAlways: Include headers on all responses (default)
//   - HeadersOnLimitExceeded: Include headers only on 429 responses
//   - HeadersNever: Never include headers
//
// Example:
//
//	limiter := ratelimit.NewBuilder(store).
//		WithIP().
//		WithHeaderMode(ratelimit.HeadersOnLimitExceeded).
//		Limit(100, time.Minute)
func (b *Builder) WithHeaderMode(mode HeaderMode) *Builder {
	b.headerMode = mode
	return b
}

// Limit sets the rate limit and window, returning the middleware handler.
// All configured dimensions are combined into a single key using ":" as a separator.
// If any dimension returns an empty string, rate limiting is skipped for that request.
//
// Parameters:
//   - limit: Maximum number of requests allowed in the time window
//   - window: Duration of the time window (e.g., time.Minute, time.Hour)
func (b *Builder) Limit(limit int, window time.Duration) func(http.Handler) http.Handler {
	b.limit = limit
	b.window = window

	keyFn := func(r *http.Request) string {
		var sb strings.Builder
		sb.Grow(20 + len(b.keyFns)*30)
		hasContent := false

		if b.name != "" {
			sb.WriteString(b.name)
			hasContent = true
		}

		for _, fn := range b.keyFns {
			if part := fn(r); part != "" {
				if hasContent {
					sb.WriteByte(':')
				}
				sb.WriteString(part)
				hasContent = true
			}
		}

		if !hasContent {
			return ""
		}
		return sb.String()
	}

	limiter := New(b.store, limit, window, keyFn, WithHeaderMode(b.headerMode))
	return limiter.Handler
}
