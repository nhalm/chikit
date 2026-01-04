# Claude Configuration for chikit

## Project Overview

**chikit** is a production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (pgxkit, chikit) providing focused, high-quality Go libraries.

## Architecture

```
chikit/
├── wrapper/             # Context-based response handling, Stripe-style errors
├── ratelimit/           # Rate limiting with Redis/memory backends
│   └── store/           # Storage interface + implementations
├── headers/             # Header extraction with context injection
├── validate/            # Body size, query params, header validation
├── auth/                # API key and bearer token authentication
└── slo/                 # SLO tracking with callbacks
```

## Core Patterns

### Wrapper Package (Central to Architecture)

The `wrapper` package is the foundation. `wrapper.Handler()` must be the outermost middleware. Key concepts:

1. **State in Context**: `wrapper.Handler` stores a mutable `*State` in context at request start
2. **Middleware never writes responses**: All middleware uses `wrapper.SetError()` instead of `http.Error()`
3. **Single response point**: `wrapper.Handler` writes all JSON responses in its deferred cleanup
4. **Dual-mode support**: Middleware checks `wrapper.HasState(ctx)` to support standalone use

```go
// Middleware pattern - always check for wrapper state
if wrapper.HasState(r.Context()) {
    wrapper.SetError(r, wrapper.ErrUnauthorized.With("Invalid API key"))
} else {
    http.Error(w, "Invalid API key", http.StatusUnauthorized)
}
return // Always return after setting error
```

### Sentinel Errors

Predefined errors with `.With()` for custom messages:
- `wrapper.ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`
- `wrapper.ErrConflict`, `ErrUnprocessableEntity`, `ErrRateLimited`, `ErrInternal`

```go
wrapper.SetError(r, wrapper.ErrNotFound.With("User not found"))
wrapper.SetError(r, wrapper.ErrBadRequest.WithParam("Invalid format", "email"))
```

### Rate Limiting

```go
// Single dimension
limiter := ratelimit.New(st, 100, time.Minute, ratelimit.WithIP())

// Multi-dimensional with required/optional
limiter := ratelimit.New(st, 100, time.Minute,
    ratelimit.WithIP(),
    ratelimit.WithHeader("X-Tenant-ID", true),   // required=true: 400 if missing
    ratelimit.WithQueryParam("user_id", false),  // required=false: skip if missing
)
r.Use(limiter.Handler)
```

Use `WithName()` for layered rate limiting to avoid key collisions.

### Storage Backends

- `store.NewMemory()` - Development/testing only
- `store.NewRedis(RedisConfig{...})` - Production, distributed deployments

## Code Standards

- **Go 1.24+** - Use modern features
- **`any` not `interface{}`** - Modern Go idiom
- **Explicit parameters** - Never read env vars or config files
- **Comprehensive tests** - Use `httptest`, in-memory stores
- **Minimal dependencies** - Standard library preferred

## Before Committing

Run these before every commit:

```bash
gofmt -w .
go test ./...
golangci-lint run ./...
```

## After Creating a PR

Wait for CI checks to pass before requesting review. If CI fails:

```bash
gh pr checks <pr-number>         # Check status
go test -v -race ./...           # Run tests with race detection
golangci-lint run ./...          # Run linter locally
```
