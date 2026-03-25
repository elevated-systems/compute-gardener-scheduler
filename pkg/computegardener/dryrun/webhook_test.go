package dryrun

import (
	"encoding/json"
	"testing"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func setupTestWebhook(t *testing.T) (*Webhook, *PodEvaluationStore) {
	t.Helper()

	cfg := &Config{
		Mode:       "annotate",
		FilterMode: FilterModeSchedulerName,
	}

	// Create evaluator with minimal config (no carbon/price implementations)
	evalCfg := &config.Config{}
	evaluator := eval.NewEvaluator(nil, nil, evalCfg)
	podStore := NewPodEvaluationStore()

	webhook := NewWebhook(cfg, evaluator, podStore)

	return webhook, podStore
}

func TestWebhook_CreateDryRunAnnotations_WouldDelay(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	result := &eval.EvaluationResult{
		ShouldDelay:                true,
		DelayType:                  "carbon",
		ReasonDescription:          "High carbon intensity",
		CurrentCarbon:              1.2,
		CarbonThreshold:            0.8,
		CurrentPrice:               0.05,
		PriceThreshold:             0.04,
		EstimatedCarbonSavingsGCO2: 50.0,
		EstimatedCostSavingsUSD:    2.5,
	}

	annotations := webhook.createDryRunAnnotations(result)

	if annotations[common.AnnotationDryRunEvaluated] != "true" {
		t.Errorf("Expected evaluated annotation to be true")
	}

	if annotations[common.AnnotationDryRunWouldDelay] != "true" {
		t.Errorf("Expected would-delay annotation to be true")
	}

	if annotations[common.AnnotationDryRunDelayType] != "carbon" {
		t.Errorf("Expected delay type to be 'carbon', got %s", annotations[common.AnnotationDryRunDelayType])
	}

	if annotations[common.AnnotationDryRunReason] != "High carbon intensity" {
		t.Errorf("Expected reason to be 'High carbon intensity'")
	}

	if annotations[common.AnnotationDryRunCarbonIntensity] != "1.20" {
		t.Errorf("Expected carbon intensity '1.20', got %s", annotations[common.AnnotationDryRunCarbonIntensity])
	}

	if annotations[common.AnnotationDryRunPrice] != "0.0500" {
		t.Errorf("Expected price '0.0500', got %s", annotations[common.AnnotationDryRunPrice])
	}

	if annotations[common.AnnotationDryRunEstimatedCarbonSavings] != "50.00" {
		t.Errorf("Expected savings '50.00', got %s", annotations[common.AnnotationDryRunEstimatedCarbonSavings])
	}
}

func TestWebhook_CreateDryRunAnnotations_WouldNotDelay(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	result := &eval.EvaluationResult{
		ShouldDelay:       false,
		DelayType:         "",
		ReasonDescription: "Conditions acceptable",
		CurrentCarbon:     0.5,
		CarbonThreshold:   0.8,
	}

	annotations := webhook.createDryRunAnnotations(result)

	if annotations[common.AnnotationDryRunWouldDelay] != "false" {
		t.Errorf("Expected would-delay annotation to be false")
	}

	if _, exists := annotations[common.AnnotationDryRunDelayType]; exists {
		t.Errorf("Expected delay type annotation not to exist")
	}
}

func TestWebhook_StoreInitialEvaluation(t *testing.T) {
	webhook, podStore := setupTestWebhook(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "test-container"},
			},
		},
	}

	result := &eval.EvaluationResult{
		ShouldDelay:           true,
		DelayType:             "carbon",
		CurrentCarbon:         1.2,
		CurrentPrice:          0.05,
		CarbonThreshold:       0.8,
		PriceThreshold:        0.04,
		EstimatedPowerW:       100.0,
		EstimatedRuntimeHours: 2.0,
	}

	webhook.storeInitialEvaluation(pod, result)

	// Verify storage
	startData, found := podStore.GetStart("test-uid")
	if !found {
		t.Fatal("Expected start data to be stored")
	}

	if startData.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got %s", startData.Namespace)
	}

	if !startData.WouldHaveDelayed {
		t.Errorf("Expected WouldHaveDelayed to be true")
	}

	if startData.DelayType != "carbon" {
		t.Errorf("Expected DelayType 'carbon', got %s", startData.DelayType)
	}

	if startData.InitialCarbon != 1.2 {
		t.Errorf("Expected InitialCarbon 1.2, got %f", startData.InitialCarbon)
	}

	if startData.EstimatedPowerW != 100.0 {
		t.Errorf("Expected EstimatedPowerW 100.0, got %f", startData.EstimatedPowerW)
	}

	if startData.EstimatedRuntimeH != 2.0 {
		t.Errorf("Expected EstimatedRuntimeH 2.0, got %f", startData.EstimatedRuntimeH)
	}
}

// --- Namespace filtering tests ---

