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
	goroutines := 100
	incrementsPerGoroutine := 100
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
	incrementsPerKey := 50

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

func TestMemory_Get_Concurrent(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx := context.Background()
	key := "test:concurrent"
	expected := int64(100)

	m.entries[key] = &memoryEntry{
		count:      expected,
		expiration: time.Now().Add(time.Minute),
	}

	var wg sync.WaitGroup
	goroutines := 100
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			got, err := m.Get(ctx, key)
			if err != nil {
				t.Errorf("Get() error = %v", err)
			}
			if got != expected {
				t.Errorf("Get() = %v, want %v", got, expected)
			}
		}()
	}

	wg.Wait()
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

func TestMemory_Cleanup(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	now := time.Now()
	m.mu.Lock()
	m.entries["expired1"] = &memoryEntry{
		count:      1,
		expiration: now.Add(-2 * time.Second),
	}
	m.entries["expired2"] = &memoryEntry{
		count:      1,
		expiration: now.Add(-1 * time.Second),
	}
	m.entries["active"] = &memoryEntry{
		count:      1,
		expiration: now.Add(time.Hour),
	}
	m.mu.Unlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		<-ticker.C
		m.mu.Lock()
		now := time.Now()
		for key, entry := range m.entries {
			if now.After(entry.expiration) {
				delete(m.entries, key)
			}
		}
		m.mu.Unlock()
	}()

	time.Sleep(200 * time.Millisecond)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.entries["expired1"]; exists {
		t.Error("cleanup() failed to remove expired1")
	}
	if _, exists := m.entries["expired2"]; exists {
		t.Error("cleanup() failed to remove expired2")
	}
	if _, exists := m.entries["active"]; !exists {
		t.Error("cleanup() incorrectly removed active entry")
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

func TestMemory_ContextCancellation(t *testing.T) {
	m := NewMemory()
	defer m.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	key := "test:context"
	window := time.Minute

	_, _, err := m.Increment(ctx, key, window)
	if err != nil {
		t.Errorf("Increment() with canceled context should not error, got %v", err)
	}

	_, err = m.Get(ctx, key)
	if err != nil {
		t.Errorf("Get() with canceled context should not error, got %v", err)
	}

	err = m.Reset(ctx, key)
	if err != nil {
		t.Errorf("Reset() with canceled context should not error, got %v", err)
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
