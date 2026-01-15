package electricitymaps

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// IntensityData contains carbon intensity along with data quality information
type IntensityData struct {
	Value      float64
	DataStatus string // "real" or "estimated"
}

// Provider provides carbon intensity data using the Electricity Maps API
type Provider struct {
	config    *config.CarbonConfig
	apiClient *api.Client
}

// NewProvider creates a new Electricity Maps carbon provider
func NewProvider(cfg *config.CarbonConfig, apiClient *api.Client) *Provider {
	return &Provider{
		config:    cfg,
		apiClient: apiClient,
	}
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
	// Log region used for debugging
	klog.V(3).InfoS("Fetching carbon intensity data from Electricity Maps",
		"region", p.config.APIConfig.Region,
		"apiKey", p.config.APIConfig.APIKey != "")

	// The API client will check cache first and only make a request if needed
	data, err := p.apiClient.GetCarbonIntensity(ctx, p.config.APIConfig.Region)
	if err != nil {
		klog.V(2).InfoS("Failed to get carbon intensity data from Electricity Maps", "error", err)
		return nil, fmt.Errorf("failed to get carbon intensity data: %v", err)
	}

	return &IntensityData{
		Value:      data.CarbonIntensity,
		DataStatus: data.GetDataStatus(),
	}, nil
}
