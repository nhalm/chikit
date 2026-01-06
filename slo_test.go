package chikit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestTrack_SetsTierInContext(t *testing.T) {
	tests := []struct {
		name           string
		tier           SLOTier
		expectedTier   SLOTier
		expectedTarget time.Duration
	}{
		{"Critical", SLOCritical, SLOCritical, 50 * time.Millisecond},
		{"HighFast", SLOHighFast, SLOHighFast, 100 * time.Millisecond},
		{"HighSlow", SLOHighSlow, SLOHighSlow, 1000 * time.Millisecond},
		{"Low", SLOLow, SLOLow, 5000 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedTier SLOTier
			var capturedTarget time.Duration
			var found bool

			handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				capturedTier, capturedTarget, found = GetSLO(r.Context())
			})

			middleware := SLO(tt.tier)
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

	var capturedTier SLOTier
	var capturedTarget time.Duration
	var found bool

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedTier, capturedTarget, found = GetSLO(r.Context())
	})

	middleware := SLOWithTarget(customTarget)
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
		tier, target, found := GetSLO(r.Context())
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
	var capturedTier SLOTier
	var capturedTarget time.Duration
	var found bool

	r := chi.NewRouter()
	r.With(SLO(SLOHighFast)).Get("/users/{id}", func(_ http.ResponseWriter, r *http.Request) {
		capturedTier, capturedTarget, found = GetSLO(r.Context())
	})

	req := httptest.NewRequest("GET", "/users/123", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !found {
		t.Fatal("expected SLO tier to be set in context")
	}

	if capturedTier != SLOHighFast {
		t.Errorf("expected tier %s, got %s", SLOHighFast, capturedTier)
	}

	if capturedTarget != 100*time.Millisecond {
		t.Errorf("expected target 100ms, got %v", capturedTarget)
	}
}

func TestTrack_DifferentRoutesHaveDifferentSLOs(t *testing.T) {
	r := chi.NewRouter()

	var healthTier, usersTier, reportsTier SLOTier

	r.With(SLO(SLOCritical)).Get("/health", func(_ http.ResponseWriter, r *http.Request) {
		healthTier, _, _ = GetSLO(r.Context())
	})

	r.With(SLO(SLOHighFast)).Get("/users/{id}", func(_ http.ResponseWriter, r *http.Request) {
		usersTier, _, _ = GetSLO(r.Context())
	})

	r.With(SLO(SLOHighSlow)).Post("/reports", func(_ http.ResponseWriter, r *http.Request) {
		reportsTier, _, _ = GetSLO(r.Context())
	})

	req := httptest.NewRequest("GET", "/health", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("GET", "/users/123", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("POST", "/reports", http.NoBody)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if healthTier != SLOCritical {
		t.Errorf("expected health tier %s, got %s", SLOCritical, healthTier)
	}
	if usersTier != SLOHighFast {
		t.Errorf("expected users tier %s, got %s", SLOHighFast, usersTier)
	}
	if reportsTier != SLOHighSlow {
		t.Errorf("expected reports tier %s, got %s", SLOHighSlow, reportsTier)
	}
}

func TestTrack_RouteWithoutSLO(t *testing.T) {
	r := chi.NewRouter()

	var withSLOFound, withoutSLOFound bool

	r.With(SLO(SLOHighFast)).Get("/with-slo", func(_ http.ResponseWriter, r *http.Request) {
		_, _, withSLOFound = GetSLO(r.Context())
	})

	r.Get("/without-slo", func(_ http.ResponseWriter, r *http.Request) {
		_, _, withoutSLOFound = GetSLO(r.Context())
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
	if SLOCritical != "critical" {
		t.Errorf("expected Critical = 'critical', got %s", SLOCritical)
	}
	if SLOHighFast != "high_fast" {
		t.Errorf("expected HighFast = 'high_fast', got %s", SLOHighFast)
	}
	if SLOHighSlow != "high_slow" {
		t.Errorf("expected HighSlow = 'high_slow', got %s", SLOHighSlow)
	}
	if SLOLow != "low" {
		t.Errorf("expected Low = 'low', got %s", SLOLow)
	}
}
