package metrics

import (
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics/clients"
	"k8s.io/klog/v2"
)

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

// CalculateAveragePower calculates the average power consumption for a pod in watts
func CalculateAveragePower(records []PodMetricsRecord) float64 {
	if len(records) == 0 {
		return 0
	}

	totalPower := 0.0
	for _, record := range records {
		totalPower += record.PowerEstimate
	}

	return totalPower / float64(len(records))
}

// CalculatePeakPower finds the maximum power consumption for a pod in watts
func CalculatePeakPower(records []PodMetricsRecord) float64 {
	if len(records) == 0 {
		return 0
	}

	maxPower := records[0].PowerEstimate
	for _, record := range records {
		if record.PowerEstimate > maxPower {
			maxPower = record.PowerEstimate
		}
	}

	return maxPower
}

// CalculateGPUEnergy calculates the total GPU energy used by a pod in kWh
func CalculateGPUEnergy(records []PodMetricsRecord) float64 {
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

		// Average GPU power during this interval (W)
		avgPower := (current.GPUPowerWatts + previous.GPUPowerWatts) / 2

		// Energy used in this interval (kWh)
		intervalEnergy := (avgPower * deltaHours) / 1000

		totalEnergyKWh += intervalEnergy
	}

	return totalEnergyKWh
}

// CalculateAverageGPUPower calculates the average GPU power consumption for a pod in watts
func CalculateAverageGPUPower(records []PodMetricsRecord) float64 {
	if len(records) == 0 {
		return 0
	}

	totalGPUPower := 0.0
	gpuRecordCount := 0

	for _, record := range records {
		if record.GPUPowerWatts > 0 {
			totalGPUPower += record.GPUPowerWatts
			gpuRecordCount++
		}
	}

	if gpuRecordCount == 0 {
		return 0
	}

	return totalGPUPower / float64(gpuRecordCount)
}

// CalculateTotalCarbonEmissions calculates the total carbon emissions in gCO2eq
// using the trapezoid rule for numerical integration.
//
// IMPORTANT: This function uses time-series carbon intensity data collected throughout
// the pod's execution, NOT a fixed intensity value. Each PodMetricsRecord contains the
// carbon intensity (gCO2/kWh) at that moment, which may vary significantly during long-running
// jobs. This provides an accurate estimate of actual carbon consumed over time.
//
// For scheduler savings calculations (measuring the benefit of delaying), see
// processPodCompletionMetrics in pod_completion.go, which uses initial vs bind-time intensity.
func CalculateTotalCarbonEmissions(records []PodMetricsRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	totalCarbonEmissions := 0.0

	// Integrate over the time series using trapezoid rule
	// This accounts for varying carbon intensity throughout execution
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
		// Using the actual intensity values from Prometheus/carbon API at each timestamp
		avgCarbonIntensity := (current.CarbonIntensity + previous.CarbonIntensity) / 2

		// Carbon emissions for this interval (gCO2eq)
		intervalCarbon := intervalEnergy * avgCarbonIntensity

		totalCarbonEmissions += intervalCarbon
	}

	return totalCarbonEmissions
}

// ConvertGPUHistoryToStandardFormat converts GPU metrics history to standard PodMetricsRecord format
// that can be used with the common calculation utilities
func ConvertGPUHistoryToStandardFormat(h *clients.PodGPUMetricsHistory) []PodMetricsRecord {
	if len(h.Timestamps) == 0 {
		return nil
	}

	records := make([]PodMetricsRecord, len(h.Timestamps))

	for i := range h.Timestamps {
		records[i] = PodMetricsRecord{
			Timestamp:     h.Timestamps[i],
			GPUPowerWatts: h.Power[i], // GPU power in watts
			PowerEstimate: h.Power[i], // GPU power in watts
			// CPU and Memory will be 0 as this is GPU-specific
		}
	}

	return records
}
