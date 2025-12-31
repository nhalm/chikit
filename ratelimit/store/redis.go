// Package store provides storage backends for rate limiting.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrScript is a Lua script that atomically increments a counter and sets its expiration.
// This ensures that the INCR, EXPIRE, and TTL operations happen atomically without other
// clients interleaving commands. Returns [count, ttl] where count is the new value and
// ttl is the remaining time in seconds.
var incrScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

// Redis is a Redis-backed implementation of Store suitable for distributed deployments.
// Uses Redis atomic operations via Lua scripts to ensure rate limit accuracy across
// multiple instances in Kubernetes or other distributed environments.
type Redis struct {
	client *redis.Client
	prefix string
}

// RedisConfig holds configuration for Redis connection.
// All fields should be populated explicitly by your application code from environment
// variables, config files, or other sources. Never reads environment variables directly.
type RedisConfig struct {
	// URL is the Redis server address (e.g., "localhost:6379")
	URL string

	// Password for Redis authentication (optional, leave empty if not needed)
	Password string

	// DB is the Redis database number (0-15, default: 0)
	DB int

	// Prefix is prepended to all keys to namespace rate limit data (default: "ratelimit:")
	Prefix string

	// PoolSize is the maximum number of connections (default: 10 * runtime.GOMAXPROCS)
	PoolSize int

	// MinIdleConns is the minimum number of idle connections (default: 0)
	MinIdleConns int

	// DialTimeout is the timeout for establishing new connections (default: 5s)
	DialTimeout time.Duration

	// ReadTimeout is the timeout for socket reads (default: 3s)
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for socket writes (default: ReadTimeout)
	WriteTimeout time.Duration
}

// NewRedis creates a Redis store with the given configuration.
// Validates the connection with a ping before returning. Returns an error if
// the connection cannot be established within 5 seconds.
//
// Example:
//
//	store, err := store.NewRedis(store.RedisConfig{
//		URL:      "localhost:6379",
//		Password: "",
//		DB:       0,
//		Prefix:   "ratelimit:",
//	})
func NewRedis(config RedisConfig) (*Redis, error) {
	if config.Prefix == "" {
		config.Prefix = "ratelimit:"
	}

	opts := &redis.Options{
		Addr:     config.URL,
		Password: config.Password,
		DB:       config.DB,
	}

	if config.PoolSize > 0 {
		opts.PoolSize = config.PoolSize
	}
	if config.MinIdleConns > 0 {
		opts.MinIdleConns = config.MinIdleConns
	}
	if config.DialTimeout > 0 {
		opts.DialTimeout = config.DialTimeout
	}
	if config.ReadTimeout > 0 {
		opts.ReadTimeout = config.ReadTimeout
	}
	if config.WriteTimeout > 0 {
		opts.WriteTimeout = config.WriteTimeout
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &Redis{
		client: client,
		prefix: config.Prefix,
	}, nil
}

// Increment atomically increments the counter for the given key using a Lua script.
// The script ensures that INCR, EXPIRE, and TTL operations execute atomically without
// other clients interleaving commands. Returns the new count, time remaining until
// window reset, and any error.
func (r *Redis) Increment(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	fullKey := r.prefix + key

	result, err := incrScript.Run(ctx, r.client, []string{fullKey}, int(window.Seconds())).Slice()
	if err != nil {
		return 0, 0, fmt.Errorf("redis increment failed: %w", err)
	}

	if len(result) != 2 {
		return 0, 0, fmt.Errorf("unexpected result length: got %d, want 2", len(result))
	}

	count, ok := result[0].(int64)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected type for count: %T", result[0])
	}

	ttlSeconds, ok := result[1].(int64)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected type for ttl: %T", result[1])
	}

	ttl := time.Duration(ttlSeconds) * time.Second

	return count, ttl, nil
}

// Get retrieves the current count for the given key without incrementing.
// Returns 0 if the key doesn't exist or has expired.
func (r *Redis) Get(ctx context.Context, key string) (int64, error) {
	val, err := r.client.Get(ctx, r.prefix+key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("redis get failed: %w", err)
	}
	return val, nil
}

// Reset removes the counter for the given key.
func (r *Redis) Reset(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, r.prefix+key).Err(); err != nil {
		return fmt.Errorf("redis reset failed: %w", err)
	}
	return nil
}

// Close releases the Redis client connection.
func (r *Redis) Close() error {
	return r.client.Close()
}
