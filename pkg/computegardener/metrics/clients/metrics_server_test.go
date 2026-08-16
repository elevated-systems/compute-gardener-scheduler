package clients

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// fakePodMetrics is a hand-rolled PodMetricsInterface so tests can drive both
// success and error paths without a running API server.
type fakePodMetrics struct {
	getFn  func(ctx context.Context, name string, opts metav1.GetOptions) (*metricsv1beta1.PodMetrics, error)
	listFn func(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.PodMetricsList, error)
}

func (f *fakePodMetrics) Get(ctx context.Context, name string, opts metav1.GetOptions) (*metricsv1beta1.PodMetrics, error) {
	if f.getFn != nil {
		return f.getFn(ctx, name, opts)
	}
	return nil, nil
}

func (f *fakePodMetrics) List(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.PodMetricsList, error) {
	if f.listFn != nil {
		return f.listFn(ctx, opts)
	}
	return &metricsv1beta1.PodMetricsList{}, nil
}

func (f *fakePodMetrics) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func makePodMetrics(ns, name string) *metricsv1beta1.PodMetrics {
	return &metricsv1beta1.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
	}
}

func TestNewK8sMetricsClient(t *testing.T) {
	c := NewK8sMetricsClient(&fakePodMetrics{})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.client == nil {
		t.Fatal("expected client field to be set")
	}
}

func TestK8sMetricsClientGetPodMetrics(t *testing.T) {
	want := makePodMetrics("default", "pod-a")
	c := NewK8sMetricsClient(&fakePodMetrics{
		getFn: func(ctx context.Context, name string, opts metav1.GetOptions) (*metricsv1beta1.PodMetrics, error) {
			if name != "pod-a" {
				t.Fatalf("unexpected name %q", name)
			}
			return want, nil
		},
	})

	got, err := c.GetPodMetrics(context.Background(), "default", "pod-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected to get the exact pod metrics returned by the client")
	}
}

func TestK8sMetricsClientGetPodMetricsError(t *testing.T) {
	sentinel := errors.New("boom")
	c := NewK8sMetricsClient(&fakePodMetrics{
		getFn: func(ctx context.Context, name string, opts metav1.GetOptions) (*metricsv1beta1.PodMetrics, error) {
			return nil, sentinel
		},
	})

	if _, err := c.GetPodMetrics(context.Background(), "default", "pod-a"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestK8sMetricsClientListPodMetrics(t *testing.T) {
	c := NewK8sMetricsClient(&fakePodMetrics{
		listFn: func(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.PodMetricsList, error) {
			return &metricsv1beta1.PodMetricsList{
				Items: []metricsv1beta1.PodMetrics{
					*makePodMetrics("default", "pod-a"),
					*makePodMetrics("default", "pod-b"),
				},
			}, nil
		},
	})

	got, err := c.ListPodMetrics(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got[0].Name != "pod-a" || got[1].Name != "pod-b" {
		t.Fatalf("unexpected items: %+v", got)
	}
}

func TestK8sMetricsClientListPodMetricsError(t *testing.T) {
	sentinel := errors.New("boom")
	c := NewK8sMetricsClient(&fakePodMetrics{
		listFn: func(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.PodMetricsList, error) {
			return nil, sentinel
		},
	})

	if _, err := c.ListPodMetrics(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestNullGPUMetricsClient(t *testing.T) {
	c := NewNullGPUMetricsClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}

	ctx := context.Background()

	if v, err := c.GetPodGPUUtilization(ctx, "ns", "pod"); err != nil || v != 0 {
		t.Fatalf("GetPodGPUUtilization = %v, %v; want 0, nil", v, err)
	}

	m, err := c.ListPodsGPUUtilization(ctx)
	if err != nil || len(m) != 0 {
		t.Fatalf("ListPodsGPUUtilization = %v, %v; want empty map, nil", m, err)
	}

	if v, err := c.GetPodGPUPower(ctx, "ns", "pod"); err != nil || v != 0 {
		t.Fatalf("GetPodGPUPower = %v, %v; want 0, nil", v, err)
	}

	m, err = c.ListPodsGPUPower(ctx)
	if err != nil || len(m) != 0 {
		t.Fatalf("ListPodsGPUPower = %v, %v; want empty map, nil", m, err)
	}
}

func TestMockCoreMetricsClient(t *testing.T) {
	c := NewMockCoreMetricsClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.pods == nil {
		t.Fatal("expected pods map to be initialized")
	}

	ctx := context.Background()

	// Missing pod returns nil, nil.
	if pm, err := c.GetPodMetrics(ctx, "default", "absent"); err != nil || pm != nil {
		t.Fatalf("missing pod = %v, %v; want nil, nil", pm, err)
	}

	// Add a pod and fetch it back.
	pm := makePodMetrics("default", "pod-a")
	c.AddPodMetrics(pm)
	got, err := c.GetPodMetrics(ctx, "default", "pod-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != pm {
		t.Fatalf("expected the same pod metrics object back")
	}

	// List returns all added pods.
	list, err := c.ListPodMetrics(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "pod-a" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestMockGPUMetricsClient(t *testing.T) {
	c := NewMockGPUMetricsClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.utilization == nil || c.power == nil {
		t.Fatal("expected utilization and power maps to be initialized")
	}

	ctx := context.Background()

	// Missing values return 0, nil.
	if v, err := c.GetPodGPUUtilization(ctx, "ns", "pod"); err != nil || v != 0 {
		t.Fatalf("missing util = %v, %v; want 0, nil", v, err)
	}
	if v, err := c.GetPodGPUPower(ctx, "ns", "pod"); err != nil || v != 0 {
		t.Fatalf("missing power = %v, %v; want 0, nil", v, err)
	}

	// Set values and read them back.
	c.SetPodGPUUtilization("ns", "pod", 0.75)
	c.SetPodGPUPower("ns", "pod", 120.5)

	if v, err := c.GetPodGPUUtilization(ctx, "ns", "pod"); err != nil || v != 0.75 {
		t.Fatalf("util = %v, %v; want 0.75, nil", v, err)
	}
	if v, err := c.GetPodGPUPower(ctx, "ns", "pod"); err != nil || v != 120.5 {
		t.Fatalf("power = %v, %v; want 120.5, nil", v, err)
	}

	// List returns a copy of the maps.
	u, err := c.ListPodsGPUUtilization(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(u) != 1 || u["ns/pod"] != 0.75 {
		t.Fatalf("unexpected util list: %+v", u)
	}

	p, err := c.ListPodsGPUPower(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p) != 1 || p["ns/pod"] != 120.5 {
		t.Fatalf("unexpected power list: %+v", p)
	}

	// Mutating the returned copy must not affect the mock.
	u["ns/pod"] = 0.0
	if got := c.utilization["ns/pod"]; got != 0.75 {
		t.Fatalf("returned map was not a copy; mock value changed to %v", got)
	}
}
