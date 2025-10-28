package headers

import (
	"context"
	"net/http"
)

type contextKey string

// HeaderToContext extracts a header value and stores it in the request context.
type HeaderToContext struct {
	header     string
	ctxKey     contextKey
	required   bool
	defaultVal string
	validator  func(string) (any, error)
}

// Option configures a HeaderToContext middleware.
type Option func(*HeaderToContext)

// Required marks the header as required. Returns 400 if missing.
func Required() Option {
	return func(h *HeaderToContext) {
		h.required = true
	}
}

// WithDefault provides a default value if the header is missing.
func WithDefault(val string) Option {
	return func(h *HeaderToContext) {
		h.defaultVal = val
	}
}

// WithValidator provides a custom validator that can transform the header value.
// The validator should return an error if the value is invalid.
func WithValidator(fn func(string) (any, error)) Option {
	return func(h *HeaderToContext) {
		h.validator = fn
	}
}

// New creates middleware that extracts a header and stores it in context.
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
					http.Error(w, "Missing required header: "+h.header, http.StatusBadRequest)
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
					http.Error(w, "Invalid "+h.header+" header: "+err.Error(), http.StatusBadRequest)
					return
				}
			}

			ctx := context.WithValue(r.Context(), h.ctxKey, contextVal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromContext retrieves a value from the request context.
func FromContext(ctx context.Context, key string) (any, bool) {
	val := ctx.Value(contextKey(key))
	if val == nil {
		return nil, false
	}
	return val, true
}
