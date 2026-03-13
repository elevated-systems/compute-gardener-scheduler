package dryrun

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// setupTestCompletionController creates a controller with a fake client and returns
// it alongside the pod store. The controller is not started — callers use
// startControllerWithPod when they need a running controller with a pre-created pod.
func setupTestCompletionController(t *testing.T) (*CompletionController, *PodEvaluationStore) {
	t.Helper()

	config := &Config{
		WatchNamespaces: []string{"default"},
	}

	podStore := NewPodEvaluationStore()
	fakeClient := fake.NewSimpleClientset()
	controller := NewCompletionController(fakeClient, config, podStore)

	return controller, podStore
}

// runController starts the controller in a goroutine, tolerating context.Canceled
// on shutdown (normal when the test cancels the context).
func runController(t *testing.T, controller *CompletionController, ctx context.Context) {
	t.Helper()
	go func() {
		if err := controller.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("Run error: %v", err)
		}
	}()
}

// controllerTestEnv bundles everything a controller integration test needs.
type controllerTestEnv struct {
	Controller *CompletionController
	PodStore   *PodEvaluationStore
	Pod        *corev1.Pod
	Ctx        context.Context
	Cancel     context.CancelFunc
}

// setupControllerWithPod creates a controller, starts it, and pre-creates a pod
// in the fake client so subsequent Updates work. It waits for the informer cache
// to sync before returning.
func setupControllerWithPod(t *testing.T, pod *corev1.Pod) *controllerTestEnv {
	t.Helper()

	controller, podStore := setupTestCompletionController(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	runController(t, controller, ctx)

	// Wait for controller to start and cache to sync
	time.Sleep(200 * time.Millisecond)

	// Pre-create the pod in the fake client
	_, err := controller.kubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		cancel()
		t.Fatalf("Failed to create pod: %v", err)
	}

	// Let informer pick up the Create
	time.Sleep(200 * time.Millisecond)

	return &controllerTestEnv{
		Controller: controller,
		PodStore:   podStore,
		Pod:        pod,
		Ctx:        ctx,
		Cancel:     cancel,
	}
}

// --- Controller lifecycle tests ---

