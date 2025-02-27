package mock

import (
	"time"

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
