package dryrun

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	// Subsystem name for dry-run metrics
	dryrunSubsystem = "compute_gardener_dryrun"
)

var (
	// PodsEvaluatedTotal counts total pods evaluated by dry-run webhook
	PodsEvaluatedTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "pods_evaluated_total",
			Help:      "Total number of pods evaluated by dry-run mode",
		},
		[]string{"namespace"},
	)

	// PodsWouldDelayTotal counts pods that would have been delayed
	PodsWouldDelayTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "pods_would_delay_total",
			Help:      "Total number of pods that would have been delayed",
		},
		[]string{"namespace", "delay_type"},
	)

	// EstimatedCarbonSavingsTotal accumulates estimated carbon savings (gCO2eq)
	EstimatedCarbonSavingsTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "estimated_carbon_savings_gco2eq_total",
			Help:      "Total estimated carbon savings in grams CO2 equivalent",
		},
		[]string{"namespace"},
	)

	// EstimatedCostSavingsTotal accumulates estimated cost savings (USD)
	EstimatedCostSavingsTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "estimated_cost_savings_usd_total",
			Help:      "Total estimated cost savings in USD",
		},
		[]string{"namespace"},
	)

	// CurrentCarbonIntensity shows current carbon intensity at evaluation time
	CurrentCarbonIntensity = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem: dryrunSubsystem,
			Name:      "current_carbon_intensity_gco2eq_per_kwh",
			Help:      "Current carbon intensity at pod evaluation time (gCO2eq/kWh)",
		},
		[]string{"namespace"},
	)

	// CurrentElectricityPrice shows current electricity price at evaluation time
	CurrentElectricityPrice = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem: dryrunSubsystem,
			Name:      "current_electricity_price_usd_per_kwh",
			Help:      "Current electricity price at pod evaluation time (USD/kWh)",
		},
		[]string{"namespace"},
	)

	// ActualCarbonSavingsTotal accumulates actual carbon savings (using real runtime)
	ActualCarbonSavingsTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "actual_carbon_savings_gco2eq_total",
			Help:      "Total actual carbon savings using real pod runtime (gCO2eq)",
		},
		[]string{"namespace"},
	)

	// ActualCostSavingsTotal accumulates actual cost savings (using real runtime)
	ActualCostSavingsTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "actual_cost_savings_usd_total",
			Help:      "Total actual cost savings using real pod runtime (USD)",
		},
		[]string{"namespace"},
	)

	// PodsCompletedTotal counts pods that completed (for tracking completion rate)
	PodsCompletedTotal = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem: dryrunSubsystem,
			Name:      "pods_completed_total",
			Help:      "Total number of pods that completed",
		},
		[]string{"namespace"},
	)

	// PodRuntimeHours histogram of pod runtimes
	PodRuntimeHours = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem: dryrunSubsystem,
			Name:      "pod_runtime_hours",
			Help:      "Histogram of pod runtimes in hours",
			Buckets:   []float64{0.1, 0.5, 1, 2, 4, 8, 12, 24, 48, 72},
		},
		[]string{"namespace"},
	)

	// PodEnergyConsumptionKWh histogram of pod energy consumption
	PodEnergyConsumptionKWh = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem: dryrunSubsystem,
			Name:      "pod_energy_consumption_kwh",
			Help:      "Histogram of pod energy consumption in kWh",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 50, 100},
		},
		[]string{"namespace"},
	)
)

func init() {
	// Register all dry-run metrics with the legacy registry
	legacyregistry.MustRegister(PodsEvaluatedTotal)
	legacyregistry.MustRegister(PodsWouldDelayTotal)
	legacyregistry.MustRegister(EstimatedCarbonSavingsTotal)
	legacyregistry.MustRegister(EstimatedCostSavingsTotal)
	legacyregistry.MustRegister(CurrentCarbonIntensity)
	legacyregistry.MustRegister(CurrentElectricityPrice)
	legacyregistry.MustRegister(ActualCarbonSavingsTotal)
	legacyregistry.MustRegister(ActualCostSavingsTotal)
	legacyregistry.MustRegister(PodsCompletedTotal)
	legacyregistry.MustRegister(PodRuntimeHours)
	legacyregistry.MustRegister(PodEnergyConsumptionKWh)
}
