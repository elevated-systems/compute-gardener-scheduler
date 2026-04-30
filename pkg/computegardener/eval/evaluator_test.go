package eval

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	testingmocks "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/testing"
)

// --- helpers ---

func carbonOnlyConfig(threshold float64) *config.Config {
	return &config.Config{
		Carbon: config.CarbonConfig{Enabled: true, IntensityThreshold: threshold},
	}
}

func priceOnlyConfig() *config.Config {
	return &config.Config{
		Pricing: config.PriceConfig{Enabled: true},
	}
}

func bothEnabledConfig(carbonThreshold float64) *config.Config {
	return &config.Config{
		Carbon:  config.CarbonConfig{Enabled: true, IntensityThreshold: carbonThreshold},
		Pricing: config.PriceConfig{Enabled: true},
	}
}

// podWithOwner builds a minimal pod owned by a single resource of the given kind.
func podWithOwner(kind string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod", Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{Kind: kind, Name: "owner"}},
		},
	}
}

// podWithResources builds a pod with a single container that has the given
// CPU (e.g. "2"), memory (e.g. "4Gi"), and optional GPU count (0 = no GPU).
func podWithResources(cpu, mem string, gpus int) *v1.Pod {
	requests := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(mem),
	}
	if gpus > 0 {
		requests[v1.ResourceName("nvidia.com/gpu")] = *resource.NewQuantity(int64(gpus), resource.DecimalSI)
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:      "c",
				Resources: v1.ResourceRequirements{Requests: requests},
			}},
		},
	}
}

// --- determineWorkloadType ---

func TestDetermineWorkloadType(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "explicit label overrides everything",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Labels:          map[string]string{common.LabelWorkloadType: "batch"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment"}},
			}},
			expected: "batch",
		},
		{
			name:     "Job owner → batch",
			pod:      podWithOwner("Job"),
			expected: common.WorkloadTypeBatch,
		},
		{
			name:     "CronJob owner → batch",
			pod:      podWithOwner("CronJob"),
			expected: common.WorkloadTypeBatch,
		},
		{
			name:     "Deployment owner → service",
			pod:      podWithOwner("Deployment"),
			expected: common.WorkloadTypeService,
		},
		{
			name:     "ReplicaSet owner → service",
			pod:      podWithOwner("ReplicaSet"),
			expected: common.WorkloadTypeService,
		},
		{
			name:     "StatefulSet owner → stateful",
			pod:      podWithOwner("StatefulSet"),
			expected: common.WorkloadTypeStateful,
		},
		{
			name:     "DaemonSet owner → system",
			pod:      podWithOwner("DaemonSet"),
			expected: common.WorkloadTypeSystem,
		},
		{
			name:     "unknown owner kind → generic",
			pod:      podWithOwner("SomeCustomCRD"),
			expected: common.WorkloadTypeGeneric,
		},
		{
			name:     "no labels, no owners → generic",
			pod:      &v1.Pod{},
			expected: common.WorkloadTypeGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineWorkloadType(tt.pod)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// emptyEvaluator creates an Evaluator with no implementations (for estimatePodResources tests).
func emptyEvaluator() *Evaluator { return &Evaluator{} }

// --- estimatePodResources – CPU + memory power math ---

func TestEstimatePodResources_PowerMath(t *testing.T) {
	// 2 CPUs × 10W = 20W, 4GiB × 0.375W/GB ≈ 1.5W → total ≈ 21.5W
	pod := podWithResources("2", "4Gi", 0)
	est := emptyEvaluator().estimatePodResources(pod)

	if est.CPUCores != 2.0 {
		t.Errorf("CPUCores: got %v, want 2.0", est.CPUCores)
	}
	// 4GiB = 4 * 2^30 / 2^30 = 4.0 GB
	if est.MemoryGB != 4.0 {
		t.Errorf("MemoryGB: got %v, want 4.0", est.MemoryGB)
	}

	wantPower := 2.0*10.0 + 4.0*0.375 // 21.5W
	if est.PowerWatts != wantPower {
		t.Errorf("PowerWatts: got %v, want %v", est.PowerWatts, wantPower)
	}
}

// --- estimatePodResources – GPU adds 250W per GPU ---

func TestEstimatePodResources_GPUPower(t *testing.T) {
	pod := podWithResources("1", "2Gi", 2) // 2 GPUs
	est := emptyEvaluator().estimatePodResources(pod)

	if est.GPUCount != 2.0 {
		t.Errorf("GPUCount: got %v, want 2.0", est.GPUCount)
	}

	// 1 CPU×10W + 2GiB×0.375W + 2GPU×250W
	wantPower := 1.0*10.0 + 2.0*0.375 + 2.0*250.0
	if est.PowerWatts != wantPower {
		t.Errorf("PowerWatts: got %v, want %v", est.PowerWatts, wantPower)
	}
}

// --- estimatePodResources – runtime heuristics and annotation override ---

func TestEstimatePodResources_RuntimeHeuristics(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		wantRuntime float64
	}{
		{
			name:        "batch owner → 2h",
			pod:         podWithOwner("Job"),
			wantRuntime: 2.0,
		},
		{
			name:        "service owner → 24h",
			pod:         podWithOwner("Deployment"),
			wantRuntime: 24.0,
		},
		{
			name:        "generic (no owner) → 1h",
			pod:         &v1.Pod{},
			wantRuntime: 1.0,
		},
		{
			name: "annotation overrides heuristic",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{{Kind: "Job"}},
				Annotations:     map[string]string{common.AnnotationEstimatedRuntimeHours: "5.5"},
			}},
			wantRuntime: 5.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			est := emptyEvaluator().estimatePodResources(tt.pod)
			if est.RuntimeHours != tt.wantRuntime {
				t.Errorf("RuntimeHours: got %v, want %v", est.RuntimeHours, tt.wantRuntime)
			}
		})
	}
}

