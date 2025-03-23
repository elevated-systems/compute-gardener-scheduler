package mock

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// MockCoreMetricsClient implements CoreMetricsClient for testing
type MockCoreMetricsClient struct {
	GetPodMetricsFunc  func(ctx context.Context, podName string, podNamespace string) (*metricsv1beta1.PodMetrics, error)
	ListPodMetricsFunc func(ctx context.Context) ([]metricsv1beta1.PodMetrics, error)
}

// GetPodMetrics delegates to the mock function
func (m *MockCoreMetricsClient) GetPodMetrics(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error) {
	if m.GetPodMetricsFunc != nil {
		return m.GetPodMetricsFunc(ctx, name, namespace)
	}
	return nil, nil
}

// ListPodMetrics delegates to the mock function
func (m *MockCoreMetricsClient) ListPodMetrics(ctx context.Context) ([]metricsv1beta1.PodMetrics, error) {
	if m.ListPodMetricsFunc != nil {
		return m.ListPodMetricsFunc(ctx)
	}
	return nil, nil
}

// MockGPUMetricsClient implements GPUMetricsClient for testing
type MockGPUMetricsClient struct {
	GetPodGPUUtilizationFunc    func(ctx context.Context, namespace, name string) (float64, error)
	ListPodsGPUUtilizationFunc  func(ctx context.Context) (map[string]float64, error)
	GetPodGPUPowerFunc          func(ctx context.Context, namespace, name string) (float64, error)
	ListPodsGPUPowerFunc        func(ctx context.Context) (map[string]float64, error)
}

// GetPodGPUUtilization delegates to the mock function
func (m *MockGPUMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
	if m.GetPodGPUUtilizationFunc != nil {
		return m.GetPodGPUUtilizationFunc(ctx, namespace, name)
	}
	return 0, nil
}

// ListPodsGPUUtilization delegates to the mock function
func (m *MockGPUMetricsClient) ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) {
	if m.ListPodsGPUUtilizationFunc != nil {
		return m.ListPodsGPUUtilizationFunc(ctx)
	}
	return make(map[string]float64), nil
}

// GetPodGPUPower delegates to the mock function
func (m *MockGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	if m.GetPodGPUPowerFunc != nil {
		return m.GetPodGPUPowerFunc(ctx, namespace, name)
	}
	return 0, nil
}

// ListPodsGPUPower delegates to the mock function
func (m *MockGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]float64, error) {
	if m.ListPodsGPUPowerFunc != nil {
		return m.ListPodsGPUPowerFunc(ctx)
	}
	return make(map[string]float64), nil
}

// MockHardwareProfiles implements a hardware profiler for testing
type MockHardwareProfiles struct {
	GetNodePowerProfileFunc func(node *v1.Node) (*config.NodePower, error)
	GetNodeHardwareInfoFunc func(node *v1.Node) (string, string)
	RefreshNodeCacheFunc    func(node *v1.Node)
	GetEffectivePowerFunc   func(profile *config.NodePower, idle bool) float64
}

// GetNodePowerProfile delegates to the mock function
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

// GetNodeHardwareInfo delegates to the mock function
func (m *MockHardwareProfiles) GetNodeHardwareInfo(node *v1.Node) (string, string) {
	if m.GetNodeHardwareInfoFunc != nil {
		return m.GetNodeHardwareInfoFunc(node)
	}
	return "CPU", "GPU"
}

// RefreshNodeCache delegates to the mock function
func (m *MockHardwareProfiles) RefreshNodeCache(node *v1.Node) {
	if m.RefreshNodeCacheFunc != nil {
		m.RefreshNodeCacheFunc(node)
	}
}

// GetEffectivePower delegates to the mock function
func (m *MockHardwareProfiles) GetEffectivePower(profile *config.NodePower, idle bool) float64 {
	if m.GetEffectivePowerFunc != nil {
		return m.GetEffectivePowerFunc(profile, idle)
	}
	if idle {
		return profile.IdlePower * profile.PUE
	}
	return profile.MaxPower * profile.PUE
}