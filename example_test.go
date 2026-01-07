package chikit_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/chikit"
	"github.com/nhalm/chikit/store"
)

func ExampleHandler() {
	r := chi.NewRouter()
	r.Use(chikit.Handler())

	r.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		chikit.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func ExampleSetError() {
	handler := func(_ http.ResponseWriter, r *http.Request) {
		// Return a 404 with custom message
		chikit.SetError(r, chikit.ErrNotFound.With("User not found"))
	}
	_ = handler
}

func ExampleNewRateLimiter() {
	st := store.NewMemory()
	defer st.Close()

	// Rate limit by IP: 100 requests per minute
	limiter := chikit.NewRateLimiter(st, 100, time.Minute,
		chikit.RateLimitWithIP(),
	)

	r := chi.NewRouter()
	r.Use(limiter.Handler)
}

func ExampleNewRateLimiter_multiDimensional() {
	st := store.NewMemory()
	defer st.Close()

	// Rate limit by tenant + endpoint: 100 requests per minute
	limiter := chikit.NewRateLimiter(st, 100, time.Minute,
		chikit.RateLimitWithHeaderRequired("X-Tenant-ID"),
		chikit.RateLimitWithEndpoint(),
	)

	r := chi.NewRouter()
	r.Use(limiter.Handler)
}

func ExampleExtractHeader() {
	r := chi.NewRouter()

	// Extract optional header with default
	r.Use(chikit.ExtractHeader("X-Request-ID", "request_id",
		chikit.ExtractDefault("unknown"),
	))

	r.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		if val, ok := chikit.HeaderFromContext(r.Context(), "request_id"); ok {
			fmt.Printf("Request ID: %s\n", val)
		}
	})
}

func ExampleAPIKey() {
	validator := func(key string) bool {
		return key == "valid-api-key"
	}

	r := chi.NewRouter()
	r.Use(chikit.APIKey(validator))
}

func ExampleJSON() {
	type Request struct {
		Email string `json:"email" validate:"required,email"`
	}

	handler := func(_ http.ResponseWriter, r *http.Request) {
		var req Request
		if !chikit.JSON(r, &req) {
			return // Validation error already set
		}
		chikit.SetResponse(r, http.StatusOK, req)
	}
	_ = handler
}

func ExampleMaxBodySize() {
	r := chi.NewRouter()
	r.Use(chikit.Handler())
	r.Use(chikit.MaxBodySize(1024 * 1024)) // 1MB limit
}

func ExampleValidateHeaders() {
	r := chi.NewRouter()
	r.Use(chikit.ValidateHeaders(
		chikit.ValidateWithHeader("Content-Type",
			chikit.ValidateRequired(),
			chikit.ValidateAllowList("application/json"),
		),
	))
}

func ExampleHandler_timeout() {
	r := chi.NewRouter()
	r.Use(chikit.Handler(
		chikit.WithTimeout(30*time.Second),
		chikit.WithCanonlog(),
	))

	r.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		// Handler code runs with a 30-second deadline.
		// If the handler doesn't complete in time, a 504 Gateway Timeout
		// is returned to the client immediately.
		chikit.SetResponse(r, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func ExampleHandler_timeoutWithGrace() {
	r := chi.NewRouter()
	r.Use(chikit.Handler(
		chikit.WithTimeout(30*time.Second),
		chikit.WithGraceTimeout(10*time.Second),
		chikit.WithAbandonCallback(func(r *http.Request) {
			// Handler didn't exit within grace period after timeout.
			// Log this for investigation - may indicate a stuck handler.
			fmt.Printf("handler abandoned: %s %s\n", r.Method, r.URL.Path)
		}),
	))
}
