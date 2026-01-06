// Rate limiting middleware for Chi and standard http.Handler.
//
// Uses a functional options pattern for configuring rate limiters. Key dimensions
// (IP, header, endpoint, etc.) are added via options, allowing single or multi-dimensional
// rate limiting. All middleware sets standard rate limit headers (RateLimit-Limit,
// RateLimit-Remaining, RateLimit-Reset) and returns 429 (Too Many Requests) when limits
// are exceeded.
//
// Single dimension example:
//
//	store := store.NewMemory()
//	defer store.Close()
//	r.Use(chikit.RateLimiter(store, 100, 1*time.Minute, chikit.RateLimitWithIP()).Handler)
//
// Multi-dimensional example:
//
//	limiter := chikit.RateLimiter(store, 100, 1*time.Minute,
//	    chikit.RateLimitWithName("api"),
//	    chikit.RateLimitWithIP(),
//	    chikit.RateLimitWithHeader("X-Tenant-ID"),
//	)
//	r.Use(limiter.Handler)
//
// Key dimension options have optional *Required variants (e.g., RateLimitWithHeaderRequired).
// When a required dimension is missing, the request is rejected with 400 Bad Request.
// When a non-required dimension is missing, rate limiting is skipped for that request.
//
// For distributed deployments (Kubernetes), use the Redis store. The in-memory store
// is only suitable for single-instance deployments and development.

package chikit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nhalm/chikit/store"
)

// RateLimitHeaderMode controls when rate limit headers are included in responses.
type RateLimitHeaderMode int

const (
	// RateLimitHeadersAlways includes rate limit headers on all responses (default).
	// Headers: RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset
	// On 429: Also includes Retry-After
	RateLimitHeadersAlways RateLimitHeaderMode = iota

	// RateLimitHeadersOnLimitExceeded includes rate limit headers only on 429 responses.
	// Headers on 429: RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, Retry-After
	RateLimitHeadersOnLimitExceeded

	// RateLimitHeadersNever never includes rate limit headers in any response.
	// Use this when you want rate limiting without exposing limits to clients.
	RateLimitHeadersNever
)

// rateLimitKeyFunc extracts a rate limiting key component from an HTTP request.
// Returning an empty string indicates the value is missing.
type rateLimitKeyFunc func(*http.Request) string

// rateLimitDimension holds a key function with validation metadata.
type rateLimitDimension struct {
	fn       rateLimitKeyFunc
	required bool
	name     string // for error messages (e.g., "header X-API-Key")
}

// RateLimiter implements rate limiting middleware.
type RateLimiter struct {
	store      store.Store
	limit      int64
	window     time.Duration
	name       string
	keyDims    []rateLimitDimension
	headerMode RateLimitHeaderMode
}

// RateLimitOption configures a RateLimiter.
type RateLimitOption func(*RateLimiter)

// RateLimitWithHeaderMode configures when rate limit headers are included in responses.
func RateLimitWithHeaderMode(mode RateLimitHeaderMode) RateLimitOption {
	return func(l *RateLimiter) {
		l.headerMode = mode
	}
}

// RateLimitWithName sets a prefix for rate limit keys.
// Use to prevent key collisions when layering multiple rate limiters.
func RateLimitWithName(name string) RateLimitOption {
	return func(l *RateLimiter) {
		l.name = name
	}
}

