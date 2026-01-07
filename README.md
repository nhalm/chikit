# chikit

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/doc/install)
[![Go Reference](https://pkg.go.dev/badge/github.com/nhalm/chikit.svg)](https://pkg.go.dev/github.com/nhalm/chikit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (alongside [pgxkit](https://github.com/nhalm/pgxkit)) providing focused, high-quality Go libraries.

Follows 12-factor app principles with all configuration via explicit parameters—no config files, no environment variable access in middleware.

## Features

- **Response Wrapper**: Context-based response handling with structured JSON errors
- **Request Timeout**: Hard-cutoff timeout with 504 response, context cancellation for DB/HTTP calls
- **Flexible Rate Limiting**: Multi-dimensional rate limiting with Redis support for distributed deployments
- **Header Management**: Extract and validate headers with context injection
- **Request Validation**: Body size limits, query parameter validation, header allow/deny lists
- **Request Binding**: JSON body and query parameter binding with validation
- **Authentication**: API key and bearer token validation with custom validators
- **SLO Tracking**: Per-route SLO classification with PASS/FAIL logging via canonlog
- **Zero Config Files**: Pure code configuration - no config files or environment variables
- **Distributed-Ready**: Redis backend for Kubernetes deployments
- **Fluent API**: Chainable, readable middleware configuration

## Installation

```bash
go get github.com/nhalm/chikit
```

## Quick Start

```go
import (
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit"
)

func main() {
    r := chi.NewRouter()

    // chikit middleware (Handler must be outermost)
    r.Use(chikit.Handler())
    r.Use(chikit.Binder())  // Required for JSON/Query binding

    r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
        var req CreateUserRequest
        if !chikit.JSON(r, &req) {
            return  // Validation error already set
        }
        chikit.SetResponse(r, http.StatusCreated, user)
    })

    http.ListenAndServe(":8080", r)
}
```

Key points:
- `chikit.Handler()` must be the outermost middleware - it manages response state
- `chikit.Binder()` is required before using `chikit.JSON()` or `chikit.Query()`
- Use `chikit.SetResponse()` and `chikit.SetError()` instead of writing directly to `http.ResponseWriter`
- All responses are JSON with consistent error formatting

## Response Wrapper

The wrapper provides context-based response handling. Handlers and middleware set responses in request context rather than writing directly to ResponseWriter, enabling consistent JSON responses and structured errors.

### Basic Usage

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit"
)

func main() {
    r := chi.NewRouter()
    r.Use(chikit.Handler())  // Must be outermost middleware

    r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
        user, err := createUser(r)
        if err != nil {
            chikit.SetError(r, chikit.ErrInternal.With("Failed to create user"))
            return
        }
        chikit.SetResponse(r, http.StatusCreated, user)
    })
}
```

### Structured Errors

Errors follow a structured format:

```go
// Predefined sentinel errors
chikit.ErrBadRequest          // 400
chikit.ErrUnauthorized        // 401
chikit.ErrPaymentRequired     // 402
chikit.ErrForbidden           // 403
chikit.ErrNotFound            // 404
chikit.ErrMethodNotAllowed    // 405
chikit.ErrConflict            // 409
chikit.ErrGone                // 410
chikit.ErrPayloadTooLarge     // 413
chikit.ErrUnprocessableEntity // 422
chikit.ErrRateLimited         // 429
chikit.ErrInternal            // 500
chikit.ErrNotImplemented      // 501
chikit.ErrServiceUnavailable  // 503
chikit.ErrGatewayTimeout      // 504

// Customize message
chikit.SetError(r, chikit.ErrNotFound.With("User not found"))

