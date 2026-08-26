package dryrun

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func setupTestWebhook(t *testing.T) (*Webhook, *PendingStore) {
	t.Helper()

	cfg := &Config{
		Mode:       "annotate",
		FilterMode: FilterModeSchedulerName,
	}
	pendingStore := NewPendingStore(0)

	return NewWebhook(cfg, pendingStore), pendingStore
}

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

func targetedPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			SchedulerName: common.SchedulerName,
			Containers:    []corev1.Container{{Name: "test"}},
		},
	}
}

// decodePatch unmarshals the JSON patch carried by an admission response
func decodePatch(t *testing.T, resp *admissionv1.AdmissionResponse) []map[string]interface{} {
	t.Helper()
	var patches []map[string]interface{}
	if err := json.Unmarshal(resp.Patch, &patches); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}
	return patches
}

// TestWebhook_MutatesSchedulerNameOnly guards the contract the webhook exists
// to keep: spec.schedulerName is the only field it ever writes.
func TestWebhook_MutatesSchedulerNameOnly(t *testing.T) {
	for _, mode := range []string{"metrics", "annotate"} {
		t.Run(mode, func(t *testing.T) {
			webhook, _ := setupTestWebhook(t)
			webhook.config.Mode = mode

			resp := webhook.handleAdmission(makeAdmissionRequest(t, targetedPod("test-pod", "default")))

			if !resp.Allowed {
				t.Error("Expected pod to be allowed")
			}

			patches := decodePatch(t, resp)
			if len(patches) != 1 {
				t.Fatalf("Expected exactly 1 patch operation, got %d: %v", len(patches), patches)
			}

			patch := patches[0]
			if patch["path"] != "/spec/schedulerName" {
				t.Errorf("Expected path '/spec/schedulerName', got %v", patch["path"])
			}
			if patch["op"] != "replace" {
				t.Errorf("Expected op 'replace', got %v", patch["op"])
			}
			if patch["value"] != common.DefaultSchedulerName {
				t.Errorf("Expected value %q, got %v", common.DefaultSchedulerName, patch["value"])
			}
		})
	}
}

func TestWebhook_RecordsPendingForTargetedPod(t *testing.T) {
	webhook, pendingStore := setupTestWebhook(t)

	pod := targetedPod("test-pod", "default")
	webhook.handleAdmission(makeAdmissionRequest(t, pod))

	if pendingStore.Count() != 1 {
		t.Fatalf("Expected 1 pending admission, got %d", pendingStore.Count())
	}

	// The persisted pod carries a UID the webhook never saw, so the claim has to
	// succeed on identity alone
	persisted := pod.DeepCopy()
	persisted.UID = "assigned-after-admission"
	persisted.Spec.SchedulerName = common.DefaultSchedulerName

	if !pendingStore.Claim(persisted, time.Now()) {
		t.Error("Expected the persisted pod to claim its admission")
	}
}

func TestWebhook_SchedulerNameMode_NonMatchingPod(t *testing.T) {
	webhook, pendingStore := setupTestWebhook(t)

	pod := targetedPod("other-pod", "default")
	pod.Spec.SchedulerName = common.DefaultSchedulerName

	resp := webhook.handleAdmission(makeAdmissionRequest(t, pod))

	if !resp.Allowed {
		t.Error("Expected non-matching pod to be allowed")
	}
	if resp.Patch != nil {
		t.Error("Expected no patch for non-matching pod")
	}
	if pendingStore.Count() != 0 {
		t.Error("Expected non-matching pod not to be recorded")
	}
}

func TestWebhook_SchedulerNameMode_EmptySchedulerName(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	pod := targetedPod("default-pod", "kube-system")
	pod.Spec.SchedulerName = ""

	resp := webhook.handleAdmission(makeAdmissionRequest(t, pod))

	if !resp.Allowed {
		t.Error("Expected pod to be allowed")
	}
	if resp.Patch != nil {
		t.Error("Expected no patch for pod without schedulerName")
	}
}

// TestWebhook_OptedOutPod checks that a skipped pod is still handed back to the
// default scheduler, since it has to remain schedulable, but is not tracked.
func TestWebhook_OptedOutPod(t *testing.T) {
	webhook, pendingStore := setupTestWebhook(t)

	pod := targetedPod("skipped-pod", "default")
	pod.Annotations = map[string]string{common.AnnotationSkip: "true"}

	resp := webhook.handleAdmission(makeAdmissionRequest(t, pod))

	patches := decodePatch(t, resp)
	if len(patches) != 1 || patches[0]["path"] != "/spec/schedulerName" {
		t.Errorf("Expected only the schedulerName patch, got %v", patches)
	}
	if pendingStore.Count() != 0 {
		t.Error("Expected opted-out pod not to be recorded for tracking")
	}
}

// TestWebhook_NamespaceMode_NeverMutates covers the mode where the webhook has
// nothing to do: the controller watches those namespaces directly.
func TestWebhook_NamespaceMode_NeverMutates(t *testing.T) {
	webhook, pendingStore := setupTestWebhook(t)
	webhook.config.FilterMode = FilterModeNamespace
	webhook.config.WatchNamespaces = []string{"production"}

	for _, namespace := range []string{"production", "development"} {
		resp := webhook.handleAdmission(makeAdmissionRequest(t, targetedPod("test-pod", namespace)))

		if !resp.Allowed {
			t.Errorf("Expected pod in %s to be allowed", namespace)
		}
		if resp.Patch != nil {
			t.Errorf("Expected no patch for pod in %s", namespace)
		}
	}

	if pendingStore.Count() != 0 {
		t.Error("Expected no pending admissions in namespace mode")
	}
}

func TestWebhook_MalformedPodIsAllowed(t *testing.T) {
	webhook, _ := setupTestWebhook(t)

	req := &admissionv1.AdmissionRequest{
		Namespace: "default",
		Object:    runtime.RawExtension{Raw: []byte("not a pod")},
	}

	resp := webhook.handleAdmission(req)
	if !resp.Allowed {
		t.Error("Expected malformed pod to be allowed rather than blocked")
	}
	if resp.Patch != nil {
		t.Error("Expected no patch for malformed pod")
	}
}
