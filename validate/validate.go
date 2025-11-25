// Package validate provides middleware for request validation.
//
// The package offers validation for query parameters, headers, and request body size.
// All validation middleware returns 400 (Bad Request) for validation failures.
// MaxBodySize wraps the request body with http.MaxBytesReader; downstream handlers
// must check for errors when reading the body to detect size limit violations.
//
// Query parameter validation:
//
//	r.Use(validate.QueryParams(
//		validate.Param("page", validate.Required(), validate.WithValidator(validate.Pattern(`^\d+$`))),
//		validate.Param("limit", validate.WithDefault("10")),
//	))
//
// Header validation:
//
//	r.Use(validate.Headers(
//		validate.Header("Content-Type", validate.RequiredHeader(), validate.WithAllowList("application/json")),
//	))
//
// Body size limiting:
//
//	r.Use(validate.MaxBodySize(10 * 1024 * 1024)) // 10MB limit
package validate

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// MaxBodySizeConfig configures the MaxBodySize middleware.
type MaxBodySizeConfig struct {
	// MaxBytes is the maximum allowed request body size in bytes
	MaxBytes int64

	// StatusCode is the HTTP status code to return when limit is exceeded (default: 413)
	StatusCode int

	// Message is the error message to return when limit is exceeded
	Message string
}

// MaxBodySize returns middleware that limits request body size using http.MaxBytesReader.
// This middleware wraps the request body to prevent reading beyond the specified limit.
//
// IMPORTANT: This middleware only wraps the body - it does NOT automatically send error responses.
// Downstream handlers must handle errors when reading the body. When the limit is exceeded,
// the body reader will return an error of type *http.MaxBytesError. Your handler should check
// for this error type and respond appropriately.
//
// The StatusCode and Message fields in MaxBodySizeConfig are provided for convenience but are
// not used by this middleware itself. Downstream handlers should use these values when crafting
// their error responses.
//
// Example:
//
//	r.Use(validate.MaxBodySize(10 * 1024 * 1024)) // 10MB limit
//
// With custom status code and message:
//
//	r.Use(validate.MaxBodySize(1024,
//		validate.WithBodySizeStatus(http.StatusBadRequest),
//		validate.WithBodySizeMessage("Request too large")))
//
// Example handler that checks for body size errors:
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
//		// ... process body
//	}
func MaxBodySize(maxBytes int64, opts ...MaxBodySizeOption) func(http.Handler) http.Handler {
	config := MaxBodySizeConfig{
		MaxBytes:   maxBytes,
		StatusCode: http.StatusRequestEntityTooLarge,
		Message:    "Request body too large",
	}

	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, config.MaxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodySizeOption configures MaxBodySize middleware.
type MaxBodySizeOption func(*MaxBodySizeConfig)

// WithBodySizeStatus sets the HTTP status code returned when body size is exceeded.
func WithBodySizeStatus(code int) MaxBodySizeOption {
	return func(c *MaxBodySizeConfig) {
		c.StatusCode = code
	}
}

// WithBodySizeMessage sets the error message returned when body size is exceeded.
func WithBodySizeMessage(msg string) MaxBodySizeOption {
	return func(c *MaxBodySizeConfig) {
		c.Message = msg
	}
}

// QueryParamRule defines validation rules for a query parameter.
type QueryParamRule struct {
	// Name is the query parameter name to validate
	Name string

	// Required indicates whether the parameter must be present
	Required bool

	// Validator is an optional custom validation function
	Validator func(string) error

	// Default is the value to use if the parameter is missing (only used when Required is false)
	Default string
}

// QueryParams returns middleware that validates query parameters according to the given rules.
// For each rule, checks if the parameter is present (when required), applies the validator
// (if provided), and sets default values (when specified). Returns 400 (Bad Request) if
// validation fails.
//
// IMPORTANT: This middleware modifies the request URL by setting default values for missing
// parameters. The modified URL is available to downstream handlers via r.URL.Query().
//
// Note: When Required is true, the Default value has no effect since the parameter
// must be present.
//
// Example:
//
//	r.Use(validate.QueryParams(
//		validate.Param("page", validate.Required(), validate.WithValidator(validate.Pattern(`^\d+$`))),
//		validate.Param("limit", validate.WithDefault("10"), validate.WithValidator(validate.MinLength(1))),
//		validate.Param("sort", validate.WithValidator(validate.OneOf("asc", "desc"))),
//	))
func QueryParams(rules ...QueryParamRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()

			for _, rule := range rules {
				value := query.Get(rule.Name)

				if value == "" {
					if rule.Required {
						http.Error(w, fmt.Sprintf("Missing required query parameter: %s", rule.Name), http.StatusBadRequest)
						return
					}
					if rule.Default != "" {
						query.Set(rule.Name, rule.Default)
					}
					continue
				}

				if rule.Validator != nil {
					if err := rule.Validator(value); err != nil {
						http.Error(w, fmt.Sprintf("Invalid query parameter %s: %v", rule.Name, err), http.StatusBadRequest)
						return
					}
				}
			}

			r.URL.RawQuery = query.Encode()
			next.ServeHTTP(w, r)
		})
	}
}