// Customize message and parameter
chikit.SetError(r, chikit.ErrBadRequest.WithParam("Invalid email format", "email"))
```

JSON response format:

```json
{
  "error": {
    "type": "not_found",
    "code": "resource_not_found",
    "message": "User not found"
  }
}
```

### Validation Errors

For multiple field errors:

```go
chikit.SetError(r, chikit.NewValidationError([]chikit.FieldError{
    {Param: "email", Code: "required", Message: "Email is required"},
    {Param: "age", Code: "min", Message: "Age must be at least 18"},
}))
```

JSON response:

```json
{
  "error": {
    "type": "validation_error",
    "code": "invalid_request",
    "message": "Validation failed",
    "errors": [
      {"param": "email", "code": "required", "message": "Email is required"},
      {"param": "age", "code": "min", "message": "Age must be at least 18"}
    ]
  }
}
```

### Setting Headers

```go
chikit.SetHeader(r, "X-Request-ID", requestID)
chikit.SetHeader(r, "X-RateLimit-Remaining", "99")
chikit.AddHeader(r, "X-Custom", "value1")
chikit.AddHeader(r, "X-Custom", "value2")  // Adds second value
```

### Dual-Mode Middleware

Middleware can check if wrapper is present and fall back gracefully:

```go
func MyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if err := validate(r); err != nil {
            if chikit.HasState(r.Context()) {
                chikit.SetError(r, chikit.ErrBadRequest.With(err.Error()))
            } else {
                http.Error(w, err.Error(), http.StatusBadRequest)
            }
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Panic Recovery

The Handler middleware automatically recovers from panics and returns a 500 error:

```go
r.Use(chikit.Handler())

r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("something went wrong")  // Returns {"error": {"type": "internal_error", ...}}
})
```

### Request Timeout

Add hard-cutoff timeouts that guarantee response time:

```go
r.Use(chikit.Handler(
    chikit.WithTimeout(30*time.Second),
    chikit.WithCanonlog(),
))

r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
    // If this takes longer than 30s, the client gets a 504 immediately.
    // The context is cancelled so DB/HTTP calls can exit early.
    result, err := slowDatabaseQuery(r.Context())
    if err != nil {
        // Handle context.DeadlineExceeded gracefully
        if errors.Is(err, context.DeadlineExceeded) {
            return // Timeout already handled by middleware
        }
        chikit.SetError(r, chikit.ErrInternal.With("Query failed"))
        return
    }
    chikit.SetResponse(r, http.StatusOK, result)
})
```

**How it works:**
1. Handler runs in a goroutine with `context.WithTimeout()`
2. If timeout fires, a 504 Gateway Timeout is written immediately
3. The context is cancelled so DB/HTTP calls see the deadline and can exit early
4. After the grace timeout (default 5s), the handler is considered abandoned

**Timeout options:**

| Option | Description |
|--------|-------------|
| `WithTimeout(d)` | Maximum handler execution time |
| `WithGraceTimeout(d)` | How long to wait for handler to exit after timeout (default 5s) |
| `WithAbandonCallback(fn)` | Called when handler doesn't exit within grace period |

**Graceful shutdown:**

When using timeouts, handlers run in goroutines. For graceful shutdown, wait for all handlers to complete:

```go
func main() {
    r := chi.NewRouter()
    r.Use(chikit.Handler(chikit.WithTimeout(30 * time.Second)))
    // ... routes ...

    srv := &http.Server{Addr: ":8080", Handler: r}
    go srv.ListenAndServe()

    // Wait for shutdown signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    // Graceful shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    srv.Shutdown(ctx)           // Wait for in-flight requests
    chikit.WaitForHandlers(ctx) // Wait for handler goroutines
}
```

**Important limitation:** Go cannot forcibly terminate goroutines. If your handler ignores context cancellation (tight CPU loops, blocking syscalls, legacy code without context), the goroutine continues running after the 504 response. Use `WithAbandonCallback` to track this with metrics.

### Canonical Logging

Integrate with [canonlog](https://github.com/nhalm/canonlog) for structured request logging:

```go
import "github.com/nhalm/canonlog"

func main() {
    canonlog.SetupGlobalLogger("info", "json")

    r := chi.NewRouter()
    r.Use(chikit.Handler(
        chikit.WithCanonlog(),
        chikit.WithCanonlogFields(func(r *http.Request) map[string]any {
            return map[string]any{
                "request_id": r.Header.Get("X-Request-ID"),
                "tenant_id":  r.Header.Get("X-Tenant-ID"),
            }
        }),
    ))
}
```

This automatically logs for each request:
- `method`, `path`, `route` (Chi route pattern)
- `status`, `duration_ms`
- `errors` array (when errors occur)

Output:
```json
{"time":"...","level":"INFO","msg":"","method":"GET","path":"/users/123","route":"/users/{id}","status":200,"duration_ms":45,"request_id":"abc-123"}
```

### SLO Integration

Enable SLO status logging with `WithSLOs()`. See [SLO Tracking](#slo-tracking) for details.

## Rate Limiting

### Basic Usage

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit"
    "github.com/nhalm/chikit/store"
    "time"
)

func main() {
    r := chi.NewRouter()

    // In-memory store (development)
    st := store.NewMemory()
    defer st.Close()

    // Rate limit by IP: 100 requests per minute
    r.Use(chikit.NewRateLimiter(st, 100, 1*time.Minute, chikit.RateLimitWithIP()).Handler)

    // Rate limit by header (skip if header missing)
    r.Use(chikit.NewRateLimiter(st, 1000, 1*time.Hour, chikit.RateLimitWithHeader("X-API-Key")).Handler)

    // Rate limit by endpoint
    r.Use(chikit.NewRateLimiter(st, 100, 1*time.Minute, chikit.RateLimitWithEndpoint()).Handler)
}
```

### Multi-Dimensional Rate Limiting

Combine multiple key dimensions for fine-grained control:

```go
// Rate limit by IP + endpoint combination
limiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithEndpoint(),
)
r.Use(limiter.Handler)

// Rate limit by IP + tenant header (reject if header missing)
limiter := chikit.NewRateLimiter(st, 1000, 1*time.Hour,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithHeaderRequired("X-Tenant-ID"),
)
r.Use(limiter.Handler)

// Complex multi-dimensional rate limiting
limiter := chikit.NewRateLimiter(st, 50, 1*time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithEndpoint(),
    chikit.RateLimitWithHeaderRequired("X-API-Key"),
    chikit.RateLimitWithQueryParam("user_id"),
)
r.Use(limiter.Handler)
```

### Key Dimension Options

| Option | Description |
|--------|-------------|
| `RateLimitWithIP()` | Client IP from RemoteAddr (direct connections) |
| `RateLimitWithRealIP()` | Client IP from X-Forwarded-For/X-Real-IP (behind proxy) |
| `RateLimitWithRealIPRequired()` | Same as RateLimitWithRealIP, but returns 400 if missing |
| `RateLimitWithEndpoint()` | HTTP method + path (e.g., `GET:/api/users`) |
| `RateLimitWithHeader(name)` | Header value (skip if missing) |
| `RateLimitWithHeaderRequired(name)` | Header value (400 if missing) |
| `RateLimitWithQueryParam(name)` | Query parameter value (skip if missing) |
| `RateLimitWithQueryParamRequired(name)` | Query parameter value (400 if missing) |
| `RateLimitWithName(name)` | Key prefix for collision prevention |

The `*Required` variants return 400 Bad Request if the value is missing.
The non-required variants skip rate limiting for that request if the value is missing.

### Redis Backend (Production)

For distributed deployments:

```go
import (
    "log"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit"
    "github.com/nhalm/chikit/store"
)

func main() {
    st, err := store.NewRedis(store.RedisConfig{
        URL:    "redis:6379",
        Prefix: "rl:",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer st.Close()

    r := chi.NewRouter()
    r.Use(chikit.NewRateLimiter(st, 100, time.Minute, chikit.RateLimitWithIP()).Handler)
}
```

### Rate Limit Headers

All rate limiters set standard headers following the IETF draft-ietf-httpapi-ratelimit-headers specification:

```
RateLimit-Limit: 100
RateLimit-Remaining: 95
RateLimit-Reset: 1735401600
Retry-After: 60
```

Header behavior can be configured:

```go
// Always include headers (default)
limiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithHeaderMode(chikit.RateLimitHeadersAlways),
)

// Include headers only on 429 responses
limiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithHeaderMode(chikit.RateLimitHeadersOnLimitExceeded),
)

// Never include headers
limiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithHeaderMode(chikit.RateLimitHeadersNever),
)
```

### Layered Rate Limiting

When applying multiple rate limiters to the same routes, use `RateLimitWithName()` to prevent key collisions:

```go
st := store.NewMemory()
defer st.Close()

