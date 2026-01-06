package store

import (
	"context"
	"sync"
	"time"
)

type memoryEntry struct {
	count      int64
	expiration time.Time
}

// Memory is an in-memory implementation of Store using a map with mutex protection.
//
// WARNING: This implementation is NOT suitable for distributed deployments.
// In Kubernetes or any multi-instance environment, each instance maintains its own
// separate in-memory state, meaning rate limits are NOT shared across instances.
// This can allow clients to exceed the intended rate limit by distributing requests
// across multiple instances.
//
// Use Memory only for:
//   - Local development and testing
//   - Single-instance deployments where horizontal scaling is not needed
//
// For production distributed systems, use the Redis store instead.
type Memory struct {
	mu      sync.RWMutex
	entries map[string]*memoryEntry
	stopCh  chan struct{}
}

// NewMemory creates a new in-memory store with automatic cleanup of expired entries.
// A background goroutine runs every minute to remove expired entries and prevent
// unbounded memory growth.
//
// Important: You must call Close() when done to stop the cleanup goroutine.
// Failing to call Close() will result in a goroutine leak.
func NewMemory() *Memory {
	m := &Memory{
		entries: make(map[string]*memoryEntry),
		stopCh:  make(chan struct{}),
	}

	go m.cleanup()
	return m
}

// Increment atomically increments the counter for the given key and returns the new count, TTL, and any error.
// If the key doesn't exist or has expired, creates a new entry with count=1.
// The operation is atomic due to the write lock, ensuring accuracy under concurrent load.
//
// Note: The context parameter is accepted for interface compatibility but is not used.
// In-memory operations complete immediately and cannot be cancelled.
func (m *Memory) Increment(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	entry, exists := m.entries[key]

	if !exists || now.After(entry.expiration) {
		expiration := now.Add(window)
		m.entries[key] = &memoryEntry{
			count:      1,
			expiration: expiration,
		}
		return 1, window, nil
	}

	entry.count++
	ttl := max(0, time.Until(entry.expiration))
	return entry.count, ttl, nil
}

// Get retrieves the current count for the given key without incrementing.
// Returns 0 if the key doesn't exist or has expired.
func (m *Memory) Get(_ context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[key]
	if !exists || time.Now().After(entry.expiration) {
		return 0, nil
	}

	return entry.count, nil
}

// Reset removes the counter for the given key.
func (m *Memory) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, key)
	return nil
}

// Close stops the background cleanup goroutine and releases resources.
func (m *Memory) Close() error {
	close(m.stopCh)
	m.mu.Lock()
	m.entries = nil
	m.mu.Unlock()
	return nil
}

// runCleanup executes a single cleanup cycle, removing all expired entries.
// This is exposed for testing purposes to trigger cleanup without waiting for the ticker.
func (m *Memory) runCleanup() {
	now := time.Now()
	var expiredKeys []string

	m.mu.RLock()
	for key, entry := range m.entries {
		if now.After(entry.expiration) {
			expiredKeys = append(expiredKeys, key)
		}
	}
	m.mu.RUnlock()

	if len(expiredKeys) > 0 {
		m.mu.Lock()
		now := time.Now()
		for _, key := range expiredKeys {
			if entry, exists := m.entries[key]; exists && now.After(entry.expiration) {
				delete(m.entries, key)
			}
		}
		m.mu.Unlock()
	}
}

func (m *Memory) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.runCleanup()
		case <-m.stopCh:
			return
		}
	}
}
