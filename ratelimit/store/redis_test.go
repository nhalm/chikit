package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func isRedisAvailable() bool {
	config := RedisConfig{
		URL: "localhost:6379",
		DB:  15,
	}
	store, err := NewRedis(config)
	if err != nil {
		return false
	}
	store.Close()
	return true
}

func setupRedisTest(t *testing.T) (*Redis, func()) {
	t.Helper()

	config := RedisConfig{
		URL:      "localhost:6379",
		Password: "",
		DB:       15,
		Prefix:   "test:ratelimit:",
	}

	store, err := NewRedis(config)
	if err != nil {
		t.Skip("Redis not available:", err)
	}

	cleanup := func() {
		ctx := context.Background()
		pattern := config.Prefix + "*"
		iter := store.client.Scan(ctx, 0, pattern, 0).Iterator()
		for iter.Next(ctx) {
			store.client.Del(ctx, iter.Val())
		}
		store.Close()
	}

	return store, cleanup
}

func TestNewRedis(t *testing.T) {
	if !isRedisAvailable() {
		t.Skip("Redis not available")
	}

	tests := []struct {
		name    string
		config  RedisConfig
		wantErr bool
	}{
		{
			name: "valid connection",
			config: RedisConfig{
				URL:      "localhost:6379",
				Password: "",
				DB:       15,
				Prefix:   "test:",
			},
			wantErr: false,
		},
		{
			name: "default prefix",
			config: RedisConfig{
				URL:      "localhost:6379",
				Password: "",
				DB:       15,
			},
			wantErr: false,
		},
		{
			name: "invalid connection",
			config: RedisConfig{
				URL:      "localhost:9999",
				Password: "",
				DB:       0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewRedis(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRedis() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if tt.config.Prefix == "" && store.prefix != "ratelimit:" {
					t.Errorf("NewRedis() prefix = %v, want ratelimit:", store.prefix)
				} else if tt.config.Prefix != "" && store.prefix != tt.config.Prefix {
					t.Errorf("NewRedis() prefix = %v, want %v", store.prefix, tt.config.Prefix)
				}
				store.Close()
			}
		})
	}
}

func TestRedis_Increment(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	tests := []struct {
		name    string
		key     string
		window  time.Duration
		count   int
		want    int64
		wantErr bool
	}{
		{
			name:   "first increment",
			key:    "test:first",
			window: time.Minute,
			count:  1,
			want:   1,
		},
		{
			name:   "sequential increments",
			key:    "test:sequential",
			window: time.Minute,
			count:  5,
			want:   5,
		},
		{
			name:   "empty key",
			key:    "",
			window: time.Minute,
			count:  1,
			want:   1,
		},
		{
			name:   "zero window",
			key:    "test:zero",
			window: 0,
			count:  1,
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			var lastCount int64
			for i := 0; i < tt.count; i++ {
				got, _, err := store.Increment(ctx, tt.key, tt.window)
				if (err != nil) != tt.wantErr {
					t.Errorf("Increment() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				lastCount = got
			}

			if lastCount != tt.want {
				t.Errorf("Increment() = %v, want %v", lastCount, tt.want)
			}
		})
	}
}

func TestRedis_Increment_Expiration(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:expiration"
	window := 2 * time.Second

	count, _, err := store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Increment() = %v, want 1", count)
	}

	count, _, err = store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if count != 2 {
		t.Errorf("Increment() before expiration = %v, want 2", count)
	}

	time.Sleep(3 * time.Second)

	count, _, err = store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() after expiration error = %v", err)
	}
	if count != 1 {
		t.Errorf("Increment() after expiration = %v, want 1 (reset)", count)
	}
}

func TestRedis_Increment_Concurrent(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:concurrent"
	window := time.Minute
	numGoroutines := 100
	incrementsPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				_, _, err := store.Increment(ctx, key, window)
				if err != nil {
					t.Errorf("Increment() error = %v", err)
				}
			}
		}()
	}

	wg.Wait()

	finalCount, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	expected := int64(numGoroutines * incrementsPerGoroutine)
	if finalCount != expected {
		t.Errorf("Concurrent Increment() final count = %v, want %v", finalCount, expected)
	}
}

