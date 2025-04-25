package computegardener

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics"
)

// metricsCollectionWorker periodically collects pod metrics and updates the store
func (cs *ComputeGardenerScheduler) metricsCollectionWorker(ctx context.Context) {
	// Parse sampling interval from config
	interval, err := time.ParseDuration(cs.config.Metrics.SamplingInterval)
	if err != nil {
		klog.ErrorS(err, "Invalid metrics sampling interval, using default of 15s")
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Create a separate ticker for energy budget tracking updates (every 5 minutes)
	budgetTicker := time.NewTicker(5 * time.Minute)
	defer budgetTicker.Stop()

	klog.V(2).InfoS("Starting metrics collection worker",
		"samplingInterval", interval.String(),
		"budgetTrackingInterval", "5m")

	for {
		select {
		case <-cs.stopCh:
			klog.V(2).InfoS("Stopping metrics collection worker")
			return
		case <-ticker.C:
			cs.collectPodMetrics(ctx)
		case <-budgetTicker.C:
			cs.updateEnergyBudgetTracking(ctx)
		}
	}
}

// collectPodMetrics retrieves current pod metrics and updates the metrics store
func (cs *ComputeGardenerScheduler) collectPodMetrics(ctx context.Context) {
	// Skip if metrics client is not configured
	if cs.coreMetricsClient == nil {
		return
	}

	// Get carbon intensity
	carbonIntensity := 0.0
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		if intensity, err := cs.carbonImpl.GetCurrentIntensity(ctx); err == nil {
			carbonIntensity = intensity
			// Also update the carbon intensity gauge here so we're not dependent on pods to trigger
			metrics.CarbonIntensityGauge.WithLabelValues(cs.config.Carbon.APIConfig.Region).Set(intensity)
			klog.V(2).InfoS("Updated carbon intensity gauge from metrics collector",
				"region", cs.config.Carbon.APIConfig.Region,
				"intensity", intensity)
		} else {
			klog.ErrorS(err, "Failed to get carbon intensity")
		}
	}

	// Get current electricity rate and update gauge - Ensure we're updating this periodically
	// even when not making scheduling decisions
	if cs.config.Pricing.Enabled && cs.priceImpl != nil {
		now := cs.clock.Now()
		currentRate := cs.priceImpl.GetCurrentRate(now)

		// Determine if we're in peak or off-peak period
		isPeak := cs.priceImpl.IsPeakTime(now)
		period := "off-peak"
		if isPeak {
			period = "peak"
		}

		// Update electricity rate gauge
		metrics.ElectricityRateGauge.WithLabelValues("tou", period).Set(currentRate)
		klog.V(2).InfoS("Updated electricity rate gauge from metrics collector",
			"rate", currentRate,
			"period", period,
			"isPeak", isPeak)
	}

	// Get metrics for all pods
	podMetricsList, err := cs.coreMetricsClient.ListPodMetrics(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to list pod metrics")
		return
	}

	klog.V(3).InfoS("Retrieved pod metrics from metrics server",
		"podCount", len(podMetricsList))

	// Get GPU power measurements for all pods if GPU metrics client is configured
	gpuPowers := make(map[string]float64)
	if cs.gpuMetricsClient != nil {
		if powers, err := cs.gpuMetricsClient.ListPodsGPUPower(ctx); err == nil {
			klog.V(2).InfoS("Retrieved GPU power measurements", "count", len(powers), "values", powers)
			gpuPowers = powers
		} else {
			klog.ErrorS(err, "Failed to list GPU power measurements")
		}
	}

	processedCount := 0
	skippedDifferentScheduler := 0
	skippedNoNode := 0
	skippedNotRunning := 0

	// Track active pods and their status for heartbeat monitoring
	activePods := make(map[string]*v1.Pod)

	for _, podMetrics := range podMetricsList {
		// Get pod details
		pod, err := cs.handle.ClientSet().CoreV1().Pods(podMetrics.Namespace).Get(ctx, podMetrics.Name, metav1.GetOptions{})
		if err != nil {
			klog.V(2).InfoS("Failed to get pod details",
				"namespace", podMetrics.Namespace,
				"name", podMetrics.Name,
				"error", err)
			continue
		}

		// Keep track of this pod for heartbeat monitoring
		activePods[string(pod.UID)] = pod

		// Add debugging log to check scheduler names
		klog.V(4).InfoS("Checking pod scheduler name",
			"pod", klog.KObj(pod),
			"podSchedulerName", pod.Spec.SchedulerName,
			"ourSchedulerName", SchedulerName,
			"ourPluginName", Name,
			"match", pod.Spec.SchedulerName == SchedulerName)

		// Skip pods not scheduled by our scheduler - more tolerant check
		if pod.Spec.SchedulerName != SchedulerName && pod.Spec.SchedulerName != Name {
			klog.V(4).InfoS("Skipping pod with different scheduler",
				"pod", klog.KObj(pod),
				"schedulerName", pod.Spec.SchedulerName)
			skippedDifferentScheduler++
			continue
		}

		klog.V(3).InfoS("Found pod using our scheduler",
			"pod", klog.KObj(pod),
			"schedulerName", pod.Spec.SchedulerName)

		// Skip pods not assigned to nodes yet
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			skippedNoNode++
			continue
		}

		// Check if this pod is no longer running - handle completed pods separately below
		if pod.Status.Phase != v1.PodRunning {
			skippedNotRunning++
			klog.V(3).InfoS("Skipping pod not in Running phase",
				"pod", klog.KObj(pod),
				"phase", pod.Status.Phase)
			continue
		}

		// Get GPU power for this pod
		gpuPower := 0.0
		key := podMetrics.Namespace + "/" + podMetrics.Name

		// First try direct mapping by pod name/namespace
		if power, exists := gpuPowers[key]; exists {
			gpuPower = power
			klog.V(2).InfoS("Found direct GPU power measurement for pod", "pod", key, "power", gpuPower)
		} else {
			// Check if this is a GPU pod
			isGPUPod := common.IsGPUPod(pod)

			// For GPU pods, find GPU power measurements from DCGM
			if isGPUPod && len(gpuPowers) > 0 {
				// Use the first GPU power value we find
				for gpuKey, power := range gpuPowers {
					gpuPower = power
					klog.V(2).InfoS("Attributed GPU power to pod",
						"pod", klog.KObj(pod), "gpuKey", gpuKey, "power", power)
					break
				}
			}
		}

		// Calculate pod metrics record
		record := metrics.CalculatePodMetrics(
			&podMetrics,
			pod,
			gpuPower,
			carbonIntensity,
			cs.calculatePodPower,
		)

		// Add electricity rate if pricing implementation is available
		if cs.priceImpl != nil {
			currentRate := cs.priceImpl.GetCurrentRate(time.Now())
			record.ElectricityRate = currentRate
		}

		// Store metrics in cache
		cs.metricsStore.AddRecord(
			string(pod.UID),
			pod.Name,
			pod.Namespace,
			nodeName,
			record,
		)

		// Update current metrics in Prometheus
		metrics.NodeCPUUsage.WithLabelValues(nodeName, pod.Name, "current").Set(record.CPU)
		metrics.NodeMemoryUsage.WithLabelValues(nodeName, pod.Name, "current").Set(record.Memory)
		metrics.NodeGPUPower.WithLabelValues(nodeName, pod.Name, "current").Set(record.GPUPowerWatts)
		metrics.NodePowerEstimate.WithLabelValues(nodeName, pod.Name, "current").Set(record.PowerEstimate)

		processedCount++
	}

	// Perform pod heartbeat check - looking for pods that need final energy calculations
	if cs.metricsStore != nil {
		cs.checkPodsForCompletion(ctx, activePods)
	}

	// Update metrics store stats
	cacheSize := cs.metricsStore.Size()
	metrics.MetricsCacheSize.Set(float64(cacheSize))

	// Update samples stored metric for active pods
	for _, podMetrics := range podMetricsList {
		if hist, found := cs.metricsStore.GetHistory(string(podMetrics.UID)); found {
			metrics.MetricsSamplesStored.WithLabelValues(podMetrics.Name, podMetrics.Namespace).Set(float64(len(hist.Records)))
		}
	}

	// Log metrics collection stats with less frequency (only every 5 minutes or when we've processed pods)
	if processedCount > 0 || time.Now().Minute()%5 == 0 {
		klog.V(2).InfoS("Metrics collection completed",
			"totalPodsFromMetricsAPI", len(podMetricsList),
			"processedPods", processedCount,
			"skippedDifferentScheduler", skippedDifferentScheduler,
			"skippedNoNode", skippedNoNode,
			"skippedNotRunning", skippedNotRunning,
			"cacheSize", cacheSize)
	}
}

