package chikit

import (
	"context"
	"net/http"
	"strings"
)

type authContextKey string

const (
	apiKeyKey      authContextKey = "api_key"
	bearerTokenKey authContextKey = "bearer_token"
)

// APIKeyValidator validates an API key and returns true if valid.
// The validator function is provided by the application and can check
// against a database, cache, or any other validation mechanism.
//
// Thread safety: Validators are called concurrently from multiple goroutines
// and must be safe for concurrent use. Avoid shared mutable state.
type APIKeyValidator func(key string) bool

// apiKeyConfig configures the APIKey middleware.
type apiKeyConfig struct {
	// Header is the HTTP header to read the API key from (default: "X-API-Key")
	Header string

	// Validator is the function that validates the API key
	Validator APIKeyValidator

	// Optional determines whether the API key is required (default: false)
	// When true, requests without an API key are allowed through
	Optional bool
}

// APIKey returns middleware that validates API keys from a header.
// Returns 401 (Unauthorized) if the key is missing (when required) or invalid.
// The validated API key is stored in the request context and can be retrieved
// using APIKeyFromContext.
//
// Example:
//
//	validator := func(key string) bool {
//		return key == "secret-key" // In production, check against DB/cache
//	}
//	r.Use(chikit.APIKey(validator))
//
// With custom header:
//
//	r.Use(chikit.APIKey(validator, chikit.WithAPIKeyHeader("X-Custom-Key")))
//
// Optional authentication:
//
//	r.Use(chikit.APIKey(validator, chikit.WithOptionalAPIKey()))
func APIKey(validator APIKeyValidator, opts ...APIKeyOption) func(http.Handler) http.Handler {
	config := apiKeyConfig{
		Header:    "X-API-Key",
		Validator: validator,
		Optional:  false,
	}

	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(config.Header)

			if key == "" {
				if config.Optional {
					next.ServeHTTP(w, r)
					return
				}
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Missing API key"))
				} else {
					http.Error(w, "Missing API key", http.StatusUnauthorized)
				}
				return
			}

			if !config.Validator(key) {
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Invalid API key"))
				} else {
					http.Error(w, "Invalid API key", http.StatusUnauthorized)
				}
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyOption configures APIKey middleware.
type APIKeyOption func(*apiKeyConfig)

// WithAPIKeyHeader sets the header to read the API key from.
// Default is "X-API-Key".
func WithAPIKeyHeader(header string) APIKeyOption {
	return func(c *apiKeyConfig) {
		c.Header = header
	}
}

// WithOptionalAPIKey makes the API key optional.
// When set, requests without an API key are allowed through without validation.
// The API key will not be present in the context for these requests.
func WithOptionalAPIKey() APIKeyOption {
	return func(c *apiKeyConfig) {
		c.Optional = true
	}
}

// APIKeyFromContext retrieves the validated API key from the request context.
// Returns the key and true if present, or empty string and false if not present.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if key, ok := chikit.APIKeyFromContext(r.Context()); ok {
//			log.Printf("Request authenticated with key: %s", key)
//		}
//	}
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyKey).(string)
	return key, ok
}

// BearerTokenValidator validates a bearer token and returns true if valid.
// The validator function is provided by the application and can perform
// JWT validation, token lookup, or any other validation mechanism.
//
// Thread safety: Validators are called concurrently from multiple goroutines
// and must be safe for concurrent use. Avoid shared mutable state.
type BearerTokenValidator func(token string) bool

// bearerTokenConfig configures the BearerToken middleware.
type bearerTokenConfig struct {
	// Validator is the function that validates the bearer token
	Validator BearerTokenValidator

	// Optional determines whether the bearer token is required (default: false)
	// When true, requests without a bearer token are allowed through
	Optional bool
}

// BearerToken returns middleware that validates bearer tokens from the Authorization header.
// Expects the header format "Bearer <token>". Returns 401 (Unauthorized) if the token
// is missing (when required), malformed, or invalid. The validated token is stored in
// the request context and can be retrieved using BearerTokenFromContext.
//
// Example:
//
//	validator := func(token string) bool {
//		return jwt.Validate(token) // Use your JWT library
//	}
//	r.Use(chikit.BearerToken(validator))
//
// Optional authentication:
//
//	r.Use(chikit.BearerToken(validator, chikit.WithOptionalBearerToken()))
func BearerToken(validator BearerTokenValidator, opts ...BearerTokenOption) func(http.Handler) http.Handler {
	config := bearerTokenConfig{
		Validator: validator,
		Optional:  false,
	}

	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")

			if auth == "" {
				if config.Optional {
					next.ServeHTTP(w, r)
					return
				}
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Missing authorization header"))
				} else {
					http.Error(w, "Missing authorization header", http.StatusUnauthorized)
				}
				return
			}

			// RFC 7235: "Bearer" scheme is case-insensitive
			if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Invalid authorization format"))
				} else {
					http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
				}
				return
			}

			token := auth[7:] // Extract token after "Bearer "
			if token == "" {
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Empty bearer token"))
				} else {
					http.Error(w, "Empty bearer token", http.StatusUnauthorized)
				}
				return
			}

			if !config.Validator(token) {
				if HasState(r.Context()) {
					SetError(r, ErrUnauthorized.With("Invalid bearer token"))
				} else {
					http.Error(w, "Invalid bearer token", http.StatusUnauthorized)
				}
				return
			}

			ctx := context.WithValue(r.Context(), bearerTokenKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BearerTokenOption configures BearerToken middleware.
type BearerTokenOption func(*bearerTokenConfig)

// WithOptionalBearerToken makes the bearer token optional.
// When set, requests without a bearer token are allowed through without validation.
// The token will not be present in the context for these requests.
func WithOptionalBearerToken() BearerTokenOption {
	return func(c *bearerTokenConfig) {
		c.Optional = true
	}
}

// BearerTokenFromContext retrieves the validated bearer token from the request context.
// Returns the token and true if present, or empty string and false if not present.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if token, ok := chikit.BearerTokenFromContext(r.Context()); ok {
//			claims := jwt.Parse(token)
//			log.Printf("User: %s", claims.Subject)
//		}
//	}
func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(bearerTokenKey).(string)
	return token, ok
}
