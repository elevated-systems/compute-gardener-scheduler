package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		Cache: APICacheConfig{
			Timeout:     getDurationOrDefault("API_TIMEOUT", 10*time.Second),
			MaxRetries:  getIntOrDefault("API_MAX_RETRIES", 3),
			RetryDelay:  getDurationOrDefault("API_RETRY_DELAY", 1*time.Second),
			RateLimit:   getIntOrDefault("API_RATE_LIMIT", 10),
			CacheTTL:    getDurationOrDefault("CACHE_TTL", 5*time.Minute),
			MaxCacheAge: getDurationOrDefault("MAX_CACHE_AGE", 1*time.Hour),
		},
		Scheduling: SchedulingConfig{
			MaxSchedulingDelay:  getDurationOrDefault("MAX_SCHEDULING_DELAY", 24*time.Hour),
			EnablePodPriorities: getBoolOrDefault("ENABLE_POD_PRIORITIES", false),
		},
		Carbon: CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: getFloatOrDefault("CARBON_INTENSITY_THRESHOLD", 150.0),
			APIConfig: ElectricityMapsAPIConfig{
				APIKey: os.Getenv("ELECTRICITY_MAP_API_KEY"),
				URL:    getEnvOrDefault("ELECTRICITY_MAP_API_URL", "https://api.electricitymap.org/v3/carbon-intensity/latest?zone="),
				Region: getEnvOrDefault("ELECTRICITY_MAP_API_REGION", "US-CAL-CISO"),
			},
		},
		Pricing: PricingConfig{
			Enabled:   getBoolOrDefault("PRICING_ENABLED", false),
			Provider:  getEnvOrDefault("PRICING_PROVIDER", "tou"),
			Schedules: []Schedule{},
		},
		Power: PowerConfig{
			DefaultIdlePower: getFloatOrDefault("NODE_DEFAULT_IDLE_POWER", 100.0),
			DefaultMaxPower:  getFloatOrDefault("NODE_DEFAULT_MAX_POWER", 400.0),
			NodePowerConfig:  loadNodePowerConfig(),
		},
		Metrics: MetricsConfig{
			SamplingInterval:     getEnvOrDefault("METRICS_SAMPLING_INTERVAL", "30s"),
			MaxSamplesPerPod:     getIntOrDefault("MAX_SAMPLES_PER_POD", 500),
			PodRetention:         getEnvOrDefault("COMPLETED_POD_RETENTION", "1h"),
			DownsamplingStrategy: getEnvOrDefault("DOWNSAMPLING_STRATEGY", "timeBased"),
		},
	}
	
	// Try to load hardware profiles if path is provided
	hwProfilesPath := os.Getenv("HARDWARE_PROFILES_PATH")
	if hwProfilesPath != "" {
		if profiles, err := LoadHardwareProfiles(hwProfilesPath); err == nil {
			cfg.Power.HardwareProfiles = profiles
			klog.V(2).InfoS("Loaded hardware profiles", 
				"path", hwProfilesPath,
				"cpuProfiles", len(profiles.CPUProfiles),
				"gpuProfiles", len(profiles.GPUProfiles),
				"memProfiles", len(profiles.MemProfiles))
		} else {
			klog.ErrorS(err, "Failed to load hardware profiles", "path", hwProfilesPath)
		}
	}

	// Load pricing schedules if enabled and path provided
	if cfg.Pricing.Enabled {
		if schedulePath := os.Getenv("PRICING_SCHEDULES_PATH"); schedulePath != "" {
			if err := loadPricingSchedules(cfg, schedulePath); err != nil {
				return nil, fmt.Errorf("failed to load pricing schedules: %v", err)
			}
		}
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	return cfg, nil
}

