package computegardener

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/metrics"
)

// metricsCollectionWorker periodically collects pod metrics and updates the store
func (cs *ComputeGardenerScheduler) metricsCollectionWorker(ctx context.Context) {
	// Parse sampling interval from config
	interval, err := time.ParseDuration(cs.config.Metrics.SamplingInterval)
	if err != nil {
		klog.ErrorS(err, "Invalid metrics sampling interval, using default of 30s")
		interval = 30 * time.Second
	}

	klog.V(2).InfoS("Starting metrics collection worker", 
		"interval", interval.String(),
		"maxSamplesPerPod", cs.config.Metrics.MaxSamplesPerPod)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			cs.collectPodMetrics(ctx)
		}
	}
}

// collectPodMetrics retrieves current pod metrics and updates the metrics store
func (cs *ComputeGardenerScheduler) collectPodMetrics(ctx context.Context) {
	// Skip if metrics client is not configured
	if cs.coreMetricsClient == nil {
		klog.V(2).Info("Skipping metrics collection - metrics client not configured")
		return
	}

	// Get carbon intensity
	carbonIntensity := 0.0
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		if intensity, err := cs.carbonImpl.GetCurrentIntensity(ctx); err == nil {
			carbonIntensity = intensity
		} else {
			klog.V(2).InfoS("Failed to get carbon intensity", "error", err)
		}
	}

	// Get metrics for all pods
	podMetricsList, err := cs.coreMetricsClient.ListPodMetrics(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to list pod metrics")
		return
	}

	// Get GPU utilization for all pods if GPU metrics client is configured
	gpuUtilizations := make(map[string]float64)
	if cs.gpuMetricsClient != nil {
		if utils, err := cs.gpuMetricsClient.ListPodsGPUUtilization(ctx); err == nil {
			gpuUtilizations = utils
		} else {
			klog.V(2).InfoS("Failed to list GPU utilizations", "error", err)
		}
	}

	processedCount := 0
	for _, podMetrics := range podMetricsList {
		// Get pod details
		pod, err := cs.handle.ClientSet().CoreV1().Pods(podMetrics.Namespace).Get(ctx, podMetrics.Name, metav1.GetOptions{})
		if err != nil {
			klog.V(2).InfoS("Failed to get pod info", 
				"pod", podMetrics.Name, 
				"namespace", podMetrics.Namespace, 
				"error", err)
			continue
		}

		// Skip pods not assigned to nodes yet
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}

		// Skip pods not in Running phase
		if pod.Status.Phase != v1.PodRunning {
			continue
		}

		// Get GPU utilization for this pod
		gpuUtilization := 0.0
		key := podMetrics.Namespace + "/" + podMetrics.Name
		if util, exists := gpuUtilizations[key]; exists {
			gpuUtilization = util
		}

		// Calculate pod metrics record
		record := metrics.CalculatePodMetrics(
			&podMetrics,
			pod,
			gpuUtilization,
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
		NodeGPUUsage.WithLabelValues(nodeName, pod.Name, "current").Set(record.GPU)
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

	klog.V(2).InfoS("Collected pod metrics", 
		"collectedCount", processedCount, 
		"totalPodsInCache", cacheSize)
}

// calculatePodPower estimates power consumption for a pod based on resource usage
func (cs *ComputeGardenerScheduler) calculatePodPower(nodeName string, cpu, memory, gpu float64) float64 {
	// Get node power configuration
	var idlePower, maxPower, idleGPUPower, maxGPUPower float64
	if nodePower, ok := cs.config.Power.NodePowerConfig[nodeName]; ok {
		idlePower = nodePower.IdlePower
		maxPower = nodePower.MaxPower
		idleGPUPower = nodePower.IdleGPUPower
		maxGPUPower = nodePower.MaxGPUPower
	} else {
		idlePower = cs.config.Power.DefaultIdlePower
		maxPower = cs.config.Power.DefaultMaxPower
		// Default GPU power settings (only used if GPU utilization > 0)
		idleGPUPower = 50  // Default idle GPU power (W)
		maxGPUPower = 300  // Default max GPU power (W)
	}

	// Linear interpolation between idle and max power based on CPU usage
	cpuPower := idlePower + (maxPower-idlePower)*cpu

	// Add GPU power if utilization > 0
	gpuPower := 0.0
	if gpu > 0 {
		gpuPower = idleGPUPower + (maxGPUPower-idleGPUPower)*gpu
	}

	// Total power is sum of CPU and GPU power
	// TODO: Add memory power model once we have better data
	return cpuPower + gpuPower
}