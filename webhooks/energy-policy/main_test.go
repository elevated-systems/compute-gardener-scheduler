package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

// baseAnnotation is the prefix every compute-gardener annotation uses.
const baseAnnotation = "compute-gardener-scheduler.kubernetes.io"

const (
	policyKey   = baseAnnotation + "/policy-pue"                            // namespace-level policy annotation
	podPueKey   = "pue"                                                     // resulting pod annotation (prefix stripped)
	overrideKey = baseAnnotation + "/workload-" + "batch" + "-" + policyKey // batch workload override
)

func podWithAnnotations(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns", Annotations: annotations},
	}
}

func admissionReviewFor(t *testing.T, namespace string, pod *corev1.Pod) v1.AdmissionReview {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}
	return v1.AdmissionReview{
		Request: &v1.AdmissionRequest{
			Namespace: namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// isEnergyPolicyAnnotation / convertNamespacePolicyToAnnotation are table-driven.
func TestIsEnergyPolicyAnnotation(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{key: baseAnnotation + "/policy-pue", want: true},
		{key: baseAnnotation + "/policy-cpu", want: true},
		{key: baseAnnotation + "/pue", want: false}, // not a policy prefix
		{key: "unrelated", want: false},
		{key: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			if got := isEnergyPolicyAnnotation(tc.key); got != tc.want {
				t.Fatalf("isEnergyPolicyAnnotation(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

func TestConvertNamespacePolicyToAnnotation(t *testing.T) {
	got := convertNamespacePolicyToAnnotation(policyKey)
	if got != podPueKey {
		t.Fatalf("got %q, want %q", got, podPueKey)
	}
}

func TestDetermineWorkloadType(t *testing.T) {
	t.Run("explicit label wins", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{common.LabelWorkloadType: "custom"},
			},
		}
		if got := determineWorkloadType(pod); got != "custom" {
			t.Fatalf("got %q, want custom", got)
		}
	})

	ownerCases := []struct {
		kind string
		want string
	}{
		{"Job", common.WorkloadTypeBatch},
		{"CronJob", common.WorkloadTypeBatch},
		{"Deployment", common.WorkloadTypeService},
		{"ReplicaSet", common.WorkloadTypeService},
		{"StatefulSet", common.WorkloadTypeStateful},
		{"DaemonSet", common.WorkloadTypeSystem},
	}
	for _, tc := range ownerCases {
		t.Run("owner "+tc.kind, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{{Kind: tc.kind}},
				},
			}
			if got := determineWorkloadType(pod); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("no owner and no label -> generic", func(t *testing.T) {
		pod := &corev1.Pod{}
		if got := determineWorkloadType(pod); got != common.WorkloadTypeGeneric {
			t.Fatalf("got %q, want %q", got, common.WorkloadTypeGeneric)
		}
	})
}

func TestApplyNamespacePolicies(t *testing.T) {
	t.Run("skip annotation returns unchanged", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					policyKey: "1.5",
				},
			},
		}
		pod := podWithAnnotations(map[string]string{common.AnnotationSkip: "true"})
		got, err := (&EnergyPolicyWebhook{}).applyNamespacePolicies(ns, pod)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got[podPueKey]; ok {
			t.Fatal("policy should not be applied when pod opted out")
		}
	})

	t.Run("namespace default applied when pod has no annotation", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					policyKey: "1.5",
				},
			},
		}
		pod := podWithAnnotations(nil)
		got, err := (&EnergyPolicyWebhook{}).applyNamespacePolicies(ns, pod)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[podPueKey] != "1.5" {
			t.Fatalf("expected pue 1.5, got %q", got[podPueKey])
		}
	})

	t.Run("existing pod annotation is not overridden", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					policyKey: "1.5",
				},
			},
		}
		pod := podWithAnnotations(map[string]string{podPueKey: "2.0"})
		got, err := (&EnergyPolicyWebhook{}).applyNamespacePolicies(ns, pod)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[podPueKey] != "2.0" {
			t.Fatalf("existing pod annotation should win, got %q", got[podPueKey])
		}
	})

	t.Run("workload-specific override beats namespace default", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					policyKey:   "1.5",
					overrideKey: "3.0",
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "pod-1",
				OwnerReferences: []metav1.OwnerReference{{Kind: "Job"}},
			},
		}
		got, err := (&EnergyPolicyWebhook{}).applyNamespacePolicies(ns, pod)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[podPueKey] != "3.0" {
			t.Fatalf("expected workload override 3.0, got %q", got[podPueKey])
		}
	})

	t.Run("non-policy annotations are ignored", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"some/other-annotation": "x",
				},
			},
		}
		pod := podWithAnnotations(nil)
		got, err := (&EnergyPolicyWebhook{}).applyNamespacePolicies(ns, pod)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected no annotations copied, got %v", got)
		}
	})
}

