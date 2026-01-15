package carbon

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon/electricitymaps"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// IntensityData contains carbon intensity along with data quality information
type IntensityData struct {
	Value      float64
	DataStatus string // "real" or "estimated"
}

// IsEstimated returns true if the data is estimated (helper for boolean checks)
func (i *IntensityData) IsEstimated() bool {
	return i.DataStatus == "estimated"
}

// Implementation defines the interface for carbon-aware scheduling
type Implementation interface {
	// GetCurrentIntensity returns the current carbon intensity for the configured region
	GetCurrentIntensity(ctx context.Context) (float64, error)

	// GetCurrentIntensityWithStatus returns carbon intensity along with data quality info
	GetCurrentIntensityWithStatus(ctx context.Context) (*IntensityData, error)

	// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
	CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status
}

// Factory creates carbon implementations based on configuration
func Factory(cfg *config.CarbonConfig, apiClient *api.Client) (Implementation, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	switch cfg.Provider {
	case "electricity-maps-api":
		klog.V(2).InfoS("Creating Electricity Maps carbon implementation",
			"region", cfg.APIConfig.Region)
		return newElectricityMapsImplementation(cfg, apiClient), nil

	case "cg-api":
		if cfg.CGAPIConfig == nil {
			return nil, fmt.Errorf("cgApi configuration required for cg-api provider")
		}

		klog.V(2).InfoS("Creating Compute Gardener API carbon implementation",
			"endpoint", cfg.CGAPIConfig.Endpoint,
			"region", cfg.APIConfig.Region,
			"fallbackToEM", cfg.CGAPIConfig.FallbackToEM)

		// Create Electricity Maps implementation as fallback if configured
		var fallback Implementation
		if cfg.CGAPIConfig.FallbackToEM {
			fallback = newElectricityMapsImplementation(cfg, apiClient)
			klog.V(2).InfoS("Fallback to Electricity Maps enabled")
		}

		return newCGAPIImplementation(*cfg.CGAPIConfig, cfg.APIConfig.Region, fallback)

	default:
		return nil, fmt.Errorf("unknown carbon provider: %s", cfg.Provider)
	}
}

// New creates a new carbon implementation using the factory pattern
// Deprecated: Use Factory instead for better provider selection
func New(cfg *config.CarbonConfig, apiClient *api.Client) Implementation {
	impl, err := Factory(cfg, apiClient)
	if err != nil {
		klog.ErrorS(err, "Failed to create carbon implementation, falling back to Electricity Maps")
		// Fallback to Electricity Maps if factory fails
		return newElectricityMapsImplementation(cfg, apiClient)
	}
	return impl
}
