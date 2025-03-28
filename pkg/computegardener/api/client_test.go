package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// MockHTTPClient is a mock implementation of HTTPClient for testing
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

// Do implements the HTTPClient interface
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("mock http client not implemented")
}

// MockCache is a mock implementation of CacheInterface for testing
type MockCache struct {
	GetFunc func(region string) (*ElectricityData, bool)
	SetFunc func(region string, data *ElectricityData)
}

// Get implements the CacheInterface interface
func (m *MockCache) Get(region string) (*ElectricityData, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(region)
	}
	return nil, false
}

// Set implements the CacheInterface interface
func (m *MockCache) Set(region string, data *ElectricityData) {
	if m.SetFunc != nil {
		m.SetFunc(region, data)
	}
}

func TestNewClient(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  3,
		RetryDelay:  time.Second,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	tests := []struct {
		name             string
		options          []ClientOption
		expectHTTPClient bool
		expectCache      bool
	}{
		{
			name:             "default client",
			options:          []ClientOption{},
			expectHTTPClient: true,
			expectCache:      false,
		},
		{
			name: "with custom HTTP client",
			options: []ClientOption{
				WithHTTPClient(&MockHTTPClient{}),
			},
			expectHTTPClient: true,
			expectCache:      false,
		},
		{
			name: "with custom cache",
			options: []ClientOption{
				WithCache(&MockCache{}),
			},
			expectHTTPClient: true,
			expectCache:      true,
		},
		{
			name: "with both custom HTTP client and cache",
			options: []ClientOption{
				WithHTTPClient(&MockHTTPClient{}),
				WithCache(&MockCache{}),
			},
			expectHTTPClient: true,
			expectCache:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(apiCfg, cacheCfg, tt.options...)

			// Test client configuration
			if client.apiConfig.APIKey != apiCfg.APIKey {
				t.Errorf("Expected API key %s, got %s", apiCfg.APIKey, client.apiConfig.APIKey)
			}

			if client.apiConfig.URL != apiCfg.URL {
				t.Errorf("Expected URL %s, got %s", apiCfg.URL, client.apiConfig.URL)
			}

			if client.httpClient == nil && tt.expectHTTPClient {
				t.Error("Expected HTTP client to be set, got nil")
			}

			if (client.cache != nil) != tt.expectCache {
				t.Errorf("Expected cache to be %v, got %v", tt.expectCache, client.cache != nil)
			}

			if client.rateLimiter == nil {
				t.Error("Expected rate limiter to be set, got nil")
			}
		})
	}
}

