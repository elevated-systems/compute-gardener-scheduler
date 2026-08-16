package powerprovider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics/clients"
)

// memQty builds a memory resource quantity from GiB.
func memQty(gib int64) resource.Quantity {
	return *resource.NewQuantity(gib*1024*1024*1024, resource.BinarySI)
}

func TestEstimateMemoryPower(t *testing.T) {
	tests := []struct {
		name     string
		memoryGB float64
		isMax    bool
		want     float64
	}{
		{name: "idle zero memory", memoryGB: 0, isMax: false, want: 1.0},
		{name: "idle 8gb", memoryGB: 8, isMax: false, want: 1.0 + 0.125*8},
		{name: "max zero memory", memoryGB: 0, isMax: true, want: 1.0},
		{name: "max 8gb", memoryGB: 8, isMax: true, want: 1.0 + 0.35*8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateMemoryPower(tt.memoryGB, tt.isMax); got != tt.want {
				t.Errorf("estimateMemoryPower(%v, %v) = %v, want %v", tt.memoryGB, tt.isMax, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: time.Hour, want: "3600s"},
		{d: 24 * time.Hour, want: "86400s"},
		{d: 90 * time.Second, want: "90s"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestPriorityConstants(t *testing.T) {
	// Verify the documented priority ordering.
	if !(PRIORITY_KEPLER_MEASURED > PRIORITY_ANNOTATION) {
		t.Error("KEPLER_MEASURED should out-rank ANNOTATION")
	}
	if !(PRIORITY_ANNOTATION > PRIORITY_NFD) {
		t.Error("ANNOTATION should out-rank NFD")
	}
	if !(PRIORITY_NFD > PRIORITY_FALLBACK) {
		t.Error("NFD should out-rank FALLBACK")
	}
}

// ---------------------------------------------------------------------------
// AnnotationPowerProvider
// ---------------------------------------------------------------------------

func newAnnotationNode(annotations map[string]string, memGiB int64) *v1.Node {
	capacity := v1.ResourceList{}
	if memGiB > 0 {
		capacity[v1.ResourceMemory] = memQty(memGiB)
	}
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "anno-node", Annotations: annotations},
		Status:     v1.NodeStatus{Capacity: capacity},
	}
}

func TestAnnotationProviderIdentity(t *testing.T) {
	p := &AnnotationPowerProvider{}
	if p.GetPriority() != PRIORITY_ANNOTATION {
		t.Errorf("GetPriority() = %d, want %d", p.GetPriority(), PRIORITY_ANNOTATION)
	}
	if p.GetProviderType() != PowerDataTypeEstimated {
		t.Errorf("GetProviderType() = %q, want %q", p.GetProviderType(), PowerDataTypeEstimated)
	}
	if p.GetProviderName() != "Annotation-Based" {
		t.Errorf("GetProviderName() = %q, want Annotation-Based", p.GetProviderName())
	}
}

func TestAnnotationProviderIsAvailable(t *testing.T) {
	p := &AnnotationPowerProvider{}

	t.Run("with cpu model annotation", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationCPUModel: "Some CPU"}, 0)
		if !p.IsAvailable(node) {
			t.Error("expected provider available with cpu-model annotation")
		}
	})
	t.Run("without cpu model annotation", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{"other": "x"}, 0)
		if p.IsAvailable(node) {
			t.Error("expected provider unavailable without cpu-model annotation")
		}
	})
	t.Run("nil annotations", func(t *testing.T) {
		if p.IsAvailable(&v1.Node{}) {
			t.Error("expected provider unavailable with nil annotations")
		}
	})
}

