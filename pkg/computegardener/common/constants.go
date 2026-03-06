package common

// Constants for annotation keys used throughout the compute-gardener-scheduler project
const (
	// Base prefix for all compute-gardener annotations
	AnnotationBase = "compute-gardener-scheduler.kubernetes.io"

	// ----------------------------------------
	// Pod annotations used by the scheduler
	// ----------------------------------------

	// Carbon-aware scheduling annotations
	AnnotationCarbonIntensityThreshold = AnnotationBase + "/carbon-intensity-threshold"
	AnnotationCarbonEnabled            = AnnotationBase + "/carbon-enabled"

	// Price-aware scheduling annotations
	AnnotationPriceThreshold = AnnotationBase + "/price-threshold"

	// Energy budget annotations
	AnnotationEnergyBudgetKWh        = AnnotationBase + "/energy-budget-kwh"
	AnnotationEnergyBudgetAction     = AnnotationBase + "/energy-budget-action"
	AnnotationEnergyBudgetExceeded   = AnnotationBase + "/energy-budget-exceeded"
	AnnotationEnergyUsageKWh         = AnnotationBase + "/energy-usage-kwh"
	AnnotationEnergyBudgetExceededBy = AnnotationBase + "/energy-budget-exceeded-by"

	// Carbon and cost tracking annotations
	AnnotationInitialCarbonIntensity  = AnnotationBase + "/initial-carbon-intensity"
	AnnotationInitialElectricityRate  = AnnotationBase + "/initial-electricity-rate"
	AnnotationInitialTimestamp        = AnnotationBase + "/initial-timestamp"           // RFC3339 timestamp when pod was first delayed (DEPRECATED: use carbon or price specific)
	AnnotationCarbonDelayTimestamp    = AnnotationBase + "/carbon-delay-timestamp"      // RFC3339 timestamp when pod was first delayed by carbon
	AnnotationPriceDelayTimestamp     = AnnotationBase + "/price-delay-timestamp"       // RFC3339 timestamp when pod was first delayed by price
	AnnotationBindTimestamp           = AnnotationBase + "/bind-timestamp"              // RFC3339 timestamp when pod bound to node
	AnnotationBindCarbonIntensity     = AnnotationBase + "/bind-time-carbon-intensity"  // Carbon intensity when pod bound
	AnnotationBindElectricityRate     = AnnotationBase + "/bind-time-electricity-rate"  // Electricity rate when pod bound

	// Hardware efficiency annotations
	AnnotationMaxPowerWatts   = AnnotationBase + "/max-power-watts"
	AnnotationMinEfficiency   = AnnotationBase + "/min-efficiency"
	AnnotationGPUWorkloadType = AnnotationBase + "/gpu-workload-type"

	// PUE annotations
	AnnotationPUE    = AnnotationBase + "/pue"
	AnnotationGPUPUE = AnnotationBase + "/gpu-pue"

	// General scheduling annotations
	AnnotationSkip                   = AnnotationBase + "/skip"
	AnnotationMaxSchedulingDelay     = AnnotationBase + "/max-scheduling-delay"
	AnnotationEstimatedRuntimeHours  = AnnotationBase + "/estimated-runtime-hours" // Optional hint for savings estimation

	// Dry-run mode annotations
	AnnotationDryRunEvaluated               = AnnotationBase + "/dry-run-evaluated"
	AnnotationDryRunTimestamp               = AnnotationBase + "/dry-run-timestamp"
	AnnotationDryRunWouldDelay              = AnnotationBase + "/dry-run-would-delay"
	AnnotationDryRunDelayType               = AnnotationBase + "/dry-run-delay-type"
	AnnotationDryRunReason                  = AnnotationBase + "/dry-run-reason"
	AnnotationDryRunCarbonIntensity         = AnnotationBase + "/dry-run-carbon-intensity"
	AnnotationDryRunCarbonThreshold         = AnnotationBase + "/dry-run-carbon-threshold"
	AnnotationDryRunPrice                   = AnnotationBase + "/dry-run-price"
	AnnotationDryRunPriceThreshold          = AnnotationBase + "/dry-run-price-threshold"
	AnnotationDryRunEstimatedCarbonSavings  = AnnotationBase + "/dry-run-estimated-carbon-savings-gco2"
	AnnotationDryRunEstimatedCostSavings    = AnnotationBase + "/dry-run-estimated-cost-savings-usd"

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

	// Hardware annotations - CPU
	AnnotationCPUModel         = AnnotationBase + "/cpu-model"
	AnnotationCPUBaseFrequency = AnnotationBase + "/cpu-base-frequency" // Base/nominal CPU frequency in GHz
	AnnotationCPUMinFrequency  = AnnotationBase + "/cpu-min-frequency"  // Minimum CPU frequency in GHz
	AnnotationCPUMaxFrequency  = AnnotationBase + "/cpu-max-frequency"  // Maximum CPU frequency in GHz

	// Hardware annotations - GPU
	AnnotationGPUModel      = AnnotationBase + "/gpu-model"
	AnnotationGPUCount      = AnnotationBase + "/gpu-count"
	AnnotationGPUTotalPower = AnnotationBase + "/gpu-total-power"

	// Node Feature Discovery (NFD) labels for hardware information
	// Base prefix for all NFD labels
	NFDLabelBase = "feature.node.kubernetes.io"

	// CPU labels - from NFD discovery or our node exporter
	NFDLabelCPUModelFamily   = NFDLabelBase + "/cpu-model.family"
	NFDLabelCPUModelID       = NFDLabelBase + "/cpu-model.id"
	NFDLabelCPUModelVendorID = NFDLabelBase + "/cpu-model.vendor_id"
	NFDLabelCPUModel         = NFDLabelBase + "/cpu-model.name" // Used by our exporter when family/id/vendor_id are not present

	// CPU power state labels - from NFD discovery
	NFDLabelCPUPStateScalingGovernor = NFDLabelBase + "/cpu-pstate.scaling_governor" // e.g., "powersave", "performance"
	NFDLabelCPUPStateStatus          = NFDLabelBase + "/cpu-pstate.status"           // e.g., "active"
	NFDLabelCPUPStateTurbo           = NFDLabelBase + "/cpu-pstate.turbo"            // e.g., "true", "false"

	// Generic NFD labels for PCIe devices (may be used for non-NVIDIA GPUs)
	NFDLabelPCIVendorPrefix = NFDLabelBase + "/pci-" // Vendor-specific prefixes follow

	NvidiaLabelBase = "nvidia.com/gpu"

	// GPU labels - from standard NFD discovery or NVIDIA GPU operator
	NvidiaLabelGPUCount   = NvidiaLabelBase + ".count"
	NvidiaLabelGPUProduct = NFDLabelBase + ".product" // For NVIDIA GPU model

	// CPU frequency dynamic check enabled
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
	DefaultPUE           = 1.15
	DefaultGPUPUE        = 1.2
	DefaultPowerExponent = 1.4 // Power-law exponent for CPU power modeling
)

