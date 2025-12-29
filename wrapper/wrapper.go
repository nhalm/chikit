// Package wrapper provides context-based response handling for Chi middleware.
package wrapper

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

type contextKey string

const stateKey contextKey = "wrapper_state"

// State holds mutable response state, stored in context by Handler.
type State struct {
	mu      sync.Mutex
	err     *Error
	status  int
	body    any
	headers http.Header
}

// Error is a Stripe-style structured error.
type Error struct {
	Type    string       `json:"type"`
	Code    string       `json:"code,omitempty"`
	Message string       `json:"message"`
	Param   string       `json:"param,omitempty"`
	Errors  []FieldError `json:"errors,omitempty"`
	Status  int          `json:"-"`
}

// FieldError represents a single field validation error.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// With creates a copy of the error with a custom message.
func (e *Error) With(message string) *Error {
	dup := *e
	dup.Message = message
	return &dup
}

// WithParam creates a copy of the error with a custom message and param.
func (e *Error) WithParam(message, param string) *Error {
	dup := *e
	dup.Message = message
	dup.Param = param
	return &dup
}

// Is implements errors.Is for sentinel comparison by Type and Code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Type == t.Type && e.Code == t.Code
}

// Predefined sentinel errors - 4xx
var (
	ErrBadRequest          = &Error{Type: "request_error", Code: "bad_request", Message: "Bad request", Status: http.StatusBadRequest}
	ErrUnauthorized        = &Error{Type: "auth_error", Code: "unauthorized", Message: "Unauthorized", Status: http.StatusUnauthorized}
	ErrPaymentRequired     = &Error{Type: "payment_error", Code: "payment_required", Message: "Payment required", Status: http.StatusPaymentRequired}
	ErrForbidden           = &Error{Type: "auth_error", Code: "forbidden", Message: "Forbidden", Status: http.StatusForbidden}
	ErrNotFound            = &Error{Type: "not_found", Code: "resource_not_found", Message: "Resource not found", Status: http.StatusNotFound}
	ErrMethodNotAllowed    = &Error{Type: "request_error", Code: "method_not_allowed", Message: "Method not allowed", Status: http.StatusMethodNotAllowed}
	ErrConflict            = &Error{Type: "request_error", Code: "conflict", Message: "Conflict", Status: http.StatusConflict}
	ErrGone                = &Error{Type: "request_error", Code: "gone", Message: "Resource no longer available", Status: http.StatusGone}
	ErrPayloadTooLarge     = &Error{Type: "request_error", Code: "payload_too_large", Message: "Payload too large", Status: http.StatusRequestEntityTooLarge}
	ErrUnprocessableEntity = &Error{Type: "validation_error", Code: "unprocessable", Message: "Unprocessable entity", Status: http.StatusUnprocessableEntity}
	ErrRateLimited         = &Error{Type: "rate_limit_error", Code: "limit_exceeded", Message: "Rate limit exceeded", Status: http.StatusTooManyRequests}
)

// Predefined sentinel errors - 5xx
var (
	ErrInternal           = &Error{Type: "internal_error", Code: "internal", Message: "Internal server error", Status: http.StatusInternalServerError}
	ErrNotImplemented     = &Error{Type: "internal_error", Code: "not_implemented", Message: "Not implemented", Status: http.StatusNotImplemented}
	ErrServiceUnavailable = &Error{Type: "internal_error", Code: "service_unavailable", Message: "Service unavailable", Status: http.StatusServiceUnavailable}
)

// NewValidationError creates a validation error with multiple field errors.
func NewValidationError(fieldErrors []FieldError) *Error {
	return &Error{
		Type:    "validation_error",
		Code:    "invalid_request",
		Message: "Validation failed",
		Errors:  fieldErrors,
		Status:  http.StatusBadRequest,
	}
}

// SetError stores an error in the request's state.
func SetError(r *http.Request, err *Error) {
	if state := getState(r.Context()); state != nil {
		state.mu.Lock()
		state.err = err
		state.mu.Unlock()
	}
}

// SetResponse stores a success response in the request's state.
func SetResponse(r *http.Request, status int, body any) {
	if state := getState(r.Context()); state != nil {
		state.mu.Lock()
		state.status = status
		state.body = body
		state.mu.Unlock()
	}
}

// SetHeader stores a header to be written with the response.
func SetHeader(r *http.Request, key, value string) {
	if state := getState(r.Context()); state != nil {
		state.mu.Lock()
		if state.headers == nil {
			state.headers = make(http.Header)
		}
		state.headers.Set(key, value)
		state.mu.Unlock()
	}
}

// AddHeader appends a header value (for multi-value headers).
func AddHeader(r *http.Request, key, value string) {
	if state := getState(r.Context()); state != nil {
		state.mu.Lock()
		if state.headers == nil {
			state.headers = make(http.Header)
		}
		state.headers.Add(key, value)
		state.mu.Unlock()
	}
}

func getState(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey).(*State)
	return state
}

// HasState returns true if wrapper.Handler is in the middleware chain.
func HasState(ctx context.Context) bool {
	return getState(ctx) != nil
}

// Handler is the outermost middleware that manages response state.
func Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &State{}
			ctx := context.WithValue(r.Context(), stateKey, state)
			r = r.WithContext(ctx)

			defer func() {
				if rec := recover(); rec != nil {
					state.err = ErrInternal
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

	for k, v := range state.headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}

	if state.err != nil {
		writeErrorResponse(w, state.err)
		return
	}

	if state.body != nil {
		writeJSONResponse(w, state.status, state.body)
		return
	}

	if state.status != 0 {
		w.WriteHeader(state.status)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func writeErrorResponse(w http.ResponseWriter, err *Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)
	json.NewEncoder(w).Encode(map[string]*Error{"error": err})
}

func writeJSONResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
