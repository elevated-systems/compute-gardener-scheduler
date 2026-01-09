package eval

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
)

// Evaluator performs scheduling constraint checks independent of scheduler framework
// This shared logic can be used by both the scheduler and the dry-run webhook
type Evaluator struct {
	carbonImpl carbon.Implementation
	priceImpl  price.Implementation
	config     *config.Config
}

// NewEvaluator creates a new constraint evaluator
func NewEvaluator(carbonImpl carbon.Implementation, priceImpl price.Implementation, cfg *config.Config) *Evaluator {
	return &Evaluator{
		carbonImpl: carbonImpl,
		priceImpl:  priceImpl,
		config:     cfg,
	}
}

// EvaluateAll runs all constraint checks and returns comprehensive result
func (e *Evaluator) EvaluateAll(ctx context.Context, pod *v1.Pod, now time.Time) (*EvaluationResult, error) {
	result := &EvaluationResult{
		ShouldDelay: false,
		DelayType:   "none",
	}

	// Check carbon constraints if enabled
	var carbonDelay bool
	if e.config.Carbon.Enabled && e.carbonImpl != nil {
		carbonResult, err := e.EvaluateCarbonConstraints(ctx, pod)
		if err != nil {
			klog.V(2).InfoS("Carbon evaluation failed, treating as no delay",
				"pod", klog.KObj(pod),
				"error", err)
		} else if carbonResult.ShouldDelay {
			carbonDelay = true
			result.CurrentCarbon = carbonResult.CurrentCarbon
			result.CarbonThreshold = carbonResult.CarbonThreshold
			result.ReasonDescription = carbonResult.ReasonDescription
		} else {
			result.CurrentCarbon = carbonResult.CurrentCarbon
			result.CarbonThreshold = carbonResult.CarbonThreshold
		}
	}

	// Check price constraints if enabled
	var priceDelay bool
	if e.config.Pricing.Enabled && e.priceImpl != nil {
		priceResult, err := e.EvaluatePriceConstraints(pod, now)
		if err != nil {
			klog.V(2).InfoS("Price evaluation failed, treating as no delay",
				"pod", klog.KObj(pod),
				"error", err)
		} else if priceResult.ShouldDelay {
			priceDelay = true
			result.CurrentPrice = priceResult.CurrentPrice
			result.PriceThreshold = priceResult.PriceThreshold

			if carbonDelay {
				// Both constraints triggered
				result.ReasonDescription = fmt.Sprintf("%s and %s",
					result.ReasonDescription,
					priceResult.ReasonDescription)
			} else {
				result.ReasonDescription = priceResult.ReasonDescription
			}
		} else {
			result.CurrentPrice = priceResult.CurrentPrice
			result.PriceThreshold = priceResult.PriceThreshold
		}
	}

	// Determine overall delay type
	if carbonDelay && priceDelay {
		result.ShouldDelay = true
		result.DelayType = "both"
	} else if carbonDelay {
		result.ShouldDelay = true
		result.DelayType = "carbon"
	} else if priceDelay {
		result.ShouldDelay = true
		result.DelayType = "price"
	}

	// Estimate power and savings if pod would be delayed
	if result.ShouldDelay {
		estimate := e.estimatePodResources(pod)
		result.EstimatedPowerW = estimate.PowerWatts
		result.EstimatedRuntimeHours = estimate.RuntimeHours

		// Calculate conservative savings (current - threshold)
		energyKWh := (estimate.PowerWatts / 1000.0) * estimate.RuntimeHours

		if carbonDelay {
			carbonDelta := result.CurrentCarbon - result.CarbonThreshold
			if carbonDelta > 0 {
				result.EstimatedCarbonSavingsGCO2 = carbonDelta * energyKWh
			}
		}

		if priceDelay {
			priceDelta := result.CurrentPrice - result.PriceThreshold
			if priceDelta > 0 {
				result.EstimatedCostSavingsUSD = priceDelta * energyKWh
			}
		}
	}

	return result, nil
}

// EvaluateCarbonConstraints checks if pod should be delayed due to carbon intensity
func (e *Evaluator) EvaluateCarbonConstraints(ctx context.Context, pod *v1.Pod) (*EvaluationResult, error) {
	result := &EvaluationResult{
		ShouldDelay: false,
		DelayType:   "none",
	}

	// Get pod-specific threshold if it exists, otherwise use global threshold
	threshold := e.config.Carbon.IntensityThreshold
	if threshStr, ok := pod.Annotations[common.AnnotationCarbonIntensityThreshold]; ok {
		if threshVal, err := strconv.ParseFloat(threshStr, 64); err == nil {
			threshold = threshVal
		}
	}
	result.CarbonThreshold = threshold

	// Get current carbon intensity
	intensityData, err := e.carbonImpl.GetCurrentIntensityWithStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get carbon intensity: %w", err)
	}
	result.CurrentCarbon = intensityData.Value

	// Check if current intensity exceeds threshold
	if intensityData.Value > threshold {
		result.ShouldDelay = true
		result.DelayType = "carbon"
		result.ReasonDescription = fmt.Sprintf(
			"Current carbon intensity (%.2f gCO2eq/kWh) exceeds threshold (%.2f gCO2eq/kWh)",
			intensityData.Value, threshold)
	}

	return result, nil
}

