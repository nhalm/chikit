package chikit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/canonlog"
)

// HandlerOption configures the Handler middleware.
type HandlerOption func(*config)

type config struct {
	canonlog       bool
	canonlogFields func(*http.Request) map[string]any
	slosEnabled    bool
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

				canonlog.InfoAddMany(ctx, map[string]any{
					"method": r.Method,
					"path":   r.URL.Path,
				})

				if cfg.canonlogFields != nil {
					canonlog.InfoAddMany(ctx, cfg.canonlogFields(r))
				}
			}

			r = r.WithContext(ctx)

			defer func() {
				if rec := recover(); rec != nil {
					state.mu.Lock()
					state.err = ErrInternal
					state.mu.Unlock()

					if cfg.canonlog {
						canonlog.ErrorAdd(ctx, fmt.Errorf("panic: %v", rec))
					}
				}

				if cfg.canonlog {
					state.mu.Lock()
					status := state.status
					if state.err != nil {
						status = state.err.Status
						canonlog.ErrorAdd(ctx, state.err)
					}
					state.mu.Unlock()

					duration := time.Since(start)

					route := r.URL.Path
					if rctx := chi.RouteContext(ctx); rctx != nil {
						if pattern := rctx.RoutePattern(); pattern != "" {
							route = pattern
						}
					}

					canonlog.InfoAddMany(ctx, map[string]any{
						"route":       route,
						"status":      status,
						"duration_ms": duration.Milliseconds(),
					})

					if cfg.slosEnabled {
						if tier, target, ok := GetSLO(ctx); ok {
							sloStatus := "PASS"
							if duration > target {
								sloStatus = "FAIL"
							}
							canonlog.InfoAdd(ctx, "slo_class", string(tier))
							canonlog.InfoAdd(ctx, "slo_status", sloStatus)
						}
					}

					canonlog.Flush(ctx)
				}

				writeResponse(w, state)
			}()

			next.ServeHTTP(w, r)
		})
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
