// Package validate provides middleware for request validation.
//
// The package offers validation for headers and request body size.
// All validation middleware returns 400 (Bad Request) for validation failures.
// MaxBodySize wraps the request body with http.MaxBytesReader; downstream handlers
// must check for errors when reading the body to detect size limit violations.
//
// Header validation:
//
//	r.Use(validate.NewHeaders(
//		validate.WithHeader("Content-Type", validate.WithRequired(), validate.WithAllowList("application/json")),
//	))
//
// Body size limiting:
//
//	r.Use(validate.MaxBodySize(10 * 1024 * 1024)) // 10MB limit
package validate

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nhalm/chikit/wrapper"
)

// bodySizeConfig holds configuration for MaxBodySize middleware.
type bodySizeConfig struct {
	maxBytes int64
}

// BodySizeOption configures MaxBodySize middleware.
type BodySizeOption func(*bodySizeConfig)

// MaxBodySize returns middleware that limits request body size using http.MaxBytesReader.
//
// IMPORTANT: This middleware ONLY wraps the request body - it does NOT automatically
// detect or return error responses. The body size limit is enforced only when the body
// is read by your handler. Handlers MUST check for *http.MaxBytesError when reading
// the body to properly handle size violations.
//
// How it works:
//  1. This middleware wraps r.Body with http.MaxBytesReader before calling the next handler
//  2. The wrapped body reader tracks bytes read and returns an error if the limit is exceeded
//  3. Your handler must read the body (e.g., via io.ReadAll or json.Decoder) to trigger the check
//  4. Your handler must check for *http.MaxBytesError to detect size violations
//
// Basic usage:
//
//	r.Use(validate.MaxBodySize(10 * 1024 * 1024)) // 10MB limit
//
// Handler example with proper error handling:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		body, err := io.ReadAll(r.Body)
//		if err != nil {
//			var maxBytesErr *http.MaxBytesError
//			if errors.As(err, &maxBytesErr) {
//				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
//				return
//			}
//			http.Error(w, "Failed to read body", http.StatusInternalServerError)
//			return
//		}
//		// Process body...
//	}
//
// With wrapper middleware (recommended):
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		body, err := io.ReadAll(r.Body)
//		if err != nil {
//			var maxBytesErr *http.MaxBytesError
//			if errors.As(err, &maxBytesErr) {
//				wrapper.SetError(r, wrapper.ErrUnprocessableEntity.With("Request body exceeds maximum size"))
//				return
//			}
//			wrapper.SetError(r, wrapper.ErrInternal.With("Failed to read request body"))
//			return
//		}
//		// Process body...
//	}
//
// With JSON decoding:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		var req MyRequest
//		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
//			var maxBytesErr *http.MaxBytesError
//			if errors.As(err, &maxBytesErr) {
//				wrapper.SetError(r, wrapper.ErrUnprocessableEntity.With("Request body too large"))
//				return
//			}
//			wrapper.SetError(r, wrapper.ErrBadRequest.With("Invalid JSON"))
//			return
//		}
//		// Process req...
//	}
func MaxBodySize(maxBytes int64, opts ...BodySizeOption) func(http.Handler) http.Handler {
	cfg := &bodySizeConfig{
		maxBytes: maxBytes,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// HeaderConfig defines validation rules for a header.
type HeaderConfig struct {
	// Name is the HTTP header name to validate
	Name string

	// Required indicates whether the header must be present
	Required bool

	// AllowedList is a list of allowed values (empty means any value is allowed)
	AllowedList []string

	// DeniedList is a list of denied values
	DeniedList []string

	// CaseSensitive determines whether value comparisons are case-sensitive (default: false)
	CaseSensitive bool
}

// headersConfig holds the configuration for NewHeaders middleware.
type headersConfig struct {
	rules []HeaderConfig
}

// HeadersOption configures NewHeaders middleware.
type HeadersOption func(*headersConfig)

// WithHeader adds a header validation rule with the given name and options.
func WithHeader(name string, opts ...HeaderOption) HeadersOption {
	return func(cfg *headersConfig) {
		rule := HeaderConfig{Name: name}
		for _, opt := range opts {
			opt(&rule)
		}
		cfg.rules = append(cfg.rules, rule)
	}
}

// NewHeaders returns middleware that validates request headers according to the given rules.
// For each rule, checks if the header is present (when required), validates against
// allow/deny lists, and enforces case sensitivity settings. Returns 400 (Bad Request)
// for all validation failures.
//
// Example:
//
//	r.Use(validate.NewHeaders(
//		validate.WithHeader("Content-Type",
//			validate.WithRequired(),
//			validate.WithAllowList("application/json", "application/xml")),
//		validate.WithHeader("X-Custom-Header",
//			validate.WithDenyList("forbidden-value")),
//	))
func NewHeaders(opts ...HeadersOption) func(http.Handler) http.Handler {
	cfg := &headersConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			useWrapper := wrapper.HasState(r.Context())

			for i := range cfg.rules {
				if err := validateHeader(r, &cfg.rules[i]); err != nil {
					if useWrapper {
						wrapper.SetError(r, err)
					} else {
						http.Error(w, err.Message, err.Status)
					}
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func validateHeader(r *http.Request, rule *HeaderConfig) *wrapper.Error {
	value := r.Header.Get(rule.Name)

	if value == "" {
		if rule.Required {
			return &wrapper.Error{
				Type:    "validation_error",
				Code:    "missing_header",
				Message: fmt.Sprintf("Missing required header: %s", rule.Name),
				Param:   rule.Name,
				Status:  http.StatusBadRequest,
			}
		}
		return nil
	}

	checkValue := value
	if !rule.CaseSensitive {
		checkValue = strings.ToLower(value)
	}

	if err := checkAllowList(rule, checkValue); err != nil {
		return err
	}

	return checkDenyList(rule, checkValue)
}

func checkAllowList(rule *HeaderConfig, checkValue string) *wrapper.Error {
	if len(rule.AllowedList) == 0 {
		return nil
	}

	for _, a := range rule.AllowedList {
		compareVal := a
		if !rule.CaseSensitive {
			compareVal = strings.ToLower(a)
		}
		if checkValue == compareVal {
			return nil
		}
	}

	return &wrapper.Error{
		Type:    "validation_error",
		Code:    "invalid_header",
		Message: fmt.Sprintf("Header %s value not in allowed list", rule.Name),
		Param:   rule.Name,
		Status:  http.StatusBadRequest,
	}
}

func checkDenyList(rule *HeaderConfig, checkValue string) *wrapper.Error {
	if len(rule.DeniedList) == 0 {
		return nil
	}

	for _, d := range rule.DeniedList {
		compareVal := d
		if !rule.CaseSensitive {
			compareVal = strings.ToLower(d)
		}
		if checkValue == compareVal {
			return &wrapper.Error{
				Type:    "validation_error",
				Code:    "invalid_header",
				Message: fmt.Sprintf("Header %s value is denied", rule.Name),
				Param:   rule.Name,
				Status:  http.StatusBadRequest,
			}
		}
	}

	return nil
}

// HeaderOption configures a header validation rule.
type HeaderOption func(*HeaderConfig)

// WithRequired marks a header as required.
func WithRequired() HeaderOption {
	return func(r *HeaderConfig) {
		r.Required = true
	}
}

// WithAllowList sets the list of allowed values for a header.
// If set, only values in this list are permitted. Returns 400 if the value is not in the list.
func WithAllowList(values ...string) HeaderOption {
	return func(r *HeaderConfig) {
		r.AllowedList = values
	}
}

// WithDenyList sets the list of denied values for a header.
// If set, values in this list are explicitly forbidden. Returns 400 if the value is in the list.
func WithDenyList(values ...string) HeaderOption {
	return func(r *HeaderConfig) {
		r.DeniedList = values
	}
}

// WithCaseSensitive makes header value comparisons case-sensitive.
// By default, comparisons are case-insensitive.
func WithCaseSensitive() HeaderOption {
	return func(r *HeaderConfig) {
		r.CaseSensitive = true
	}
}
