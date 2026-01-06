// Validation middleware for request headers and body size.
//
// Header validation:
//
//	r.Use(chikit.ValidateHeaders(
//		chikit.ValidateWithHeader("Content-Type", chikit.ValidateRequired(), chikit.ValidateAllowList("application/json")),
//	))
//
// Body size limiting:
//
//	r.Use(chikit.MaxBodySize(10 * 1024 * 1024)) // 10MB limit

package chikit

import (
	"fmt"
	"net/http"
	"strings"
)

// validateBodySizeConfig holds configuration for MaxBodySize middleware.
type validateBodySizeConfig struct {
	maxBytes int64
}

// BodySizeOption configures MaxBodySize middleware.
type BodySizeOption func(*validateBodySizeConfig)

// MaxBodySize returns middleware that limits request body size.
//
// The middleware provides two-stage protection:
//  1. Content-Length check: Requests with Content-Length exceeding the limit are
//     rejected with 413 immediately, before the handler runs
//  2. MaxBytesReader wrapper: All request bodies are wrapped with http.MaxBytesReader
//     as defense-in-depth, catching chunked transfers and missing/incorrect Content-Length
//
// When used with bind.JSON, the second stage is automatic:
//
//	r.Use(chikit.MaxBodySize(1024 * 1024))
//	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
//	    var req CreateUserRequest
//	    if !bind.JSON(r, &req) {
//	        return // Returns 413 if body exceeds limit during decode
//	    }
//	})
//
// Returns 413 (Request Entity Too Large) when the limit is exceeded.
//
// Basic usage:
//
//	r.Use(chikit.MaxBodySize(10 * 1024 * 1024)) // 10MB limit
func MaxBodySize(maxBytes int64, opts ...BodySizeOption) func(http.Handler) http.Handler {
	cfg := &validateBodySizeConfig{
		maxBytes: maxBytes,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > cfg.maxBytes {
				if HasState(r.Context()) {
					SetError(r, ErrPayloadTooLarge.With("Request body too large"))
				} else {
					http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				}
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateHeaderConfig defines validation rules for a header.
type ValidateHeaderConfig struct {
	Name          string
	Required      bool
	AllowedList   []string
	DeniedList    []string
	CaseSensitive bool
}

// validateHeadersConfig holds the configuration for ValidateHeaders middleware.
type validateHeadersConfig struct {
	rules []ValidateHeaderConfig
}

// ValidateHeadersOption configures ValidateHeaders middleware.
type ValidateHeadersOption func(*validateHeadersConfig)

// ValidateWithHeader adds a header validation rule with the given name and options.
func ValidateWithHeader(name string, opts ...ValidateHeaderOption) ValidateHeadersOption {
	return func(cfg *validateHeadersConfig) {
		rule := ValidateHeaderConfig{Name: name}
		for _, opt := range opts {
			opt(&rule)
		}
		cfg.rules = append(cfg.rules, rule)
	}
}

// ValidateHeaders returns middleware that validates request headers according to the given rules.
// For each rule, checks if the header is present (when required), validates against
// allow/deny lists, and enforces case sensitivity settings. Returns 400 (Bad Request)
// for all validation failures.
//
// Example:
//
//	r.Use(chikit.ValidateHeaders(
//		chikit.ValidateWithHeader("Content-Type",
//			chikit.ValidateRequired(),
//			chikit.ValidateAllowList("application/json", "application/xml")),
//		chikit.ValidateWithHeader("X-Custom-Header",
//			chikit.ValidateDenyList("forbidden-value")),
//	))
func ValidateHeaders(opts ...ValidateHeadersOption) func(http.Handler) http.Handler {
	cfg := &validateHeadersConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			useWrapper := HasState(r.Context())

			for i := range cfg.rules {
				if err := validateHeaderRule(r, &cfg.rules[i]); err != nil {
					if useWrapper {
						SetError(r, err)
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

func validateHeaderRule(r *http.Request, rule *ValidateHeaderConfig) *APIError {
	value := r.Header.Get(rule.Name)

	if value == "" {
		if rule.Required {
			return &APIError{
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

	if err := validateCheckAllowList(rule, checkValue); err != nil {
		return err
	}

	return validateCheckDenyList(rule, checkValue)
}

func validateCheckAllowList(rule *ValidateHeaderConfig, checkValue string) *APIError {
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

	return &APIError{
		Type:    "validation_error",
		Code:    "invalid_header",
		Message: fmt.Sprintf("Header %s value not in allowed list", rule.Name),
		Param:   rule.Name,
		Status:  http.StatusBadRequest,
	}
}

func validateCheckDenyList(rule *ValidateHeaderConfig, checkValue string) *APIError {
	if len(rule.DeniedList) == 0 {
		return nil
	}

	for _, d := range rule.DeniedList {
		compareVal := d
		if !rule.CaseSensitive {
			compareVal = strings.ToLower(d)
		}
		if checkValue == compareVal {
			return &APIError{
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

// ValidateHeaderOption configures a header validation rule.
type ValidateHeaderOption func(*ValidateHeaderConfig)

// ValidateRequired marks a header as required.
func ValidateRequired() ValidateHeaderOption {
	return func(r *ValidateHeaderConfig) {
		r.Required = true
	}
}

// ValidateAllowList sets the list of allowed values for a header.
// If set, only values in this list are permitted. Returns 400 if the value is not in the list.
func ValidateAllowList(values ...string) ValidateHeaderOption {
	return func(r *ValidateHeaderConfig) {
		r.AllowedList = values
	}
}

// ValidateDenyList sets the list of denied values for a header.
// If set, values in this list are explicitly forbidden. Returns 400 if the value is in the list.
func ValidateDenyList(values ...string) ValidateHeaderOption {
	return func(r *ValidateHeaderConfig) {
		r.DeniedList = values
	}
}

// ValidateCaseSensitive makes header value comparisons case-sensitive.
// By default, comparisons are case-insensitive.
func ValidateCaseSensitive() ValidateHeaderOption {
	return func(r *ValidateHeaderConfig) {
		r.CaseSensitive = true
	}
}