// Global limit: 1000 requests per hour per IP
globalLimiter := chikit.NewRateLimiter(st, 1000, 1*time.Hour,
    chikit.RateLimitWithName("global"),
    chikit.RateLimitWithIP(),
)

// Endpoint-specific limit: 10 requests per minute per IP+endpoint
endpointLimiter := chikit.NewRateLimiter(st, 10, 1*time.Minute,
    chikit.RateLimitWithName("endpoint"),
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithEndpoint(),
)

// Apply both limiters
r.Use(globalLimiter.Handler)
r.Use(endpointLimiter.Handler)
```

Without `RateLimitWithName()`, the keys would collide because both limiters use `RateLimitWithIP()`. The name is prepended to the key:

```
// Without RateLimitWithName():
192.168.1.1                           // Both limiters use this key - collision!

// With RateLimitWithName():
global:192.168.1.1                    // Global limiter
endpoint:192.168.1.1:GET:/api/users   // Endpoint limiter - independent
```

This pattern is useful for implementing tiered rate limits:

```go
// Tier 1: Broad protection (DDoS prevention)
ddosLimiter := chikit.NewRateLimiter(st, 10000, 1*time.Hour,
    chikit.RateLimitWithName("ddos"),
    chikit.RateLimitWithIP(),
)
r.Use(ddosLimiter.Handler)