// Prometheus metric names for hardware monitoring
const (
	// CPU metrics from standard node-exporter
	MetricCPUFrequencyHertz    = "node_cpu_scaling_frequency_hertz" // Current dynamic CPU frequency in Hz
	MetricCPUFrequencyMinHertz = "node_cpu_frequency_min_hertz"     // Minimum CPU frequency in Hz (static limit)
	MetricCPUFrequencyMaxHertz = "node_cpu_frequency_max_hertz"     // Maximum CPU frequency in Hz (static limit)

	// GPU metrics
	MetricGPUCount             = "compute_gardener_gpu_count"                      // Number of GPUs on a node
	MetricGPUPower             = "compute_gardener_gpu_power_watts"                // Current GPU power consumption in watts
	MetricGPUMaxPower          = "compute_gardener_gpu_max_power_watts"            // Maximum GPU power limit in watts
	MetricGPUUtilization       = "compute_gardener_gpu_utilization_percent"        // GPU utilization percentage
	MetricGPUMemoryUtilization = "compute_gardener_gpu_memory_utilization_percent" // GPU memory utilization percentage

	// Temperature metrics from standard node-exporter
	MetricCPUTemperatureQuery = `node_hwmon_temp_celsius{chip=~"coretemp-.*", sensor=~"temp[0-9]+"}` // CPU core temperature query

	// DCGM GPU metrics
	DCGMMetricGPUPower       = "DCGM_FI_DEV_POWER_USAGE" // GPU power consumption in watts
	DCGMMetricGPUUtilization = "DCGM_FI_DEV_GPU_UTIL"    // GPU utilization percentage
	DCGMMetricGPUTempCore    = "DCGM_FI_DEV_GPU_TEMP"    // GPU core temperature in Celsius
	DCGMMetricGPUTempMemory  = "DCGM_FI_DEV_MEM_TEMP"    // GPU memory temperature in Celsius
)
