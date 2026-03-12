package computegardener

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/almanac"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

// checkAlmanacScore checks if the pod should be scheduled based on almanac scoring
// Returns nil status if scheduling should proceed, otherwise returns an Unschedulable status
func (cs *ComputeGardenerScheduler) checkAlmanacScore(ctx context.Context, pod *v1.Pod, node *v1.Node) *framework.Status {
	// Only proceed if almanac is enabled
	if !cs.config.Almanac.Enabled || cs.almanacClient == nil {
		return nil
	}

	// Check if pod has almanac enabled (opt-in via annotation)
	enabled := false
	if val, ok := pod.Annotations[common.AnnotationAlmanacEnabled]; ok {
		if parsed, err := strconv.ParseBool(val); err == nil {
			enabled = parsed
		}
	}

	if !enabled {
		klog.V(3).InfoS("Almanac scoring not enabled for pod",
			"pod", klog.KObj(pod))
		return nil
	}

	// Extract node information
	nodeInfo := almanac.ExtractNodeInfo(node)
	nodeInfo.LogMissing(node.Name)

	// Get weights from annotations or use defaults
	carbonWeight := cs.config.Almanac.DefaultCarbonWeight
	priceWeight := cs.config.Almanac.DefaultPriceWeight

	if val, ok := pod.Annotations[common.AnnotationAlmanacCarbonWeight]; ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			carbonWeight = parsed
		}
	}

	if val, ok := pod.Annotations[common.AnnotationAlmanacPriceWeight]; ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			priceWeight = parsed
		}
	}

	// Ensure weights sum to 1.0
	weightSum := carbonWeight + priceWeight
	if weightSum > 0 && (weightSum < 0.99 || weightSum > 1.01) {
		// Normalize weights
		carbonWeight = carbonWeight / weightSum
		priceWeight = priceWeight / weightSum
	}

	// Build scoring request
	req := almanac.ScoreRequest{
		Weights: map[string]float64{
			"carbon": carbonWeight,
			"price":  priceWeight,
		},
	}

	// Prefer zone if available, otherwise use provider+region
	if nodeInfo.Zone != "" {
		req.Zone = nodeInfo.Zone
	} else if nodeInfo.Provider != "" && nodeInfo.Region != "" {
		req.Provider = nodeInfo.Provider
		req.Region = nodeInfo.Region
	} else {
		// Fall back to defaults if available
		if cs.config.Almanac.DefaultProvider != "" && cs.config.Almanac.DefaultRegion != "" {
			req.Provider = cs.config.Almanac.DefaultProvider
			req.Region = cs.config.Almanac.DefaultRegion
			klog.V(2).InfoS("Using default provider/region for almanac scoring",
				"pod", klog.KObj(pod),
				"node", node.Name,
				"provider", req.Provider,
				"region", req.Region)
		} else {
			// Cannot determine location - fail based on config
			if cs.config.Almanac.FailOpen {
				klog.V(2).InfoS("Cannot determine node location for almanac scoring, allowing (fail-open)",
					"pod", klog.KObj(pod),
					"node", node.Name)
				return nil
			}
			return framework.NewStatus(framework.Unschedulable,
				"cannot determine node location for almanac scoring")
		}
	}

	// Add instance type if available
	if nodeInfo.InstanceType != "" {
		req.InstanceType = nodeInfo.InstanceType
	} else if cs.config.Almanac.DefaultInstanceType != "" {
		req.InstanceType = cs.config.Almanac.DefaultInstanceType
	}

	// Make scoring request
	score, err := cs.almanacClient.GetScore(ctx, req)
	if err != nil {
		// Handle API error based on fail-open/closed policy
		klog.ErrorS(err, "Failed to get almanac score",
			"pod", klog.KObj(pod),
			"node", node.Name)

		if cs.config.Almanac.FailOpen {
			klog.V(2).InfoS("Almanac API error, allowing scheduling (fail-open)",
				"pod", klog.KObj(pod),
				"node", node.Name,
				"error", err)
			return nil
		}
		return framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("almanac scoring failed: %v", err))
	}

	// Get threshold from annotation or use default
	threshold := cs.config.Almanac.DefaultScoreThreshold
	if val, ok := pod.Annotations[common.AnnotationAlmanacScoreThreshold]; ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = parsed
		}
	}

	// Check if score meets threshold
	if score.OptimizationScore < threshold {
		msg := fmt.Sprintf("almanac score %.3f below threshold %.3f (recommendation: %s)",
			score.OptimizationScore, threshold, score.Recommendation)

		klog.V(2).InfoS("Node filtered by almanac score",
			"pod", klog.KObj(pod),
			"node", node.Name,
			"score", score.OptimizationScore,
			"threshold", threshold,
			"recommendation", score.Recommendation,
			"carbonScore", score.Components.CarbonScore,
			"priceScore", score.Components.PriceScore,
			"carbonIntensity", score.RawValues.CarbonIntensity,
			"spotPrice", score.RawValues.SpotPrice)

		return framework.NewStatus(framework.Unschedulable, msg)
	}

	// Score is acceptable
	klog.V(2).InfoS("Node passed almanac scoring check",
		"pod", klog.KObj(pod),
		"node", node.Name,
		"score", score.OptimizationScore,
		"threshold", threshold,
		"recommendation", score.Recommendation,
		"carbonScore", score.Components.CarbonScore,
		"priceScore", score.Components.PriceScore)

	return nil
}
