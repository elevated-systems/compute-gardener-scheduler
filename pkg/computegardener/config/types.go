package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	
	"k8s.io/klog/v2"
)

// PowerConfig holds power consumption settings for nodes
type PowerConfig struct {
	DefaultIdlePower float64              `yaml:"defaultIdlePower"` // Default idle power in watts
	DefaultMaxPower  float64              `yaml:"defaultMaxPower"`  // Default max power in watts
	DefaultPUE       float64              `yaml:"defaultPUE"`       // Default Power Usage Effectiveness (typically 1.1-1.6)
	DefaultGPUPUE    float64              `yaml:"defaultGPUPUE"`    // Default GPU Power Usage Effectiveness (typically 1.15-1.25)
	NodePowerConfig  map[string]NodePower `yaml:"nodePowerConfig"`  // Per-node power settings
	HardwareProfiles *HardwareProfiles    `yaml:"hardwareProfiles,omitempty"` // Hardware profile registry
}

// HardwareProfiles contains mappings from hardware identifiers to power profiles
type HardwareProfiles struct {
	// Power profiles for hardware components
	CPUProfiles  map[string]PowerProfile `yaml:"cpuProfiles"`  // CPU model -> power profile
	GPUProfiles  map[string]PowerProfile `yaml:"gpuProfiles"`  // GPU model -> power profile
	MemProfiles  map[string]MemoryPowerProfile `yaml:"memProfiles"`  // Memory type -> power profile
	
	// Cloud instance mappings to hardware components
	CloudInstanceMapping map[string]map[string]HardwareComponents `yaml:"cloudInstanceMapping"` // Provider -> instance type -> hardware components
}

// PowerProfile defines power characteristics for a hardware component
type PowerProfile struct {
	IdlePower         float64 `yaml:"idlePower"`                   // Idle power in watts
	MaxPower          float64 `yaml:"maxPower"`                    // Max power in watts
	BaseFrequencyGHz  float64 `yaml:"baseFrequencyGHz,omitempty"` // Base/nominal CPU frequency in GHz
	PowerScaling      string  `yaml:"powerScaling,omitempty"`      // Power scaling model: "linear", "quadratic", etc.
	FrequencyRangeGHz struct {
		Min float64 `yaml:"min,omitempty"` // Minimum operating frequency in GHz
		Max float64 `yaml:"max,omitempty"` // Maximum operating frequency (turbo) in GHz
	} `yaml:"frequencyRangeGHz,omitempty"` // Frequency operating range if applicable
}

// MemoryPowerProfile defines power characteristics for memory components
type MemoryPowerProfile struct {
	IdlePowerPerGB float64 `yaml:"idlePowerPerGB"` // Idle power in watts per GB
	MaxPowerPerGB  float64 `yaml:"maxPowerPerGB"`  // Max power in watts per GB
	BaseIdlePower  float64 `yaml:"baseIdlePower,omitempty"` // Base idle power for memory controller
}

// HardwareComponents defines the hardware composition of a node or instance
type HardwareComponents struct {
	CPUModel         string  `yaml:"cpuModel"`                    // CPU model identifier
	GPUModel         string  `yaml:"gpuModel,omitempty"`          // GPU model identifier, if present
	MemoryType       string  `yaml:"memoryType,omitempty"`        // Memory type identifier
	NumCPUs          int     `yaml:"numCPUs,omitempty"`           // Number of CPU cores/threads
	NumGPUs          int     `yaml:"numGPUs,omitempty"`           // Number of GPUs, if present
	TotalMemory      int     `yaml:"totalMemory,omitempty"`       // Total memory in MB
	MemoryChannels   int     `yaml:"memChannels,omitempty"`       // Number of memory channels
	CurrentFreqGHz   float64 `yaml:"currentFreqGHz,omitempty"`    // Current CPU frequency in GHz
	CPU_TDP_Watts    float64 `yaml:"cpuTdpWatts,omitempty"`       // TDP rating of CPU in watts
}

// MetricsConfig holds configuration for metrics collection and storage
type MetricsConfig struct {
	SamplingInterval     string           `yaml:"samplingInterval"`     // e.g. "30s" or "1m"
	MaxSamplesPerPod     int              `yaml:"maxSamplesPerPod"`     // e.g. 1000
	PodRetention         string           `yaml:"podRetention"`         // e.g. "1h"
	DownsamplingStrategy string           `yaml:"downsamplingStrategy"` // "lttb", "timeBased", "minMax"
	Prometheus          *PrometheusConfig `yaml:"prometheus,omitempty"` // Prometheus configuration
}

