// Package auth provides authentication middleware.
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
type APIKeyValidator func(key string) bool

// APIKeyConfig configures the APIKey middleware.
type APIKeyConfig struct {
	Header    string
	Validator APIKeyValidator
	Optional  bool
}

// APIKey returns middleware that validates API keys from a header.
// Returns 401 if the key is missing or invalid.
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
func WithAPIKeyHeader(header string) APIKeyOption {
	return func(c *APIKeyConfig) {
		c.Header = header
	}
}

// OptionalAPIKey makes the API key optional.
func OptionalAPIKey() APIKeyOption {
	return func(c *APIKeyConfig) {
		c.Optional = true
	}
}

// APIKeyFromContext retrieves the API key from the request context.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}

// BearerTokenValidator validates a bearer token and returns true if valid.
type BearerTokenValidator func(token string) bool

// BearerTokenConfig configures the BearerToken middleware.
type BearerTokenConfig struct {
	Validator BearerTokenValidator
	Optional  bool
}

// BearerToken returns middleware that validates bearer tokens from the Authorization header.
// Returns 401 if the token is missing or invalid.
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
func OptionalBearerToken() BearerTokenOption {
	return func(c *BearerTokenConfig) {
		c.Optional = true
	}
}

// BearerTokenFromContext retrieves the bearer token from the request context.
func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(bearerTokenKey).(string)
	return token, ok
}
