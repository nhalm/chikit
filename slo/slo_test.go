package slo_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nhalm/chikit/slo"
)

func TestTrack_SetsTierInContext(t *testing.T) {
	tests := []struct {
		name           string
		tier           slo.Tier
		expectedTier   slo.Tier
		expectedTarget time.Duration
	}{
		{"Critical", slo.Critical, slo.Critical, 50 * time.Millisecond},
		{"HighFast", slo.HighFast, slo.HighFast, 100 * time.Millisecond},
		{"HighSlow", slo.HighSlow, slo.HighSlow, 1000 * time.Millisecond},
		{"Low", slo.Low, slo.Low, 5000 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedTier slo.Tier
			var capturedTarget time.Duration
			var found bool

			handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				capturedTier, capturedTarget, found = slo.GetTier(r.Context())
			})

			middleware := slo.Track(tt.tier)
			tracked := middleware(handler)

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			rec := httptest.NewRecorder()
			tracked.ServeHTTP(rec, req)

			if !found {
				t.Fatal("expected SLO tier to be set in context")
			}

			if capturedTier != tt.expectedTier {
				t.Errorf("expected tier %s, got %s", tt.expectedTier, capturedTier)
			}

			if capturedTarget != tt.expectedTarget {
				t.Errorf("expected target %v, got %v", tt.expectedTarget, capturedTarget)
			}
		})
	}
}

func TestTrackWithTarget_SetsCustomTarget(t *testing.T) {
	customTarget := 200 * time.Millisecond

	var capturedTier slo.Tier
	var capturedTarget time.Duration
	var found bool

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedTier, capturedTarget, found = slo.GetTier(r.Context())
	})

	middleware := slo.TrackWithTarget(customTarget)
	tracked := middleware(handler)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	tracked.ServeHTTP(rec, req)

	if !found {
		t.Fatal("expected SLO tier to be set in context")
	}

	if capturedTier != "custom" {
		t.Errorf("expected tier 'custom', got %s", capturedTier)
	}

	if capturedTarget != customTarget {
		t.Errorf("expected target %v, got %v", customTarget, capturedTarget)
	}
}

func TestGetTier_NoContext(t *testing.T) {
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tier, target, found := slo.GetTier(r.Context())
		if found {
			t.Error("expected SLO tier to not be found in context")
		}
		if tier != "" {
			t.Errorf("expected empty tier, got %s", tier)
		}
		if target != 0 {
			t.Errorf("expected zero target, got %v", target)
		}
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestTrack_WithChiRouter(t *testing.T) {
	var capturedTier slo.Tier
	var capturedTarget time.Duration
	var found bool

	r := chi.NewRouter()
	r.With(slo.Track(slo.HighFast)).Get("/users/{id}", func(_ http.ResponseWriter, r *http.Request) {
		capturedTier, capturedTarget, found = slo.GetTier(r.Context())
	})

	req := httptest.NewRequest("GET", "/users/123", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !found {
		t.Fatal("expected SLO tier to be set in context")
	}

	if capturedTier != slo.HighFast {
		t.Errorf("expected tier %s, got %s", slo.HighFast, capturedTier)
	}

	if capturedTarget != 100*time.Millisecond {
		t.Errorf("expected target 100ms, got %v", capturedTarget)
	}
}

func TestTrack_DifferentRoutesHaveDifferentSLOs(t *testing.T) {
	r := chi.NewRouter()

	var healthTier, usersTier, reportsTier slo.Tier

	r.With(slo.Track(slo.Critical)).Get("/health", func(_ http.ResponseWriter, r *http.Request) {
		healthTier, _, _ = slo.GetTier(r.Context())
	})

	r.With(slo.Track(slo.HighFast)).Get("/users/{id}", func(_ http.ResponseWriter, r *http.Request) {
		usersTier, _, _ = slo.GetTier(r.Context())
	})

	r.With(slo.Track(slo.HighSlow)).Post("/reports", func(_ http.ResponseWriter, r *http.Request) {
		reportsTier, _, _ = slo.GetTier(r.Context())
	})

	req := httptest.NewRequest("GET", "/health", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("GET", "/users/123", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("POST", "/reports", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if healthTier != slo.Critical {
		t.Errorf("expected health tier %s, got %s", slo.Critical, healthTier)
	}
	if usersTier != slo.HighFast {
		t.Errorf("expected users tier %s, got %s", slo.HighFast, usersTier)
	}
	if reportsTier != slo.HighSlow {
		t.Errorf("expected reports tier %s, got %s", slo.HighSlow, reportsTier)
	}
}

func TestTrack_RouteWithoutSLO(t *testing.T) {
	r := chi.NewRouter()

	var withSLOFound, withoutSLOFound bool

	r.With(slo.Track(slo.HighFast)).Get("/with-slo", func(_ http.ResponseWriter, r *http.Request) {
		_, _, withSLOFound = slo.GetTier(r.Context())
	})

	r.Get("/without-slo", func(_ http.ResponseWriter, r *http.Request) {
		_, _, withoutSLOFound = slo.GetTier(r.Context())
	})

	req := httptest.NewRequest("GET", "/with-slo", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("GET", "/without-slo", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if !withSLOFound {
		t.Error("expected SLO to be found on /with-slo route")
	}
	if withoutSLOFound {
		t.Error("expected SLO to not be found on /without-slo route")
	}
}

func TestTierConstants(t *testing.T) {
	if slo.Critical != "critical" {
		t.Errorf("expected Critical = 'critical', got %s", slo.Critical)
	}
	if slo.HighFast != "high_fast" {
		t.Errorf("expected HighFast = 'high_fast', got %s", slo.HighFast)
	}
	if slo.HighSlow != "high_slow" {
		t.Errorf("expected HighSlow = 'high_slow', got %s", slo.HighSlow)
	}
	if slo.Low != "low" {
		t.Errorf("expected Low = 'low', got %s", slo.Low)
	}
}