// PrometheusConfig holds configuration for Prometheus metrics integration
type PrometheusConfig struct {
	URL             string `yaml:"url"`             // Prometheus server URL, e.g. "http://prometheus.monitoring:9090"
	QueryTimeout    string `yaml:"queryTimeout"`    // Prometheus query timeout, e.g. "30s"
	CompletionDelay string `yaml:"completionDelay"` // Delay after pod completion before collecting final metrics, e.g. "30s"
	
	// DCGM exporter integration
	UseDCGM         bool   `yaml:"useDCGM"`         // Whether to use DCGM exporter metrics (default: true)
	DCGMPowerMetric string `yaml:"dcgmPowerMetric"` // DCGM power metric name (default: DCGM_FI_DEV_POWER_USAGE)
	DCGMUtilMetric  string `yaml:"dcgmUtilMetric"`  // DCGM utilization metric name (default: DCGM_FI_DEV_GPU_UTIL)
}

// NodePower holds power settings for a specific node
type NodePower struct {
	IdlePower float64 `yaml:"idlePower"` // Idle power in watts
	MaxPower  float64 `yaml:"maxPower"`  // Max power in watts
	// Optional GPU-specific power settings
	IdleGPUPower float64 `yaml:"idleGPUPower,omitempty"` // Idle GPU power in watts
	MaxGPUPower  float64 `yaml:"maxGPUPower,omitempty"`  // Max GPU power in watts
	// Power Usage Effectiveness factors
	PUE         float64 `yaml:"pue,omitempty"`          // Power Usage Effectiveness (default: 1.1)
	GPUPUE      float64 `yaml:"gpuPue,omitempty"`       // GPU-specific Power Usage Effectiveness (default: 1.15)
	// Workload type classification hints
	GPUWorkloadTypes map[string]float64 `yaml:"gpuWorkloadTypes,omitempty"` // Workload type (e.g. "inference", "training") to power coefficient mappings
}

// Config holds all configuration for the compute-gardener scheduler
type Config struct {
	Cache      APICacheConfig   `yaml:"cache"`
	Scheduling SchedulingConfig `yaml:"scheduling"`
	Carbon     CarbonConfig     `yaml:"carbon"`
	Pricing    PricingConfig    `yaml:"pricing"`
	Power      PowerConfig      `yaml:"power"`
	Metrics    MetricsConfig    `yaml:"metrics"`
}

// APICacheConfig holds configuration for API caching behavior
type APICacheConfig struct {
	Timeout     time.Duration `yaml:"timeout"`
	MaxRetries  int           `yaml:"maxRetries"`
	RetryDelay  time.Duration `yaml:"retryDelay"`
	RateLimit   int           `yaml:"rateLimit"`
	CacheTTL    time.Duration `yaml:"cacheTTL"`
	MaxCacheAge time.Duration `yaml:"maxCacheAge"`
}

// ElectricityMapsAPIConfig holds configuration specific to the Electricity Maps API
type ElectricityMapsAPIConfig struct {
	APIKey string `yaml:"apiKey"`
	URL    string `yaml:"url"`
	Region string `yaml:"region"`
}

// SchedulingConfig holds configuration for scheduling behavior
type SchedulingConfig struct {
	MaxSchedulingDelay  time.Duration `yaml:"maxSchedulingDelay"`
	EnablePodPriorities bool          `yaml:"enablePodPriorities"`
}

// Schedule defines a time range with its peak and off-peak rates
type Schedule struct {
	DayOfWeek   string  `yaml:"dayOfWeek"`
	StartTime   string  `yaml:"startTime"`
	EndTime     string  `yaml:"endTime"`
	PeakRate    float64 `yaml:"peakRate"`    // Rate in $/kWh during this time period
	OffPeakRate float64 `yaml:"offPeakRate"` // Rate in $/kWh outside this time period
}

// CarbonConfig holds configuration for carbon-aware scheduling
type CarbonConfig struct {
	Enabled            bool                     `yaml:"enabled"`
	Provider           string                   `yaml:"provider"` // e.g. "electricity-maps-api"
	IntensityThreshold float64                  `yaml:"carbonIntensityThreshold"`
	APIConfig          ElectricityMapsAPIConfig `yaml:"api"`
}

// PricingConfig holds configuration for price-aware scheduling
type PricingConfig struct {
	Enabled   bool       `yaml:"enabled"`
	Provider  string     `yaml:"provider"`  // e.g. "tou" for time-of-use pricing
	Schedules []Schedule `yaml:"schedules"` // Time-based pricing periods with their rates
}

