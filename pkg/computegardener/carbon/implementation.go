package carbon

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/api"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
)

// Implementation defines the interface for carbon-aware scheduling
type Implementation interface {
	// GetCurrentIntensity returns the current carbon intensity for the configured region
	GetCurrentIntensity(ctx context.Context) (float64, error)

	// CheckIntensityConstraints checks if current carbon intensity exceeds pod's threshold
	CheckIntensityConstraints(ctx context.Context, pod *v1.Pod) *framework.Status
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

func (c *carbonImpl) CheckIntensityConstraints(ctx context.Context, pod *v1.Pod) *framework.Status {
	intensity, err := c.GetCurrentIntensity(ctx)
	if err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	// Get threshold from pod annotation or use configured threshold
	threshold := c.config.IntensityThreshold
	klog.V(2).InfoS("Initial carbon intensity threshold from config",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"threshold", threshold)

	if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
		klog.V(2).InfoS("Found carbon intensity threshold annotation",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"value", val)
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
			klog.V(2).InfoS("Using carbon intensity threshold from annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"threshold", threshold)
		} else {
			klog.ErrorS(err, "Invalid carbon intensity threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"value", val)
			return framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation")
		}
	}

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	return framework.NewStatus(framework.Success, "")
}
