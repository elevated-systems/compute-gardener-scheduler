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

// electricityMapsImplementation wraps the Electricity Maps provider to implement carbon.Implementation
type electricityMapsImplementation struct {
	provider *electricitymaps.Provider
	config   *config.CarbonConfig
}

// newElectricityMapsImplementation creates a new Electricity Maps carbon implementation
func newElectricityMapsImplementation(cfg *config.CarbonConfig, apiClient *api.Client) *electricityMapsImplementation {
	return &electricityMapsImplementation{
		provider: electricitymaps.NewProvider(cfg, apiClient),
		config:   cfg,
	}
}

// GetCurrentIntensity returns the current carbon intensity for the configured region
func (i *electricityMapsImplementation) GetCurrentIntensity(ctx context.Context) (float64, error) {
	data, err := i.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		return 0, err
	}
	return data.Value, nil
}

// GetCurrentIntensityWithStatus returns carbon intensity along with data quality info
func (i *electricityMapsImplementation) GetCurrentIntensityWithStatus(ctx context.Context) (*IntensityData, error) {
	data, err := i.provider.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &IntensityData{
		Value:      data.Value,
		DataStatus: data.DataStatus,
	}, nil
}

// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
func (i *electricityMapsImplementation) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	klog.V(2).InfoS("Checking carbon intensity constraints",
		"threshold", threshold,
		"region", i.config.APIConfig.Region,
		"provider", "electricity-maps-api")

	intensity, err := i.GetCurrentIntensity(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to get carbon intensity data from Electricity Maps",
			"region", i.config.APIConfig.Region)
		return framework.NewStatus(framework.Error, err.Error())
	}

	klog.V(2).InfoS("Carbon intensity check",
		"intensity", intensity,
		"threshold", threshold,
		"region", i.config.APIConfig.Region,
		"provider", "electricity-maps-api",
		"exceeds", intensity > threshold)

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		klog.V(2).InfoS("Carbon intensity exceeds threshold - delaying scheduling",
			"intensity", intensity,
			"threshold", threshold,
			"region", i.config.APIConfig.Region)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	klog.V(2).InfoS("Carbon intensity within acceptable limits",
		"intensity", intensity,
		"threshold", threshold,
		"region", i.config.APIConfig.Region,
		"provider", "electricity-maps-api")
	return framework.NewStatus(framework.Success, "")
}
