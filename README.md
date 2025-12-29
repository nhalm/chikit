# chikit

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/doc/install)
[![Go Reference](https://pkg.go.dev/badge/github.com/nhalm/chikit.svg)](https://pkg.go.dev/github.com/nhalm/chikit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (alongside [pgxkit](https://github.com/nhalm/pgxkit)) providing focused, high-quality Go libraries.

Follows 12-factor app principles with all configuration via explicit parameters—no config files, no environment variable access in middleware.

## Features

- **Response Wrapper**: Context-based response handling with Stripe-style structured errors
- **Flexible Rate Limiting**: Multi-dimensional rate limiting with Redis support for distributed deployments
- **Request Binding**: JSON body decoding with struct validation using go-playground/validator
- **Header Management**: Extract and validate headers with context injection
- **Request Validation**: Body size limits, query parameter validation, header allow/deny lists
- **Authentication**: API key and bearer token validation with custom validators
- **SLO Tracking**: Callback-based metrics for latency, traffic, and errors
- **Zero Config Files**: Pure code configuration - no config files or environment variables
- **Distributed-Ready**: Redis backend for Kubernetes deployments
- **Fluent API**: Chainable, readable middleware configuration

## Installation

```bash
go get github.com/nhalm/chikit
```

## Response Wrapper

The `wrapper` package provides context-based response handling. Handlers and middleware set responses/errors in context rather than writing directly to ResponseWriter. The `wrapper.Handler` middleware writes all JSON responses.

### Basic Usage

```go
import (
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit/wrapper"
)

func main() {
    r := chi.NewRouter()

    // wrapper.Handler MUST be the outermost middleware
    r.Use(wrapper.Handler())

    r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
        user, err := createUser(r)
        if err != nil {
            wrapper.SetError(r, wrapper.ErrInternal.With("Failed to create user"))
            return
        }
        wrapper.SetResponse(r, http.StatusCreated, user)
    })
}
```

### Structured Errors (Stripe-style)

All errors follow a consistent JSON structure:

```json
{
  "error": {
    "type": "validation_error",
    "code": "invalid_request",
    "message": "Validation failed",
    "param": "email",
    "errors": [
      {"field": "email", "code": "required", "message": "email is required"},
      {"field": "age", "code": "gte", "message": "age must be at least 18"}
    ]
  }
}
```

### Predefined Sentinel Errors

```go
// 4xx errors
wrapper.ErrBadRequest          // 400
wrapper.ErrUnauthorized        // 401
wrapper.ErrPaymentRequired     // 402
wrapper.ErrForbidden           // 403
wrapper.ErrNotFound            // 404
wrapper.ErrMethodNotAllowed    // 405
wrapper.ErrConflict            // 409
wrapper.ErrGone                // 410
wrapper.ErrPayloadTooLarge     // 413
wrapper.ErrUnprocessableEntity // 422
wrapper.ErrRateLimited         // 429

// 5xx errors
wrapper.ErrInternal            // 500
wrapper.ErrNotImplemented      // 501
wrapper.ErrServiceUnavailable  // 503
```

### Customizing Error Messages

```go
// Custom message
wrapper.SetError(r, wrapper.ErrNotFound.With("User not found"))

// Custom message with param
wrapper.SetError(r, wrapper.ErrBadRequest.WithParam("Invalid email format", "email"))

// Validation error with multiple fields
wrapper.SetError(r, wrapper.NewValidationError([]wrapper.FieldError{
    {Field: "email", Code: "required", Message: "email is required"},
    {Field: "age", Code: "gte", Message: "age must be at least 18"},
}))
```

### Setting Headers

```go
func handler(w http.ResponseWriter, r *http.Request) {
    wrapper.SetHeader(r, "X-Request-ID", "abc123")
    wrapper.SetHeader(r, "X-Custom-Header", "value")
    wrapper.SetResponse(r, http.StatusOK, data)
}
```

## Request Binding

The `bind` package provides JSON body decoding with struct validation using [go-playground/validator](https://github.com/go-playground/validator).

### Basic Usage

```go
import (
    "net/http"
    "github.com/nhalm/chikit/bind"
    "github.com/nhalm/chikit/wrapper"
)

type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=2,max=100"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"gte=18,lte=120"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := bind.JSON(r, &req); err != nil {
        wrapper.SetError(r, err.(*wrapper.Error))
        return
    }

    // req is decoded and validated
    wrapper.SetResponse(r, http.StatusCreated, user)
}
```

### Custom Validators

Register custom validators for enum types:

```go
import "github.com/go-playground/validator/v10"

type Status string

const (
    StatusPending  Status = "pending"
    StatusActive   Status = "active"
    StatusInactive Status = "inactive"
)

func (s Status) IsValid() bool {
    switch s {
    case StatusPending, StatusActive, StatusInactive:
        return true
    }
    return false
}

func init() {
    bind.RegisterValidation("status", func(fl validator.FieldLevel) bool {
        status, ok := fl.Field().Interface().(Status)
        return ok && status.IsValid()
    })
}

type UpdateRequest struct {
    Status Status `json:"status" validate:"required,status"`
}
```

### Available Validation Tags

Common validation tags from go-playground/validator:

- `required` - Field must be present
- `email` - Valid email format
- `url` - Valid URL format
- `uuid` - Valid UUID format
- `min=n` - Minimum length/value
- `max=n` - Maximum length/value
- `len=n` - Exact length
- `gte=n` - Greater than or equal
- `lte=n` - Less than or equal
- `oneof=a b c` - Value must be one of listed values

See [validator documentation](https://pkg.go.dev/github.com/go-playground/validator/v10) for full list.

## Rate Limiting

### Simple API

For common use cases, use the simple API:

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit/ratelimit"
    "github.com/nhalm/chikit/ratelimit/store"
    "github.com/nhalm/chikit/wrapper"
    "time"
)

func main() {
    r := chi.NewRouter()
    r.Use(wrapper.Handler())

    // In-memory store (development)
    st := store.NewMemory()
    defer st.Close()

    // Rate limit by IP: 10 requests per minute
    r.Use(ratelimit.ByIP(st, 10, time.Minute))

    // Rate limit by header: 1000 requests per hour
    r.Use(ratelimit.ByHeader(st, "X-API-Key", 1000, time.Hour))

    // Rate limit by endpoint: 100 requests per minute
    r.Use(ratelimit.ByEndpoint(st, 100, time.Minute))

    // Rate limit by query parameter
    r.Use(ratelimit.ByQueryParam(st, "user_id", 50, time.Minute))
}
```

### Fluent Builder API

For complex multi-dimensional rate limiting:

```go
// Rate limit by IP + endpoint combination
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithEndpoint().
    Limit(100, time.Minute))

// Rate limit by IP + tenant header
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithHeader("X-Tenant-ID").
    Limit(1000, time.Hour))

// Complex multi-dimensional rate limiting
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithEndpoint().
    WithHeader("X-API-Key").
    WithQueryParam("user_id").
    Limit(50, time.Minute))
