package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Redis-backed implementation of Store.
// Suitable for distributed deployments in Kubernetes.
type Redis struct {
	client *redis.Client
	prefix string
}

// RedisConfig holds configuration for Redis connection.
// Populate from environment variables in your application code.
type RedisConfig struct {
	URL      string
	Password string
	DB       int
	Prefix   string
}

// NewRedis creates a Redis store with the given configuration.
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

// Increment increments the counter for the given key and returns the new count, TTL, and any error.
func (r *Redis) Increment(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	fullKey := r.prefix + key

	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, fullKey)
	pipe.ExpireNX(ctx, fullKey, window)
	ttlCmd := pipe.TTL(ctx, fullKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, fmt.Errorf("redis increment failed: %w", err)
	}

	ttl := max(window, ttlCmd.Val())

	return incr.Val(), ttl, nil
}

// Get retrieves the current count for the given key without incrementing.
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

// Close releases resources held by the Redis client.
func (r *Redis) Close() error {
	return r.client.Close()
}
