package almanac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// HTTPClient interface allows mocking http.Client in tests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client handles interactions with the compute-gardener-almanac scoring API
type Client struct {
	baseURL    string
	httpClient HTTPClient
	timeout    time.Duration
}

// ClientOption allows customizing the client
type ClientOption func(*Client)

// WithHTTPClient allows injecting a custom HTTP client
func WithHTTPClient(client HTTPClient) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithTimeout sets the request timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// NewClient creates a new almanac scoring API client
func NewClient(baseURL string, opts ...ClientOption) *Client {
	client := &Client{
		baseURL: baseURL,
		timeout: 10 * time.Second,
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Create default HTTP client if none provided
	if client.httpClient == nil {
		client.httpClient = &http.Client{
			Timeout: client.timeout,
		}
	}

	return client
}

// GetScore fetches a carbon-cost optimization score for the given parameters
func (c *Client) GetScore(ctx context.Context, req ScoreRequest) (*ScoreResult, error) {
	// Validate request
	if err := c.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid scoring request: %w", err)
	}

	// Marshal request body
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/v1/score", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	klog.V(3).InfoS("Making almanac scoring API request",
		"url", url,
		"provider", req.Provider,
		"region", req.Region,
		"zone", req.Zone,
		"instanceType", req.InstanceType,
		"weights", req.Weights)

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("scoring API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status
	if resp.StatusCode != http.StatusOK {
		// Try to decode error response
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			return nil, fmt.Errorf("scoring API error (status %d): %s - %s",
				resp.StatusCode, errResp.Error, errResp.Message)
		}
		return nil, fmt.Errorf("scoring API returned status %d", resp.StatusCode)
	}

	// Decode response
	var scoringResp ScoreResult
	if err := json.NewDecoder(resp.Body).Decode(&scoringResp); err != nil {
		return nil, fmt.Errorf("failed to decode scoring response: %w", err)
	}

	klog.V(2).InfoS("Received scoring result",
		"provider", req.Provider,
		"region", req.Region,
		"zone", req.Zone,
		"score", scoringResp.OptimizationScore,
		"recommendation", scoringResp.Recommendation,
		"carbonScore", scoringResp.Components.CarbonScore,
		"priceScore", scoringResp.Components.PriceScore)

	return &scoringResp, nil
}

// validateRequest validates the scoring request parameters
func (c *Client) validateRequest(req ScoreRequest) error {
	// Either zone OR (provider + region) must be provided
	if req.Zone == "" && (req.Provider == "" || req.Region == "") {
		return fmt.Errorf("either 'zone' or ('provider' and 'region') must be provided")
	}

	// Weights are required
	if req.Weights == nil || len(req.Weights) == 0 {
		return fmt.Errorf("weights are required and must be non-empty")
	}

	// Validate weights sum to approximately 1.0 (with tolerance for floating point)
	weightSum := 0.0
	for _, weight := range req.Weights {
		if weight < 0 || weight > 1 {
			return fmt.Errorf("individual weight must be between 0 and 1, got %.2f", weight)
		}
		weightSum += weight
	}

	if weightSum < 0.99 || weightSum > 1.01 {
		return fmt.Errorf("weights must sum to 1.0, got %.2f", weightSum)
	}

	return nil
}

// GetBaseURL returns the base URL of the scoring API
func (c *Client) GetBaseURL() string {
	return c.baseURL
}
