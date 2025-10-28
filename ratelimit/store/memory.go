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

// Memory is an in-memory implementation of Store.
// Suitable for single-instance deployments and development.
type Memory struct {
	mu      sync.RWMutex
	entries map[string]*memoryEntry
	stopCh  chan struct{}
}

// NewMemory creates a new in-memory store with automatic cleanup of expired entries.
func NewMemory() *Memory {
	m := &Memory{
		entries: make(map[string]*memoryEntry),
		stopCh:  make(chan struct{}),
	}

	go m.cleanup()
	return m
}

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

func (m *Memory) Get(_ context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[key]
	if !exists || time.Now().After(entry.expiration) {
		return 0, nil
	}

	return entry.count, nil
}

func (m *Memory) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, key)
	return nil
}

func (m *Memory) Close() error {
	close(m.stopCh)
	return nil
}

func (m *Memory) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
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
				for _, key := range expiredKeys {
					delete(m.entries, key)
				}
				m.mu.Unlock()
			}
		case <-m.stopCh:
			return
		}
	}
}
