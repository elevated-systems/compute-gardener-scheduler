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

	// Check if this namespace is in our watch list
	if !w.isNamespaceWatched(req.Namespace) {
		klog.V(4).InfoS("Namespace not in watch list, skipping",
			"namespace", req.Namespace,
			"pod", klog.KObj(&pod))
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Check if pod is opted out
	if pod.Annotations[common.AnnotationSkip] == "true" {
		klog.V(3).InfoS("Pod opted out of evaluation",
			"pod", klog.KObj(&pod))
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Evaluate pod constraints
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.evaluator.EvaluateAll(ctx, &pod, time.Now())
	if err != nil {
		klog.ErrorS(err, "Evaluation failed", "pod", klog.KObj(&pod))
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	klog.V(2).InfoS("Evaluated pod",
		"pod", klog.KObj(&pod),
		"wouldDelay", result.ShouldDelay,
		"delayType", result.DelayType)

	// Record metrics (if in metrics mode)
	if w.config.Mode == "metrics" {
		w.recordMetrics(result, &pod)
	}

	// Store initial evaluation for completion tracking
	if result.ShouldDelay {
		w.storeInitialEvaluation(&pod, result)
	}

	// Return response based on mode
	if w.config.Mode == "annotate" {
		// Add dry-run annotations to pod
		annotations := w.createDryRunAnnotations(result)
		patch, err := createJSONPatch(annotations)
		if err != nil {
			klog.ErrorS(err, "Failed to create patch", "pod", klog.KObj(&pod))
			return &admissionv1.AdmissionResponse{Allowed: true}
		}

		patchType := admissionv1.PatchTypeJSONPatch
		return &admissionv1.AdmissionResponse{
			Allowed:   true,
			Patch:     patch,
			PatchType: &patchType,
		}
	}

	// Metrics mode - just allow without modifications
	return &admissionv1.AdmissionResponse{Allowed: true}
}

// isNamespaceWatched checks if the namespace is in the watch list
func (w *Webhook) isNamespaceWatched(namespace string) bool {
	// If no namespaces specified, watch all
	if len(w.config.WatchNamespaces) == 0 {
		return true
	}

	for _, ns := range w.config.WatchNamespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// storeInitialEvaluation stores evaluation data for later completion tracking
func (w *Webhook) storeInitialEvaluation(pod *corev1.Pod, result *eval.EvaluationResult) {
	startData := &eval.PodStartData{
		Namespace:         pod.Namespace,
		Name:              pod.Name,
		UID:               string(pod.UID),
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

	w.podStore.RecordStart(string(pod.UID), startData)
	klog.V(3).InfoS("Stored initial evaluation for tracking",
		"pod", klog.KObj(pod),
		"wouldDelay", result.ShouldDelay)
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

// createJSONPatch creates a JSON patch for adding annotations
func createJSONPatch(annotations map[string]string) ([]byte, error) {
	var patches []map[string]interface{}

	for key, value := range annotations {
		patch := map[string]interface{}{
			"op":    "add",
			"path":  fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(key)),
			"value": value,
		}
		patches = append(patches, patch)
	}

	return json.Marshal(patches)
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

// recordMetrics records dry-run metrics (placeholder - will implement with metrics package)
func (w *Webhook) recordMetrics(result *eval.EvaluationResult, pod *corev1.Pod) {
	// TODO: Implement metrics recording
	// This will be implemented in the next step with the metrics package
	klog.V(4).InfoS("Recording metrics",
		"pod", klog.KObj(pod),
		"wouldDelay", result.ShouldDelay,
		"delayType", result.DelayType)
}
