package chikit

// SLO (Service Level Objective) tracking middleware.
// Sets SLO tier and target in request context for the Handler middleware
// to log PASS/FAIL status based on request duration.

import (
	"context"
	"net/http"
	"time"
)

// SLOTier represents an SLO classification level.
type SLOTier string

const (
	// SLOCritical is for essential functions requiring 99.99% availability.
	SLOCritical SLOTier = "critical"

	// SLOHighFast is for user-facing requests requiring quick responses (99.9% availability, 100ms latency).
	SLOHighFast SLOTier = "high_fast"

	// SLOHighSlow is for important requests that can tolerate higher latency (99.9% availability, 1000ms latency).
	SLOHighSlow SLOTier = "high_slow"

	// SLOLow is for background tasks or non-interactive functions (99% availability).
	SLOLow SLOTier = "low"

	// sloCustom is used internally for SLOWithTarget.
	sloCustom SLOTier = "custom"
)

var sloTargets = map[SLOTier]time.Duration{
	SLOCritical: 50 * time.Millisecond,
	SLOHighFast: 100 * time.Millisecond,
	SLOHighSlow: 1000 * time.Millisecond,
	SLOLow:      5000 * time.Millisecond,
}

type sloContextKey string

const sloConfigKey sloContextKey = "slo_config"

type sloConfig struct {
	tier   SLOTier
	target time.Duration
}

// SLO sets a predefined SLO tier in context.
// The tier determines the latency target:
//   - SLOCritical: 50ms
//   - SLOHighFast: 100ms
//   - SLOHighSlow: 1000ms
//   - SLOLow: 5000ms
func SLO(tier SLOTier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := &sloConfig{
				tier:   tier,
				target: sloTargets[tier],
			}
			ctx := context.WithValue(r.Context(), sloConfigKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SLOWithTarget sets a custom SLO target in context.
// The tier is logged as "custom".
func SLOWithTarget(target time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := &sloConfig{
				tier:   sloCustom,
				target: target,
			}
			ctx := context.WithValue(r.Context(), sloConfigKey, cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSLO retrieves the SLO tier and target from context.
// Returns the tier, target duration, and true if set; otherwise empty values and false.
func GetSLO(ctx context.Context) (SLOTier, time.Duration, bool) {
	cfg, ok := ctx.Value(sloConfigKey).(*sloConfig)
	if !ok {
		return "", 0, false
	}
	return cfg.tier, cfg.target, true
}
