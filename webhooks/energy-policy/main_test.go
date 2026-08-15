package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

// TestNewEnergyPolicyWebhookInClusterSimulated drives the constructor's
// success path without a real cluster: rest.InClusterConfig only needs the
// KUBERNETES_SERVICE_HOST/PORT env vars plus a readable service-account
// token at the well-known path, and kubernetes.NewForConfig makes no
// network calls. client-go hard-codes the token path, so the test writes
// there and skips if it cannot (e.g. unprivileged CI).
func TestNewEnergyPolicyWebhookInClusterSimulated(t *testing.T) {
	const tokenDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	if _, err := os.Stat(tokenDir); os.IsNotExist(err) {
		if err := os.MkdirAll(tokenDir, 0o755); err != nil {
			t.Skipf("cannot simulate in-cluster env (cannot create %s): %v", tokenDir, err)
		}
	}
	tokenFile := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0o600); err != nil {
		t.Skipf("cannot simulate in-cluster env (cannot write %s): %v", tokenFile, err)
	}

	t.Setenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	w, err := NewEnergyPolicyWebhook()
	if err != nil {
		t.Fatalf("expected success with simulated in-cluster env, got: %v", err)
	}
	if w == nil || w.kubeClient == nil {
		t.Fatal("expected webhook with a non-nil kube client")
	}
}

// TestMainSimulated drives main()'s startup path: simulated in-cluster env,
// a self-signed cert pair at the relative "tls.crt"/"tls.key" paths main()
// expects, and -port 0 replaced with a free port so ListenAndServeTLS never
// collides. main() blocks in the listener once it has started, so the test
// runs it in a goroutine and waits for the port to accept connections.
func TestMainSimulated(t *testing.T) {
	const tokenDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	if err := os.MkdirAll(tokenDir, 0o755); err != nil {
		t.Skipf("cannot simulate in-cluster env (cannot create %s): %v", tokenDir, err)
	}
	if err := os.WriteFile(filepath.Join(tokenDir, "token"), []byte("test-token"), 0o600); err != nil {
		t.Skipf("cannot simulate in-cluster env (cannot write token): %v", err)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	// Free port so the server never collides with a live one.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	// Self-signed cert pair for ListenAndServeTLS.
	dir := t.TempDir()
	t.Chdir(dir) // main() loads "tls.crt"/"tls.key" relative to the cwd
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "energy-policy-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	if err := os.WriteFile("tls.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write tls.crt: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile("tls.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write tls.key: %v", err)
	}

	// main() parses process args and then blocks in ListenAndServeTLS.
	oldArgs := os.Args
	os.Args = []string{"energy-policy", "-port", strconv.Itoa(port)}
	t.Cleanup(func() { os.Args = oldArgs })

	done := make(chan struct{})
	go func() {
		main()
		close(done) // reached only if the server fails to start
	}()

	// Wait until the TLS listener accepts connections.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			conn.Close()
			return
		}
		select {
		case <-done:
			t.Fatal("main() exited before the server was serving")
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("server did not start within 10s")
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