func TestWebhook_IsNamespaceWatched_EmptyList(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.WatchNamespaces = []string{}

	// Empty list = watch nothing
	if webhook.isNamespaceWatched("any-namespace") {
		t.Error("Expected empty namespace list to watch nothing")
	}
}

func TestWebhook_IsNamespaceWatched_SpecificNamespaces(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	webhook.config.WatchNamespaces = []string{"production", "staging"}

	tests := []struct {
		namespace     string
		expectedWatch bool
	}{
		{"production", true},
		{"staging", true},
		{"default", false},
		{"dev", false},
	}

	for _, tt := range tests {
		result := webhook.isNamespaceWatched(tt.namespace)
		if result != tt.expectedWatch {
			t.Errorf("isNamespaceWatched(%q) = %v, want %v", tt.namespace, result, tt.expectedWatch)
		}
	}
}

// --- Filter mode tests ---

func makeAdmissionRequest(t *testing.T, pod *corev1.Pod) *admissionv1.AdmissionRequest {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("Failed to marshal pod: %v", err)
	}
	return &admissionv1.AdmissionRequest{
		Namespace: pod.Namespace,
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}
}

func TestWebhook_SchedulerNameMode_MatchingPod(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeSchedulerName

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			SchedulerName: common.SchedulerName,
			Containers:    []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if !resp.Allowed {
		t.Error("Expected pod to be allowed")
	}

	// Should have a patch (at minimum the schedulerName mutation)
	if resp.Patch == nil {
		t.Error("Expected patch for matching pod in schedulerName mode")
	}
}

func TestWebhook_SchedulerNameMode_NonMatchingPod(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeSchedulerName

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			SchedulerName: "default-scheduler",
			Containers:    []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if !resp.Allowed {
		t.Error("Expected non-matching pod to be allowed")
	}

	// Should NOT have a patch — skipped entirely
	if resp.Patch != nil {
		t.Error("Expected no patch for non-matching pod")
	}
}

func TestWebhook_SchedulerNameMode_EmptySchedulerName(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeSchedulerName

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-pod",
			Namespace: "kube-system",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if !resp.Allowed {
		t.Error("Expected pod to be allowed")
	}

	if resp.Patch != nil {
		t.Error("Expected no patch for pod without schedulerName")
	}
}

func TestWebhook_SchedulerNameMutation(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeSchedulerName
	webhook.config.Mode = "metrics" // Even in metrics mode, schedulerName should be mutated

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			SchedulerName: common.SchedulerName,
			Containers:    []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if resp.Patch == nil {
		t.Fatal("Expected patch with schedulerName mutation")
	}

	var patches []map[string]interface{}
	if err := json.Unmarshal(resp.Patch, &patches); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	// Find the schedulerName patch
	found := false
	for _, p := range patches {
		if p["path"] == "/spec/schedulerName" {
			found = true
			if p["op"] != "replace" {
				t.Errorf("Expected op 'replace', got %v", p["op"])
			}
			if p["value"] != common.DefaultSchedulerName {
				t.Errorf("Expected value %q, got %v", common.DefaultSchedulerName, p["value"])
			}
		}
	}

	if !found {
		t.Error("Expected /spec/schedulerName patch operation")
	}
}

func TestWebhook_NamespaceMode_EmptyList(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeNamespace
	webhook.config.WatchNamespaces = []string{}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if !resp.Allowed {
		t.Error("Expected pod to be allowed")
	}

	// Should skip — no namespaces configured
	if resp.Patch != nil {
		t.Error("Expected no patch when namespace list is empty")
	}
}

func TestWebhook_NamespaceMode_ExplicitList(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeNamespace
	webhook.config.WatchNamespaces = []string{"production"}

	// Pod in watched namespace — should be evaluated
	watchedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-pod",
			Namespace: "production",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, watchedPod)
	resp := webhook.handleAdmission(req)
	if !resp.Allowed {
		t.Error("Expected watched pod to be allowed")
	}

	// Pod NOT in watched namespace — should be skipped
	unwatchedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dev-pod",
			Namespace: "development",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	req = makeAdmissionRequest(t, unwatchedPod)
	resp = webhook.handleAdmission(req)
	if !resp.Allowed {
		t.Error("Expected unwatched pod to be allowed")
	}
	if resp.Patch != nil {
		t.Error("Expected no patch for pod outside watched namespace")
	}
}

func TestWebhook_NamespaceMode_NoSchedulerNameMutation(t *testing.T) {
	webhook, _ := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeNamespace
	webhook.config.Mode = "annotate"
	webhook.config.WatchNamespaces = []string{"default"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	req := makeAdmissionRequest(t, pod)
	resp := webhook.handleAdmission(req)

	if resp.Patch == nil {
		// In annotate mode with a watched namespace, there should be annotation patches
		// but no schedulerName mutation
		return
	}

	var patches []map[string]interface{}
	if err := json.Unmarshal(resp.Patch, &patches); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	for _, p := range patches {
		if p["path"] == "/spec/schedulerName" {
			t.Error("Should NOT have schedulerName mutation in namespace mode")
		}
	}
}