func TestCompletionController_RunsSuccessfully(t *testing.T) {
	controller, _ := setupTestCompletionController(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runController(t, controller, ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestCompletionController_ContextCancellation(t *testing.T) {
	controller, _ := setupTestCompletionController(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runController(t, controller, ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

// --- Filtering tests ---

func TestCompletionController_NonEvaluatedPodsIgnored(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runController(t, controller, ctx)

	time.Sleep(200 * time.Millisecond)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-evaluated-pod",
			Namespace: "default",
			UID:       "non-evaluated-uid",
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	_, err := controller.kubeClient.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	cancel()

	if _, found := podStore.GetStart("non-evaluated-uid"); found {
		t.Error("Expected no data for non-evaluated pod")
	}
}

func TestCompletionController_UnwatchedNamespaceIgnored(t *testing.T) {
	controller, podStore := setupTestCompletionController(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runController(t, controller, ctx)

	time.Sleep(200 * time.Millisecond)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unwatched-pod",
			Namespace: "other",
			UID:       "unwatched-uid",
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	_, err := controller.kubeClient.CoreV1().Pods("other").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	cancel()

	if _, found := podStore.GetStart("unwatched-uid"); found {
		t.Error("Expected no data for unwatched namespace")
	}
}

// --- Pod lifecycle tracking tests ---

func TestCompletionController_PodStartTimeTracking(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Annotations: map[string]string{
				common.AnnotationDryRunEvaluated: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test-container"}},
		},
	}

	env := setupControllerWithPod(t, pod)
	defer env.Cancel()

	// Store initial evaluation
	env.PodStore.RecordStart("test-uid", &eval.PodStartData{
		Namespace:        "default",
		UID:              "test-uid",
		StartTime:        time.Now(),
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.2,
		InitialPrice:     0.05,
		EstimatedPowerW:  100.0,
	})

	// Update pod with start time
	now := time.Now()
	env.Pod.Status.StartTime = &metav1.Time{Time: now}

	_, err := env.Controller.kubeClient.CoreV1().Pods("default").Update(env.Ctx, env.Pod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	env.Cancel()

	updatedData, found := env.PodStore.GetStart("test-uid")
	if !found {
		t.Fatal("Expected start data to be stored")
	}

	if !updatedData.StartTime.Equal(now) {
		t.Errorf("Expected start time %v, got %v", now, updatedData.StartTime)
	}
}

func TestCompletionController_CompletedPodSavings(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Annotations: map[string]string{
				common.AnnotationDryRunEvaluated: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test-container"}},
		},
	}

	env := setupControllerWithPod(t, pod)
	defer env.Cancel()

	startTime := time.Now().Add(-2 * time.Hour)
	env.PodStore.RecordStart("test-uid", &eval.PodStartData{
		Namespace:        "default",
		UID:              "test-uid",
		StartTime:        startTime,
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.2,
		InitialPrice:     0.05,
		EstimatedPowerW:  100.0,
	})

	// Update pod to completed status
	now := time.Now()
	env.Pod.Status.Phase = corev1.PodSucceeded
	env.Pod.Status.StartTime = &metav1.Time{Time: startTime}
	env.Pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  "test-container",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: metav1.Time{Time: now}}},
		},
	}

	_, err := env.Controller.kubeClient.CoreV1().Pods("default").Update(env.Ctx, env.Pod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	env.Cancel()

	if _, found := env.PodStore.GetStart("test-uid"); found {
		t.Error("Expected pod to be removed from store after completion")
	}
}

func TestCompletionController_DidNotDelayPodNoSavings(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-delayed-pod",
			Namespace: "default",
			UID:       "non-delayed-uid",
			Annotations: map[string]string{
				common.AnnotationDryRunEvaluated: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test-container"}},
		},
	}

	env := setupControllerWithPod(t, pod)
	defer env.Cancel()

	startTime := time.Now().Add(-1 * time.Hour)
	env.PodStore.RecordStart("non-delayed-uid", &eval.PodStartData{
		Namespace:        "default",
		UID:              "non-delayed-uid",
		StartTime:        startTime,
		WouldHaveDelayed: false,
	})

	// Update pod to completed status
	env.Pod.Status.Phase = corev1.PodSucceeded
	env.Pod.Status.StartTime = &metav1.Time{Time: startTime}

	_, err := env.Controller.kubeClient.CoreV1().Pods("default").Update(env.Ctx, env.Pod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	env.Cancel()

	if _, found := env.PodStore.GetStart("non-delayed-uid"); found {
		t.Error("Expected pod to be removed from store (no savings for non-delayed pods)")
	}
}

func TestCompletionController_TombstoneHandling(t *testing.T) {
	controller, _ := setupTestCompletionController(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runController(t, controller, ctx)

	time.Sleep(200 * time.Millisecond)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deleted-pod",
			Namespace: "default",
			UID:       "deleted-uid",
		},
		Spec: corev1.PodSpec{NodeName: "test-node"},
	}

	_, err := controller.kubeClient.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	err = controller.kubeClient.CoreV1().Pods("default").Delete(ctx, "deleted-pod", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete pod: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	cancel()
}

// --- Unit tests for pure functions ---

func TestIsPodCompleted(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name:     "succeeded pod",
			pod:      &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
			expected: true,
		},
		{
			name:     "failed pod",
			pod:      &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
			expected: true,
		},
		{
			name:     "running pod",
			pod:      &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			expected: false,
		},
		{
			name: "pending pod with terminated containers",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "test", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}},
					},
				},
			},
			expected: true,
		},
		{
			name:     "nil pod",
			pod:      nil,
			expected: false,
		},
		{
			name:     "pod with empty status",
			pod:      &corev1.Pod{Status: corev1.PodStatus{}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodCompleted(tt.pod); got != tt.expected {
				t.Errorf("isPodCompleted() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractPod(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}

	tests := []struct {
		name     string
		obj      interface{}
		expected *corev1.Pod
	}{
		{"pod object", pod, pod},
		{"tombstone", cache.DeletedFinalStateUnknown{Obj: pod}, pod},
		{"nil object", nil, nil},
		{"wrong type", &corev1.Node{}, nil},
		{"tombstone with wrong type", cache.DeletedFinalStateUnknown{Obj: &corev1.Node{}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPod(tt.obj); got != tt.expected {
				t.Errorf("extractPod() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// --- Store tests (also exercised via controller, but good to test in isolation) ---

func TestPodEvaluationStore_CRUD(t *testing.T) {
	store := NewPodEvaluationStore()

	now := time.Now()

	data := &eval.PodStartData{
		Namespace:        "default",
		UID:              "test-uid-1",
		StartTime:        now,
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.2,
		InitialPrice:     0.05,
		EstimatedPowerW:  100.0,
	}
	store.RecordStart("test-uid-1", data)

	got, found := store.GetStart("test-uid-1")
	if !found {
		t.Fatal("Expected to find stored data")
	}
	if got.UID != "test-uid-1" {
		t.Errorf("Expected UID test-uid-1, got %s", got.UID)
	}

	// Non-existent get
	if _, found = store.GetStart("non-existent-uid"); found {
		t.Error("Expected not to find non-existent data")
	}

	// Remove
	store.Remove("test-uid-1")
	if _, found = store.GetStart("test-uid-1"); found {
		t.Error("Expected data to be removed")
	}
}

func TestPodEvaluationStore_Cleanup(t *testing.T) {
	store := NewPodEvaluationStore()

	now := time.Now()

	for _, uid := range []string{"uid-1", "uid-2", "uid-3"} {
		store.RecordStart(uid, &eval.PodStartData{
			Namespace:        "default",
			UID:              uid,
			StartTime:        now,
			WouldHaveDelayed: true,
		})
	}

	if count := store.Count(); count != 3 {
		t.Errorf("Expected 3 entries, got %d", count)
	}

	store.Remove("uid-2")

	if count := store.Count(); count != 2 {
		t.Errorf("Expected 2 entries after removal, got %d", count)
	}

	if _, found := store.GetStart("uid-1"); !found {
		t.Error("Expected uid-1 to still exist")
	}
	if _, found := store.GetStart("uid-2"); found {
		t.Error("Expected uid-2 to be removed")
	}
	if _, found := store.GetStart("uid-3"); !found {
		t.Error("Expected uid-3 to still exist")
	}
}
