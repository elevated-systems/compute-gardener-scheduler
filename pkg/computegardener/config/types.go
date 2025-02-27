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

// NodePower holds power settings for a specific node
type NodePower struct {
	IdlePower float64 `yaml:"idlePower"` // Idle power in watts
	MaxPower  float64 `yaml:"maxPower"`  // Max power in watts
}

// Config holds all configuration for the compute-gardener scheduler
type Config struct {
	API           APIConfig           `yaml:"api"`
	Scheduling    SchedulingConfig    `yaml:"scheduling"`
	Carbon        CarbonConfig        `yaml:"carbon"`
	Pricing       PricingConfig       `yaml:"pricing"`
	Observability ObservabilityConfig `yaml:"observability"`
	Power         PowerConfig         `yaml:"power"`
}

// APIConfig holds configuration for external API interactions
type APIConfig struct {
	ElectricityMapKey    string        `yaml:"electricityMapKey"`
	ElectricityMapURL    string        `yaml:"electricityMapUrl"`
	ElectricityMapRegion string        `yaml:"electricityMapRegion"`
	Timeout              time.Duration `yaml:"timeout"`
	MaxRetries           int           `yaml:"maxRetries"`
	RetryDelay           time.Duration `yaml:"retryDelay"`
	RateLimit            int           `yaml:"rateLimit"`
	CacheTTL             time.Duration `yaml:"cacheTTL"`
	MaxCacheAge          time.Duration `yaml:"maxCacheAge"`
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

// PricingConfig holds configuration for price-aware scheduling
type CarbonConfig struct {
	IntensityThreshold float64 `yaml:"carbonIntensityThreshold"`
	DefaultRegion      string  `yaml:"defaultRegion"`
}

// PricingConfig holds configuration for price-aware scheduling
type PricingConfig struct {
	Enabled   bool       `yaml:"enabled"`
	Provider  string     `yaml:"provider"`  // e.g. "tou" for time-of-use pricing
	Schedules []Schedule `yaml:"schedules"` // Time-based pricing periods with their rates
}

// ObservabilityConfig holds configuration for monitoring and debugging
type ObservabilityConfig struct {
	MetricsEnabled     bool   `yaml:"metricsEnabled"`
	MetricsPort        int    `yaml:"metricsPort"`
	HealthCheckEnabled bool   `yaml:"healthCheckEnabled"`
	HealthCheckPort    int    `yaml:"healthCheckPort"`
	LogLevel           string `yaml:"logLevel"`
	EnableTracing      bool   `yaml:"enableTracing"`
}

// Validate performs validation of the configuration
func (c *Config) Validate() error {
	if c.API.ElectricityMapKey == "" {
		return fmt.Errorf("Electricity Map API key is required")
	}

	if c.Carbon.IntensityThreshold <= 0 {
		return fmt.Errorf("base carbon intensity threshold must be positive")
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
