// Package chikit provides production-grade middleware components for Chi routers.
//
// This file contains the core error types used throughout chikit for structured
// API error responses. These types enable consistent, Stripe-style error handling
// across all middleware and handlers.
package chikit

import (
	"net/http"
)

// APIError represents a structured API error response.
type APIError struct {
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
	Error *APIError `json:"error"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return e.Message
}

// Is implements errors.Is for comparing error types.
func (e *APIError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	t, ok := target.(*APIError)
	if !ok {
		return false
	}
	return e.Type == t.Type && e.Code == t.Code
}

// With returns a copy of the error with a custom message.
func (e *APIError) With(message string) *APIError {
	if e == nil {
		return nil
	}
	dup := *e
	dup.Message = message
	return &dup
}

// WithParam returns a copy of the error with a custom message and parameter.
func (e *APIError) WithParam(message, param string) *APIError {
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
	ErrBadRequest          = &APIError{Type: "request_error", Code: "bad_request", Message: "Bad request", Status: http.StatusBadRequest}
	ErrUnauthorized        = &APIError{Type: "auth_error", Code: "unauthorized", Message: "Unauthorized", Status: http.StatusUnauthorized}
	ErrPaymentRequired     = &APIError{Type: "request_error", Code: "payment_required", Message: "Payment required", Status: http.StatusPaymentRequired}
	ErrForbidden           = &APIError{Type: "auth_error", Code: "forbidden", Message: "Forbidden", Status: http.StatusForbidden}
	ErrNotFound            = &APIError{Type: "not_found", Code: "resource_not_found", Message: "Resource not found", Status: http.StatusNotFound}
	ErrMethodNotAllowed    = &APIError{Type: "request_error", Code: "method_not_allowed", Message: "Method not allowed", Status: http.StatusMethodNotAllowed}
	ErrConflict            = &APIError{Type: "request_error", Code: "conflict", Message: "Conflict", Status: http.StatusConflict}
	ErrGone                = &APIError{Type: "request_error", Code: "gone", Message: "Resource gone", Status: http.StatusGone}
	ErrPayloadTooLarge     = &APIError{Type: "request_error", Code: "payload_too_large", Message: "Payload too large", Status: http.StatusRequestEntityTooLarge}
	ErrUnprocessableEntity = &APIError{Type: "validation_error", Code: "unprocessable", Message: "Unprocessable entity", Status: http.StatusUnprocessableEntity}
	ErrRateLimited         = &APIError{Type: "rate_limit_error", Code: "limit_exceeded", Message: "Rate limit exceeded", Status: http.StatusTooManyRequests}
	ErrInternal            = &APIError{Type: "internal_error", Code: "internal", Message: "Internal server error", Status: http.StatusInternalServerError}
	ErrNotImplemented      = &APIError{Type: "request_error", Code: "not_implemented", Message: "Not implemented", Status: http.StatusNotImplemented}
	ErrServiceUnavailable  = &APIError{Type: "request_error", Code: "service_unavailable", Message: "Service unavailable", Status: http.StatusServiceUnavailable}
)

// NewValidationError creates a validation error with multiple field errors.
func NewValidationError(errors []FieldError) *APIError {
	return &APIError{
		Type:    "validation_error",
		Code:    "invalid_request",
		Message: "Validation failed",
		Errors:  errors,
		Status:  http.StatusBadRequest,
	}
}
