// Package wrapper provides context-based response handling for Chi middleware.
//
// The wrapper pattern allows handlers and middleware to set responses and errors
// in request context rather than writing directly to ResponseWriter. This enables:
//   - Consistent JSON error responses across all middleware
//   - Stripe-style structured errors with type, code, message, and field errors
//   - Response interception and modification by outer middleware
//   - Panic recovery with safe error responses
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
package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
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

// config holds configuration for the wrapper middleware.
// Currently empty but supports future options without breaking changes.
type config struct{}

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
			r = r.WithContext(ctx)

			defer func() {
				if rec := recover(); rec != nil {
					state.mu.Lock()
					state.err = ErrInternal
					state.mu.Unlock()
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
