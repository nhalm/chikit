package errors_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhalm/chikit/errors"
)

func TestSanitize_SuccessResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success with /path/to/file.go:123"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := errors.Sanitize()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "/path/to/file.go:123") {
		t.Error("success response should not be sanitized")
	}
}

func TestSanitize_ErrorWithStackTrace(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`panic: runtime error
	at main.handler()
	goroutine 1 [running]:
	main.go:42 +0x123`))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := errors.Sanitize()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "goroutine") {
		t.Error("stack trace should be removed")
	}
	if strings.Contains(body, "main.go:42") {
		t.Error("file paths should be removed")
	}
}

func TestSanitize_ErrorWithFilePath(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error at /usr/local/app/handlers/api.go:156"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := errors.Sanitize()
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "/usr/local/app") {
		t.Error("file path should be removed")
	}
	if strings.Contains(body, "api.go") {
		t.Error("file name should be removed")
	}
}

func TestSanitize_4xxErrors(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("validation failed at /app/validator.go:42"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := errors.Sanitize()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "validator.go") {
		t.Error("file paths should be removed from 4xx errors")
	}
}
