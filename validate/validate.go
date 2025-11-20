// Package validate provides middleware for request validation.
package validate

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MaxBodySizeConfig configures the MaxBodySize middleware.
type MaxBodySizeConfig struct {
	MaxBytes   int64
	StatusCode int
	Message    string
}

// MaxBodySize returns middleware that limits request body size.
// Returns 413 (Payload Too Large) when the limit is exceeded.
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
	Name      string
	Required  bool
	Validator func(string) error
	Default   string
}

// QueryParams returns middleware that validates query parameters.
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
func WithDefault(val string) func(*QueryParamRule) {
	return func(r *QueryParamRule) {
		r.Default = val
	}
}

// WithValidator sets a validation function for a query parameter.
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
	Name          string
	Required      bool
	AllowedList   []string
	DeniedList    []string
	CaseSensitive bool
}

// Headers returns middleware that validates request headers.
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
func WithAllowList(values ...string) func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.AllowedList = values
	}
}

// WithDenyList sets the list of denied values for a header.
func WithDenyList(values ...string) func(*HeaderRule) {
	return func(r *HeaderRule) {
		r.DeniedList = values
	}
}

// CaseSensitive makes header value comparisons case-sensitive (default: false).
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

// Pattern is a validator that checks if a value matches a pattern.
func Pattern(pattern string) func(string) error {
	return func(val string) error {
		matched, err := url.QueryUnescape(val)
		if err != nil {
			return fmt.Errorf("invalid value")
		}
		if !strings.Contains(matched, pattern) {
			return fmt.Errorf("must match pattern: %s", pattern)
		}
		return nil
	}
}
