package metrics

import (
	"time"
)

// PodMetricsRecord represents a point-in-time measurement of pod resource usage
type PodMetricsRecord struct {
	Timestamp       time.Time
	CPU             float64 // CPU usage in cores
	Memory          float64 // Memory usage in bytes
	GPU             float64 // GPU utilization (0-1 range)
	PowerEstimate   float64 // Estimated power at this point (Watts)
	CarbonIntensity float64 // Carbon intensity at this point (gCO2eq/kWh)
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
}

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
}

// DownsamplingStrategy defines how to reduce the number of metrics points
// while preserving the overall shape of the time series
type DownsamplingStrategy interface {
	// Downsample reduces the number of data points while preserving trend
	Downsample(records []PodMetricsRecord, targetCount int) []PodMetricsRecord
}
