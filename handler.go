package chikit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/canonlog"
)

// activeHandlers tracks spawned handler goroutines for graceful shutdown.
var activeHandlers sync.WaitGroup

// activeHandlerCount tracks the count for ActiveHandlerCount().
var activeHandlerCount atomic.Int64

// HandlerOption configures the Handler middleware.
type HandlerOption func(*config)

type config struct {
	canonlog         bool
	canonlogFields   func(*http.Request) map[string]any
	slosEnabled      bool
	timeout          time.Duration
	gracefulShutdown time.Duration
	onAbandon        func(*http.Request)
}

// WithCanonlog enables canonical logging for requests.
// Creates a logger at request start and flushes it after response.
// Logs method, path, route, status, and duration_ms for each request.
// Errors set via SetError are automatically logged.
func WithCanonlog() HandlerOption {
	return func(c *config) {
		c.canonlog = true
	}
}

// WithCanonlogFields adds custom fields to each log entry.
// The function receives the request and returns fields to add.
// Called at request start, before the handler executes.
func WithCanonlogFields(fn func(*http.Request) map[string]any) HandlerOption {
	return func(c *config) {
		c.canonlogFields = fn
	}
}

// WithSLOs enables SLO status logging.
// Requires WithCanonlog() to be enabled.
// Reads SLO tier and target from context (set via SLO or SLOWithTarget)
// and logs slo_class and slo_status (PASS or FAIL) based on request duration.
func WithSLOs() HandlerOption {
	return func(c *config) {
		c.slosEnabled = true
	}
}

// WithTimeout sets a maximum duration for handler execution.
// If the handler doesn't complete within the timeout, a 504 Gateway Timeout
// response is returned immediately. The context is cancelled so DB/HTTP calls
// can exit early. The handler goroutine continues running but its response
// is discarded.
//
// When timeout is enabled, handlers run in a separate goroutine. You MUST call
// WaitForHandlers during graceful shutdown to wait for handler goroutines to
// complete before process exit. See WaitForHandlers for the shutdown pattern.
//
// Default graceful shutdown timeout is 5 seconds. Use WithGracefulShutdown to
// change this value. Options can be specified in any order.
//
// Note: Go cannot forcibly terminate goroutines. If handlers ignore context
// cancellation (CGO calls, tight CPU loops), they continue running after the
// 504 response. Use WithAbandonCallback to track this with metrics.
func WithTimeout(d time.Duration) HandlerOption {
	return func(c *config) {
		c.timeout = d
	}
}

// WithGracefulShutdown sets how long to wait for a handler goroutine to exit
// after timeout fires. This grace period allows handlers to complete cleanup
// (e.g., database rollbacks) after the 504 response is sent to the client.
//
// After the grace period, the handler is considered abandoned. If canonlog is
// enabled, an error is logged. Use WithAbandonCallback for metrics/alerting.
//
// Default is 5 seconds. Can be specified before or after WithTimeout.
func WithGracefulShutdown(d time.Duration) HandlerOption {
	return func(c *config) {
		c.gracefulShutdown = d
	}
}

// WithAbandonCallback sets a function to call when a handler doesn't exit
// within the grace timeout. Use this for metrics or alerting.
func WithAbandonCallback(fn func(*http.Request)) HandlerOption {
	return func(c *config) {
		c.onAbandon = fn
	}
}

// Handler returns middleware that manages response state and writes responses.
func Handler(opts ...HandlerOption) func(http.Handler) http.Handler {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Apply defaults and validation
	if cfg.timeout > 0 && cfg.gracefulShutdown == 0 {
		cfg.gracefulShutdown = 5 * time.Second
	}
	if cfg.timeout < 0 {
		cfg.timeout = 0 // Treat negative as disabled
	}
	if cfg.gracefulShutdown < 0 {
		cfg.gracefulShutdown = 0
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &State{}
			ctx := context.WithValue(r.Context(), stateKey, state)

			var start time.Time
			if cfg.canonlog {
				ctx = canonlog.NewContext(ctx)
				start = time.Now()
				canonlog.InfoAddMany(ctx, map[string]any{"method": r.Method, "path": r.URL.Path})
				if cfg.canonlogFields != nil {
					canonlog.InfoAddMany(ctx, cfg.canonlogFields(r))
				}
			}

			if cfg.timeout == 0 {
				handleSync(ctx, cfg, next, w, r.WithContext(ctx), state, start)
				return
			}
			handleWithTimeout(ctx, cfg, next, w, r, state, start)
		})
	}
}

func handleSync(ctx context.Context, cfg *config, next http.Handler, w http.ResponseWriter, r *http.Request, state *State, start time.Time) {
	defer func() {
		if rec := recover(); rec != nil {
			state.mu.Lock()
			state.err = ErrInternal
			state.mu.Unlock()
			if cfg.canonlog {
				canonlog.ErrorAdd(ctx, fmt.Errorf("panic: %v", rec))
			}
		}
		flushCanonlog(ctx, cfg, state, r, start)
		if state.markWritten() {
			writeResponse(w, state)
		}
	}()
	next.ServeHTTP(w, r)
}

