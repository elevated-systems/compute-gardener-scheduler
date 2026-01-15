package carbon

import (
	"context"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

func TestFactory(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.CarbonConfig
		wantImpl   bool
		wantErr    bool
		wantNil    bool
	}{
		{
			name: "electricity-maps-api provider",
			cfg: config.CarbonConfig{
				Enabled:  true,
				Provider: "electricity-maps-api",
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "US-CAL-CISO",
					APIKey: "test-key",
					URL:    "https://test.com",
				},
			},
			wantImpl: true,
			wantErr:  false,
			wantNil:  false,
		},
		{
			name: "cg-api provider",
			cfg: config.CarbonConfig{
				Enabled:  true,
				Provider: "cg-api",
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "US-CAL-CISO",
					APIKey: "test-key",
				},
				CGAPIConfig: &config.CGAPIConfig{
					Endpoint:     "https://api.computegardener.io",
					APIKey:       "cg-test-key",
					Timeout:      10 * time.Second,
					FallbackToEM: false,
				},
			},
			wantImpl: true,
			wantErr:  false,
			wantNil:  false,
		},
		{
			name: "cg-api provider with fallback",
			cfg: config.CarbonConfig{
				Enabled:  true,
				Provider: "cg-api",
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "US-CAL-CISO",
					APIKey: "test-key",
					URL:    "https://test.com",
				},
				CGAPIConfig: &config.CGAPIConfig{
					Endpoint:     "https://api.computegardener.io",
					APIKey:       "cg-test-key",
					Timeout:      10 * time.Second,
					FallbackToEM: true,
				},
			},
			wantImpl: true,
			wantErr:  false,
			wantNil:  false,
		},
		{
			name: "cg-api provider missing config",
			cfg: config.CarbonConfig{
				Enabled:  true,
				Provider: "cg-api",
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "US-CAL-CISO",
					APIKey: "test-key",
				},
				// CGAPIConfig is nil
			},
			wantImpl: false,
			wantErr:  true,
			wantNil:  false,
		},
		{
			name: "unknown provider",
			cfg: config.CarbonConfig{
				Enabled:  true,
				Provider: "unknown-provider",
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "US-CAL-CISO",
					APIKey: "test-key",
				},
			},
			wantImpl: false,
			wantErr:  true,
			wantNil:  false,
		},
		{
			name: "disabled carbon",
			cfg: config.CarbonConfig{
				Enabled:  false,
				Provider: "electricity-maps-api",
			},
			wantImpl: false,
			wantErr:  false,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test API client
			apiConfig := config.ElectricityMapsAPIConfig{
				URL:    "https://test-api.example.com/",
				APIKey: "test-key",
				Region: "test-region",
			}
			cacheConfig := config.APICacheConfig{
				CacheTTL:    time.Minute,
				MaxCacheAge: time.Minute * 10,
				Timeout:     time.Second * 5,
				MaxRetries:  3,
				RetryDelay:  time.Millisecond * 100,
				RateLimit:   10,
			}
			apiClient := api.NewClient(apiConfig, cacheConfig)

			// Execute
			impl, err := Factory(&tt.cfg, apiClient)

			// Verify error
			if (err != nil) != tt.wantErr {
				t.Errorf("Factory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify nil behavior
			if tt.wantNil && impl != nil {
				t.Error("Factory() expected nil implementation for disabled carbon")
			}

			// Verify implementation returned
			if tt.wantImpl && impl == nil {
				t.Error("Factory() returned nil implementation when one was expected")
			}
		})
	}
}

