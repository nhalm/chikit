package chikit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/canonlog"
)

// activeHandlers tracks spawned handler goroutines for graceful shutdown.
var activeHandlers sync.WaitGroup

// HandlerOption configures the Handler middleware.
type HandlerOption func(*config)

type config struct {
	canonlog       bool
	canonlogFields func(*http.Request) map[string]any
	slosEnabled    bool
	timeout        time.Duration
	graceTimeout   time.Duration
	onAbandon      func(*http.Request)
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
// When timeout is enabled, handlers run in a separate goroutine. Use
// WaitForHandlers during graceful shutdown to wait for all handler goroutines.
func WithTimeout(d time.Duration) HandlerOption {
	return func(c *config) {
		c.timeout = d
		if c.graceTimeout == 0 {
			c.graceTimeout = 5 * time.Second
		}
	}
}

// WithGraceTimeout sets how long to wait for a handler to exit after timeout.
// After the grace period, the handler is considered abandoned and logged.
// Default is 5 seconds.
func WithGraceTimeout(d time.Duration) HandlerOption {
	return func(c *config) {
		c.graceTimeout = d
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

func handleWithTimeout(ctx context.Context, cfg *config, next http.Handler, w http.ResponseWriter, r *http.Request, state *State, start time.Time) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	r = r.WithContext(ctx)
	done := make(chan struct{})
	panicVal := make(chan any, 1)

	activeHandlers.Add(1)
	go func() {
		defer activeHandlers.Done()
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
		handlePanic(ctx, cfg, state, panicVal)
		flushCanonlog(ctx, cfg, state, r, start)
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
		waitForGrace(ctx, cfg, r, done, panicVal)
		flushCanonlog(ctx, cfg, state, r, start)
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
	case <-time.After(cfg.graceTimeout):
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

	state.mu.Lock()
	status := state.status
	if state.err != nil {
		status = state.err.Status
		canonlog.ErrorAdd(ctx, state.err)
	}
	state.mu.Unlock()

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
func WaitForHandlers(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		activeHandlers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
