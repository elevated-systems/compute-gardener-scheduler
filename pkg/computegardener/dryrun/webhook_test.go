package dryrun

import (
	"testing"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setupTestWebhook(t *testing.T) (*Webhook, *PodEvaluationStore) {
	t.Helper()

	config := &Config{
		Mode: "annotate",
	}

	evaluator := &eval.Evaluator{}
	podStore := NewPodEvaluationStore()

	webhook := NewWebhook(config, evaluator, podStore)

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

func TestWebhook_IsNamespaceWatched_AllNamespaces(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	webhook.config.WatchNamespaces = []string{}

	// Should watch all namespaces
	if !webhook.isNamespaceWatched("any-namespace") {
		t.Error("Expected to watch all namespaces")
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
