// Package auth provides authentication middleware for API key and bearer token validation.
//
// The package offers two authentication strategies with customizable validators:
//   - APIKey: Validates API keys from configurable headers (default: X-API-Key)
//   - BearerToken: Validates bearer tokens from Authorization header
//
// Both strategies store validated credentials in the request context for downstream
// handlers to access. Validation failures return 401 (Unauthorized).
//
// Example with API key authentication:
//
//	validator := func(key string) bool {
//		return db.ValidateAPIKey(key)
//	}
//	r.Use(auth.APIKey(validator))
//
// Example with bearer token:
//
//	validator := func(token string) bool {
//		return jwt.Validate(token)
//	}
//	r.Use(auth.BearerToken(validator))
//
// Access authenticated values from context:
//
//	if key, ok := auth.APIKeyFromContext(ctx); ok {
//		// Use the validated API key
//	}
package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	apiKeyContextKey contextKey = "api_key"
	bearerTokenKey   contextKey = "bearer_token"
)

// APIKeyValidator validates an API key and returns true if valid.
// The validator function is provided by the application and can check
// against a database, cache, or any other validation mechanism.
type APIKeyValidator func(key string) bool

// APIKeyConfig configures the APIKey middleware.
type APIKeyConfig struct {
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
//	r.Use(auth.APIKey(validator))
//
// With custom header:
//
//	r.Use(auth.APIKey(validator, auth.WithAPIKeyHeader("X-Custom-Key")))
//
// Optional authentication:
//
//	r.Use(auth.APIKey(validator, auth.OptionalAPIKey()))
func APIKey(validator APIKeyValidator, opts ...APIKeyOption) func(http.Handler) http.Handler {
	config := APIKeyConfig{
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
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}

			if !config.Validator(key) {
				http.Error(w, "Invalid API key", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyOption configures APIKey middleware.
type APIKeyOption func(*APIKeyConfig)

// WithAPIKeyHeader sets the header to read the API key from.
// Default is "X-API-Key".
func WithAPIKeyHeader(header string) APIKeyOption {
	return func(c *APIKeyConfig) {
		c.Header = header
	}
}

// OptionalAPIKey makes the API key optional.
// When set, requests without an API key are allowed through without validation.
// The API key will not be present in the context for these requests.
func OptionalAPIKey() APIKeyOption {
	return func(c *APIKeyConfig) {
		c.Optional = true
	}
}

// APIKeyFromContext retrieves the validated API key from the request context.
// Returns the key and true if present, or empty string and false if not present.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if key, ok := auth.APIKeyFromContext(r.Context()); ok {
//			log.Printf("Request authenticated with key: %s", key)
//		}
//	}
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}

// BearerTokenValidator validates a bearer token and returns true if valid.
// The validator function is provided by the application and can perform
// JWT validation, token lookup, or any other validation mechanism.
type BearerTokenValidator func(token string) bool

// BearerTokenConfig configures the BearerToken middleware.
type BearerTokenConfig struct {
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
//	r.Use(auth.BearerToken(validator))
//
// Optional authentication:
//
//	r.Use(auth.BearerToken(validator, auth.OptionalBearerToken()))
func BearerToken(validator BearerTokenValidator, opts ...BearerTokenOption) func(http.Handler) http.Handler {
	config := BearerTokenConfig{
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
				http.Error(w, "Missing authorization header", http.StatusUnauthorized)
				return
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(auth, prefix)
			if token == "" {
				http.Error(w, "Empty bearer token", http.StatusUnauthorized)
				return
			}

			if !config.Validator(token) {
				http.Error(w, "Invalid bearer token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), bearerTokenKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BearerTokenOption configures BearerToken middleware.
type BearerTokenOption func(*BearerTokenConfig)

// OptionalBearerToken makes the bearer token optional.
// When set, requests without a bearer token are allowed through without validation.
// The token will not be present in the context for these requests.
func OptionalBearerToken() BearerTokenOption {
	return func(c *BearerTokenConfig) {
		c.Optional = true
	}
}

// BearerTokenFromContext retrieves the validated bearer token from the request context.
// Returns the token and true if present, or empty string and false if not present.
//
// Example:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		if token, ok := auth.BearerTokenFromContext(r.Context()); ok {
//			claims := jwt.Parse(token)
//			log.Printf("User: %s", claims.Subject)
//		}
//	}
func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(bearerTokenKey).(string)
	return token, ok
}
