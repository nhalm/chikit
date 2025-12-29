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
	handler := wrapper.Handler()(validate.MaxBodySize(1024)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"body": string(body)})
	})))

	body := bytes.NewBufferString("small body")
	req := httptest.NewRequest("POST", "/", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestMaxBodySize_ExceedsLimit(t *testing.T) {
	handler := wrapper.Handler()(validate.MaxBodySize(1024)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			wrapper.SetError(r, wrapper.ErrPayloadTooLarge)
			return
		}
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	body := bytes.NewBufferString(strings.Repeat("x", 2000))
	req := httptest.NewRequest("POST", "/", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
}

func TestQueryParams_Required(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("user", validate.WithRequired()),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?user=123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryParams_MissingRequired(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("user", validate.WithRequired()),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error.Type != "validation_error" {
		t.Errorf("expected type validation_error, got %s", resp.Error.Type)
	}
	if resp.Error.Param != "user" {
		t.Errorf("expected param user, got %s", resp.Error.Param)
	}
}

func TestQueryParams_WithDefault(t *testing.T) {
	var limitValue string
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("limit", validate.WithDefault("10")),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		limitValue = r.URL.Query().Get("limit")
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"limit": limitValue})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if limitValue != "10" {
		t.Errorf("expected default value '10', got '%s'", limitValue)
	}
}

func TestQueryParams_WithValidator(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("age", validate.WithValidator(validate.OneOf("18", "21", "25"))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?age=25", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryParams_ValidatorFails(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("age", validate.WithValidator(validate.OneOf("18", "21", "25"))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?age=30", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestQueryParams_MultipleValues(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()["tags"]
		if len(values) == 2 && values[0] == "a" && values[1] == "b" {
			wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
		} else {
			wrapper.SetError(r, wrapper.ErrBadRequest)
		}
	})))

	req := httptest.NewRequest("GET", "/?tags=a&tags=b", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_Required(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-API-Key", validate.WithRequiredHeader()),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_MissingRequired(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-API-Key", validate.WithRequiredHeader()),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Type  string `json:"type"`
			Param string `json:"param"`
		} `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error.Type != "validation_error" {
		t.Errorf("expected type validation_error, got %s", resp.Error.Type)
	}
	if resp.Error.Param != "X-API-Key" {
		t.Errorf("expected param X-API-Key, got %s", resp.Error.Param)
	}
}

func TestHeaders_AllowList(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Environment", "production")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_AllowListFails(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-Environment", validate.WithAllowList("production", "staging")),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Environment", "development")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestHeaders_DenyList(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-Source", validate.WithDenyList("blocked", "banned")),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Source", "api")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHeaders_DenyListFails(t *testing.T) {
	handler := wrapper.Handler()(validate.Headers(
		validate.Header("X-Source", validate.WithDenyList("blocked", "banned")),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.Header.Set("X-Source", "blocked")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestPattern_ValidRegex(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("email", validate.WithValidator(validate.Pattern(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?email=test@example.com", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPattern_InvalidRegex(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("email", validate.WithValidator(validate.Pattern(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?email=invalid-email", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestPattern_NumericPattern(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("id", validate.WithValidator(validate.Pattern(`^\d+$`))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?id=12345", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPattern_NumericPatternFails(t *testing.T) {
	handler := wrapper.Handler()(validate.QueryParams(
		validate.Param("id", validate.WithValidator(validate.Pattern(`^\d+$`))),
	)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapper.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})))

	req := httptest.NewRequest("GET", "/?id=abc123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}