func TestEnsureNonZero(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{
			name:     "positive number",
			input:    5,
			expected: 5,
		},
		{
			name:     "zero",
			input:    0,
			expected: 1,
		},
		{
			name:     "negative number",
			input:    -3,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureNonZero(tt.input)
			if result != tt.expected {
				t.Errorf("ensureNonZero(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetCarbonIntensity_CacheHit(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  3,
		RetryDelay:  time.Second,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	expectedData := &ElectricityData{
		CarbonIntensity: 200.5,
		Timestamp:       time.Now(),
	}

	mockCache := &MockCache{
		GetFunc: func(region string) (*ElectricityData, bool) {
			if region == "test-region" {
				return expectedData, true
			}
			return nil, false
		},
	}

	// HTTP client should not be called on cache hit
	mockHTTP := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			t.Error("HTTP client should not be called on cache hit")
			return nil, errors.New("http client should not be called")
		},
	}

	client := NewClient(apiCfg, cacheCfg,
		WithHTTPClient(mockHTTP),
		WithCache(mockCache),
	)

	data, err := client.GetCarbonIntensity(context.Background(), "test-region")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if data.CarbonIntensity != expectedData.CarbonIntensity {
		t.Errorf("Expected carbon intensity %f, got %f", expectedData.CarbonIntensity, data.CarbonIntensity)
	}
}

func TestGetCarbonIntensity_CacheMiss(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  3,
		RetryDelay:  time.Second,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	expectedData := &ElectricityData{
		CarbonIntensity: 300.5,
		Timestamp:       time.Now(),
	}

	var cachedData *ElectricityData
	var cacheSet bool

	mockCache := &MockCache{
		GetFunc: func(region string) (*ElectricityData, bool) {
			return nil, false // Cache miss
		},
		SetFunc: func(region string, data *ElectricityData) {
			cachedData = data
			cacheSet = true
		},
	}

	// Simulate successful HTTP response
	mockHTTP := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Check request headers
			if req.Header.Get("auth-token") != "test-key" {
				t.Errorf("Expected auth-token header to be test-key, got %s", req.Header.Get("auth-token"))
			}

			// Create a response with valid JSON
			jsonResponse := `{"carbonIntensity": 300.5, "timestamp": "2023-01-01T12:00:00Z"}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(jsonResponse)),
			}, nil
		},
	}

	client := NewClient(apiCfg, cacheCfg,
		WithHTTPClient(mockHTTP),
		WithCache(mockCache),
	)

	data, err := client.GetCarbonIntensity(context.Background(), "test-region")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if data.CarbonIntensity != expectedData.CarbonIntensity {
		t.Errorf("Expected carbon intensity %f, got %f", expectedData.CarbonIntensity, data.CarbonIntensity)
	}

	// Verify data was cached
	if !cacheSet {
		t.Error("Expected data to be cached, but Set was not called")
	}

	if cachedData == nil || cachedData.CarbonIntensity != expectedData.CarbonIntensity {
		t.Errorf("Expected cached data to have intensity %f", expectedData.CarbonIntensity)
	}
}

func TestGetCarbonIntensity_HTTPError(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,                // Set to 1 to speed up the test
		RetryDelay:  time.Microsecond, // Very short delay to speed up test
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	mockCache := &MockCache{
		GetFunc: func(region string) (*ElectricityData, bool) {
			return nil, false // Cache miss
		},
	}

	// Simulate HTTP error
	mockHTTP := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("simulated network error")
		},
	}

	client := NewClient(apiCfg, cacheCfg,
		WithHTTPClient(mockHTTP),
		WithCache(mockCache),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := client.GetCarbonIntensity(ctx, "test-region")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "all retries failed") {
		t.Errorf("Expected 'all retries failed' error, got %v", err)
	}
}

func TestGetCarbonIntensity_InvalidResponse(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,
		RetryDelay:  time.Microsecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	mockCache := &MockCache{
		GetFunc: func(region string) (*ElectricityData, bool) {
			return nil, false // Cache miss
		},
	}

	// Simulate invalid JSON response
	mockHTTP := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("not json")),
			}, nil
		},
	}

	client := NewClient(apiCfg, cacheCfg,
		WithHTTPClient(mockHTTP),
		WithCache(mockCache),
	)

	_, err := client.GetCarbonIntensity(context.Background(), "test-region")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to decode") {
		t.Errorf("Expected 'failed to decode' error, got %v", err)
	}
}

func TestGetCarbonIntensity_HTTPStatusCodes(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,
		RetryDelay:  time.Microsecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	tests := []struct {
		name       string
		statusCode int
		errorMsg   string
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			errorMsg:   "invalid API key",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			errorMsg:   "region not found",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			errorMsg:   "rate limit exceeded",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			errorMsg:   "unexpected status code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCache := &MockCache{
				GetFunc: func(region string) (*ElectricityData, bool) {
					return nil, false // Cache miss
				},
			}

			mockHTTP := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.statusCode,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				},
			}

			client := NewClient(apiCfg, cacheCfg,
				WithHTTPClient(mockHTTP),
				WithCache(mockCache),
			)

			_, err := client.GetCarbonIntensity(context.Background(), "test-region")
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error containing '%s', got %v", tt.errorMsg, err)
			}
		})
	}
}

func TestGetCarbonIntensity_InvalidRegion(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,
		RetryDelay:  time.Microsecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	client := NewClient(apiCfg, cacheCfg)

	_, err := client.GetCarbonIntensity(context.Background(), "")
	if err == nil {
		t.Fatal("Expected error for empty region, got nil")
	}

	if !strings.Contains(err.Error(), "region cannot be empty") {
		t.Errorf("Expected 'region cannot be empty' error, got %v", err)
	}
}

func TestGetURL(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,
		RetryDelay:  time.Microsecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	client := NewClient(apiCfg, cacheCfg)

	url := client.GetURL()
	if url != apiCfg.URL {
		t.Errorf("Expected URL %s, got %s", apiCfg.URL, url)
	}
}

func TestClose(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  1,
		RetryDelay:  time.Microsecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	client := NewClient(apiCfg, cacheCfg)

	// No way to easily test the ticker stopped, but we can at least
	// ensure Close() doesn't panic
	client.Close()
}

func TestGetBackoffDuration(t *testing.T) {
	apiCfg := config.ElectricityMapsAPIConfig{
		APIKey: "test-key",
		URL:    "https://example.com/",
		Region: "test-region",
	}

	cacheCfg := config.APICacheConfig{
		Timeout:     10 * time.Second,
		MaxRetries:  3,
		RetryDelay:  100 * time.Millisecond,
		RateLimit:   10,
		CacheTTL:    30 * time.Minute,
		MaxCacheAge: 24 * time.Hour,
	}

	client := NewClient(apiCfg, cacheCfg)

	// Test exponential backoff
	backoff0 := client.getBackoffDuration(0)
	backoff1 := client.getBackoffDuration(1)
	backoff2 := client.getBackoffDuration(2)

	if backoff1 <= backoff0 {
		t.Errorf("Expected exponential backoff: backoff1 > backoff0, got %v <= %v", backoff1, backoff0)
	}

	if backoff2 <= backoff1 {
		t.Errorf("Expected exponential backoff: backoff2 > backoff1, got %v <= %v", backoff2, backoff1)
	}

	// Test max backoff limit
	highAttempt := 20 // This should hit the max backoff
	maxBackoff := client.getBackoffDuration(highAttempt)

	// The max backoff is 1 minute according to the code
	if maxBackoff > time.Minute*2 {
		t.Errorf("Expected max backoff near 1 minute, got %v", maxBackoff)
	}
}
