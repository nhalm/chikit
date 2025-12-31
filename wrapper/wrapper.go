// Package wrapper provides context-based response handling for Chi middleware.
//
// The wrapper pattern allows handlers and middleware to set responses and errors
// in request context rather than writing directly to ResponseWriter. This enables:
//   - Consistent JSON error responses across all middleware
//   - Stripe-style structured errors with type, code, message, and field errors
//   - Response interception and modification by outer middleware
//   - Panic recovery with safe error responses
//   - Optional canonical logging via canonlog integration
//   - Optional SLO tracking with PASS/FAIL status
//
// Basic usage:
//
//	r := chi.NewRouter()
//	r.Use(wrapper.New())  // Outermost middleware
//
//	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
//	    user, err := createUser(req)
//	    if err != nil {
//	        wrapper.SetError(r, wrapper.ErrInternal)
//	        return
//	    }
//	    wrapper.SetResponse(r, http.StatusCreated, user)
//	})
//
// With canonical logging and SLO tracking:
//
//	r.Use(wrapper.New(
//	    wrapper.WithCanonlog(),
//	    wrapper.WithCanonlogFields(func(r *http.Request) map[string]any {
//	        return map[string]any{"request_id": r.Header.Get("X-Request-ID")}
//	    }),
//	    wrapper.WithSLOs(),
//	))
//
//	r.With(slo.Track(slo.HighFast)).Get("/users/{id}", getUser)
package wrapper

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
	"github.com/nhalm/chikit/slo"
)

type contextKey string

const stateKey contextKey = "wrapper_state"

// State holds the response state for a request.
type State struct {
	mu      sync.Mutex
	err     *Error
	status  int
	body    any
	headers http.Header
}

// Error represents a structured API error response.
type Error struct {
	Type    string       `json:"type"`
	Code    string       `json:"code,omitempty"`
	Message string       `json:"message"`
	Param   string       `json:"param,omitempty"`
	Errors  []FieldError `json:"errors,omitempty"`
	Status  int          `json:"-"`
}

// FieldError represents a validation error for a specific field.
type FieldError struct {
	Param   string `json:"param"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error *Error `json:"error"`
}

var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Message
}

// Is implements errors.Is for comparing error types.
func (e *Error) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Type == t.Type && e.Code == t.Code
}

// With returns a copy of the error with a custom message.
func (e *Error) With(message string) *Error {
	if e == nil {
		return nil
	}
	dup := *e
	dup.Message = message
	return &dup
}

// WithParam returns a copy of the error with a custom message and parameter.
func (e *Error) WithParam(message, param string) *Error {
	if e == nil {
		return nil
	}
	dup := *e
	dup.Message = message
	dup.Param = param
	return &dup
}

// Predefined sentinel errors
var (
	ErrBadRequest          = &Error{Type: "request_error", Code: "bad_request", Message: "Bad request", Status: http.StatusBadRequest}
	ErrUnauthorized        = &Error{Type: "auth_error", Code: "unauthorized", Message: "Unauthorized", Status: http.StatusUnauthorized}
	ErrPaymentRequired     = &Error{Type: "request_error", Code: "payment_required", Message: "Payment required", Status: http.StatusPaymentRequired}
	ErrForbidden           = &Error{Type: "auth_error", Code: "forbidden", Message: "Forbidden", Status: http.StatusForbidden}
	ErrNotFound            = &Error{Type: "not_found", Code: "resource_not_found", Message: "Resource not found", Status: http.StatusNotFound}
	ErrMethodNotAllowed    = &Error{Type: "request_error", Code: "method_not_allowed", Message: "Method not allowed", Status: http.StatusMethodNotAllowed}
	ErrConflict            = &Error{Type: "request_error", Code: "conflict", Message: "Conflict", Status: http.StatusConflict}
	ErrGone                = &Error{Type: "request_error", Code: "gone", Message: "Resource gone", Status: http.StatusGone}
	ErrPayloadTooLarge     = &Error{Type: "request_error", Code: "payload_too_large", Message: "Payload too large", Status: http.StatusRequestEntityTooLarge}
	ErrUnprocessableEntity = &Error{Type: "validation_error", Code: "unprocessable", Message: "Unprocessable entity", Status: http.StatusUnprocessableEntity}
	ErrRateLimited         = &Error{Type: "rate_limit_error", Code: "limit_exceeded", Message: "Rate limit exceeded", Status: http.StatusTooManyRequests}
	ErrInternal            = &Error{Type: "internal_error", Code: "internal", Message: "Internal server error", Status: http.StatusInternalServerError}
	ErrNotImplemented      = &Error{Type: "request_error", Code: "not_implemented", Message: "Not implemented", Status: http.StatusNotImplemented}
	ErrServiceUnavailable  = &Error{Type: "request_error", Code: "service_unavailable", Message: "Service unavailable", Status: http.StatusServiceUnavailable}
)

// NewValidationError creates a validation error with multiple field errors.
func NewValidationError(errors []FieldError) *Error {
	return &Error{
		Type:    "validation_error",
		Code:    "invalid_request",
		Message: "Validation failed",
		Errors:  errors,
		Status:  http.StatusBadRequest,
	}
}

// SetError sets an error response in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetError(r *http.Request, err *Error) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.err = err
}

// SetResponse sets a success response in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetResponse(r *http.Request, status int, body any) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.status = status
	state.body = body
}

// SetHeader sets a response header in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetHeader(r *http.Request, key, value string) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.headers == nil {
		state.headers = make(http.Header)
	}
	state.headers.Set(key, value)
}

// AddHeader adds a response header value in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func AddHeader(r *http.Request, key, value string) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.headers == nil {
		state.headers = make(http.Header)
	}
	state.headers.Add(key, value)
}

// HasState returns true if wrapper state exists in the context.
func HasState(ctx context.Context) bool {
	return getState(ctx) != nil
}

func getState(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey).(*State)
	return state
}

// Option configures the wrapper middleware.
type Option func(*config)

type config struct {
	canonlog       bool
	canonlogFields func(*http.Request) map[string]any
	slosEnabled    bool
}

// WithCanonlog enables canonical logging for requests.
// Creates a logger at request start and flushes it after response.
// Logs method, path, route, status, and duration_ms for each request.
// Errors set via SetError are automatically logged.
func WithCanonlog() Option {
	return func(c *config) {
		c.canonlog = true
	}
}

// WithCanonlogFields adds custom fields to each log entry.
// The function receives the request and returns fields to add.
// Called at request start, before the handler executes.
func WithCanonlogFields(fn func(*http.Request) map[string]any) Option {
	return func(c *config) {
		c.canonlogFields = fn
	}
}

// WithSLOs enables SLO status logging.
// Requires WithCanonlog() to be enabled.
// Reads SLO tier and target from context (set via slo.Track or slo.TrackWithTarget)
// and logs slo_class and slo_status (PASS or FAIL) based on request duration.
func WithSLOs() Option {
	return func(c *config) {
		c.slosEnabled = true
	}
}

// New returns middleware that manages response state and writes responses.
func New(opts ...Option) func(http.Handler) http.Handler {
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
						if tier, target, ok := slo.GetTier(ctx); ok {
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
		buf := bufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer bufferPool.Put(buf)

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
		buf := bufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer bufferPool.Put(buf)

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
