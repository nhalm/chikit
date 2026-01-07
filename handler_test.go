package chikit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/canonlog"
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

	var body map[string]*APIError
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

	var body map[string]*APIError
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

	req2 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if HasState(req2.Context()) {
		t.Error("expected HasState to return false without Handler")
	}
}

func TestAPIError_Is(t *testing.T) {
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
		{Param: "email", Code: "required", Message: "Email is required"},
		{Param: "age", Code: "min", Message: "Age must be at least 18"},
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
			{Param: "email", Code: "required", Message: "Email is required"},
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
	if body.Error.Errors[0].Param != "email" {
		t.Errorf("expected param 'email', got %s", body.Error.Errors[0].Param)
	}
}

func TestAllSentinelErrors(t *testing.T) {
	sentinels := []*APIError{
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
		ErrGatewayTimeout,
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

func TestAPIError_IsWithNilReceiverAndTarget(t *testing.T) {
	var nilErr *APIError

	if !nilErr.Is(nil) {
		t.Error("expected nil error to match nil target")
	}

	if nilErr.Is(ErrNotFound) {
		t.Error("expected nil error not to match non-nil target")
	}
}

func TestAPIError_WithNilReceiver(t *testing.T) {
	var nilErr *APIError

	result := nilErr.With("Some message")
	if result != nil {
		t.Error("expected With() on nil receiver to return nil")
	}
}

func TestAPIError_WithParamNilReceiver(t *testing.T) {
	var nilErr *APIError

	result := nilErr.WithParam("Some message", "param")
	if result != nil {
		t.Error("expected WithParam() on nil receiver to return nil")
	}
}

func TestHandler_JSONEncodingFailureBody(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		unencodable := make(chan int)
		SetResponse(r, http.StatusOK, map[string]any{"channel": unencodable})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", ct)
	}

	if body := rec.Body.String(); body != "Internal server error" {
		t.Errorf("expected body 'Internal server error', got %s", body)
	}
}

func TestHandler_ConcurrentSetError(t *testing.T) {
	const goroutines = 100

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				if idx%2 == 0 {
					SetError(r, ErrNotFound.With("Error from goroutine"))
				} else {
					SetError(r, ErrUnauthorized.With("Different error"))
				}
			}(i)
		}

		wg.Wait()
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d or %d, got %d", http.StatusNotFound, http.StatusUnauthorized, rec.Code)
	}

	var body map[string]*APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] == nil {
		t.Error("expected error in response")
	}
}

func TestHandler_ConcurrentSetResponse(t *testing.T) {
	const goroutines = 100

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				SetResponse(r, http.StatusOK, map[string]int{"value": idx})
			}(i)
		}

		wg.Wait()
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := body["value"]; !ok {
		t.Error("expected 'value' key in response")
	}
}

func TestHandler_ConcurrentSetHeader(t *testing.T) {
	const goroutines = 100

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(_ int) {
				defer wg.Done()
				SetHeader(r, "X-Request-ID", "test-id")
				AddHeader(r, "X-Custom", "value")
			}(i)
		}

		wg.Wait()
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Header().Get("X-Request-ID") != "test-id" {
		t.Errorf("expected X-Request-ID=test-id, got %s", rec.Header().Get("X-Request-ID"))
	}

	customHeaders := rec.Header().Values("X-Custom")
	if len(customHeaders) != goroutines {
		t.Errorf("expected %d X-Custom headers, got %d", goroutines, len(customHeaders))
	}
}