func handleWithTimeout(parentCtx context.Context, cfg *config, next http.Handler, w http.ResponseWriter, r *http.Request, state *State, start time.Time) {
	ctx, cancel := context.WithTimeout(parentCtx, cfg.timeout)
	defer cancel()

	r = r.WithContext(ctx)
	done := make(chan struct{})
	panicVal := make(chan any, 1)

	activeHandlers.Add(1)
	activeHandlerCount.Add(1)
	go func() {
		defer activeHandlers.Done()
		defer activeHandlerCount.Add(-1)
		defer close(done)
		defer func() {
			if rec := recover(); rec != nil {
				panicVal <- rec
			}
		}()
		next.ServeHTTP(w, r)
	}()

	select {
	case <-done:
		handlePanic(parentCtx, cfg, state, panicVal)
		flushCanonlog(parentCtx, cfg, state, r, start)
		if state.markWritten() {
			writeResponse(w, state)
		}

	case <-ctx.Done():
		state.mu.Lock()
		state.err = ErrGatewayTimeout
		state.mu.Unlock()
		if state.markWritten() {
			writeResponse(w, state)
		}
		waitForGrace(parentCtx, cfg, r, done, panicVal)
		flushCanonlog(parentCtx, cfg, state, r, start)
	}
}

func handlePanic(ctx context.Context, cfg *config, state *State, panicVal <-chan any) {
	select {
	case p := <-panicVal:
		state.mu.Lock()
		state.err = ErrInternal
		state.mu.Unlock()
		if cfg.canonlog {
			canonlog.ErrorAdd(ctx, fmt.Errorf("panic: %v", p))
		}
	default:
	}
}

func waitForGrace(ctx context.Context, cfg *config, r *http.Request, done <-chan struct{}, panicVal <-chan any) {
	select {
	case <-done:
		select {
		case p := <-panicVal:
			if cfg.canonlog {
				canonlog.ErrorAdd(ctx, fmt.Errorf("panic after timeout: %v", p))
			}
		default:
		}
	case <-time.After(cfg.gracefulShutdown):
		if cfg.canonlog {
			canonlog.ErrorAdd(ctx, fmt.Errorf("handler abandoned after grace timeout"))
		}
		if cfg.onAbandon != nil {
			cfg.onAbandon(r)
		}
	}
}

func flushCanonlog(ctx context.Context, cfg *config, state *State, r *http.Request, start time.Time) {
	if !cfg.canonlog {
		return
	}

	// Take a snapshot to safely read state (handler may still be running)
	snap := state.snapshot()

	status := snap.status
	if snap.err != nil {
		status = snap.err.Status
		canonlog.ErrorAdd(ctx, snap.err)
	}

	route := r.URL.Path
	if rctx := chi.RouteContext(ctx); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			route = pattern
		}
	}

	canonlog.InfoAddMany(ctx, map[string]any{
		"route":       route,
		"status":      status,
		"duration_ms": time.Since(start).Milliseconds(),
	})

	if cfg.slosEnabled {
		if tier, target, ok := GetSLO(ctx); ok {
			sloStatus := "PASS"
			if time.Since(start) > target {
				sloStatus = "FAIL"
			}
			canonlog.InfoAdd(ctx, "slo_class", string(tier))
			canonlog.InfoAdd(ctx, "slo_status", sloStatus)
		}
	}

	canonlog.Flush(ctx)
}

// WaitForHandlers waits for all spawned handler goroutines to complete.
// Call this during graceful shutdown after http.Server.Shutdown().
// Returns nil if all handlers complete, or ctx.Err() if the context
// deadline is exceeded.
//
// Example graceful shutdown pattern:
//
//	srv := &http.Server{Addr: ":8080", Handler: r}
//	go srv.ListenAndServe()
//	<-shutdownSignal
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	srv.Shutdown(ctx)           // Wait for in-flight requests
//	chikit.WaitForHandlers(ctx) // Wait for handler goroutines
//
// Note: If the context deadline is exceeded, WaitForHandlers returns immediately.
// Use ActiveHandlerCount to monitor how many handlers are still running.
func WaitForHandlers(ctx context.Context) error {
	for {
		if activeHandlerCount.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// Poll again
		}
	}
}

// ActiveHandlerCount returns the number of handler goroutines currently running.
// This is useful for monitoring during graceful shutdown or for metrics.
// Only counts handlers started with WithTimeout enabled.
func ActiveHandlerCount() int {
	// We can't directly read WaitGroup counter, so we track separately
	return int(activeHandlerCount.Load())
}

func writeResponse(w http.ResponseWriter, state *State) {
	state.mu.Lock()
	defer state.mu.Unlock()

	for key, values := range state.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	if state.err != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(errorResponse{Error: state.err}); err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(state.err.Status)
		w.Write(buf.Bytes())
		return
	}

	if state.body != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(state.body); err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(state.status)
		w.Write(buf.Bytes())
		return
	}

	if state.status != 0 {
		w.WriteHeader(state.status)
	}
}