func TestAnnotationProviderGetNodeHardwareInfo(t *testing.T) {
	p := &AnnotationPowerProvider{}

	t.Run("cpu and comma separated gpus", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100, NVIDIA V100",
		}, 0)
		cpu, gpus := p.GetNodeHardwareInfo(node)
		if cpu != "Test CPU" {
			t.Errorf("cpu = %q, want Test CPU", cpu)
		}
		if len(gpus) != 2 || gpus[0] != "NVIDIA A100" || gpus[1] != "NVIDIA V100" {
			t.Errorf("gpus = %v, want [NVIDIA A100 NVIDIA V100]", gpus)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationGPUModel: "  NVIDIA A100 ,  NVIDIA V100 ",
		}, 0)
		_, gpus := p.GetNodeHardwareInfo(node)
		if len(gpus) != 2 || gpus[0] != "NVIDIA A100" || gpus[1] != "NVIDIA V100" {
			t.Errorf("gpus = %v, want trimmed [NVIDIA A100 NVIDIA V100]", gpus)
		}
	})

	t.Run("gpu none yields no models", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationGPUModel: "none"}, 0)
		_, gpus := p.GetNodeHardwareInfo(node)
		if len(gpus) != 0 {
			t.Errorf("gpus = %v, want empty for 'none'", gpus)
		}
	})

	t.Run("empty gpu string yields no models", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationGPUModel: ""}, 0)
		_, gpus := p.GetNodeHardwareInfo(node)
		if len(gpus) != 0 {
			t.Errorf("gpus = %v, want empty", gpus)
		}
	})

	t.Run("nothing present", func(t *testing.T) {
		node := newAnnotationNode(nil, 0)
		cpu, gpus := p.GetNodeHardwareInfo(node)
		if cpu != "" || len(gpus) != 0 {
			t.Errorf("got cpu=%q gpus=%v, want empty", cpu, gpus)
		}
	})
}