func TestHandler_ConcurrentMixedOperations(t *testing.T) {
	const goroutines = 50

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		wg.Add(goroutines * 3)

		for i := 0; i < goroutines; i++ {
			go func(_ int) {
				defer wg.Done()
				SetError(r, ErrNotFound)
			}(i)

			go func(idx int) {
				defer wg.Done()
				SetResponse(r, http.StatusOK, map[string]int{"id": idx})
			}(i)

			go func(_ int) {
				defer wg.Done()
				SetHeader(r, "X-Test", "value")
			}(i)
		}

		wg.Wait()
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestWithCanonlog_CreatesLogger(t *testing.T) {
	var loggerFound bool

	handler := Handler(WithCanonlog())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, loggerFound = canonlog.TryGetLogger(r.Context())
		SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !loggerFound {
		t.Error("expected canonlog logger to be in context")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWithCanonlog_Disabled(t *testing.T) {
	var loggerFound bool

	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, loggerFound = canonlog.TryGetLogger(r.Context())
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if loggerFound {
		t.Error("expected canonlog logger to not be in context when disabled")
	}
}

func TestWithCanonlogFields_AddsCustomFields(t *testing.T) {
	var capturedRequestID string

	handler := Handler(
		WithCanonlog(),
		WithCanonlogFields(func(r *http.Request) map[string]any {
			return map[string]any{
				"request_id": r.Header.Get("X-Request-ID"),
			}
		}),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		logger, _ := canonlog.TryGetLogger(r.Context())
		if logger != nil {
			capturedRequestID = r.Header.Get("X-Request-ID")
		}
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("X-Request-ID", "test-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedRequestID != "test-123" {
		t.Errorf("expected request_id 'test-123', got %s", capturedRequestID)
	}
}

func TestWithSLOs_LogsSLOStatus(t *testing.T) {
	handler := Handler(
		WithCanonlog(),
		WithSLOs(),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetResponse(r, http.StatusOK, nil)
	}))

	r := chi.NewRouter()
	r.With(SLO(SLOHighFast)).Get("/test", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWithSLOs_NoSLOOnRoute(t *testing.T) {
	handler := Handler(
		WithCanonlog(),
		WithSLOs(),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tier, _, found := GetSLO(r.Context())
		if found {
			t.Errorf("expected no SLO tier, got %s", tier)
		}
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWithSLOs_DisabledWithoutWithCanonlog(t *testing.T) {
	handler := Handler(
		WithSLOs(),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetResponse(r, http.StatusOK, nil)
	}))

	r := chi.NewRouter()
	r.With(SLO(SLOHighFast)).Get("/test", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWithCanonlog_ErrorLogging(t *testing.T) {
	handler := Handler(WithCanonlog())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetError(r, ErrNotFound.With("User not found"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestWithCanonlog_PanicLogging(t *testing.T) {
	handler := Handler(WithCanonlog())(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestHandler_Timeout_Fires(t *testing.T) {
	handler := Handler(WithTimeout(50 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			SetResponse(r, http.StatusOK, map[string]string{"status": "completed"})
		case <-r.Context().Done():
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}

	var body map[string]*APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"].Code != "gateway_timeout" {
		t.Errorf("expected code gateway_timeout, got %s", body["error"].Code)
	}
}

func TestHandler_Timeout_NotFired(t *testing.T) {
	handler := Handler(WithTimeout(200 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %s", body["status"])
	}
}

func TestHandler_Timeout_HandlerPanics(t *testing.T) {
	handler := Handler(WithTimeout(200 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("handler panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body map[string]*APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"].Type != "internal_error" {
		t.Errorf("expected type internal_error, got %s", body["error"].Type)
	}
}

func TestHandler_Timeout_PanicAfterTimeout(t *testing.T) {
	handler := Handler(
		WithTimeout(20*time.Millisecond),
		WithGracefulShutdown(100*time.Millisecond),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		time.Sleep(10 * time.Millisecond)
		panic("panic after timeout")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}
}

func TestHandler_Timeout_DoubleWrite(t *testing.T) {
	handler := Handler(WithTimeout(20 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		time.Sleep(10 * time.Millisecond)
		SetResponse(r, http.StatusOK, map[string]string{"status": "late"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d (timeout wins), got %d", http.StatusGatewayTimeout, rec.Code)
	}

	var body map[string]*APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"].Code != "gateway_timeout" {
		t.Errorf("expected code gateway_timeout (timeout wins), got %s", body["error"].Code)
	}
}

func TestHandler_Timeout_ContextCancelled(t *testing.T) {
	var ctxErr error

	handler := Handler(WithTimeout(20 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-r.Context().Done():
			ctxErr = r.Context().Err()
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}

	if ctxErr == nil {
		t.Error("expected context to be cancelled")
	}
}

func TestHandler_Timeout_GraceTimeout(t *testing.T) {
	handlerExited := make(chan struct{})

	handler := Handler(
		WithTimeout(20*time.Millisecond),
		WithGracefulShutdown(100*time.Millisecond),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer close(handlerExited)
		<-r.Context().Done()
		time.Sleep(30 * time.Millisecond)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}

	select {
	case <-handlerExited:
	case <-time.After(200 * time.Millisecond):
		t.Error("expected handler to exit within grace period")
	}
}

func TestHandler_Timeout_Abandoned(t *testing.T) {
	var abandonCalled bool
	var abandonMu sync.Mutex

	handler := Handler(
		WithTimeout(20*time.Millisecond),
		WithGracefulShutdown(30*time.Millisecond),
		WithAbandonCallback(func(_ *http.Request) {
			abandonMu.Lock()
			abandonCalled = true
			abandonMu.Unlock()
		}),
	)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}

	abandonMu.Lock()
	called := abandonCalled
	abandonMu.Unlock()

	if !called {
		t.Error("expected abandon callback to be called")
	}
}

func TestHandler_Timeout_NoTimeoutConfigured(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandler_Timeout_WithCanonlog(t *testing.T) {
	handler := Handler(
		WithTimeout(20*time.Millisecond),
		WithCanonlog(),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			SetResponse(r, http.StatusOK, nil)
		case <-r.Context().Done():
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}
}

func TestWaitForHandlers(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})

	handler := Handler(WithTimeout(500 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		time.Sleep(50 * time.Millisecond)
		SetResponse(r, http.StatusOK, nil)
		close(handlerDone)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	go handler.ServeHTTP(rec, req)

	<-handlerStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := WaitForHandlers(ctx)
	if err != nil {
		t.Errorf("expected WaitForHandlers to succeed, got: %v", err)
	}

	select {
	case <-handlerDone:
	default:
		t.Error("expected handler to have completed")
	}
}

func TestWaitForHandlers_Timeout(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})

	handler := Handler(WithTimeout(500 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		close(handlerStarted)
		time.Sleep(200 * time.Millisecond)
		SetResponse(r, http.StatusOK, nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	go handler.ServeHTTP(rec, req)

	<-handlerStarted

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := WaitForHandlers(ctx)
	if err == nil {
		t.Error("expected WaitForHandlers to timeout")
	}

	<-handlerDone
}

func TestHandler_Timeout_StateFrozenAfterWrite(t *testing.T) {
	handlerDone := make(chan struct{})

	handler := Handler(WithTimeout(20 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		<-r.Context().Done()
		time.Sleep(10 * time.Millisecond)
		// These should be no-ops since state is frozen after 504 is written
		SetError(r, ErrNotFound.With("Should be ignored"))
		SetResponse(r, http.StatusOK, map[string]string{"status": "ignored"})
		SetHeader(r, "X-Ignored", "value")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	<-handlerDone

	// Verify 504 was written, not the handler's response
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}

	// Verify the ignored header wasn't set
	if rec.Header().Get("X-Ignored") != "" {
		t.Error("expected X-Ignored header to not be set after state frozen")
	}

	var body map[string]*APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"].Code != "gateway_timeout" {
		t.Errorf("expected code gateway_timeout, got %s", body["error"].Code)
	}
}

func TestHandler_Timeout_Concurrent(t *testing.T) {
	const numRequests = 10

	handler := Handler(WithTimeout(50 * time.Millisecond))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Half the requests time out, half succeed
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		SetResponse(r, http.StatusOK, map[string]string{"path": r.URL.Path})
	}))

	var wg sync.WaitGroup
	results := make(chan int, numRequests*2)

	// Launch concurrent requests - half slow, half fast
	for i := range numRequests {
		wg.Add(2)

		// Slow request (will timeout)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/slow", http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- rec.Code
		}()

		// Fast request (will succeed)
		go func(n int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/fast/%d", n), http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- rec.Code
		}(i)
	}

	wg.Wait()
	close(results)

	// Count results
	var timeouts, successes int
	for code := range results {
		switch code {
		case http.StatusGatewayTimeout:
			timeouts++
		case http.StatusOK:
			successes++
		default:
			t.Errorf("unexpected status code: %d", code)
		}
	}

	if timeouts != numRequests {
		t.Errorf("expected %d timeouts, got %d", numRequests, timeouts)
	}
	if successes != numRequests {
		t.Errorf("expected %d successes, got %d", numRequests, successes)
	}
}
