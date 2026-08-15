package computegardener

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	cachepkg "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/cache"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	testingmocks "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/testing"

	testingclock "k8s.io/utils/clock/testing"
)

// newCacheHealthTestScheduler builds a scheduler wired up for exercising the
// carbon cache health logic: carbon enabled with a healthy (reachable) mock
// data source, pricing disabled, and a real cache whose contents the test
// controls.
func newCacheHealthTestScheduler(t *testing.T, cache *cachepkg.Cache, failures int) *ComputeGardenerScheduler {
	t.Helper()

	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Cache: config.APICacheConfig{
			Timeout:     time.Second,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			RateLimit:   10,
			CacheTTL:    time.Minute,
			MaxCacheAge: time.Hour,
		},
		Carbon: config.CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: 200,
			APIConfig: config.ElectricityMapsAPIConfig{
				APIKey: "test-key",
				Region: "test-region",
				URL:    "http://mock-url/",
			},
		},
		Pricing: config.PriceConfig{
			Enabled: false,
		},
	}

	cs := &ComputeGardenerScheduler{
		handle:            &mockHandle{},
		config:            cfg,
		cache:             cache,
		priceImpl:         testingmocks.NewMockPricing(0.1),
		carbonImpl:        testingmocks.NewMockCarbon(100),
		clock:             testingclock.NewFakeClock(baseTime),
		startTime:         baseTime.Add(-10 * time.Minute),
		carbonDelayedPods: make(map[string]bool),
		priceDelayedPods:  make(map[string]bool),
	}
	cs.carbonRefreshFailures = failures
	return cs
}

// newCacheHealthCache creates a cache populated with a single entry for the
// test region, backdated by the given age (0 leaves it fresh).
func newCacheHealthCache(t *testing.T, ttl, age time.Duration) *cachepkg.Cache {
	t.Helper()
	cache := cachepkg.New(ttl, time.Hour)
	t.Cleanup(cache.Close)
	cache.Set("test-region", &api.ElectricityData{CarbonIntensity: 100})
	if age > 0 {
		cache.TestBackdate("test-region", age)
	}
	return cache
}

