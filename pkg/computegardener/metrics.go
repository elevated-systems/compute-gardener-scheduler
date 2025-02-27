package computegardener

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	// Subsystem name used for scheduler metrics
	schedulerSubsystem = "scheduler_carbon_aware"
)

var (
	// CarbonIntensityGauge measures the current carbon intensity for a region
	CarbonIntensityGauge = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "carbon_intensity",
			Help:           "Current carbon intensity (gCO2eq/kWh) for a given region",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"region"},
	)

	// PodSchedulingLatency measures the latency of pod scheduling attempts
	PodSchedulingLatency = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "pod_scheduling_duration_seconds",
			Help:           "Latency for scheduling attempts in the compute-gardener scheduler",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"result"}, // "total", "api_success", "api_error"
	)

	// SchedulingAttempts counts the total number of scheduling attempts
	SchedulingAttempts = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "scheduling_attempt_total",
			Help:           "Number of attempts to schedule pods by result",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"result"}, // "success", "error", "skipped", "max_delay_exceeded", "invalid_threshold", "intensity_exceeded"
	)

	// NodeCPUUsage tracks CPU usage on nodes at job start and completion
	NodeCPUUsage = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_cpu_usage_cores",
			Help:           "CPU usage in cores on nodes at baseline (bind) and final (completion)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "final"
	)

	// NodePowerEstimate estimates node power consumption based on CPU usage
	NodePowerEstimate = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_power_estimate_watts",
			Help:           "Estimated power consumption in watts based on node CPU usage",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "final"
	)

	// JobEnergyUsage tracks estimated energy usage for jobs
	JobEnergyUsage = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_energy_usage_kwh",
			Help:           "Estimated energy usage in kWh for completed jobs",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// SchedulingEfficiencyMetrics tracks carbon/cost improvements
	SchedulingEfficiencyMetrics = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "scheduling_efficiency",
			Help:           "Scheduling efficiency metrics comparing initial vs actual scheduling time",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"metric", "pod"}, // metric: "carbon_intensity_delta", "electricity_rate_delta"
	)

	// EstimatedSavings tracks carbon and cost savings
	EstimatedSavings = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "estimated_savings",
			Help:           "Estimated savings from compute-gardener scheduling",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"type", "unit"}, // type: "carbon", "cost", unit: "grams_co2", "kwh", "dollars"
	)

	// ElectricityRateGauge measures the current electricity rate
	ElectricityRateGauge = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "electricity_rate",
			Help:           "Current electricity rate ($/kWh) for a given location",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"location", "period"}, // period can be "peak" or "off-peak"
	)

	// PriceBasedDelays counts scheduling delays due to price thresholds
	PriceBasedDelays = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "price_delay_total",
			Help:           "Number of scheduling delays due to electricity price thresholds",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"period"}, // "peak" or "off-peak"
	)

	// JobCarbonEmissions tracks estimated carbon emissions for jobs
	JobCarbonEmissions = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_carbon_emissions_grams",
			Help:           "Estimated carbon emissions in gCO2eq for completed jobs",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)
)

func init() {
	// Register all metrics with the legacy registry
	legacyregistry.MustRegister(CarbonIntensityGauge)
	legacyregistry.MustRegister(PodSchedulingLatency)
	legacyregistry.MustRegister(SchedulingAttempts)
	legacyregistry.MustRegister(NodeCPUUsage)
	legacyregistry.MustRegister(NodePowerEstimate)
	legacyregistry.MustRegister(JobEnergyUsage)
	legacyregistry.MustRegister(SchedulingEfficiencyMetrics)
	legacyregistry.MustRegister(EstimatedSavings)
	legacyregistry.MustRegister(ElectricityRateGauge)
	legacyregistry.MustRegister(PriceBasedDelays)
	legacyregistry.MustRegister(JobCarbonEmissions)
}
