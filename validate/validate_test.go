package validate_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhalm/chikit/validate"
	"github.com/nhalm/chikit/wrapper"
)

func TestMaxBodySize_WithinLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	})

	body := bytes.NewBufferString("small body")
	req := httptest.NewRequest("POST", "/", body)
	rec := httptest.NewRecorder()

	middleware := validate.MaxBodySize(1024)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "small body" {
		t.Error("body should be preserved when under limit")
	}
}

func TestMaxBodySize_ExceedsLimit(t *testing.T) {
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.Write([]byte("ok"))
	})

	body := bytes.NewBufferString(strings.Repeat("x", 2000))
	req := httptest.NewRequest("POST", "/", body)
	req.ContentLength = 2000
	rec := httptest.NewRecorder()

	middleware := validate.MaxBodySize(1024)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called when Content-Length exceeds limit")
	}
}

func TestMaxBodySize_ExceedsLimit_WithWrapper(t *testing.T) {
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.Write([]byte("ok"))
	})

	body := bytes.NewBufferString(strings.Repeat("x", 2000))
	req := httptest.NewRequest("POST", "/", body)
	req.ContentLength = 2000
	rec := httptest.NewRecorder()

	chain := wrapper.New()(validate.MaxBodySize(1024)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called when Content-Length exceeds limit")
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"].Code != "payload_too_large" {
		t.Errorf("expected code payload_too_large, got %s", resp["error"].Code)
	}
}

func TestMaxBodySize_ChunkedTransfer_ExceedsLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.Write([]byte("ok"))
	})

	body := bytes.NewBufferString(strings.Repeat("x", 2000))
	req := httptest.NewRequest("POST", "/", body)
	rec := httptest.NewRecorder()

	middleware := validate.MaxBodySize(1024)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
}

func TestHeaders_Required(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-API-Key", validate.WithRequired()),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_MissingRequired(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-API-Key", validate.WithRequired()),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Missing required header: X-API-Key") {
		t.Error("should return error message for missing required header")
	}
}

func TestHeaders_AllowList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Environment", "production")
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-Environment", validate.WithAllowList("production", "staging")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_AllowListFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Environment", "development")
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-Environment", validate.WithAllowList("production", "staging")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not in allowed list") {
		t.Error("should return error message for disallowed value")
	}
}

func TestHeaders_DenyList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Source", "api")
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-Source", validate.WithDenyList("blocked", "banned")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_DenyListFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Source", "blocked")
	rec := httptest.NewRecorder()

	middleware := validate.NewHeaders(
		validate.WithHeader("X-Source", validate.WithDenyList("blocked", "banned")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "is denied") {
		t.Error("should return error message for denied value")
	}
}

func TestHeaders_CaseSensitive_AllowList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	tests := []struct {
		name           string
		headerValue    string
		allowList      []string
		caseSensitive  bool
		expectedStatus int
		description    string
	}{
		{
			name:           "case_insensitive_matches_different_case",
			headerValue:    "Application/JSON",
			allowList:      []string{"application/json"},
			caseSensitive:  false,
			expectedStatus: http.StatusOK,
			description:    "without WithCaseSensitive, different cases should match",
		},
		{
			name:           "case_sensitive_rejects_different_case",
			headerValue:    "Application/JSON",
			allowList:      []string{"application/json"},
			caseSensitive:  true,
			expectedStatus: http.StatusBadRequest,
			description:    "with WithCaseSensitive, different cases should not match",
		},
		{
			name:           "case_sensitive_accepts_exact_match",
			headerValue:    "application/json",
			allowList:      []string{"application/json"},
			caseSensitive:  true,
			expectedStatus: http.StatusOK,
			description:    "with WithCaseSensitive, exact case should match",
		},
		{
			name:           "case_insensitive_all_lowercase",
			headerValue:    "application/json",
			allowList:      []string{"APPLICATION/JSON"},
			caseSensitive:  false,
			expectedStatus: http.StatusOK,
			description:    "case insensitive should match regardless of list case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", http.NoBody)
			req.Header.Set("Content-Type", tt.headerValue)
			rec := httptest.NewRecorder()

			var opts []validate.HeaderOption
			opts = append(opts, validate.WithAllowList(tt.allowList...))
			if tt.caseSensitive {
				opts = append(opts, validate.WithCaseSensitive())
			}

			middleware := validate.NewHeaders(
				validate.WithHeader("Content-Type", opts...),
			)
			middleware(handler).ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestHeaders_CaseSensitive_DenyList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	tests := []struct {
		name           string
		headerValue    string
		denyList       []string
		caseSensitive  bool
		expectedStatus int
		description    string
	}{
		{
			name:           "case_insensitive_blocks_different_case",
			headerValue:    "BLOCKED",
			denyList:       []string{"blocked"},
			caseSensitive:  false,
			expectedStatus: http.StatusBadRequest,
			description:    "without WithCaseSensitive, different cases should be blocked",
		},
		{
			name:           "case_sensitive_allows_different_case",
			headerValue:    "BLOCKED",
			denyList:       []string{"blocked"},
			caseSensitive:  true,
			expectedStatus: http.StatusOK,
			description:    "with WithCaseSensitive, different cases should not match deny list",
		},
		{
			name:           "case_sensitive_blocks_exact_match",
			headerValue:    "blocked",
			denyList:       []string{"blocked"},
			caseSensitive:  true,
			expectedStatus: http.StatusBadRequest,
			description:    "with WithCaseSensitive, exact case should be blocked",
		},
		{
			name:           "case_insensitive_any_case_blocked",
			headerValue:    "BlOcKeD",
			denyList:       []string{"BLOCKED"},
			caseSensitive:  false,
			expectedStatus: http.StatusBadRequest,
			description:    "case insensitive should block regardless of case variations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", http.NoBody)
			req.Header.Set("X-Source", tt.headerValue)
			rec := httptest.NewRecorder()

			var opts []validate.HeaderOption
			opts = append(opts, validate.WithDenyList(tt.denyList...))
			if tt.caseSensitive {
				opts = append(opts, validate.WithCaseSensitive())
			}

			middleware := validate.NewHeaders(
				validate.WithHeader("X-Source", opts...),
			)
			middleware(handler).ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestHeaders_WithWrapper_MissingRequired(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	chain := wrapper.New()(validate.NewHeaders(
		validate.WithHeader("X-API-Key", validate.WithRequired()),
	)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "validation_error" {
		t.Errorf("expected error type validation_error, got %s", resp["error"].Type)
	}
	if resp["error"].Code != "missing_header" {
		t.Errorf("expected code missing_header, got %s", resp["error"].Code)
	}
	if resp["error"].Param != "X-API-Key" {
		t.Errorf("expected param 'X-API-Key', got %s", resp["error"].Param)
	}
}

func TestHeaders_WithWrapper_NotInAllowList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Environment", "development")
	rec := httptest.NewRecorder()

	chain := wrapper.New()(validate.NewHeaders(
		validate.WithHeader("X-Environment", validate.WithAllowList("production", "staging")),
	)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "validation_error" {
		t.Errorf("expected error type validation_error, got %s", resp["error"].Type)
	}
	if resp["error"].Code != "invalid_header" {
		t.Errorf("expected code invalid_header, got %s", resp["error"].Code)
	}
}