// Validate performs validation of the configuration
func (c *Config) Validate() error {
	if c.Carbon.Enabled {
		if c.Carbon.Provider == "electricity-maps-api" && c.Carbon.APIConfig.APIKey == "" {
			return fmt.Errorf("Electricity Maps API key is required when provider is electricity-maps-api")
		}
		if c.Carbon.IntensityThreshold <= 0 {
			return fmt.Errorf("base carbon intensity threshold must be positive")
		}
	}

	if c.Pricing.Enabled {
		if err := c.validatePricing(); err != nil {
			return fmt.Errorf("invalid pricing config: %v", err)
		}
	}

	// Validate power settings
	if c.Power.DefaultIdlePower <= 0 {
		return fmt.Errorf("default idle power must be positive")
	}
	if c.Power.DefaultMaxPower <= c.Power.DefaultIdlePower {
		return fmt.Errorf("default max power must be greater than idle power")
	}
	// Validate PUE if provided, or set default
	if c.Power.DefaultPUE == 0 {
		// Set default PUE if not specified
		c.Power.DefaultPUE = 1.1 // Typical efficient datacenter
	} else if c.Power.DefaultPUE < 1.0 {
		return fmt.Errorf("default PUE must be at least 1.0 (100%% efficiency)")
	} else if c.Power.DefaultPUE > 2.0 {
		// Warn but don't error if PUE seems unusually high
		klog.V(2).InfoS("Unusually high default PUE specified", "pue", c.Power.DefaultPUE)
	}
	
	// Validate GPU PUE if provided, or set default
	if c.Power.DefaultGPUPUE == 0 {
		// Set default GPU PUE if not specified  
		c.Power.DefaultGPUPUE = 1.15 // Accounts for power conversion losses and auxiliary components
	} else if c.Power.DefaultGPUPUE < 1.0 {
		return fmt.Errorf("default GPU PUE must be at least 1.0 (100%% efficiency)")
	} else if c.Power.DefaultGPUPUE > 1.5 {
		// Warn but don't error if GPU PUE seems unusually high
		klog.V(2).InfoS("Unusually high default GPU PUE specified", "gpuPue", c.Power.DefaultGPUPUE)
	}
	for node, power := range c.Power.NodePowerConfig {
		if power.IdlePower <= 0 {
			return fmt.Errorf("idle power for node %s must be positive", node)
		}
		if power.MaxPower <= power.IdlePower {
			return fmt.Errorf("max power must be greater than idle power for node %s", node)
		}
		
		// Validate GPU power settings if specified
		if power.IdleGPUPower > 0 && power.MaxGPUPower <= power.IdleGPUPower {
			return fmt.Errorf("max GPU power must be greater than idle GPU power for node %s", node)
		}
	}
	
	// Validate hardware profiles if configured
	if c.Power.HardwareProfiles != nil {
		if err := c.validateHardwareProfiles(); err != nil {
			return fmt.Errorf("invalid hardware profiles: %v", err)
		}
	}
	
	// Validate metrics settings
	if c.Metrics.SamplingInterval != "" {
		if _, err := time.ParseDuration(c.Metrics.SamplingInterval); err != nil {
			return fmt.Errorf("invalid metrics sampling interval: %v", err)
		}
	}
	
	if c.Metrics.PodRetention != "" {
		if _, err := time.ParseDuration(c.Metrics.PodRetention); err != nil {
			return fmt.Errorf("invalid completed pod retention duration: %v", err)
		}
	}
	
	// Validate downsampling strategy
	if c.Metrics.DownsamplingStrategy != "" && 
	   c.Metrics.DownsamplingStrategy != "lttb" && 
	   c.Metrics.DownsamplingStrategy != "timeBased" && 
	   c.Metrics.DownsamplingStrategy != "minMax" {
		return fmt.Errorf("invalid downsampling strategy: %s (must be one of: lttb, timeBased, minMax)", 
			c.Metrics.DownsamplingStrategy)
	}
	
	// Validate Prometheus config if provided
	if c.Metrics.Prometheus != nil {
		if c.Metrics.Prometheus.URL == "" {
			return fmt.Errorf("Prometheus URL must be provided if Prometheus config is enabled")
		}
		
		// Validate query timeout if provided
		if c.Metrics.Prometheus.QueryTimeout != "" {
			if _, err := time.ParseDuration(c.Metrics.Prometheus.QueryTimeout); err != nil {
				return fmt.Errorf("invalid Prometheus query timeout: %v", err)
			}
		}
		
		// Validate completion delay if provided
		if c.Metrics.Prometheus.CompletionDelay != "" {
			if _, err := time.ParseDuration(c.Metrics.Prometheus.CompletionDelay); err != nil {
				return fmt.Errorf("invalid pod completion delay: %v", err)
			}
		}
	}

	return nil
}

