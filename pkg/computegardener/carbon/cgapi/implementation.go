package cgapi

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// IntensityData contains carbon intensity along with data quality information
type IntensityData struct {
	Value      float64
	DataStatus string // "real" or "estimated"
}

// Provider provides carbon intensity data using the Compute Gardener API
type Provider struct {
	client *Client
	config config.CGAPIConfig
	region string
}

// NewProvider creates a new CG API carbon provider
func NewProvider(cfg config.CGAPIConfig, region string) (*Provider, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create CG API client: %w", err)
	}

	return &Provider{
		client: client,
		config: cfg,
		region: region,
	}, nil
}

// GetCurrentIntensity returns the current carbon intensity for the configured region
func (p *Provider) GetCurrentIntensity(ctx context.Context) (float64, error) {
	data, err := p.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		return 0, err
	}
	return data.Value, nil
}

// GetCurrentIntensityWithStatus returns carbon intensity along with data quality info
func (p *Provider) GetCurrentIntensityWithStatus(ctx context.Context) (*IntensityData, error) {
	klog.V(3).InfoS("Fetching carbon intensity from CG API",
		"region", p.region,
		"endpoint", p.config.Endpoint)

	intensity, err := p.client.GetIntensity(ctx, p.region)
	if err != nil {
		return nil, fmt.Errorf("CG API request failed: %w", err)
	}

	klog.V(3).InfoS("Got carbon intensity from CG API",
		"region", p.region,
		"intensity", intensity.CarbonIntensity,
		"source", intensity.Source,
		"cached", intensity.Cached,
		"dataStatus", intensity.DataStatus)

	// Determine data status - default to "real" if not provided
	dataStatus := intensity.DataStatus
	if dataStatus == "" {
		dataStatus = "real"
	}

	return &IntensityData{
		Value:      intensity.CarbonIntensity,
		DataStatus: dataStatus,
	}, nil
}
