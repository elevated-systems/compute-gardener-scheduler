package metrics

import (
	"context"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics/clients"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics/powerprovider"
	v1 "k8s.io/api/core/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/klog/v2"
)

// MetricsCoordinator provides domain-specific "shapes" of metrics data by orchestrating
// between specialized metrics clients. Acts like a gateway, of sorts - callers specify
// the data shape they need, coordinator figures out how to fetch it efficiently.
type MetricsCoordinator struct {
	// Specialized clients - each maintains its own domain expertise
	coreMetrics clients.CoreMetricsClient        // K8s metrics-server (CPU/Memory)
	gpuMetrics  clients.GPUMetricsClient         // GPU utilization/power
	promClient  *clients.PrometheusMetricsClient // Historical + node-exporter data

	// Power provider integration (Path A: respect existing system)
	powerProvider powerprovider.PowerInfoProvider
}

// NewMetricsCoordinator creates a coordinator that orchestrates between specialized clients
func NewMetricsCoordinator(
	coreMetrics clients.CoreMetricsClient,
	gpuMetrics clients.GPUMetricsClient,
	promClient *clients.PrometheusMetricsClient,
	powerProvider powerprovider.PowerInfoProvider,
) *MetricsCoordinator {
	return &MetricsCoordinator{
		coreMetrics:   coreMetrics,
		gpuMetrics:    gpuMetrics,
		promClient:    promClient,
		powerProvider: powerProvider,
	}
}

// SchedulingMetrics represents the "shape" of data needed for scheduling decisions
type SchedulingMetrics struct {
	// Core resource metrics (always available)
	PodMetrics *metricsv1beta1.PodMetrics

	// GPU metrics (optional - soft fail if not available)
	GPUUtilization *float64 // nil if no GPU or metrics unavailable
	GPUPower       *float64 // nil if no GPU or metrics unavailable

	// Power and efficiency data (respects power provider hierarchy)
	PowerLimitsAvailable bool

	// Node context for power calculations
	Node *v1.Node
}

// GetSchedulingMetrics fetches the specific "shape" of data needed for scheduling decisions.
// This demonstrates the GraphQL-like approach: caller specifies their data needs,
// coordinator intelligently orchestrates the underlying clients.
func (mc *MetricsCoordinator) GetSchedulingMetrics(
	ctx context.Context,
	pod *v1.Pod,
	node *v1.Node,
) (*SchedulingMetrics, error) {

	result := &SchedulingMetrics{
		Node: node,
	}

	// 1. Always try to get core metrics (CPU/Memory) - required for scheduling
	if mc.coreMetrics != nil {
		podMetrics, err := mc.coreMetrics.GetPodMetrics(ctx, pod.Namespace, pod.Name)
		if err != nil {
			klog.V(2).InfoS("Core metrics unavailable for pod",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"error", err)
			// Don't fail hard - pod might not be running yet
		} else {
			result.PodMetrics = podMetrics
		}
	}

	// 2. Try to get GPU metrics (soft fail - GPUs are optional)
	if mc.gpuMetrics != nil && mc.hasGPURequest(pod) {
		if gpuUtil, err := mc.gpuMetrics.GetPodGPUUtilization(ctx, pod.Namespace, pod.Name); err == nil {
			result.GPUUtilization = &gpuUtil
		} else {
			klog.V(2).InfoS("GPU utilization unavailable for pod",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"error", err)
		}

		if gpuPower, err := mc.gpuMetrics.GetPodGPUPower(ctx, pod.Namespace, pod.Name); err == nil {
			result.GPUPower = &gpuPower
		} else {
			klog.V(2).InfoS("GPU power unavailable for pod",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"error", err)
		}
	}

	// 3. Check power limits availability (Path A: delegate to existing power provider)
	if mc.powerProvider != nil {
		result.PowerLimitsAvailable = mc.powerProvider.IsAvailable(node)
	}

	return result, nil
}

// ShouldCollectInternalPowerMetrics determines if we should run internal power collection
// for this node, or if an external source (like Kepler) is already providing power data.
// This respects the existing power provider hierarchy (Path A approach).
func (mc *MetricsCoordinator) ShouldCollectInternalPowerMetrics(node *v1.Node) bool {
	if mc.powerProvider == nil {
		return true // No power provider configured, collect internal metrics
	}

	// Path A: Respect existing power provider system
	// If Kepler is active, don't duplicate collection efforts
	if mc.powerProvider.IsAvailable(node) {
		providerName := mc.powerProvider.GetProviderName()
		if providerName == "Kepler-Measured" {
			klog.V(2).InfoS("Kepler power provider active, skipping internal power collection",
				"node", node.Name,
				"provider", providerName)
			return false
		}
	}

	return true
}

// hasGPURequest checks if the pod requests GPU resources
func (mc *MetricsCoordinator) hasGPURequest(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if _, hasNvidiaGPU := container.Resources.Requests["nvidia.com/gpu"]; hasNvidiaGPU {
			return true
		}
		if _, hasAMDGPU := container.Resources.Requests["amd.com/gpu"]; hasAMDGPU {
			return true
		}
	}
	return false
}

