package computegardener

import (
	"context"
	"strconv"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// handlePodCompletion records metrics when a pod completes using time-series metrics
func (cs *ComputeGardenerScheduler) handlePodCompletion(pod *v1.Pod) {
	klog.V(2).InfoS("Starting pod completion handling",
		"pod", klog.KObj(pod),
		"phase", pod.Status.Phase,
		"nodeName", pod.Spec.NodeName)

	// Skip if we don't have the metrics store configured
	if cs.metricsStore == nil {
		klog.V(2).InfoS("Metrics store not configured, skipping pod completion handling",
			"pod", klog.KObj(pod))
		return
	}

	podUID := string(pod.UID)
	podName := pod.Name
	namespace := pod.Namespace
	nodeName := pod.Spec.NodeName

	// Check if we should delay metrics collection to allow final metrics to be collected
	// This is useful when using Prometheus metrics which may have a lag in reporting
	if cs.config.Metrics.Prometheus != nil && cs.config.Metrics.Prometheus.CompletionDelay != "" {
		// Parse the delay duration
		delay, err := time.ParseDuration(cs.config.Metrics.Prometheus.CompletionDelay)
		if err == nil && delay > 0 {
			klog.V(2).InfoS("Delaying pod completion handling to allow metrics collection",
				"pod", klog.KObj(pod),
				"delay", delay.String())

			// Start a goroutine to handle this after delay
			go func() {
				// Sleep for the specified delay
				time.Sleep(delay)

				// Continue with metrics processing
				cs.processPodCompletionMetrics(pod, podUID, podName, namespace, nodeName)
			}()

			return
		} else if err != nil {
			klog.V(2).ErrorS(err, "Invalid completion delay, proceeding immediately",
				"pod", klog.KObj(pod))
		}
	}

	// Process pod completion metrics immediately if no delay configured
	cs.processPodCompletionMetrics(pod, podUID, podName, namespace, nodeName)
}

// processPodCompletionMetrics processes metrics for a completed pod
func (cs *ComputeGardenerScheduler) processPodCompletionMetrics(pod *v1.Pod, podUID, podName, namespace, nodeName string) {
	// Get pod metrics history first to check if already completed
	metricsHistory, found := cs.metricsStore.GetHistory(podUID)

	// Check if already processed or no history available
	if !found {
		klog.V(2).InfoS("No metrics history found for pod",
			"pod", klog.KObj(pod),
			"podUID", podUID)
		return
	} else if found && metricsHistory.Completed {
		// Pod was already processed for completion, avoid double-counting
		klog.V(2).InfoS("Pod already marked as completed, skipping metrics calculation to avoid double-counting",
			"pod", klog.KObj(pod),
			"podUID", podUID)
		return
	} else if len(metricsHistory.Records) == 0 {
		klog.V(2).InfoS("Metrics history is empty for pod",
			"pod", klog.KObj(pod),
			"podUID", podUID)
		return
	}

	// Mark pod as completed in metrics store to prevent further collection
	cs.metricsStore.MarkCompleted(podUID)

	// Check if pod was delayed BEFORE removing from tracking maps
	wasCarbonDelayed := cs.carbonDelayedPods != nil && cs.carbonDelayedPods[podUID]
	wasPriceDelayed := cs.priceDelayedPods != nil && cs.priceDelayedPods[podUID]

	// Remove pod from delay tracking maps to clean up memory
	if cs.carbonDelayedPods != nil {
		delete(cs.carbonDelayedPods, podUID)
	}
	if cs.priceDelayedPods != nil {
		delete(cs.priceDelayedPods, podUID)
	}

	if wasCarbonDelayed || wasPriceDelayed {
		klog.V(2).InfoS("Removed pod from delay tracking maps",
			"pod", klog.KObj(pod),
			"podUID", podUID,
			"wasCarbonDelayed", wasCarbonDelayed,
			"wasPriceDelayed", wasPriceDelayed)
	}

	klog.V(2).InfoS("Found metrics history for pod",
		"pod", klog.KObj(pod),
		"podUID", podUID,
		"recordCount", len(metricsHistory.Records))

	// Log a sample of the metrics record to verify GPU data
	if len(metricsHistory.Records) > 0 {
		firstRecord := metricsHistory.Records[0]
		midIndex := len(metricsHistory.Records) / 2
		midRecord := metricsHistory.Records[midIndex]
		lastRecord := metricsHistory.Records[len(metricsHistory.Records)-1]

		klog.V(1).InfoS("Sample metrics records for energy calculation",
			"pod", klog.KObj(pod),
			"firstRecord", map[string]interface{}{
				"timestamp":     firstRecord.Timestamp,
				"cpu":           firstRecord.CPU,
				"memory":        firstRecord.Memory,
				"gpuPower":      firstRecord.GPUPowerWatts,
				"powerEstimate": firstRecord.PowerEstimate,
			},
			"midRecord", map[string]interface{}{
				"timestamp":     midRecord.Timestamp,
				"cpu":           midRecord.CPU,
				"memory":        midRecord.Memory,
				"gpuPower":      midRecord.GPUPowerWatts,
				"powerEstimate": midRecord.PowerEstimate,
			},
			"lastRecord", map[string]interface{}{
				"timestamp":     lastRecord.Timestamp,
				"cpu":           lastRecord.CPU,
				"memory":        lastRecord.Memory,
				"gpuPower":      lastRecord.GPUPowerWatts,
				"powerEstimate": lastRecord.PowerEstimate,
			})
	}

	// Calculate energy and carbon emissions using our utility functions
	// TODO: Consider refactoring energy calculations to store CPU/memory and GPU power
	// directly in PodMetricsRecord instead of computing via subtraction, for cleaner
	// separation of concerns and more efficient dashboard queries
	totalEnergyKWh := metrics.CalculateTotalEnergy(metricsHistory.Records)
	gpuEnergyKWh := metrics.CalculateGPUEnergy(metricsHistory.Records)
	totalCarbonEmissions := metrics.CalculateTotalCarbonEmissions(metricsHistory.Records)

	// Validate values before recording
	if totalEnergyKWh <= 0 {
		klog.ErrorS(nil, "Warning: Zero or negative energy value being recorded",
			"pod", podName,
			"namespace", namespace,
			"value", totalEnergyKWh,
			"recordCount", len(metricsHistory.Records))
	}
	if totalCarbonEmissions <= 0 {
		klog.ErrorS(nil, "Warning: Zero or negative carbon emissions value being recorded",
			"pod", podName,
			"namespace", namespace,
			"value", totalCarbonEmissions,
			"recordCount", len(metricsHistory.Records))
	}

	// Log the values we're recording even if positive
	klog.V(3).InfoS("Recording pod energy and carbon metrics",
		"pod", klog.KObj(pod),
		"namespace", namespace,
		"energyKWh", totalEnergyKWh,
		"carbonEmissions", totalCarbonEmissions,
		"metricsCount", len(metricsHistory.Records),
		"firstTimestamp", metricsHistory.Records[0].Timestamp,
		"lastTimestamp", metricsHistory.Records[len(metricsHistory.Records)-1].Timestamp)

	// Record the metrics with debug logging
	klog.V(3).InfoS("Recording energy and carbon metrics",
		"pod", klog.KObj(pod),
		"namespace", namespace,
		"energyKWh", totalEnergyKWh,
		"carbonEmissions", totalCarbonEmissions,
		"metricsCount", len(metricsHistory.Records))

	metrics.JobEnergyUsage.WithLabelValues(podName, namespace).Set(totalEnergyKWh)
	metrics.JobGPUEnergyUsage.WithLabelValues(podName, namespace).Set(gpuEnergyKWh)
	metrics.JobCarbonEmissions.WithLabelValues(podName, namespace).Set(totalCarbonEmissions)

	// Calculate true carbon and cost differences based on the delta between initial and final intensity/rate
	// multiplied by actual energy used - requires all three values to be known

	// Carbon difference calculation - only if pod was actually delayed by carbon constraints
	// The presence of the initial-carbon-intensity annotation indicates the pod was delayed
	if initialIntensityStr, ok := pod.Annotations[common.AnnotationInitialCarbonIntensity]; ok {
		// Use annotation presence as source of truth for whether pod was delayed
		if initialIntensityStr != "" {
			initialIntensity, err := strconv.ParseFloat(initialIntensityStr, 64)
			if err == nil {
				// Get the bind-time carbon intensity from annotation (captured when pod passed filter)
				var bindTimeIntensity float64
				if bindTimeStr, hasBindTime := pod.Annotations[common.AnnotationBindTimeCarbonIntensity]; hasBindTime {
					bindTimeIntensity, _ = strconv.ParseFloat(bindTimeStr, 64)
				} else if len(metricsHistory.Records) > 0 {
					// Fallback to first metrics record if bind-time annotation is missing (legacy)
					bindTimeIntensity = metricsHistory.Records[0].CarbonIntensity
				} else if cs.carbonImpl != nil {
					// Final fallback: get current intensity
					bindTimeIntensity, _ = cs.carbonImpl.GetCurrentIntensity(context.Background())
				}

				if bindTimeIntensity > 0 {
					// Calculate true scheduler effectiveness: (initial - bind-time) * energy consumed
					// This compares when scheduler first heard vs when job actually started executing
					intensityDiff := initialIntensity - bindTimeIntensity
					// Intensity is gCO2/kWh, Energy is kWh, result is gCO2
					carbonSavingsGrams := intensityDiff * totalEnergyKWh

					// Log regardless of whether savings are positive or negative
					klog.V(2).InfoS("Calculated carbon savings from scheduling decision",
						"pod", klog.KObj(pod),
						"initialIntensity", initialIntensity,
						"bindTimeIntensity", bindTimeIntensity,
						"energyKWh", totalEnergyKWh,
						"savingsGrams", carbonSavingsGrams,
						"isPositive", intensityDiff > 0)

					// Record actual calculated savings or costs (even if negative)
					metrics.EstimatedSavings.WithLabelValues("carbon", "grams_co2", podName, namespace).Set(carbonSavingsGrams)

					// Record efficiency metrics
					metrics.SchedulingEfficiencyMetrics.WithLabelValues("carbon_intensity_delta", podName).Set(intensityDiff)
				}
			}
		}
	}

	// Cost difference calculation - only if pod was actually delayed by price constraints
	// The presence of the initial-electricity-rate annotation indicates the pod was delayed
	if initialRateStr, ok := pod.Annotations[common.AnnotationInitialElectricityRate]; ok {
		// Use annotation presence as source of truth for whether pod was delayed
		// This is more reliable than in-memory state which may be false if pod passed threshold before binding
		if initialRateStr != "" {
			initialRate, err := strconv.ParseFloat(initialRateStr, 64)
			if err == nil {
				// Get the bind-time electricity rate from annotation (captured when pod passed filter)
				var bindTimeRate float64
				if bindTimeRateStr, hasBindTime := pod.Annotations[common.AnnotationBindTimeElectricityRate]; hasBindTime {
					bindTimeRate, _ = strconv.ParseFloat(bindTimeRateStr, 64)
				} else if len(metricsHistory.Records) > 0 && metricsHistory.Records[0].ElectricityRate > 0 {
					// Fallback to first metrics record if bind-time annotation is missing (legacy)
					bindTimeRate = metricsHistory.Records[0].ElectricityRate
				} else if cs.priceImpl != nil {
					// Final fallback: get current rate
					bindTimeRate = cs.priceImpl.GetCurrentRate(time.Now())
				}

				if bindTimeRate > 0 {
					// Calculate cost savings from scheduling decision: (initial - bind-time) * energy consumed
					// This compares when scheduler first heard vs when job actually started executing
					rateDiff := initialRate - bindTimeRate
					costSavingsDollars := rateDiff * totalEnergyKWh

					// Log regardless of whether savings are positive or negative
					klog.V(2).InfoS("Calculated cost savings from scheduling decision",
						"pod", klog.KObj(pod),
						"initialRate", initialRate,
						"bindTimeRate", bindTimeRate,
						"energyKWh", totalEnergyKWh,
						"savingsDollars", costSavingsDollars,
						"isPositive", rateDiff > 0)

					// Record actual calculated savings or costs (even if negative)
					metrics.EstimatedSavings.WithLabelValues("cost", "dollars", podName, namespace).Set(costSavingsDollars)

					// Record efficiency metrics
					metrics.SchedulingEfficiencyMetrics.WithLabelValues("electricity_rate_delta", podName).Set(rateDiff)
				}
			}
		}
	}

	// Determine owner reference type for metrics
	ownerKind := "Pod"
	if len(pod.OwnerReferences) > 0 {
		ownerKind = pod.OwnerReferences[0].Kind
	}

	// Check if pod had an energy budget annotation
	if val, ok := pod.Annotations[common.AnnotationEnergyBudgetKWh]; ok {
		budgetKWh, err := strconv.ParseFloat(val, 64)
		if err == nil && budgetKWh > 0 {
			// Calculate percentage of budget used
			usagePercent := (totalEnergyKWh / budgetKWh) * 100

			// Record energy budget usage metric
			metrics.EnergyBudgetTracking.WithLabelValues(podName, namespace).Set(usagePercent)

			// Log whether the pod exceeded its energy budget
			if totalEnergyKWh > budgetKWh {
				klog.V(2).InfoS("Pod exceeded energy budget",
					"pod", klog.KObj(pod),
					"budgetKWh", budgetKWh,
					"actualKWh", totalEnergyKWh,
					"exceededBy", totalEnergyKWh-budgetKWh,
					"usagePercent", usagePercent,
					"ownerKind", ownerKind)

				// Record exceeded metric with default action
				action := common.EnergyBudgetActionLog

				// Check if there's an action to take when budget exceeded
				if actionVal, ok := pod.Annotations[common.AnnotationEnergyBudgetAction]; ok {
					action = actionVal
					cs.handleEnergyBudgetAction(pod, action, totalEnergyKWh, budgetKWh)
				}

				// Increment the counter for exceeded budgets
				metrics.EnergyBudgetExceeded.WithLabelValues(namespace, ownerKind, action).Inc()
			} else {
				klog.V(2).InfoS("Pod completed within energy budget",
					"pod", klog.KObj(pod),
					"budgetKWh", budgetKWh,
					"actualKWh", totalEnergyKWh,
					"remainingKWh", budgetKWh-totalEnergyKWh,
					"usagePercent", usagePercent,
					"ownerKind", ownerKind)
			}
		}
	}

	// Set final metrics for the pod
	if len(metricsHistory.Records) > 0 {
		final := metricsHistory.Records[len(metricsHistory.Records)-1]
		metrics.NodeCPUUsage.WithLabelValues(nodeName, podName, "final").Set(final.CPU)
		metrics.NodeMemoryUsage.WithLabelValues(nodeName, podName, "final").Set(final.Memory)
		metrics.NodeGPUPower.WithLabelValues(nodeName, podName, "final").Set(final.GPUPowerWatts)
		metrics.NodePowerEstimate.WithLabelValues(nodeName, podName, "final").Set(final.PowerEstimate)

		// Reset current phase metrics to 0 to ensure completed pods don't appear in dashboards
		metrics.NodeCPUUsage.WithLabelValues(nodeName, podName, "current").Set(0)
		metrics.NodeMemoryUsage.WithLabelValues(nodeName, podName, "current").Set(0)
		metrics.NodeGPUPower.WithLabelValues(nodeName, podName, "current").Set(0)
		metrics.NodePowerEstimate.WithLabelValues(nodeName, podName, "current").Set(0)

		klog.V(2).InfoS("Reset current power metrics to zero for completed pod",
			"pod", klog.KObj(pod),
			"podUID", podUID)
	}
}

// handleEnergyBudgetAction performs actions when a pod exceeds its energy budget
func (cs *ComputeGardenerScheduler) handleEnergyBudgetAction(pod *v1.Pod, action string, actualKWh, budgetKWh float64) {
	clientset := cs.handle.ClientSet()
	ctx := context.Background()

	switch action {
	case common.EnergyBudgetActionLog:
		// Just log the event (already done in the calling function)
		return

	case common.EnergyBudgetActionAnnotate:
		// Add annotations with energy usage information
		podCopy := pod.DeepCopy()
		if podCopy.Annotations == nil {
			podCopy.Annotations = make(map[string]string)
		}

		// Add energy usage annotations
		podCopy.Annotations[common.AnnotationEnergyUsageKWh] = strconv.FormatFloat(actualKWh, 'f', 6, 64)
		podCopy.Annotations[common.AnnotationEnergyBudgetExceeded] = "true"
		podCopy.Annotations[common.AnnotationEnergyBudgetExceededBy] = strconv.FormatFloat(actualKWh-budgetKWh, 'f', 6, 64)

		// Update the pod with new annotations
		_, err := clientset.CoreV1().Pods(pod.Namespace).Update(ctx, podCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to update pod with energy budget annotations",
				"pod", klog.KObj(pod))
		}

	case common.EnergyBudgetActionLabel:
		// Add labels with energy usage information (for service selection)
		podCopy := pod.DeepCopy()
		if podCopy.Labels == nil {
			podCopy.Labels = make(map[string]string)
		}

		// Add energy usage label
		podCopy.Labels[common.AnnotationBase+"/energy-budget-exceeded"] = "true"

		// Update the pod with new labels
		_, err := clientset.CoreV1().Pods(pod.Namespace).Update(ctx, podCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to update pod with energy budget labels",
				"pod", klog.KObj(pod))
		}

	case common.EnergyBudgetActionNotify:
		// Create an event for the pod
		_, err := clientset.CoreV1().Events(pod.Namespace).Create(ctx, &v1.Event{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "energy-budget-exceeded-",
				Namespace:    pod.Namespace,
			},
			InvolvedObject: v1.ObjectReference{
				Kind:            "Pod",
				Namespace:       pod.Namespace,
				Name:            pod.Name,
				UID:             pod.UID,
				APIVersion:      "v1",
				ResourceVersion: pod.ResourceVersion,
			},
			Reason:  "EnergyBudgetExceeded",
			Message: "Pod exceeded its energy budget of " + strconv.FormatFloat(budgetKWh, 'f', 6, 64) + " kWh by using " + strconv.FormatFloat(actualKWh, 'f', 6, 64) + " kWh",
			Type:    "Warning",
			Source: v1.EventSource{
				Component: "compute-gardener-scheduler",
			},
		}, metav1.CreateOptions{})

		if err != nil {
			klog.ErrorS(err, "Failed to create event for energy budget exceeded",
				"pod", klog.KObj(pod))
		}

	default:
		klog.V(2).InfoS("Unknown energy budget action",
			"pod", klog.KObj(pod),
			"action", action)
	}
}
