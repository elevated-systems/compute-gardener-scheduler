package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
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

// NewClient creates a new API client
func NewClient(apiCfg config.ElectricityMapsAPIConfig, cacheCfg config.APICacheConfig, opts ...ClientOption) *Client {
	client := &Client{
		apiConfig:   apiCfg,
		cacheConfig: cacheCfg,
		httpClient: &http.Client{
			Timeout: cacheCfg.Timeout,
		},
		rateLimiter: time.NewTicker(time.Second / time.Duration(cacheCfg.RateLimit)),
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

func (c *Client) doRequest(ctx context.Context, region string) (*ElectricityData, error) {
	// Validate inputs
	if region == "" {
		return nil, fmt.Errorf("region cannot be empty")
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiConfig.URL+region, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Log the exact URL being used for debugging
	klog.V(2).InfoS("Making carbon API request", 
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
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		// Continue processing
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("rate limit exceeded")
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("invalid API key")
	case http.StatusNotFound:
		return nil, fmt.Errorf("region not found: %s", region)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

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