// CollectionMetrics represents the "shape" of data needed for metrics collection
type CollectionMetrics struct {
	// All running pods with their core metrics
	PodMetrics []metricsv1beta1.PodMetrics

	// GPU power measurements keyed by pod namespace/name or GPU UUID
	GPUPowerData map[string]float64

	// Environmental context
	CarbonIntensity  float64
	ElectricityRate  float64
	Timestamp        string
}

// GetCollectionMetrics fetches the "shape" of data needed for periodic metrics collection.
// This method coordinates fetching bulk metrics data efficiently.
func (mc *MetricsCoordinator) GetCollectionMetrics(ctx context.Context) (*CollectionMetrics, error) {
	result := &CollectionMetrics{
		GPUPowerData: make(map[string]float64),
		Timestamp: ctx.Value("timestamp").(string), // Expected to be provided by caller
	}

	// 1. Get metrics for all pods from core metrics client
	if mc.coreMetrics != nil {
		podMetrics, err := mc.coreMetrics.ListPodMetrics(ctx)
		if err != nil {
			klog.ErrorS(err, "Failed to list pod metrics from core client")
			return nil, err
		}
		result.PodMetrics = podMetrics
		klog.V(3).InfoS("Retrieved pod metrics from core client", "count", len(podMetrics))
	}

	// 2. Get GPU power measurements for all pods (soft fail)
	if mc.gpuMetrics != nil {
		if gpuPowerData, err := mc.gpuMetrics.ListPodsGPUPower(ctx); err == nil {
			result.GPUPowerData = gpuPowerData
			klog.V(2).InfoS("Retrieved GPU power measurements", "count", len(gpuPowerData))
		} else {
			klog.V(2).InfoS("GPU power measurements unavailable", "error", err)
		}
	}

	// Note: Environmental data (carbon intensity, electricity rate) is typically
	// fetched by the caller and passed in via context or separate methods.
	// This coordinator focuses on core + GPU metrics orchestration.

	return result, nil
}

// CompletionMetrics represents the "shape" of data needed for pod completion processing
type CompletionMetrics struct {
	// Historical metrics for the completed pod
	HistoricalRecords []PodMetricsRecord

	// Final power and energy calculations
	TotalEnergyKWh   float64
	AveragePowerW    float64
	PeakPowerW       float64

	// GPU-specific completion data (optional)
	GPUEnergyKWh     *float64
	GPUAveragePowerW *float64
}

// GetCompletionMetrics fetches the "shape" of data needed for pod completion processing.
// This method provides historical analysis and final energy calculations.
func (mc *MetricsCoordinator) GetCompletionMetrics(
	ctx context.Context,
	podUID string,
	history *PodMetricsHistory,
) (*CompletionMetrics, error) {
	result := &CompletionMetrics{
		HistoricalRecords: history.Records,
	}

	// 1. Calculate total energy consumption using utility functions
	if len(history.Records) > 0 {
		result.TotalEnergyKWh = CalculateTotalEnergy(history.Records)
		result.AveragePowerW = CalculateAveragePower(history.Records)
		result.PeakPowerW = CalculatePeakPower(history.Records)
	}

	// 2. GPU-specific completion metrics (if this was a GPU pod)
	if mc.gpuMetrics != nil && mc.promClient != nil {
		// Check if we have any GPU power in the historical records
		hasGPUData := false
		for _, record := range history.Records {
			if record.GPUPowerWatts > 0 {
				hasGPUData = true
				break
			}
		}

		if hasGPUData {
			// Calculate GPU-specific energy metrics
			gpuEnergy := CalculateGPUEnergy(history.Records)
			gpuAvgPower := CalculateAverageGPUPower(history.Records)

			result.GPUEnergyKWh = &gpuEnergy
			result.GPUAveragePowerW = &gpuAvgPower

			klog.V(2).InfoS("Calculated GPU completion metrics",
				"podUID", podUID,
				"gpuEnergy", gpuEnergy,
				"gpuAvgPower", gpuAvgPower)
		}
	}

	klog.V(2).InfoS("Calculated completion metrics",
		"podUID", podUID,
		"totalEnergy", result.TotalEnergyKWh,
		"avgPower", result.AveragePowerW,
		"peakPower", result.PeakPowerW,
		"hasGPUData", result.GPUEnergyKWh != nil)

	return result, nil
}

// GetUnderlyingClients provides access to underlying specialized clients when needed.
// This escape hatch allows callers to access client-specific functionality while
// still benefiting from coordinator orchestration for common use cases.
func (mc *MetricsCoordinator) GetUnderlyingClients() (clients.CoreMetricsClient, clients.GPUMetricsClient, *clients.PrometheusMetricsClient) {
	return mc.coreMetrics, mc.gpuMetrics, mc.promClient
}