func TestFactory_ProviderTypes(t *testing.T) {
	apiConfig := config.ElectricityMapsAPIConfig{
		URL:    "https://test-api.example.com/",
		APIKey: "test-key",
		Region: "test-region",
	}
	cacheConfig := config.APICacheConfig{
		CacheTTL:    time.Minute,
		MaxCacheAge: time.Minute * 10,
		Timeout:     time.Second * 5,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
		RateLimit:   10,
	}
	apiClient := api.NewClient(apiConfig, cacheConfig)

	t.Run("electricity-maps creates correct type", func(t *testing.T) {
		cfg := &config.CarbonConfig{
			Enabled:  true,
			Provider: "electricity-maps-api",
			APIConfig: config.ElectricityMapsAPIConfig{
				Region: "US-CAL-CISO",
				APIKey: "test-key",
				URL:    "https://test.com",
			},
		}

		impl, err := Factory(cfg, apiClient)
		if err != nil {
			t.Fatalf("Factory() error = %v", err)
		}

		_, ok := impl.(*electricityMapsImplementation)
		if !ok {
			t.Errorf("Factory() did not return *electricityMapsImplementation for electricity-maps-api provider")
		}
	})

	t.Run("cg-api creates correct type", func(t *testing.T) {
		cfg := &config.CarbonConfig{
			Enabled:  true,
			Provider: "cg-api",
			APIConfig: config.ElectricityMapsAPIConfig{
				Region: "US-CAL-CISO",
				APIKey: "test-key",
			},
			CGAPIConfig: &config.CGAPIConfig{
				Endpoint:     "https://api.computegardener.io",
				APIKey:       "cg-test-key",
				Timeout:      10 * time.Second,
				FallbackToEM: false,
			},
		}

		impl, err := Factory(cfg, apiClient)
		if err != nil {
			t.Fatalf("Factory() error = %v", err)
		}

		_, ok := impl.(*cgapiImplementation)
		if !ok {
			t.Errorf("Factory() did not return *cgapiImplementation for cg-api provider")
		}
	})
}

func TestCGAPIFallback(t *testing.T) {
	// This test would require mocking the CG API client to simulate failures
	// For now, we'll just verify the fallback is created when configured
	cfg := &config.CarbonConfig{
		Enabled:  true,
		Provider: "cg-api",
		APIConfig: config.ElectricityMapsAPIConfig{
			Region: "US-CAL-CISO",
			APIKey: "test-key",
			URL:    "https://test.com",
		},
		CGAPIConfig: &config.CGAPIConfig{
			Endpoint:     "https://api.computegardener.io",
			APIKey:       "cg-test-key",
			Timeout:      10 * time.Second,
			FallbackToEM: true,
		},
	}

	apiConfig := config.ElectricityMapsAPIConfig{
		URL:    "https://test-api.example.com/",
		APIKey: "test-key",
		Region: "test-region",
	}
	cacheConfig := config.APICacheConfig{
		CacheTTL:    time.Minute,
		MaxCacheAge: time.Minute * 10,
		Timeout:     time.Second * 5,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
		RateLimit:   10,
	}
	apiClient := api.NewClient(apiConfig, cacheConfig)

	impl, err := Factory(cfg, apiClient)
	if err != nil {
		t.Fatalf("Factory() error = %v", err)
	}

	cgImpl, ok := impl.(*cgapiImplementation)
	if !ok {
		t.Fatal("Factory() did not return *cgapiImplementation")
	}

	// Verify fallback was created
	if cgImpl.fallback == nil {
		t.Error("Factory() did not create fallback implementation when fallbackToEM is true")
	}
}

func TestNew_BackwardsCompatibility(t *testing.T) {
	// Verify that the deprecated New() function still works
	cfg := &config.CarbonConfig{
		Enabled:  true,
		Provider: "electricity-maps-api",
		APIConfig: config.ElectricityMapsAPIConfig{
			Region: "US-CAL-CISO",
			APIKey: "test-key",
			URL:    "https://test.com",
		},
	}

	apiConfig := config.ElectricityMapsAPIConfig{
		URL:    "https://test-api.example.com/",
		APIKey: "test-key",
		Region: "test-region",
	}
	cacheConfig := config.APICacheConfig{
		CacheTTL:    time.Minute,
		MaxCacheAge: time.Minute * 10,
		Timeout:     time.Second * 5,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
		RateLimit:   10,
	}
	apiClient := api.NewClient(apiConfig, cacheConfig)

	impl := New(cfg, apiClient)
	if impl == nil {
		t.Fatal("New() returned nil")
	}

	// Should be able to call interface methods
	ctx := context.Background()
	_, err := impl.GetCurrentIntensity(ctx)
	// Error is expected since we don't have a real API, but the method should exist
	if err == nil {
		// This is actually unexpected in a test environment without real API
		// but we're just testing that the interface is implemented
	}
}
