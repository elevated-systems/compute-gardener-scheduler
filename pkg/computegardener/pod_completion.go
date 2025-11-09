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

	// Calculate scheduler effectiveness using time-series counterfactual analysis.
	// This compares actual emissions (what happened) with counterfactual emissions
	// (what would have happened if the job ran during the delay period).
	//
	// We use the same power profile for both scenarios (since the job's computation is intrinsic)
	// but apply historical carbon intensity from the delayed period to estimate counterfactual emissions.

	// Carbon savings calculation - only if pod was actually delayed by carbon constraints
	if initialTimestampStr, ok := pod.Annotations[common.AnnotationInitialTimestamp]; ok {
		// wasCarbonDelayed was already captured above before removing from map
		// TODO: This in-memory state is lost on scheduler restart, so we won't calculate savings
		// for pods that were delayed before a restart and complete after. Consider persisting delay
		// state to etcd as annotation if we need to be resilient to scheduler restarts during job execution.

		if !wasCarbonDelayed {
			klog.V(3).InfoS("Skipping carbon savings calculation - pod was not carbon-delayed",
				"pod", klog.KObj(pod))
		} else {
			// Try time-series counterfactual first (best precision)
			counterfactualEmissions := cs.calculateCounterfactualCarbonEmissions(
				context.Background(),
				pod,
				metricsHistory.Records,
				initialTimestampStr,
			)

			var carbonSavingsGrams float64
			var method string

			if counterfactualEmissions > 0 {
				// Success! Use high-precision time-series method
				method = "timeseries"

				// Record counterfactual emissions metric
				metrics.JobCounterfactualCarbonEmissions.WithLabelValues(podName, namespace).Set(counterfactualEmissions)

				// Calculate savings: counterfactual - actual
				carbonSavingsGrams = counterfactualEmissions - totalCarbonEmissions

				klog.V(2).InfoS("Calculated carbon savings using time-series counterfactual analysis",
					"pod", klog.KObj(pod),
					"method", method,
					"actualEmissions", totalCarbonEmissions,
					"counterfactualEmissions", counterfactualEmissions,
					"savingsGrams", carbonSavingsGrams,
					"isPositive", carbonSavingsGrams > 0)
			} else {
				// Fallback to simple calculation if Prometheus unavailable or data incomplete
				method = "simple"

				// Get initial and bind intensities for simple calculation
				initialIntensity, err := strconv.ParseFloat(pod.Annotations[common.AnnotationInitialCarbonIntensity], 64)
				if err != nil {
					klog.ErrorS(err, "Failed to parse initial carbon intensity for fallback calculation",
						"pod", klog.KObj(pod))
					return // Can't calculate without initial intensity
				}

				var bindTimeIntensity float64
				if bindTimeStr, ok := pod.Annotations[common.AnnotationBindCarbonIntensity]; ok {
					bindTimeIntensity, _ = strconv.ParseFloat(bindTimeStr, 64)
				} else if len(metricsHistory.Records) > 0 && metricsHistory.Records[0].CarbonIntensity > 0 {
					bindTimeIntensity = metricsHistory.Records[0].CarbonIntensity
				}

				if bindTimeIntensity > 0 {
					// Simple calculation: (initial - bind) × total_energy
					intensityDiff := initialIntensity - bindTimeIntensity
					carbonSavingsGrams = intensityDiff * totalEnergyKWh

					klog.V(2).InfoS("Calculated carbon savings using simple fallback method (Prometheus unavailable)",
						"pod", klog.KObj(pod),
						"method", method,
						"initialIntensity", initialIntensity,
						"bindTimeIntensity", bindTimeIntensity,
						"energyKWh", totalEnergyKWh,
						"savingsGrams", carbonSavingsGrams,
						"note", "Rough estimate - historical data unavailable")
				} else {
					klog.ErrorS(nil, "Cannot calculate carbon savings - no bind-time intensity available",
						"pod", klog.KObj(pod))
					return // Can't calculate at all
				}
			}

			// Record savings metric with method label
			metrics.EstimatedSavings.WithLabelValues("carbon", "grams_co2", method, podName, namespace).Set(carbonSavingsGrams)

			// Record efficiency metric
			metrics.SchedulingEfficiencyMetrics.WithLabelValues("carbon_emissions_delta", podName).Set(carbonSavingsGrams)
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
					hasBindTime = bindTimeRate > 0
				} else if len(metricsHistory.Records) > 0 && metricsHistory.Records[0].ElectricityRate > 0 {
					// Fallback to first metrics record if bind-time annotation is missing (legacy)
					// This represents the rate when the pod started executing
					bindTimeRate = metricsHistory.Records[0].ElectricityRate
					hasBindTime = true
					klog.V(2).InfoS("Using first metrics record for bind-time rate (legacy fallback)",
						"pod", klog.KObj(pod),
						"rate", bindTimeRate)
				}

				if hasBindTime {
					// Calculate cost savings from scheduling decision: (initial - bind-time) * energy consumed
					// This compares the rate when first delayed vs when job actually started executing.
					// If the job was seen at $0.25/kWh and started at $0.25/kWh, savings = 0 (correct).
					// Note: Currently uses "simple" method as we don't track electricity rate time-series in Prometheus
					rateDiff := initialRate - bindTimeRate
					costSavingsDollars := rateDiff * totalEnergyKWh

					// Log regardless of whether savings are positive or negative
					klog.V(2).InfoS("Calculated cost savings from scheduling decision",
						"pod", klog.KObj(pod),
						"method", "simple",
						"initialRate", initialRate,
						"bindTimeRate", bindTimeRate,
						"energyKWh", totalEnergyKWh,
						"savingsDollars", costSavingsDollars,
						"isPositive", rateDiff > 0)

					// Record actual calculated savings or costs (even if negative)
					// Always uses "simple" method for now (no time-series electricity rate tracking yet)
					metrics.EstimatedSavings.WithLabelValues("cost", "dollars", "simple", podName, namespace).Set(costSavingsDollars)

					// Record efficiency metrics
					metrics.SchedulingEfficiencyMetrics.WithLabelValues("electricity_rate_delta", podName).Set(rateDiff)
				} else {
					klog.ErrorS(nil, "Cannot calculate cost savings - no bind-time rate available",
						"pod", klog.KObj(pod),
						"initialRate", initialRate,
						"note", "Bind-time annotation missing and no metrics records available")
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

// calculateCounterfactualCarbonEmissions calculates what emissions would have been
// if the job had run during the initial delay period instead of when it actually executed.
//
// This uses the actual power profile (from metrics records) but applies historical carbon
// intensity from Prometheus for the period when the pod was delayed. The methodology:
//
// 1. Query Prometheus for historical carbon intensity during [initial_time, bind_time]
// 2. Match historical intensity with execution power profile in lock-step
// 3. Calculate emissions using same trapezoid integration as actual emissions
//
// This provides a time-series based counterfactual that accounts for grid intensity
// variations, giving a more accurate estimate than simple multiplication.
func (cs *ComputeGardenerScheduler) calculateCounterfactualCarbonEmissions(
	ctx context.Context,
	pod *v1.Pod,
	executionRecords []metrics.PodMetricsRecord,
	initialTimestampStr string,
) float64 {
	// Check if we have Prometheus client for historical queries
	if cs.prometheusClient == nil {
		klog.V(2).InfoS("No Prometheus client available, cannot calculate counterfactual emissions",
			"pod", klog.KObj(pod))
		return 0
	}

	// Parse initial timestamp
	initialTime, err := time.Parse(time.RFC3339, initialTimestampStr)
	if err != nil {
		klog.ErrorS(err, "Failed to parse initial timestamp",
			"pod", klog.KObj(pod),
			"timestamp", initialTimestampStr)
		return 0
	}

	// Parse bind timestamp
	bindTimestampStr, ok := pod.Annotations[common.AnnotationBindTimestamp]
	if !ok {
		// Fallback to first execution record timestamp if bind timestamp not available
		if len(executionRecords) > 0 {
			bindTimestampStr = executionRecords[0].Timestamp.Format(time.RFC3339)
			klog.V(2).InfoS("Using first execution record for bind time (legacy fallback)",
				"pod", klog.KObj(pod))
		} else {
			klog.ErrorS(nil, "No bind timestamp or execution records available",
				"pod", klog.KObj(pod))
			return 0
		}
	}

	_, err = time.Parse(time.RFC3339, bindTimestampStr)
	if err != nil {
		klog.ErrorS(err, "Failed to parse bind timestamp",
			"pod", klog.KObj(pod),
			"timestamp", bindTimestampStr)
		return 0
	}

	// Determine execution duration from metrics records
	if len(executionRecords) < 2 {
		klog.V(2).InfoS("Insufficient metrics records for counterfactual calculation",
			"pod", klog.KObj(pod),
			"recordCount", len(executionRecords))
		return 0
	}

	executionDuration := executionRecords[len(executionRecords)-1].Timestamp.Sub(executionRecords[0].Timestamp)
	counterfactualEnd := initialTime.Add(executionDuration)

	// Determine step size from execution records (use median interval)
	step := 15 * time.Second // Default
	if len(executionRecords) > 1 {
		step = executionRecords[1].Timestamp.Sub(executionRecords[0].Timestamp)
	}

	// Get carbon configuration region
	if cs.config == nil || !cs.config.Carbon.Enabled {
		klog.V(2).InfoS("Carbon awareness not enabled, cannot query historical intensity",
			"pod", klog.KObj(pod))
		return 0
	}
	region := cs.config.Carbon.APIConfig.Region

	klog.V(2).InfoS("Querying historical carbon intensity for counterfactual calculation",
		"pod", klog.KObj(pod),
		"region", region,
		"initialTime", initialTime,
		"counterfactualEnd", counterfactualEnd,
		"executionDuration", executionDuration,
		"step", step)

	// Query historical carbon intensity from Prometheus
	historicalIntensity, err := cs.prometheusClient.QueryHistoricalCarbonIntensity(
		ctx,
		region,
		initialTime,
		counterfactualEnd,
		step,
	)

	if err != nil {
		klog.ErrorS(err, "Failed to query historical carbon intensity",
			"pod", klog.KObj(pod),
			"region", region)
		return 0
	}

	if len(historicalIntensity) == 0 {
		klog.V(2).InfoS("No historical carbon intensity data available",
			"pod", klog.KObj(pod),
			"region", region)
		return 0
	}

	klog.V(2).InfoS("Retrieved historical carbon intensity data",
		"pod", klog.KObj(pod),
		"dataPoints", len(historicalIntensity),
		"executionRecords", len(executionRecords))

	// Match historical intensity with execution power profile
	// We iterate through execution records and find corresponding historical intensity
	// using forward-fill for missing data points
	totalCounterfactualEmissions := 0.0
	historicalIdx := 0

	for i := 1; i < len(executionRecords); i++ {
		current := executionRecords[i]
		previous := executionRecords[i-1]

		// Calculate time offset from execution start
		offsetFromStart := current.Timestamp.Sub(executionRecords[0].Timestamp)
		// Map to counterfactual timeline
		counterfactualTime := initialTime.Add(offsetFromStart)

		// Find matching historical intensity (or use forward-fill)
		var historicalIntensityCurrent, historicalIntensityPrevious float64

		// Find closest historical intensity for current timestamp
		for historicalIdx < len(historicalIntensity)-1 &&
			historicalIntensity[historicalIdx+1].Timestamp.Before(counterfactualTime) {
			historicalIdx++
		}
		historicalIntensityCurrent = historicalIntensity[historicalIdx].Intensity

		// Use same index for previous (forward-fill if needed)
		historicalIntensityPrevious = historicalIntensity[historicalIdx].Intensity
		if historicalIdx > 0 {
			historicalIntensityPrevious = historicalIntensity[historicalIdx-1].Intensity
		}

		// Calculate energy for this interval using trapezoid rule
		deltaHours := current.Timestamp.Sub(previous.Timestamp).Hours()
		avgPower := (current.PowerEstimate + previous.PowerEstimate) / 2
		intervalEnergy := (avgPower * deltaHours) / 1000 // kWh

		// Calculate counterfactual emissions using historical intensity
		avgHistoricalIntensity := (historicalIntensityCurrent + historicalIntensityPrevious) / 2
		intervalCounterfactualEmissions := intervalEnergy * avgHistoricalIntensity

		totalCounterfactualEmissions += intervalCounterfactualEmissions
	}

	klog.V(2).InfoS("Calculated counterfactual emissions",
		"pod", klog.KObj(pod),
		"counterfactualEmissions", totalCounterfactualEmissions)

	return totalCounterfactualEmissions
}