func TestAnnotationProviderGetNodePowerInfo(t *testing.T) {
	p := &AnnotationPowerProvider{}
	hw := &config.HardwareProfiles{
		CPUProfiles: map[string]config.PowerProfile{
			"Test CPU":  {IdlePower: 10, MaxPower: 100},
			"Other CPU": {IdlePower: 20, MaxPower: 200},
		},
		GPUProfiles: map[string]config.PowerProfile{
			"NVIDIA A100": {IdlePower: 50, MaxPower: 250},
			"NVIDIA V100": {IdlePower: 40, MaxPower: 200},
		},
	}

	t.Run("nil hardware config errors", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationCPUModel: "Test CPU"}, 8)
		if _, err := p.GetNodePowerInfo(node, nil); err == nil {
			t.Fatal("expected error for nil hardware config")
		}
	})

	t.Run("missing cpu model errors", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{}, 8)
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for missing cpu model")
		}
	})

	t.Run("unknown cpu model errors", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationCPUModel: "Unknown"}, 8)
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for unknown cpu model")
		}
	})

	t.Run("cpu only with memory and default pue", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationCPUModel: "Test CPU"}, 8)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// 10 + (1 + 0.125*8) = 12.0 ; 100 + (1 + 0.35*8) = 103.8
		if got.IdlePower != 12.0 {
			t.Errorf("IdlePower = %v, want 12.0", got.IdlePower)
		}
		if got.MaxPower != 103.8 {
			t.Errorf("MaxPower = %v, want 103.8", got.MaxPower)
		}
		if got.PUE != common.DefaultPUE {
			t.Errorf("PUE = %v, want default %v", got.PUE, common.DefaultPUE)
		}
		if got.GPUPUE != 0 {
			t.Errorf("GPUPUE = %v, want 0 (no gpus)", got.GPUPUE)
		}
	})

	t.Run("multiple gpus sum and default gpu pue", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100, NVIDIA V100",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdleGPUPower != 90 {
			t.Errorf("IdleGPUPower = %v, want 90", got.IdleGPUPower)
		}
		if got.MaxGPUPower != 450 {
			t.Errorf("MaxGPUPower = %v, want 450", got.MaxGPUPower)
		}
		if got.GPUPUE != common.DefaultGPUPUE {
			t.Errorf("GPUPUE = %v, want default %v", got.GPUPUE, common.DefaultGPUPUE)
		}
	})

	t.Run("gpu model not in profiles is skipped", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100, SomeUnknownGPU",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdleGPUPower != 50 || got.MaxGPUPower != 250 {
			t.Errorf("GPU power = %v/%v, want 50/250 (unknown skipped)", got.IdleGPUPower, got.MaxGPUPower)
		}
	})

	t.Run("zero memory skips memory estimate", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{common.AnnotationCPUModel: "Test CPU"}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdlePower != 10 || got.MaxPower != 100 {
			t.Errorf("power = %v/%v, want 10/100 (no memory)", got.IdlePower, got.MaxPower)
		}
	})

	t.Run("valid pue and gpu pue annotations", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationPUE:      "1.3",
			common.AnnotationGPUPUE:   "1.4",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PUE != 1.3 {
			t.Errorf("PUE = %v, want 1.3", got.PUE)
		}
	})

	t.Run("invalid pue falls back to default", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationPUE:      "not-a-number",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PUE != common.DefaultPUE {
			t.Errorf("PUE = %v, want default %v", got.PUE, common.DefaultPUE)
		}
	})

	t.Run("pue below 1 falls back to default", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationPUE:      "0.5",
			common.AnnotationGPUPUE:   "0.5",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PUE != common.DefaultPUE {
			t.Errorf("PUE = %v, want default %v", got.PUE, common.DefaultPUE)
		}
		// GPU PUE stays unset because the node has no GPU power.
		if got.GPUPUE != 0 {
			t.Errorf("GPUPUE = %v, want 0 (no gpus)", got.GPUPUE)
		}
	})

	t.Run("valid gpu pue with gpu present", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100",
			common.AnnotationGPUPUE:   "1.6",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.GPUPUE != 1.6 {
			t.Errorf("GPUPUE = %v, want 1.6", got.GPUPUE)
		}
	})

	t.Run("invalid gpu pue falls back to default when gpus present", func(t *testing.T) {
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100",
			common.AnnotationGPUPUE:   "oops",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.GPUPUE != common.DefaultGPUPUE {
			t.Errorf("GPUPUE = %v, want default %v", got.GPUPUE, common.DefaultGPUPUE)
		}
	})

	t.Run("second gpu added to nonzero base", func(t *testing.T) {
		// Ensures the "additional GPUs add to existing" path where the first
		// gpu already set a nonzero IdleGPUPower.
		node := newAnnotationNode(map[string]string{
			common.AnnotationCPUModel: "Test CPU",
			common.AnnotationGPUModel: "NVIDIA A100, NVIDIA V100",
		}, 0)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdleGPUPower != 90 || got.MaxGPUPower != 450 {
			t.Errorf("GPU power = %v/%v, want 90/450", got.IdleGPUPower, got.MaxGPUPower)
		}
	})
}

// ---------------------------------------------------------------------------
// NFDPowerProvider
// ---------------------------------------------------------------------------

func nfdNode(labels map[string]string, memGiB, cpuCores int64) *v1.Node {
	capacity := v1.ResourceList{}
	if memGiB > 0 {
		capacity[v1.ResourceMemory] = memQty(memGiB)
	}
	if cpuCores > 0 {
		capacity[v1.ResourceCPU] = *resource.NewQuantity(cpuCores, resource.DecimalSI)
	}
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "nfd-node", Labels: labels},
		Status:     v1.NodeStatus{Capacity: capacity},
	}
}

func nfdLabels(family, modelID, vendorID string) map[string]string {
	return map[string]string{
		common.NFDLabelCPUModelFamily:   family,
		common.NFDLabelCPUModelID:       modelID,
		common.NFDLabelCPUModelVendorID: vendorID,
	}
}

