package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemory_Increment(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Memory)
		key     string
		window  time.Duration
		want    int64
		wantErr bool
	}{
		{
			name:   "first increment creates new entry",
			key:    "test:key",
			window: time.Minute,
			want:   1,
		},
		{
			name: "increment existing key",
			setup: func(m *Memory) {
				m.entries["test:key"] = &memoryEntry{
					count:      5,
					expiration: time.Now().Add(time.Minute),
				}
			},
			key:    "test:key",
			window: time.Minute,
			want:   6,
		},
		{
			name: "increment expired key resets counter",
			setup: func(m *Memory) {
				m.entries["test:key"] = &memoryEntry{
					count:      10,
					expiration: time.Now().Add(-time.Second),
				}
			},
			key:    "test:key",
			window: time.Minute,
			want:   1,
		},
		{
			name:   "empty key",
			key:    "",
			window: time.Minute,
			want:   1,
		},
		{
			name:   "zero window duration",
			key:    "test:key",
			window: 0,
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Memory{
				entries: make(map[string]*memoryEntry),
				stopCh:  make(chan struct{}),
			}
			defer m.Close()

			if tt.setup != nil {
				tt.setup(m)
			}

			got, _, err := m.Increment(context.Background(), tt.key, tt.window)
			if (err != nil) != tt.wantErr {
				t.Errorf("Increment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Increment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemory_Increment_Sequential(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:sequential"
	window := time.Minute

	for i := int64(1); i <= 10; i++ {
		got, _, err := m.Increment(ctx, key, window)
		if err != nil {
			t.Fatalf("Increment() error = %v", err)
		}
		if got != i {
			t.Errorf("Increment() = %v, want %v", got, i)
		}
	}
}

func TestMemory_Increment_Concurrent(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:concurrent"
	window := time.Minute
	goroutines := 10
	incrementsPerGoroutine := 10
	expectedTotal := int64(goroutines * incrementsPerGoroutine)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				if _, _, err := m.Increment(ctx, key, window); err != nil {
					t.Errorf("Increment() error = %v", err)
				}
			}
		}()
	}

	wg.Wait()

	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != expectedTotal {
		t.Errorf("Get() = %v, want %v", got, expectedTotal)
	}
}

func TestMemory_Increment_ConcurrentDifferentKeys(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	window := time.Minute
	keys := 10
	incrementsPerKey := 5

	var wg sync.WaitGroup
	wg.Add(keys)

	for i := 0; i < keys; i++ {
		key := "test:key:" + string(rune(i))
		go func(k string) {
			defer wg.Done()
			for j := 0; j < incrementsPerKey; j++ {
				if _, _, err := m.Increment(ctx, k, window); err != nil {
					t.Errorf("Increment() error = %v", err)
				}
			}
		}(key)
	}

	wg.Wait()

	for i := 0; i < keys; i++ {
		key := "test:key:" + string(rune(i))
		got, err := m.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%s) error = %v", key, err)
		}
		if got != int64(incrementsPerKey) {
			t.Errorf("Get(%s) = %v, want %v", key, got, incrementsPerKey)
		}
	}
}

func TestMemory_Get(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Memory)
		key     string
		want    int64
		wantErr bool
	}{
		{
			name: "non-existent key returns zero",
			key:  "test:nonexistent",
			want: 0,
		},
		{
			name: "existing key returns count",
			setup: func(m *Memory) {
				m.entries["test:key"] = &memoryEntry{
					count:      42,
					expiration: time.Now().Add(time.Minute),
				}
			},
			key:  "test:key",
			want: 42,
		},
		{
			name: "expired key returns zero",
			setup: func(m *Memory) {
				m.entries["test:key"] = &memoryEntry{
					count:      100,
					expiration: time.Now().Add(-time.Second),
				}
			},
			key:  "test:key",
			want: 0,
		},
		{
			name: "empty key returns zero",
			key:  "",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Memory{
				entries: make(map[string]*memoryEntry),
				stopCh:  make(chan struct{}),
			}
			defer m.Close()

			if tt.setup != nil {
				tt.setup(m)
			}

			got, err := m.Get(context.Background(), tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemory_Reset(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Memory)
		key     string
		wantErr bool
	}{
		{
			name: "reset non-existent key succeeds",
			key:  "test:nonexistent",
		},
		{
			name: "reset existing key removes entry",
			setup: func(m *Memory) {
				m.entries["test:key"] = &memoryEntry{
					count:      50,
					expiration: time.Now().Add(time.Minute),
				}
			},
			key: "test:key",
		},
		{
			name: "reset empty key succeeds",
			key:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Memory{
				entries: make(map[string]*memoryEntry),
				stopCh:  make(chan struct{}),
			}
			defer m.Close()

			if tt.setup != nil {
				tt.setup(m)
			}

			err := m.Reset(context.Background(), tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reset() error = %v, wantErr %v", err, tt.wantErr)
			}

			if _, exists := m.entries[tt.key]; exists {
				t.Errorf("Reset() failed to remove key %s", tt.key)
			}
		})
	}
}