// Tier 2: API endpoint protection
r.Route("/api", func(r chi.Router) {
    apiLimiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
        chikit.RateLimitWithName("api"),
        chikit.RateLimitWithIP(),
        chikit.RateLimitWithEndpoint(),
    )
    r.Use(apiLimiter.Handler)
})

// Tier 3: Expensive operation protection
r.Route("/api/analytics/run", func(r chi.Router) {
    analyticsLimiter := chikit.NewRateLimiter(st, 5, 1*time.Hour,
        chikit.RateLimitWithName("analytics"),
        chikit.RateLimitWithHeaderRequired("X-User-ID"),
    )
    r.Use(analyticsLimiter.Handler)
    r.Post("/", analyticsHandler)
})
```

## Header Management

### Generic Header to Context

Extract any header with validation:

```go
import "github.com/nhalm/chikit"

// Simple header extraction
r.Use(chikit.ExtractHeader("X-API-Key", "api_key"))

// With validation
r.Use(chikit.ExtractHeader("X-Correlation-ID", "correlation_id",
    chikit.ExtractRequired(),
    chikit.ExtractWithValidator(func(val string) (any, error) {
        if len(val) < 10 {
            return nil, errors.New("correlation ID too short")
        }
        return val, nil
    }),
))

// With default value
r.Use(chikit.ExtractHeader("X-Environment", "environment",
    chikit.ExtractDefault("production"),
))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    apiKey, ok := chikit.HeaderFromContext(r.Context(), "api_key")
    if !ok {
        http.Error(w, "No API key", http.StatusUnauthorized)
        return
    }
}
```

### Example: Tenant ID as UUID

```go
import (
    "github.com/google/uuid"
    "github.com/nhalm/chikit"
)

// Extract X-Tenant-ID header as UUID with validation
r.Use(chikit.ExtractHeader("X-Tenant-ID", "tenant_id",
    chikit.ExtractRequired(),
    chikit.ExtractWithValidator(func(val string) (any, error) {
        return uuid.Parse(val)
    }),
))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    val, ok := chikit.HeaderFromContext(r.Context(), "tenant_id")
    if !ok {
        http.Error(w, "No tenant ID", http.StatusBadRequest)
        return
    }
    tenantID := val.(uuid.UUID)
    // Use tenantID...
}
```

## Request Validation

### Body Size Limits

Prevent DoS attacks by limiting request body size:

```go
import "github.com/nhalm/chikit"

// Limit request body to 1MB
r.Use(chikit.MaxBodySize(1024 * 1024))
```

The middleware provides two-stage protection:

1. **Content-Length check**: Requests with `Content-Length` exceeding the limit are rejected with 413 immediately, before the handler runs
2. **MaxBytesReader wrapper**: All request bodies are wrapped with `http.MaxBytesReader` as defense-in-depth, catching chunked transfers and requests with missing/incorrect Content-Length headers

When using `chikit.JSON`, the second stage is automatic - if the body exceeds the limit during decoding, `chikit.JSON` detects the error and returns `chikit.ErrPayloadTooLarge` (413).

### Header Validation

Validate headers with allow/deny lists:

```go
// Required header
r.Use(chikit.ValidateHeaders(
    chikit.ValidateWithHeader("X-API-Key", chikit.ValidateRequired()),
))

