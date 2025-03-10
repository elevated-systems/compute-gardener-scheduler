package computegardener

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			CarbonIntensityGauge.WithLabelValues(cs.config.Carbon.APIConfig.Region).Set(intensity)
			klog.V(2).InfoS("Updated carbon intensity gauge from metrics collector",
				"region", cs.config.Carbon.APIConfig.Region,
				"intensity", intensity)
		} else {
			klog.ErrorS(err, "Failed to get carbon intensity")
		}
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

		// Skip pods not in Running phase
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

		// Store metrics in cache
		cs.metricsStore.AddRecord(
			string(pod.UID),
			pod.Name,
			pod.Namespace,
			nodeName,
			record,
		)

		// Update current metrics in Prometheus
		NodeCPUUsage.WithLabelValues(nodeName, pod.Name, "current").Set(record.CPU)
		NodeMemoryUsage.WithLabelValues(nodeName, pod.Name, "current").Set(record.Memory)
		NodeGPUPower.WithLabelValues(nodeName, pod.Name, "current").Set(record.GPUPowerWatts)
		NodePowerEstimate.WithLabelValues(nodeName, pod.Name, "current").Set(record.PowerEstimate)

		processedCount++
	}

	// Update metrics store stats
	cacheSize := cs.metricsStore.Size()
	MetricsCacheSize.Set(float64(cacheSize))

	// Update samples stored metric for each pod
	for _, podMetrics := range podMetricsList {
		if hist, found := cs.metricsStore.GetHistory(string(podMetrics.UID)); found {
			MetricsSamplesStored.WithLabelValues(podMetrics.Name, podMetrics.Namespace).Set(float64(len(hist.Records)))
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

// calculatePodPower estimates power consumption for a pod based on resource usage
func (cs *ComputeGardenerScheduler) calculatePodPower(nodeName string, cpu, memory, gpuPower float64) float64 {
	// Get node power configuration
	var idlePower, maxPower float64
	var nodePower *config.NodePower
	var hasNodePower bool
	
	// First try to get the node from the Kubernetes API
	node, err := cs.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	
	// Check if we have a hardware profiler and can get a node-specific power profile
	if err == nil && cs.hardwareProfiler != nil {
		// Try to get a node-specific profile based on detected hardware
		profile, profileErr := cs.hardwareProfiler.GetNodePowerProfile(node)
		if profileErr == nil && profile != nil {
			nodePower = profile
			idlePower = profile.IdlePower
			maxPower = profile.MaxPower
			hasNodePower = true
			
			cpuModel := "unknown"
			if val, ok := node.Annotations[common.AnnotationCPUModel]; ok {
				cpuModel = val
			}
			
			klog.V(2).InfoS("Using hardware-profiled power values",
				"node", nodeName,
				"cpuModel", cpuModel,
				"idlePower", idlePower,
				"maxPower", maxPower)
		}
	}
	
	// If no hardware profile, check manually configured power values
	if !hasNodePower {
		if np, ok := cs.config.Power.NodePowerConfig[nodeName]; ok {
			nodePower = &np
			idlePower = np.IdlePower
			maxPower = np.MaxPower
			hasNodePower = true
			klog.V(2).InfoS("Using manually configured power values", 
				"node", nodeName,
				"idlePower", idlePower,
				"maxPower", maxPower)
		} else {
			// Last resort: use defaults
			idlePower = cs.config.Power.DefaultIdlePower
			maxPower = cs.config.Power.DefaultMaxPower
			hasNodePower = false
			klog.V(2).InfoS("Using default power values (no profile found)",
				"node", nodeName,
				"idlePower", idlePower,
				"maxPower", maxPower)
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

	// Power-law model (exponent 1.4) provides more realistic power scaling than linear model
	powerExponent := 1.4
	cpuPower := adjustedIdlePower + (adjustedMaxPower-adjustedIdlePower)*math.Pow(cpu, powerExponent)

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

	klog.V(2).InfoS("Power calculation breakdown",
		"node", nodeName,
		"cpuUtilization", cpu,
		"gpuPower", gpuPower,
		"adjustedGPUPower", adjustedGPUPower,
		"cpuPower", cpuPower,
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

	// Try to get CPU model from node annotations
	cpuModel := ""
	if model, ok := node.Annotations[common.AnnotationCPUModel]; ok {
		cpuModel = model
		klog.V(2).InfoS("Found CPU model from node annotation", "node", nodeName, "model", cpuModel)
	} else {
		klog.V(2).InfoS("No CPU model annotation found", "node", nodeName)
		return "", 0.0, "quadratic"
	}

	baseFreq := 0.0
	powerScaling := "quadratic" // Default power scaling model

	// Get base frequency from annotation if available
	if freqStr, ok := node.Annotations[common.AnnotationCPUBaseFrequency]; ok {
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
		EnergyBudgetTracking.WithLabelValues(pod.Name, pod.Namespace).Set(usagePercent)

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
				EnergyBudgetExceeded.WithLabelValues(pod.Namespace, ownerKind, action).Inc()
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
