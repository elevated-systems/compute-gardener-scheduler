package metrics

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	// Subsystem name used for scheduler metrics
	schedulerSubsystem = "compute_gardener_scheduler"
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
		[]string{"region", "data_status"},
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
			Help:           "CPU usage in cores on nodes at baseline (bind) and current",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "current", "final"
	)

	// NodeMemoryUsage tracks memory usage on nodes
	NodeMemoryUsage = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_memory_usage_bytes",
			Help:           "Memory usage in bytes on nodes",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "current", "final"
	)

	// NodeGPUPower tracks GPU power usage on nodes
	NodeGPUPower = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_gpu_power_watts",
			Help:           "GPU power consumption in watts on nodes",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "current", "final"
	)

	// NodePowerEstimate estimates node power consumption based on CPU usage
	NodePowerEstimate = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_power_estimate_watts",
			Help:           "Estimated power consumption in watts based on node resource usage",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node", "pod", "phase"}, // phase: "baseline", "current", "final"
	)

	// MetricsSamplesStored tracks the number of time-series samples stored per pod
	MetricsSamplesStored = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "metrics_samples_stored",
			Help:           "Number of pod metrics samples currently stored in cache",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// MetricsCacheSize tracks the total number of pods being monitored
	MetricsCacheSize = metrics.NewGauge(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "metrics_cache_size",
			Help:           "Number of pods being tracked in metrics cache",
			StabilityLevel: metrics.ALPHA,
		},
	)

	// JobEnergyUsage tracks estimated energy usage for jobs
	JobEnergyUsage = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_energy_usage_kwh",
			Help:           "Estimated energy usage in kWh for completed jobs",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// JobGPUEnergyUsage tracks estimated GPU energy usage for jobs
	JobGPUEnergyUsage = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_gpu_energy_usage_kwh",
			Help:           "Estimated GPU energy usage in kWh for completed jobs",
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

	// EstimatedSavings tracks carbon and cost savings for completed pods (can be negative)
	// The 'method' label indicates calculation methodology:
	//   - "timeseries": High-precision counterfactual using historical Prometheus data
	//   - "simple": Rough estimate using (initial - bind) × energy when Prometheus unavailable
	EstimatedSavings = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "estimated_savings",
			Help:           "Estimated savings from compute-gardener scheduling per completed pod (grams_co2 or dollars)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"type", "unit", "method", "pod", "namespace"}, // method: "timeseries", "simple"
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

	// CarbonBasedDelays counts scheduling delays due to carbon intensity thresholds
	CarbonBasedDelays = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "carbon_delay_total",
			Help:           "Number of scheduling delays due to carbon intensity thresholds",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"region"}, // region where carbon intensity was measured
	)

	// JobCarbonEmissions tracks estimated carbon emissions for jobs
	JobCarbonEmissions = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_carbon_emissions_grams",
			Help:           "Estimated carbon emissions in gCO2eq for completed jobs (actual emissions during execution)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// JobCounterfactualCarbonEmissions tracks what emissions would have been without carbon-aware scheduling
	JobCounterfactualCarbonEmissions = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "job_counterfactual_carbon_emissions_grams",
			Help:           "Estimated carbon emissions in gCO2eq if job had run during initial delay period (counterfactual scenario)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// NodePUE tracks PUE (Power Usage Effectiveness) values for nodes
	NodePUE = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_pue",
			Help:           "Power Usage Effectiveness for nodes (ratio of total facility energy to IT equipment energy)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node"},
	)

	// PowerFilteredNodes counts nodes filtered due to power efficiency reasons
	PowerFilteredNodes = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "power_filtered_nodes_total",
			Help:           "Number of nodes filtered due to power or efficiency constraints",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"reason"}, // "max_power", "efficiency", "energy_budget"
	)

	// NodeEfficiency tracks calculated efficiency metrics for nodes
	NodeEfficiency = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "node_efficiency",
			Help:           "Efficiency metric for nodes (higher is better)",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"node"},
	)

	// EnergyBudgetTracking tracks energy budget usage for workloads
	EnergyBudgetTracking = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "energy_budget_usage_percent",
			Help:           "Percentage of energy budget used by workloads",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"pod", "namespace"},
	)

	// EnergyBudgetExceeded counts workloads that exceeded their energy budget
	EnergyBudgetExceeded = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "energy_budget_exceeded_total",
			Help:           "Number of workloads that exceeded their energy budget",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"namespace", "owner_kind", "action"},
	)
)

func init() {
	// Register all metrics with the legacy registry
	legacyregistry.MustRegister(CarbonIntensityGauge)
	legacyregistry.MustRegister(PodSchedulingLatency)
	legacyregistry.MustRegister(SchedulingAttempts)
	legacyregistry.MustRegister(NodeCPUUsage)
	legacyregistry.MustRegister(NodeMemoryUsage)
	legacyregistry.MustRegister(NodeGPUPower)
	legacyregistry.MustRegister(NodePowerEstimate)
	legacyregistry.MustRegister(MetricsSamplesStored)
	legacyregistry.MustRegister(MetricsCacheSize)
	legacyregistry.MustRegister(JobEnergyUsage)
	legacyregistry.MustRegister(JobGPUEnergyUsage)
	legacyregistry.MustRegister(SchedulingEfficiencyMetrics)
	legacyregistry.MustRegister(EstimatedSavings)
	legacyregistry.MustRegister(ElectricityRateGauge)
	legacyregistry.MustRegister(PriceBasedDelays)
	legacyregistry.MustRegister(CarbonBasedDelays)
	legacyregistry.MustRegister(JobCarbonEmissions)
	legacyregistry.MustRegister(JobCounterfactualCarbonEmissions)
	legacyregistry.MustRegister(NodePUE)
	legacyregistry.MustRegister(PowerFilteredNodes)
	legacyregistry.MustRegister(NodeEfficiency)
	legacyregistry.MustRegister(EnergyBudgetTracking)
	legacyregistry.MustRegister(EnergyBudgetExceeded)
}