func TestMemory_Reset_AfterIncrement(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:reset"
	window := time.Minute

	count, _, err := m.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Increment() = %v, want 1", count)
	}

	err = m.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != 0 {
		t.Errorf("Get() after Reset() = %v, want 0", got)
	}

	count, _, err = m.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() after Reset() error = %v", err)
	}
	if count != 1 {
		t.Errorf("Increment() after Reset() = %v, want 1", count)
	}
}

func TestMemory_Close(t *testing.T) {
	m := NewMemory()

	err := m.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	select {
	case <-m.stopCh:
	case <-time.After(100 * time.Millisecond):
		t.Error("Close() did not close stopCh")
	}
}

func TestMemory_Expiration(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:expiration"
	window := 200 * time.Millisecond

	count, _, err := m.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Increment() = %v, want 1", count)
	}

	time.Sleep(100 * time.Millisecond)
	count, _, err = m.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() before expiration error = %v", err)
	}
	if count != 2 {
		t.Errorf("Increment() before expiration = %v, want 2", count)
	}

	time.Sleep(150 * time.Millisecond)
	count, _, err = m.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() after expiration error = %v", err)
	}
	if count != 1 {
		t.Errorf("Increment() after expiration = %v, want 1 (reset)", count)
	}
}

func TestMemory_Cleanup_ExpiredEntriesRemoved(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	expiredKeys := []string{"test:expired1", "test:expired2", "test:expired3"}
	shortTTL := 10 * time.Millisecond

	for _, key := range expiredKeys {
		_, _, err := m.Increment(ctx, key, shortTTL)
		if err != nil {
			t.Fatalf("Increment(%s) error = %v", key, err)
		}
	}

	time.Sleep(20 * time.Millisecond)

	m.runCleanup()

	m.mu.RLock()
	entriesCount := len(m.entries)
	m.mu.RUnlock()

	if entriesCount != 0 {
		t.Errorf("cleanup failed: %d entries remain, want 0", entriesCount)
	}

	for _, key := range expiredKeys {
		got, err := m.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%s) error = %v", key, err)
		}
		if got != 0 {
			t.Errorf("Get(%s) = %v after cleanup, want 0", key, got)
		}
	}
}

func TestMemory_Cleanup_NonExpiredEntriesPreserved(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	keys := map[string]int64{
		"test:long1": 5,
		"test:long2": 10,
		"test:long3": 15,
	}
	longTTL := time.Hour

	for key, count := range keys {
		for i := int64(0); i < count; i++ {
			_, _, err := m.Increment(ctx, key, longTTL)
			if err != nil {
				t.Fatalf("Increment(%s) error = %v", key, err)
			}
		}
	}

	m.runCleanup()

	m.mu.RLock()
	entriesCount := len(m.entries)
	m.mu.RUnlock()

	if entriesCount != len(keys) {
		t.Errorf("cleanup removed non-expired entries: %d entries remain, want %d", entriesCount, len(keys))
	}

	for key, wantCount := range keys {
		got, err := m.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%s) error = %v", key, err)
		}
		if got != wantCount {
			t.Errorf("Get(%s) = %v after cleanup, want %v", key, got, wantCount)
		}
	}
}

