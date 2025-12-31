# chikit

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/doc/install)
[![Go Reference](https://pkg.go.dev/badge/github.com/nhalm/chikit.svg)](https://pkg.go.dev/github.com/nhalm/chikit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (alongside [pgxkit](https://github.com/nhalm/pgxkit)) providing focused, high-quality Go libraries.

Follows 12-factor app principles with all configuration via explicit parameters—no config files, no environment variable access in middleware.

## Features

- **Response Wrapper**: Context-based response handling with Stripe-style structured errors
- **Flexible Rate Limiting**: Multi-dimensional rate limiting with Redis support for distributed deployments
- **Header Management**: Extract and validate headers with context injection
- **Request Validation**: Body size limits, query parameter validation, header allow/deny lists
- **Request Binding**: JSON body and query parameter binding with validation
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

The wrapper package provides context-based response handling. Handlers and middleware set responses in request context rather than writing directly to ResponseWriter, enabling consistent JSON responses and Stripe-style structured errors.

### Basic Usage

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit/wrapper"
)

func main() {
    r := chi.NewRouter()
    r.Use(wrapper.New())  // Must be outermost middleware

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

### Structured Errors

Errors follow Stripe's API error format:

```go
// Predefined sentinel errors
wrapper.ErrBadRequest          // 400
wrapper.ErrUnauthorized        // 401
wrapper.ErrForbidden           // 403
wrapper.ErrNotFound            // 404
wrapper.ErrConflict            // 409
wrapper.ErrUnprocessableEntity // 422
wrapper.ErrRateLimited         // 429
wrapper.ErrInternal            // 500

// Customize message
wrapper.SetError(r, wrapper.ErrNotFound.With("User not found"))

// Customize message and parameter
wrapper.SetError(r, wrapper.ErrBadRequest.WithParam("Invalid email format", "email"))
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
wrapper.SetError(r, wrapper.NewValidationError([]wrapper.FieldError{
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
wrapper.SetHeader(r, "X-Request-ID", requestID)
wrapper.SetHeader(r, "X-RateLimit-Remaining", "99")
wrapper.AddHeader(r, "X-Custom", "value1")
wrapper.AddHeader(r, "X-Custom", "value2")  // Adds second value
```

### Dual-Mode Middleware

Middleware can check if wrapper is present and fall back gracefully:

```go
func MyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if err := validate(r); err != nil {
            if wrapper.HasState(r.Context()) {
                wrapper.SetError(r, wrapper.ErrBadRequest.With(err.Error()))
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
r.Use(wrapper.New())

r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("something went wrong")  // Returns {"error": {"type": "internal_error", ...}}
})
```

## Rate Limiting

### Simple API

For common use cases, use the simple API:

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/nhalm/chikit/ratelimit"
    "github.com/nhalm/chikit/ratelimit/store"
    "time"
)

func main() {
    r := chi.NewRouter()

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

This pattern is useful for implementing tiered rate limits:

```go
// Tier 1: Broad protection (DDoS prevention)
r.Use(ratelimit.NewBuilder(st).
    WithName("ddos").
    WithIP().
    Limit(10000, time.Hour))

// Tier 2: API endpoint protection
r.Route("/api", func(r chi.Router) {
    r.Use(ratelimit.NewBuilder(st).
        WithName("api").
        WithIP().
        WithEndpoint().
        Limit(100, time.Minute))
})

// Tier 3: Expensive operation protection
r.Route("/api/analytics/run", func(r chi.Router) {
    r.Use(ratelimit.NewBuilder(st).
        WithName("analytics").
        WithHeader("X-User-ID").
        Limit(5, time.Hour))
    r.Post("/", analyticsHandler)
})
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
        http.Error(w, "No API key", http.StatusUnauthorized)
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
import "github.com/nhalm/chikit/validate"

// Limit request body to 1MB
r.Use(validate.MaxBodySize(1024 * 1024))
```

### Header Validation

Validate headers with allow/deny lists:

```go
// Required header
r.Use(validate.NewHeaders(
    validate.WithHeader("X-API-Key", validate.WithRequired()),
))

// Allow list (only specific values allowed)
r.Use(validate.NewHeaders(
    validate.WithHeader("X-Environment",
        validate.WithAllowList("production", "staging", "development"),
    ),
))

// Deny list (block specific values)
r.Use(validate.NewHeaders(
    validate.WithHeader("X-Source",
        validate.WithDenyList("blocked-client", "banned-user"),
    ),
))

// Case-sensitive validation (default: case-insensitive)
r.Use(validate.NewHeaders(
    validate.WithHeader("X-Auth-Token",
        validate.WithAllowList("Bearer", "Basic"),
        validate.WithCaseSensitive(),
    ),
))

// Multiple header rules
r.Use(validate.NewHeaders(
    validate.WithHeader("X-API-Key", validate.WithRequired()),
    validate.WithHeader("X-Environment", validate.WithAllowList("production", "staging")),
    validate.WithHeader("X-Source", validate.WithDenyList("blocked")),
))
```

## Request Binding

The bind package provides JSON body and query parameter binding with validation using go-playground/validator/v10.

### JSON Binding

```go
import (
    "github.com/nhalm/chikit/bind"
    "github.com/nhalm/chikit/wrapper"
)

type CreateUserRequest struct {
    Email string `json:"email" validate:"required,email"`
    Name  string `json:"name" validate:"required,min=2"`
    Age   int    `json:"age" validate:"omitempty,min=18"`
}

func main() {
    r := chi.NewRouter()
    r.Use(wrapper.New())
    r.Use(bind.New())

    r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
        var req CreateUserRequest
        if !bind.JSON(r, &req) {
            return  // Validation error already set in wrapper
        }
        // Use req.Email, req.Name, req.Age
        wrapper.SetResponse(r, http.StatusCreated, user)
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
    if !bind.Query(r, &query) {
        return  // Validation error already set in wrapper
    }
    // Use query.Page, query.Limit, query.Search
})
```

### Custom Validation Messages

```go
r.Use(bind.New(bind.WithFormatter(func(field, tag, param string) string {
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
    bind.RegisterValidation("customtag", func(fl validator.FieldLevel) bool {
        return fl.Field().String() == "valid"
    })
}
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

r.Use(slo.New(onMetric))
```

### Prometheus Integration

Example showing how to adapt the callback for Prometheus (prometheus/client_golang is not a dependency):

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

r.Use(slo.New(prometheusCallback))
```

### Datadog Integration

Example showing how to adapt the callback for Datadog (DataDog/datadog-go is not a dependency):

```go
import (
    "context"
    "fmt"

    "github.com/DataDog/datadog-go/v5/statsd"
    "github.com/nhalm/chikit/slo"
)

func datadogCallback(client *statsd.Client) func(context.Context, slo.Metric) {
    return func(ctx context.Context, m slo.Metric) {
        tags := []string{
            fmt.Sprintf("method:%s", m.Method),
            fmt.Sprintf("route:%s", m.Route),
            fmt.Sprintf("status:%d", m.StatusCode),
        }

        // Send timing metric
        client.Timing("http.request.duration", m.Duration, tags, 1.0)

        // Increment request counter
        client.Incr("http.request.count", tags, 1.0)

        // Track errors
        if m.StatusCode >= 500 {
            client.Incr("http.request.errors", tags, 1.0)
        }
    }
}

// Usage
ddClient, _ := statsd.New("127.0.0.1:8125")
r.Use(slo.New(datadogCallback(ddClient)))
```

### OpenTelemetry Integration

Example showing how to adapt the callback for OpenTelemetry:

```go
import (
    "context"
    "strconv"

    "github.com/nhalm/chikit/slo"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

func otelCallback(meter metric.Meter) func(context.Context, slo.Metric) {
    histogram, _ := meter.Float64Histogram(
        "http.server.request.duration",
        metric.WithUnit("s"),
        metric.WithDescription("HTTP request duration"),
    )

    counter, _ := meter.Int64Counter(
        "http.server.request.count",
        metric.WithDescription("Total HTTP requests"),
    )

    return func(ctx context.Context, m slo.Metric) {
        attrs := []attribute.KeyValue{
            attribute.String("http.method", m.Method),
            attribute.String("http.route", m.Route),
            attribute.Int("http.status_code", m.StatusCode),
        }

        histogram.Record(ctx, m.Duration.Seconds(), metric.WithAttributes(attrs...))
        counter.Add(ctx, 1, metric.WithAttributes(attrs...))
    }
}

// Usage
meter := otel.Meter("chikit")
r.Use(slo.New(otelCallback(meter)))
```

### Per-Route Tracking

Track metrics for specific routes only:

```go
r.Route("/api/v1", func(r chi.Router) {
    r.Use(slo.New(onMetric))  // Track only /api/v1 routes
    r.Get("/users", listUsers)
    r.Post("/users", createUser)
})
```

### Panic Handling

The middleware automatically handles panics, recording them as 500 errors before re-raising:

```go
onMetric := func(ctx context.Context, m slo.Metric) {
    if m.StatusCode >= 500 {
        // Log or alert on server errors
        log.Printf("ERROR: %s %s returned %d", m.Method, m.Route, m.StatusCode)
    }
}

r.Use(slo.New(onMetric))
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

type ListUsersQuery struct {
    Page  int `query:"page" validate:"omitempty,min=1"`
    Limit int `query:"limit" validate:"omitempty,min=1,max=100"`
}

func main() {
    r := chi.NewRouter()

    // Standard Chi middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)

    // Wrapper for context-based response handling
    r.Use(wrapper.New())

    // Bind middleware for request binding/validation
    r.Use(bind.New())

    // Limit request body size to 10MB
    r.Use(validate.MaxBodySize(10 * 1024 * 1024))

    // SLO tracking with callback
    r.Use(slo.New(func(ctx context.Context, m slo.Metric) {
        log.Printf("method=%s route=%s status=%d duration=%v",
            m.Method, m.Route, m.StatusCode, m.Duration)
    }))

    // Validate environment header
    r.Use(validate.NewHeaders(
        validate.WithHeader("X-Environment",
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
    // Implement your API key validation
    return true
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    val, ok := headers.FromContext(r.Context(), "tenant_id")
    if !ok {
        wrapper.SetError(r, wrapper.ErrBadRequest.With("No tenant ID"))
        return
    }
    tenantID := val.(uuid.UUID)

    var query ListUsersQuery
    if !bind.Query(r, &query) {
        return
    }

    // Query users for tenant...
    wrapper.SetResponse(r, http.StatusOK, map[string]any{
        "tenant": tenantID.String(),
        "page":   query.Page,
        "limit":  query.Limit,
    })
}

func createUser(w http.ResponseWriter, r *http.Request) {
    val, ok := headers.FromContext(r.Context(), "tenant_id")
    if !ok {
        wrapper.SetError(r, wrapper.ErrBadRequest.With("No tenant ID"))
        return
    }
    tenantID := val.(uuid.UUID)

    // Create user for tenant...
    wrapper.SetResponse(r, http.StatusCreated, map[string]any{
        "tenant": tenantID.String(),
    })
}
```


## License

MIT
