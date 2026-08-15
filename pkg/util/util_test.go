package util

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/apis/scheduling/v1alpha1"
)

func TestResourceList(t *testing.T) {
	tests := []struct {
		name string
		res  *framework.Resource
		want v1.ResourceList
	}{
		{
			name: "cpu memory pods ephemeral",
			res: &framework.Resource{
				MilliCPU:         1000,
				Memory:           2048,
				AllowedPodNumber: 30,
				EphemeralStorage: 4096,
			},
			want: v1.ResourceList{
				v1.ResourceCPU:              *resource.NewMilliQuantity(1000, resource.DecimalSI),
				v1.ResourceMemory:           *resource.NewQuantity(2048, resource.BinarySI),
				v1.ResourcePods:             *resource.NewQuantity(30, resource.BinarySI),
				v1.ResourceEphemeralStorage: *resource.NewQuantity(4096, resource.BinarySI),
			},
		},
		{
			name: "zero values",
			res:  &framework.Resource{},
			want: v1.ResourceList{
				v1.ResourceCPU:              *resource.NewMilliQuantity(0, resource.DecimalSI),
				v1.ResourceMemory:           *resource.NewQuantity(0, resource.BinarySI),
				v1.ResourcePods:             *resource.NewQuantity(0, resource.BinarySI),
				v1.ResourceEphemeralStorage: *resource.NewQuantity(0, resource.BinarySI),
			},
		},
		{
			name: "scalar and hugepage resources",
			res: &framework.Resource{
				MilliCPU: 250,
				ScalarResources: map[v1.ResourceName]int64{
					"example.com/gpu": 2,
					"hugepages-2Mi":   8,
				},
			},
			want: v1.ResourceList{
				v1.ResourceCPU:              *resource.NewMilliQuantity(250, resource.DecimalSI),
				v1.ResourceMemory:           *resource.NewQuantity(0, resource.BinarySI),
				v1.ResourcePods:             *resource.NewQuantity(0, resource.BinarySI),
				v1.ResourceEphemeralStorage: *resource.NewQuantity(0, resource.BinarySI),
				"example.com/gpu":           *resource.NewQuantity(2, resource.DecimalSI),
				"hugepages-2Mi":             *resource.NewQuantity(8, resource.BinarySI),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResourceList(tt.res)
			if len(got) != len(tt.want) {
				t.Fatalf("ResourceList() returned %d resources, want %d", len(got), len(tt.want))
			}
			for name, wantQty := range tt.want {
				gotQty, ok := got[name]
				if !ok {
					t.Fatalf("ResourceList() missing resource %q", name)
				}
				if gotQty.Cmp(wantQty) != 0 {
					t.Errorf("ResourceList()[%q] = %v, want %v", name, gotQty, wantQty)
				}
			}
		})
	}
}

func cont(name string, reqs v1.ResourceList) v1.Container {
	return v1.Container{
		Name:      name,
		Resources: v1.ResourceRequirements{Requests: reqs},
	}
}

func newTestPod(init, containers []v1.Container, overhead v1.ResourceList) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1.PodSpec{
			InitContainers: init,
			Containers:     containers,
			Overhead:       overhead,
		},
	}
}

func TestGetPodEffectiveRequest(t *testing.T) {
	cpu := func(m int64) *resource.Quantity { return resource.NewMilliQuantity(m, resource.DecimalSI) }
	mem := func(b int64) *resource.Quantity { return resource.NewQuantity(b, resource.BinarySI) }

	tests := []struct {
		name       string
		init       []v1.Container
		containers []v1.Container
		overhead   v1.ResourceList
		want       v1.ResourceList
	}{
		{
			name:       "no containers",
			init:       nil,
			containers: nil,
			overhead:   nil,
			want:       v1.ResourceList{},
		},
		{
			name: "single container request",
			containers: []v1.Container{
				cont("a", v1.ResourceList{v1.ResourceCPU: *cpu(500)}),
			},
			want: v1.ResourceList{v1.ResourceCPU: *cpu(500)},
		},
		{
			name: "containers sum requests",
			containers: []v1.Container{
				cont("a", v1.ResourceList{v1.ResourceCPU: *cpu(500)}),
				cont("b", v1.ResourceList{v1.ResourceCPU: *cpu(250)}),
			},
			want: v1.ResourceList{v1.ResourceCPU: *cpu(750)},
		},
		{
			name: "init higher than containers wins",
			init: []v1.Container{
				cont("i", v1.ResourceList{v1.ResourceMemory: *mem(2048)}),
			},
			containers: []v1.Container{
				cont("a", v1.ResourceList{v1.ResourceMemory: *mem(1024)}),
			},
			want: v1.ResourceList{v1.ResourceMemory: *mem(2048)},
		},
		{
			name: "containers higher than init wins",
			init: []v1.Container{
				cont("i", v1.ResourceList{v1.ResourceMemory: *mem(1024)}),
			},
			containers: []v1.Container{
				cont("a", v1.ResourceList{v1.ResourceMemory: *mem(2048)}),
			},
			want: v1.ResourceList{v1.ResourceMemory: *mem(2048)},
		},
		{
			name: "multiple init containers take max",
			init: []v1.Container{
				cont("i0", v1.ResourceList{v1.ResourceCPU: *cpu(100)}),
				cont("i1", v1.ResourceList{v1.ResourceCPU: *cpu(300)}),
			},
			want: v1.ResourceList{v1.ResourceCPU: *cpu(300)},
		},
		{
			name: "second init container lower is ignored",
			init: []v1.Container{
				cont("i0", v1.ResourceList{v1.ResourceCPU: *cpu(300)}),
				cont("i1", v1.ResourceList{v1.ResourceCPU: *cpu(100)}),
			},
			want: v1.ResourceList{v1.ResourceCPU: *cpu(300)},
		},
		{
			name: "overhead added on top",
			containers: []v1.Container{
				cont("a", v1.ResourceList{v1.ResourceMemory: *mem(1024)}),
			},
			overhead: v1.ResourceList{v1.ResourceMemory: *mem(512)},
			want:     v1.ResourceList{v1.ResourceMemory: *mem(1536)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newTestPod(tt.init, tt.containers, tt.overhead)
			got := GetPodEffectiveRequest(pod)
			if len(got) != len(tt.want) {
				t.Fatalf("GetPodEffectiveRequest() returned %d resources, want %d", len(got), len(tt.want))
			}
			for name, wantQty := range tt.want {
				gotQty, ok := got[name]
				if !ok {
					t.Fatalf("GetPodEffectiveRequest() missing resource %q", name)
				}
				if gotQty.Cmp(wantQty) != 0 {
					t.Errorf("GetPodEffectiveRequest()[%q] = %v, want %v", name, gotQty, wantQty)
				}
			}
		})
	}
}

