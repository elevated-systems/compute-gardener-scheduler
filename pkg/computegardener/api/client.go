package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"k8s.io/klog/v2"
)

// HTTPClient interface allows mocking http.Client in tests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client handles interactions with the electricity data API
type Client struct {
	apiConfig   config.ElectricityMapsAPIConfig
	cacheConfig config.APICacheConfig
	httpClient  HTTPClient
	rateLimiter *time.Ticker
	cache       CacheInterface // Added cache interface
}

// ElectricityData represents the response from the API
type ElectricityData struct {
	CarbonIntensity float64   `json:"carbonIntensity"`
	Timestamp       time.Time `json:"timestamp"`
}

// ForecastData represents a single forecast data point
type ForecastData struct {
	Datetime        time.Time `json:"datetime"`
	CarbonIntensity float64   `json:"carbonIntensity"`
}

// ElectricityForecast represents the forecast response from the API
type ElectricityForecast struct {
	Zone                string         `json:"zone"`
	Data                []ForecastData `json:"data"`
	TemporalGranularity string         `json:"temporalGranularity"`
}

// ClientOption allows customizing the client
type ClientOption func(*Client)

// WithHTTPClient allows injecting a custom HTTP client
func WithHTTPClient(client HTTPClient) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithCache allows injecting a custom cache
type CacheInterface interface {
	Get(region string) (*ElectricityData, bool)
	Set(region string, data *ElectricityData)
}

// WithCache adds a cache to the client
func WithCache(cache CacheInterface) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

// Helper function to avoid divide by zero
func ensureNonZero(n int) int {
	if n <= 0 {
		return 1 // Default to 1 request per second if RateLimit is not set
	}
	return n
}

// NewClient creates a new API client
func NewClient(apiCfg config.ElectricityMapsAPIConfig, cacheCfg config.APICacheConfig, opts ...ClientOption) *Client {
	client := &Client{
		apiConfig:   apiCfg,
		cacheConfig: cacheCfg,
		httpClient: &http.Client{
			Timeout: cacheCfg.Timeout,
		},
		rateLimiter: time.NewTicker(time.Second / time.Duration(ensureNonZero(cacheCfg.RateLimit))),
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	return client
}

// GetCarbonIntensity fetches carbon intensity data with retries and circuit breaking
func (c *Client) GetCarbonIntensity(ctx context.Context, region string) (*ElectricityData, error) {
	// First check the cache if available
	if c.cache != nil {
		if data, fresh := c.cache.Get(region); fresh {
			klog.V(2).InfoS("Using cached carbon intensity data",
				"region", region,
				"intensity", data.CarbonIntensity)
			return data, nil
		}
	}

	// Cache miss or no cache configured, fetch from API
	var lastErr error
	for attempt := 0; attempt <= c.cacheConfig.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %v", ctx.Err())
		case <-c.rateLimiter.C:
			data, err := c.doRequest(ctx, region)
			if err == nil {
				// Store successful result in cache if available
				if c.cache != nil {
					c.cache.Set(region, data)
					klog.V(2).InfoS("Stored carbon intensity data in cache",
						"region", region,
						"intensity", data.CarbonIntensity)
				}
				return data, nil
			}
			lastErr = err
			klog.V(2).InfoS("API request failed, retrying",
				"attempt", attempt+1,
				"maxRetries", c.cacheConfig.MaxRetries,
				"error", err)

			// Calculate backoff duration
			backoff := c.getBackoffDuration(attempt)

			// Wait with context awareness
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, fmt.Errorf("context cancelled during backoff: %v", ctx.Err())
			case <-timer.C:
				continue
			}
		}
	}
	return nil, fmt.Errorf("all retries failed: %v", lastErr)
}

// GetCarbonIntensityForecast fetches carbon intensity forecast data with retries and circuit breaking
func (c *Client) GetCarbonIntensityForecast(ctx context.Context, region string, horizonHours int) (*ElectricityForecast, error) {
	// Validate inputs
	if region == "" {
		return nil, fmt.Errorf("region cannot be empty")
	}
	if horizonHours <= 0 || horizonHours > 72 {
		return nil, fmt.Errorf("horizonHours must be between 1 and 72, got %d", horizonHours)
	}

	// Forecast data is less frequently cached since it changes less often
	// Cache miss or no cache configured, fetch from API
	var lastErr error
	for attempt := 0; attempt <= c.cacheConfig.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %v", ctx.Err())
		case <-c.rateLimiter.C:
			data, err := c.doForecastRequest(ctx, region, horizonHours)
			if err == nil {
				klog.V(2).InfoS("Successfully fetched carbon intensity forecast",
					"region", region,
					"horizonHours", horizonHours,
					"dataPoints", len(data.Data))
				return data, nil
			}
			lastErr = err
			klog.V(2).InfoS("Forecast API request failed, retrying",
				"attempt", attempt+1,
				"maxRetries", c.cacheConfig.MaxRetries,
				"error", err)

			// Calculate backoff duration
			backoff := c.getBackoffDuration(attempt)

			// Wait with context awareness
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, fmt.Errorf("context cancelled during backoff: %v", ctx.Err())
			case <-timer.C:
				continue
			}
		}
	}
	return nil, fmt.Errorf("all forecast retries failed: %v", lastErr)
}

