package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
)

// Client handles interactions with the electricity data API
type Client struct {
	config      config.APIConfig
	httpClient  *http.Client
	rateLimiter *time.Ticker
}

// ElectricityData represents the response from the API
type ElectricityData struct {
	CarbonIntensity float64   `json:"carbonIntensity"`
	Timestamp       time.Time `json:"timestamp"`
}

// NewClient creates a new API client
func NewClient(cfg config.APIConfig) *Client {
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		rateLimiter: time.NewTicker(time.Second / time.Duration(cfg.RateLimit)),
	}
}

// GetCarbonIntensity fetches carbon intensity data with retries and circuit breaking
func (c *Client) GetCarbonIntensity(ctx context.Context, region string) (*ElectricityData, error) {
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %v", ctx.Err())
		case <-c.rateLimiter.C:
			data, err := c.doRequest(ctx, region)
			if err == nil {
				return data, nil
			}
			lastErr = err
			klog.V(2).InfoS("API request failed, retrying",
				"attempt", attempt+1,
				"maxRetries", c.config.MaxRetries,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.ElectricityMapURL+region, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add headers
	req.Header.Set("auth-token", c.config.ElectricityMapKey)
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
	backoff := c.config.RetryDelay * time.Duration(1<<uint(attempt))
	maxBackoff := 1 * time.Minute
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Add jitter (±20%)
	jitter := time.Duration(float64(backoff) * (0.8 + 0.4*float64(time.Now().UnixNano()%100)/100.0))
	return jitter
}

// Close cleans up client resources
func (c *Client) Close() {
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
	}
}