// --- EvaluateCarbonConstraints – threshold and annotation logic ---

func TestEvaluateCarbonConstraints_ThresholdLogic(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name             string
		currentIntensity float64
		globalThreshold  float64
		podAnnotation    string
		wantDelay        bool
	}{
		{
			name:             "below global threshold → no delay",
			currentIntensity: 150,
			globalThreshold:  200,
			wantDelay:        false,
		},
		{
			name:             "above global threshold → delay",
			currentIntensity: 250,
			globalThreshold:  200,
			wantDelay:        true,
		},
		{
			name:             "pod annotation raises threshold → no delay",
			currentIntensity: 250,
			globalThreshold:  200,
			podAnnotation:    "300",
			wantDelay:        false,
		},
		{
			name:             "pod annotation lowers threshold → delay",
			currentIntensity: 150,
			globalThreshold:  200,
			podAnnotation:    "100",
			wantDelay:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := carbonOnlyConfig(tt.globalThreshold)
			e := NewEvaluator(testingmocks.NewMockCarbon(tt.currentIntensity), nil, cfg)

			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
			if tt.podAnnotation != "" {
				pod.Annotations = map[string]string{common.AnnotationCarbonIntensityThreshold: tt.podAnnotation}
			}

			result, err := e.EvaluateCarbonConstraints(ctx, pod)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ShouldDelay != tt.wantDelay {
				t.Errorf("ShouldDelay: got %v, want %v (intensity=%.0f threshold=%.0f annotation=%q)",
					result.ShouldDelay, tt.wantDelay, tt.currentIntensity, tt.globalThreshold, tt.podAnnotation)
			}
		})
	}
}

// --- EvaluateCarbonConstraints – implementation error is propagated ---

func TestEvaluateCarbonConstraints_Error(t *testing.T) {
	cfg := carbonOnlyConfig(200)
	e := NewEvaluator(testingmocks.NewMockCarbonWithError(), nil, cfg)
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}

	_, err := e.EvaluateCarbonConstraints(context.Background(), pod)
	if err == nil {
		t.Error("expected error from failing carbon implementation, got nil")
	}
}

// --- EvaluatePriceConstraints – TOU peak/off-peak ---

func TestEvaluatePriceConstraints_TOUPeakOffPeak(t *testing.T) {
	now := time.Now()
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
	cfg := priceOnlyConfig()

	tests := []struct {
		name      string
		isPeak    bool
		wantDelay bool
	}{
		{"peak time → delay", true, true},
		{"off-peak time → no delay", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEvaluator(nil, testingmocks.NewMockPricingWithPeakStatus(0.20, tt.isPeak), cfg)
			result, err := e.EvaluatePriceConstraints(pod, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ShouldDelay != tt.wantDelay {
				t.Errorf("ShouldDelay: got %v, want %v", result.ShouldDelay, tt.wantDelay)
			}
		})
	}
}

// --- EvaluatePriceConstraints – pod annotation threshold overrides TOU ---

func TestEvaluatePriceConstraints_PodAnnotationThreshold(t *testing.T) {
	now := time.Now()
	cfg := priceOnlyConfig()

	tests := []struct {
		name       string
		rate       float64
		isPeak     bool
		annotation string
		wantDelay  bool
	}{
		{
			name:       "rate exceeds annotation threshold → delay even off-peak",
			rate:       0.20,
			isPeak:     false,
			annotation: "0.10",
			wantDelay:  true,
		},
		{
			name:       "rate below annotation threshold → no delay even peak",
			rate:       0.10,
			isPeak:     true,
			annotation: "0.20",
			wantDelay:  false,
		},
		{
			name:       "invalid annotation falls back to TOU peak → delay",
			rate:       0.15,
			isPeak:     true,
			annotation: "not-a-number",
			wantDelay:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEvaluator(nil, testingmocks.NewMockPricingWithPeakStatus(tt.rate, tt.isPeak), cfg)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p",
					Namespace:   "default",
					Annotations: map[string]string{common.AnnotationPriceThreshold: tt.annotation},
				},
			}
			result, err := e.EvaluatePriceConstraints(pod, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ShouldDelay != tt.wantDelay {
				t.Errorf("ShouldDelay: got %v, want %v (rate=%.2f peak=%v annotation=%q)",
					result.ShouldDelay, tt.wantDelay, tt.rate, tt.isPeak, tt.annotation)
			}
		})
	}
}

