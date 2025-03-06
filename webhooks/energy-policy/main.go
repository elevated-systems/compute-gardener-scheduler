package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

// EnergyPolicyWebhook is a webhook that applies energy policies from namespaces to pods
type EnergyPolicyWebhook struct {
	kubeClient kubernetes.Interface
}

// NewEnergyPolicyWebhook creates a new webhook instance
func NewEnergyPolicyWebhook() (*EnergyPolicyWebhook, error) {
	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %v", err)
	}

	// Create Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &EnergyPolicyWebhook{
		kubeClient: kubeClient,
	}, nil
}

// Apply policies from namespace to pod if not already set
func (w *EnergyPolicyWebhook) applyNamespacePolicies(namespace *corev1.Namespace, pod *corev1.Pod) (map[string]string, error) {
	podAnnotations := pod.Annotations
	if podAnnotations == nil {
		podAnnotations = make(map[string]string)
	}

	// Skip if the pod opted out of compute-gardener scheduling
	if podAnnotations[common.AnnotationSkip] == "true" {
		return podAnnotations, nil
	}

	// Determine workload type based on pod labels or owner references
	workloadType := determineWorkloadType(pod)

	// Apply namespace-level policy annotations
	for k, v := range namespace.Annotations {
		// Look for policy annotations that need to be applied
		if isEnergyPolicyAnnotation(k) {
			// If pod doesn't have this annotation, copy from namespace
			podKey := convertNamespacePolicyToAnnotation(k)
			if _, exists := podAnnotations[podKey]; !exists {
				// Check if we have a workload-specific override
				workloadKey := fmt.Sprintf("%s%s", common.AnnotationWorkloadTypePrefix, workloadType)
				if workloadValue, hasWorkloadOverride := namespace.Annotations[workloadKey+"-"+k]; hasWorkloadOverride {
					// Apply workload-specific override
					podAnnotations[podKey] = workloadValue
					klog.V(4).InfoS("Applied workload-specific policy", 
						"pod", klog.KObj(pod),
						"policy", podKey, 
						"value", workloadValue,
						"workloadType", workloadType)
				} else {
					// Apply namespace default
					podAnnotations[podKey] = v
					klog.V(4).InfoS("Applied namespace policy", 
						"pod", klog.KObj(pod),
						"policy", podKey, 
						"value", v)
				}
			}
		}
	}

	return podAnnotations, nil
}

// isEnergyPolicyAnnotation checks if an annotation is a compute-gardener policy
func isEnergyPolicyAnnotation(key string) bool {
	return len(key) > len(common.AnnotationNamespacePolicyPrefix) && 
		key[:len(common.AnnotationNamespacePolicyPrefix)] == common.AnnotationNamespacePolicyPrefix
}

// convertNamespacePolicyToAnnotation converts a namespace policy key to a pod annotation key
func convertNamespacePolicyToAnnotation(namespaceKey string) string {
	// Remove the policy prefix to get the actual annotation name
	return namespaceKey[len(common.AnnotationNamespacePolicyPrefix):]
}

// determineWorkloadType identifies the type of workload (batch, service, etc.)
func determineWorkloadType(pod *corev1.Pod) string {
	// Check for explicit type label
	if typeLabel, ok := pod.Labels[common.LabelWorkloadType]; ok {
		return typeLabel
	}

	// Check owner references
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0]
		switch owner.Kind {
		case "Job", "CronJob":
			return common.WorkloadTypeBatch
		case "Deployment", "ReplicaSet":
			return common.WorkloadTypeService
		case "StatefulSet":
			return common.WorkloadTypeStateful
		case "DaemonSet":
			return common.WorkloadTypeSystem
		}
	}

	// Default to "generic" if we can't determine type
	return common.WorkloadTypeGeneric
}

// Serve handles admission requests
func (w *EnergyPolicyWebhook) Serve(review admissionv1.AdmissionReview) (*admissionv1.AdmissionResponse, error) {
	req := review.Request
	var pod corev1.Pod

	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Failed to unmarshal pod: %v", err),
			},
		}, nil
	}

	// Get the namespace for this pod
	namespace, err := w.kubeClient.CoreV1().Namespaces().Get(context.Background(), req.Namespace, metav1.GetOptions{})
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Failed to get namespace: %v", err),
			},
		}, nil
	}

	// Apply namespace policies
	newAnnotations, err := w.applyNamespacePolicies(namespace, &pod)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Failed to apply namespace policies: %v", err),
			},
		}, nil
	}

	// If annotations are unchanged, skip the patch
	if equalAnnotations(pod.Annotations, newAnnotations) {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}, nil
	}

	// Prepare the patch to modify annotations
	originalPod := pod.DeepCopy()
	originalPod.Annotations = newAnnotations

	// Create patch
	patch, err := createPatch(&pod, originalPod)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Failed to create patch: %v", err),
			},
		}, nil
	}

	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patch,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}, nil
}

// createPatch creates a JSON patch to update pod annotations
func createPatch(current, modified *corev1.Pod) ([]byte, error) {
	// Set only the annotations
	current.Annotations = modified.Annotations

	// Marshal the modified pod (which now has updated annotations)
	modifiedBytes, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}

	// Create and return patch
	return modifiedBytes, nil
}

// equalAnnotations checks if two annotation maps are identical
func equalAnnotations(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v1 := range a {
		if v2, ok := b[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

// ServeHTTP implements http.Handler
func (w *EnergyPolicyWebhook) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// Read request body
	var body []byte
	if request.Body != nil {
		if data, err := io.ReadAll(request.Body); err == nil {
			body = data
		}
	}

	// Unmarshal request
	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		http.Error(writer, fmt.Sprintf("Failed to unmarshal request: %v", err), http.StatusBadRequest)
		return
	}

	// Call Serve to handle the admission request
	response, err := w.Serve(admissionReview)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error serving admission request: %v", err), http.StatusInternalServerError)
		return
	}

	// Create response
	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: admissionReview.TypeMeta,
		Response: response,
	}

	// Marshal response
	responseBytes, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	writer.Write(responseBytes)
}

func main() {
	var port int
	flag.IntVar(&port, "port", 8443, "Port to listen on")
	flag.Parse()

	// Initialize webhook
	webhook, err := NewEnergyPolicyWebhook()
	if err != nil {
		klog.ErrorS(err, "Failed to create webhook")
		os.Exit(1)
	}

	// Start server
	klog.InfoS("Starting energy policy webhook", "port", port)
	http.HandleFunc("/mutate", webhook.ServeHTTP)
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
	}

	if err := server.ListenAndServeTLS("tls.crt", "tls.key"); err != nil {
		klog.ErrorS(err, "Failed to start webhook server")
		os.Exit(1)
	}
}