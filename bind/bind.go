// Package bind provides request binding and validation for Chi middleware.
//
// The package offers JSON body and query parameter binding with struct tag validation
// using go-playground/validator/v10. It follows chikit's middleware pattern with
// context-based configuration.
//
// Basic usage:
//
//	r := chi.NewRouter()
//	r.Use(wrapper.New())
//	r.Use(bind.New())
//
//	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
//	    var req CreateUserRequest
//	    if !bind.JSON(r, &req) {
//	        return
//	    }
//	    wrapper.SetResponse(r, http.StatusCreated, user)
//	})
//
// With custom message formatter:
//
//	r.Use(bind.New(bind.WithFormatter(func(field, tag, param string) string {
//	    return myTranslator.T("validation."+tag, field, param)
//	})))
package bind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/nhalm/chikit/wrapper"
)

type contextKey string

const configKey contextKey = "bind_config"

var (
	validate      *validator.Validate
	validateMu    sync.RWMutex
	defaultConfig = &config{formatter: defaultFormatter}
)

func init() {
	validate = validator.New(validator.WithRequiredStructEnabled())

	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		if name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]; name != "" && name != "-" {
			return name
		}
		if name := strings.SplitN(fld.Tag.Get("query"), ",", 2)[0]; name != "" && name != "-" {
			return name
		}
		return fld.Name
	})
}

// MessageFormatter generates human-readable message from validation error.
// Parameters: field name, validation tag, tag parameter (e.g., "10" from "min=10")
type MessageFormatter func(field, tag, param string) string

type config struct {
	formatter MessageFormatter
}

// Option configures the bind middleware.
type Option func(*config)

// WithFormatter sets a custom message formatter for validation errors.
func WithFormatter(fn MessageFormatter) Option {
	return func(c *config) {
		c.formatter = fn
	}
}

// New returns middleware with optional configuration.
func New(opts ...Option) func(http.Handler) http.Handler {
	cfg := &config{formatter: defaultFormatter}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), configKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func getConfig(ctx context.Context) *config {
	if cfg, ok := ctx.Value(configKey).(*config); ok {
		return cfg
	}
	return defaultConfig
}

func defaultFormatter(_, tag, param string) string {
	switch tag {
	case "required":
		return "required"
	case "email":
		return "must be a valid email"
	case "min":
		return "must be at least " + param
	case "max":
		return "must be at most " + param
	case "oneof":
		return "must be one of: " + param
	case "uuid":
		return "must be a valid UUID"
	case "url":
		return "must be a valid URL"
	default:
		if param != "" {
			return tag + "=" + param
		}
		return tag
	}
}

// JSON decodes request body into dest and validates it.
// Returns true if binding and validation succeeded, false otherwise.
// When validation fails, an error is set in the wrapper context (if available).
//
// Body size limits: If validate.MaxBodySize middleware is active, requests exceeding
// the limit during decode return ErrPayloadTooLarge (413). This handles chunked
// transfers and requests with missing/incorrect Content-Length headers.
func JSON(r *http.Request, dest any) bool {
	ctx := r.Context()

	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		if wrapper.HasState(ctx) {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				wrapper.SetError(r, wrapper.ErrPayloadTooLarge.With("Request body too large"))
			} else {
				wrapper.SetError(r, wrapper.ErrBadRequest.With("Invalid JSON request body"))
			}
		}
		return false
	}

	validateMu.RLock()
	err := validate.Struct(dest)
	validateMu.RUnlock()

	if err != nil {
		if wrapper.HasState(ctx) {
			cfg := getConfig(ctx)
			wrapper.SetError(r, wrapper.NewValidationError(translateErrors(err, cfg.formatter)))
		}
		return false
	}

	return true
}

// Query decodes query parameters into dest and validates it.
// Returns true if binding and validation succeeded, false otherwise.
// When validation fails, an error is set in the wrapper context (if available).
func Query(r *http.Request, dest any) bool {
	ctx := r.Context()

	if err := decodeQuery(r, dest); err != nil {
		if wrapper.HasState(ctx) {
			wrapper.SetError(r, wrapper.ErrBadRequest.With("Invalid query parameters"))
		}
		return false
	}

	validateMu.RLock()
	err := validate.Struct(dest)
	validateMu.RUnlock()

	if err != nil {
		if wrapper.HasState(ctx) {
			cfg := getConfig(ctx)
			wrapper.SetError(r, wrapper.NewValidationError(translateErrors(err, cfg.formatter)))
		}
		return false
	}

	return true
}

// RegisterValidation registers a custom validation function.
// Must be called at startup before handling requests.
func RegisterValidation(tag string, fn validator.Func) error {
	validateMu.Lock()
	defer validateMu.Unlock()
	return validate.RegisterValidation(tag, fn)
}

func translateErrors(err error, formatter MessageFormatter) []wrapper.FieldError {
	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		return []wrapper.FieldError{{
			Param:   "",
			Code:    "validation",
			Message: err.Error(),
		}}
	}
	result := make([]wrapper.FieldError, len(errs))
	for i, e := range errs {
		result[i] = wrapper.FieldError{
			Param:   e.Field(),
			Code:    e.Tag(),
			Message: formatter(e.Field(), e.Tag(), e.Param()),
		}
	}
	return result
}

func decodeQuery(r *http.Request, dest any) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("dest must be non-nil pointer to struct")
	}
	v := rv.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("dest must be pointer to struct, got pointer to %s", v.Kind())
	}
	t := v.Type()

	query := r.URL.Query()

	for i := range t.NumField() {
		structField := t.Field(i)
		tag := structField.Tag.Get("query")
		if tag == "" || tag == "-" {
			continue
		}

		fieldVal := v.Field(i)
		if !fieldVal.CanSet() {
			continue
		}

		name := strings.SplitN(tag, ",", 2)[0]
		value := query.Get(name)
		if value == "" {
			continue
		}

		if err := setField(fieldVal, value); err != nil {
			return fmt.Errorf("invalid value for %s: %w", name, err)
		}
	}

	return nil
}

func setField(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bitSize := field.Type().Bits()
		n, err := strconv.ParseInt(value, 10, bitSize)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bitSize := field.Type().Bits()
		n, err := strconv.ParseUint(value, 10, bitSize)
		if err != nil {
			return err
		}
		field.SetUint(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Float32, reflect.Float64:
		bitSize := field.Type().Bits()
		f, err := strconv.ParseFloat(value, bitSize)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	default:
		return fmt.Errorf("unsupported type: %s", field.Kind())
	}
	return nil
}