// Allow list (only specific values allowed)
r.Use(chikit.ValidateHeaders(
    chikit.ValidateWithHeader("X-Environment",
        chikit.ValidateAllowList("production", "staging", "development"),
    ),
))

// Deny list (block specific values)
r.Use(chikit.ValidateHeaders(
    chikit.ValidateWithHeader("X-Source",
        chikit.ValidateDenyList("blocked-client", "banned-user"),
    ),
))

// Case-sensitive validation (default: case-insensitive)
r.Use(chikit.ValidateHeaders(
    chikit.ValidateWithHeader("X-Auth-Token",
        chikit.ValidateAllowList("Bearer", "Basic"),
        chikit.ValidateCaseSensitive(),
    ),
))

// Multiple header rules
r.Use(chikit.ValidateHeaders(
    chikit.ValidateWithHeader("X-API-Key", chikit.ValidateRequired()),
    chikit.ValidateWithHeader("X-Environment", chikit.ValidateAllowList("production", "staging")),
    chikit.ValidateWithHeader("X-Source", chikit.ValidateDenyList("blocked")),
))
```

## Request Binding

The bind functions provide JSON body and query parameter binding with validation using go-playground/validator/v10.

### JSON Binding

```go
import "github.com/nhalm/chikit"

type CreateUserRequest struct {
    Email string `json:"email" validate:"required,email"`
    Name  string `json:"name" validate:"required,min=2"`
    Age   int    `json:"age" validate:"omitempty,min=18"`
}

func main() {
    r := chi.NewRouter()
    r.Use(chikit.Handler())
    r.Use(chikit.Binder())

    r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
        var req CreateUserRequest
        if !chikit.JSON(r, &req) {
            return  // Validation error already set in wrapper
        }
        // Use req.Email, req.Name, req.Age
        chikit.SetResponse(r, http.StatusCreated, user)
    })
}
```

### Query Parameter Binding

```go
type ListUsersQuery struct {
    Page   int    `query:"page" validate:"omitempty,min=1"`
    Limit  int    `query:"limit" validate:"omitempty,min=1,max=100"`
    Search string `query:"search" validate:"omitempty,min=3"`
}

r.Get("/users", func(w http.ResponseWriter, r *http.Request) {
    var query ListUsersQuery
    if !chikit.Query(r, &query) {
        return  // Validation error already set in wrapper
    }
    // Use query.Page, query.Limit, query.Search
})
```

### Custom Validation Messages

```go
r.Use(chikit.Binder(chikit.BindWithFormatter(func(field, tag, param string) string {
    switch tag {
    case "required":
        return field + " is required"
    case "email":
        return field + " must be a valid email address"
    case "min":
        return field + " must be at least " + param
    default:
        return field + " is invalid"
    }
})))
```

### Custom Validators

Register custom validation tags at startup:

```go
func init() {
    chikit.RegisterValidation("customtag", func(fl validator.FieldLevel) bool {
        return fl.Field().String() == "valid"
    })
}
```

## Authentication

### API Key Authentication

Validate API keys with custom validators:

```go
import "github.com/nhalm/chikit"

// Simple validator
validator := func(key string) bool {
    return key == "secret-key"
}

r.Use(chikit.APIKey(validator))

// Custom header
r.Use(chikit.APIKey(validator, chikit.WithAPIKeyHeader("X-Custom-Key")))

// Optional API key
r.Use(chikit.APIKey(validator, chikit.WithOptionalAPIKey()))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    key, ok := chikit.APIKeyFromContext(r.Context())
    if ok {
        // Use API key
    }
}
```

### Bearer Token Authentication

Validate bearer tokens from Authorization headers:

```go
// JWT validator example
validator := func(token string) bool {
    // Validate JWT, check expiry, etc.
    return validateJWT(token)
}

