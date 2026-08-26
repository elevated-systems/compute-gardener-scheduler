package dryrun

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// createPodAndSettle creates a pod and waits for the informer to deliver it
func createPodAndSettle(t *testing.T, controller *CompletionController, ctx context.Context, pod *corev1.Pod) {
	t.Helper()
	if _, err := controller.kubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
}

// TestController_EvaluatesClaimedPod covers the handoff the marker annotations
// used to carry: the webhook records the admission, the controller claims it and
// evaluates against the real pod UID.
func TestController_EvaluatesClaimedPod(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runController(t, controller, ctx)
	time.Sleep(200 * time.Millisecond)

	// The webhook saw this pod at admission, before it had a name or a UID
	admitted := ownedPod("", "batch-", "default", "job-uid")
	controller.pendingStore.Record(admitted, time.Now())

	persisted := ownedPod("batch-abc12", "batch-", "default", "job-uid")
	persisted.UID = "persisted-uid"
	createPodAndSettle(t, controller, ctx, persisted)
	cancel()

	startData, found := podStore.GetStart("persisted-uid")
	if !found {
		t.Fatal("Expected claimed pod to be evaluated and tracked")
	}
	if startData.UID != "persisted-uid" {
		t.Errorf("Expected tracking keyed on pod UID, got %q", startData.UID)
	}
	if controller.pendingStore.Count() != 0 {
		t.Error("Expected the admission to be consumed")
	}
}

func TestController_UnclaimedPodIsNotEvaluated(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runController(t, controller, ctx)
	time.Sleep(200 * time.Millisecond)

	// No admission recorded: this pod never targeted our scheduler
	pod := ownedPod("other-abc12", "other-", "default", "job-uid")
	pod.UID = "other-uid"
	createPodAndSettle(t, controller, ctx, pod)
	cancel()

	if _, found := podStore.GetStart("other-uid"); found {
		t.Error("Expected a pod with no matching admission to be ignored")
	}
}

// TestController_NamespaceModeEvaluatesWithoutWebhook covers the mode where
// pods are never mutated at all.
func TestController_NamespaceModeEvaluatesWithoutWebhook(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)
	controller.config.FilterMode = FilterModeNamespace

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runController(t, controller, ctx)
	time.Sleep(200 * time.Millisecond)

	pod := ownedPod("plain-pod", "", "default", "")
	pod.UID = "plain-uid"
	createPodAndSettle(t, controller, ctx, pod)
	cancel()

	if _, found := podStore.GetStart("plain-uid"); !found {
		t.Error("Expected pods in a watched namespace to be evaluated without a pending admission")
	}
}

func TestController_OptedOutPodIsNotEvaluated(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)
	controller.config.FilterMode = FilterModeNamespace

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runController(t, controller, ctx)
	time.Sleep(200 * time.Millisecond)

	pod := ownedPod("skipped-pod", "", "default", "")
	pod.UID = "skipped-uid"
	pod.Annotations = map[string]string{common.AnnotationSkip: "true"}
	createPodAndSettle(t, controller, ctx, pod)
	cancel()

	if _, found := podStore.GetStart("skipped-uid"); found {
		t.Error("Expected opted-out pod not to be evaluated")
	}
}

// TestController_AnnotateModeWritesAnnotations checks that annotate mode still
// surfaces its result on the pod, now written after the pod is persisted.
func TestController_AnnotateModeWritesAnnotations(t *testing.T) {
	controller, _ := setupTestCompletionController(t)
	controller.config.FilterMode = FilterModeNamespace
	controller.config.Mode = "annotate"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runController(t, controller, ctx)
	time.Sleep(200 * time.Millisecond)

	pod := ownedPod("annotated-pod", "", "default", "")
	pod.UID = "annotated-uid"
	createPodAndSettle(t, controller, ctx, pod)
	cancel()

	updated, err := controller.kubeClient.CoreV1().Pods("default").Get(ctx, "annotated-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to read back pod: %v", err)
	}

	if updated.Annotations[common.AnnotationDryRunEvaluated] != "true" {
		t.Errorf("Expected evaluated annotation, got %v", updated.Annotations)
	}
	for key := range updated.Annotations {
		if strings.Contains(key, "tracking-id") {
			t.Errorf("Expected no tracking annotation on the pod, found %q", key)
		}
	}
}

// --- Annotation payload ---

func TestCreateDryRunAnnotations_WouldDelay(t *testing.T) {
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

	annotations := createDryRunAnnotations(result)

	expected := map[string]string{
		common.AnnotationDryRunEvaluated:              "true",
		common.AnnotationDryRunWouldDelay:             "true",
		common.AnnotationDryRunDelayType:              "carbon",
		common.AnnotationDryRunReason:                 "High carbon intensity",
		common.AnnotationDryRunCarbonIntensity:        "1.20",
		common.AnnotationDryRunPrice:                  "0.0500",
		common.AnnotationDryRunEstimatedCarbonSavings: "50.00",
		common.AnnotationDryRunEstimatedCostSavings:   "2.5000",
	}

	for key, want := range expected {
		if got := annotations[key]; got != want {
			t.Errorf("Annotation %s = %q, want %q", key, got, want)
		}
	}
}

func TestCreateDryRunAnnotations_WouldNotDelay(t *testing.T) {
	result := &eval.EvaluationResult{
		ShouldDelay:       false,
		ReasonDescription: "Conditions acceptable",
		CurrentCarbon:     0.5,
		CarbonThreshold:   0.8,
	}

	annotations := createDryRunAnnotations(result)

	if annotations[common.AnnotationDryRunWouldDelay] != "false" {
		t.Error("Expected would-delay annotation to be false")
	}
	if _, exists := annotations[common.AnnotationDryRunDelayType]; exists {
		t.Error("Expected no delay type annotation")
	}
}
