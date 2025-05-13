package types

import (
	"time"
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
	TotalPower float64 // Cumulative power in kWh
	TotalCO2   float64 // Cumulative CO2 in grams
	TotalCost  float64 // Cumulative cost in $
}
