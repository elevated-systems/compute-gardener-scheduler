package testing

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
)

// MockCarbonImplementation implements carbon.Implementation for testing
// Supports both simple fixed values and function overrides for more complex scenarios
type MockCarbonImplementation struct {
	// Simple mode fields
	intensity float64
	errorMode bool

	// Advanced mode function overrides (when set, these take precedence)
	GetCurrentIntensityFunc       func(ctx context.Context) (float64, error)
	CheckIntensityConstraintsFunc func(ctx context.Context, threshold float64) *framework.Status
}

// NewMockCarbon creates a new mock carbon implementation with fixed intensity
func NewMockCarbon(intensity float64) carbon.Implementation {
	return &MockCarbonImplementation{intensity: intensity, errorMode: false}
}

// NewMockCarbonWithError creates a new mock carbon implementation that returns errors
func NewMockCarbonWithError() carbon.Implementation {
	return &MockCarbonImplementation{intensity: 0, errorMode: true}
}

func (m *MockCarbonImplementation) GetCurrentIntensity(ctx context.Context) (float64, error) {
	// Function override takes precedence
	if m.GetCurrentIntensityFunc != nil {
		return m.GetCurrentIntensityFunc(ctx)
	}

	// Simple mode behavior
	if m.errorMode {
		return 0, fmt.Errorf("carbon API error (mock)")
	}
	return m.intensity, nil
}

func (m *MockCarbonImplementation) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	// Function override takes precedence
	if m.CheckIntensityConstraintsFunc != nil {
		return m.CheckIntensityConstraintsFunc(ctx, threshold)
	}

	// Default behavior
	intensity, err := m.GetCurrentIntensity(ctx)
	if err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	return framework.NewStatus(framework.Success, "")
}

// MockPriceImplementation implements price.Implementation for testing
// Supports both simple fixed values and function overrides for more complex scenarios
type MockPriceImplementation struct {
	// Simple mode fields
	rate   float64
	isPeak bool

	// Advanced mode function overrides (when set, these take precedence)
	GetCurrentRateFunc        func(currentTime time.Time) float64
	IsPeakTimeFunc            func(currentTime time.Time) bool
	CheckPriceConstraintsFunc func(pod *v1.Pod, currentTime time.Time) *framework.Status
}

// NewMockPricing creates a new mock pricing implementation with fixed rate
func NewMockPricing(rate float64) price.Implementation {
	return &MockPriceImplementation{rate: rate, isPeak: false}
}

// NewMockPricingWithPeakStatus creates a new mock pricing with specific peak status
func NewMockPricingWithPeakStatus(rate float64, isPeak bool) price.Implementation {
	return &MockPriceImplementation{rate: rate, isPeak: isPeak}
}

func (m *MockPriceImplementation) GetCurrentRate(now time.Time) float64 {
	// Function override takes precedence
	if m.GetCurrentRateFunc != nil {
		return m.GetCurrentRateFunc(now)
	}

	// Simple mode behavior
	return m.rate
}

func (m *MockPriceImplementation) IsPeakTime(now time.Time) bool {
	// Function override takes precedence
	if m.IsPeakTimeFunc != nil {
		return m.IsPeakTimeFunc(now)
	}

	// Simple mode behavior
	return m.isPeak
}

func (m *MockPriceImplementation) IsCurrentlyPeakTime(now time.Time) bool {
	return m.IsPeakTime(now)
}

func (m *MockPriceImplementation) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	// Function override takes precedence
	if m.CheckPriceConstraintsFunc != nil {
		return m.CheckPriceConstraintsFunc(pod, now)
	}

	// Default behavior - respect pod's price threshold annotation
	rate := m.GetCurrentRate(now)
	threshold := 0.15 // Default test threshold

	// Check if pod has custom threshold annotation
	if val, ok := pod.Annotations[common.AnnotationPriceThreshold]; ok {
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
		} else {
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	}

	if rate > threshold {
		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current electricity rate ($%.3f/kWh) exceeds threshold ($%.3f/kWh)",
				rate, threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

// MockCoreMetricsClient implements CoreMetricsClient for testing
type MockCoreMetricsClient struct {
	GetPodMetricsFunc  func(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error)
	ListPodMetricsFunc func(ctx context.Context) ([]metricsv1beta1.PodMetrics, error)
}

func (m *MockCoreMetricsClient) GetPodMetrics(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error) {
	if m.GetPodMetricsFunc != nil {
		return m.GetPodMetricsFunc(ctx, namespace, name)
	}
	return nil, nil
}

func (m *MockCoreMetricsClient) ListPodMetrics(ctx context.Context) ([]metricsv1beta1.PodMetrics, error) {
	if m.ListPodMetricsFunc != nil {
		return m.ListPodMetricsFunc(ctx)
	}
	return nil, nil
}

// MockHardwareProfiles implements a hardware profiler for testing
type MockHardwareProfiles struct {
	GetNodePowerProfileFunc func(node *v1.Node) (*config.NodePower, error)
	GetNodeHardwareInfoFunc func(node *v1.Node) (string, string)
	RefreshNodeCacheFunc    func(node *v1.Node)
	GetEffectivePowerFunc   func(profile *config.NodePower, idle bool) float64
}

func (m *MockHardwareProfiles) GetNodePowerProfile(node *v1.Node) (*config.NodePower, error) {
	if m.GetNodePowerProfileFunc != nil {
		return m.GetNodePowerProfileFunc(node)
	}
	return &config.NodePower{
		IdlePower: 100,
		MaxPower:  400,
		PUE:       1.15,
	}, nil
}

func (m *MockHardwareProfiles) GetNodeHardwareInfo(node *v1.Node) (string, string) {
	if m.GetNodeHardwareInfoFunc != nil {
		return m.GetNodeHardwareInfoFunc(node)
	}
	return "CPU", "GPU"
}

func (m *MockHardwareProfiles) RefreshNodeCache(node *v1.Node) {
	if m.RefreshNodeCacheFunc != nil {
		m.RefreshNodeCacheFunc(node)
	}
}

func (m *MockHardwareProfiles) GetEffectivePower(profile *config.NodePower, idle bool) float64 {
	if m.GetEffectivePowerFunc != nil {
		return m.GetEffectivePowerFunc(profile, idle)
	}
	if idle {
		return profile.IdlePower * profile.PUE
	}
	return profile.MaxPower * profile.PUE
}
