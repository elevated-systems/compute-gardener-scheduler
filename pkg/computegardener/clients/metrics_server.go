package clients

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1client "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

// CoreMetricsClient defines the interface for retrieving CPU and memory metrics
type CoreMetricsClient interface {
	// GetPodMetrics retrieves metrics for a specific pod
	GetPodMetrics(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error)

	// ListPodMetrics retrieves metrics for all pods
	ListPodMetrics(ctx context.Context) ([]metricsv1beta1.PodMetrics, error)
}

// GPUMetricsClient defines the interface for retrieving GPU metrics
type GPUMetricsClient interface {
	// GetPodGPUUtilization gets GPU utilization (0-1) for a specific pod
	GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error)

	// ListPodsGPUUtilization gets GPU utilization for all pods with GPUs
	ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) // key: namespace/name

	// GetPodGPUPower gets average GPU power use in watts for a specific pod
	GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error)

	// ListPodsGPUPower gets average GPU power for all pods with GPUs
	ListPodsGPUPower(ctx context.Context) (map[string]float64, error) // key: namespace/name
}

// K8sMetricsClient implements CoreMetricsClient using the Kubernetes metrics API
type K8sMetricsClient struct {
	client metricsv1beta1client.PodMetricsInterface
}

// NewK8sMetricsClient creates a new metrics client
func NewK8sMetricsClient(client metricsv1beta1client.PodMetricsInterface) *K8sMetricsClient {
	return &K8sMetricsClient{
		client: client,
	}
}

// GetPodMetrics retrieves metrics for a specific pod
func (c *K8sMetricsClient) GetPodMetrics(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error) {
	return c.client.Get(ctx, name, metav1.GetOptions{})
}

// ListPodMetrics retrieves metrics for all pods
func (c *K8sMetricsClient) ListPodMetrics(ctx context.Context) ([]metricsv1beta1.PodMetrics, error) {
	list, err := c.client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// NullGPUMetricsClient is a placeholder implementation when GPU metrics are not available
type NullGPUMetricsClient struct{}

// GetPodGPUUtilization always returns 0 for GPU utilization
func (c *NullGPUMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
	return 0, nil
}

// ListPodsGPUUtilization returns an empty map
func (c *NullGPUMetricsClient) ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) {
	return make(map[string]float64), nil
}

// GetPodGPUPower always returns 0 for GPU power
func (c *NullGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	return 0, nil
}

// ListPodsGPUPower returns an empty map
func (c *NullGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]float64, error) {
	return make(map[string]float64), nil
}

// NewNullGPUMetricsClient creates a placeholder GPU metrics client
func NewNullGPUMetricsClient() *NullGPUMetricsClient {
	return &NullGPUMetricsClient{}
}

// MockCoreMetricsClient implements CoreMetricsClient for testing
type MockCoreMetricsClient struct {
	pods map[string]*metricsv1beta1.PodMetrics // key: namespace/name
}

// NewMockCoreMetricsClient creates a new mock metrics client
func NewMockCoreMetricsClient() *MockCoreMetricsClient {
	return &MockCoreMetricsClient{
		pods: make(map[string]*metricsv1beta1.PodMetrics),
	}
}

// GetPodMetrics retrieves metrics for a specific pod
func (c *MockCoreMetricsClient) GetPodMetrics(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error) {
	key := namespace + "/" + name
	if pod, exists := c.pods[key]; exists {
		return pod, nil
	}
	return nil, nil
}

// ListPodMetrics retrieves metrics for all pods
func (c *MockCoreMetricsClient) ListPodMetrics(ctx context.Context) ([]metricsv1beta1.PodMetrics, error) {
	result := make([]metricsv1beta1.PodMetrics, 0, len(c.pods))
	for _, pod := range c.pods {
		result = append(result, *pod)
	}
	return result, nil
}

// AddPodMetrics adds a pod to the mock
func (c *MockCoreMetricsClient) AddPodMetrics(pod *metricsv1beta1.PodMetrics) {
	key := pod.Namespace + "/" + pod.Name
	c.pods[key] = pod
}

// MockGPUMetricsClient implements GPUMetricsClient for testing
type MockGPUMetricsClient struct {
	utilization map[string]float64 // key: namespace/name
	power       map[string]float64 // key: namespace/name
}

// NewMockGPUMetricsClient creates a new mock GPU metrics client
func NewMockGPUMetricsClient() *MockGPUMetricsClient {
	return &MockGPUMetricsClient{
		utilization: make(map[string]float64),
		power:       make(map[string]float64),
	}
}

// GetPodGPUUtilization gets GPU utilization for a specific pod
func (c *MockGPUMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
	key := namespace + "/" + name
	if util, exists := c.utilization[key]; exists {
		return util, nil
	}
	return 0, nil
}

// ListPodsGPUUtilization gets GPU utilization for all pods
func (c *MockGPUMetricsClient) ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) {
	// Return a copy to prevent modification
	result := make(map[string]float64, len(c.utilization))
	for k, v := range c.utilization {
		result[k] = v
	}
	return result, nil
}

// GetPodGPUPower gets GPU power for a specific pod
func (c *MockGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	key := namespace + "/" + name
	if power, exists := c.power[key]; exists {
		return power, nil
	}
	return 0, nil
}

// ListPodsGPUPower gets GPU power for all pods
func (c *MockGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]float64, error) {
	// Return a copy to prevent modification
	result := make(map[string]float64, len(c.power))
	for k, v := range c.power {
		result[k] = v
	}
	return result, nil
}

// SetPodGPUUtilization sets the GPU utilization for a pod in the mock
func (c *MockGPUMetricsClient) SetPodGPUUtilization(namespace, name string, utilization float64) {
	key := namespace + "/" + name
	c.utilization[key] = utilization
}

// SetPodGPUPower sets the GPU power for a pod in the mock
func (c *MockGPUMetricsClient) SetPodGPUPower(namespace, name string, power float64) {
	key := namespace + "/" + name
	c.power[key] = power
}