func TestCheckCarbonCacheHealth(t *testing.T) {
	// checkCarbonCacheHealth is a pure function of cache state and the
	// refresh-worker failure counter; it does NOT gate on the carbon enabled
	// flag (its caller, healthCheck, does). These cases cover the distinction
	// introduced for issue #50: an empty or merely-stale cache is NOT an error
	// unless the refresh worker is demonstrably failing.
	tests := []struct {
		name        string
		ttl         time.Duration
		cacheAge    time.Duration // age of the single cached entry; emptyCache => no entry
		emptyCache  bool
		failures    int
		expectError bool
		errContains string
	}{
		{
			name:       "empty cache, no failures is normal at startup",
			ttl:        time.Minute,
			emptyCache: true,
			failures:   0,
		},
		{
			name:        "empty cache with repeated refresh failures is an error",
			ttl:         time.Minute,
			emptyCache:  true,
			failures:    3,
			expectError: true,
			errContains: "not reaching the cache",
		},
		{
			name:     "fresh data, healthy refresh",
			ttl:      time.Minute,
			cacheAge: 10 * time.Second,
			failures: 0,
		},
		{
			// Past TTL but the refresh worker has not been failing: transient
			// staleness, must NOT be an error (the on-demand fetch covers it).
			// This is the core of issue #50.
			name:     "stale data but healthy refresh is not an error",
			ttl:      time.Minute,
			cacheAge: 3 * time.Minute,
			failures: 0,
		},
		{
			name:        "stale data and repeated refresh failures is an error",
			ttl:         time.Minute,
			cacheAge:    4 * time.Minute,
			failures:    4,
			expectError: true,
			errContains: "may be unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cache *cachepkg.Cache
			if tt.emptyCache {
				cache = cachepkg.New(tt.ttl, time.Hour)
				t.Cleanup(cache.Close)
			} else {
				cache = newCacheHealthCache(t, tt.ttl, tt.cacheAge)
			}

			cs := newCacheHealthTestScheduler(t, cache, tt.failures)

			err := cs.checkCarbonCacheHealth()
			if (err != nil) != tt.expectError {
				t.Fatalf("checkCarbonCacheHealth() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

// TestHealthCheckCarbonCacheGating verifies that healthCheck skips the carbon
// cache inspection entirely when carbon is disabled, even if the cache state
// would otherwise be an error. (When carbon is enabled, healthCheck delegates
// to checkCarbonCacheHealth, which is covered in TestCheckCarbonCacheHealth.)
func TestHealthCheckCarbonCacheGating(t *testing.T) {
	// A cache state that checkCarbonCacheHealth would flag as an error.
	cache := newCacheHealthCache(t, time.Minute, 10*time.Minute)
	cs := newCacheHealthTestScheduler(t, cache, 5)

	cs.config.Carbon.Enabled = false
	if err := cs.healthCheck(context.Background()); err != nil {
		t.Fatalf("healthCheck() with carbon disabled should ignore the carbon cache, got error: %v", err)
	}

	// Sanity check: the same scheduler DOES surface the cache problem once
	// carbon is re-enabled, confirming the gating (not the cache state) is
	// what suppressed it.
	cs.config.Carbon.Enabled = true
	if err := cs.healthCheck(context.Background()); err == nil {
		t.Fatal("healthCheck() with carbon enabled should surface the stale-cache error")
	}
}

func TestCarbonCacheRefreshOutcomeTracking(t *testing.T) {
	cs := newCacheHealthTestScheduler(t, cachepkg.New(time.Minute, time.Hour), 0)
	t.Cleanup(cs.cache.Close)

	cs.recordCarbonRefreshOutcome(false)
	cs.recordCarbonRefreshOutcome(false)
	cs.recordCarbonRefreshOutcome(false)
	cs.refreshMu.Lock()
	failures := cs.carbonRefreshFailures
	cs.refreshMu.Unlock()
	if failures != 3 {
		t.Fatalf("Expected 3 consecutive failures, got %d", failures)
	}

	// A single success resets the streak.
	cs.recordCarbonRefreshOutcome(true)
	cs.refreshMu.Lock()
	failures = cs.carbonRefreshFailures
	cs.refreshMu.Unlock()
	if failures != 0 {
		t.Fatalf("Expected failure streak reset to 0 after success, got %d", failures)
	}

	// Failure streaks build up again after a recovery.
	cs.recordCarbonRefreshOutcome(false)
	cs.refreshMu.Lock()
	failures = cs.carbonRefreshFailures
	cs.refreshMu.Unlock()
	if failures != 1 {
		t.Fatalf("Expected 1 failure after recovery, got %d", failures)
	}
}

// TestRefreshCarbonCacheRecordsOutcomes drives refreshCarbonCache end-to-end
// with a mock data source that succeeds then fails, verifying the outcome is
// recorded.
func TestRefreshCarbonCacheRecordsOutcomes(t *testing.T) {
	cs := newCacheHealthTestScheduler(t, cachepkg.New(time.Minute, time.Hour), 0)
	t.Cleanup(cs.cache.Close)

	// Success path.
	cs.refreshCarbonCache(context.Background(), []string{"test-region"})
	cs.refreshMu.Lock()
	failures := cs.carbonRefreshFailures
	cs.refreshMu.Unlock()
	if failures != 0 {
		t.Fatalf("Expected 0 failures after a successful refresh, got %d", failures)
	}

	// Now make the data source fail and refresh again.
	cs.carbonImpl = testingmocks.NewMockCarbonWithError()
	cs.refreshCarbonCache(context.Background(), []string{"test-region"})
	cs.refreshMu.Lock()
	failures = cs.carbonRefreshFailures
	cs.refreshMu.Unlock()
	if failures != 1 {
		t.Fatalf("Expected 1 failure after a failed refresh, got %d", failures)
	}
}
