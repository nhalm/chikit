// Package auth provides authentication middleware for API key and bearer token validation.
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/nhalm/chikit/wrapper"
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
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Missing API key"))
				return
			}

			if !config.Validator(key) {
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Invalid API key"))
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

// WithOptionalAPIKey makes the API key optional.
func WithOptionalAPIKey() APIKeyOption {
	return func(c *APIKeyConfig) {
		c.Optional = true
	}
}

// APIKeyFromContext retrieves the validated API key from the request context.
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
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Missing authorization header"))
				return
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Invalid authorization format"))
				return
			}

			token := strings.TrimPrefix(auth, prefix)
			if token == "" {
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Empty bearer token"))
				return
			}

			if !config.Validator(token) {
				wrapper.SetError(r, wrapper.ErrUnauthorized.With("Invalid bearer token"))
				return
			}

			ctx := context.WithValue(r.Context(), bearerTokenKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BearerTokenOption configures BearerToken middleware.
type BearerTokenOption func(*BearerTokenConfig)

// WithOptionalBearerToken makes the bearer token optional.
func WithOptionalBearerToken() BearerTokenOption {
	return func(c *BearerTokenConfig) {
		c.Optional = true
	}
}

// BearerTokenFromContext retrieves the validated bearer token from the request context.
func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(bearerTokenKey).(string)
	return token, ok
}