func TestNFDProviderIdentity(t *testing.T) {
	p := &NFDPowerProvider{}
	if p.GetPriority() != PRIORITY_NFD {
		t.Errorf("GetPriority() = %d, want %d", p.GetPriority(), PRIORITY_NFD)
	}
	if p.GetProviderType() != PowerDataTypeEstimated {
		t.Errorf("GetProviderType() = %q, want %q", p.GetProviderType(), PowerDataTypeEstimated)
	}
	if p.GetProviderName() != "NFD-Based" {
		t.Errorf("GetProviderName() = %q, want NFD-Based", p.GetProviderName())
	}
}

func TestNFDProviderIsAvailable(t *testing.T) {
	p := &NFDPowerProvider{}

	t.Run("all labels present", func(t *testing.T) {
		if !p.IsAvailable(nfdNode(nfdLabels("6", "94", "Intel"), 0, 0)) {
			t.Error("expected available with all NFD labels")
		}
	})
	t.Run("missing vendor id", func(t *testing.T) {
		lbls := map[string]string{
			common.NFDLabelCPUModelFamily: "6",
			common.NFDLabelCPUModelID:     "94",
		}
		if p.IsAvailable(nfdNode(lbls, 0, 0)) {
			t.Error("expected unavailable without vendor id")
		}
	})
	t.Run("no labels", func(t *testing.T) {
		if p.IsAvailable(nfdNode(nil, 0, 0)) {
			t.Error("expected unavailable with no labels")
		}
	})
}

func TestNFDProviderGetNodeHardwareInfo(t *testing.T) {
	p := &NFDPowerProvider{}
	cpu, gpus := p.GetNodeHardwareInfo(nfdNode(nfdLabels("6", "94", "Intel"), 0, 0))
	if cpu != "" || len(gpus) != 0 {
		t.Errorf("GetNodeHardwareInfo = %q %v, want empty/empty (not implemented)", cpu, gpus)
	}
}