func TestEqualAnnotations(t *testing.T) {
	if !equalAnnotations(map[string]string{"a": "1"}, map[string]string{"a": "1"}) {
		t.Fatal("identical maps should be equal")
	}
	if equalAnnotations(map[string]string{"a": "1"}, map[string]string{"a": "2"}) {
		t.Fatal("different values should not be equal")
	}
	if equalAnnotations(map[string]string{"a": "1"}, map[string]string{"b": "1"}) {
		t.Fatal("different keys should not be equal")
	}
	if !equalAnnotations(nil, nil) {
		t.Fatal("two nil maps should be equal")
	}
}

func TestCreatePatch(t *testing.T) {
	current := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Annotations: map[string]string{}}}
	modified := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Annotations: map[string]string{podPueKey: "1.5"}}}

	patch, err := createPatch(current, modified)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(patch, []byte("\""+podPueKey+"\"")) {
		t.Fatalf("patch should contain the new annotation: %s", patch)
	}
}

// Serve tests go through a fake kube clientset so the namespace lookup is real.
func TestServe(t *testing.T) {
	t.Run("bad pod object -> error status", func(t *testing.T) {
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset()}
		resp, err := w.Serve(v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "ns", Object: runtime.RawExtension{Raw: []byte("not-json")}}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Result == nil || !strings.Contains(resp.Result.Message, "Failed to unmarshal pod") {
			t.Fatalf("expected unmarshal error, got %+v", resp.Result)
		}
	})

	t.Run("missing namespace -> error status", func(t *testing.T) {
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset()} // no namespace created
		pod := podWithAnnotations(nil)
		resp, err := w.Serve(admissionReviewFor(t, "does-not-exist", pod))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Result == nil || !strings.Contains(resp.Result.Message, "Failed to get namespace") {
			t.Fatalf("expected namespace error, got %+v", resp.Result)
		}
	})

	t.Run("unchanged annotations -> allowed without patch", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}}
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset(ns)}
		pod := podWithAnnotations(nil) // no policies on ns -> nothing to add
		resp, err := w.Serve(admissionReviewFor(t, "ns", pod))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Allowed || resp.Patch != nil {
			t.Fatalf("expected allowed with no patch, got %+v", resp)
		}
	})

	t.Run("policy applied -> allowed with JSON patch", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "ns",
				Annotations: map[string]string{policyKey: "1.5"},
			},
		}
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset(ns)}
		pod := podWithAnnotations(nil)
		resp, err := w.Serve(admissionReviewFor(t, "ns", pod))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Allowed {
			t.Fatalf("expected allowed, got %+v", resp)
		}
		if resp.Patch == nil {
			t.Fatal("expected a patch")
		}
		if resp.PatchType == nil || *resp.PatchType != v1.PatchTypeJSONPatch {
			t.Fatalf("expected JSON patch type, got %v", resp.PatchType)
		}
		if !bytes.Contains(resp.Patch, []byte("\""+podPueKey+"\"")) {
			t.Fatalf("patch should include the policy annotation: %s", resp.Patch)
		}
	})
}

func TestNewEnergyPolicyWebhookOutOfCluster(t *testing.T) {
	// Outside a cluster (no KUBERNETES_SERVICE_HOST/PORT) the in-cluster
	// config lookup fails, so the constructor must return an error rather
	// than panic. Skip if the test happens to run inside a cluster.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	if _, err := NewEnergyPolicyWebhook(); err == nil {
		t.Fatal("expected an error when not running in a cluster")
	}
}

func TestServeHTTP(t *testing.T) {
	t.Run("bad JSON body -> 400", func(t *testing.T) {
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset()}
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("not-json")))
		rec := httptest.NewRecorder()
		w.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("valid request -> 200 with JSON response", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "ns",
				Annotations: map[string]string{policyKey: "1.5"},
			},
		}
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset(ns)}
		pod := podWithAnnotations(nil)
		review := admissionReviewFor(t, "ns", pod)
		body, err := json.Marshal(review)
		if err != nil {
			t.Fatalf("marshal review: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		w.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json content type, got %q", ct)
		}
		// The response should be an AdmissionReview with a non-nil Response.
		var out v1.AdmissionReview
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if out.Response == nil {
			t.Fatal("expected a response in the admission review")
		}
	})

	t.Run("nil body -> 400", func(t *testing.T) {
		w := &EnergyPolicyWebhook{kubeClient: fake.NewSimpleClientset()}
		req := httptest.NewRequest(http.MethodPost, "/mutate", nil)
		req.Body = http.NoBody
		rec := httptest.NewRecorder()
		w.ServeHTTP(rec, req)
		// Empty body fails to unmarshal into an AdmissionReview -> 400.
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}
