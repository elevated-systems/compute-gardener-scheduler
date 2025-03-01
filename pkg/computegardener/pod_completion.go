package computegardener

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// handlePodCompletion records metrics when a pod completes using time-series metrics
func (cs *ComputeGardenerScheduler) handlePodCompletion(pod *v1.Pod) {
	klog.V(2).InfoS("Pod completion handler triggered", 
		"pod", pod.Name, 
		"namespace", pod.Namespace,
		"phase", pod.Status.Phase,
		"nodeName", pod.Spec.NodeName)
	
	// Skip if we don't have the metrics store configured
	if cs.metricsStore == nil {
		klog.V(2).InfoS("Skipping pod completion metrics - metrics store not configured",
			"pod", pod.Name,
			"namespace", pod.Namespace)
		return
	}

	podUID := string(pod.UID)
	podName := pod.Name
	namespace := pod.Namespace
	nodeName := pod.Spec.NodeName

	// Mark pod as completed in metrics store to prevent further collection
	cs.metricsStore.MarkCompleted(podUID)

	// Get pod metrics history
	metricsHistory, found := cs.metricsStore.GetHistory(podUID)
	if !found || len(metricsHistory.Records) == 0 {
		klog.V(2).InfoS("No metrics history found for completed pod",
			"pod", podName,
			"namespace", namespace)
		return
	}

	// Calculate energy usage by numerical integration (trapezoid rule)
	totalEnergyKWh := 0.0
	totalCarbonEmissions := 0.0

	// Record pod duration
	var podDuration float64
	if pod.Status.StartTime != nil && len(metricsHistory.Records) > 0 {
		startTime := pod.Status.StartTime.Time
		endTime := cs.clock.Now()
		
		// Use the timestamp of the last record if available
		if len(metricsHistory.Records) > 0 {
			endTime = metricsHistory.Records[len(metricsHistory.Records)-1].Timestamp
		}
		
		podDuration = endTime.Sub(startTime).Hours()
	}

	// Integrate over the time series
	for i := 1; i < len(metricsHistory.Records); i++ {
		current := metricsHistory.Records[i]
		previous := metricsHistory.Records[i-1]

		// Time difference in hours
		deltaHours := current.Timestamp.Sub(previous.Timestamp).Hours()

		// Average power during this interval (W)
		avgPower := (current.PowerEstimate + previous.PowerEstimate) / 2

		// Energy used in this interval (kWh)
		intervalEnergy := (avgPower * deltaHours) / 1000

		// Average carbon intensity during this interval (gCO2eq/kWh)
		avgCarbonIntensity := (current.CarbonIntensity + previous.CarbonIntensity) / 2

		// Carbon emissions for this interval (gCO2eq)
		intervalCarbon := intervalEnergy * avgCarbonIntensity

		totalEnergyKWh += intervalEnergy
		totalCarbonEmissions += intervalCarbon
	}

	// Record final metrics
	klog.V(2).InfoS("Recording final job metrics to Prometheus", 
		"pod", podName,
		"namespace", namespace,
		"energyKWh", totalEnergyKWh,
		"carbonEmissions", totalCarbonEmissions,
		"metricSamples", len(metricsHistory.Records))
	
	// Validate values before recording
	if totalEnergyKWh <= 0 {
		klog.V(2).InfoS("Warning: Zero or negative energy value being recorded", 
			"pod", podName, 
			"namespace", namespace,
			"value", totalEnergyKWh)
	}
	if totalCarbonEmissions <= 0 {
		klog.V(2).InfoS("Warning: Zero or negative carbon emissions value being recorded", 
			"pod", podName, 
			"namespace", namespace,
			"value", totalCarbonEmissions)
	}
	
	JobEnergyUsage.WithLabelValues(podName, namespace).Observe(totalEnergyKWh)
	JobCarbonEmissions.WithLabelValues(podName, namespace).Observe(totalCarbonEmissions)

	// Set final metrics for the pod
	if len(metricsHistory.Records) > 0 {
		final := metricsHistory.Records[len(metricsHistory.Records)-1]
		NodeCPUUsage.WithLabelValues(nodeName, podName, "final").Set(final.CPU)
		NodeMemoryUsage.WithLabelValues(nodeName, podName, "final").Set(final.Memory)
		NodeGPUUsage.WithLabelValues(nodeName, podName, "final").Set(final.GPU)
		NodePowerEstimate.WithLabelValues(nodeName, podName, "final").Set(final.PowerEstimate)
	}

	klog.V(2).InfoS("Calculated energy usage for completed pod",
		"pod", podName,
		"namespace", namespace,
		"nodeName", nodeName,
		"durationHours", podDuration,
		"energyKWh", totalEnergyKWh,
		"carbonEmissions", totalCarbonEmissions,
		"metricSamples", len(metricsHistory.Records))
}