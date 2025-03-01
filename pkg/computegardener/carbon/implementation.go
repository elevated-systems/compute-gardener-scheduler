package carbon

import (
	"context"
	"fmt"

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
	data, err := c.apiClient.GetCarbonIntensity(ctx, c.config.APIConfig.Region)
	if err != nil {
		return 0, fmt.Errorf("failed to get carbon intensity data: %v", err)
	}
	return data.CarbonIntensity, nil
}

func (c *carbonImpl) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	intensity, err := c.GetCurrentIntensity(ctx)
	if err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	return framework.NewStatus(framework.Success, "")
}