// checkPodsForCompletion performs a "heartbeat" check on all tracked pods
// to ensure we properly handle completed pods even if we missed their completion events
func (cs *ComputeGardenerScheduler) checkPodsForCompletion(ctx context.Context, activePods map[string]*v1.Pod) {
	if cs.metricsStore == nil {
		return
	}

	// Get all unique pod UIDs from our metrics store that aren't marked as completed
	trackedPods := make(map[string]bool)

	// Find active pods in our metrics cache
	cs.metricsStore.ForEach(func(podUID string, history *metrics.PodMetricsHistory) {
		// Skip pods already marked as completed - we only care about active ones
		if !history.Completed {
			trackedPods[podUID] = true
		}
	})

	completedCount := 0
	checkedCount := 0

	// Check each tracked pod that's not yet marked as completed
	for podUID := range trackedPods {
		checkedCount++

		// Get pod metrics history
		history, found := cs.metricsStore.GetHistory(podUID)
		if !found || history.Completed {
			continue // Skip if not found or already completed
		}

		// Check if the pod is in our active pods list
		if pod, exists := activePods[podUID]; exists {
			// Even if the pod exists in the active pods list, check if it's in a terminal state
			if isTerminalState(pod) {
				klog.V(2).InfoS("Found pod in terminal state that needs final energy calculation",
					"pod", klog.KObj(pod),
					"podUID", podUID,
					"phase", pod.Status.Phase)

				// Process this pod's metrics as if we caught the completion event normally
				cs.processPodCompletionMetrics(pod, podUID, pod.Name, pod.Namespace, pod.Spec.NodeName)
				completedCount++
				continue
			}
		} else {
			// The pod isn't in our active pods list at all - either it was deleted or
			// it completed and our metrics server didn't capture it in the activePods list

			// Try to fetch the pod directly
			pod, err := cs.handle.ClientSet().CoreV1().Pods(history.Namespace).Get(ctx, history.PodName, metav1.GetOptions{})
			if err != nil {
				// Pod likely doesn't exist anymore
				klog.V(2).InfoS("Pod in metrics cache no longer exists",
					"podUID", podUID,
					"podName", history.PodName,
					"namespace", history.Namespace,
					"error", err)

				// Do final calculations and zero out the metrics
				// We'll create a skeleton pod with the minimum information needed
				skeletonPod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       types.UID(podUID),
						Name:      history.PodName,
						Namespace: history.Namespace,
					},
					Spec: v1.PodSpec{
						NodeName: history.NodeName,
					},
				}

				// Process this pod's completion metrics
				cs.processPodCompletionMetrics(skeletonPod, podUID, history.PodName, history.Namespace, history.NodeName)
				completedCount++
			} else if isTerminalState(pod) {
				// Pod exists but is in a terminal state
				klog.V(2).InfoS("Found completed pod that needs final energy calculation",
					"pod", klog.KObj(pod),
					"podUID", podUID,
					"phase", pod.Status.Phase)

				// Process this pod's metrics as if we caught the completion event normally
				cs.processPodCompletionMetrics(pod, podUID, pod.Name, pod.Namespace, pod.Spec.NodeName)
				completedCount++
			} else {
				// Pod exists but in a non-terminal state - log this unusual situation at high visibility
				klog.V(1).InfoS("Pod in metrics cache not in active list but still exists in non-terminal state",
					"podUID", podUID,
					"podName", history.PodName,
					"namespace", history.Namespace,
					"phase", pod.Status.Phase)
			}
		}
	}

	if completedCount > 0 {
		klog.V(1).InfoS("Pod heartbeat check completed",
			"checkedPods", checkedCount,
			"completedPods", completedCount)
	}
}