// RateLimitWithIP adds the client IP address (from RemoteAddr) to the rate limiting key.
// Use this for direct connections without a proxy. RemoteAddr is always present.
func RateLimitWithIP() RateLimitOption {
	return func(l *RateLimiter) {
		l.keyDims = append(l.keyDims, rateLimitDimension{
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

// RateLimitWithRealIP adds the client IP from X-Forwarded-For or X-Real-IP headers.
// Use this when behind a proxy/load balancer.
// If neither header is present, rate limiting is skipped for that request.
//
// SECURITY: Only use this behind a trusted reverse proxy that sets these headers.
// Without a proxy, clients can spoof X-Forwarded-For to bypass rate limits.
func RateLimitWithRealIP() RateLimitOption {
	return rateLimitWithRealIP(false)
}

// RateLimitWithRealIPRequired adds the client IP from X-Forwarded-For or X-Real-IP headers.
// Use this when behind a proxy/load balancer.
// Returns 400 Bad Request when neither header is present.
//
// SECURITY: Only use this behind a trusted reverse proxy that sets these headers.
// Without a proxy, clients can spoof X-Forwarded-For to bypass rate limits.
func RateLimitWithRealIPRequired() RateLimitOption {
	return rateLimitWithRealIP(true)
}

func rateLimitWithRealIP(required bool) RateLimitOption {
	return func(l *RateLimiter) {
		l.keyDims = append(l.keyDims, rateLimitDimension{
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

// RateLimitWithEndpoint adds the HTTP method and path to the rate limiting key.
// Key component format: "<method>:<path>". Method and path are always present.
func RateLimitWithEndpoint() RateLimitOption {
	return func(l *RateLimiter) {
		l.keyDims = append(l.keyDims, rateLimitDimension{
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

// RateLimitWithHeader adds a header value to the rate limiting key.
// If the header is missing, rate limiting is skipped for that request.
func RateLimitWithHeader(header string) RateLimitOption {
	return rateLimitWithHeader(header, false)
}

// RateLimitWithHeaderRequired adds a header value to the rate limiting key.
// Returns 400 Bad Request when the header is missing.
func RateLimitWithHeaderRequired(header string) RateLimitOption {
	return rateLimitWithHeader(header, true)
}

func rateLimitWithHeader(header string, required bool) RateLimitOption {
	return func(l *RateLimiter) {
		l.keyDims = append(l.keyDims, rateLimitDimension{
			fn: func(r *http.Request) string {
				return r.Header.Get(header)
			},
			required: required,
			name:     fmt.Sprintf("header %s", header),
		})
	}
}

// RateLimitWithQueryParam adds a query parameter value to the rate limiting key.
// If the parameter is missing, rate limiting is skipped for that request.
func RateLimitWithQueryParam(param string) RateLimitOption {
	return rateLimitWithQueryParam(param, false)
}

// RateLimitWithQueryParamRequired adds a query parameter value to the rate limiting key.
// Returns 400 Bad Request when the parameter is missing.
func RateLimitWithQueryParamRequired(param string) RateLimitOption {
	return rateLimitWithQueryParam(param, true)
}

func rateLimitWithQueryParam(param string, required bool) RateLimitOption {
	return func(l *RateLimiter) {
		l.keyDims = append(l.keyDims, rateLimitDimension{
			fn: func(r *http.Request) string {
				return r.URL.Query().Get(param)
			},
			required: required,
			name:     fmt.Sprintf("query param %s", param),
		})
	}
}

// NewRateLimiter creates a new rate limiter with the given store, limit, and window.
// Use RateLimitWith* options to configure key dimensions and behavior.
// Returns 429 (Too Many Requests) when the limit is exceeded, with standard
// rate limit headers and a Retry-After header indicating seconds until reset.
// Returns 400 (Bad Request) if a *Required dimension is missing.
// Returns 500 (Internal Server Error) if the store operation fails.
//
// At least one key dimension option must be provided.
// Panics if no key dimensions are configured.
//
// Key dimension options:
//   - RateLimitWithIP: Add RemoteAddr IP to key (direct connections)
//   - RateLimitWithRealIP / RateLimitWithRealIPRequired: Add X-Forwarded-For/X-Real-IP to key
//   - RateLimitWithEndpoint: Add method:path to key
//   - RateLimitWithHeader / RateLimitWithHeaderRequired: Add header value to key
//   - RateLimitWithQueryParam / RateLimitWithQueryParamRequired: Add query parameter to key
//
// Other options:
//   - RateLimitWithName: Set key prefix for collision prevention
//   - RateLimitWithHeaderMode: Configure header visibility (default: RateLimitHeadersAlways)
func NewRateLimiter(st store.Store, limit int, window time.Duration, opts ...RateLimitOption) *RateLimiter {
	l := &RateLimiter{
		store:      st,
		limit:      int64(limit),
		window:     window,
		keyDims:    make([]rateLimitDimension, 0),
		headerMode: RateLimitHeadersAlways,
	}
	for _, opt := range opts {
		opt(l)
	}
	if len(l.keyDims) == 0 {
		panic("ratelimit: must configure at least one key dimension option (RateLimitWithIP, RateLimitWithRealIP, RateLimitWithEndpoint, RateLimitWithHeader, or RateLimitWithQueryParam)")
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
func (l *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		useWrapper := HasState(ctx)

		key, missingDim := l.buildKey(r)

		if missingDim != "" {
			errMsg := fmt.Sprintf("Missing required %s", missingDim)
			if useWrapper {
				SetError(r, ErrBadRequest.With(errMsg))
			} else {
				http.Error(w, errMsg, http.StatusBadRequest)
			}
			return
		}

		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		count, ttl, err := l.store.Increment(ctx, key, l.window)
		if err != nil {
			if useWrapper {
				SetError(r, ErrInternal.With("Rate limit check failed"))
			} else {
				http.Error(w, "Rate limit check failed", http.StatusInternalServerError)
			}
			return
		}

		remaining := max(0, l.limit-count)
		resetTime := time.Now().Add(ttl).Unix()
		exceeded := count > l.limit

		shouldSetHeaders := l.headerMode == RateLimitHeadersAlways || (l.headerMode == RateLimitHeadersOnLimitExceeded && exceeded)

		if shouldSetHeaders {
			if useWrapper {
				SetHeader(r, "RateLimit-Limit", strconv.FormatInt(l.limit, 10))
				SetHeader(r, "RateLimit-Remaining", strconv.FormatInt(remaining, 10))
				SetHeader(r, "RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			} else {
				w.Header().Set("RateLimit-Limit", strconv.FormatInt(l.limit, 10))
				w.Header().Set("RateLimit-Remaining", strconv.FormatInt(remaining, 10))
				w.Header().Set("RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			}
		}

		if exceeded {
			if shouldSetHeaders {
				if useWrapper {
					SetHeader(r, "Retry-After", strconv.Itoa(int(ttl.Seconds())))
				} else {
					w.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
				}
			}
			errMsg := fmt.Sprintf("Rate limit exceeded: %d requests per %s", l.limit, l.window)
			if useWrapper {
				SetError(r, ErrRateLimited.With(errMsg))
			} else {
				http.Error(w, errMsg, http.StatusTooManyRequests)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// buildKey builds the rate limit key from all dimensions.
// Returns (key, missingDimName). If missingDimName is non-empty, a required dimension was missing.
func (l *RateLimiter) buildKey(r *http.Request) (string, string) {
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
