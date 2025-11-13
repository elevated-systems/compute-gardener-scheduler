package eval

import (
	"time"

	v1 "k8s.io/api/core/v1"
)

// EvaluationResult contains the outcome of scheduling constraint checks
type EvaluationResult struct {
	// Whether the pod should be delayed
	ShouldDelay bool

	// Type of delay: "carbon", "price", "both", or "none" (machine-readable enum)
	DelayType string

	// Human-readable explanation with actual values (for logging/annotations)
	ReasonDescription string

	// Current carbon intensity at evaluation time
	CurrentCarbon float64

	// Carbon threshold that was checked
	CarbonThreshold float64

	// Current electricity price at evaluation time
	CurrentPrice float64

	// Price threshold that was checked
	PriceThreshold float64

	// Estimated pod power consumption in watts
	EstimatedPowerW float64

	// Estimated runtime in hours (if available)
	EstimatedRuntimeHours float64

	// Conservative savings estimates (current - threshold)
	EstimatedCarbonSavingsGCO2 float64
	EstimatedCostSavingsUSD    float64
}

// PodStartData stores information about a pod when it starts running
// Used by the dry-run completion controller to calculate actual savings
type PodStartData struct {
	// Pod identification
	Namespace string
	Name      string
	UID       string

	// Timing
	StartTime time.Time

	// Initial evaluation data (from webhook)
	InitialCarbon       float64
	InitialPrice        float64
	CarbonThreshold     float64
	PriceThreshold      float64
	WouldHaveDelayed    bool
	DelayType           string
	EstimatedPowerW     float64
	EstimatedRuntimeH   float64
}

// EstimatedSavings contains the calculated savings using actual pod runtime
// Note: These are ESTIMATES based on conservative assumptions:
// - We assume pod would have run at threshold intensity/price (not current)
// - We use actual runtime but estimated power consumption
type EstimatedSavings struct {
	// Actual energy consumed in kWh (based on estimated power × actual runtime)
	EnergyKWh float64

	// Estimated carbon savings in gCO2
	CarbonGCO2 float64

	// Estimated cost savings in USD
	CostUSD float64

	// Actual runtime in hours
	RuntimeHours float64
}

// PodResourceEstimate contains estimated resource usage for a pod
type PodResourceEstimate struct {
	CPUCores    float64
	MemoryGB    float64
	GPUCount    float64
	PowerWatts  float64
	RuntimeHours float64
}
