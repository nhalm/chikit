package sanitize_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhalm/chikit/sanitize"
)

func TestSanitize_SuccessResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success with /path/to/file.go:123"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New()
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

	middleware := sanitize.New()
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

	middleware := sanitize.New()
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

	middleware := sanitize.New()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "validator.go") {
		t.Error("file paths should be removed from 4xx errors")
	}
}

func TestSanitize_WithStackTracesDisabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`panic: runtime error
	at main.handler()
	goroutine 1 [running]:
	main.go:42 +0x123`))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New(sanitize.WithStackTraces(false))
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "goroutine") {
		t.Error("stack trace should be kept when disabled")
	}
	if !strings.Contains(body, "at main.handler()") {
		t.Error("stack trace should be kept when disabled")
	}
}

func TestSanitize_WithFilePathsDisabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error at /usr/local/app/handlers/api.go:156"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New(sanitize.WithFilePaths(false))
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/usr/local/app") {
		t.Error("file path should be kept when disabled")
	}
	if !strings.Contains(body, "api.go:156") {
		t.Error("file path should be kept when disabled")
	}
}

func TestSanitize_WithReplacementMessage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("/usr/local/app/handlers/api.go:156"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	customMessage := "Service temporarily unavailable"
	middleware := sanitize.New(sanitize.WithReplacementMessage(customMessage))
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, customMessage) {
		t.Errorf("expected replacement message %q in response, got %q", customMessage, body)
	}
}

func TestSanitize_WithMultipleOptions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`panic at /app/main.go:42
	goroutine 1 [running]:
	error details`))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New(
		sanitize.WithStackTraces(true),
		sanitize.WithFilePaths(true),
		sanitize.WithReplacementMessage("Custom error message"),
	)
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "main.go") {
		t.Error("file path should be removed")
	}
	if strings.Contains(body, "goroutine") {
		t.Error("stack trace should be removed")
	}
}

func TestSanitize_EmptyErrorBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "Internal Server Error" {
		t.Errorf("expected default replacement message, got %q", body)
	}
}

func TestSanitize_OnlyFilePathInBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("/app/main.go:42"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New()
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "main.go") {
		t.Error("file path should be removed")
	}
	if body == "" {
		t.Error("should have replacement message when body is empty after sanitization")
	}
}

func TestSanitize_WindowsFilePath(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`error at C:\Users\app\handlers\api.go:156`))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New()
	middleware(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `C:\Users`) {
		t.Error("Windows file path should be removed")
	}
	if strings.Contains(body, "api.go") {
		t.Error("file name should be removed")
	}
}

func TestSanitize_3xxRedirect(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/redirect")
		w.WriteHeader(http.StatusMovedPermanently)
		w.Write([]byte("redirecting to /app/file.go:123"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := sanitize.New()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("expected status 301, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "/app/file.go:123") {
		t.Error("3xx response should not be sanitized")
	}
}
