package metrics

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// PodMetricsStorage defines the interface for storing pod metrics
type PodMetricsStorage interface {
	// AddRecord adds a new metrics record for a pod
	AddRecord(podUID, podName, namespace, nodeName string, record PodMetricsRecord)

	// MarkCompleted marks a pod as completed to prevent further metrics collection
	MarkCompleted(podUID string)

	// GetHistory retrieves the full metrics history for a pod
	GetHistory(podUID string) (*PodMetricsHistory, bool)

	// Cleanup removes old completed pod data
	Cleanup()

	// Close releases resources
	Close()

	// Size returns the number of pods being tracked
	Size() int

	// ForEach executes a function for each pod history in the store
	ForEach(func(string, *PodMetricsHistory))
}

// DownsamplingStrategy defines how to reduce the number of metrics points
// while preserving the overall shape of the time series
type DownsamplingStrategy interface {
	// Downsample reduces the number of data points while preserving trend
	Downsample(records []PodMetricsRecord, targetCount int) []PodMetricsRecord
}

// CalculatePodMetrics converts Kubernetes metrics to our internal format
func CalculatePodMetrics(
	podMetrics *metricsv1beta1.PodMetrics,
	pod *corev1.Pod,
	gpuPowerWatts float64,
	carbonIntensity float64,
	carbonDataStatus string,
	calculatePower func(nodeName string, cpu, memory, gpu float64) float64,
) PodMetricsRecord {
	// Sum CPU and memory usage across all containers
	var totalCPU float64
	var totalMemory float64

	for _, container := range podMetrics.Containers {
		cpuQuantity := container.Usage.Cpu()
		memQuantity := container.Usage.Memory()

		totalCPU += float64(cpuQuantity.MilliValue()) / 1000
		totalMemory += float64(memQuantity.Value())
	}

	// Calculate power estimate using the provided calculator function
	nodeName := pod.Spec.NodeName
	estimatedPower := calculatePower(nodeName, totalCPU, totalMemory, gpuPowerWatts)

	// Return a record with all the fields including 0 for ElectricityRate
	// ElectricityRate will be set in the metrics collector if available
	return PodMetricsRecord{
		Timestamp:        podMetrics.Timestamp.Time,
		CPU:              totalCPU,
		Memory:           totalMemory,
		GPUPowerWatts:    gpuPowerWatts,
		PowerEstimate:    estimatedPower,
		CarbonIntensity:  carbonIntensity,
		CarbonDataStatus: carbonDataStatus,
		ElectricityRate:  0, // Will be set later if available
	}
}

// PodMetricsRecord represents a point-in-time measurement of pod resource usage
type PodMetricsRecord struct {
	Timestamp        time.Time
	CPU              float64 // CPU usage in cores
	Memory           float64 // Memory usage in bytes
	GPUPowerWatts    float64 // GPU power in Watts
	PowerEstimate    float64 // Estimated power at this point across all hw in Watts
	CarbonIntensity  float64 // Carbon intensity at this point in gCO2eq/kWh
	CarbonDataStatus string  // Carbon data quality: "real" or "estimated"
	ElectricityRate  float64 // Electricity rate at this point in $/kWh
}

// PodMetricsHistory stores a time series of pod metrics
type PodMetricsHistory struct {
	PodUID     string
	PodName    string
	Namespace  string
	NodeName   string
	Records    []PodMetricsRecord
	StartTime  time.Time
	LastSeen   time.Time
	MaxRecords int // Configurable limit on records to prevent unbounded growth
	Completed  bool
	TotalPower float64 // Cumulative power in kWh
	TotalCO2   float64 // Cumulative CO2 in grams
	TotalCost  float64 // Cumulative cost in $
}
