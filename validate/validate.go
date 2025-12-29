// Package validate provides middleware for request validation.
package validate

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/nhalm/chikit/wrapper"
)

// MaxBodySizeConfig configures the MaxBodySize middleware.
type MaxBodySizeConfig struct {
	MaxBytes int64
}

// MaxBodySize returns middleware that limits request body size using http.MaxBytesReader.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// QueryParamConfig defines validation rules for a query parameter.
type QueryParamConfig struct {
	Name      string
	Required  bool
	Validator func(string) error
	Default   string
}

// QueryParams returns middleware that validates query parameters according to the given rules.
func QueryParams(rules ...QueryParamConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()

			for _, rule := range rules {
				value := query.Get(rule.Name)

				if value == "" {
					if rule.Required {
						wrapper.SetError(r, &wrapper.Error{
							Type:    "validation_error",
							Code:    "missing_parameter",
							Message: fmt.Sprintf("Missing required query parameter: %s", rule.Name),
							Param:   rule.Name,
							Status:  http.StatusBadRequest,
						})
						return
					}
					if rule.Default != "" {
						query.Set(rule.Name, rule.Default)
					}
					continue
				}

				if rule.Validator != nil {
					if err := rule.Validator(value); err != nil {
						wrapper.SetError(r, &wrapper.Error{
							Type:    "validation_error",
							Code:    "invalid_parameter",
							Message: fmt.Sprintf("Invalid query parameter %s: %v", rule.Name, err),
							Param:   rule.Name,
							Status:  http.StatusBadRequest,
						})
						return
					}
				}
			}

			r.URL.RawQuery = query.Encode()
			next.ServeHTTP(w, r)
		})
	}
}

// WithRequired marks a query parameter as required.
func WithRequired() func(*QueryParamConfig) {
	return func(r *QueryParamConfig) {
		r.Required = true
	}
}

// WithDefault sets a default value for a query parameter.
func WithDefault(val string) func(*QueryParamConfig) {
	return func(r *QueryParamConfig) {
		r.Default = val
	}
}

// WithValidator sets a validation function for a query parameter.
func WithValidator(fn func(string) error) func(*QueryParamConfig) {
	return func(r *QueryParamConfig) {
		r.Validator = fn
	}
}

// Param creates a query parameter rule with the given name and options.
func Param(name string, opts ...func(*QueryParamConfig)) QueryParamConfig {
	rule := QueryParamConfig{Name: name}
	for _, opt := range opts {
		opt(&rule)
	}
	return rule
}

// HeaderConfig defines validation rules for a header.
type HeaderConfig struct {
	Name          string
	Required      bool
	AllowedList   []string
	DeniedList    []string
	CaseSensitive bool
}

// Headers returns middleware that validates request headers according to the given rules.
func Headers(rules ...HeaderConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, rule := range rules {
				value := r.Header.Get(rule.Name)

				if value == "" {
					if rule.Required {
						wrapper.SetError(r, &wrapper.Error{
							Type:    "validation_error",
							Code:    "missing_header",
							Message: fmt.Sprintf("Missing required header: %s", rule.Name),
							Param:   rule.Name,
							Status:  http.StatusBadRequest,
						})
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
						wrapper.SetError(r, &wrapper.Error{
							Type:    "validation_error",
							Code:    "invalid_header",
							Message: fmt.Sprintf("Header %s value not in allowed list", rule.Name),
							Param:   rule.Name,
							Status:  http.StatusForbidden,
						})
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
							wrapper.SetError(r, &wrapper.Error{
								Type:    "validation_error",
								Code:    "denied_header",
								Message: fmt.Sprintf("Header %s value is denied", rule.Name),
								Param:   rule.Name,
								Status:  http.StatusForbidden,
							})
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
func Header(name string, opts ...func(*HeaderConfig)) HeaderConfig {
	rule := HeaderConfig{Name: name}
	for _, opt := range opts {
		opt(&rule)
	}
	return rule
}

// WithRequiredHeader marks a header as required.
func WithRequiredHeader() func(*HeaderConfig) {
	return func(r *HeaderConfig) {
		r.Required = true
	}
}

// WithAllowList sets the list of allowed values for a header.
func WithAllowList(values ...string) func(*HeaderConfig) {
	return func(r *HeaderConfig) {
		r.AllowedList = values
	}
}

// WithDenyList sets the list of denied values for a header.
func WithDenyList(values ...string) func(*HeaderConfig) {
	return func(r *HeaderConfig) {
		r.DeniedList = values
	}
}

// WithCaseSensitive makes header value comparisons case-sensitive.
func WithCaseSensitive() func(*HeaderConfig) {
	return func(r *HeaderConfig) {
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
func Pattern(pattern string) func(string) error {
	re := regexp.MustCompile(pattern)
	return func(val string) error {
		if !re.MatchString(val) {
			return fmt.Errorf("must match pattern: %s", pattern)
		}
		return nil
	}
}
