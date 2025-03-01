package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PowerConfig holds power consumption settings for nodes
type PowerConfig struct {
	DefaultIdlePower float64              `yaml:"defaultIdlePower"` // Default idle power in watts
	DefaultMaxPower  float64              `yaml:"defaultMaxPower"`  // Default max power in watts
	NodePowerConfig  map[string]NodePower `yaml:"nodePowerConfig"`  // Per-node power settings
}

// MetricsConfig holds configuration for metrics collection and storage
type MetricsConfig struct {
	SamplingInterval     string `yaml:"samplingInterval"`     // e.g. "30s" or "1m"
	MaxSamplesPerPod     int    `yaml:"maxSamplesPerPod"`     // e.g. 1000
	PodRetention         string `yaml:"podRetention"`         // e.g. "1h"
	DownsamplingStrategy string `yaml:"downsamplingStrategy"` // "lttb", "timeBased", "minMax"
}

// NodePower holds power settings for a specific node
type NodePower struct {
	IdlePower float64 `yaml:"idlePower"` // Idle power in watts
	MaxPower  float64 `yaml:"maxPower"`  // Max power in watts
	// Optional GPU-specific power settings
	IdleGPUPower float64 `yaml:"idleGPUPower,omitempty"` // Idle GPU power in watts
	MaxGPUPower  float64 `yaml:"maxGPUPower,omitempty"`  // Max GPU power in watts
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

	return nil
}

func (c *Config) validatePricing() error {
	for i, schedule := range c.Pricing.Schedules {
		if err := validateSchedule(schedule); err != nil {
			return fmt.Errorf("invalid schedule at index %d: %v", i, err)
		}
		if schedule.PeakRate <= 0 {
			return fmt.Errorf("peak rate must be positive in schedule at index %d", i)
		}
		if schedule.OffPeakRate <= 0 {
			return fmt.Errorf("off-peak rate must be positive in schedule at index %d", i)
		}
		if schedule.PeakRate <= schedule.OffPeakRate {
			return fmt.Errorf("peak rate must be greater than off-peak rate in schedule at index %d", i)
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
