package metrics

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
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

	// ListPodsGPUPower gets average GPU power per GPU device on each node
	ListPodsGPUPower(ctx context.Context) (map[string]map[string]float64, error) // key: NodeName -> UUID -> Power
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
func (c *NullGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]map[string]float64, error) {
	return make(map[string]map[string]float64), nil
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

// MockPodAssignment stores mock assignment info for GetPodGPUPower
type MockPodAssignment struct {
	NodeName string
	UUIDs    []string
}

// MockGPUMetricsClient implements GPUMetricsClient for testing
type MockGPUMetricsClient struct {
	utilization    map[string]float64            // key: namespace/name
	power          map[string]map[string]float64 // key: nodeName -> uuid -> power
	podAssignments map[string]MockPodAssignment  // key: namespace/name -> assignment info
}

// NewMockGPUMetricsClient creates a new mock GPU metrics client
func NewMockGPUMetricsClient() *MockGPUMetricsClient {
	return &MockGPUMetricsClient{
		utilization:    make(map[string]float64),
		power:          make(map[string]map[string]float64),
		podAssignments: make(map[string]MockPodAssignment),
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

// GetPodGPUPower gets GPU power for a specific pod based on mock assignments
func (c *MockGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	key := namespace + "/" + name
	assignment, exists := c.podAssignments[key]
	if !exists {
		klog.V(2).Infof("MockGPUMetricsClient: No assignment found for pod %s", key)
		return 0, fmt.Errorf("mock assignment not found for pod %s/%s", namespace, name)
	}

	nodePowerMap, nodeExists := c.power[assignment.NodeName]
	if !nodeExists {
		klog.V(2).Infof("MockGPUMetricsClient: No power data found for node %s", assignment.NodeName)
		return 0, fmt.Errorf("mock power data not found for node %s", assignment.NodeName)
	}

	totalPower := 0.0
	foundGPUs := 0
	for _, uuid := range assignment.UUIDs {
		if powerVal, uuidExists := nodePowerMap[uuid]; uuidExists {
			totalPower += powerVal
			foundGPUs++
		} else {
			klog.Warningf("MockGPUMetricsClient: Power data for assigned UUID %s on node %s not found", uuid, assignment.NodeName)
		}
	}

	if foundGPUs == 0 && len(assignment.UUIDs) > 0 {
		klog.Warningf("MockGPUMetricsClient: Pod %s assigned GPUs %v on node %s, but no power data found for these UUIDs", key, assignment.UUIDs, assignment.NodeName)
		// Return error or 0? Let's return 0 for now.
		return 0, fmt.Errorf("mock power data not found for assigned UUIDs %v on node %s", assignment.UUIDs, assignment.NodeName)
	}

	klog.V(2).Infof("MockGPUMetricsClient: Calculated power %f for pod %s (Node: %s, UUIDs: %v)", totalPower, key, assignment.NodeName, assignment.UUIDs)
	return totalPower, nil
}

// ListPodsGPUPower gets GPU power per node and UUID
func (c *MockGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]map[string]float64, error) {
	// Return a deep copy to prevent modification
	result := make(map[string]map[string]float64, len(c.power))
	for node, nodeData := range c.power {
		result[node] = make(map[string]float64, len(nodeData))
		for uuid, powerVal := range nodeData {
			result[node][uuid] = powerVal
		}
	}
	return result, nil
}

// SetPodGPUUtilization sets the GPU utilization for a pod in the mock
func (c *MockGPUMetricsClient) SetPodGPUUtilization(namespace, name string, utilization float64) {
	key := namespace + "/" + name
	c.utilization[key] = utilization
}

// SetNodeGPUPower sets the GPU power for a specific GPU on a node in the mock
func (c *MockGPUMetricsClient) SetNodeGPUPower(nodeName, uuid string, power float64) {
	if _, ok := c.power[nodeName]; !ok {
		c.power[nodeName] = make(map[string]float64)
	}
	c.power[nodeName][uuid] = power
}

// AssignPodToGPU sets the mock assignment of a pod to specific GPUs on a node
func (c *MockGPUMetricsClient) AssignPodToGPU(namespace, name, nodeName string, uuids []string) {
	key := namespace + "/" + name
	c.podAssignments[key] = MockPodAssignment{
		NodeName: nodeName,
		UUIDs:    uuids,
	}
}
