package bind_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/nhalm/chikit/bind"
	"github.com/nhalm/chikit/wrapper"
)

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=2,max=100"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=18,lte=120"`
}

func TestJSON_ValidRequest(t *testing.T) {
	body := `{"name": "John Doe", "email": "john@example.com", "age": 30}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if user.Name != "John Doe" {
		t.Errorf("expected name 'John Doe', got %s", user.Name)
	}
	if user.Email != "john@example.com" {
		t.Errorf("expected email 'john@example.com', got %s", user.Email)
	}
	if user.Age != 30 {
		t.Errorf("expected age 30, got %d", user.Age)
	}
}

func TestJSON_InvalidJSON(t *testing.T) {
	body := `{"name": invalid json`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	wrapperErr, ok := err.(*wrapper.Error)
	if !ok {
		t.Fatal("expected *wrapper.Error")
	}

	if wrapperErr.Type != "request_error" {
		t.Errorf("expected type 'request_error', got %s", wrapperErr.Type)
	}
	if wrapperErr.Code != "invalid_json" {
		t.Errorf("expected code 'invalid_json', got %s", wrapperErr.Code)
	}
	if wrapperErr.Status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", wrapperErr.Status)
	}
}

func TestJSON_MissingRequiredField(t *testing.T) {
	body := `{"email": "john@example.com", "age": 30}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected validation error")
	}

	wrapperErr, ok := err.(*wrapper.Error)
	if !ok {
		t.Fatal("expected *wrapper.Error")
	}

	if wrapperErr.Type != "validation_error" {
		t.Errorf("expected type 'validation_error', got %s", wrapperErr.Type)
	}
	if wrapperErr.Code != "invalid_request" {
		t.Errorf("expected code 'invalid_request', got %s", wrapperErr.Code)
	}
	if len(wrapperErr.Errors) == 0 {
		t.Fatal("expected field errors")
	}

	foundNameError := false
	for _, fe := range wrapperErr.Errors {
		if fe.Field == "name" && fe.Code == "required" {
			foundNameError = true
			break
		}
	}
	if !foundNameError {
		t.Error("expected field error for 'name' with code 'required'")
	}
}

func TestJSON_InvalidEmail(t *testing.T) {
	body := `{"name": "John", "email": "invalid-email", "age": 30}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected validation error")
	}

	wrapperErr := err.(*wrapper.Error)
	foundEmailError := false
	for _, fe := range wrapperErr.Errors {
		if fe.Field == "email" && fe.Code == "email" {
			foundEmailError = true
			if fe.Message != "email must be a valid email address" {
				t.Errorf("unexpected message: %s", fe.Message)
			}
			break
		}
	}
	if !foundEmailError {
		t.Error("expected field error for 'email'")
	}
}

func TestJSON_AgeOutOfRange(t *testing.T) {
	body := `{"name": "John", "email": "john@example.com", "age": 15}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected validation error")
	}

	wrapperErr := err.(*wrapper.Error)
	foundAgeError := false
	for _, fe := range wrapperErr.Errors {
		if fe.Field == "age" && fe.Code == "gte" {
			foundAgeError = true
			break
		}
	}
	if !foundAgeError {
		t.Error("expected field error for 'age' with code 'gte'")
	}
}

func TestJSON_MultipleErrors(t *testing.T) {
	body := `{"name": "", "email": "invalid", "age": 200}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected validation error")
	}

	wrapperErr := err.(*wrapper.Error)
	if len(wrapperErr.Errors) < 2 {
		t.Errorf("expected multiple field errors, got %d", len(wrapperErr.Errors))
	}
}

func TestJSON_EmptyBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(""))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestJSON_WithWrapper(t *testing.T) {
	handler := wrapper.Handler()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var user CreateUserRequest
		if err := bind.JSON(r, &user); err != nil {
			wrapper.SetError(r, err.(*wrapper.Error))
			return
		}
		wrapper.SetResponse(r, http.StatusCreated, user)
	}))

	body := `{"name": "J", "email": "john@example.com", "age": 30}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp struct {
		Error struct {
			Type   string `json:"type"`
			Code   string `json:"code"`
			Errors []struct {
				Field   string `json:"field"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error.Type != "validation_error" {
		t.Errorf("expected type 'validation_error', got %s", resp.Error.Type)
	}
}

type EnumRequest struct {
	Status Status `json:"status" validate:"required,status"`
}

type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusActive, StatusInactive:
		return true
	}
	return false
}

func TestRegisterValidation_CustomEnum(t *testing.T) {
	err := bind.RegisterValidation("status", func(fl validator.FieldLevel) bool {
		status, ok := fl.Field().Interface().(Status)
		if !ok {
			return false
		}
		return status.IsValid()
	})
	if err != nil {
		t.Fatalf("failed to register validation: %v", err)
	}

	body := `{"status": "active"}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var enumReq EnumRequest
	bindErr := bind.JSON(req, &enumReq)

	if bindErr != nil {
		t.Errorf("expected no error for valid status, got %v", bindErr)
	}

	body = `{"status": "invalid"}`
	req = httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	bindErr = bind.JSON(req, &enumReq)
	if bindErr == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestJSON_ErrorsIs(t *testing.T) {
	body := `{"name": "", "email": "", "age": 0}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))

	var user CreateUserRequest
	err := bind.JSON(req, &user)

	if err == nil {
		t.Fatal("expected error")
	}

	wrapperErr := err.(*wrapper.Error)
	validationErr := &wrapper.Error{Type: "validation_error", Code: "invalid_request"}

	if !errors.Is(wrapperErr, validationErr) {
		t.Error("expected error to match validation_error type")
	}
}
