package dryrun

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// pendingSweepInterval is how often unclaimed admissions are discarded
const pendingSweepInterval = time.Minute

// CompletionController evaluates pods once they are persisted, then watches for
// their completion and calculates actual savings.
//
// Evaluation runs here rather than in the webhook so that it stays off the pod
// creation path, and so tracking can key on the real pod UID instead of state
// written onto the user's pod.
type CompletionController struct {
	kubeClient   kubernetes.Interface
	config       *Config
	podStore     *PodEvaluationStore
	pendingStore *PendingStore
	evaluator    *eval.Evaluator
}

// NewCompletionController creates a new completion controller
func NewCompletionController(
	kubeClient kubernetes.Interface,
	config *Config,
	podStore *PodEvaluationStore,
	pendingStore *PendingStore,
	evaluator *eval.Evaluator,
) *CompletionController {
	return &CompletionController{
		kubeClient:   kubeClient,
		config:       config,
		podStore:     podStore,
		pendingStore: pendingStore,
		evaluator:    evaluator,
	}
}

// Run starts the completion controller
func (c *CompletionController) Run(ctx context.Context) error {
	klog.InfoS("Starting dry-run completion controller",
		"filterMode", c.config.FilterMode,
		"watchNamespaces", c.config.WatchNamespaces)

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(c.kubeClient, 30*time.Second)

	// Setup pod informer with filtering
	podInformer := informerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			pod := extractPod(obj)
			if pod == nil {
				return false
			}
			return c.isNamespaceWatched(pod.Namespace)
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					return
				}
				c.handlePodAdd(ctx, pod)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldPod, ok := oldObj.(*corev1.Pod)
				if !ok {
					return
				}
				newPod, ok := newObj.(*corev1.Pod)
				if !ok {
					return
				}
				if !c.isTracked(newPod) {
					return
				}

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
				if pod != nil && pod.Spec.NodeName != "" && c.isTracked(pod) {
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

	go c.runPendingSweep(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	klog.InfoS("Dry-run completion controller stopped")
	return nil
}

// runPendingSweep periodically discards admissions no pod ever claimed
func (c *CompletionController) runPendingSweep(ctx context.Context) {
	ticker := time.NewTicker(pendingSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pendingStore.Sweep(time.Now())
		}
	}
}

// isTracked reports whether the pod has been evaluated and is being tracked
func (c *CompletionController) isTracked(pod *corev1.Pod) bool {
	_, ok := c.podStore.GetStart(string(pod.UID))
	return ok
}

// shouldEvaluate reports whether a newly observed pod belongs to this dry-run.
//
// In schedulerName mode the pod's opt-in was consumed by the webhook's
// schedulerName rewrite, so the claim on the pending store is the only evidence
// it targeted us. In namespace mode nothing mutates the pod at all and every
// pod in a watched namespace is evaluated.
func (c *CompletionController) shouldEvaluate(pod *corev1.Pod) bool {
	if pod.Annotations[common.AnnotationSkip] == "true" {
		return false
	}

	if c.config.FilterMode == FilterModeNamespace {
		return true
	}

	return c.pendingStore.Claim(pod, time.Now())
}

// handlePodAdd evaluates a pod the first time it is observed
func (c *CompletionController) handlePodAdd(ctx context.Context, pod *corev1.Pod) {
	if c.isTracked(pod) {
		// Relist of a pod already evaluated
		return
	}
	if !c.shouldEvaluate(pod) {
		return
	}

	result, err := c.evaluator.EvaluateAll(ctx, pod, time.Now())
	if err != nil {
		klog.ErrorS(err, "Evaluation failed", "pod", klog.KObj(pod))
		return
	}

	klog.V(2).InfoS("Evaluated pod",
		"pod", klog.KObj(pod),
		"wouldDelay", result.ShouldDelay,
		"delayType", result.DelayType)

	// Track every evaluated pod, not just delayed ones, so runtime and energy
	// are recorded for all workloads
	c.storeInitialEvaluation(pod, result)

	if c.config.Mode == "metrics" {
		recordMetrics(result, pod)
	}

	if c.config.Mode == "annotate" {
		c.annotatePod(ctx, pod, result)
	}

	// A pod that was already running when we observed it still needs its start
	// time recorded, which the update handler would otherwise have missed
	if pod.Status.StartTime != nil {
		c.handlePodStart(pod)
	}
}