// isTerminalState checks if a pod is in a terminal state (completed, failed, or all containers terminated)
// This function is a more comprehensive check than just looking at pod phase
func isTerminalState(pod *v1.Pod) bool {
	// Check pod phase
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return true
	}

	// Check if all containers are terminated
	if len(pod.Status.ContainerStatuses) > 0 {
		allTerminated := true
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Terminated == nil {
				allTerminated = false
				break
			}
		}
		if allTerminated {
			return true
		}
	}

	// Check completion timestamp - if present, the pod has completed regardless of phase
	if pod.DeletionTimestamp != nil {
		return true
	}

	// Additional Kubernetes-specific conditions that indicate pod termination
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady &&
			condition.Status == v1.ConditionFalse &&
			(condition.Reason == "PodCompleted" || condition.Reason == "PodFailed") {
			return true
		}
	}

	return false
}

// calculatePodPower estimates power consumption for a pod based on resource usage
func (cs *ComputeGardenerScheduler) calculatePodPower(nodeName string, cpu, memory, gpuPower float64) float64 {
	// Get node power configuration
	var idlePower, maxPower float64
	var nodePower *config.NodePower
	var hasNodePower bool
	var powerSource string = "unknown"
	var diagnostics []string

	// First try to get the node from the Kubernetes API
	node, err := cs.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("Failed to get node from API: %v", err))
	}

	// Check if we have a hardware profiler and can get a node-specific power profile
	if err == nil && cs.hardwareProfiler != nil {
		// Try to get a node-specific profile based on detected hardware
		profile, profileErr := cs.hardwareProfiler.GetNodePowerProfile(node)

		// Log CPU model annotation for diagnostics
		cpuModel := "missing"
		if val, ok := node.Labels[common.NFDLabelCPUModel]; ok {
			cpuModel = val
		}

		if profileErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("Hardware profile detection error: %v", profileErr))
			diagnostics = append(diagnostics, fmt.Sprintf("Node CPU model annotation: %s", cpuModel))
		}

		if profileErr == nil && profile != nil {
			nodePower = profile
			idlePower = profile.IdlePower
			maxPower = profile.MaxPower
			hasNodePower = true
			powerSource = "hardware-profile"

			klog.V(2).InfoS("Using hardware-profiled power values",
				"node", nodeName,
				"cpuModel", cpuModel,
				"idlePower", idlePower,
				"maxPower", maxPower,
				"hasCPUModelAnnotation", cpuModel != "missing")
		} else {
			diagnostics = append(diagnostics, fmt.Sprintf("Hardware profile failed or nil: err=%v, profile=%v", profileErr, profile != nil))

			// Log more details about the hardware profiler
			if cs.hardwareProfiler != nil && cs.config.Power.HardwareProfiles != nil {
				cpuProfileCount := len(cs.config.Power.HardwareProfiles.CPUProfiles)
				diagnostics = append(diagnostics, fmt.Sprintf("Hardware profiler has %d CPU profiles", cpuProfileCount))

				// Check if the CPU model we have is in the profiles
				if cpuModel != "missing" && cs.config.Power.HardwareProfiles.CPUProfiles != nil {
					_, found := cs.config.Power.HardwareProfiles.CPUProfiles[cpuModel]
					diagnostics = append(diagnostics, fmt.Sprintf("CPU model '%s' exists in profiles: %v", cpuModel, found))
				}
			} else {
				diagnostics = append(diagnostics, "Hardware profiler or profiles is nil")
			}
		}
	} else if err != nil {
		diagnostics = append(diagnostics, "Skipping hardware profile due to node lookup error")
	} else if cs.hardwareProfiler == nil {
		diagnostics = append(diagnostics, "Hardware profiler is nil")
	}

	// If no hardware profile, check manually configured power values
	if !hasNodePower {
		if np, ok := cs.config.Power.NodePowerConfig[nodeName]; ok {
			nodePower = &np
			idlePower = np.IdlePower
			maxPower = np.MaxPower
			hasNodePower = true
			powerSource = "manual-config"
			klog.V(2).InfoS("Using manually configured power values",
				"node", nodeName,
				"idlePower", idlePower,
				"maxPower", maxPower)
		} else {
			// Last resort: use defaults
			idlePower = cs.config.Power.DefaultIdlePower
			maxPower = cs.config.Power.DefaultMaxPower
			hasNodePower = false
			powerSource = "default-values"
			klog.V(2).InfoS("Using default power values (no profile found)",
				"node", nodeName,
				"idlePower", idlePower,
				"maxPower", maxPower,
				"diagnosticsInfo", strings.Join(diagnostics, "; "))
		}
	}

	// Check if we have frequency data for this node
	adjustedIdlePower, adjustedMaxPower := idlePower, maxPower

	if cpuFreqMetric, err := cs.getNodeCPUFrequency(nodeName); err == nil && cpuFreqMetric > 0 {
		// Get the CPU model and its base frequency from hardware profiles
		cpuModel, baseFreq, powerScaling := cs.getNodeCPUModelInfo(nodeName)

		if baseFreq > 0 && cpuFreqMetric > 0 {
			// Calculate frequency ratio
			freqRatio := cpuFreqMetric / baseFreq

			// Apply frequency scaling to power values
			adjustedIdlePower = metrics.AdjustPowerForFrequency(idlePower, freqRatio, powerScaling)
			adjustedMaxPower = metrics.AdjustPowerForFrequency(maxPower, freqRatio, powerScaling)

			klog.V(2).InfoS("Adjusted power values based on CPU frequency",
				"node", nodeName,
				"cpuModel", cpuModel,
				"currentFreq", cpuFreqMetric,
				"baseFreq", baseFreq,
				"freqRatio", freqRatio,
				"originalIdlePower", idlePower,
				"adjustedIdlePower", adjustedIdlePower,
				"originalMaxPower", maxPower,
				"adjustedMaxPower", adjustedMaxPower)
		}
	}

	// Get number of CPU cores for the node to normalize utilization
	var totalCPUCores float64 = 1.0 // Default to 1 if we can't determine
	if node != nil {
		if cpuQuantity, exists := node.Status.Capacity[v1.ResourceCPU]; exists {
			totalCPUCores = float64(cpuQuantity.Value())
		}
	}

	// Normalize CPU utilization to 0-1 range by dividing by total cores
	normalizedCPU := cpu
	if totalCPUCores > 0 {
		normalizedCPU = cpu / totalCPUCores
		// Ensure normalized value doesn't exceed 1.0 (100%)
		if normalizedCPU > 1.0 {
			normalizedCPU = 1.0
		}
	}

	// Power-law model provides more realistic power scaling than linear model
	powerExponent := common.DefaultPowerExponent
	cpuPower := adjustedIdlePower + (adjustedMaxPower-adjustedIdlePower)*math.Pow(normalizedCPU, powerExponent)

	klog.V(3).InfoS("CPU power calculation details",
		"node", nodeName,
		"cpuCores", totalCPUCores,
		"absoluteCpuUsage", cpu,
		"normalizedCpuUsage", normalizedCPU,
		"powerExponent", powerExponent,
		"idlePower", adjustedIdlePower,
		"maxPower", adjustedMaxPower,
		"calculatedCpuPower", cpuPower)

	// Apply GPU PUE factor if we have GPU power
	adjustedGPUPower := 0.0
	if gpuPower > 0 {
		gpuPUE := common.DefaultGPUPUE
		if hasNodePower && nodePower.GPUPUE > 0 {
			gpuPUE = nodePower.GPUPUE
		} else if cs.config.Power.DefaultGPUPUE > 0 {
			gpuPUE = cs.config.Power.DefaultGPUPUE
		}

		adjustedGPUPower = gpuPower * gpuPUE
		klog.V(2).InfoS("GPU power with PUE applied",
			"node", nodeName,
			"rawPower", gpuPower,
			"pue", gpuPUE,
			"adjustedPower", adjustedGPUPower)
	}

	totalPower := cpuPower + adjustedGPUPower

	// Expanded logging with power source information
	klog.V(2).InfoS("Power calculation full breakdown",
		"node", nodeName,
		"powerSource", powerSource,
		"cpuUtilization", cpu,
		"originalIdlePower", idlePower,
		"originalMaxPower", maxPower,
		"adjustedIdlePower", adjustedIdlePower,
		"adjustedMaxPower", adjustedMaxPower,
		"cpuPower", cpuPower,
		"gpuPower", gpuPower,
		"adjustedGPUPower", adjustedGPUPower,
		"totalPower", totalPower)

	// Total power is sum of CPU and GPU power
	// TODO: Add memory power model once we have better data
	return totalPower
}