// Load creates a new Config from the provided runtime.Object
func Load(obj runtime.Object) (*Config, error) {
	if obj == nil {
		return LoadFromEnv()
	}

	// Use reflection to find the Config field
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// Look for a Config field
	var cfg *Config
	if val.Kind() == reflect.Struct {
		for i := 0; i < val.NumField(); i++ {
			field := val.Field(i)
			if field.Type() == reflect.TypeOf(Config{}) {
				configVal := field.Interface().(Config)
				cfg = &configVal
				break
			}
		}
	}

	if cfg == nil {
		return LoadFromEnv()
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getIntOrDefault(key string, defaultValue int) int {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := strconv.Atoi(strValue); err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid integer value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func getFloatOrDefault(key string, defaultValue float64) float64 {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := strconv.ParseFloat(strValue, 64); err == nil {
			klog.V(2).InfoS("Using float value from environment",
				"key", key,
				"value", value)
			return value
		} else {
			klog.ErrorS(err, "Invalid float value in environment, using default",
				"key", key,
				"value", strValue,
				"default", defaultValue)
		}
	}
	klog.V(2).InfoS("No environment value found, using default",
		"key", key,
		"default", defaultValue)
	return defaultValue
}

func getBoolOrDefault(key string, defaultValue bool) bool {
	if strValue := os.Getenv(key); strValue != "" {
		value, err := strconv.ParseBool(strValue)
		if err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid boolean value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func getDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := time.ParseDuration(strValue); err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid duration value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

// loadNodePowerConfig loads per-node power configurations from environment variables
func loadNodePowerConfig() map[string]NodePower {
	config := make(map[string]NodePower)

	// Look for NODE_POWER_CONFIG_[NAME] environment variables
	// Format: NODE_POWER_CONFIG_worker1=idle:100,max:400
	for _, env := range os.Environ() {
		if name, value, found := strings.Cut(env, "="); found && strings.HasPrefix(name, "NODE_POWER_CONFIG_") {
			nodeName := strings.TrimPrefix(name, "NODE_POWER_CONFIG_")
			parts := strings.Split(value, ",")

			var power NodePower
			for _, part := range parts {
				if key, val, found := strings.Cut(part, ":"); found {
					switch key {
					case "idle":
						if p, err := strconv.ParseFloat(val, 64); err == nil {
							power.IdlePower = p
						}
					case "max":
						if p, err := strconv.ParseFloat(val, 64); err == nil {
							power.MaxPower = p
						}
					}
				}
			}

			// Only add if both values were parsed successfully
			if power.IdlePower > 0 && power.MaxPower > power.IdlePower {
				config[nodeName] = power
			}
		}
	}

	return config
}

// LoadHardwareProfiles tries to load hardware profiles from a ConfigMap
func LoadHardwareProfiles(configMapPath string) (*HardwareProfiles, error) {
	// If no path is provided, return nil (profiles will be disabled)
	if configMapPath == "" {
		return nil, nil
	}
	
	// Read the file data
	data, err := os.ReadFile(configMapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read hardware profiles file: %v", err)
	}
	
	// Parse the YAML
	profiles := &HardwareProfiles{}
	if err := yaml.Unmarshal(data, profiles); err != nil {
		return nil, fmt.Errorf("failed to parse hardware profiles: %v", err)
	}
	
	// Basic validation
	if len(profiles.CPUProfiles) == 0 {
		return nil, fmt.Errorf("no CPU profiles found in hardware profiles")
	}
	
	return profiles, nil
}

func loadPricingSchedules(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read pricing schedules file: %v", err)
	}

	schedules := &PricingConfig{}
	if err := yaml.Unmarshal(data, schedules); err != nil {
		return fmt.Errorf("failed to parse pricing schedules: %v", err)
	}

	// Validate all schedules have same off-peak rate
	if len(schedules.Schedules) > 1 {
		offPeakRate := schedules.Schedules[0].OffPeakRate
		for i, schedule := range schedules.Schedules[1:] {
			if schedule.OffPeakRate != offPeakRate {
				return fmt.Errorf("schedule at index %d has different off-peak rate than first schedule", i+1)
			}
		}
	}

	cfg.Pricing.Schedules = schedules.Schedules
	return nil
}
