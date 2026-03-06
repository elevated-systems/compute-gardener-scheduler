package dryrun

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// CompletionController watches for pod completions and calculates actual savings
type CompletionController struct {
	kubeClient kubernetes.Interface
	config     *Config
	podStore   *PodEvaluationStore
}

// NewCompletionController creates a new completion controller
func NewCompletionController(kubeClient kubernetes.Interface, config *Config, podStore *PodEvaluationStore) *CompletionController {
	return &CompletionController{
		kubeClient: kubeClient,
		config:     config,
		podStore:   podStore,
	}
}

// Run starts the completion controller
func (c *CompletionController) Run(ctx context.Context) error {
	klog.InfoS("Starting dry-run completion controller",
		"watchNamespaces", c.config.WatchNamespaces)

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(c.kubeClient, 30*time.Second)

	// Setup pod informer with filtering
	podInformer := informerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				// Handle tombstone objects
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return false
				}
				pod, ok = tombstone.Obj.(*corev1.Pod)
				if !ok {
					return false
				}
			}

			// Only track pods that were evaluated by dry-run webhook
			return c.wasEvaluated(pod) && c.isNamespaceWatched(pod.Namespace)
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod := obj.(*corev1.Pod)
				// Record pod start time when it gets scheduled and starts running
				if pod.Status.StartTime != nil {
					c.handlePodStart(pod)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldPod := oldObj.(*corev1.Pod)
				newPod := newObj.(*corev1.Pod)

				// Check if pod just got a start time
				if oldPod.Status.StartTime == nil && newPod.Status.StartTime != nil {
					c.handlePodStart(newPod)
				}

				// Check for completion
				if isPodCompleted(newPod) {
					c.handlePodCompletion(newPod)
				}
			},
			DeleteFunc: func(obj interface{}) {
				pod := extractPod(obj)
				if pod != nil && pod.Spec.NodeName != "" {
					c.handlePodCompletion(pod)
				}
			},
		},
	})

	// Start informer
	informerFactory.Start(ctx.Done())

	// Wait for cache sync
	if !cache.WaitForCacheSync(ctx.Done(), podInformer.HasSynced) {
		return ctx.Err()
	}

	klog.InfoS("Dry-run completion controller cache synced")

	// Wait for context cancellation
	<-ctx.Done()
	klog.InfoS("Dry-run completion controller stopped")
	return nil
}

// wasEvaluated checks if the pod was evaluated by the dry-run webhook
func (c *CompletionController) wasEvaluated(pod *corev1.Pod) bool {
	_, ok := pod.Annotations[common.AnnotationDryRunEvaluated]
	return ok
}

// isNamespaceWatched checks if the namespace is in the watch list
func (c *CompletionController) isNamespaceWatched(namespace string) bool {
	// If no namespaces specified, watch all
	if len(c.config.WatchNamespaces) == 0 {
		return true
	}

	for _, ns := range c.config.WatchNamespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// handlePodStart records when a pod actually starts running
func (c *CompletionController) handlePodStart(pod *corev1.Pod) {
	// Check if we have initial evaluation data for this pod
	startData, found := c.podStore.GetStart(string(pod.UID))
	if !found {
		klog.V(3).InfoS("No initial evaluation found for pod start",
			"pod", klog.KObj(pod))
		return
	}

	// Update the actual start time
	startData.StartTime = pod.Status.StartTime.Time

	// Store the updated data
	c.podStore.RecordStart(string(pod.UID), startData)

	klog.V(2).InfoS("Recorded actual pod start time",
		"pod", klog.KObj(pod),
		"startTime", startData.StartTime)
}

// handlePodCompletion calculates savings using actual runtime
func (c *CompletionController) handlePodCompletion(pod *corev1.Pod) {
	// Retrieve start data
	startData, found := c.podStore.GetStart(string(pod.UID))
	if !found {
		klog.V(3).InfoS("No start data found for completed pod",
			"pod", klog.KObj(pod))
		return
	}

	// Only calculate savings if pod would have been delayed
	if !startData.WouldHaveDelayed {
		c.podStore.Remove(string(pod.UID))
		return
	}

	// Calculate actual runtime
	completionTime := time.Now()
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		// Try to get more accurate completion time from container statuses
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Terminated != nil {
				completionTime = status.State.Terminated.FinishedAt.Time
				break
			}
		}
	}

	actualRuntimeHours := completionTime.Sub(startData.StartTime).Hours()
	if actualRuntimeHours <= 0 {
		klog.V(2).InfoS("Invalid runtime for pod, skipping savings calculation",
			"pod", klog.KObj(pod),
			"startTime", startData.StartTime,
			"completionTime", completionTime)
		c.podStore.Remove(string(pod.UID))
		return
	}

	// Calculate actual energy consumed (using estimated power × actual runtime)
	actualEnergyKWh := (startData.EstimatedPowerW / 1000.0) * actualRuntimeHours

	// Calculate estimated savings (conservative: current - threshold)
	savings := c.calculateEstimatedSavings(startData, actualEnergyKWh, actualRuntimeHours)

	klog.InfoS("Pod completed - calculated estimated savings",
		"pod", klog.KObj(pod),
		"runtime", fmt.Sprintf("%.2fh", actualRuntimeHours),
		"energy", fmt.Sprintf("%.3f kWh", actualEnergyKWh),
		"carbonSavings", fmt.Sprintf("%.2f gCO2eq", savings.CarbonGCO2),
		"costSavings", fmt.Sprintf("$%.4f", savings.CostUSD))

	// Record actual savings metrics
	c.recordActualSavings(savings, pod)

	// Clean up from store
	c.podStore.Remove(string(pod.UID))
}

