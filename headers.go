package chikit

// Header extraction middleware for extracting and validating HTTP headers.
// Provides flexible header extraction with support for required headers,
// default values, and custom validation/transformation functions.

import (
	"context"
	"net/http"
)

type headerContextKey string

// HeaderExtractor extracts a header value and stores it in the request context.
// Supports optional headers, default values, and custom validation/transformation.
type HeaderExtractor struct {
	header     string
	ctxKey     headerContextKey
	required   bool
	defaultVal string
	validator  func(string) (any, error)
}

// HeaderExtractorOption configures a HeaderExtractor middleware.
type HeaderExtractorOption func(*HeaderExtractor)

// ExtractRequired marks the header as required.
// Returns 400 (Bad Request) if the header is missing and no default is provided.
func ExtractRequired() HeaderExtractorOption {
	return func(h *HeaderExtractor) {
		h.required = true
	}
}

// ExtractDefault provides a default value if the header is missing.
// The default takes precedence over the Required setting - if a default is provided,
// the header becomes effectively optional.
func ExtractDefault(val string) HeaderExtractorOption {
	return func(h *HeaderExtractor) {
		h.defaultVal = val
	}
}

// ExtractWithValidator provides a custom validator that can transform the header value.
// The validator receives the header value (string) and returns:
//   - The transformed value (any type) to store in context
//   - An error if validation fails (returns 400 to client)
//
// Use this to parse and validate headers like UUIDs, timestamps, enums, etc.
//
// Example:
//
//	validator := func(val string) (any, error) {
//		if val == "admin" || val == "user" {
//			return val, nil
//		}
//		return nil, fmt.Errorf("must be 'admin' or 'user'")
//	}
func ExtractWithValidator(fn func(string) (any, error)) HeaderExtractorOption {
	return func(h *HeaderExtractor) {
		h.validator = fn
	}
}

// ExtractHeader creates middleware that extracts a header and stores it in context.
// The header value (or transformed value from validator) is stored in the request
// context under the specified ctxKey and can be retrieved using HeaderFromContext.
//
// Parameters:
//   - header: The HTTP header name to extract (e.g., "X-API-Key")
//   - ctxKey: The context key to store the value under (e.g., "api_key")
//   - opts: Optional configuration (ExtractRequired, ExtractDefault, ExtractWithValidator)
//
// Returns 400 (Bad Request) if:
//   - A required header is missing and no default is provided
//   - Validation fails (validator returns an error)
//
// Example:
//
//	// Simple extraction
//	r.Use(chikit.ExtractHeader("X-Request-ID", "request_id"))
//
//	// Required with validation
//	r.Use(chikit.ExtractHeader("X-Tenant-ID", "tenant_id",
//		chikit.ExtractRequired(),
//		chikit.ExtractWithValidator(validateUUID)))
//
//	// Optional with default
//	r.Use(chikit.ExtractHeader("X-Client-Version", "version",
//		chikit.ExtractDefault("1.0.0")))
func ExtractHeader(header, ctxKey string, opts ...HeaderExtractorOption) func(http.Handler) http.Handler {
	h := &HeaderExtractor{
		header: header,
		ctxKey: headerContextKey(ctxKey),
	}

	for _, opt := range opts {
		opt(h)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val := r.Header.Get(h.header)

			if val == "" {
				switch {
				case h.defaultVal != "":
					val = h.defaultVal
				case h.required:
					if HasState(r.Context()) {
						SetError(r, ErrBadRequest.With("Missing required header: "+h.header))
					} else {
						http.Error(w, "Missing required header: "+h.header, http.StatusBadRequest)
					}
					return
				default:
					next.ServeHTTP(w, r)
					return
				}
			}

			var contextVal any = val
			if h.validator != nil {
				var err error
				contextVal, err = h.validator(val)
				if err != nil {
					if HasState(r.Context()) {
						SetError(r, ErrBadRequest.With("Invalid "+h.header+" header: "+err.Error()))
					} else {
						http.Error(w, "Invalid "+h.header+" header: "+err.Error(), http.StatusBadRequest)
					}
					return
				}
			}

			ctx := context.WithValue(r.Context(), h.ctxKey, contextVal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HeaderFromContext retrieves a value from the request context by key.
// Returns the value and true if present, or nil and false if not present.
// The returned value has type 'any' and should be type-asserted to the expected type.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if val, ok := chikit.HeaderFromContext(r.Context(), "api_key"); ok {
//			apiKey := val.(string)
//			log.Printf("API Key: %s", apiKey)
//		}
//
//		// With type assertion for transformed values
//		if val, ok := chikit.HeaderFromContext(r.Context(), "tenant_id"); ok {
//			tenantID := val.(uuid.UUID)
//			// Use typed value
//		}
//	}
func HeaderFromContext(ctx context.Context, key string) (any, bool) {
	val := ctx.Value(headerContextKey(key))
	if val == nil {
		return nil, false
	}
	return val, true
}
