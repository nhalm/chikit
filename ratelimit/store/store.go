package store

import (
	"context"
	"time"
)

// Store defines the interface for rate limit storage backends.
// Implementations must be safe for concurrent use.
type Store interface {
	// Increment increments the counter for the given key and returns the new count,
	// the TTL until the window resets, and any error.
	// The counter should expire after the window duration.
	Increment(ctx context.Context, key string, window time.Duration) (count int64, ttl time.Duration, err error)

	// Get retrieves the current count for the given key without incrementing.
	// Returns 0 if the key doesn't exist.
	Get(ctx context.Context, key string) (int64, error)

	// Reset removes the counter for the given key.
	Reset(ctx context.Context, key string) error

	// Close releases any resources held by the store.
	Close() error
}