func TestRedis_Increment_ConcurrentAccuracy(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:concurrent_accuracy"
	window := time.Minute
	numGoroutines := 50

	var successCount atomic.Int64
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			count, _, err := store.Increment(ctx, key, window)
			if err != nil {
				t.Errorf("Increment() error = %v", err)
				return
			}
			if count > 0 {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	finalCount, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if finalCount != int64(numGoroutines) {
		t.Errorf("Concurrent increments count = %v, want %v", finalCount, numGoroutines)
	}

	if successCount.Load() != int64(numGoroutines) {
		t.Errorf("Success count = %v, want %v", successCount.Load(), numGoroutines)
	}
}

func TestRedis_Get(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func() string
		want    int64
		wantErr bool
	}{
		{
			name: "non-existent key returns zero",
			setup: func() string {
				return "test:nonexistent"
			},
			want: 0,
		},
		{
			name: "existing key returns count",
			setup: func() string {
				key := "test:existing"
				_, _, _ = store.Increment(ctx, key, time.Minute)
				_, _, _ = store.Increment(ctx, key, time.Minute)
				_, _, _ = store.Increment(ctx, key, time.Minute)
				return key
			},
			want: 3,
		},
		{
			name: "expired key returns zero",
			setup: func() string {
				key := "test:expired"
				fullKey := store.prefix + key
				store.client.Set(ctx, fullKey, 100, 1*time.Millisecond)
				time.Sleep(50 * time.Millisecond)
				return key
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.setup()

			got, err := store.Get(ctx, key)
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

func TestRedis_Reset(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func() string
		wantErr bool
	}{
		{
			name: "reset non-existent key succeeds",
			setup: func() string {
				return "test:nonexistent"
			},
		},
		{
			name: "reset existing key removes entry",
			setup: func() string {
				key := "test:reset"
				_, _, _ = store.Increment(ctx, key, time.Minute)
				return key
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.setup()

			err := store.Reset(ctx, key)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reset() error = %v, wantErr %v", err, tt.wantErr)
			}

			got, err := store.Get(ctx, key)
			if err != nil {
				t.Fatalf("Get() after Reset() error = %v", err)
			}
			if got != 0 {
				t.Errorf("Get() after Reset() = %v, want 0", got)
			}
		})
	}
}

func TestRedis_Close(t *testing.T) {
	config := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "test:",
	}

	store, err := NewRedis(config)
	if err != nil {
		t.Skip("Redis not available:", err)
	}

	err = store.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	ctx := context.Background()
	_, err = store.Get(ctx, "test:key")
	if err == nil {
		t.Error("Expected error after Close(), got nil")
	}
}

func TestRedis_Close_ProperlyClosesConnection(t *testing.T) {
	config := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "test:",
	}

	store, err := NewRedis(config)
	if err != nil {
		t.Skip("Redis not available:", err)
	}

	ctx := context.Background()
	key := "test:close"
	window := time.Minute

	_, _, err = store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	_, _, err = store.Increment(ctx, key, window)
	if err == nil {
		t.Error("Increment() after Close() should error")
	}

	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("Get() after Close() should error")
	}

	err = store.Reset(ctx, key)
	if err == nil {
		t.Error("Reset() after Close() should error")
	}
}

func TestRedis_ContextCancellation(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	key := "test:context"
	window := time.Minute

	_, _, err := store.Increment(ctx, key, window)
	if err == nil {
		t.Error("Increment() with canceled context should error")
	}

	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("Get() with canceled context should error")
	}

	err = store.Reset(ctx, key)
	if err == nil {
		t.Error("Reset() with canceled context should error")
	}
}

func TestRedis_ContextTimeout(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond)

	key := "test:timeout"
	window := time.Minute

	_, _, err := store.Increment(ctx, key, window)
	if err == nil {
		t.Error("Increment() with timed out context should error")
	}
}

func TestRedis_PrefixIsolation(t *testing.T) {
	config1 := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "test:prefix1:",
	}
	store1, err := NewRedis(config1)
	if err != nil {
		t.Skip("Redis not available:", err)
	}
	defer store1.Close()

	config2 := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "test:prefix2:",
	}
	store2, err := NewRedis(config2)
	if err != nil {
		t.Skip("Redis not available:", err)
	}
	defer store2.Close()

	ctx := context.Background()
	key := "shared"
	window := time.Minute

	count1, _, err := store1.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("store1.Increment() error = %v", err)
	}
	if count1 != 1 {
		t.Fatalf("store1.Increment() = %v, want 1", count1)
	}

	count2, err := store2.Get(ctx, key)
	if err != nil {
		t.Fatalf("store2.Get() error = %v", err)
	}
	if count2 != 0 {
		t.Errorf("store2.Get() = %v, want 0 (prefixes should isolate)", count2)
	}

	store1.client.Del(ctx, config1.Prefix+key)
	store2.client.Del(ctx, config2.Prefix+key)
}