```

### Redis Backend (Production)

For distributed deployments:

```go
import "github.com/nhalm/chikit/ratelimit/store"

st, err := store.NewRedis(store.RedisConfig{
    URL:      "redis.default.svc.cluster.local:6379",
    Password: "your-password",
    DB:       0,
    Prefix:   "ratelimit:",
})
if err != nil {
    log.Fatal(err)
}
defer st.Close()
```

### Custom Key Functions

Build your own rate limiting logic:

```go
keyFn := func(r *http.Request) string {
    // Extract user ID from JWT or context
    userID := getUserIDFromToken(r)
    return fmt.Sprintf("user:%s:%s", userID, r.URL.Path)
}

limiter := ratelimit.New(st, 100, time.Minute, keyFn)
r.Use(limiter.Handler)
```

### Rate Limit Headers

All rate limiters set standard headers following the IETF draft-ietf-httpapi-ratelimit-headers specification:

```
RateLimit-Limit: 100
RateLimit-Remaining: 95
RateLimit-Reset: 1735401600
Retry-After: 60
```

Header behavior can be configured using the Builder API:

```go
// Always include headers (default)
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithHeaderMode(ratelimit.HeadersAlways).
    Limit(100, time.Minute))

// Include headers only on 429 responses
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithHeaderMode(ratelimit.HeadersOnLimitExceeded).
    Limit(100, time.Minute))

// Never include headers
r.Use(ratelimit.NewBuilder(st).
    WithIP().
    WithHeaderMode(ratelimit.HeadersNever).
    Limit(100, time.Minute))
```

### Layered Rate Limiting

When applying multiple rate limiters to the same routes, use `WithName()` to prevent key collisions:

```go
st := store.NewMemory()
defer st.Close()

// Global limit: 1000 requests per hour per IP
globalLimiter := ratelimit.NewBuilder(st).
    WithName("global").
    WithIP().
    Limit(1000, time.Hour)

// Endpoint-specific limit: 10 requests per minute per IP+endpoint
endpointLimiter := ratelimit.NewBuilder(st).
    WithName("endpoint").
    WithIP().
    WithEndpoint().
    Limit(10, time.Minute)

