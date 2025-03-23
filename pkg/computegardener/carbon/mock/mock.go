package mock

import (
	"context"
	"fmt"

	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
)

// MockCarbon implements the carbon.Implementation interface for testing
type MockCarbon struct {
	intensity float64
	errorMode bool
}

// New creates a new mock carbon implementation
func New(intensity float64) carbon.Implementation {
	return &MockCarbon{intensity: intensity, errorMode: false}
}

// NewWithError creates a new mock carbon implementation that returns errors
func NewWithError() carbon.Implementation {
	return &MockCarbon{intensity: 0, errorMode: true}
}

// GetCurrentIntensity returns the configured mock intensity
func (m *MockCarbon) GetCurrentIntensity(ctx context.Context) (float64, error) {
	if m.errorMode {
		return 0, fmt.Errorf("carbon API error (mock)")
	}
	return m.intensity, nil
}

// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
func (m *MockCarbon) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
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

// MockCarbonImplementation is another mock implementation that provides more control for tests
type MockCarbonImplementation struct {
	GetCurrentIntensityFunc       func(ctx context.Context) (float64, error)
	CheckIntensityConstraintsFunc func(ctx context.Context, threshold float64) *framework.Status
}

// GetCurrentIntensity delegates to the mock function
func (m *MockCarbonImplementation) GetCurrentIntensity(ctx context.Context) (float64, error) {
	if m.GetCurrentIntensityFunc != nil {
		return m.GetCurrentIntensityFunc(ctx)
	}
	return 0, nil
}

// CheckIntensityConstraints delegates to the mock function
func (m *MockCarbonImplementation) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	if m.CheckIntensityConstraintsFunc != nil {
		return m.CheckIntensityConstraintsFunc(ctx, threshold)
	}
	return framework.NewStatus(framework.Success, "")
}
