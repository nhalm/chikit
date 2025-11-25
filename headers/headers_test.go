package headers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicHeaderExtraction(t *testing.T) {
	tests := []struct {
		name       string
		headerVal  string
		wantStatus int
		wantInCtx  bool
	}{
		{
			name:       "header present",
			headerVal:  "test-value",
			wantStatus: http.StatusOK,
			wantInCtx:  true,
		},
		{
			name:       "header missing",
			headerVal:  "",
			wantStatus: http.StatusOK,
			wantInCtx:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			handler := New("X-Custom-Header", "custom_key")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set("X-Custom-Header", tt.headerVal)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			val, ok := FromContext(capturedCtx, "custom_key")
			if ok != tt.wantInCtx {
				t.Errorf("value in context = %v, want %v", ok, tt.wantInCtx)
			}

			if tt.wantInCtx && val != tt.headerVal {
				t.Errorf("context value = %v, want %v", val, tt.headerVal)
			}
		})
	}
}

func TestRequiredHeaders(t *testing.T) {
	tests := []struct {
		name       string
		headerVal  string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "required header present",
			headerVal:  "present",
			wantStatus: http.StatusOK,
			wantBody:   "",
		},
		{
			name:       "required header missing",
			headerVal:  "",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Missing required header: X-Required-Header\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := New("X-Required-Header", "required_key", WithRequired())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set("X-Required-Header", tt.headerVal)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestDefaultValues(t *testing.T) {
	tests := []struct {
		name       string
		headerVal  string
		defaultVal string
		wantVal    string
		wantStatus int
	}{
		{
			name:       "header present, default ignored",
			headerVal:  "custom-value",
			defaultVal: "default-value",
			wantVal:    "custom-value",
			wantStatus: http.StatusOK,
		},
		{
			name:       "header missing, default used",
			headerVal:  "",
			defaultVal: "default-value",
			wantVal:    "default-value",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			handler := New("X-Header", "key", WithDefault(tt.defaultVal))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set("X-Header", tt.headerVal)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			val, ok := FromContext(capturedCtx, "key")
			if !ok {
				t.Fatal("expected value in context")
			}

			if val != tt.wantVal {
				t.Errorf("context value = %v, want %v", val, tt.wantVal)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	intValidator := func(val string) (any, error) {
		if val == "42" {
			return 42, nil
		}
		return nil, errors.New("must be 42")
	}

	tests := []struct {
		name       string
		headerVal  string
		validator  func(string) (any, error)
		wantStatus int
		wantBody   string
		wantCtxVal any
	}{
		{
			name:       "validation success",
			headerVal:  "42",
			validator:  intValidator,
			wantStatus: http.StatusOK,
			wantCtxVal: 42,
		},
		{
			name:       "validation failure",
			headerVal:  "invalid",
			validator:  intValidator,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Invalid X-Validated header: must be 42\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			handler := New("X-Validated", "validated_key", WithValidator(tt.validator))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			req.Header.Set("X-Validated", tt.headerVal)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}

			if tt.wantStatus == http.StatusOK {
				val, ok := FromContext(capturedCtx, "validated_key")
				if !ok {
					t.Fatal("expected value in context")
				}
				if val != tt.wantCtxVal {
					t.Errorf("context value = %v, want %v", val, tt.wantCtxVal)
				}
			}
		})
	}
}

type testContextKey string

func TestFromContext(t *testing.T) {
	tests := []struct {
		name      string
		setupCtx  func() context.Context
		key       string
		wantVal   any
		wantFound bool
	}{
		{
			name: "value exists",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), contextKey("test_key"), "test_value")
			},
			key:       "test_key",
			wantVal:   "test_value",
			wantFound: true,
		},
		{
			name: "value missing",
			setupCtx: func() context.Context {
				return context.Background()
			},
			key:       "missing_key",
			wantVal:   nil,
			wantFound: false,
		},
		{
			name: "wrong key type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), testContextKey("plain_string_key"), "value")
			},
			key:       "plain_string_key",
			wantVal:   nil,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			val, ok := FromContext(ctx, tt.key)

			if ok != tt.wantFound {
				t.Errorf("found = %v, want %v", ok, tt.wantFound)
			}

			if val != tt.wantVal {
				t.Errorf("value = %v, want %v", val, tt.wantVal)
			}
		})
	}
}

func TestMultipleOptions(t *testing.T) {
	validator := func(val string) (any, error) {
		if val == "valid" {
			return "transformed", nil
		}
		return nil, errors.New("invalid value")
	}

	tests := []struct {
		name       string
		opts       []Option
		headerVal  string
		wantStatus int
		wantCtxVal any
	}{
		{
			name:       "required with validator success",
			opts:       []Option{WithRequired(), WithValidator(validator)},
			headerVal:  "valid",
			wantStatus: http.StatusOK,
			wantCtxVal: "transformed",
		},
		{
			name:       "required with validator failure",
			opts:       []Option{WithRequired(), WithValidator(validator)},
			headerVal:  "invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "default with validator success",
			opts:       []Option{WithDefault("valid"), WithValidator(validator)},
			headerVal:  "",
			wantStatus: http.StatusOK,
			wantCtxVal: "transformed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			handler := New("X-Multi", "multi_key", tt.opts...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set("X-Multi", tt.headerVal)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				val, ok := FromContext(capturedCtx, "multi_key")
				if !ok {
					t.Fatal("expected value in context")
				}
				if val != tt.wantCtxVal {
					t.Errorf("context value = %v, want %v", val, tt.wantCtxVal)
				}
			}
		})
	}
}

func TestChainedMiddleware(t *testing.T) {
	var capturedCtx context.Context

	middleware1 := New("X-Header-1", "key1")
	middleware2 := New("X-Header-2", "key2")

	handler := middleware1(middleware2(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("X-Header-1", "value1")
	req.Header.Set("X-Header-2", "value2")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	val1, ok1 := FromContext(capturedCtx, "key1")
	if !ok1 || val1 != "value1" {
		t.Errorf("key1: got (%v, %v), want (value1, true)", val1, ok1)
	}

	val2, ok2 := FromContext(capturedCtx, "key2")
	if !ok2 || val2 != "value2" {
		t.Errorf("key2: got (%v, %v), want (value2, true)", val2, ok2)
	}
}

func TestHeaders_ValidatorPanic(t *testing.T) {
	panicValidator := func(_ string) (any, error) {
		panic("validator panic")
	}

	handler := New("X-Panic", "panic_key", WithValidator(panicValidator))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("X-Panic", "test-value")
	rr := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate, but it did not")
		} else if r != "validator panic" {
			t.Errorf("expected panic message 'validator panic', got %v", r)
		}
	}()

	handler.ServeHTTP(rr, req)
}
