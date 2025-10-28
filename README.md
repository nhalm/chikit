# chikit

Production-grade Chi middleware library for distributed systems. Follows 12-factor app principles with all configuration via code or environment variables.

## Features

- **Flexible Rate Limiting**: Multi-dimensional rate limiting with Redis support for distributed deployments
- **Header Management**: Extract and validate headers with context injection
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

### Tenant ID Middleware

Compatible with paymentlinks pattern:

```go
import "github.com/nhalm/chikit/headers"

// Extract X-Tenant-ID header as UUID
r.Use(headers.TenantID(true, ""))

// With default tenant ID for development
r.Use(headers.TenantID(false, "00000000-0000-0000-0000-000000000001"))

// Retrieve in handler
func handler(w http.ResponseWriter, r *http.Request) {
    tenantID, ok := headers.TenantIDFromContext(r.Context())
    if !ok {
        http.Error(w, "No tenant ID", http.StatusBadRequest)
        return
    }
    // Use tenantID...
}
```

### Generic Header to Context

Extract any header with validation:

```go
// Simple header extraction
r.Use(headers.New("X-API-Key", "api_key"))

// With validation
r.Use(headers.New("X-Correlation-ID", "correlation_id",
    headers.Required(),
    headers.WithValidator(func(val string) (interface{}, error) {
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

## Complete Example

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/nhalm/chikit/headers"
    "github.com/nhalm/chikit/ratelimit"
    "github.com/nhalm/chikit/ratelimit/store"
)

func main() {
    r := chi.NewRouter()

    // Standard Chi middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    // Extract tenant ID from header
    r.Use(headers.TenantID(false, "00000000-0000-0000-0000-000000000001"))

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

        r.Get("/users", listUsers)
        r.Post("/users", createUser)
    })

    log.Fatal(http.ListenAndServe(":8080", r))
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    tenantID, _ := headers.TenantIDFromContext(r.Context())
    // Query users for tenant...
    w.Write([]byte("Users for tenant: " + tenantID.String()))
}

func createUser(w http.ResponseWriter, r *http.Request) {
    tenantID, _ := headers.TenantIDFromContext(r.Context())
    // Create user for tenant...
    w.WriteHeader(http.StatusCreated)
}
```


## Roadmap

### Phase 2: Security & Error Handling
- Error response sanitization
- Query parameter validation
- Request size limits

### Phase 3: Authentication & Observability
- API key validation
- Bearer token extraction
- SLO tracking (latency percentiles, error rates)
- OpenTelemetry integration

## License

MIT
