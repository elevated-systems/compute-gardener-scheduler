package carbon

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon/cgapi"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// cgapiImplementation wraps the CG API provider to implement carbon.Implementation
type cgapiImplementation struct {
	provider *cgapi.Provider
	config   config.CGAPIConfig
	region   string
	fallback Implementation // Electricity Maps implementation for fallback
}

// newCGAPIImplementation creates a new CG API carbon implementation
func newCGAPIImplementation(cfg config.CGAPIConfig, region string, fallback Implementation) (*cgapiImplementation, error) {
	provider, err := cgapi.NewProvider(cfg, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create CG API provider: %w", err)
	}

	return &cgapiImplementation{
		provider: provider,
		config:   cfg,
		region:   region,
		fallback: fallback,
	}, nil
}

// GetCurrentIntensity returns the current carbon intensity for the configured region
func (i *cgapiImplementation) GetCurrentIntensity(ctx context.Context) (float64, error) {
	data, err := i.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		return 0, err
	}
	return data.Value, nil
}

// GetCurrentIntensityWithStatus returns carbon intensity along with data quality info
func (i *cgapiImplementation) GetCurrentIntensityWithStatus(ctx context.Context) (*IntensityData, error) {
	data, err := i.provider.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		// Fallback to Electricity Maps if configured and CG API fails
		if i.config.FallbackToEM && i.fallback != nil {
			klog.V(2).InfoS("CG API request failed, falling back to Electricity Maps",
				"error", err,
				"region", i.region)
			return i.fallback.GetCurrentIntensityWithStatus(ctx)
		}
		return nil, err
	}

	return &IntensityData{
		Value:      data.Value,
		DataStatus: data.DataStatus,
	}, nil
}

// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
func (i *cgapiImplementation) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	klog.V(2).InfoS("Checking carbon intensity constraints",
		"threshold", threshold,
		"region", i.region,
		"provider", "cg-api")

	intensity, err := i.GetCurrentIntensity(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to get carbon intensity data from CG API",
			"region", i.region)
		return framework.NewStatus(framework.Error, err.Error())
	}

	klog.V(2).InfoS("Carbon intensity check",
		"intensity", intensity,
		"threshold", threshold,
		"region", i.region,
		"provider", "cg-api",
		"exceeds", intensity > threshold)

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		klog.V(2).InfoS("Carbon intensity exceeds threshold - delaying scheduling",
			"intensity", intensity,
			"threshold", threshold,
			"region", i.region,
			"provider", "cg-api")
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	klog.V(2).InfoS("Carbon intensity within acceptable limits",
		"intensity", intensity,
		"threshold", threshold,
		"region", i.region,
		"provider", "cg-api")
	return framework.NewStatus(framework.Success, "")
}
