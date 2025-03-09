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
	// Mark pod as completed in metrics store to prevent further collection
	cs.metricsStore.MarkCompleted(podUID)

	// Get pod metrics history
	metricsHistory, found := cs.metricsStore.GetHistory(podUID)
	if !found {
		klog.V(2).InfoS("No metrics history found for pod",
			"pod", klog.KObj(pod),
			"podUID", podUID)
		return
	}

	if len(metricsHistory.Records) == 0 {
		klog.V(2).InfoS("Metrics history is empty for pod",
			"pod", klog.KObj(pod),
			"podUID", podUID)
		return
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
				"timestamp": firstRecord.Timestamp,
				"cpu": firstRecord.CPU,
				"memory": firstRecord.Memory,
				"gpu": firstRecord.GPU,
				"powerEstimate": firstRecord.PowerEstimate,
			},
			"midRecord", map[string]interface{}{
				"timestamp": midRecord.Timestamp,
				"cpu": midRecord.CPU,
				"memory": midRecord.Memory,
				"gpu": midRecord.GPU,
				"powerEstimate": midRecord.PowerEstimate,
			},
			"lastRecord", map[string]interface{}{
				"timestamp": lastRecord.Timestamp,
				"cpu": lastRecord.CPU,
				"memory": lastRecord.Memory,
				"gpu": lastRecord.GPU,
				"powerEstimate": lastRecord.PowerEstimate,
			})
	}
	
	// Calculate energy and carbon emissions using our utility functions
	totalEnergyKWh := metrics.CalculateTotalEnergy(metricsHistory.Records)
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

	// Record the metrics
	JobEnergyUsage.WithLabelValues(podName, namespace).Observe(totalEnergyKWh)
	JobCarbonEmissions.WithLabelValues(podName, namespace).Observe(totalCarbonEmissions)

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
			EnergyBudgetTracking.WithLabelValues(podName, namespace).Set(usagePercent)

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
				EnergyBudgetExceeded.WithLabelValues(namespace, ownerKind, action).Inc()
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
		NodeCPUUsage.WithLabelValues(nodeName, podName, "final").Set(final.CPU)
		NodeMemoryUsage.WithLabelValues(nodeName, podName, "final").Set(final.Memory)
		NodeGPUUsage.WithLabelValues(nodeName, podName, "final").Set(final.GPU)
		NodePowerEstimate.WithLabelValues(nodeName, podName, "final").Set(final.PowerEstimate)
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
