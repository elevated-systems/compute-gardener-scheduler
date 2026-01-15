package cgapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.CGAPIConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.CGAPIConfig{
				Endpoint: "https://api.example.com",
				APIKey:   "test-key",
				Timeout:  10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			cfg: config.CGAPIConfig{
				APIKey:  "test-key",
				Timeout: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "default timeout",
			cfg: config.CGAPIConfig{
				Endpoint: "https://api.example.com",
				APIKey:   "test-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.cfg, "test-region")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && provider == nil {
				t.Error("NewProvider() returned nil provider without error")
			}
		})
	}
}

func TestClient_GetIntensity(t *testing.T) {
	tests := []struct {
		name            string
		region          string
		mockResponse    IntensityResponse
		mockStatusCode  int
		wantErr         bool
		wantIntensity   float64
		wantDataStatus  string
		requireAuthKey  bool
	}{
		{
			name:   "successful response",
			region: "US-CAL-CISO",
			mockResponse: IntensityResponse{
				Zone:            "US-CAL-CISO",
				CarbonIntensity: 123.45,
				Unit:            "gCO2eq/kWh",
				Source:          "electricity-maps",
				Timestamp:       time.Now(),
				Cached:          false,
				DataStatus:      "real",
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantIntensity:  123.45,
			wantDataStatus: "real",
		},
		{
			name:   "cached response",
			region: "EU-DE",
			mockResponse: IntensityResponse{
				Zone:            "EU-DE",
				CarbonIntensity: 250.0,
				Unit:            "gCO2eq/kWh",
				Source:          "cache",
				Timestamp:       time.Now().Add(-5 * time.Minute),
				Cached:          true,
				DataStatus:      "estimated",
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantIntensity:  250.0,
			wantDataStatus: "estimated",
		},
		{
			name:           "server error",
			region:         "US-CAL-CISO",
			mockStatusCode: http.StatusInternalServerError,
			wantErr:        true,
		},
		{
			name:           "not found",
			region:         "INVALID-REGION",
			mockStatusCode: http.StatusNotFound,
			wantErr:        true,
		},
		{
			name:   "with auth key",
			region: "US-CAL-CISO",
			mockResponse: IntensityResponse{
				Zone:            "US-CAL-CISO",
				CarbonIntensity: 100.0,
				Unit:            "gCO2eq/kWh",
				Source:          "electricity-maps",
				Timestamp:       time.Now(),
				Cached:          false,
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantIntensity:  100.0,
			requireAuthKey: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request headers
				if r.Header.Get("Accept") != "application/json" {
					t.Error("Missing or incorrect Accept header")
				}
				if r.Header.Get("User-Agent") != "compute-gardener-scheduler/1.0" {
					t.Error("Missing or incorrect User-Agent header")
				}

				// Verify auth header if required
				if tt.requireAuthKey {
					authHeader := r.Header.Get("Authorization")
					if authHeader != "Bearer test-api-key" {
						t.Errorf("Expected Authorization header 'Bearer test-api-key', got '%s'", authHeader)
					}
				}

				// Set response status code
				w.WriteHeader(tt.mockStatusCode)

				// Write response body if successful
				if tt.mockStatusCode == http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			// Create client
			cfg := config.CGAPIConfig{
				Endpoint: server.URL,
				Timeout:  5 * time.Second,
			}
			if tt.requireAuthKey {
				cfg.APIKey = "test-api-key"
			}

			client, err := NewClient(cfg)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			// Execute
			response, err := client.GetIntensity(context.Background(), tt.region)

			// Verify error
			if (err != nil) != tt.wantErr {
				t.Errorf("GetIntensity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify response if no error expected
			if !tt.wantErr {
				if response == nil {
					t.Fatal("GetIntensity() returned nil response without error")
				}
				if response.CarbonIntensity != tt.wantIntensity {
					t.Errorf("GetIntensity() intensity = %v, want %v", response.CarbonIntensity, tt.wantIntensity)
				}
				if tt.wantDataStatus != "" && response.DataStatus != tt.wantDataStatus {
					t.Errorf("GetIntensity() dataStatus = %v, want %v", response.DataStatus, tt.wantDataStatus)
				}
			}
		})
	}
}

func TestProvider_GetCurrentIntensity(t *testing.T) {
	// Create a test server
	intensity := 123.45
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntensityResponse{
			Zone:            "US-CAL-CISO",
			CarbonIntensity: intensity,
			Unit:            "gCO2eq/kWh",
			Source:          "electricity-maps",
			Timestamp:       time.Now(),
			DataStatus:      "real",
		})
	}))
	defer server.Close()

	cfg := config.CGAPIConfig{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Timeout:  5 * time.Second,
	}

	provider, err := NewProvider(cfg, "US-CAL-CISO")
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	// Test GetCurrentIntensity
	result, err := provider.GetCurrentIntensity(context.Background())
	if err != nil {
		t.Errorf("GetCurrentIntensity() error = %v", err)
	}
	if result != intensity {
		t.Errorf("GetCurrentIntensity() = %v, want %v", result, intensity)
	}
}

func TestProvider_GetCurrentIntensityWithStatus(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   IntensityResponse
		wantValue      float64
		wantDataStatus string
	}{
		{
			name: "real data",
			mockResponse: IntensityResponse{
				Zone:            "US-CAL-CISO",
				CarbonIntensity: 100.0,
				DataStatus:      "real",
			},
			wantValue:      100.0,
			wantDataStatus: "real",
		},
		{
			name: "estimated data",
			mockResponse: IntensityResponse{
				Zone:            "US-CAL-CISO",
				CarbonIntensity: 150.0,
				DataStatus:      "estimated",
			},
			wantValue:      150.0,
			wantDataStatus: "estimated",
		},
		{
			name: "missing data status defaults to real",
			mockResponse: IntensityResponse{
				Zone:            "US-CAL-CISO",
				CarbonIntensity: 200.0,
				// DataStatus not set
			},
			wantValue:      200.0,
			wantDataStatus: "real",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			cfg := config.CGAPIConfig{
				Endpoint: server.URL,
				APIKey:   "test-key",
				Timeout:  5 * time.Second,
			}

			provider, err := NewProvider(cfg, "US-CAL-CISO")
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}

			// Execute
			data, err := provider.GetCurrentIntensityWithStatus(context.Background())
			if err != nil {
				t.Errorf("GetCurrentIntensityWithStatus() error = %v", err)
				return
			}

			// Verify
			if data.Value != tt.wantValue {
				t.Errorf("GetCurrentIntensityWithStatus() value = %v, want %v", data.Value, tt.wantValue)
			}
			if data.DataStatus != tt.wantDataStatus {
				t.Errorf("GetCurrentIntensityWithStatus() dataStatus = %v, want %v", data.DataStatus, tt.wantDataStatus)
			}
		})
	}
}

func TestClient_Timeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.CGAPIConfig{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Timeout:  100 * time.Millisecond, // Very short timeout
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx := context.Background()
	_, err = client.GetIntensity(ctx, "US-CAL-CISO")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}
