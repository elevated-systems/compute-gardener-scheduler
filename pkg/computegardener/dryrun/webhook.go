package dryrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// Webhook handles admission requests for dry-run evaluation
type Webhook struct {
	config    *Config
	evaluator *eval.Evaluator
	podStore  *PodEvaluationStore
}

// NewWebhook creates a new webhook handler
func NewWebhook(config *Config, evaluator *eval.Evaluator, podStore *PodEvaluationStore) *Webhook {
	return &Webhook{
		config:    config,
		evaluator: evaluator,
		podStore:  podStore,
	}
}

// ServeHTTP handles the webhook admission request
func (w *Webhook) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// Read request body
	var body []byte
	if request.Body != nil {
		if data, err := io.ReadAll(request.Body); err == nil {
			body = data
		} else {
			http.Error(writer, fmt.Sprintf("Failed to read request: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Unmarshal admission review
	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		http.Error(writer, fmt.Sprintf("Failed to unmarshal request: %v", err), http.StatusBadRequest)
		return
	}

	// Handle the request
	response := w.handleAdmission(admissionReview.Request)

	// Create response
	responseReview := admissionv1.AdmissionReview{
		TypeMeta: admissionReview.TypeMeta,
		Response: response,
	}
	responseReview.Response.UID = admissionReview.Request.UID

	// Marshal and send response
	responseBytes, err := json.Marshal(responseReview)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	writer.Write(responseBytes)
}

// handleAdmission processes the admission request
func (w *Webhook) handleAdmission(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	// Parse pod from request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		klog.ErrorS(err, "Failed to unmarshal pod")
		return &admissionv1.AdmissionResponse{
			Allowed: true, // Always allow, we're just observing
			Result: &metav1.Status{
				Message: fmt.Sprintf("Failed to unmarshal pod: %v", err),
			},
		}
	}

	// Apply filter mode
	if w.config.FilterMode == FilterModeNamespace {
		// Namespace mode: only evaluate pods in explicitly listed namespaces
		if !w.isNamespaceWatched(req.Namespace) {
			logArgs := []interface{}{
				"namespace", req.Namespace,
				"pod", klog.KObj(&pod),
			}
			if pod.Name == "" && pod.GenerateName != "" {
				logArgs = append(logArgs, "generateName", pod.GenerateName)
			}
			klog.V(4).InfoS("Namespace not in watch list, skipping", logArgs...)
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
	} else {
		// SchedulerName mode (default): only evaluate pods targeting our scheduler
		if pod.Spec.SchedulerName != common.SchedulerName {
			logArgs := []interface{}{
				"pod", klog.KObj(&pod),
				"schedulerName", pod.Spec.SchedulerName,
			}
			if pod.Name == "" && pod.GenerateName != "" {
				logArgs = append(logArgs, "generateName", pod.GenerateName)
			}
			klog.V(5).InfoS("Pod not targeting our scheduler, skipping", logArgs...)
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
	}

	// Build patches - start with schedulerName mutation since it must happen
	// even for opted-out pods (they still need to be schedulable)
	var patches []map[string]interface{}

	// In schedulerName mode, mutate schedulerName back to default-scheduler
	// so the pod can be scheduled by the default scheduler
	if w.config.FilterMode != FilterModeNamespace {
		patches = append(patches, map[string]interface{}{
			"op":    "replace",
			"path":  "/spec/schedulerName",
			"value": common.DefaultSchedulerName,
		})
	}

	// Check if pod is opted out (skip evaluation but still apply schedulerName mutation)
	if pod.Annotations[common.AnnotationSkip] == "true" {
		klog.V(3).InfoS("Pod opted out of evaluation",
			"pod", klog.KObj(&pod))
		return w.buildResponse(patches)
	}

	// Evaluate pod constraints 1s under the Kubernetes webhook timeout so we
	// always respond before the apiserver deadline discards our response.
	evalTimeout := time.Duration(w.config.WebhookTimeoutSeconds-1) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), evalTimeout)
	defer cancel()

	result, err := w.evaluator.EvaluateAll(ctx, &pod, time.Now())
	if err != nil {
		klog.ErrorS(err, "Evaluation failed", "pod", klog.KObj(&pod))
		return w.buildResponse(patches)
	}

	podID := klog.KObj(&pod)
	if pod.Name == "" && pod.GenerateName != "" {
		klog.V(2).InfoS("Evaluated pod",
			"pod", podID,
			"generateName", pod.GenerateName,
			"wouldDelay", result.ShouldDelay,
			"delayType", result.DelayType)
	} else {
		klog.V(2).InfoS("Evaluated pod",
			"pod", podID,
			"wouldDelay", result.ShouldDelay,
			"delayType", result.DelayType)
	}

	// Record metrics (if in metrics mode)
	if w.config.Mode == "metrics" {
		w.recordMetrics(result, &pod)
	}

	// Use admission request UID as tracking ID (pod UID isn't assigned yet at admission time)
	trackingID := string(req.UID)

	// Store initial evaluation for completion tracking (all evaluated pods,
	// not just delayed ones, so we can track runtime/energy for all workloads)
	w.storeInitialEvaluation(&pod, result, trackingID)

	// Ensure /metadata/annotations exists before adding annotation patches
	if pod.Annotations == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": map[string]string{},
		})
	}

	// Always add marker annotations (needed for completion tracking in all modes)
	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/metadata/annotations/" + escapeJSONPointer(common.AnnotationDryRunEvaluated),
		"value": "true",
	})
	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/metadata/annotations/" + escapeJSONPointer(common.AnnotationDryRunTrackingID),
		"value": trackingID,
	})

	// In annotate mode, add full dry-run annotations
	if w.config.Mode == "annotate" {
		annotations := w.createDryRunAnnotations(result)
		// Remove the evaluated key since we already added it above
		delete(annotations, common.AnnotationDryRunEvaluated)

		annotationPatches, err := createAnnotationPatches(annotations)
		if err != nil {
			klog.ErrorS(err, "Failed to create annotation patches", "pod", klog.KObj(&pod))
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
		patches = append(patches, annotationPatches...)
	}

	return w.buildResponse(patches)
}

