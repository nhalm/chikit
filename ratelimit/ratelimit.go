// Package ratelimit provides flexible rate limiting middleware for Chi and standard http.Handler.
//
// The package uses a functional options pattern for configuring rate limiters. Key dimensions
// (IP, header, endpoint, etc.) are added via options, allowing single or multi-dimensional
// rate limiting. All middleware sets standard rate limit headers (RateLimit-Limit,
// RateLimit-Remaining, RateLimit-Reset) and returns 429 (Too Many Requests) when limits
// are exceeded.
//
// Single dimension example:
//
//	store := store.NewMemory()
//	defer store.Close()
//	r.Use(ratelimit.New(store, 100, 1*time.Minute, ratelimit.WithIP()).Handler)
//
// Multi-dimensional example:
//
//	limiter := ratelimit.New(store, 100, 1*time.Minute,
//	    ratelimit.WithName("api"),
//	    ratelimit.WithIP(),
//	    ratelimit.WithHeader("X-Tenant-ID"),
//	)
//	r.Use(limiter.Handler)
//
// Rate limiting is automatically skipped when no key dimensions produce a value.
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
	name       string
	keyFns     []KeyFunc
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

// WithName sets a prefix for rate limit keys.
// Use to prevent key collisions when layering multiple rate limiters.
func WithName(name string) Option {
	return func(l *Limiter) {
		l.name = name
	}
}

// WithIP adds the client IP address (from RemoteAddr) to the rate limiting key.
// Use this for direct connections without a proxy.
func WithIP() Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, func(r *http.Request) string {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				return r.RemoteAddr
			}
			return ip
		})
	}
}

// WithRealIP adds the client IP from X-Forwarded-For or X-Real-IP headers.
// Use this when behind a proxy/load balancer. Returns empty string if neither
// header is present (rate limiting will be skipped).
//
// SECURITY: Only use this behind a trusted reverse proxy that sets these headers.
// Without a proxy, clients can spoof X-Forwarded-For to bypass rate limits.
func WithRealIP() Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, func(r *http.Request) string {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if idx := strings.Index(xff, ","); idx != -1 {
					return strings.TrimSpace(xff[:idx])
				}
				return strings.TrimSpace(xff)
			}
			if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
				return strings.TrimSpace(realIP)
			}
			return ""
		})
	}
}

// WithEndpoint adds the HTTP method and path to the rate limiting key.
// Key component format: "<method>:<path>"
func WithEndpoint() Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, func(r *http.Request) string {
			var sb strings.Builder
			sb.Grow(len(r.Method) + 1 + len(r.URL.Path))
			sb.WriteString(r.Method)
			sb.WriteByte(':')
			sb.WriteString(r.URL.Path)
			return sb.String()
		})
	}
}

// WithHeader adds a header value to the rate limiting key.
// If the header is missing, returns empty string (rate limiting skipped).
func WithHeader(header string) Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, func(r *http.Request) string {
			return r.Header.Get(header)
		})
	}
}

// WithQueryParam adds a query parameter value to the rate limiting key.
// If the parameter is missing, returns empty string (rate limiting skipped).
func WithQueryParam(param string) Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, func(r *http.Request) string {
			return r.URL.Query().Get(param)
		})
	}
}

// WithCustomKey adds a custom key function to the rate limiting key.
// Return empty string to skip rate limiting for a request.
func WithCustomKey(fn KeyFunc) Option {
	return func(l *Limiter) {
		l.keyFns = append(l.keyFns, fn)
	}
}

// New creates a new rate limiter with the given store, limit, and window.
// Use With* options to configure key dimensions and behavior.
// Returns 429 (Too Many Requests) when the limit is exceeded, with standard
// rate limit headers and a Retry-After header indicating seconds until reset.
// Returns 500 (Internal Server Error) if the store operation fails.
//
// At least one key dimension option (WithIP, WithRealIP, WithHeader, WithEndpoint,
// WithQueryParam, or WithCustomKey) must be provided. Panics if no key dimensions
// are configured.
//
// If no key dimensions produce a value at runtime (e.g., missing header),
// rate limiting is skipped for that request.
//
// Options:
//   - WithIP: Add RemoteAddr IP to key (direct connections)
//   - WithRealIP: Add X-Forwarded-For/X-Real-IP to key (proxied requests, skips if missing)
//   - WithEndpoint: Add method:path to key
//   - WithHeader: Add header value to key (skips if missing)
//   - WithQueryParam: Add query parameter to key (skips if missing)
//   - WithCustomKey: Add custom key function
//   - WithName: Set key prefix for collision prevention
//   - WithHeaderMode: Configure header visibility (default: HeadersAlways)
func New(st store.Store, limit int, window time.Duration, opts ...Option) *Limiter {
	l := &Limiter{
		store:      st,
		limit:      int64(limit),
		window:     window,
		keyFns:     make([]KeyFunc, 0),
		headerMode: HeadersAlways,
	}
	for _, opt := range opts {
		opt(l)
	}
	if len(l.keyFns) == 0 {
		panic("ratelimit: must configure at least one key dimension option (WithIP, WithRealIP, WithEndpoint, WithHeader, WithQueryParam, or WithCustomKey)")
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
		key := l.buildKey(r)
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

func (l *Limiter) buildKey(r *http.Request) string {
	var sb strings.Builder
	sb.Grow(20 + len(l.keyFns)*30)
	hasContent := false

	if l.name != "" {
		sb.WriteString(l.name)
		hasContent = true
	}

	for _, fn := range l.keyFns {
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
