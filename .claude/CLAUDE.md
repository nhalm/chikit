# Claude Configuration for chikit

## Project Overview

**chikit** is a production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (pgxkit, chikit) providing focused, high-quality Go libraries.

## Architecture

```
chikit/
├── api_error.go    # APIError type, FieldError, sentinels
├── state.go        # State, HasState
├── response.go     # SetError, SetResponse, SetHeader
├── handler.go      # Handler middleware + options
├── bind.go         # JSON, Query, RegisterValidation
├── ratelimit.go    # NewRateLimiter + options
├── auth.go         # APIKey, BearerToken + options
├── headers.go      # ExtractHeader + options
├── validate.go     # ValidateHeaders, MaxBodySize + options
├── slo.go          # SLO tracking
└── store/          # Rate limit backends
```

## Core Patterns

### Handler Middleware (Central to Architecture)

`chikit.Handler()` must be the outermost middleware. Key concepts:

1. **State in Context**: `chikit.Handler` stores a mutable `*State` in context at request start
2. **Middleware never writes responses**: All middleware uses `chikit.SetError()` instead of `http.Error()`
3. **Single response point**: `chikit.Handler` writes all JSON responses in its deferred cleanup
4. **Dual-mode support**: Middleware checks `chikit.HasState(ctx)` to support standalone use

```go
// Middleware pattern - always check for state
if chikit.HasState(r.Context()) {
    chikit.SetError(r, chikit.ErrUnauthorized.With("Invalid API key"))
} else {
    http.Error(w, "Invalid API key", http.StatusUnauthorized)
}
return // Always return after setting error
```

### Sentinel Errors

Predefined errors with `.With()` for custom messages:
- `chikit.ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`
- `chikit.ErrConflict`, `ErrUnprocessableEntity`, `ErrRateLimited`, `ErrInternal`

```go
chikit.SetError(r, chikit.ErrNotFound.With("User not found"))
chikit.SetError(r, chikit.ErrBadRequest.WithParam("Invalid format", "email"))
```

### Rate Limiting

```go
// Single dimension
limiter := chikit.NewRateLimiter(st, 100, time.Minute, chikit.RateLimitWithIP())
r.Use(limiter.Handler)

// Multi-dimensional with required/optional
limiter := chikit.NewRateLimiter(st, 100, time.Minute,
    chikit.RateLimitWithIP(),
    chikit.RateLimitWithHeaderRequired("X-Tenant-ID"),  // 400 if missing
    chikit.RateLimitWithQueryParam("user_id"),          // skip if missing
)
r.Use(limiter.Handler)
```

Use `RateLimitWithName()` for layered rate limiting to avoid key collisions.

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
