# Claude Configuration for chikit

## Project Overview

**chikit** is a production-grade Chi middleware library for distributed systems. Part of the *kit ecosystem (pgxkit, chikit) providing focused, high-quality Go libraries.

## Core Principles

### Initialization Philosophy
- Accept explicit parameters/structs in constructors
- **Never** read environment variables, config files, or any external sources
- Application handles initialization - chikit provides building blocks
- Stateless, horizontally scalable

### Distributed-First
- Redis backend required for Kubernetes/multi-instance deployments
- In-memory store only for development/single-instance
- All features must work correctly in distributed environments

### API Design Philosophy
- **Simple API first**: Pre-built functions for 90% of use cases (`ByIP`, `ByHeader`, etc.)
- **Advanced API for power users**: Fluent builder for complex multi-dimensional scenarios
- **Composable**: Middleware should chain naturally with Chi patterns
- **Explicit over implicit**: No magic, clear behavior

## Architecture

```
chikit/
├── ratelimit/           # Rate limiting middleware
│   ├── ratelimit.go     # Simple + fluent APIs
│   └── store/           # Storage backends
│       ├── store.go     # Interface
│       ├── memory.go    # In-memory (dev only)
│       └── redis.go     # Redis (production)
├── headers/             # Header extraction/validation
├── slo/                 # SLO tracking (future)
└── sanitize/            # Error sanitization
```

## Code Standards

### Go Version & Idioms
- **Go version**: 1.24+ minimum (use modern Go features)
- **Always use `any` instead of `interface{}`** - Modern Go idiom (1.18+)
- Use generics where appropriate
- Prefer standard library over external dependencies

### Middleware Patterns
```go
// Simple API: Pre-built common cases
func ByIP(store Store, limit int, window time.Duration) func(http.Handler) http.Handler

// Fluent API: Complex scenarios
func NewBuilder(store Store) *Builder
builder.WithIP().WithHeader("X-Tenant-ID").Limit(100, time.Minute)
```

### Testing Requirements
- All middleware must have comprehensive tests
- Test both simple and advanced APIs
- Test rate limit headers, error cases, missing values
- Use `httptest` for HTTP testing
- No external dependencies in tests (use in-memory store)

### Documentation
- Every exported function needs a comment
- README must have examples for all major features
- Include K8s deployment examples
- Show both development (in-memory) and production (Redis) usage

## Common Tasks

### Adding New Middleware

1. Create package under root (e.g., `sanitize/`, `auth/`)
2. Follow the two-tier API pattern:
   - Simple functions for common cases
   - Builder for complex cases
3. Support distributed deployments (use context, avoid local state)
4. Write comprehensive tests
5. Add examples to README
6. Update roadmap if this was a planned feature

### Adding New Storage Backend

1. Implement `store.Store` interface in `ratelimit/store/`
2. Accept explicit struct in constructor (e.g., `PostgresConfig`, `DynamoDBConfig`)
3. Add connection testing/health checks
4. Document required fields in README
5. Add error handling for connection failures
6. **Never** read environment variables, config files, or any external source

### Rate Limiting Guidelines

- Use sliding window algorithm (current implementation)
- Always set standard headers: `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`, `Retry-After`
- Return 429 when limit exceeded
- Support empty keys (skip rate limiting if key function returns "")
- Keys should be human-readable for Redis debugging: `ip:192.168.1.1`, `header:X-API-Key:abc123`

## Dependencies

**Minimize dependencies** - only add when necessary:
- `github.com/redis/go-redis/v9` - Redis client (required)
- `github.com/google/uuid` - UUID parsing for tenant headers
- `github.com/go-chi/chi/v5` - For examples only, not required

Don't add:
- Web frameworks besides Chi
- Logging libraries (let users bring their own)
- Config file parsers (against 12-factor)
- Metrics libraries (provide hooks instead)

## Compatibility

- **Go version**: 1.24+ minimum (use modern Go features)
- **Chi version**: v5 (current stable)
- **Redis version**: 6.0+ (for Lua script support if needed)

## Security Considerations

- Never log rate limit keys containing sensitive data
- Support for stripping sensitive headers in error responses (future)
- Rate limiting must be distributed-safe (no race conditions)
- Validate all header values before using in keys
- Default to safe behavior (fail closed on errors)

## Performance Targets

- Rate limit check: < 1ms (in-memory), < 5ms (Redis)
- Memory usage: O(n) where n = number of unique keys
- Redis: Use pipelining for atomic increment + expire
- Support high concurrency (10k+ req/s per instance)

## Future Features (Roadmap)

### Phase 2: Security & Error Handling
- `sanitize.New()` - Strip stack traces, internal paths from error responses
- `validate.QueryParams()` - Query parameter validation with inline rules
- `validate.Headers()` - Header validation with allow/deny lists
- `validate.MaxBodySize()` - Request size limits

### Phase 3: Authentication & Observability
- `auth.APIKey()` - API key validation with custom validator
- `auth.BearerToken()` - Bearer token extraction and validation
- `slo.Track()` - Latency percentiles, error rates, availability tracking
- Metric export callbacks (Prometheus, Datadog compatible)

## Anti-Patterns

**Don't:**
- Read environment variables (`os.Getenv`, `os.LookupEnv`)
- Parse files (JSON, YAML, TOML, etc.)
- Access any external source (Consul, etcd, etc.)
- Discuss or document "config management" or "configuration"
- Force specific logging/metrics libraries
- Create tight coupling with specific frameworks
- Add middleware that requires specific deployment environments
- Use global state or singletons
- Create breaking changes without major version bump

**Do:**
- Accept explicit parameters in constructors
- Provide interfaces for extensibility
- Keep packages focused and independent
- Support graceful degradation
- Write examples showing real-world usage

## Related Projects

- **pgxkit**: Database utilities (same *kit naming pattern)
- **paymentlinks**: Consumer of chikit (tenant middleware extracted from here)

When adding features, consider if they'd be useful across the *kit ecosystem.