func TestNFDProviderGetNodePowerInfo(t *testing.T) {
	p := &NFDPowerProvider{}

	t.Run("nil hardware config errors", func(t *testing.T) {
		node := nfdNode(nfdLabels("6", "94", "Intel"), 8, 4)
		if _, err := p.GetNodePowerInfo(node, nil); err == nil {
			t.Fatal("expected error for nil hardware config")
		}
	})

	t.Run("direct family-model mapping resolves cpu", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles: map[string]config.PowerProfile{
				"Intel Xeon Scalable": {IdlePower: 10, MaxPower: 100},
			},
			GPUProfiles: map[string]config.PowerProfile{
				"NVIDIA A100": {IdlePower: 50, MaxPower: 250},
			},
			CPUModelMappings: map[string]map[string]string{
				"Intel": {"6-94": "Intel Xeon Scalable"},
			},
		}
		node := nfdNode(nfdLabels("6", "94", "Intel"), 8, 4)
		node.Labels[common.NvidiaLabelGPUProduct] = "NVIDIA A100"
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdlePower != 12.0 || got.MaxPower != 103.8 {
			t.Errorf("power = %v/%v, want 12.0/103.8", got.IdlePower, got.MaxPower)
		}
		if got.IdleGPUPower != 50 || got.MaxGPUPower != 250 {
			t.Errorf("gpu power = %v/%v, want 50/250", got.IdleGPUPower, got.MaxGPUPower)
		}
		if got.GPUPUE != common.DefaultGPUPUE {
			t.Errorf("GPUPUE = %v, want default", got.GPUPUE)
		}
	})

	t.Run("family-only fallback mapping", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles: map[string]config.PowerProfile{
				"Intel Family 6": {IdlePower: 15, MaxPower: 120},
			},
			CPUModelMappings: map[string]map[string]string{
				"Intel": {"6": "Intel Family 6"}, // family-only, no "6-94"
			},
		}
		node := nfdNode(nfdLabels("6", "94", "Intel"), 0, 4)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdlePower != 15 || got.MaxPower != 120 {
			t.Errorf("power = %v/%v, want 15/120", got.IdlePower, got.MaxPower)
		}
	})

	t.Run("no mapping yields generic model and unknown-profile error", func(t *testing.T) {
		// No CPUModelMappings, so identifyCPUModelFromNFDLabels produces a
		// generic string that is not in CPUProfiles -> error.
		hw := &config.HardwareProfiles{
			CPUProfiles: map[string]config.PowerProfile{},
		}
		node := nfdNode(nfdLabels("6", "94", "Intel"), 0, 4)
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for unresolvable cpu model")
		}
	})

	t.Run("missing nfd labels fall back to arch amd64", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles: map[string]config.PowerProfile{},
		}
		lbls := map[string]string{"kubernetes.io/arch": "amd64"}
		node := nfdNode(lbls, 0, 8)
		// cpuModel becomes generic; not in profiles -> error, but exercises
		// the arch-based fallback branch in detectNodeHardwareInfo.
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for generic arch model not in profiles")
		}
	})

	t.Run("arch arm64 fallback", func(t *testing.T) {
		hw := &config.HardwareProfiles{CPUProfiles: map[string]config.PowerProfile{}}
		node := nfdNode(map[string]string{"kubernetes.io/arch": "arm64"}, 0, 8)
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for generic arm64 model")
		}
	})

	t.Run("unknown arch fallback", func(t *testing.T) {
		hw := &config.HardwareProfiles{CPUProfiles: map[string]config.PowerProfile{}}
		node := nfdNode(map[string]string{"kubernetes.io/arch": "riscv64"}, 0, 8)
		if _, err := p.GetNodePowerInfo(node, hw); err == nil {
			t.Fatal("expected error for unknown arch model")
		}
	})

	t.Run("gpu inferred from node name p3", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles: map[string]config.PowerProfile{
				"Intel Xeon Scalable": {IdlePower: 10, MaxPower: 100},
			},
			GPUProfiles: map[string]config.PowerProfile{
				"NVIDIA V100": {IdlePower: 40, MaxPower: 200},
			},
			CPUModelMappings: map[string]map[string]string{
				"Intel": {"6-94": "Intel Xeon Scalable"},
			},
		}
		lbls := nfdLabels("6", "94", "Intel")
		node := nfdNode(lbls, 0, 4)
		node.Name = "ip-10-0-0-0.p3.2xlarge"
		node.Status.Capacity[common.NvidiaLabelBase] = *resource.NewQuantity(4, resource.DecimalSI)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MaxGPUPower != 200 {
			t.Errorf("MaxGPUPower = %v, want 200 (V100 inferred)", got.MaxGPUPower)
		}
	})

	t.Run("gpu inferred a10g from node name", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles:      map[string]config.PowerProfile{"Intel Xeon Scalable": {IdlePower: 10, MaxPower: 100}},
			GPUProfiles:      map[string]config.PowerProfile{"NVIDIA A10G": {IdlePower: 30, MaxPower: 150}},
			CPUModelMappings: map[string]map[string]string{"Intel": {"6-94": "Intel Xeon Scalable"}},
		}
		node := nfdNode(nfdLabels("6", "94", "Intel"), 0, 4)
		node.Name = "gke-a10g-pool"
		node.Status.Capacity[common.NvidiaLabelBase] = *resource.NewQuantity(1, resource.DecimalSI)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MaxGPUPower != 150 {
			t.Errorf("MaxGPUPower = %v, want 150 (A10G inferred)", got.MaxGPUPower)
		}
	})

	t.Run("gpu inferred default t4", func(t *testing.T) {
		hw := &config.HardwareProfiles{
			CPUProfiles:      map[string]config.PowerProfile{"Intel Xeon Scalable": {IdlePower: 10, MaxPower: 100}},
			GPUProfiles:      map[string]config.PowerProfile{"NVIDIA T4": {IdlePower: 20, MaxPower: 100}},
			CPUModelMappings: map[string]map[string]string{"Intel": {"6-94": "Intel Xeon Scalable"}},
		}
		node := nfdNode(nfdLabels("6", "94", "Intel"), 0, 4)
		node.Name = "generic-worker-1"
		node.Status.Capacity[common.NvidiaLabelBase] = *resource.NewQuantity(2, resource.DecimalSI)
		got, err := p.GetNodePowerInfo(node, hw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MaxGPUPower != 100 {
			t.Errorf("MaxGPUPower = %v, want 100 (T4 default)", got.MaxGPUPower)
		}
	})
}