r.Use(chikit.BearerToken(validator))

// Optional bearer token
r.Use(chikit.BearerToken(validator, chikit.WithOptionalBearerToken()))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    token, ok := chikit.BearerTokenFromContext(r.Context())
    if ok {
        // Use bearer token
    }
}
```

## SLO Tracking

Track service level objectives with per-route SLO classification. The SLO middleware sets tier and target in request context, and the wrapper middleware logs PASS/FAIL status via canonlog.

### Predefined Tiers

| Tier | Target | Use Case |
|------|--------|----------|
| `SLOCritical` | 50ms | Essential functions (99.99% availability) |
| `SLOHighFast` | 100ms | User-facing requests requiring quick responses |
| `SLOHighSlow` | 1000ms | Important requests tolerating higher latency |
| `SLOLow` | 5000ms | Background tasks, non-interactive functions |

### Basic Usage

```go
import (
    "github.com/nhalm/canonlog"
    "github.com/nhalm/chikit"
)

func main() {
    canonlog.SetupGlobalLogger("info", "json")

    r := chi.NewRouter()

    // Enable canonlog and SLO logging
    r.Use(chikit.Handler(
        chikit.WithCanonlog(),
        chikit.WithSLOs(),
    ))

    // Set SLO tier per route
    r.With(chikit.SLO(chikit.SLOCritical)).Get("/health", healthHandler)
    r.With(chikit.SLO(chikit.SLOHighFast)).Get("/users/{id}", getUser)
    r.With(chikit.SLO(chikit.SLOHighSlow)).Post("/reports", generateReport)
    r.With(chikit.SLO(chikit.SLOLow)).Post("/batch", batchProcess)
}
```

### Custom Targets

For routes that don't fit predefined tiers:

```go
r.With(chikit.SLOWithTarget(200 * time.Millisecond)).Get("/custom", handler)
```

Custom targets are logged with `slo_class: "custom"`.

### Log Output

Success (within target):
```json
{"time":"...","level":"INFO","msg":"","method":"GET","path":"/users/123","route":"/users/{id}","status":200,"duration_ms":45,"slo_class":"high_fast","slo_status":"PASS"}
```

SLO breach (exceeded target):
```json
{"time":"...","level":"INFO","msg":"","method":"GET","path":"/users/123","route":"/users/{id}","status":200,"duration_ms":150,"slo_class":"high_fast","slo_status":"FAIL"}
```

### Alerting

Use your log aggregator to create alerts based on SLO status:

```sql
-- Example: Alert when >1% of high_fast requests fail in 5 minutes
SELECT
  COUNT(*) FILTER (WHERE slo_status = 'FAIL') * 100.0 / COUNT(*) as failure_rate
FROM logs
WHERE slo_class = 'high_fast'
  AND timestamp > NOW() - INTERVAL '5 minutes'
HAVING failure_rate > 1.0
```

### Routes Without SLO

Routes without `chikit.SLO()` middleware won't have SLO fields in logs:

```json
{"time":"...","level":"INFO","msg":"","method":"GET","path":"/misc","route":"/misc","status":200,"duration_ms":30}
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/google/uuid"
    "github.com/nhalm/canonlog"
    "github.com/nhalm/chikit"
    "github.com/nhalm/chikit/store"
)

type ListUsersQuery struct {
    Page  int `query:"page" validate:"omitempty,min=1"`
    Limit int `query:"limit" validate:"omitempty,min=1,max=100"`
}

type CreateUserRequest struct {
    Email string `json:"email" validate:"required,email"`
    Name  string `json:"name" validate:"required,min=2"`
}