// --- EvaluateAll – delay type combinations ---

func TestEvaluateAll_DelayTypeCombinations(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}

	tests := []struct {
		name          string
		carbonAbove   bool // intensity=250 vs 150 with threshold=200
		isPeak        bool
		wantDelay     bool
		wantDelayType string
	}{
		{"no constraints triggered → none", false, false, false, "none"},
		{"carbon only → carbon", true, false, true, "carbon"},
		{"price only → price", false, true, true, "price"},
		{"both triggered → both", true, true, true, "both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intensity := 150.0
			if tt.carbonAbove {
				intensity = 250.0
			}
			cfg := bothEnabledConfig(200.0)
			e := NewEvaluator(
				testingmocks.NewMockCarbon(intensity),
				testingmocks.NewMockPricingWithPeakStatus(0.20, tt.isPeak),
				cfg,
			)

			result, err := e.EvaluateAll(ctx, pod, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ShouldDelay != tt.wantDelay {
				t.Errorf("ShouldDelay: got %v, want %v", result.ShouldDelay, tt.wantDelay)
			}
			if result.DelayType != tt.wantDelayType {
				t.Errorf("DelayType: got %q, want %q", result.DelayType, tt.wantDelayType)
			}
		})
	}
}

// --- EvaluateAll – savings calculation ---

func TestEvaluateAll_SavingsCalculation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Pod: 2 CPUs (no memory/GPU requests) → powerW=20, generic→1h, energyKWh=0.02
	pod := podWithResources("2", "0", 0)
	// Add price threshold annotation so we get deterministic priceDelta
	pod.Annotations = map[string]string{common.AnnotationPriceThreshold: "0.10"}

	// carbon: current=300, threshold=200 → delta=100 → carbonSavings = 100 * 0.02 = 2.0 gCO2
	// price: rate=0.20 > annotation=0.10 → delta=0.10 → costSavings = 0.10 * 0.02 = 0.002 USD
	cfg := bothEnabledConfig(200.0)
	e := NewEvaluator(
		testingmocks.NewMockCarbon(300.0),
		testingmocks.NewMockPricingWithPeakStatus(0.20, false), // off-peak, threshold controls delay
		cfg,
	)

	result, err := e.EvaluateAll(ctx, pod, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.ShouldDelay {
		t.Fatal("expected delay")
	}
	if result.DelayType != "both" {
		t.Errorf("DelayType: got %q, want %q", result.DelayType, "both")
	}

	// energyKWh = 20W / 1000 * 1h = 0.02
	wantPower := 20.0
	if result.EstimatedPowerW != wantPower {
		t.Errorf("EstimatedPowerW: got %v, want %v", result.EstimatedPowerW, wantPower)
	}

	wantCarbonSavings := 100.0 * 0.02 // (300-200) * 0.02 kWh
	if result.EstimatedCarbonSavingsGCO2 != wantCarbonSavings {
		t.Errorf("EstimatedCarbonSavingsGCO2: got %v, want %v", result.EstimatedCarbonSavingsGCO2, wantCarbonSavings)
	}

	wantCostSavings := 0.10 * 0.02 // (0.20-0.10) * 0.02 kWh
	if result.EstimatedCostSavingsUSD != wantCostSavings {
		t.Errorf("EstimatedCostSavingsUSD: got %v, want %v", result.EstimatedCostSavingsUSD, wantCostSavings)
	}
}

func TestEvaluateAll_CarbonErrorGracefulDegradation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Carbon fails → treated as no carbon delay; price is still evaluated
	cfg := bothEnabledConfig(200.0)
	e := NewEvaluator(
		testingmocks.NewMockCarbonWithError(),
		testingmocks.NewMockPricingWithPeakStatus(0.20, true), // peak → price delay
		cfg,
	)
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}

	result, err := e.EvaluateAll(ctx, pod, now)
	if err != nil {
		t.Fatalf("EvaluateAll should not propagate carbon error, got: %v", err)
	}
	if !result.ShouldDelay {
		t.Error("expected price delay despite carbon error")
	}
	if result.DelayType != "price" {
		t.Errorf("DelayType: got %q, want %q", result.DelayType, "price")
	}
}
