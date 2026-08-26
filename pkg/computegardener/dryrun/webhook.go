package dryrun

import (
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
)

// Webhook handles admission requests for dry-run evaluation.
//
// Its only mutation is rewriting spec.schedulerName to the default scheduler,
// which is what lets a pod that opted in to compute-gardener still be scheduled
// while dry-run is only observing. Everything else (evaluating carbon and price
// constraints, recording metrics, annotating) happens in CompletionController
// once the pod has been persisted and has a UID.
type Webhook struct {
	config       *Config
	pendingStore *PendingStore
}

// NewWebhook creates a new webhook handler
func NewWebhook(config *Config, pendingStore *PendingStore) *Webhook {
	return &Webhook{
		config:       config,
		pendingStore: pendingStore,
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

	// The webhook exists to rewrite schedulerName. Namespace filter mode leaves
	// pods untouched, so the controller watches those namespaces on its own and
	// nothing needs to happen here.
	if w.config.FilterMode == FilterModeNamespace {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Only pods that opted in by targeting our scheduler need rewriting. The
	// MutatingWebhookConfiguration carries the same condition in CEL, so the
	// apiserver normally filters these out before calling us.
	if pod.Spec.SchedulerName != common.SchedulerName {
		klog.V(5).InfoS("Pod not targeting our scheduler, skipping",
			"pod", podRef(&pod),
			"schedulerName", pod.Spec.SchedulerName)
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Hand the pod back to the default scheduler. Opted-out pods are rewritten
	// too, since they still have to be schedulable, but are not tracked.
	patch := []map[string]interface{}{{
		"op":    "replace",
		"path":  "/spec/schedulerName",
		"value": common.DefaultSchedulerName,
	}}

	if pod.Annotations[common.AnnotationSkip] == "true" {
		klog.V(3).InfoS("Pod opted out of evaluation", "pod", podRef(&pod))
		return buildResponse(patch)
	}

	// Record the pod for the controller to claim once the apiserver has
	// persisted it and assigned a UID
	w.pendingStore.Record(&pod, time.Now())
	klog.V(4).InfoS("Recorded pod for dry-run tracking", "pod", podRef(&pod))

	return buildResponse(patch)
}

// buildResponse creates an AdmissionResponse carrying the given JSON patch.
func buildResponse(patch []map[string]interface{}) *admissionv1.AdmissionResponse {
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal patch")
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}

// podRef identifies a pod in logs, falling back to generateName for pods that
// are not yet named at admission time.
func podRef(pod *corev1.Pod) string {
	if pod.Name != "" {
		return pod.Namespace + "/" + pod.Name
	}
	return pod.Namespace + "/" + pod.GenerateName + "(generated)"
}
