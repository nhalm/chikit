package validate_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhalm/chikit/validate"
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

func TestQueryParams_Required(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?user=123", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("user", validate.WithRequired()),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryParams_MissingRequired(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("user", validate.WithRequired()),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Missing required query parameter: user") {
		t.Error("should return error message for missing required param")
	}
}

func TestQueryParams_WithDefault(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("10"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("limit", validate.WithDefault("10")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Body.String() != "10" {
		t.Errorf("expected default value '10', got '%s'", rec.Body.String())
	}
}

func TestQueryParams_WithValidator(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?age=25", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("age", validate.WithValidator(validate.OneOf("18", "21", "25"))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryParams_ValidatorFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?age=30", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("age", validate.WithValidator(validate.OneOf("18", "21", "25"))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestQueryParams_MultipleValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()["tags"]
		if len(values) == 2 && values[0] == "a" && values[1] == "b" {
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	req := httptest.NewRequest("GET", "/?tags=a&tags=b", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams()
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "ok" {
		t.Error("multiple query param values should be preserved")
	}
}

func TestHeaders_Required(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	middleware := validate.Headers(
		validate.Header("X-API-Key", validate.WithRequiredHeader()),
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

	middleware := validate.Headers(
		validate.Header("X-API-Key", validate.WithRequiredHeader()),
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

	middleware := validate.Headers(
		validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
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

	middleware := validate.Headers(
		validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
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

	middleware := validate.Headers(
		validate.Header("X-Source", validate.WithDenyList("blocked", "banned")),
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

	middleware := validate.Headers(
		validate.Header("X-Source", validate.WithDenyList("blocked", "banned")),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "is denied") {
		t.Error("should return error message for denied value")
	}
}

func TestPattern_ValidRegex(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?email=test@example.com", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("email", validate.WithValidator(validate.Pattern(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPattern_InvalidRegex(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?email=invalid-email", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("email", validate.WithValidator(validate.Pattern(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "must match pattern") {
		t.Errorf("should return error message for pattern mismatch, got: %s", rec.Body.String())
	}
}

func TestPattern_NumericPattern(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?id=12345", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("id", validate.WithValidator(validate.Pattern(`^\d+$`))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPattern_NumericPatternFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/?id=abc123", http.NoBody)
	rec := httptest.NewRecorder()

	middleware := validate.QueryParams(
		validate.Param("id", validate.WithValidator(validate.Pattern(`^\d+$`))),
	)
	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}