// ---------------------------------------------------------------------------
// KeplerPowerProvider
// ---------------------------------------------------------------------------

// newKeplerProvider builds a Kepler provider whose Prometheus client points at
// an httptest server that always returns a single-element vector of `value`.
func newKeplerProvider(t *testing.T, value string) *KeplerPowerProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1500000000,%q]}]}}`, value)
	}))
	t.Cleanup(srv.Close)

	client, err := clients.NewPrometheusMetricsClient(srv.URL)
	if err != nil {
		t.Fatalf("failed to build prometheus client: %v", err)
	}
	return &KeplerPowerProvider{promClient: client}
}

func TestKeplerProviderIdentity(t *testing.T) {
	p := &KeplerPowerProvider{}
	if p.GetPriority() != PRIORITY_KEPLER_MEASURED {
		t.Errorf("GetPriority() = %d, want %d", p.GetPriority(), PRIORITY_KEPLER_MEASURED)
	}
	if p.GetProviderType() != PowerDataTypeMeasured {
		t.Errorf("GetProviderType() = %q, want %q", p.GetProviderType(), PowerDataTypeMeasured)
	}
	if p.GetProviderName() != "Kepler-Measured" {
		t.Errorf("GetProviderName() = %q, want Kepler-Measured", p.GetProviderName())
	}
}

func TestKeplerProviderGetNodeHardwareInfo(t *testing.T) {
	p := &KeplerPowerProvider{}
	cpu, gpus := p.GetNodeHardwareInfo(&v1.Node{})
	if cpu != "" || len(gpus) != 0 {
		t.Errorf("GetNodeHardwareInfo = %q %v, want empty/empty (not implemented)", cpu, gpus)
	}
}

func TestNewKeplerPowerProvider(t *testing.T) {
	p := NewKeplerPowerProvider(nil)
	if p == nil {
		t.Fatal("NewKeplerPowerProvider returned nil")
	}
	if p.promClient != nil {
		t.Errorf("promClient should be nil, got %v", p.promClient)
	}
	// Should be registered.
	found := false
	for _, rp := range Registry {
		if rp.GetProviderName() == "Kepler-Measured" {
			found = true
		}
	}
	if !found {
		t.Error("expected Kepler provider to be registered")
	}
}

func TestKeplerProviderNilClient(t *testing.T) {
	p := &KeplerPowerProvider{promClient: nil}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}

	if p.IsAvailable(node) {
		t.Error("expected unavailable with nil client")
	}
	if _, err := p.GetNodePowerInfo(node, &config.HardwareProfiles{}); err == nil {
		t.Error("expected error with nil client")
	}
}

func TestKeplerProviderIsAvailable(t *testing.T) {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}

	t.Run("available when cpu power positive", func(t *testing.T) {
		p := newKeplerProvider(t, "50")
		if !p.IsAvailable(node) {
			t.Error("expected available when cpu power > 0")
		}
	})
	t.Run("unavailable when cpu power zero", func(t *testing.T) {
		p := newKeplerProvider(t, "0")
		if p.IsAvailable(node) {
			t.Error("expected unavailable when cpu power == 0")
		}
	})
}

func TestKeplerProviderGetNodePowerInfo(t *testing.T) {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}

	t.Run("measured values happy path", func(t *testing.T) {
		p := newKeplerProvider(t, "50")
		got, err := p.GetNodePowerInfo(node, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// cpu=50, dram=50, gpu=50; idle/max all 50 (no fallback needed).
		if got.IdlePower != 100 { // cpu idle 50 + mem idle 50
			t.Errorf("IdlePower = %v, want 100", got.IdlePower)
		}
		if got.MaxPower != 100 {
			t.Errorf("MaxPower = %v, want 100", got.MaxPower)
		}
		if got.IdleGPUPower != 50 || got.MaxGPUPower != 50 {
			t.Errorf("gpu power = %v/%v, want 50/50", got.IdleGPUPower, got.MaxGPUPower)
		}
		if got.PUE != common.DefaultPUE {
			t.Errorf("PUE = %v, want default", got.PUE)
		}
		if got.GPUPUE != common.DefaultGPUPUE { // MaxGPUPower > 0
			t.Errorf("GPUPUE = %v, want default", got.GPUPUE)
		}
	})

	t.Run("fallback when historical data missing", func(t *testing.T) {
		// Server returns 0 -> idle/max fall back to fractions of current.
		p := newKeplerProvider(t, "0")
		got, err := p.GetNodePowerInfo(node, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.IdlePower != 0 || got.MaxPower != 0 {
			t.Errorf("power = %v/%v, want 0/0 for zero measurements", got.IdlePower, got.MaxPower)
		}
		if got.GPUPUE != 0 { // no GPU power -> no default gpu pue
			t.Errorf("GPUPUE = %v, want 0 (no gpu)", got.GPUPUE)
		}
	})

	t.Run("valid pue and gpu pue annotations", func(t *testing.T) {
		p := newKeplerProvider(t, "50")
		ann := &v1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				common.AnnotationPUE:    "1.25",
				common.AnnotationGPUPUE: "1.35",
			},
		}}
		got, err := p.GetNodePowerInfo(ann, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PUE != 1.25 {
			t.Errorf("PUE = %v, want 1.25", got.PUE)
		}
		if got.GPUPUE != 1.35 {
			t.Errorf("GPUPUE = %v, want 1.35", got.GPUPUE)
		}
	})

	t.Run("invalid pue falls back, gpu pue defaults", func(t *testing.T) {
		p := newKeplerProvider(t, "50")
		ann := &v1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				common.AnnotationPUE:    "bogus",
				common.AnnotationGPUPUE: "bogus",
			},
		}}
		got, err := p.GetNodePowerInfo(ann, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PUE != common.DefaultPUE {
			t.Errorf("PUE = %v, want default", got.PUE)
		}
		if got.GPUPUE != common.DefaultGPUPUE { // invalid gpu pue but MaxGPUPower>0
			t.Errorf("GPUPUE = %v, want default", got.GPUPUE)
		}
	})
}

func TestKeplerProviderGetNodePowerInfoQueryError(t *testing.T) {
	// A server that always errors forces the "failed to get CPU power" branch.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"status":"error","errorType":"internal","error":"boom"}`)
	}))
	t.Cleanup(srv.Close)

	client, err := clients.NewPrometheusMetricsClient(srv.URL)
	if err != nil {
		t.Fatalf("failed to build prometheus client: %v", err)
	}
	p := &KeplerPowerProvider{promClient: client}

	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-err"}}
	if _, err := p.GetNodePowerInfo(node, nil); err == nil {
		t.Fatal("expected error when the Prometheus query fails")
	}
}