func (c *Client) doRequest(ctx context.Context, region string) (*ElectricityData, error) {
	// Validate inputs
	if region == "" {
		return nil, fmt.Errorf("region cannot be empty")
	}

	// Build URL for latest endpoint
	url := c.buildURL("latest", region, nil)
	
	// Make HTTP request using common helper
	resp, err := c.doHTTPRequest(ctx, url, "carbon API", region)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Decode response
	var data ElectricityData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Validate response data
	if data.CarbonIntensity < 0 {
		return nil, fmt.Errorf("invalid carbon intensity value: %f", data.CarbonIntensity)
	}

	// Set timestamp if not provided by API
	if data.Timestamp.IsZero() {
		data.Timestamp = time.Now()
	}

	return &data, nil
}

func (c *Client) doForecastRequest(ctx context.Context, region string, horizonHours int) (*ElectricityForecast, error) {
	// Build forecast URL with query parameters
	params := map[string]string{
		"zone":         region,
		"horizonHours": fmt.Sprintf("%d", horizonHours),
	}
	url := c.buildURL("forecast", "", params)
	
	// Make HTTP request using common helper
	resp, err := c.doHTTPRequest(ctx, url, "carbon forecast API", region)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Decode response
	var forecast ElectricityForecast
	if err := json.NewDecoder(resp.Body).Decode(&forecast); err != nil {
		return nil, fmt.Errorf("failed to decode forecast response: %v", err)
	}

	// Validate response data
	if len(forecast.Data) == 0 {
		return nil, fmt.Errorf("no forecast data returned")
	}

	for i, point := range forecast.Data {
		if point.CarbonIntensity < 0 {
			return nil, fmt.Errorf("invalid carbon intensity value at index %d: %f", i, point.CarbonIntensity)
		}
	}

	return &forecast, nil
}

// buildURL constructs the appropriate URL for different API endpoints
func (c *Client) buildURL(endpoint, region string, params map[string]string) string {
	baseURL := c.apiConfig.URL
	
	// Remove /latest suffix if present to get clean base URL
	if strings.HasSuffix(baseURL, "/latest") {
		baseURL = strings.TrimSuffix(baseURL, "/latest")
	}
	
	// Remove region suffix if present to get clean base URL
	if region != "" && strings.HasSuffix(baseURL, region) {
		baseURL = strings.TrimSuffix(baseURL, region)
	}
	
	// Ensure baseURL ends with /
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	
	// Build URL based on endpoint
	var fullURL string
	if endpoint == "latest" && region != "" {
		fullURL = baseURL + "latest/" + region
	} else if endpoint == "forecast" {
		fullURL = baseURL + "forecast"
	} else {
		fullURL = baseURL + endpoint
	}
	
	// Add query parameters if provided
	if len(params) > 0 {
		u, err := url.Parse(fullURL)
		if err == nil {
			q := u.Query()
			for key, value := range params {
				q.Set(key, value)
			}
			u.RawQuery = q.Encode()
			fullURL = u.String()
		}
	}
	
	return fullURL
}

// doHTTPRequest handles common HTTP request mechanics and status code validation
func (c *Client) doHTTPRequest(ctx context.Context, url, requestType, region string) (*http.Response, error) {
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s request: %v", requestType, err)
	}

	// Log the exact URL being used for debugging
	klog.V(2).InfoS("Making API request",
		"type", requestType,
		"url", req.URL.String(),
		"region", region,
		"hasApiKey", c.apiConfig.APIKey != "")

	// Add headers
	req.Header.Set("auth-token", c.apiConfig.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %v", requestType, err)
	}

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusTooManyRequests:
		resp.Body.Close()
		return nil, fmt.Errorf("rate limit exceeded")
	case http.StatusUnauthorized:
		resp.Body.Close()
		return nil, fmt.Errorf("invalid API key")
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, fmt.Errorf("region not found: %s", region)
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func (c *Client) getBackoffDuration(attempt int) time.Duration {
	// Exponential backoff with jitter
	backoff := c.cacheConfig.RetryDelay * time.Duration(1<<uint(attempt))
	maxBackoff := 1 * time.Minute
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Add jitter (±20%)
	jitter := time.Duration(float64(backoff) * (0.8 + 0.4*float64(time.Now().UnixNano()%100)/100.0))
	return jitter
}

// GetURL returns the base URL used for API requests
func (c *Client) GetURL() string {
	return c.apiConfig.URL
}

// Close cleans up client resources
func (c *Client) Close() {
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
	}
}
