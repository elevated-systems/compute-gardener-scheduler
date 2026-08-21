package carbon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// MockHTTPClient implements HTTPClient for testing
type MockHTTPClient struct {
	shouldFail   bool
	statusCode   int
	responseBody string
	forecastBody string
	forecastFail bool
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Forecast endpoint takes precedence when it's a forecast request
	if strings.Contains(req.URL.String(), "forecast") {
		if m.forecastFail {
			return nil, errors.New("simulated forecast network error")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(m.forecastBody)),
		}, nil
	}

	if m.shouldFail {
		return &http.Response{
			StatusCode: m.statusCode,
			Body:       http.NoBody,
		}, nil
	}

	// For a successful test, we would need to implement a proper response body
	// This is simplified for the test
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
	}, nil
}

// mockCache implements the CacheInterface for testing
type mockCache struct {
	data map[string]*api.ElectricityData
}

func newMockCache() *mockCache {
	return &mockCache{
		data: make(map[string]*api.ElectricityData),
	}
}

func (c *mockCache) Get(region string) (*api.ElectricityData, bool) {
	data, ok := c.data[region]
	return data, ok
}

func (c *mockCache) Set(region string, data *api.ElectricityData) {
	c.data[region] = data
}

// Helper function to create a test API client
func createTestAPIClient(intensity float64, shouldFail bool) *api.Client {
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

	// Create mock cache with predefined data
	cache := newMockCache()
	if !shouldFail {
		cache.Set("test-region", &api.ElectricityData{
			CarbonIntensity: intensity,
			Timestamp:       time.Now(),
		})
	}

	// Create client with mock cache and client
	return api.NewClient(apiConfig, cacheConfig,
		api.WithCache(cache),
		api.WithHTTPClient(&MockHTTPClient{shouldFail: shouldFail}),
	)
}

func TestNew(t *testing.T) {
	cfg := &config.CarbonConfig{
		APIConfig: config.ElectricityMapsAPIConfig{
			Region: "test-region",
			APIKey: "test-key",
		},
	}
	apiClient := createTestAPIClient(100.0, false)

	impl := New(cfg, apiClient)
	if impl == nil {
		t.Fatal("New() returned nil")
	}

	// Verify it's the right type
	_, ok := impl.(*carbonImpl)
	if !ok {
		t.Errorf("New() did not return a *carbonImpl")
	}
}

func TestGetCurrentIntensity(t *testing.T) {
	tests := []struct {
		name            string
		carbonIntensity float64
		apiError        bool
		wantIntensity   float64
		wantErr         bool
	}{
		{
			name:            "successful response",
			carbonIntensity: 100.5,
			apiError:        false,
			wantIntensity:   100.5,
			wantErr:         false,
		},
		{
			name:            "API error",
			carbonIntensity: 0,
			apiError:        true,
			wantIntensity:   0,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cfg := &config.CarbonConfig{
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "test-region",
					APIKey: "test-key",
				},
			}
			apiClient := createTestAPIClient(tt.carbonIntensity, tt.apiError)
			impl := New(cfg, apiClient)

			// Execute
			intensity, err := impl.GetCurrentIntensity(context.Background())

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCurrentIntensity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && intensity != tt.wantIntensity {
				t.Errorf("GetCurrentIntensity() intensity = %v, want %v", intensity, tt.wantIntensity)
			}
		})
	}
}