// Required marks a query parameter as required.
func Required() func(*QueryParamRule) {
	return func(r *QueryParamRule) {
		r.Required = true
	}
}

// WithDefault sets a default value for a query parameter.
// The default is only applied when the parameter is missing and Required is false.
func WithDefault(val string) func(*QueryParamRule) {
	return func(r *QueryParamRule) {
		r.Default = val
	}
}

// WithValidator sets a validation function for a query parameter.
// The validator receives the parameter value and should return an error if invalid.
func WithValidator(fn func(string) error) func(*QueryParamRule) {
	return func(r *QueryParamRule) {
		r.Validator = fn
	}
}

// Param creates a query parameter rule with the given name and options.
func Param(name string, opts ...func(*QueryParamRule)) QueryParamRule {
	rule := QueryParamRule{Name: name}
	for _, opt := range opts {
		opt(&rule)
	}
	return rule
}

// HeaderRule defines validation rules for a header.
type HeaderRule struct {
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

// Headers returns middleware that validates request headers according to the given rules.
// For each rule, checks if the header is present (when required), validates against
// allow/deny lists, and enforces case sensitivity settings. Returns 400 (Bad Request)
// if a required header is missing, or 403 (Forbidden) if a value is not allowed or is denied.
//
// Example:
//
//	r.Use(validate.Headers(
//		validate.Header("Content-Type",
//			validate.RequiredHeader(),
//			validate.WithAllowList("application/json", "application/xml")),
//		validate.Header("X-Custom-Header",
//			validate.WithDenyList("forbidden-value")),
//	))
func Headers(rules ...HeaderRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, rule := range rules {
				value := r.Header.Get(rule.Name)

				if value == "" {
					if rule.Required {
						http.Error(w, fmt.Sprintf("Missing required header: %s", rule.Name), http.StatusBadRequest)
						return
					}
					continue
				}

				checkValue := value
				if !rule.CaseSensitive {
					checkValue = strings.ToLower(value)
				}

				if len(rule.AllowedList) > 0 {
					allowed := false
					for _, a := range rule.AllowedList {
						compareVal := a
						if !rule.CaseSensitive {
							compareVal = strings.ToLower(a)
						}
						if checkValue == compareVal {
							allowed = true
							break
						}
					}
					if !allowed {
						http.Error(w, fmt.Sprintf("Header %s value not in allowed list", rule.Name), http.StatusForbidden)
						return
					}
				}

				if len(rule.DeniedList) > 0 {
					for _, d := range rule.DeniedList {
						compareVal := d
						if !rule.CaseSensitive {
							compareVal = strings.ToLower(d)
						}
						if checkValue == compareVal {
							http.Error(w, fmt.Sprintf("Header %s value is denied", rule.Name), http.StatusForbidden)
							return
						}
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Header creates a header validation rule with the given name and options.
func Header(name string, opts ...func(*HeaderRule)) HeaderRule {
	rule := HeaderRule{Name: name}
	for _, opt := range opts {
		opt(&rule)
	}
	return rule
}

// RequiredHeader marks a header as required.
func RequiredHeader() func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.Required = true
	}
}

// WithAllowList sets the list of allowed values for a header.
// If set, only values in this list are permitted. Returns 403 if the value is not in the list.
func WithAllowList(values ...string) func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.AllowedList = values
	}
}

// WithDenyList sets the list of denied values for a header.
// If set, values in this list are explicitly forbidden. Returns 403 if the value is in the list.
func WithDenyList(values ...string) func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.DeniedList = values
	}
}

// CaseSensitive makes header value comparisons case-sensitive.
// By default, comparisons are case-insensitive.
func CaseSensitive() func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.CaseSensitive = true
	}
}

// OneOf is a validator that checks if a value is in a list of allowed values.
func OneOf(values ...string) func(string) error {
	return func(val string) error {
		for _, v := range values {
			if val == v {
				return nil
			}
		}
		return fmt.Errorf("must be one of: %s", strings.Join(values, ", "))
	}
}

// MinLength is a validator that checks if a value has minimum length.
func MinLength(minLen int) func(string) error {
	return func(val string) error {
		if len(val) < minLen {
			return fmt.Errorf("must be at least %d characters", minLen)
		}
		return nil
	}
}

// MaxLength is a validator that checks if a value has maximum length.
func MaxLength(maxLen int) func(string) error {
	return func(val string) error {
		if len(val) > maxLen {
			return fmt.Errorf("must be at most %d characters", maxLen)
		}
		return nil
	}
}

// Pattern is a validator that checks if a value matches a regex pattern.
// Note: Query parameter values from r.URL.Query() are already URL-decoded by Go's
// query parser, so this validator works directly with the decoded values.
func Pattern(pattern string) func(string) error {
	re := regexp.MustCompile(pattern)
	return func(val string) error {
		if !re.MatchString(val) {
			return fmt.Errorf("must match pattern: %s", pattern)
		}
		return nil
	}
}