// EvaluatePriceConstraints checks if pod should be delayed due to electricity price
func (e *Evaluator) EvaluatePriceConstraints(pod *v1.Pod, now time.Time) (*EvaluationResult, error) {
	result := &EvaluationResult{
		ShouldDelay: false,
		DelayType:   "none",
	}

	// Get current rate
	currentRate := e.priceImpl.GetCurrentRate(now)
	result.CurrentPrice = currentRate

	// Determine threshold - pod annotation takes precedence
	var threshold float64
	var hasThreshold bool

	if threshStr, ok := pod.Annotations[common.AnnotationPriceThreshold]; ok {
		if threshVal, err := strconv.ParseFloat(threshStr, 64); err == nil {
			threshold = threshVal
			hasThreshold = true
		} else {
			klog.V(2).InfoS("Invalid price threshold annotation, ignoring",
				"pod", klog.KObj(pod),
				"value", threshStr)
		}
	}

	// If no pod annotation and pricing is configured, check if we're in peak time
	// (TOU pricing doesn't use a global threshold, it uses peak/off-peak rates)
	if !hasThreshold {
		// For TOU pricing, delay during peak times unless pod has explicit threshold
		if e.priceImpl.IsPeakTime(now) {
			result.ShouldDelay = true
			result.DelayType = "price"
			result.PriceThreshold = currentRate // Use current rate as threshold
			result.ReasonDescription = fmt.Sprintf(
				"Current time is peak period (rate: $%.4f/kWh)",
				currentRate)
			return result, nil
		}
		// Off-peak time, no delay
		result.PriceThreshold = 0
		return result, nil
	}

	// Pod has explicit threshold - check if current rate exceeds it
	result.PriceThreshold = threshold
	if currentRate > threshold {
		result.ShouldDelay = true
		result.DelayType = "price"

		period := "off-peak"
		if e.priceImpl.IsPeakTime(now) {
			period = "peak"
		}

		result.ReasonDescription = fmt.Sprintf(
			"Current electricity rate ($%.4f/kWh, %s) exceeds threshold ($%.4f/kWh)",
			currentRate, period, threshold)
	}

	return result, nil
}

// estimatePodResources estimates resource usage for a pod
func (e *Evaluator) estimatePodResources(pod *v1.Pod) *PodResourceEstimate {
	estimate := &PodResourceEstimate{}

	var totalPower float64
	for _, container := range pod.Spec.Containers {
		// CPU: ~10W per core at full load
		cpuCores := container.Resources.Requests.Cpu().AsApproximateFloat64()
		estimate.CPUCores += cpuCores
		totalPower += cpuCores * 10.0

		// Memory: ~0.375W per GB
		memBytes := container.Resources.Requests.Memory().Value()
		memGB := float64(memBytes) / (1024 * 1024 * 1024)
		estimate.MemoryGB += memGB
		totalPower += memGB * 0.375

		// GPU: Check for nvidia.com/gpu
		if gpu, ok := container.Resources.Requests["nvidia.com/gpu"]; ok && !gpu.IsZero() {
			gpuCount := gpu.AsApproximateFloat64()
			estimate.GPUCount += gpuCount
			totalPower += gpuCount * 250.0 // Assume ~250W per GPU
		}
	}

	estimate.PowerWatts = totalPower

	// Estimate runtime based on annotation or workload type
	if runtimeStr, ok := pod.Annotations[common.AnnotationEstimatedRuntimeHours]; ok {
		if runtime, err := strconv.ParseFloat(runtimeStr, 64); err == nil {
			estimate.RuntimeHours = runtime
			return estimate
		}
	}

	// Heuristic based on workload type
	workloadType := determineWorkloadType(pod)
	switch workloadType {
	case common.WorkloadTypeBatch:
		estimate.RuntimeHours = 2.0 // Assume 2 hours for batch jobs
	case common.WorkloadTypeService:
		estimate.RuntimeHours = 24.0 // Services run continuously, use 1 day as unit
	default:
		estimate.RuntimeHours = 1.0 // Default to 1 hour
	}

	return estimate
}

// determineWorkloadType identifies the type of workload (batch, service, etc.)
func determineWorkloadType(pod *v1.Pod) string {
	// Check for explicit type label
	if typeLabel, ok := pod.Labels[common.LabelWorkloadType]; ok {
		return typeLabel
	}

	// Check owner references
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0]
		switch owner.Kind {
		case "Job", "CronJob":
			return common.WorkloadTypeBatch
		case "Deployment", "ReplicaSet":
			return common.WorkloadTypeService
		case "StatefulSet":
			return common.WorkloadTypeStateful
		case "DaemonSet":
			return common.WorkloadTypeSystem
		}
	}

	// Default to "generic" if we can't determine type
	return common.WorkloadTypeGeneric
}
