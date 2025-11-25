package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Redis-backed implementation of Store suitable for distributed deployments.
// Uses Redis atomic operations (INCR, EXPIRE) to ensure rate limit accuracy across
// multiple instances in Kubernetes or other distributed environments. All operations
// use pipelining for efficiency.
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
	client := redis.NewClient(&redis.Options{
		Addr:     config.URL,
		Password: config.Password,
		DB:       config.DB,
	})

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

// Increment atomically increments the counter for the given key using Redis INCR and EXPIRENX.
// Uses pipelining to batch INCR, EXPIRENX, and TTL commands into a single round-trip to Redis.
// Note: While pipelining reduces network latency, it does not provide atomicity - other clients
// may interleave commands between the pipelined operations. For true atomicity, consider using
// Redis Lua scripts if needed.
// Returns the new count, time remaining until window reset, and any error.
func (r *Redis) Increment(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	fullKey := r.prefix + key

	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, fullKey)
	pipe.ExpireNX(ctx, fullKey, window)
	ttlCmd := pipe.TTL(ctx, fullKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, fmt.Errorf("redis increment failed: %w", err)
	}

	ttl := min(window, ttlCmd.Val())

	return incr.Val(), ttl, nil
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
