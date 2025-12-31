// Package headers provides middleware for extracting and validating HTTP headers.
//
// This package offers flexible header extraction with support for required headers,
// default values, and custom validation/transformation functions. Extracted headers
// are stored in the request context for downstream handlers to access.
//
// Basic extraction:
//
//	middleware := headers.New("X-API-Key", "api_key", headers.WithRequired())
//	r.Use(middleware)
//
// With transformation and validation:
//
//	validator := func(val string) (any, error) {
//		id, err := uuid.Parse(val)
//		if err != nil {
//			return nil, fmt.Errorf("invalid UUID format")
//		}
//		return id, nil
//	}
//	middleware := headers.New("X-Tenant-ID", "tenant_id",
//		headers.WithRequired(),
//		headers.WithValidator(validator))
//
// Retrieve from context:
//
//	if tenantID, ok := headers.FromContext(ctx, "tenant_id"); ok {
//		uuid := tenantID.(uuid.UUID) // Type assertion to your transformed type
//	}
package headers

import (
	"context"
	"net/http"

	"github.com/nhalm/chikit/wrapper"
)

type contextKey string

// HeaderToContext extracts a header value and stores it in the request context.
// Supports optional headers, default values, and custom validation/transformation.
type HeaderToContext struct {
	header     string
	ctxKey     contextKey
	required   bool
	defaultVal string
	validator  func(string) (any, error)
}

// Option configures a HeaderToContext middleware.
type Option func(*HeaderToContext)

// WithRequired marks the header as required.
// Returns 400 (Bad Request) if the header is missing and no default is provided.
func WithRequired() Option {
	return func(h *HeaderToContext) {
		h.required = true
	}
}

// WithDefault provides a default value if the header is missing.
// The default takes precedence over the Required setting - if a default is provided,
// the header becomes effectively optional.
func WithDefault(val string) Option {
	return func(h *HeaderToContext) {
		h.defaultVal = val
	}
}

// WithValidator provides a custom validator that can transform the header value.
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
func WithValidator(fn func(string) (any, error)) Option {
	return func(h *HeaderToContext) {
		h.validator = fn
	}
}

// New creates middleware that extracts a header and stores it in context.
// The header value (or transformed value from validator) is stored in the request
// context under the specified ctxKey and can be retrieved using FromContext.
//
// Parameters:
//   - header: The HTTP header name to extract (e.g., "X-API-Key")
//   - ctxKey: The context key to store the value under (e.g., "api_key")
//   - opts: Optional configuration (WithRequired, WithDefault, WithValidator)
//
// Returns 400 (Bad Request) if:
//   - A required header is missing and no default is provided
//   - Validation fails (validator returns an error)
//
// Example:
//
//	// Simple extraction
//	r.Use(headers.New("X-Request-ID", "request_id"))
//
//	// Required with validation
//	r.Use(headers.New("X-Tenant-ID", "tenant_id",
//		headers.WithRequired(),
//		headers.WithValidator(validateUUID)))
//
//	// Optional with default
//	r.Use(headers.New("X-Client-Version", "version",
//		headers.WithDefault("1.0.0")))
func New(header, ctxKey string, opts ...Option) func(http.Handler) http.Handler {
	h := &HeaderToContext{
		header: header,
		ctxKey: contextKey(ctxKey),
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
					if wrapper.HasState(r.Context()) {
						wrapper.SetError(r, wrapper.ErrBadRequest.With("Missing required header: "+h.header))
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
					if wrapper.HasState(r.Context()) {
						wrapper.SetError(r, wrapper.ErrBadRequest.With("Invalid "+h.header+" header: "+err.Error()))
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

// FromContext retrieves a value from the request context by key.
// Returns the value and true if present, or nil and false if not present.
// The returned value has type 'any' and should be type-asserted to the expected type.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if val, ok := headers.FromContext(r.Context(), "api_key"); ok {
//			apiKey := val.(string)
//			log.Printf("API Key: %s", apiKey)
//		}
//
//		// With type assertion for transformed values
//		if val, ok := headers.FromContext(r.Context(), "tenant_id"); ok {
//			tenantID := val.(uuid.UUID)
//			// Use typed value
//		}
//	}
func FromContext(ctx context.Context, key string) (any, bool) {
	val := ctx.Value(contextKey(key))
	if val == nil {
		return nil, false
	}
	return val, true
}
