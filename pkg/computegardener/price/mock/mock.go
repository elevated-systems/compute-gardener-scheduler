package mock

import (
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
)

// MockPricing implements the price.Implementation interface for testing
type MockPricing struct {
	rate   float64
	isPeak bool
}

// New creates a new mock pricing implementation
func New(rate float64) price.Implementation {
	return &MockPricing{rate: rate, isPeak: false}
}

// NewWithPeakStatus creates a new mock pricing with specific peak status
func NewWithPeakStatus(rate float64, isPeak bool) price.Implementation {
	return &MockPricing{rate: rate, isPeak: isPeak}
}

// GetCurrentRate returns the configured mock rate
func (m *MockPricing) GetCurrentRate(now time.Time) float64 {
	return m.rate
}

// IsPeakTime returns whether the given time is in a peak period
func (m *MockPricing) IsPeakTime(now time.Time) bool {
	return m.isPeak
}

// IsCurrentlyPeakTime is deprecated, use IsPeakTime instead
func (m *MockPricing) IsCurrentlyPeakTime(now time.Time) bool {
	return m.IsPeakTime(now)
}

// CheckPriceConstraints checks if current electricity rate exceeds pod's threshold
func (m *MockPricing) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	rate := m.GetCurrentRate(now)

	// Get threshold from pod annotation or use 0.15 as default threshold for testing
	threshold := 0.15 // Default test threshold
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
				rate,
				threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

// MockPriceImplementation provides more control for testing with function overrides
type MockPriceImplementation struct {
	GetCurrentRateFunc        func(currentTime time.Time) float64
	IsPeakTimeFunc            func(currentTime time.Time) bool
	CheckPriceConstraintsFunc func(pod *v1.Pod, currentTime time.Time) *framework.Status
}

// GetCurrentRate delegates to the mock function
func (m *MockPriceImplementation) GetCurrentRate(now time.Time) float64 {
	if m.GetCurrentRateFunc != nil {
		return m.GetCurrentRateFunc(now)
	}
	return 0.10 // Default off-peak rate
}

// IsPeakTime delegates to the mock function
func (m *MockPriceImplementation) IsPeakTime(now time.Time) bool {
	if m.IsPeakTimeFunc != nil {
		return m.IsPeakTimeFunc(now)
	}
	return false // Default not peak
}

// IsCurrentlyPeakTime delegates to IsPeakTime
func (m *MockPriceImplementation) IsCurrentlyPeakTime(now time.Time) bool {
	return m.IsPeakTime(now)
}

// CheckPriceConstraints delegates to the mock function
func (m *MockPriceImplementation) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	if m.CheckPriceConstraintsFunc != nil {
		return m.CheckPriceConstraintsFunc(pod, now)
	}

	rate := m.GetCurrentRate(now)
	threshold := 0.15 // Default test threshold

	if rate > threshold {
		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current electricity rate ($%.3f/kWh) exceeds threshold ($%.3f/kWh)",
				rate, threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}
