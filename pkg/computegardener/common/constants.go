package common

// Constants for annotation keys used throughout the compute-gardener-scheduler project
const (
	// Base annotation prefix for all compute-gardener annotations
	AnnotationBase = "compute-gardener-scheduler.kubernetes.io"

	// ----------------------------------------
	// Pod annotations used by the scheduler
	// ----------------------------------------

	// Carbon-aware scheduling annotations
	AnnotationCarbonIntensityThreshold = AnnotationBase + "/carbon-intensity-threshold"
	AnnotationCarbonEnabled           = AnnotationBase + "/carbon-enabled"

	// Price-aware scheduling annotations
	AnnotationPriceThreshold = AnnotationBase + "/price-threshold"

	// Energy budget annotations
	AnnotationEnergyBudgetKWh       = AnnotationBase + "/energy-budget-kwh"
	AnnotationEnergyBudgetAction    = AnnotationBase + "/energy-budget-action"
	AnnotationEnergyBudgetExceeded  = AnnotationBase + "/energy-budget-exceeded"
	AnnotationEnergyUsageKWh        = AnnotationBase + "/energy-usage-kwh"
	AnnotationEnergyBudgetExceededBy = AnnotationBase + "/energy-budget-exceeded-by"

	// Hardware efficiency annotations
	AnnotationMaxPowerWatts   = AnnotationBase + "/max-power-watts"
	AnnotationMinEfficiency   = AnnotationBase + "/min-efficiency"
	AnnotationGPUWorkloadType = AnnotationBase + "/gpu-workload-type"

	// PUE annotations
	AnnotationPUE    = AnnotationBase + "/pue"
	AnnotationGPUPUE = AnnotationBase + "/gpu-pue"

	// General scheduling annotations
	AnnotationSkip             = AnnotationBase + "/skip"
	AnnotationMaxSchedulingDelay = AnnotationBase + "/max-scheduling-delay"

	// ----------------------------------------
	// Namespace policy annotations
	// ----------------------------------------
	
	// Policy prefix for namespace-level defaults
	AnnotationNamespacePolicyPrefix = AnnotationBase + "/policy-"
	
	// Workload type prefix for type-specific overrides
	AnnotationWorkloadTypePrefix = AnnotationBase + "/workload-"
	
	// Workload type label for explicit type marking
	LabelWorkloadType = AnnotationBase + "/workload-type"
	
	// ----------------------------------------
	// Node annotations
	// ----------------------------------------
	
	// Hardware annotations
	AnnotationCPUModel = AnnotationBase + "/cpu-model" 
	AnnotationGPUModel = AnnotationBase + "/gpu-model"
	AnnotationCPUBaseFrequency = AnnotationBase + "/cpu-base-frequency" // Base/nominal CPU frequency in GHz
	AnnotationCPUMinFrequency = AnnotationBase + "/cpu-min-frequency" // Minimum CPU frequency in GHz
	AnnotationCPUMaxFrequency = AnnotationBase + "/cpu-max-frequency" // Maximum CPU frequency in GHz
	AnnotationCPUDynamicFrequencyEnabled = AnnotationBase + "/cpu-dynamic-frequency-enabled" // Whether to dynamically check CPU frequency
)

// Energy budget actions
const (
	EnergyBudgetActionLog      = "log"
	EnergyBudgetActionAnnotate = "annotate"
	EnergyBudgetActionLabel    = "label"
	EnergyBudgetActionNotify   = "notify"
)

// Workload types
const (
	WorkloadTypeBatch    = "batch"
	WorkloadTypeService  = "service" 
	WorkloadTypeStateful = "stateful"
	WorkloadTypeSystem   = "system"
	WorkloadTypeGeneric  = "generic"
	
	// GPU workload types with typical power profiles
	GPUWorkloadTypeInference = "inference"
	GPUWorkloadTypeTraining  = "training"
	GPUWorkloadTypeRendering = "rendering"
	GPUWorkloadTypeGeneric   = "generic"
)

// Defaults for power and efficiency
const (
	DefaultPUE    = 1.1
	DefaultGPUPUE = 1.15
)

// Prometheus metric names for CPU frequency monitoring
const (
	MetricCPUFrequencyGHz    = "node_cpu_frequency_ghz"      // Current CPU frequency in GHz
	MetricCPUBaseFrequencyGHz = "node_cpu_base_frequency_ghz" // Base/nominal CPU frequency in GHz
	MetricCPUMinFrequencyGHz  = "node_cpu_min_frequency_ghz"  // Minimum CPU frequency in GHz
	MetricCPUMaxFrequencyGHz  = "node_cpu_max_frequency_ghz"  // Maximum CPU frequency in GHz
)