func TestGetForecast(t *testing.T) {
	validForecast := `{
		"zone": "test-region",
		"data": [
			{"datetime": "2023-01-01T12:00:00Z", "carbonIntensity": 150.5},
			{"datetime": "2023-01-01T13:00:00Z", "carbonIntensity": 120.3}
		],
		"temporalGranularity": "hourly"
	}`

	invalidForecast := "not-json"

	newImpl := func(httpClient *MockHTTPClient) Implementation {
		cfg := &config.CarbonConfig{
			APIConfig: config.ElectricityMapsAPIConfig{
				Region: "test-region",
				APIKey: "test-key",
			},
		}
		apiConfig := config.ElectricityMapsAPIConfig{
			URL:    "https://test-api.example.com/v3/carbon-intensity/",
			APIKey: "test-key",
			Region: "test-region",
		}
		cacheConfig := config.APICacheConfig{
			CacheTTL:    time.Minute,
			MaxCacheAge: time.Minute * 10,
			Timeout:     time.Second * 5,
			MaxRetries:  1,
			RetryDelay:  time.Millisecond,
			RateLimit:   10,
		}
		client := api.NewClient(apiConfig, cacheConfig,
			api.WithHTTPClient(httpClient),
		)
		return New(cfg, client)
	}

	t.Run("successful forecast", func(t *testing.T) {
		impl := newImpl(&MockHTTPClient{forecastBody: validForecast})
		forecast, err := impl.GetForecast(context.Background(), 2)
		if err != nil {
			t.Fatalf("GetForecast() unexpected error: %v", err)
		}
		if forecast.Zone != "test-region" {
			t.Errorf("GetForecast() zone = %q, want %q", forecast.Zone, "test-region")
		}
		if len(forecast.Data) != 2 {
			t.Errorf("GetForecast() data length = %d, want 2", len(forecast.Data))
		}
	})

	t.Run("API error", func(t *testing.T) {
		impl := newImpl(&MockHTTPClient{forecastFail: true})
		_, err := impl.GetForecast(context.Background(), 2)
		if err == nil {
			t.Fatal("GetForecast() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to get carbon intensity forecast") {
			t.Errorf("GetForecast() error = %q, want it to contain %q",
				err.Error(), "failed to get carbon intensity forecast")
		}
	})

	t.Run("invalid forecast data", func(t *testing.T) {
		impl := newImpl(&MockHTTPClient{forecastBody: invalidForecast})
		_, err := impl.GetForecast(context.Background(), 2)
		if err == nil {
			t.Fatal("GetForecast() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to decode forecast response") {
			t.Errorf("GetForecast() error = %q, want it to contain %q",
				err.Error(), "failed to decode forecast response")
		}
	})
}

func TestCheckIntensityConstraints(t *testing.T) {
	tests := []struct {
		name            string
		carbonIntensity float64
		threshold       float64
		apiError        bool
		wantStatus      framework.Code
	}{
		{
			name:            "below threshold",
			carbonIntensity: 100.0,
			threshold:       150.0,
			apiError:        false,
			wantStatus:      framework.Success,
		},
		{
			name:            "exceeds threshold",
			carbonIntensity: 200.0,
			threshold:       150.0,
			apiError:        false,
			wantStatus:      framework.Unschedulable,
		},
		{
			name:            "API error",
			carbonIntensity: 0.0,
			threshold:       100.0,
			apiError:        true,
			wantStatus:      framework.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cfg := &config.CarbonConfig{
				APIConfig: config.ElectricityMapsAPIConfig{
					Region: "test-region",
					APIKey: "test-key",
				},
			}
			apiClient := createTestAPIClient(tt.carbonIntensity, tt.apiError)
			impl := New(cfg, apiClient)

			// Execute
			status := impl.CheckIntensityConstraints(context.Background(), tt.threshold)

			// Verify
			if status.Code() != tt.wantStatus {
				t.Errorf("CheckIntensityConstraints() status code = %v, want %v", status.Code(), tt.wantStatus)
			}

			// Additional checks based on status code
			switch tt.wantStatus {
			case framework.Unschedulable:
				if status.Message() == "" {
					t.Error("CheckIntensityConstraints() expected non-empty error message for Unschedulable status")
				}
			case framework.Error:
				if status.Message() == "" {
					t.Error("CheckIntensityConstraints() expected non-empty error message for Error status")
				}
			}
		})
	}
}
