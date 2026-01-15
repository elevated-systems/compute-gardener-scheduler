package cgapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// Client handles HTTP communication with the Compute Gardener API
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
}

// IntensityResponse represents the response from the CG API intensity endpoint
type IntensityResponse struct {
	Zone            string    `json:"zone"`
	CarbonIntensity float64   `json:"carbonIntensity"`
	Unit            string    `json:"unit"`
	Source          string    `json:"source"`
	Timestamp       time.Time `json:"timestamp"`
	Cached          bool      `json:"cached"`
	DataStatus      string    `json:"dataStatus,omitempty"` // "real" or "estimated"
}

// NewClient creates a new CG API client with the provided configuration
func NewClient(cfg config.CGAPIConfig) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("CG API endpoint is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		endpoint:   cfg.Endpoint,
		apiKey:     cfg.APIKey,
	}, nil
}

// GetIntensity fetches the current carbon intensity for the specified region
func (c *Client) GetIntensity(ctx context.Context, region string) (*IntensityResponse, error) {
	url := fmt.Sprintf("%s/v1/intensity/%s", c.endpoint, region)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if API key is provided
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "compute-gardener-scheduler/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result IntensityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