// buildResponse creates an AdmissionResponse, applying any patches if present.
func (w *Webhook) buildResponse(patches []map[string]interface{}) *admissionv1.AdmissionResponse {
	if len(patches) > 0 {
		patchBytes, err := json.Marshal(patches)
		if err != nil {
			klog.ErrorS(err, "Failed to marshal patches")
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
		patchType := admissionv1.PatchTypeJSONPatch
		return &admissionv1.AdmissionResponse{
			Allowed:   true,
			Patch:     patchBytes,
			PatchType: &patchType,
		}
	}
	return &admissionv1.AdmissionResponse{Allowed: true}
}

// isNamespaceWatched checks if the namespace is in the watch list.
// Empty list = watch nothing (explicit opt-in required).
func (w *Webhook) isNamespaceWatched(namespace string) bool {
	for _, ns := range w.config.WatchNamespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// storeInitialEvaluation stores evaluation data for later completion tracking
func (w *Webhook) storeInitialEvaluation(pod *corev1.Pod, result *eval.EvaluationResult, trackingID string) {
	startData := &eval.PodStartData{
		Namespace:         pod.Namespace,
		Name:              pod.Name,
		UID:               trackingID, // Use tracking ID since pod UID isn't assigned yet at admission
		StartTime:         time.Now(), // Will be updated when pod actually starts
		InitialCarbon:     result.CurrentCarbon,
		InitialPrice:      result.CurrentPrice,
		CarbonThreshold:   result.CarbonThreshold,
		PriceThreshold:    result.PriceThreshold,
		WouldHaveDelayed:  result.ShouldDelay,
		DelayType:         result.DelayType,
		EstimatedPowerW:   result.EstimatedPowerW,
		EstimatedRuntimeH: result.EstimatedRuntimeHours,
	}

	w.podStore.RecordStart(trackingID, startData)
	logKV := []interface{}{
		"pod", klog.KObj(pod),
		"wouldDelay", result.ShouldDelay,
	}
	if pod.Name == "" && pod.GenerateName != "" {
		logKV = append(logKV, "generateName", pod.GenerateName)
	}
	klog.V(3).InfoS("Stored initial evaluation for tracking", logKV...)
}

// createDryRunAnnotations creates annotations for annotate mode
func (w *Webhook) createDryRunAnnotations(result *eval.EvaluationResult) map[string]string {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated: "true",
		common.AnnotationDryRunTimestamp: time.Now().Format(time.RFC3339),
	}

	if result.ShouldDelay {
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
	} else {
		annotations[common.AnnotationDryRunWouldDelay] = "false"
	}

	return annotations
}

// createAnnotationPatches creates JSON patch operations for adding annotations
func createAnnotationPatches(annotations map[string]string) ([]map[string]interface{}, error) {
	var patches []map[string]interface{}

	for key, value := range annotations {
		patch := map[string]interface{}{
			"op":    "add",
			"path":  fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(key)),
			"value": value,
		}
		patches = append(patches, patch)
	}

	return patches, nil
}

// escapeJSONPointer escapes special characters for JSON Pointer (RFC 6901)
func escapeJSONPointer(s string) string {
	s = string([]byte(s)) // Ensure it's a regular string
	// Replace ~ with ~0 and / with ~1
	result := ""
	for _, c := range s {
		switch c {
		case '~':
			result += "~0"
		case '/':
			result += "~1"
		default:
			result += string(c)
		}
	}
	return result
}

// recordMetrics records dry-run metrics
func (w *Webhook) recordMetrics(result *eval.EvaluationResult, pod *corev1.Pod) {
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