func main() {
    // Setup canonical logging
    canonlog.SetupGlobalLogger("info", "json")

    r := chi.NewRouter()

    // Standard Chi middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)

    // Wrapper with timeout, canonlog, and SLO logging
    r.Use(chikit.Handler(
        chikit.WithTimeout(30*time.Second),
        chikit.WithCanonlog(),
        chikit.WithCanonlogFields(func(r *http.Request) map[string]any {
            return map[string]any{
                "request_id": middleware.GetReqID(r.Context()),
            }
        }),
        chikit.WithSLOs(),
    ))

    // Bind middleware for request binding/validation
    r.Use(chikit.Binder())

    // Limit request body size to 10MB
    r.Use(chikit.MaxBodySize(10 * 1024 * 1024))

    // Validate environment header
    r.Use(chikit.ValidateHeaders(
        chikit.ValidateWithHeader("X-Environment",
            chikit.ValidateAllowList("production", "staging", "development"),
        ),
    ))

    // Extract tenant ID from header
    r.Use(chikit.ExtractHeader("X-Tenant-ID", "tenant_id",
        chikit.ExtractRequired(),
        chikit.ExtractWithValidator(func(val string) (any, error) {
            return uuid.Parse(val)
        }),
    ))

    // Redis store
    st, err := store.NewRedis(store.RedisConfig{
        URL:      "redis:6379",
        Password: "",
        DB:       0,
        Prefix:   "ratelimit:",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer st.Close()

    // Global rate limit: 1000 requests per hour per IP
    globalLimiter := chikit.NewRateLimiter(st, 1000, 1*time.Hour,
        chikit.RateLimitWithName("global"),
        chikit.RateLimitWithIP(),
    )
    r.Use(globalLimiter.Handler)

    // Health check with strict SLO
    r.With(chikit.SLO(chikit.SLOCritical)).Get("/health", healthHandler)

    // API routes
    r.Route("/api/v1", func(r chi.Router) {
        // API key authentication
        r.Use(chikit.APIKey(func(key string) bool {
            return validateAPIKey(key)
        }))

        // Per-tenant rate limiting: 100 requests per minute
        tenantLimiter := chikit.NewRateLimiter(st, 100, 1*time.Minute,
            chikit.RateLimitWithName("tenant"),
            chikit.RateLimitWithIP(),
            chikit.RateLimitWithHeaderRequired("X-Tenant-ID"),
        )
        r.Use(tenantLimiter.Handler)

        r.With(chikit.SLO(chikit.SLOHighFast)).Get("/users", listUsers)
        r.With(chikit.SLO(chikit.SLOHighFast)).Get("/users/{id}", getUser)
        r.With(chikit.SLO(chikit.SLOHighFast)).Post("/users", createUser)
        r.With(chikit.SLO(chikit.SLOHighSlow)).Post("/reports", generateReport)
    })

    srv := &http.Server{Addr: ":8080", Handler: r}
    go func() {
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    // Wait for shutdown signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    // Graceful shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    srv.Shutdown(ctx)
    chikit.WaitForHandlers(ctx) // Wait for handler goroutines when using WithTimeout
}

func validateAPIKey(key string) bool {
    // Implement your API key validation
    return true
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    chikit.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    val, ok := chikit.HeaderFromContext(r.Context(), "tenant_id")
    if !ok {
        chikit.SetError(r, chikit.ErrBadRequest.With("No tenant ID"))
        return
    }
    tenantID := val.(uuid.UUID)

    var query ListUsersQuery
    if !chikit.Query(r, &query) {
        return
    }

    // Query users for tenant...
    chikit.SetResponse(r, http.StatusOK, map[string]any{
        "tenant": tenantID.String(),
        "page":   query.Page,
        "limit":  query.Limit,
    })
}

func getUser(w http.ResponseWriter, r *http.Request) {
    chikit.SetResponse(r, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

func createUser(w http.ResponseWriter, r *http.Request) {
    val, ok := chikit.HeaderFromContext(r.Context(), "tenant_id")
    if !ok {
        chikit.SetError(r, chikit.ErrBadRequest.With("No tenant ID"))
        return
    }
    tenantID := val.(uuid.UUID)

    var req CreateUserRequest
    if !chikit.JSON(r, &req) {
        return // Returns 400 for validation errors, 413 if body exceeds MaxBodySize limit
    }

    // Create user for tenant...
    chikit.SetResponse(r, http.StatusCreated, map[string]any{
        "tenant": tenantID.String(),
        "email":  req.Email,
    })
}

func generateReport(w http.ResponseWriter, r *http.Request) {
    // Long-running report generation...
    chikit.SetResponse(r, http.StatusOK, map[string]string{"status": "complete"})
}
```


## License

MIT