func TestRedis_Pipeline_Atomicity(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:pipeline"
	window := time.Minute

	count1, _, err := store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if count1 != 1 {
		t.Fatalf("Increment() = %v, want 1", count1)
	}

	fullKey := store.prefix + key
	val, err := store.client.Get(ctx, fullKey).Int64()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if val != 1 {
		t.Errorf("Redis value = %v, want 1", val)
	}

	ttl, err := store.client.TTL(ctx, fullKey).Result()
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl <= 0 {
		t.Errorf("TTL() = %v, want > 0", ttl)
	}
}

func TestRedis_ConnectionFailure(t *testing.T) {
	config := RedisConfig{
		URL:    "localhost:9999",
		DB:     0,
		Prefix: "test:",
	}

	_, err := NewRedis(config)
	if err == nil {
		t.Error("NewRedis() with invalid connection should error")
	}
}

func TestRedis_ErrorWrapping(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	store.Close()

	ctx := context.Background()
	key := "test:error"
	window := time.Minute

	_, _, err := store.Increment(ctx, key, window)
	if err == nil {
		t.Error("Increment() after Close() should error")
	}
	if err != nil && fmt.Sprintf("%v", err) == "" {
		t.Error("Error should have a message")
	}

	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("Get() after Close() should error")
	}

	err = store.Reset(ctx, key)
	if err == nil {
		t.Error("Reset() after Close() should error")
	}
}

func TestRedis_NilError(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:nil"

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Errorf("Get() on non-existent key should not error, got %v", err)
	}
	if got != 0 {
		t.Errorf("Get() on non-existent key = %v, want 0", got)
	}
}

func TestRedis_Increment_UnexpectedResultLength(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:result_length"
	window := time.Minute

	count, ttl, err := store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}

	if count != 1 {
		t.Errorf("Increment() count = %v, want 1", count)
	}
	if ttl <= 0 {
		t.Errorf("Increment() ttl = %v, want > 0", ttl)
	}
}

func TestRedis_Increment_NegativeTTL(t *testing.T) {
	store, cleanup := setupRedisTest(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:negative_ttl"

	fullKey := store.prefix + key
	store.client.Set(ctx, fullKey, 5, 0)

	window := time.Minute
	count, ttl, err := store.Increment(ctx, key, window)
	if err != nil {
		t.Fatalf("Increment() error = %v", err)
	}

	if count != 6 {
		t.Errorf("Increment() count = %v, want 6", count)
	}

	if ttl < -2 {
		t.Errorf("Increment() ttl = %v, should be -1 or -2 for key without expiry", ttl)
	}
}

func BenchmarkRedis_Increment(b *testing.B) {
	config := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "bench:",
	}

	store, err := NewRedis(config)
	if err != nil {
		b.Skip("Redis not available:", err)
	}
	defer store.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = store.Increment(ctx, key, window)
	}
}

func BenchmarkRedis_Get(b *testing.B) {
	config := RedisConfig{
		URL:    "localhost:6379",
		DB:     15,
		Prefix: "bench:",
	}

	store, err := NewRedis(config)
	if err != nil {
		b.Skip("Redis not available:", err)
	}
	defer store.Close()

	ctx := context.Background()
	key := "bench:key"
	window := time.Minute

	_, _, _ = store.Increment(ctx, key, window)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Get(ctx, key)
	}
}

func ExampleRedis() {
	config := RedisConfig{
		URL:      "localhost:6379",
		Password: "",
		DB:       0,
		Prefix:   "myapp:",
	}

	store, err := NewRedis(config)
	if err != nil {
		panic(err)
	}
	defer store.Close()

	ctx := context.Background()
	key := "user:123"
	window := time.Minute

	count, _, err := store.Increment(ctx, key, window)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Request count: %d\n", count)
}