func TestCreateMergePatch(t *testing.T) {
	t.Run("valid patch", func(t *testing.T) {
		// CreateTwoWayMergePatch requires the schema argument to be a struct.
		type sample struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		original := &sample{A: 1, B: 2}
		newObj := &sample{A: 1, B: 3}
		patch, err := CreateMergePatch(original, newObj)
		if err != nil {
			t.Fatalf("CreateMergePatch() unexpected error: %v", err)
		}
		if len(patch) == 0 {
			t.Fatal("CreateMergePatch() returned empty patch")
		}
	})

	t.Run("first marshal error", func(t *testing.T) {
		// original cannot be marshaled, so the first json.Marshal fails.
		if _, err := CreateMergePatch(make(chan int), make(chan int)); err == nil {
			t.Fatal("CreateMergePatch() expected error when original is unmarshalable, got nil")
		}
	})

	t.Run("second marshal error", func(t *testing.T) {
		// original marshals fine (struct), new cannot be marshaled (channel).
		type sample struct {
			A int `json:"a"`
		}
		if _, err := CreateMergePatch(&sample{A: 1}, make(chan int)); err == nil {
			t.Fatal("CreateMergePatch() expected error when new is unmarshalable, got nil")
		}
	})

	t.Run("merge patch error", func(t *testing.T) {
		// Both marshal, but the new value is a scalar, not an object, so the
		// two-way merge patch cannot be created.
		type sample struct {
			A int `json:"a"`
		}
		if _, err := CreateMergePatch(&sample{A: 1}, 42); err == nil {
			t.Fatal("CreateMergePatch() expected error for scalar new value, got nil")
		}
	})
}

func TestGetPodGroupLabel(t *testing.T) {
	tests := []struct {
		name string
		lbls map[string]string
		want string
	}{
		{name: "with label", lbls: map[string]string{v1alpha1.PodGroupLabel: "my-group"}, want: "my-group"},
		{name: "no label", lbls: map[string]string{"other": "value"}, want: ""},
		{name: "nil labels", lbls: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: tt.lbls}}
			if got := GetPodGroupLabel(pod); got != tt.want {
				t.Errorf("GetPodGroupLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetPodGroupFullName(t *testing.T) {
	tests := []struct {
		name string
		ns   string
		lbls map[string]string
		want string
	}{
		{name: "namespaced group", ns: "team-a", lbls: map[string]string{v1alpha1.PodGroupLabel: "pg-1"}, want: "team-a/pg-1"},
		{name: "no group label", ns: "team-a", lbls: map[string]string{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: tt.ns, Labels: tt.lbls}}
			if got := GetPodGroupFullName(pod); got != tt.want {
				t.Errorf("GetPodGroupFullName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetWaitTimeDuration(t *testing.T) {
	five := int32(5)
	thirty := time.Second * 30
	zero := time.Duration(0)

	tests := []struct {
		name string
		pg   *v1alpha1.PodGroup
		to   *time.Duration
		want time.Duration
	}{
		{
			name: "podgroup timeout wins",
			pg:   &v1alpha1.PodGroup{Spec: v1alpha1.PodGroupSpec{ScheduleTimeoutSeconds: &five}},
			to:   &thirty,
			want: 5 * time.Second,
		},
		{
			name: "explicit schedule timeout",
			pg:   nil,
			to:   &thirty,
			want: 30 * time.Second,
		},
		{
			name: "zero schedule timeout falls back to default",
			pg:   nil,
			to:   &zero,
			want: DefaultWaitTime,
		},
		{
			name: "both nil defaults",
			pg:   nil,
			to:   nil,
			want: DefaultWaitTime,
		},
		{
			name: "podgroup without timeout defaults",
			pg:   &v1alpha1.PodGroup{},
			to:   nil,
			want: DefaultWaitTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetWaitTimeDuration(tt.pg, tt.to); got != tt.want {
				t.Errorf("GetWaitTimeDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