// getNodeCPUFrequency attempts to get the current CPU frequency for a node from Prometheus
func (cs *ComputeGardenerScheduler) getNodeCPUFrequency(nodeName string) (float64, error) {
	// Check if we have Prometheus client available
	if cs.gpuMetricsClient == nil {
		return 0, fmt.Errorf("prometheus client not available")
	}

	// Get the Prometheus client from the GPU metrics client (which is a PrometheusGPUMetricsClient)
	promClient, ok := cs.gpuMetricsClient.(*metrics.PrometheusGPUMetricsClient)
	if !ok {
		return 0, fmt.Errorf("prometheus client not available (wrong client type)")
	}

	// Query for CPU frequency using the node exporter metric
	// The metric name is defined in common.MetricCPUFrequencyGHz
	// This assumes that our node exporter is exporting this metric
	freq, err := promClient.QueryNodeMetric(context.Background(), common.MetricCPUFrequencyGHz, nodeName)
	if err != nil {
		return 0, fmt.Errorf("failed to query CPU frequency: %v", err)
	}

	// Return the frequency in GHz
	return freq, nil
}

// getNodeCPUModelInfo returns CPU model, base frequency, and power scaling mode
func (cs *ComputeGardenerScheduler) getNodeCPUModelInfo(nodeName string) (string, float64, string) {
	// Attempt to get node from informer cache
	node, err := cs.handle.SharedInformerFactory().Core().V1().Nodes().Lister().Get(nodeName)
	if err != nil {
		klog.V(2).InfoS("Failed to get node for CPU model info", "node", nodeName, "error", err)
		return "", 0.0, "quadratic"
	}

	// Try to get CPU model from node label
	cpuModel := ""
	if model, ok := node.Labels[common.NFDLabelCPUModel]; ok {
		cpuModel = model
		klog.V(2).InfoS("Found CPU model from node annotation", "node", nodeName, "model", cpuModel)
	} else {
		klog.V(2).InfoS("No CPU model annotation found", "node", nodeName)
		return "", 0.0, "quadratic"
	}

	baseFreq := 0.0
	powerScaling := "quadratic" // Default power scaling model

	// Get base frequency from annotation if available
	if freqStr, ok := node.Labels[common.NFDLabelCPUBaseFrequency]; ok {
		if freq, err := strconv.ParseFloat(freqStr, 64); err == nil {
			baseFreq = freq
			klog.V(2).InfoS("Found base frequency from annotation", "node", nodeName, "freq", baseFreq)
		}
	}

	// If not found in annotation, look up in hardware profiles
	if baseFreq == 0.0 && cs.config.Power.HardwareProfiles != nil && cpuModel != "" {
		if profile, exists := cs.config.Power.HardwareProfiles.CPUProfiles[cpuModel]; exists {
			baseFreq = profile.BaseFrequencyGHz
			if profile.PowerScaling != "" {
				powerScaling = profile.PowerScaling
			}
			klog.V(2).InfoS("Using hardware profile frequency data",
				"node", nodeName,
				"cpuModel", cpuModel,
				"baseFrequency", baseFreq,
				"powerScaling", powerScaling)
		}
	}

	return cpuModel, baseFreq, powerScaling
}

