package carbon

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// Implementation defines the interface for carbon-aware scheduling
type Implementation interface {
	// GetCurrentIntensity returns the current carbon intensity for the configured region
	GetCurrentIntensity(ctx context.Context) (float64, error)

	// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
	CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status
}

type carbonImpl struct {
	config    *config.CarbonConfig
	apiClient *api.Client
}

// New creates a new carbon implementation
func New(cfg *config.CarbonConfig, apiClient *api.Client) Implementation {
	return &carbonImpl{
		config:    cfg,
		apiClient: apiClient,
	}
}

func (c *carbonImpl) GetCurrentIntensity(ctx context.Context) (float64, error) {
	// Log region used for debugging
	klog.V(2).InfoS("Fetching carbon intensity data",
		"region", c.config.APIConfig.Region,
		"apiKey", c.config.APIConfig.APIKey != "")

	data, err := c.apiClient.GetCarbonIntensity(ctx, c.config.APIConfig.Region)
	if err != nil {
		klog.V(2).InfoS("Failed to get carbon intensity data", "error", err)
		return 0, fmt.Errorf("failed to get carbon intensity data: %v", err)
	}
	klog.V(2).InfoS("Carbon intensity data retrieved",
		"region", c.config.APIConfig.Region,
		"intensity", data.CarbonIntensity)
	return data.CarbonIntensity, nil
}

func (c *carbonImpl) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	klog.V(2).InfoS("Checking carbon intensity constraints",
		"threshold", threshold,
		"region", c.config.APIConfig.Region)

	intensity, err := c.GetCurrentIntensity(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to get carbon intensity data",
			"region", c.config.APIConfig.Region)
		return framework.NewStatus(framework.Error, err.Error())
	}

	klog.V(2).InfoS("Carbon intensity check",
		"intensity", intensity,
		"threshold", threshold,
		"region", c.config.APIConfig.Region,
		"exceeds", intensity > threshold)

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		klog.V(2).InfoS("Carbon intensity exceeds threshold - delaying scheduling",
			"intensity", intensity,
			"threshold", threshold,
			"region", c.config.APIConfig.Region)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	klog.V(2).InfoS("Carbon intensity within acceptable limits",
		"intensity", intensity,
		"threshold", threshold,
		"region", c.config.APIConfig.Region)
	return framework.NewStatus(framework.Success, "")
}
