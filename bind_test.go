package chikit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
)

type CreateUserRequest struct {
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"min=18"`
}

type ListUsersRequest struct {
	Page   int    `query:"page" validate:"omitempty,min=1"`
	Limit  int    `query:"limit" validate:"omitempty,min=1,max=100"`
	Status string `query:"status" validate:"omitempty,oneof=active inactive"`
}

func TestJSON_ValidInput(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"email": "test@example.com", "age": 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp CreateUserRequest
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", resp.Email)
	}
	if resp.Age != 25 {
		t.Errorf("expected age 25, got %d", resp.Age)
	}
}

func TestJSON_MalformedJSON(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"email": "test@example.com", age: 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp map[string]APIError
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Type != "request_error" {
		t.Errorf("expected error type request_error, got %s", resp["error"].Type)
	}
	if resp["error"].Message != "Invalid JSON request body" {
		t.Errorf("expected message 'Invalid JSON request body', got %s", resp["error"].Message)
	}
}

func TestJSON_ValidationFailure(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"email": "invalid-email", "age": 15}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Type    string       `json:"type"`
			Code    string       `json:"code"`
			Message string       `json:"message"`
			Errors  []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Type != "validation_error" {
		t.Errorf("expected error type validation_error, got %s", resp.Error.Type)
	}
	if len(resp.Error.Errors) != 2 {
		t.Errorf("expected 2 field errors, got %d", len(resp.Error.Errors))
	}
}

func TestJSON_MissingRequired(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"age": 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Errors []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Error.Errors) != 1 {
		t.Fatalf("expected 1 field error, got %d", len(resp.Error.Errors))
	}
	if resp.Error.Errors[0].Param != "email" {
		t.Errorf("expected param 'email', got %s", resp.Error.Errors[0].Param)
	}
	if resp.Error.Errors[0].Code != "required" {
		t.Errorf("expected code 'required', got %s", resp.Error.Errors[0].Code)
	}
}

func TestQuery_ValidInput(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req ListUsersRequest
		if !Query(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?page=2&limit=50&status=active", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp ListUsersRequest
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Page != 2 {
		t.Errorf("expected page 2, got %d", resp.Page)
	}
	if resp.Limit != 50 {
		t.Errorf("expected limit 50, got %d", resp.Limit)
	}
	if resp.Status != "active" {
		t.Errorf("expected status 'active', got %s", resp.Status)
	}
}

func TestQuery_ValidationFailure(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req ListUsersRequest
		if !Query(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?page=-1&limit=200&status=unknown", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Type   string       `json:"type"`
			Errors []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Type != "validation_error" {
		t.Errorf("expected error type validation_error, got %s", resp.Error.Type)
	}
	if len(resp.Error.Errors) != 3 {
		t.Errorf("expected 3 field errors, got %d", len(resp.Error.Errors))
	}
}

func TestQuery_TypeConversionError(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req ListUsersRequest
		if !Query(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?page=notanumber", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp map[string]APIError
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Message != "Invalid query parameters" {
		t.Errorf("expected message 'Invalid query parameters', got %s", resp["error"].Message)
	}
}

func TestCustomFormatter(t *testing.T) {
	customFormatter := func(field, tag, _ string) string {
		return "CUSTOM:" + field + ":" + tag
	}

	handler := Handler()(Binder(WithFormatter(customFormatter))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"age": 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp struct {
		Error struct {
			Errors []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Errors[0].Message != "CUSTOM:email:required" {
		t.Errorf("expected custom message 'CUSTOM:email:required', got %s", resp.Error.Errors[0].Message)
	}
}

func TestDefaultFormatterWithoutMiddleware(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	}))

	body := `{"age": 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp struct {
		Error struct {
			Errors []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Errors[0].Message != "required" {
		t.Errorf("expected default message 'required', got %s", resp.Error.Errors[0].Message)
	}
}

func TestRegisterValidation(t *testing.T) {
	err := RegisterValidation("customtag", func(fl validator.FieldLevel) bool {
		return fl.Field().String() == "valid"
	})
	if err != nil {
		t.Fatalf("failed to register validation: %v", err)
	}

	type CustomRequest struct {
		Value string `json:"value" validate:"customtag"`
	}

	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CustomRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"value": "valid"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body = `{"value": "invalid"}`
	req = httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestJSON_EmptyBody(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestDefaultFormatter_AllTags(t *testing.T) {
	type AllTagsRequest struct {
		Email  string `json:"email" validate:"email"`
		Age    int    `json:"age" validate:"min=18"`
		Count  int    `json:"count" validate:"max=100"`
		Status string `json:"status" validate:"oneof=a b c"`
		ID     string `json:"id" validate:"uuid"`
		URL    string `json:"url" validate:"url"`
		NoOp   string `json:"noop" validate:"alpha"`
	}

	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req AllTagsRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	body := `{"email": "x", "age": 1, "count": 200, "status": "x", "id": "x", "url": "x", "noop": "123"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp struct {
		Error struct {
			Errors []FieldError `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	messages := make(map[string]string)
	for _, e := range resp.Error.Errors {
		messages[e.Param] = e.Message
	}

	if messages["email"] != "must be a valid email" {
		t.Errorf("expected email message 'must be a valid email', got %s", messages["email"])
	}
	if messages["age"] != "must be at least 18" {
		t.Errorf("expected age message 'must be at least 18', got %s", messages["age"])
	}
	if messages["count"] != "must be at most 100" {
		t.Errorf("expected count message 'must be at most 100', got %s", messages["count"])
	}
	if messages["status"] != "must be one of: a b c" {
		t.Errorf("expected status message 'must be one of: a b c', got %s", messages["status"])
	}
	if messages["id"] != "must be a valid UUID" {
		t.Errorf("expected id message 'must be a valid UUID', got %s", messages["id"])
	}
	if messages["url"] != "must be a valid URL" {
		t.Errorf("expected url message 'must be a valid URL', got %s", messages["url"])
	}
	if messages["noop"] != "alpha" {
		t.Errorf("expected noop message 'alpha', got %s", messages["noop"])
	}
}

func TestQuery_NilPointer(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req *ListUsersRequest
		if !Query(r, req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?page=1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for nil pointer, got %d", rec.Code)
	}
}

func TestQuery_NonPointer(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req ListUsersRequest
		if !Query(r, req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?page=1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for non-pointer, got %d", rec.Code)
	}
}

func TestQuery_PointerToNonStruct(t *testing.T) {
	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var s string
		if !Query(r, &s) {
			return
		}
		SetResponse(r, http.StatusOK, s)
	})))

	req := httptest.NewRequest("GET", "/?page=1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for pointer to non-struct, got %d", rec.Code)
	}
}

func TestQuery_IntegerOverflow(t *testing.T) {
	type SmallIntRequest struct {
		Count int8 `query:"count"`
	}

	handler := Handler()(Binder()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var req SmallIntRequest
		if !Query(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	})))

	req := httptest.NewRequest("GET", "/?count=1000", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for integer overflow, got %d", rec.Code)
	}
}

func TestJSON_BodyTooLarge(t *testing.T) {
	handler := Handler()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10)

		var req CreateUserRequest
		if !JSON(r, &req) {
			return
		}
		SetResponse(r, http.StatusOK, req)
	}))

	body := `{"email": "test@example.com", "age": 25}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}

	var resp map[string]APIError
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"].Code != "payload_too_large" {
		t.Errorf("expected code payload_too_large, got %s", resp["error"].Code)
	}
	if resp["error"].Message != "Request body too large" {
		t.Errorf("expected message 'Request body too large', got %s", resp["error"].Message)
	}
}
