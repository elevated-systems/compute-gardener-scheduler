package computegardener

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// handlePodCompletion records metrics when a pod completes using time-series metrics
func (cs *ComputeGardenerScheduler) handlePodCompletion(pod *v1.Pod) {
	// Skip if we don't have the metrics store configured
	if cs.metricsStore == nil {
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
		return
	}

	// Calculate energy usage by numerical integration (trapezoid rule)
	totalEnergyKWh := 0.0
	totalCarbonEmissions := 0.0

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

	// Validate values before recording
	if totalEnergyKWh <= 0 {
		klog.ErrorS(nil, "Warning: Zero or negative energy value being recorded",
			"pod", podName,
			"namespace", namespace,
			"value", totalEnergyKWh)
	}
	if totalCarbonEmissions <= 0 {
		klog.ErrorS(nil, "Warning: Zero or negative carbon emissions value being recorded",
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

}