// calculateEstimatedSavings calculates conservative savings estimates
func (c *CompletionController) calculateEstimatedSavings(
	startData *eval.PodStartData,
	actualEnergyKWh float64,
	actualRuntimeHours float64,
) *eval.EstimatedSavings {
	savings := &eval.EstimatedSavings{
		EnergyKWh:    actualEnergyKWh,
		RuntimeHours: actualRuntimeHours,
	}

	// Conservative estimate: assume pod would have run at threshold (not current)
	if startData.DelayType == "carbon" || startData.DelayType == "both" {
		carbonDelta := startData.InitialCarbon - startData.CarbonThreshold
		if carbonDelta > 0 {
			savings.CarbonGCO2 = carbonDelta * actualEnergyKWh
		}
	}

	if startData.DelayType == "price" || startData.DelayType == "both" {
		priceDelta := startData.InitialPrice - startData.PriceThreshold
		if priceDelta > 0 {
			savings.CostUSD = priceDelta * actualEnergyKWh
		}
	}

	return savings
}

// recordActualSavings records savings metrics using actual runtime
func (c *CompletionController) recordActualSavings(savings *eval.EstimatedSavings, pod *corev1.Pod) {
	// Count completed pods
	PodsCompletedTotal.WithLabelValues(pod.Namespace).Inc()

	// Record actual savings
	if savings.CarbonGCO2 > 0 {
		ActualCarbonSavingsTotal.WithLabelValues(pod.Namespace).Add(savings.CarbonGCO2)
	}
	if savings.CostUSD > 0 {
		ActualCostSavingsTotal.WithLabelValues(pod.Namespace).Add(savings.CostUSD)
	}

	// Record runtime and energy histograms
	PodRuntimeHours.WithLabelValues(pod.Namespace).Observe(savings.RuntimeHours)
	PodEnergyConsumptionKWh.WithLabelValues(pod.Namespace).Observe(savings.EnergyKWh)
}

// isPodCompleted checks if a pod has completed
func isPodCompleted(pod *corev1.Pod) bool {
	switch pod.Status.Phase {
	case corev1.PodSucceeded, corev1.PodFailed:
		return true
	}

	// Also check container statuses
	if len(pod.Status.ContainerStatuses) > 0 {
		allTerminated := true
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Terminated == nil {
				allTerminated = false
				break
			}
		}
		return allTerminated
	}

	return false
}

// extractPod extracts a pod from an object, handling tombstones
func extractPod(obj interface{}) *corev1.Pod {
	pod, ok := obj.(*corev1.Pod)
	if ok {
		return pod
	}

	// Handle tombstone
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil
	}

	pod, ok = tombstone.Obj.(*corev1.Pod)
	if !ok {
		return nil
	}

	return pod
}
