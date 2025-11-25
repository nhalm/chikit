package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nhalm/chikit/auth"
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

	middleware := auth.APIKey(validator, auth.OptionalAPIKey())
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
		auth.OptionalAPIKey(),
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

	middleware := auth.APIKey(validator, auth.OptionalAPIKey())
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

	middleware := auth.BearerToken(validator, auth.OptionalBearerToken())
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

	middleware := auth.BearerToken(validator, auth.OptionalBearerToken())
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with optional token, got %d", rec.Code)
	}
}