// storeInitialEvaluation stores evaluation data for later completion tracking
func (c *CompletionController) storeInitialEvaluation(pod *corev1.Pod, result *eval.EvaluationResult) {
	startData := &eval.PodStartData{
		Namespace:         pod.Namespace,
		Name:              pod.Name,
		UID:               string(pod.UID),
		StartTime:         time.Now(), // Updated once the pod actually starts
		InitialCarbon:     result.CurrentCarbon,
		InitialPrice:      result.CurrentPrice,
		CarbonThreshold:   result.CarbonThreshold,
		PriceThreshold:    result.PriceThreshold,
		WouldHaveDelayed:  result.ShouldDelay,
		DelayType:         result.DelayType,
		EstimatedPowerW:   result.EstimatedPowerW,
		EstimatedRuntimeH: result.EstimatedRuntimeHours,
	}

	c.podStore.RecordStart(string(pod.UID), startData)
	klog.V(3).InfoS("Stored initial evaluation for tracking",
		"pod", klog.KObj(pod),
		"wouldDelay", result.ShouldDelay)
}

// annotatePod writes the evaluation result onto the pod in annotate mode
func (c *CompletionController) annotatePod(ctx context.Context, pod *corev1.Pod, result *eval.EvaluationResult) {
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": createDryRunAnnotations(result),
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal annotation patch", "pod", klog.KObj(pod))
		return
	}

	_, err = c.kubeClient.CoreV1().Pods(pod.Namespace).Patch(
		ctx, pod.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to annotate pod", "pod", klog.KObj(pod))
		return
	}

	klog.V(3).InfoS("Annotated pod with dry-run result", "pod", klog.KObj(pod))
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
	startData, found := c.podStore.GetStart(string(pod.UID))
	if !found {
		klog.V(3).InfoS("No initial evaluation found for pod start", "pod", klog.KObj(pod))
		return
	}
	if pod.Status.StartTime == nil {
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
	uid := string(pod.UID)

	// Retrieve start data
	startData, found := c.podStore.GetStart(uid)
	if !found {
		klog.V(3).InfoS("No start data found for completed pod", "pod", klog.KObj(pod))
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
		klog.V(2).InfoS("Invalid runtime for pod, skipping completion tracking",
			"pod", klog.KObj(pod),
			"startTime", startData.StartTime,
			"completionTime", completionTime)
		c.podStore.Remove(uid)
		return
	}

	// Calculate actual energy consumed (using estimated power × actual runtime)
	actualEnergyKWh := (startData.EstimatedPowerW / 1000.0) * actualRuntimeHours

	// Always record completion count, runtime, and energy metrics
	PodsCompletedTotal.WithLabelValues(pod.Namespace).Inc()
	PodRuntimeHours.WithLabelValues(pod.Namespace).Observe(actualRuntimeHours)
	PodEnergyConsumptionKWh.WithLabelValues(pod.Namespace).Observe(actualEnergyKWh)

	// Only calculate savings if pod would have been delayed
	if !startData.WouldHaveDelayed {
		klog.V(2).InfoS("Pod completed - no delay would have occurred",
			"pod", klog.KObj(pod),
			"runtime", fmt.Sprintf("%.2fh", actualRuntimeHours),
			"energy", fmt.Sprintf("%.3f kWh", actualEnergyKWh))
		c.podStore.Remove(uid)
		return
	}

	// Calculate savings for pods that would have been delayed
	savings := c.calculateEstimatedSavings(startData, actualEnergyKWh, actualRuntimeHours)

	klog.InfoS("Pod completed - calculated potential savings",
		"pod", klog.KObj(pod),
		"runtime", fmt.Sprintf("%.2fh", actualRuntimeHours),
		"energy", fmt.Sprintf("%.3f kWh", actualEnergyKWh),
		"carbonSavings", fmt.Sprintf("%.2f gCO2eq", savings.CarbonGCO2),
		"costSavings", fmt.Sprintf("$%.4f", savings.CostUSD))

	// Record savings metrics
	if savings.CarbonGCO2 > 0 {
		ActualCarbonSavingsTotal.WithLabelValues(pod.Namespace).Add(savings.CarbonGCO2)
	}
	if savings.CostUSD > 0 {
		ActualCostSavingsTotal.WithLabelValues(pod.Namespace).Add(savings.CostUSD)
	}

	// Clean up from store
	c.podStore.Remove(uid)
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

// isPodCompleted checks if a pod has completed
func isPodCompleted(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
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

// createDryRunAnnotations creates annotations describing the evaluation result
func createDryRunAnnotations(result *eval.EvaluationResult) map[string]string {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated: "true",
		common.AnnotationDryRunTimestamp: time.Now().Format(time.RFC3339),
	}

	if !result.ShouldDelay {
		annotations[common.AnnotationDryRunWouldDelay] = "false"
		return annotations
	}

	annotations[common.AnnotationDryRunWouldDelay] = "true"
	annotations[common.AnnotationDryRunDelayType] = result.DelayType
	annotations[common.AnnotationDryRunReason] = result.ReasonDescription

	// Add current conditions
	if result.CurrentCarbon > 0 {
		annotations[common.AnnotationDryRunCarbonIntensity] = fmt.Sprintf("%.2f", result.CurrentCarbon)
		annotations[common.AnnotationDryRunCarbonThreshold] = fmt.Sprintf("%.2f", result.CarbonThreshold)
	}

	if result.CurrentPrice > 0 {
		annotations[common.AnnotationDryRunPrice] = fmt.Sprintf("%.4f", result.CurrentPrice)
		annotations[common.AnnotationDryRunPriceThreshold] = fmt.Sprintf("%.4f", result.PriceThreshold)
	}

	// Add estimated savings
	if result.EstimatedCarbonSavingsGCO2 > 0 {
		annotations[common.AnnotationDryRunEstimatedCarbonSavings] = fmt.Sprintf("%.2f", result.EstimatedCarbonSavingsGCO2)
	}
	if result.EstimatedCostSavingsUSD > 0 {
		annotations[common.AnnotationDryRunEstimatedCostSavings] = fmt.Sprintf("%.4f", result.EstimatedCostSavingsUSD)
	}

	return annotations
}

// recordMetrics records dry-run metrics
func recordMetrics(result *eval.EvaluationResult, pod *corev1.Pod) {
	// Count all evaluated pods
	PodsEvaluatedTotal.WithLabelValues(pod.Namespace).Inc()

	// Record pods that would be delayed
	if result.ShouldDelay {
		PodsWouldDelayTotal.WithLabelValues(pod.Namespace, result.DelayType).Inc()

		// Record estimated savings
		if result.EstimatedCarbonSavingsGCO2 > 0 {
			EstimatedCarbonSavingsTotal.WithLabelValues(pod.Namespace).Add(result.EstimatedCarbonSavingsGCO2)
		}
		if result.EstimatedCostSavingsUSD > 0 {
			EstimatedCostSavingsTotal.WithLabelValues(pod.Namespace).Add(result.EstimatedCostSavingsUSD)
		}
	}

	// Record current conditions as gauges
	if result.CurrentCarbon > 0 {
		CurrentCarbonIntensity.WithLabelValues(pod.Namespace).Set(result.CurrentCarbon)
	}
	if result.CurrentPrice > 0 {
		CurrentElectricityPrice.WithLabelValues(pod.Namespace).Set(result.CurrentPrice)
	}
}
