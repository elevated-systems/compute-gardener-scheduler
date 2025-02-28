package mock

import (
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing"
)

// MockPricing implements the pricing.Implementation interface for testing
type MockPricing struct {
	rate float64
}

// New creates a new mock pricing implementation
func New(rate float64) pricing.Implementation {
	return &MockPricing{rate: rate}
}

// GetCurrentRate returns the configured mock rate
func (m *MockPricing) GetCurrentRate(now time.Time) float64 {
	return m.rate
}

// CheckPriceConstraints checks if current electricity rate exceeds pod's threshold
func (m *MockPricing) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	rate := m.GetCurrentRate(now)

	// Get threshold from pod annotation or use 0.15 as default threshold for testing
	threshold := 0.15 // Default test threshold
	if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/price-threshold"]; ok {
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