func TestNFDProviderGetCPUModelKey(t *testing.T) {
	p := &NFDPowerProvider{}

	t.Run("family and model id present", func(t *testing.T) {
		node := nfdNode(nfdLabels("6", "94", "Intel"), 0, 0)
		if got := p.getCPUModelKey(node); got != "6-94" {
			t.Errorf("getCPUModelKey() = %q, want 6-94", got)
		}
	})
	t.Run("missing family", func(t *testing.T) {
		node := nfdNode(map[string]string{
			common.NFDLabelCPUModelID:       "94",
			common.NFDLabelCPUModelVendorID: "Intel",
		}, 0, 0)
		if got := p.getCPUModelKey(node); got != "" {
			t.Errorf("getCPUModelKey() = %q, want empty (missing family)", got)
		}
	})
	t.Run("missing model id", func(t *testing.T) {
		node := nfdNode(map[string]string{
			common.NFDLabelCPUModelFamily:   "6",
			common.NFDLabelCPUModelVendorID: "Intel",
		}, 0, 0)
		if got := p.getCPUModelKey(node); got != "" {
			t.Errorf("getCPUModelKey() = %q, want empty (missing model id)", got)
		}
	})
}

func TestKeplerProviderParseFloat(t *testing.T) {
	p := &KeplerPowerProvider{}
	if v, err := p.parseFloat("1.25"); err != nil || v != 1.25 {
		t.Errorf("parseFloat(1.25) = %v, %v; want 1.25, nil", v, err)
	}
	if _, err := p.parseFloat("not-a-number"); err == nil {
		t.Error("parseFloat expected error for invalid input")
	}
}

