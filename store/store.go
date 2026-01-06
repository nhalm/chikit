// Package store provides storage backends for rate limiting.
//
// The Store interface allows different storage implementations for rate limit counters.
// Choose the implementation based on your deployment architecture:
//
//   - Memory: For development and single-instance deployments only
//   - Redis: For production distributed deployments (Kubernetes, multiple instances)
//
// Example with in-memory store:
//
//	store := store.NewMemory()
//	defer store.Close()
//
// Example with Redis:
//
//	store, err := store.NewRedis(store.RedisConfig{
//		URL:    "localhost:6379",
//		Prefix: "ratelimit:",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer store.Close()
package store

import (
	"context"
	"time"
)

// Store defines the interface for rate limit storage backends.
// Implementations must be safe for concurrent use and provide atomic operations
// for increment-and-expire to ensure accurate rate limiting in distributed systems.
//
// The Increment operation must be atomic to prevent race conditions where multiple
// concurrent requests could bypass the rate limit. Implementations should use
// appropriate locking (Memory) or atomic operations (Redis) to ensure correctness.
type Store interface {
	// Increment atomically increments the counter for the given key and returns:
	//   - count: The new count after incrementing
	//   - ttl: Time remaining until the window resets
	//   - err: Any error that occurred during the operation
	//
	// If the key doesn't exist or has expired, a new counter is created with
	// count=1 and an expiration set to the window duration. The operation must
	// be atomic to ensure accurate rate limiting under concurrent load.
	Increment(ctx context.Context, key string, window time.Duration) (count int64, ttl time.Duration, err error)

	// Get retrieves the current count for the given key without incrementing.
	// Returns 0 if the key doesn't exist or has expired.
	Get(ctx context.Context, key string) (int64, error)

	// Reset removes the counter for the given key.
	// This can be used to manually reset a rate limit for testing or administrative purposes.
	Reset(ctx context.Context, key string) error

	// Close releases any resources held by the store (connections, goroutines, etc.).
	Close() error
}