// updateEnergyBudgetTracking calculates and reports real-time energy usage against budgets for running pods
func (cs *ComputeGardenerScheduler) updateEnergyBudgetTracking(ctx context.Context) {
	// Skip if metrics store is not configured
	if cs.metricsStore == nil {
		return
	}

	// Get all pods with energy budget annotations
	podList, err := cs.handle.ClientSet().CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to list pods for energy budget tracking")
		return
	}

	totalTracked := 0
	totalExceeded := 0

	for _, pod := range podList.Items {
		// Skip pods that aren't running
		if pod.Status.Phase != v1.PodRunning {
			continue
		}

		// Check if pod has energy budget annotation
		budgetStr, hasBudget := pod.Annotations[common.AnnotationEnergyBudgetKWh]
		if !hasBudget {
			continue
		}

		// Parse budget
		budget, err := strconv.ParseFloat(budgetStr, 64)
		if err != nil || budget <= 0 {
			continue
		}

		// Get pod metrics history
		metricsHistory, found := cs.metricsStore.GetHistory(string(pod.UID))
		if !found || len(metricsHistory.Records) == 0 {
			continue
		}

		// Calculate current energy usage using utility function
		currentEnergyKWh := metrics.CalculateTotalEnergy(metricsHistory.Records)

		// Calculate percentage of budget used
		usagePercent := (currentEnergyKWh / budget) * 100

		// Record metrics
		metrics.EnergyBudgetTracking.WithLabelValues(pod.Name, pod.Namespace).Set(usagePercent)

		// Determine owner reference type for metrics
		ownerKind := "Pod"
		if len(pod.OwnerReferences) > 0 {
			ownerKind = pod.OwnerReferences[0].Kind
		}

		// Check if budget is exceeded
		if currentEnergyKWh > budget {
			// Get action from annotation or default to logging
			action := common.EnergyBudgetActionLog
			if actionVal, ok := pod.Annotations[common.AnnotationEnergyBudgetAction]; ok {
				action = actionVal
			}

			// Only log once per pod when crossing threshold, using an annotation to track
			if _, alreadyExceeded := pod.Annotations[common.AnnotationEnergyBudgetExceeded]; !alreadyExceeded {
				klog.V(2).InfoS("Running pod exceeded energy budget",
					"pod", klog.KObj(&pod),
					"namespace", pod.Namespace,
					"budget", budget,
					"currentUsage", currentEnergyKWh,
					"usagePercent", usagePercent,
					"owner", ownerKind)

				// Execute the action
				cs.handleEnergyBudgetAction(&pod, action, currentEnergyKWh, budget)

				// Update counter
				metrics.EnergyBudgetExceeded.WithLabelValues(pod.Namespace, ownerKind, action).Inc()
				totalExceeded++
			}
		}

		// Log high energy usage (over 80% but not exceeded)
		if usagePercent >= 80 && usagePercent < 100 {
			klog.V(2).InfoS("Pod approaching energy budget",
				"pod", klog.KObj(&pod),
				"namespace", pod.Namespace,
				"budget", budget,
				"currentUsage", currentEnergyKWh,
				"usagePercent", usagePercent,
				"owner", ownerKind)
		}

		totalTracked++
	}

	if totalTracked > 0 {
		klog.V(2).InfoS("Energy budget tracking update completed",
			"podsTracked", totalTracked,
			"podsExceeded", totalExceeded)
	}
}