// Apply both limiters
r.Use(globalLimiter)
r.Use(endpointLimiter)
```

Without `WithName()`, the keys would collide because both limiters use `WithIP()`. The name is prepended to the key:

```
// Without WithName():
192.168.1.1                           // Both limiters use this key - collision!

// With WithName():
global:192.168.1.1                    // Global limiter
endpoint:192.168.1.1:GET:/api/users   // Endpoint limiter - independent
```

## Header Management

### Generic Header to Context

Extract any header with validation:

```go
import "github.com/nhalm/chikit/headers"

// Simple header extraction
r.Use(headers.New("X-API-Key", "api_key"))

// With validation
r.Use(headers.New("X-Correlation-ID", "correlation_id",
    headers.WithRequired(),
    headers.WithValidator(func(val string) (any, error) {
        if len(val) < 10 {
            return nil, errors.New("correlation ID too short")
        }
        return val, nil
    }),
))

// With default value
r.Use(headers.New("X-Environment", "environment",
    headers.WithDefault("production"),
))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    apiKey, ok := headers.FromContext(r.Context(), "api_key")
    if !ok {
        wrapper.SetError(r, wrapper.ErrUnauthorized.With("No API key"))
        return
    }
}
```

### Example: Tenant ID as UUID

```go
import (
    "github.com/google/uuid"
    "github.com/nhalm/chikit/headers"
)

// Extract X-Tenant-ID header as UUID with validation
r.Use(headers.New("X-Tenant-ID", "tenant_id",
    headers.WithRequired(),
    headers.WithValidator(func(val string) (any, error) {
        return uuid.Parse(val)
    }),
))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    val, ok := headers.FromContext(r.Context(), "tenant_id")
    if !ok {
        wrapper.SetError(r, wrapper.ErrBadRequest.With("No tenant ID"))
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
import "github.com/nhalm/chikit/validate"

// Limit request body to 1MB
r.Use(validate.MaxBodySize(1024 * 1024))
```

### Query Parameter Validation

Validate query parameters with inline rules:

```go
// Single parameter with validation
r.Use(validate.QueryParams(
    validate.Param("limit",
        validate.WithRequired(),
        validate.WithDefault("10"),
        validate.WithValidator(validate.OneOf("10", "25", "50", "100")),
    ),
))

// Multiple parameters with different rules
r.Use(validate.QueryParams(
    validate.Param("page", validate.WithDefault("1")),
    validate.Param("search", validate.WithValidator(validate.MinLength(3))),
    validate.Param("sort", validate.WithValidator(validate.OneOf("name", "date", "price"))),
))

// Custom validator
r.Use(validate.QueryParams(
    validate.Param("id", validate.WithValidator(func(val string) error {
        if _, err := strconv.Atoi(val); err != nil {
            return errors.New("must be a number")
        }
        return nil
    })),
))
```

Built-in validators:
- `OneOf(values...)` - Value must be in list
- `MinLength(n)` - Minimum string length
- `MaxLength(n)` - Maximum string length
- `Pattern(pattern)` - Regex pattern match

### Header Validation

Validate headers with allow/deny lists:

```go
// Required header
r.Use(validate.Headers(
    validate.Header("X-API-Key", validate.WithRequiredHeader()),
))

// Allow list (only specific values allowed)
r.Use(validate.Headers(
    validate.Header("X-Environment",
        validate.WithAllowList("production", "staging", "development"),
    ),
))

// Deny list (block specific values)
r.Use(validate.Headers(
    validate.Header("X-Source",
        validate.WithDenyList("blocked-client", "banned-user"),
    ),
))

// Case-sensitive validation (default: case-insensitive)
r.Use(validate.Headers(
    validate.Header("X-Auth-Token",
        validate.WithAllowList("Bearer", "Basic"),
        validate.WithCaseSensitive(),
    ),
))

// Multiple header rules
r.Use(validate.Headers(
    validate.Header("X-API-Key", validate.WithRequiredHeader()),
    validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
    validate.Header("X-Source", validate.WithDenyList("blocked")),
))
```

## Authentication

### API Key Authentication

Validate API keys with custom validators:

```go
import "github.com/nhalm/chikit/auth"

// Simple validator
validator := func(key string) bool {
    return key == "secret-key"
}

r.Use(auth.APIKey(validator))

// Custom header
r.Use(auth.APIKey(validator, auth.WithAPIKeyHeader("X-Custom-Key")))

// Optional API key
r.Use(auth.APIKey(validator, auth.WithOptionalAPIKey()))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    key, ok := auth.APIKeyFromContext(r.Context())
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

r.Use(auth.BearerToken(validator))

// Optional bearer token
r.Use(auth.BearerToken(validator, auth.WithOptionalBearerToken()))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    token, ok := auth.BearerTokenFromContext(r.Context())
    if ok {
        // Use bearer token
    }
}
```

## SLO Tracking

Track service level objectives with callback-based metrics. The middleware captures request metrics and calls your callback function, allowing integration with any observability system without forcing specific client libraries.

### Basic Usage

```go
import (
    "context"
    "github.com/nhalm/chikit/slo"
)