func (c *Config) validatePricing() error {
	for i, schedule := range c.Pricing.Schedules {
		if err := validateSchedule(schedule); err != nil {
			return fmt.Errorf("invalid schedule at index %d: %v", i, err)
		}
		
		// Only validate rates if they are provided (non-zero)
		if schedule.PeakRate != 0 || schedule.OffPeakRate != 0 {
			// If one rate is provided, both should be
			if schedule.PeakRate <= 0 {
				return fmt.Errorf("peak rate must be positive in schedule at index %d when rates are provided", i)
			}
			if schedule.OffPeakRate <= 0 {
				return fmt.Errorf("off-peak rate must be positive in schedule at index %d when rates are provided", i)
			}
			if schedule.PeakRate <= schedule.OffPeakRate {
				return fmt.Errorf("peak rate must be greater than off-peak rate in schedule at index %d", i)
			}
		}
	}
	return nil
}

func validateSchedule(schedule Schedule) error {
	// Validate day of week format
	parts := strings.Split(schedule.DayOfWeek, ",")
	for _, part := range parts {
		if strings.Contains(part, "-") {
			// Handle range format (e.g., "1-5")
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return fmt.Errorf("invalid day of week range format: %s", part)
			}
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil || start < 0 || start > 6 {
				return fmt.Errorf("invalid start day in range: %s (must be 0-6)", rangeParts[0])
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil || end < 0 || end > 6 {
				return fmt.Errorf("invalid end day in range: %s (must be 0-6)", rangeParts[1])
			}
			if start > end {
				return fmt.Errorf("invalid range: start day %d is greater than end day %d", start, end)
			}
		} else {
			// Handle single day format (e.g., "0")
			day, err := strconv.Atoi(part)
			if err != nil || day < 0 || day > 6 {
				return fmt.Errorf("invalid day of week: %s (must be 0-6)", part)
			}
		}
	}

	// Validate time format
	for _, t := range []string{schedule.StartTime, schedule.EndTime} {
		if _, err := time.Parse("15:04", t); err != nil {
			return fmt.Errorf("invalid time format: %s (must be HH:MM in 24h format)", t)
		}
	}

	return nil
}

// validateHardwareProfiles validates hardware profiles configuration
func (c *Config) validateHardwareProfiles() error {
	profiles := c.Power.HardwareProfiles
	
	// Validate CPU profiles
	if len(profiles.CPUProfiles) == 0 {
		return fmt.Errorf("no CPU profiles defined")
	}
	
	for model, profile := range profiles.CPUProfiles {
		if profile.IdlePower <= 0 {
			return fmt.Errorf("idle power for CPU %s must be positive", model)
		}
		if profile.MaxPower <= profile.IdlePower {
			return fmt.Errorf("max power must be greater than idle power for CPU %s", model)
		}
	}
	
	// Validate GPU profiles if present
	for model, profile := range profiles.GPUProfiles {
		if profile.IdlePower <= 0 {
			return fmt.Errorf("idle power for GPU %s must be positive", model)
		}
		if profile.MaxPower <= profile.IdlePower {
			return fmt.Errorf("max power must be greater than idle power for GPU %s", model)
		}
	}
	
	// Validate memory profiles if present
	for memType, profile := range profiles.MemProfiles {
		if profile.IdlePowerPerGB < 0 {
			return fmt.Errorf("idle power per GB for memory %s cannot be negative", memType)
		}
		if profile.MaxPowerPerGB <= 0 {
			return fmt.Errorf("max power per GB for memory %s must be positive", memType)
		}
		if profile.MaxPowerPerGB <= profile.IdlePowerPerGB {
			return fmt.Errorf("max power per GB must be greater than idle power per GB for memory %s", memType)
		}
	}
	
	// Validate cloud instance mappings
	for provider, instances := range profiles.CloudInstanceMapping {
		for instanceType, components := range instances {
			// Check for required CPU model
			if components.CPUModel == "" {
				return fmt.Errorf("missing CPU model for %s instance %s", provider, instanceType)
			}
			
			// Check CPU model exists in profiles
			if _, exists := profiles.CPUProfiles[components.CPUModel]; !exists {
				return fmt.Errorf("CPU model %s for %s instance %s not found in CPU profiles", 
					components.CPUModel, provider, instanceType)
			}
			
			// Check GPU model exists in profiles if specified
			if components.GPUModel != "" {
				if _, exists := profiles.GPUProfiles[components.GPUModel]; !exists {
					return fmt.Errorf("GPU model %s for %s instance %s not found in GPU profiles", 
						components.GPUModel, provider, instanceType)
				}
			}
			
			// Check memory type exists in profiles if specified
			if components.MemoryType != "" {
				if _, exists := profiles.MemProfiles[components.MemoryType]; !exists {
					return fmt.Errorf("memory type %s for %s instance %s not found in memory profiles", 
						components.MemoryType, provider, instanceType)
				}
			}
		}
	}
	
	return nil
}