func TestMemory_Cleanup_MixedExpiredAndNonExpired(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()

	expiredKeys := []string{"test:expired1", "test:expired2"}
	shortTTL := 10 * time.Millisecond
	for _, key := range expiredKeys {
		_, _, err := m.Increment(ctx, key, shortTTL)
		if err != nil {
			t.Fatalf("Increment(%s) error = %v", key, err)
		}
	}

	activeKeys := map[string]int64{
		"test:active1": 3,
		"test:active2": 7,
	}
	longTTL := time.Hour
	for key, count := range activeKeys {
		for i := int64(0); i < count; i++ {
			_, _, err := m.Increment(ctx, key, longTTL)
			if err != nil {
				t.Fatalf("Increment(%s) error = %v", key, err)
			}
		}
	}

	time.Sleep(20 * time.Millisecond)

	m.runCleanup()

	m.mu.RLock()
	entriesCount := len(m.entries)
	m.mu.RUnlock()

	if entriesCount != len(activeKeys) {
		t.Errorf("cleanup failed: %d entries remain, want %d", entriesCount, len(activeKeys))
	}

	for _, key := range expiredKeys {
		got, err := m.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%s) error = %v", key, err)
		}
		if got != 0 {
			t.Errorf("expired key %s = %v after cleanup, want 0", key, got)
		}
	}

	for key, wantCount := range activeKeys {
		got, err := m.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%s) error = %v", key, err)
		}
		if got != wantCount {
			t.Errorf("active key %s = %v after cleanup, want %v", key, got, wantCount)
		}
	}
}

func TestMemory_Cleanup_ConcurrentWithIncrements(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:concurrent"
	window := time.Hour
	goroutines := 5
	incrementsPerGoroutine := 20
	cleanupCycles := 10

	var wg sync.WaitGroup
	wg.Add(goroutines + 1)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				_, _, err := m.Increment(ctx, key, window)
				if err != nil {
					t.Errorf("Increment() error = %v", err)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < cleanupCycles; i++ {
			m.runCleanup()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	wg.Wait()

	expectedCount := int64(goroutines * incrementsPerGoroutine)
	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != expectedCount {
		t.Errorf("Get() = %v after concurrent cleanup, want %v", got, expectedCount)
	}
}

func TestMemory_Cleanup_RaceWithExpiredIncrements(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	shortTTL := 5 * time.Millisecond
	goroutines := 10
	iterations := 10

	var wg sync.WaitGroup
	wg.Add(goroutines + 1)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			key := "test:key:" + string(rune(id))
			for j := 0; j < iterations; j++ {
				_, _, err := m.Increment(ctx, key, shortTTL)
				if err != nil {
					t.Errorf("Increment() error = %v", err)
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			m.runCleanup()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	time.Sleep(10 * time.Millisecond)
	m.runCleanup()

	m.mu.RLock()
	entriesCount := len(m.entries)
	m.mu.RUnlock()

	if entriesCount != 0 {
		t.Errorf("final cleanup failed: %d entries remain, want 0", entriesCount)
	}
}

func TestMemory_Cleanup_EmptyStore(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	m.runCleanup()

	m.mu.RLock()
	entriesCount := len(m.entries)
	m.mu.RUnlock()

	if entriesCount != 0 {
		t.Errorf("cleanup on empty store created entries: %d entries, want 0", entriesCount)
	}
}

func TestMemory_Cleanup_OnlyExpiredEntries(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	shortTTL := 10 * time.Millisecond

	for i := 0; i < 100; i++ {
		key := "test:expired:" + string(rune(i))
		_, _, err := m.Increment(ctx, key, shortTTL)
		if err != nil {
			t.Fatalf("Increment(%s) error = %v", key, err)
		}
	}

	m.mu.RLock()
	initialCount := len(m.entries)
	m.mu.RUnlock()

	if initialCount != 100 {
		t.Fatalf("setup failed: got %d entries, want 100", initialCount)
	}

	time.Sleep(20 * time.Millisecond)

	m.runCleanup()

	m.mu.RLock()
	finalCount := len(m.entries)
	m.mu.RUnlock()

	if finalCount != 0 {
		t.Errorf("cleanup failed: %d entries remain, want 0", finalCount)
	}
}

func BenchmarkMemory_Increment(b *testing.B) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = m.Increment(ctx, key, window)
	}
}

func BenchmarkMemory_Increment_Parallel(b *testing.B) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = m.Increment(ctx, key, window)
		}
	})
}

func BenchmarkMemory_Get(b *testing.B) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	_, _, _ = m.Increment(ctx, key, window)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Get(ctx, key)
	}
}

func BenchmarkMemory_Get_Parallel(b *testing.B) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	_, _, _ = m.Increment(ctx, key, window)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = m.Get(ctx, key)
		}
	})
}

func BenchmarkMemory_Cleanup(b *testing.B) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	window := time.Hour

	for i := 0; i < 1000; i++ {
		key := "bench:key:" + string(rune(i))
		_, _, _ = m.Increment(ctx, key, window)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.runCleanup()
	}
}
