# chikit

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/doc/install)
[![Go Reference](https://pkg.go.dev/badge/github.com/nhalm/chikit.svg)](https://pkg.go.dev/github.com/nhalm/chikit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (alongside [pgxkit](https://github.com/nhalm/pgxkit)) providing focused, high-quality Go libraries.

Follows 12-factor app principles with all configuration via explicit parameters—no config files, no environment variable access in middleware.

## Features

- **Flexible Rate Limiting**: Multi-dimensional rate limiting with Redis support for distributed deployments
- **Header Management**: Extract and validate headers with context injection
- **Request Validation**: Body size limits, query parameter validation, header allow/deny lists
- **Error Sanitization**: Strip sensitive information from error responses
- **Zero Config Files**: Pure code configuration, environment variable support
- **Distributed-Ready**: Redis backend for Kubernetes deployments
- **Fluent API**: Chainable, readable middleware configuration

## Installation

```bash
go get github.com/nhalm/chikit
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

All rate limiters automatically set standard headers:

```
RateLimit-Limit: 100
RateLimit-Remaining: 95
RateLimit-Reset: 1735401600
Retry-After: 60
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
    headers.Required(),
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
    headers.Required(),
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

## Error Sanitization

Strip sensitive information from error responses to prevent information leakage:

```go
import "github.com/nhalm/chikit/errors"

// Default: strips stack traces and file paths from 4xx/5xx responses
r.Use(errors.Sanitize())

// Custom configuration
r.Use(errors.Sanitize(
    errors.WithStackTraces(true),  // Strip stack traces (default: true)
    errors.WithFilePaths(true),    // Strip file paths (default: true)
    errors.WithReplacementMessage("An error occurred"),
))
```

The sanitizer automatically:
- Buffers error responses (4xx/5xx status codes)
- Removes stack traces (goroutine info, line numbers)
- Strips file paths (Unix and Windows formats)
- Preserves user-facing error messages
- Returns replacement message if all content stripped

## Request Validation

### Body Size Limits

Prevent DoS attacks by limiting request body size:

```go
import "github.com/nhalm/chikit/validate"

// Limit request body to 1MB
r.Use(validate.MaxBodySize(1024 * 1024))

// With custom error message
r.Use(validate.MaxBodySize(1024 * 1024,
    validate.WithBodySizeMessage("Payload exceeds 1MB limit"),
))
```

### Query Parameter Validation

Validate query parameters with inline rules:

```go
// Single parameter with validation
r.Use(validate.QueryParams(
    validate.Param("limit",
        validate.Required(),
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
    validate.Header("X-API-Key", validate.RequiredHeader()),
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
        validate.CaseSensitive(),
    ),
))

// Multiple header rules
r.Use(validate.Headers(
    validate.Header("X-API-Key", validate.RequiredHeader()),
    validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
    validate.Header("X-Source", validate.WithDenyList("blocked")),
))
```

## Complete Example

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/google/uuid"
    "github.com/nhalm/chikit/errors"
    "github.com/nhalm/chikit/headers"
    "github.com/nhalm/chikit/ratelimit"
    "github.com/nhalm/chikit/ratelimit/store"
    "github.com/nhalm/chikit/validate"
)

func main() {
    r := chi.NewRouter()

    // Standard Chi middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    // Sanitize errors to prevent information leakage
    r.Use(errors.Sanitize())

    // Limit request body size to 10MB
    r.Use(validate.MaxBodySize(10 * 1024 * 1024))

    // Validate environment header
    r.Use(validate.Headers(
        validate.Header("X-Environment",
            validate.WithAllowList("production", "staging", "development"),
        ),
    ))

    // Extract tenant ID from header
    r.Use(headers.New("X-Tenant-ID", "tenant_id",
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
    r.Use(ratelimit.ByIP(st, 1000, time.Hour))

    // API routes
    r.Route("/api/v1", func(r chi.Router) {
        // Per-tenant rate limiting: 100 requests per minute
        r.Use(ratelimit.NewBuilder(st).
            WithIP().
            WithHeader("X-Tenant-ID").
            Limit(100, time.Minute))

        // Validate query parameters
        r.Use(validate.QueryParams(
            validate.Param("page", validate.WithDefault("1")),
            validate.Param("limit", validate.WithDefault("25")),
        ))

        r.Get("/users", listUsers)
        r.Post("/users", createUser)
    })

    log.Fatal(http.ListenAndServe(":8080", r))
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    val, _ := headers.FromContext(r.Context(), "tenant_id")
    tenantID := val.(uuid.UUID)
    // Query users for tenant...
    w.Write([]byte("Users for tenant: " + tenantID.String()))
}

func createUser(w http.ResponseWriter, r *http.Request) {
    val, _ := headers.FromContext(r.Context(), "tenant_id")
    tenantID := val.(uuid.UUID)
    // Create user for tenant...
    w.WriteHeader(http.StatusCreated)
}
```


## License

MIT
