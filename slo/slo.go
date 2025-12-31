// Package slo provides SLO (Service Level Objective) tracking middleware.
//
// The package sets SLO tier and target in request context for the wrapper
// middleware to log. This enables per-route SLO classification with
// PASS/FAIL status based on request duration.
//
// Basic usage:
//
//	r := chi.NewRouter()
//	r.Use(wrapper.New(wrapper.WithCanonlog(), wrapper.WithSLOs()))
//
//	r.With(slo.Track(slo.HighFast)).Get("/users/{id}", getUser)
//	r.With(slo.Track(slo.HighSlow)).Post("/reports", generateReport)
//
// Custom target:
//
//	r.With(slo.TrackWithTarget(200 * time.Millisecond)).Get("/custom", handler)
//
// The wrapper middleware reads the SLO config from context and logs:
//   - slo_class: The tier name (critical, high_fast, high_slow, low, custom)
//   - slo_status: PASS or FAIL based on duration vs target
package slo

import (
	"context"
	"net/http"
	"time"
)

// Tier represents an SLO classification level.
type Tier string

const (
	// Critical is for essential functions requiring 99.99% availability.
	Critical Tier = "critical"

	// HighFast is for user-facing requests requiring quick responses (99.9% availability, 100ms latency).
	HighFast Tier = "high_fast"

	// HighSlow is for important requests that can tolerate higher latency (99.9% availability, 1000ms latency).
	HighSlow Tier = "high_slow"

	// Low is for background tasks or non-interactive functions (99% availability).
	Low Tier = "low"

	// custom is used internally for TrackWithTarget.
	custom Tier = "custom"
)

var targets = map[Tier]time.Duration{
	Critical: 50 * time.Millisecond,
	HighFast: 100 * time.Millisecond,
	HighSlow: 1000 * time.Millisecond,
	Low:      5000 * time.Millisecond,
}

type contextKey string

const configKey contextKey = "slo_config"

type config struct {
	tier   Tier
	target time.Duration
}

// Track sets a predefined SLO tier in context.
// The tier determines the latency target:
//   - Critical: 50ms
//   - HighFast: 100ms
//   - HighSlow: 1000ms
//   - Low: 5000ms
func Track(tier Tier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := &config{
				tier:   tier,
				target: targets[tier],
			}
			ctx := context.WithValue(r.Context(), configKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TrackWithTarget sets a custom SLO target in context.
// The tier is logged as "custom".
func TrackWithTarget(target time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := &config{
				tier:   custom,
				target: target,
			}
			ctx := context.WithValue(r.Context(), configKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetTier retrieves the SLO tier and target from context.
// Returns the tier, target duration, and true if set; otherwise empty values and false.
func GetTier(ctx context.Context) (Tier, time.Duration, bool) {
	cfg, ok := ctx.Value(configKey).(*config)
	if !ok {
		return "", 0, false
	}
	return cfg.tier, cfg.target, true
}
