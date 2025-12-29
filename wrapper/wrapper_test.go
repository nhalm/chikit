package wrapper

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_SuccessResponse(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetResponse(r, http.StatusCreated, map[string]string{"id": "123"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["id"] != "123" {
		t.Errorf("expected id=123, got %s", body["id"])
	}
}

func TestHandler_ErrorResponse(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetError(r, ErrNotFound.With("User not found"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}

	var body map[string]*Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errResp := body["error"]
	if errResp.Type != "not_found" {
		t.Errorf("expected type not_found, got %s", errResp.Type)
	}
	if errResp.Message != "User not found" {
		t.Errorf("expected message 'User not found', got %s", errResp.Message)
	}
}

func TestHandler_ErrorTakesPrecedence(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
		SetError(r, ErrUnauthorized)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandler_PanicRecovery(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("something went wrong")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body map[string]*Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"].Type != "internal_error" {
		t.Errorf("expected type internal_error, got %s", body["error"].Type)
	}
}

func TestHandler_CustomHeaders(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetHeader(r, "X-Request-ID", "abc123")
		SetHeader(r, "X-RateLimit-Remaining", "99")
		SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "abc123" {
		t.Errorf("expected X-Request-ID=abc123, got %s", rec.Header().Get("X-Request-ID"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "99" {
		t.Errorf("expected X-RateLimit-Remaining=99, got %s", rec.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestHandler_EmptyResponse(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// No SetResponse or SetError called
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandler_StatusOnlyResponse(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetResponse(r, http.StatusNoContent, nil)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestHasState(t *testing.T) {
	var hasStateInHandler bool

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hasStateInHandler = HasState(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !hasStateInHandler {
		t.Error("expected HasState to return true inside Handler")
	}

	// Without wrapper
	req2 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if HasState(req2.Context()) {
		t.Error("expected HasState to return false without Handler")
	}
}

func TestError_With(t *testing.T) {
	err := ErrNotFound.With("Custom message")

	if err.Message != "Custom message" {
		t.Errorf("expected message 'Custom message', got %s", err.Message)
	}
	if err.Type != ErrNotFound.Type {
		t.Errorf("expected type %s, got %s", ErrNotFound.Type, err.Type)
	}
	if err.Code != ErrNotFound.Code {
		t.Errorf("expected code %s, got %s", ErrNotFound.Code, err.Code)
	}
	if err.Status != ErrNotFound.Status {
		t.Errorf("expected status %d, got %d", ErrNotFound.Status, err.Status)
	}

	// Original unchanged
	if ErrNotFound.Message != "Resource not found" {
		t.Error("original sentinel was modified")
	}
}

func TestError_WithParam(t *testing.T) {
	err := ErrBadRequest.WithParam("Invalid email format", "email")

	if err.Message != "Invalid email format" {
		t.Errorf("expected message 'Invalid email format', got %s", err.Message)
	}
	if err.Param != "email" {
		t.Errorf("expected param 'email', got %s", err.Param)
	}
}

func TestError_Is(t *testing.T) {
	err := ErrNotFound.With("User not found")

	if !errors.Is(err, ErrNotFound) {
		t.Error("expected errors.Is to match ErrNotFound")
	}

	if errors.Is(err, ErrUnauthorized) {
		t.Error("expected errors.Is not to match ErrUnauthorized")
	}
}

func TestNewValidationError(t *testing.T) {
	fieldErrors := []FieldError{
		{Field: "email", Code: "required", Message: "Email is required"},
		{Field: "age", Code: "min", Message: "Age must be at least 18"},
	}

	err := NewValidationError(fieldErrors)

	if err.Type != "validation_error" {
		t.Errorf("expected type validation_error, got %s", err.Type)
	}
	if err.Code != "invalid_request" {
		t.Errorf("expected code invalid_request, got %s", err.Code)
	}
	if len(err.Errors) != 2 {
		t.Errorf("expected 2 field errors, got %d", len(err.Errors))
	}
	if err.Status != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, err.Status)
	}
}

func TestValidationError_JSONFormat(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetError(r, NewValidationError([]FieldError{
			{Field: "email", Code: "required", Message: "Email is required"},
		}))
	}))

	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Type    string       `json:"type"`
			Code    string       `json:"code"`
			Message string       `json:"message"`
			Errors  []FieldError `json:"errors"`
		} `json:"error"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Error.Type != "validation_error" {
		t.Errorf("expected type validation_error, got %s", body.Error.Type)
	}
	if len(body.Error.Errors) != 1 {
		t.Errorf("expected 1 field error, got %d", len(body.Error.Errors))
	}
	if body.Error.Errors[0].Field != "email" {
		t.Errorf("expected field 'email', got %s", body.Error.Errors[0].Field)
	}
}

func TestAddHeader(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		AddHeader(r, "X-Custom", "value1")
		AddHeader(r, "X-Custom", "value2")
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	values := rec.Header().Values("X-Custom")
	if len(values) != 2 {
		t.Errorf("expected 2 header values, got %d", len(values))
	}
}

func TestAllSentinelErrors(t *testing.T) {
	sentinels := []*Error{
		ErrBadRequest,
		ErrUnauthorized,
		ErrPaymentRequired,
		ErrForbidden,
		ErrNotFound,
		ErrMethodNotAllowed,
		ErrConflict,
		ErrGone,
		ErrPayloadTooLarge,
		ErrUnprocessableEntity,
		ErrRateLimited,
		ErrInternal,
		ErrNotImplemented,
		ErrServiceUnavailable,
	}

	for _, sentinel := range sentinels {
		if sentinel.Type == "" {
			t.Errorf("sentinel %s has empty Type", sentinel.Code)
		}
		if sentinel.Code == "" {
			t.Errorf("sentinel with Type %s has empty Code", sentinel.Type)
		}
		if sentinel.Message == "" {
			t.Errorf("sentinel %s has empty Message", sentinel.Code)
		}
		if sentinel.Status == 0 {
			t.Errorf("sentinel %s has zero Status", sentinel.Code)
		}
	}
}
