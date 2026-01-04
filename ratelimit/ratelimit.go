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
//	    ratelimit.WithHeader("X-Tenant-ID", false),
//	)
//	r.Use(limiter.Handler)
//
// Key dimension options accept a required bool parameter. When required=true and the
// value is missing, the request is rejected with 400 Bad Request. When required=false
// (default behavior), rate limiting is skipped for that request.
//
// For distributed deployments (Kubernetes), use the Redis store. The in-memory store
// is only suitable for single-instance deployments and development.
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

// keyFunc extracts a rate limiting key component from an HTTP request.
// Returning an empty string indicates the value is missing.
type keyFunc func(*http.Request) string

// keyDimension holds a key function with validation metadata.
type keyDimension struct {
	fn       keyFunc
	required bool
	name     string // for error messages (e.g., "header X-API-Key")
}

// Limiter implements rate limiting middleware.
type Limiter struct {
	store      store.Store
	limit      int64
	window     time.Duration
	name       string
	keyDims    []keyDimension
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
// Use this for direct connections without a proxy. RemoteAddr is always present.
func WithIP() Option {
	return func(l *Limiter) {
		l.keyDims = append(l.keyDims, keyDimension{
			fn: func(r *http.Request) string {
				ip, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					return r.RemoteAddr
				}
				return ip
			},
			required: false, // RemoteAddr is always present
			name:     "IP",
		})
	}
}

// WithRealIP adds the client IP from X-Forwarded-For or X-Real-IP headers.
// Use this when behind a proxy/load balancer.
//
// If required=true, returns 400 Bad Request when neither header is present.
// If required=false, rate limiting is skipped when neither header is present.
//
// SECURITY: Only use this behind a trusted reverse proxy that sets these headers.
// Without a proxy, clients can spoof X-Forwarded-For to bypass rate limits.
func WithRealIP(required bool) Option {
	return func(l *Limiter) {
		l.keyDims = append(l.keyDims, keyDimension{
			fn: func(r *http.Request) string {
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
			},
			required: required,
			name:     "X-Forwarded-For or X-Real-IP header",
		})
	}
}

// WithEndpoint adds the HTTP method and path to the rate limiting key.
// Key component format: "<method>:<path>". Method and path are always present.
func WithEndpoint() Option {
	return func(l *Limiter) {
		l.keyDims = append(l.keyDims, keyDimension{
			fn: func(r *http.Request) string {
				var sb strings.Builder
				sb.Grow(len(r.Method) + 1 + len(r.URL.Path))
				sb.WriteString(r.Method)
				sb.WriteByte(':')
				sb.WriteString(r.URL.Path)
				return sb.String()
			},
			required: false, // Method and path are always present
			name:     "endpoint",
		})
	}
}

// WithHeader adds a header value to the rate limiting key.
//
// If required=true, returns 400 Bad Request when the header is missing.
// If required=false, rate limiting is skipped when the header is missing.
func WithHeader(header string, required bool) Option {
	return func(l *Limiter) {
		l.keyDims = append(l.keyDims, keyDimension{
			fn: func(r *http.Request) string {
				return r.Header.Get(header)
			},
			required: required,
			name:     fmt.Sprintf("header %s", header),
		})
	}
}

// WithQueryParam adds a query parameter value to the rate limiting key.
//
// If required=true, returns 400 Bad Request when the parameter is missing.
// If required=false, rate limiting is skipped when the parameter is missing.
func WithQueryParam(param string, required bool) Option {
	return func(l *Limiter) {
		l.keyDims = append(l.keyDims, keyDimension{
			fn: func(r *http.Request) string {
				return r.URL.Query().Get(param)
			},
			required: required,
			name:     fmt.Sprintf("query param %s", param),
		})
	}
}

// New creates a new rate limiter with the given store, limit, and window.
// Use With* options to configure key dimensions and behavior.
// Returns 429 (Too Many Requests) when the limit is exceeded, with standard
// rate limit headers and a Retry-After header indicating seconds until reset.
// Returns 400 (Bad Request) if a required key dimension is missing.
// Returns 500 (Internal Server Error) if the store operation fails.
//
// At least one key dimension option (WithIP, WithRealIP, WithHeader, WithEndpoint,
// or WithQueryParam) must be provided. Panics if no key dimensions are configured.
//
// Options:
//   - WithIP: Add RemoteAddr IP to key (direct connections)
//   - WithRealIP: Add X-Forwarded-For/X-Real-IP to key (proxied requests)
//   - WithEndpoint: Add method:path to key
//   - WithHeader: Add header value to key
//   - WithQueryParam: Add query parameter to key
//   - WithName: Set key prefix for collision prevention
//   - WithHeaderMode: Configure header visibility (default: HeadersAlways)
func New(st store.Store, limit int, window time.Duration, opts ...Option) *Limiter {
	l := &Limiter{
		store:      st,
		limit:      int64(limit),
		window:     window,
		keyDims:    make([]keyDimension, 0),
		headerMode: HeadersAlways,
	}
	for _, opt := range opts {
		opt(l)
	}
	if len(l.keyDims) == 0 {
		panic("ratelimit: must configure at least one key dimension option (WithIP, WithRealIP, WithEndpoint, WithHeader, or WithQueryParam)")
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
		ctx := r.Context()
		useWrapper := wrapper.HasState(ctx)

		key, missingDim := l.buildKey(r)

		// Check for missing required dimension
		if missingDim != "" {
			errMsg := fmt.Sprintf("Missing required %s", missingDim)
			if useWrapper {
				wrapper.SetError(r, wrapper.ErrBadRequest.With(errMsg))
			} else {
				http.Error(w, errMsg, http.StatusBadRequest)
			}
			return
		}

		// No key produced (all optional dimensions empty) - skip rate limiting
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

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

// buildKey builds the rate limit key from all dimensions.
// Returns (key, missingDimName). If missingDimName is non-empty, a required dimension was missing.
func (l *Limiter) buildKey(r *http.Request) (string, string) {
	var sb strings.Builder
	sb.Grow(20 + len(l.keyDims)*30)
	hasContent := false

	if l.name != "" {
		sb.WriteString(l.name)
		hasContent = true
	}

	for _, dim := range l.keyDims {
		part := dim.fn(r)
		if part == "" {
			if dim.required {
				return "", dim.name
			}
			continue
		}
		if hasContent {
			sb.WriteByte(':')
		}
		sb.WriteString(part)
		hasContent = true
	}

	if !hasContent {
		return "", ""
	}
	return sb.String(), ""
}