// Define callback to handle metrics
onMetric := func(ctx context.Context, m slo.Metric) {
    // m.Method: HTTP method (GET, POST, etc.)
    // m.Route: Chi route pattern ("/api/users/{id}")
    // m.StatusCode: HTTP status code
    // m.Duration: Request processing time

    log.Printf("method=%s route=%s status=%d duration=%v",
        m.Method, m.Route, m.StatusCode, m.Duration)
}

r.Use(slo.Track(onMetric))
```

### Prometheus Integration

Example showing how to adapt the callback for Prometheus:

```go
import (
    "context"
    "strconv"

    "github.com/nhalm/chikit/slo"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    httpDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "http_request_duration_seconds",
            Help: "HTTP request latency in seconds",
            Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
        },
        []string{"method", "route", "status"},
    )

    httpRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total number of HTTP requests",
        },
        []string{"method", "route", "status"},
    )
)

func prometheusCallback(ctx context.Context, m slo.Metric) {
    labels := prometheus.Labels{
        "method": m.Method,
        "route":  m.Route,
        "status": strconv.Itoa(m.StatusCode),
    }

    httpDuration.With(labels).Observe(m.Duration.Seconds())
    httpRequestsTotal.With(labels).Inc()
}

r.Use(slo.Track(prometheusCallback))
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/google/uuid"
    "github.com/nhalm/chikit/auth"
    "github.com/nhalm/chikit/bind"
    "github.com/nhalm/chikit/headers"
    "github.com/nhalm/chikit/ratelimit"
    "github.com/nhalm/chikit/ratelimit/store"
    "github.com/nhalm/chikit/slo"
    "github.com/nhalm/chikit/validate"
    "github.com/nhalm/chikit/wrapper"
)

type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=2,max=100"`
    Email string `json:"email" validate:"required,email"`
}

func main() {
    r := chi.NewRouter()

    // Standard Chi middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)

    // wrapper.Handler MUST be outermost chikit middleware
    r.Use(wrapper.Handler())

    // Limit request body size to 10MB
    r.Use(validate.MaxBodySize(10 * 1024 * 1024))

    // SLO tracking with callback
    r.Use(slo.Track(func(ctx context.Context, m slo.Metric) {
        log.Printf("method=%s route=%s status=%d duration=%v",
            m.Method, m.Route, m.StatusCode, m.Duration)
    }))

    // Validate environment header
    r.Use(validate.Headers(
        validate.Header("X-Environment",
            validate.WithAllowList("production", "staging", "development"),
        ),
    ))

    // Extract tenant ID from header
    r.Use(headers.New("X-Tenant-ID", "tenant_id",
        headers.WithRequired(),
        headers.WithValidator(func(val string) (any, error) {
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
    r.Use(ratelimit.NewBuilder(st).
        WithName("global").
        WithIP().
        Limit(1000, time.Hour))

    // API routes
    r.Route("/api/v1", func(r chi.Router) {
        // API key authentication
        r.Use(auth.APIKey(func(key string) bool {
            return validateAPIKey(key)
        }))

        // Per-tenant rate limiting: 100 requests per minute
        r.Use(ratelimit.NewBuilder(st).
            WithName("tenant").
            WithIP().
            WithHeader("X-Tenant-ID").
            Limit(100, time.Minute))

        r.Get("/users", listUsers)
        r.Post("/users", createUser)
    })

    log.Fatal(http.ListenAndServe(":8080", r))
}

func validateAPIKey(key string) bool {
    return true
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    val, ok := headers.FromContext(r.Context(), "tenant_id")
    if !ok {
        wrapper.SetError(r, wrapper.ErrBadRequest.With("No tenant ID"))
        return
    }
    tenantID := val.(uuid.UUID)
    wrapper.SetResponse(r, http.StatusOK, map[string]string{
        "tenant_id": tenantID.String(),
        "users":     "[]",
    })
}

func createUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := bind.JSON(r, &req); err != nil {
        wrapper.SetError(r, err.(*wrapper.Error))
        return
    }

    wrapper.SetResponse(r, http.StatusCreated, map[string]string{
        "name":  req.Name,
        "email": req.Email,
    })
}
```

## License

MIT
