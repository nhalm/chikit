package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nhalm/chikit/auth"
	"github.com/nhalm/chikit/wrapper"
)

func TestAPIKey_Valid(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok || key != "valid-key" {
			t.Error("API key not found in context")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "valid-key")
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKey_Invalid(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "invalid-key")
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_Missing(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_Optional(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator, auth.WithOptionalAPIKey())
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with optional key, got %d", rec.Code)
	}
}

func TestAPIKey_WithCustomHeader(t *testing.T) {
	validator := func(key string) bool {
		return key == "secret-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok || key != "secret-key" {
			t.Error("API key not found in context")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Custom-API-Key", "secret-key")
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator, auth.WithAPIKeyHeader("X-Custom-API-Key"))
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKey_WithCustomHeader_WrongHeader(t *testing.T) {
	validator := func(key string) bool {
		return key == "secret-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "secret-key")
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator, auth.WithAPIKeyHeader("X-Custom-API-Key"))
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_WithMultipleOptions(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok || key != "valid-key" {
			t.Error("API key not found in context")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Custom-Key", "valid-key")
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator,
		auth.WithAPIKeyHeader("X-Custom-Key"),
		auth.WithOptionalAPIKey(),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKey_OptionalWithMissingKey(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.APIKeyFromContext(r.Context()); ok {
			t.Error("API key should not be in context when missing")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.APIKey(validator, auth.WithOptionalAPIKey())
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with optional key, got %d", rec.Code)
	}
}

func TestBearerToken_Valid(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := auth.BearerTokenFromContext(r.Context())
		if !ok || token != "valid-token" {
			t.Error("Bearer token not found in context")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestBearerToken_Invalid(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestBearerToken_Missing(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestBearerToken_InvalidFormat(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestBearerToken_EmptyToken(t *testing.T) {
	validator := func(_ string) bool {
		return false
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestBearerToken_Optional(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator, auth.WithOptionalBearerToken())
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with optional token, got %d", rec.Code)
	}
}

func TestBearerToken_OptionalWithMissingToken(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerTokenFromContext(r.Context()); ok {
			t.Error("Bearer token should not be in context when missing")
		}
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := auth.BearerToken(validator, auth.WithOptionalBearerToken())
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with optional token, got %d", rec.Code)
	}
}

func TestAPIKey_WithWrapper_MissingKey(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	chain := wrapper.New()(auth.APIKey(validator)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "auth_error" {
		t.Errorf("expected error type auth_error, got %s", resp["error"].Type)
	}
	if resp["error"].Message != "Missing API key" {
		t.Errorf("expected message 'Missing API key', got %s", resp["error"].Message)
	}
}

func TestAPIKey_WithWrapper_InvalidKey(t *testing.T) {
	validator := func(key string) bool {
		return key == "valid-key"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "invalid-key")
	rec := httptest.NewRecorder()

	chain := wrapper.New()(auth.APIKey(validator)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Message != "Invalid API key" {
		t.Errorf("expected message 'Invalid API key', got %s", resp["error"].Message)
	}
}

func TestBearerToken_WithWrapper_Missing(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	chain := wrapper.New()(auth.BearerToken(validator)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "auth_error" {
		t.Errorf("expected error type auth_error, got %s", resp["error"].Type)
	}
	if resp["error"].Message != "Missing authorization header" {
		t.Errorf("expected message 'Missing authorization header', got %s", resp["error"].Message)
	}
}

func TestBearerToken_WithWrapper_InvalidFormat(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()

	chain := wrapper.New()(auth.BearerToken(validator)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Message != "Invalid authorization format" {
		t.Errorf("expected message 'Invalid authorization format', got %s", resp["error"].Message)
	}
}

func TestBearerToken_WithWrapper_InvalidToken(t *testing.T) {
	validator := func(token string) bool {
		return token == "valid-token"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	chain := wrapper.New()(auth.BearerToken(validator)(handler))
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	var resp map[string]wrapper.Error
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Message != "Invalid bearer token" {
		t.Errorf("expected message 'Invalid bearer token', got %s", resp["error"].Message)
	}
}