// ---------------------------------------------------------------------------
// Registry helpers
// ---------------------------------------------------------------------------

// stubProvider is a minimal PowerInfoProvider for exercising the registry.
type stubProvider struct {
	available bool
	priority  int
	name      string
}

func (s *stubProvider) IsAvailable(_ *v1.Node) bool    { return s.available }
func (s *stubProvider) GetPriority() int               { return s.priority }
func (s *stubProvider) GetProviderType() PowerDataType { return PowerDataTypeEstimated }
func (s *stubProvider) GetProviderName() string        { return s.name }
func (s *stubProvider) GetNodeHardwareInfo(_ *v1.Node) (string, []string) {
	return "", nil
}
func (s *stubProvider) GetNodePowerInfo(_ *v1.Node, _ *config.HardwareProfiles) (*config.NodePower, error) {
	return &config.NodePower{}, nil
}

func TestRegisterAndGetAvailableProviders(t *testing.T) {
	// Snapshot and restore the registry so this test doesn't disturb others.
	saved := Registry
	Registry = nil
	defer func() { Registry = saved }()

	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "reg-node"}}

	RegisterProvider(&stubProvider{available: true, priority: 10, name: "low"})
	RegisterProvider(&stubProvider{available: true, priority: 90, name: "high"})
	RegisterProvider(&stubProvider{available: false, priority: 50, name: "unavail"})

	avail := GetAvailableProviders(node)
	if len(avail) != 2 {
		t.Fatalf("GetAvailableProviders returned %d, want 2 (only available ones)", len(avail))
	}
	// Sorted by priority descending.
	if avail[0].GetPriority() != 90 {
		t.Errorf("first provider priority = %d, want 90", avail[0].GetPriority())
	}
	if avail[1].GetPriority() != 10 {
		t.Errorf("second provider priority = %d, want 10", avail[1].GetPriority())
	}
}

func TestGetBestProvider(t *testing.T) {
	saved := Registry
	Registry = nil
	defer func() { Registry = saved }()

	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "best-node"}}

	t.Run("returns highest priority", func(t *testing.T) {
		RegisterProvider(&stubProvider{available: true, priority: 20, name: "low"})
		RegisterProvider(&stubProvider{available: true, priority: 80, name: "high"})
		best, ok := GetBestProvider(node)
		if !ok {
			t.Fatal("expected a best provider")
		}
		if best.GetPriority() != 80 {
			t.Errorf("best priority = %d, want 80", best.GetPriority())
		}
	})

	t.Run("none available", func(t *testing.T) {
		Registry = nil
		RegisterProvider(&stubProvider{available: false, priority: 80, name: "unavail"})
		if _, ok := GetBestProvider(node); ok {
			t.Error("expected no best provider when none available")
		}
	})

	t.Run("empty registry", func(t *testing.T) {
		Registry = nil
		if _, ok := GetBestProvider(node); ok {
			t.Error("expected no best provider for empty registry")
		}
	})
}
