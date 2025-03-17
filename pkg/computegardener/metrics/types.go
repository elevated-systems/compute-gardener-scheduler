package metrics

import (
	"time"

	"k8s.io/klog/v2"
)

// PodMetricsRecord represents a point-in-time measurement of pod resource usage
type PodMetricsRecord struct {
	Timestamp       time.Time
	CPU             float64 // CPU usage in cores
	Memory          float64 // Memory usage in bytes
	GPUPowerWatts   float64 // GPU power in Watts
	PowerEstimate   float64 // Estimated power at this point across all hw in Watts
	CarbonIntensity float64 // Carbon intensity at this point in gCO2eq/kWh
	ElectricityRate float64 // Electricity rate at this point in $/kWh
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

// CalculateTotalEnergy calculates the total energy used by a pod in kWh
// using the trapezoid rule for numerical integration
func CalculateTotalEnergy(records []PodMetricsRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	totalEnergyKWh := 0.0

	// Integrate over the time series using trapezoid rule
	for i := 1; i < len(records); i++ {
		current := records[i]
		previous := records[i-1]

		// Time difference in hours
		deltaHours := current.Timestamp.Sub(previous.Timestamp).Hours()

		// Average power during this interval (W)
		avgPower := (current.PowerEstimate + previous.PowerEstimate) / 2

		// Energy used in this interval (kWh)
		intervalEnergy := (avgPower * deltaHours) / 1000

		// Add detailed logging at key intervals
		if i == 1 || i == len(records)-1 || i%(len(records)/10+1) == 0 {
			klog.V(1).InfoS("Energy calculation detail",
				"interval", i,
				"previousTime", previous.Timestamp,
				"currentTime", current.Timestamp,
				"deltaHours", deltaHours,
				"previousPower", previous.PowerEstimate,
				"currentPower", current.PowerEstimate,
				"avgPower", avgPower,
				"intervalEnergy", intervalEnergy,
				"runningTotal", totalEnergyKWh+intervalEnergy,
				"previousCPU", previous.CPU,
				"currentCPU", current.CPU,
				"previousGPUPower", previous.GPUPowerWatts,
				"currentGPUPower", current.GPUPowerWatts)
		}

		totalEnergyKWh += intervalEnergy
	}

	return totalEnergyKWh
}

// CalculateTotalCarbonEmissions calculates the total carbon emissions in gCO2eq
// using the trapezoid rule for numerical integration
func CalculateTotalCarbonEmissions(records []PodMetricsRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	totalCarbonEmissions := 0.0

	// Integrate over the time series using trapezoid rule
	for i := 1; i < len(records); i++ {
		current := records[i]
		previous := records[i-1]

		// Time difference in hours
		deltaHours := current.Timestamp.Sub(previous.Timestamp).Hours()

		// Average power during this interval (W)
		avgPower := (current.PowerEstimate + previous.PowerEstimate) / 2

		// Energy used in this interval (kWh)
		intervalEnergy := (avgPower * deltaHours) / 1000

		// Average carbon intensity during this interval (gCO2eq/kWh)
		avgCarbonIntensity := (current.CarbonIntensity + previous.CarbonIntensity) / 2

		// Carbon emissions for this interval (gCO2eq)
		intervalCarbon := intervalEnergy * avgCarbonIntensity

		totalCarbonEmissions += intervalCarbon
	}

	return totalCarbonEmissions
}
