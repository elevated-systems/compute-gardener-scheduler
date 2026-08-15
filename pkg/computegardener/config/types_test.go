package config

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		expectErr  bool
		errMessage string
	}{
		{
			name: "valid config",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled:            true,
					Provider:           "electricity-maps-api",
					IntensityThreshold: 200,
					APIConfig: ElectricityMapsAPIConfig{
						APIKey: "test-key",
						URL:    "https://example.com/",
						Region: "test-region",
					},
				},
				Pricing: PriceConfig{
					Enabled:  true,
					Provider: "tou",
					Schedules: []Schedule{
						{
							Name:        "test-schedule",
							DayOfWeek:   "1-5",
							StartTime:   "10:00",
							EndTime:     "16:00",
							PeakRate:    0.30,
							OffPeakRate: 0.15,
						},
					},
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
					MaxSamplesPerPod: 1000,
					PodRetention:     "1h",
				},
			},
			expectErr: false,
		},
		{
			name: "missing API key",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled:            true,
					Provider:           "electricity-maps-api",
					IntensityThreshold: 200,
					APIConfig: ElectricityMapsAPIConfig{
						APIKey: "",
						URL:    "https://example.com/",
						Region: "test-region",
					},
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
			},
			expectErr:  true,
			errMessage: "Electricity Maps API key is required",
		},
		{
			name: "invalid carbon intensity threshold",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled:            true,
					Provider:           "electricity-maps-api",
					IntensityThreshold: 0,
					APIConfig: ElectricityMapsAPIConfig{
						APIKey: "test-key",
						URL:    "https://example.com/",
						Region: "test-region",
					},
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
			},
			expectErr:  true,
			errMessage: "base carbon intensity threshold must be positive",
		},
		{
			name: "invalid pricing config - peak rate less than off-peak",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Pricing: PriceConfig{
					Enabled:  true,
					Provider: "tou",
					Schedules: []Schedule{
						{
							Name:        "test-schedule",
							DayOfWeek:   "1-5",
							StartTime:   "10:00",
							EndTime:     "16:00",
							PeakRate:    0.15, // Off-peak rate is higher
							OffPeakRate: 0.30,
						},
					},
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
			},
			expectErr:  true,
			errMessage: "peak rate must be greater than off-peak rate",
		},
		{
			name: "invalid power config - idle power",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 0, // Zero is invalid
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
			},
			expectErr:  true,
			errMessage: "default idle power must be positive",
		},
		{
			name: "invalid power config - max power <= idle power",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  100, // Same as idle power
					DefaultPUE:       1.15,
				},
			},
			expectErr:  true,
			errMessage: "default max power must be greater than idle power",
		},
		{
			name: "invalid PUE",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       0.9, // Less than 1.0
				},
			},
			expectErr:  true,
			errMessage: "default PUE must be at least 1.0",
		},
		{
			name: "invalid GPU PUE",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					DefaultGPUPUE:    0.9, // Less than 1.0
				},
			},
			expectErr:  true,
			errMessage: "default GPU PUE must be at least 1.0",
		},
		{
			name: "invalid node power config",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					NodePowerConfig: map[string]NodePower{
						"node1": {
							IdlePower: 100,
							MaxPower:  90, // Less than idle power
						},
					},
				},
			},
			expectErr:  true,
			errMessage: "max power must be greater than idle power for node",
		},
		{
			name: "invalid GPU power config",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					NodePowerConfig: map[string]NodePower{
						"node1": {
							IdlePower:    100,
							MaxPower:     400,
							IdleGPUPower: 50,
							MaxGPUPower:  40, // Less than idle GPU power
						},
					},
				},
			},
			expectErr:  true,
			errMessage: "max GPU power must be greater than idle GPU power",
		},
		{
			name: "invalid metrics sampling interval",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "invalid", // Not a valid duration
				},
			},
			expectErr:  true,
			errMessage: "invalid metrics sampling interval",
		},
		{
			name: "invalid pod retention",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
					PodRetention:     "invalid", // Not a valid duration
				},
			},
			expectErr:  true,
			errMessage: "invalid completed pod retention duration",
		},
		{
			name: "invalid downsampling strategy",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval:     "30s",
					PodRetention:         "1h",
					DownsamplingStrategy: "invalid", // Not a valid strategy
				},
			},
			expectErr:  true,
			errMessage: "invalid downsampling strategy",
		},
		{
			name: "missing Prometheus URL",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
					PodRetention:     "1h",
					Prometheus: &PrometheusConfig{
						URL: "", // Empty URL
					},
				},
			},
			expectErr:  true,
			errMessage: "Prometheus URL must be provided",
		},
		{
			name: "invalid Prometheus query timeout",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
					PodRetention:     "1h",
					Prometheus: &PrometheusConfig{
						URL:          "http://prometheus:9090",
						QueryTimeout: "invalid", // Not a valid duration
					},
				},
			},
			expectErr:  true,
			errMessage: "invalid Prometheus query timeout",
		},
		{
			name: "invalid completion delay",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
					PodRetention:     "1h",
					Prometheus: &PrometheusConfig{
						URL:             "http://prometheus:9090",
						QueryTimeout:    "30s",
						CompletionDelay: "invalid", // Not a valid duration
					},
				},
			},
			expectErr:  true,
			errMessage: "invalid pod completion delay",
		},
		{
			name: "default PUE values",
			config: &Config{
				Carbon: CarbonConfig{
					Enabled: false,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					// DefaultPUE and DefaultGPUPUE not set, should use defaults
				},
				Metrics: MetricsConfig{
					SamplingInterval: "30s",
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if (err != nil) != tt.expectErr {
				t.Errorf("Config.Validate() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if err != nil && tt.errMessage != "" && !contains(err.Error(), tt.errMessage) {
				t.Errorf("Config.Validate() error = %v, expected to contain %v", err, tt.errMessage)
			}

			// Check default values are set
			if !tt.expectErr && tt.name == "default PUE values" {
				if tt.config.Power.DefaultPUE != 1.1 {
					t.Errorf("Expected default PUE to be 1.1, got %v", tt.config.Power.DefaultPUE)
				}
				if tt.config.Power.DefaultGPUPUE != 1.15 {
					t.Errorf("Expected default GPU PUE to be 1.15, got %v", tt.config.Power.DefaultGPUPUE)
				}
			}
		})
	}
}

func TestValidateSchedule(t *testing.T) {
	tests := []struct {
		name       string
		schedule   Schedule
		expectErr  bool
		errMessage string
	}{
		{
			name: "valid schedule - weekdays",
			schedule: Schedule{
				Name:      "weekdays",
				DayOfWeek: "1-5",
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr: false,
		},
		{
			name: "valid schedule - single day",
			schedule: Schedule{
				Name:      "monday",
				DayOfWeek: "1",
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr: false,
		},
		{
			name: "valid schedule - multiple days",
			schedule: Schedule{
				Name:      "mon-wed-fri",
				DayOfWeek: "1,3,5",
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr: false,
		},
		{
			name: "invalid day of week - bad format",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "1-5-7", // Invalid format
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid day of week range format",
		},
		{
			name: "invalid day of week - start out of range",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "7-5", // 7 is out of range (0-6)
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid start day in range",
		},
		{
			name: "invalid day of week - end out of range",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "1-7", // 7 is out of range (0-6)
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid end day in range",
		},
		{
			name: "invalid day of week - start > end",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "5-1", // Start greater than end
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid range: start day 5 is greater than end day 1",
		},
		{
			name: "invalid day of week - single day out of range",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "7", // 7 is out of range (0-6)
				StartTime: "09:00",
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid day of week",
		},
		{
			name: "invalid time format - start time",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "1-5",
				StartTime: "9am", // Invalid format, should be 24h
				EndTime:   "17:00",
			},
			expectErr:  true,
			errMessage: "invalid time format",
		},
		{
			name: "invalid time format - end time",
			schedule: Schedule{
				Name:      "invalid",
				DayOfWeek: "1-5",
				StartTime: "09:00",
				EndTime:   "5pm", // Invalid format, should be 24h
			},
			expectErr:  true,
			errMessage: "invalid time format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchedule(tt.schedule)

			if (err != nil) != tt.expectErr {
				t.Errorf("validateSchedule() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if err != nil && tt.errMessage != "" && !contains(err.Error(), tt.errMessage) {
				t.Errorf("validateSchedule() error = %v, expected to contain %v", err, tt.errMessage)
			}
		})
	}
}

func TestValidatePricing(t *testing.T) {
	// Create a minimal config with just the pricing section
	config := &Config{
		Pricing: PriceConfig{
			Enabled:  true,
			Provider: "tou",
			Schedules: []Schedule{
				{
					Name:        "peak",
					DayOfWeek:   "1-5",
					StartTime:   "14:00",
					EndTime:     "19:00",
					PeakRate:    0.30,
					OffPeakRate: 0.15,
				},
			},
		},
	}

	// Should validate successfully
	err := config.validatePricing()
	if err != nil {
		t.Errorf("validatePricing() unexpected error = %v", err)
	}

	// Test invalid schedule
	config.Pricing.Schedules[0].DayOfWeek = "1-7" // Invalid day
	err = config.validatePricing()
	if err == nil {
		t.Error("validatePricing() expected error for invalid schedule")
	}

	// Test missing peak rate
	config.Pricing.Schedules[0].DayOfWeek = "1-5" // Fix day
	config.Pricing.Schedules[0].PeakRate = 0      // Invalid
	err = config.validatePricing()
	if err == nil {
		t.Error("validatePricing() expected error for missing peak rate")
	}

	// Test missing off-peak rate
	config.Pricing.Schedules[0].PeakRate = 0.30 // Fix peak rate
	config.Pricing.Schedules[0].OffPeakRate = 0 // Invalid
	err = config.validatePricing()
	if err == nil {
		t.Error("validatePricing() expected error for missing off-peak rate")
	}
}

func TestValidateHardwareProfiles(t *testing.T) {
	validProfiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"intel-xeon": {
				IdlePower: 20,
				MaxPower:  120,
			},
		},
		GPUProfiles: map[string]PowerProfile{
			"nvidia-a100": {
				IdlePower: 50,
				MaxPower:  300,
			},
		},
		MemProfiles: map[string]MemoryPowerProfile{
			"ddr4": {
				IdlePowerPerGB: 0.1,
				MaxPowerPerGB:  0.3,
			},
		},
		CloudInstanceMapping: map[string]map[string]HardwareComponents{
			"aws": {
				"c5.large": {
					CPUModel:   "intel-xeon",
					GPUModel:   "nvidia-a100",
					MemoryType: "ddr4",
				},
			},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			DefaultPUE:       1.1,
			HardwareProfiles: validProfiles,
		},
	}

	err := config.validateHardwareProfiles()
	if err != nil {
		t.Errorf("validateHardwareProfiles() unexpected error = %v", err)
	}

	tests := []struct {
		name        string
		modifyFunc  func(*HardwareProfiles)
		errContains string
	}{
		{
			name: "empty CPU profiles",
			modifyFunc: func(p *HardwareProfiles) {
				p.CPUProfiles = map[string]PowerProfile{}
			},
			errContains: "no CPU profiles defined",
		},
		{
			name: "invalid CPU idle power",
			modifyFunc: func(p *HardwareProfiles) {
				p.CPUProfiles["intel-xeon"] = PowerProfile{
					IdlePower: 0, // Invalid (zero)
					MaxPower:  120,
				}
			},
			errContains: "idle power for CPU",
		},
		{
			name: "invalid GPU max power",
			modifyFunc: func(p *HardwareProfiles) {
				p.GPUProfiles["nvidia-a100"] = PowerProfile{
					IdlePower: 50,
					MaxPower:  50, // Same as idle (invalid)
				}
			},
			errContains: "max power must be greater than idle power for GPU",
		},
		{
			name: "invalid memory power values",
			modifyFunc: func(p *HardwareProfiles) {
				p.MemProfiles["ddr4"] = MemoryPowerProfile{
					IdlePowerPerGB: 0.3,
					MaxPowerPerGB:  0.1, // Less than idle (invalid)
				}
			},
			errContains: "max power per GB must be greater than idle power per GB",
		},
		{
			name: "missing CPU model",
			modifyFunc: func(p *HardwareProfiles) {
				p.CloudInstanceMapping["aws"]["c5.large"] = HardwareComponents{
					CPUModel: "", // Missing
				}
			},
			errContains: "missing CPU model",
		},
		{
			name: "non-existent CPU model",
			modifyFunc: func(p *HardwareProfiles) {
				p.CloudInstanceMapping["aws"]["c5.large"] = HardwareComponents{
					CPUModel: "non-existent", // Not in CPUProfiles
				}
			},
			errContains: "not found in CPU profiles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start with valid profiles and create a deep copy
			testProfiles := &HardwareProfiles{
				CPUProfiles:          make(map[string]PowerProfile),
				GPUProfiles:          make(map[string]PowerProfile),
				MemProfiles:          make(map[string]MemoryPowerProfile),
				CloudInstanceMapping: make(map[string]map[string]HardwareComponents),
			}

			// Copy CPU profiles
			for k, v := range validProfiles.CPUProfiles {
				testProfiles.CPUProfiles[k] = v
			}

			// Copy GPU profiles
			for k, v := range validProfiles.GPUProfiles {
				testProfiles.GPUProfiles[k] = v
			}

			// Copy memory profiles
			for k, v := range validProfiles.MemProfiles {
				testProfiles.MemProfiles[k] = v
			}

			// Copy cloud instance mapping
			for provider, instances := range validProfiles.CloudInstanceMapping {
				testProfiles.CloudInstanceMapping[provider] = make(map[string]HardwareComponents)
				for instanceType, components := range instances {
					testProfiles.CloudInstanceMapping[provider][instanceType] = components
				}
			}

			// Apply the modification
			tt.modifyFunc(testProfiles)

			// Create a new config with the test profiles
			testConfig := &Config{
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.1,
					HardwareProfiles: testProfiles,
				},
			}

			// Validate the profiles
			err := testConfig.validateHardwareProfiles()

			// Check if the error is as expected
			if err == nil {
				t.Errorf("validateHardwareProfiles() expected error containing '%s', got nil", tt.errContains)
				return
			}

			if !contains(err.Error(), tt.errContains) {
				t.Errorf("validateHardwareProfiles() expected error containing '%s', got '%v'", tt.errContains, err)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return s != "" && substr != "" && len(s) >= len(substr) && strings.Contains(s, substr)
}

func TestValidateAlmanac(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		expectErr  bool
		errMessage string
	}{
		{
			name: "valid almanac config",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					Timeout:             "10s",
					DefaultCarbonWeight: 0.6,
					DefaultPriceWeight:  0.4,
					FailOpen:            true,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr: false,
		},
		{
			name: "missing URL when enabled",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "", // Missing
					DefaultCarbonWeight: 0.6,
					DefaultPriceWeight:  0.4,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "almanac URL is required when almanac is enabled",
		},
		{
			name: "invalid timeout",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					Timeout:             "not-a-duration",
					DefaultCarbonWeight: 0.6,
					DefaultPriceWeight:  0.4,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "invalid almanac timeout",
		},
		{
			name: "weights don't sum to 1.0",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					DefaultCarbonWeight: 0.5,
					DefaultPriceWeight:  0.2, // Sum = 0.7
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "default weights must sum to 1.0",
		},
		{
			name: "carbon weight out of range",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					DefaultCarbonWeight: 1.5, // > 1
					DefaultPriceWeight:  -0.5,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "default carbon weight must be between 0 and 1",
		},
		{
			name: "invalid score threshold",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:               true,
					URL:                   "http://almanac.svc:8080",
					DefaultCarbonWeight:   0.6,
					DefaultPriceWeight:    0.4,
					DefaultScoreThreshold: 1.5, // > 1
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "default score threshold must be between 0 and 1",
		},
		{
			name: "negative score threshold",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:               true,
					URL:                   "http://almanac.svc:8080",
					DefaultCarbonWeight:   0.6,
					DefaultPriceWeight:    0.4,
					DefaultScoreThreshold: -0.1,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr:  true,
			errMessage: "default score threshold must be between 0 and 1",
		},
		{
			name: "default score threshold applied",
			config: &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					DefaultCarbonWeight: 0.6,
					DefaultPriceWeight:  0.4,
					// DefaultScoreThreshold not set
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateAlmanac()

			if (err != nil) != tt.expectErr {
				t.Errorf("validateAlmanac() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if err != nil && tt.errMessage != "" && !contains(err.Error(), tt.errMessage) {
				t.Errorf("validateAlmanac() error = %v, expected to contain %v", err, tt.errMessage)
			}

			// Check default threshold was applied
			if !tt.expectErr && tt.name == "default score threshold applied" {
				if tt.config.Almanac.DefaultScoreThreshold != 0.7 {
					t.Errorf("Expected default score threshold 0.7, got %f", tt.config.Almanac.DefaultScoreThreshold)
				}
			}
		})
	}
}

func TestValidateAlmanacWithCloudDefaults(t *testing.T) {
	config := &Config{
		Almanac: AlmanacConfig{
			Enabled:             true,
			URL:                 "http://almanac.svc:8080",
			DefaultCarbonWeight: 0.5,
			DefaultPriceWeight:  0.5,
			DefaultProvider:     "aws",
			DefaultRegion:       "us-west-2",
			DefaultInstanceType: "m5.xlarge",
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.validateAlmanac()
	if err != nil {
		t.Errorf("validateAlmanac() unexpected error with cloud defaults: %v", err)
	}
}

func TestValidateWithAlmanacEnabledViaValidate(t *testing.T) {
	// Test that Validate() properly calls validateAlmanac()
	config := &Config{
		Almanac: AlmanacConfig{
			Enabled:             true,
			URL:                 "", // Missing URL
			DefaultCarbonWeight: 0.6,
			DefaultPriceWeight:  0.4,
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("Validate() expected error for missing almanac URL")
	}
	if !strings.Contains(err.Error(), "invalid almanac config") {
		t.Errorf("Expected error to contain 'invalid almanac config', got: %v", err)
	}
}

func TestValidateWithAlmanacWeightsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		carbonW     float64
		priceW      float64
		expectError bool
	}{
		{"exact sum 1.0", 0.5, 0.5, false},
		{"slight float tolerance", 0.333, 0.667, false},
		{"too low sum", 0.3, 0.3, true},
		{"too high sum", 0.6, 0.6, true},
		{"zero weights", 0.0, 1.0, false},
		{"inverted weights", 1.0, 0.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Almanac: AlmanacConfig{
					Enabled:             true,
					URL:                 "http://almanac.svc:8080",
					DefaultCarbonWeight: tt.carbonW,
					DefaultPriceWeight:  tt.priceW,
				},
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
			}

			err := config.Validate()
			if (err != nil) != tt.expectError {
				t.Errorf("Validate() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestValidateHardwareProfilesMissingGPUProfile(t *testing.T) {
	profiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"test-cpu": {IdlePower: 50, MaxPower: 200},
		},
		// No GPU profiles defined
		CloudInstanceMapping: map[string]map[string]HardwareComponents{
			"aws": {
				"g4dn.xlarge": {
					CPUModel: "test-cpu",
					GPUModel: "nvidia-t4", // References non-existent GPU profile
				},
			},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			HardwareProfiles: profiles,
		},
	}

	err := config.validateHardwareProfiles()
	if err == nil {
		t.Error("Expected error for non-existent GPU model")
	}
	if !strings.Contains(err.Error(), "not found in GPU profiles") {
		t.Errorf("Expected error about GPU profiles, got: %v", err)
	}
}

func TestValidateHardwareProfilesMissingMemoryProfile(t *testing.T) {
	profiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"test-cpu": {IdlePower: 50, MaxPower: 200},
		},
		// No memory profiles defined
		CloudInstanceMapping: map[string]map[string]HardwareComponents{
			"aws": {
				"c5.large": {
					CPUModel:   "test-cpu",
					MemoryType: "ddr5", // References non-existent memory profile
				},
			},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			HardwareProfiles: profiles,
		},
	}

	err := config.validateHardwareProfiles()
	if err == nil {
		t.Error("Expected error for non-existent memory type")
	}
	if !strings.Contains(err.Error(), "not found in memory profiles") {
		t.Errorf("Expected error about memory profiles, got: %v", err)
	}
}

func TestValidateHardwareProfilesNegativeMemoryPower(t *testing.T) {
	profiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"test-cpu": {IdlePower: 50, MaxPower: 200},
		},
		MemProfiles: map[string]MemoryPowerProfile{
			"bad-mem": {
				IdlePowerPerGB: -0.1, // Negative idle power
				MaxPowerPerGB:  0.3,
			},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			HardwareProfiles: profiles,
		},
	}

	err := config.validateHardwareProfiles()
	if err == nil {
		t.Error("Expected error for negative memory idle power")
	}
	if !strings.Contains(err.Error(), "cannot be negative") {
		t.Errorf("Expected error about negative power, got: %v", err)
	}
}

func TestValidateHardwareProfilesZeroMemoryMaxPower(t *testing.T) {
	profiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"test-cpu": {IdlePower: 50, MaxPower: 200},
		},
		MemProfiles: map[string]MemoryPowerProfile{
			"bad-mem": {
				IdlePowerPerGB: 0.1,
				MaxPowerPerGB:  0, // Zero max power
			},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			HardwareProfiles: profiles,
		},
	}

	err := config.validateHardwareProfiles()
	if err == nil {
		t.Error("Expected error for zero memory max power")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("Expected error about positive power, got: %v", err)
	}
}

func TestValidateWithHardwareProfilesEnabled(t *testing.T) {
	profiles := &HardwareProfiles{
		CPUProfiles: map[string]PowerProfile{
			"test-cpu": {IdlePower: 50, MaxPower: 200},
		},
	}

	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			HardwareProfiles: profiles,
		},
	}

	// Should pass - valid hardware profiles
	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with valid hardware profiles: %v", err)
	}

	// Now test with invalid hardware profiles
	profiles.CPUProfiles["test-cpu"] = PowerProfile{IdlePower: 0, MaxPower: 200}
	err = config.Validate()
	if err == nil {
		t.Error("Validate() expected error for invalid hardware profiles")
	}
	if !strings.Contains(err.Error(), "invalid hardware profiles") {
		t.Errorf("Expected error about hardware profiles, got: %v", err)
	}
}

func TestValidateWithNegativeNodePower(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			NodePowerConfig: map[string]NodePower{
				"bad-node": {
					IdlePower: -50, // Negative idle power
					MaxPower:  400,
				},
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("Expected error for negative node idle power")
	}
	if !strings.Contains(err.Error(), "idle power for node") {
		t.Errorf("Expected error about node idle power, got: %v", err)
	}
}

func TestValidateWithGPUWorkloadTypes(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			NodePowerConfig: map[string]NodePower{
				"gpu-node": {
					IdlePower:    100,
					MaxPower:     500,
					IdleGPUPower: 50,
					MaxGPUPower:  300,
					GPUWorkloadTypes: map[string]float64{
						"inference": 0.8,
						"training":  1.2,
					},
				},
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with GPU workload types: %v", err)
	}
}

func TestValidateWithNodePUEValues(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			NodePowerConfig: map[string]NodePower{
				"node1": {
					IdlePower: 100,
					MaxPower:  400,
					PUE:       1.2,
					GPUPUE:    1.15,
				},
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with node PUE values: %v", err)
	}
}

func TestValidateWithPricingNoRates(t *testing.T) {
	config := &Config{
		Pricing: PriceConfig{
			Enabled:  true,
			Provider: "tou",
			Schedules: []Schedule{
				{
					Name:      "no-rates",
					DayOfWeek: "1-5",
					StartTime: "14:00",
					EndTime:   "19:00",
					// No rates specified - should be valid
				},
			},
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with pricing schedules without rates: %v", err)
	}
}

func TestValidateWithPricingOnlyPeakRate(t *testing.T) {
	config := &Config{
		Pricing: PriceConfig{
			Enabled:  true,
			Provider: "tou",
			Schedules: []Schedule{
				{
					Name:      "peak-only",
					DayOfWeek: "1-5",
					StartTime: "14:00",
					EndTime:   "19:00",
					PeakRate:  0.30,
					// OffPeakRate is zero - should fail when PeakRate is non-zero
				},
			},
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("Validate() expected error when only peak rate is provided")
	}
}

func TestValidateWithCarbonDisabled(t *testing.T) {
	config := &Config{
		Carbon: CarbonConfig{
			Enabled: false,
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with carbon disabled: %v", err)
	}
}

func TestValidateWithCarbonEnabledNoAPIKey(t *testing.T) {
	config := &Config{
		Carbon: CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: 200,
			APIConfig: ElectricityMapsAPIConfig{
				APIKey: "", // Missing
			},
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("Validate() expected error for missing API key")
	}
}

func TestValidateWithNegativeCarbonThreshold(t *testing.T) {
	config := &Config{
		Carbon: CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: -50, // Negative
			APIConfig: ElectricityMapsAPIConfig{
				APIKey: "test-key",
			},
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("Validate() expected error for negative carbon threshold")
	}
}

func TestValidateWithPricingEnabledNoSchedules(t *testing.T) {
	config := &Config{
		Pricing: PriceConfig{
			Enabled:   true,
			Provider:  "tou",
			Schedules: []Schedule{}, // Empty
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with empty pricing schedules: %v", err)
	}
}

func TestValidateWithMultiplePricingSchedules(t *testing.T) {
	config := &Config{
		Pricing: PriceConfig{
			Enabled:  true,
			Provider: "tou",
			Schedules: []Schedule{
				{
					Name:        "weekday",
					DayOfWeek:   "1-5",
					StartTime:   "14:00",
					EndTime:     "19:00",
					PeakRate:    0.30,
					OffPeakRate: 0.15,
				},
				{
					Name:        "weekend",
					DayOfWeek:   "0,6",
					StartTime:   "10:00",
					EndTime:     "16:00",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with multiple pricing schedules: %v", err)
	}
}

func TestValidateWithAlmanacDisabled(t *testing.T) {
	config := &Config{
		Almanac: AlmanacConfig{
			Enabled: false,
			URL:     "", // Should not matter when disabled
		},
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with almanac disabled: %v", err)
	}
}

func TestValidateWithMetricsDefaults(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
		Metrics: MetricsConfig{
			// Empty metrics config - should use defaults
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with empty metrics config: %v", err)
	}
}

func TestValidateWithPrometheusCompletionDelay(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
		Metrics: MetricsConfig{
			SamplingInterval: "30s",
			Prometheus: &PrometheusConfig{
				URL:             "http://prometheus:9090",
				QueryTimeout:    "30s",
				CompletionDelay: "45s",
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with Prometheus completion delay: %v", err)
	}
}

func TestValidateWithAllValidDownsamplingStrategies(t *testing.T) {
	strategies := []string{"lttb", "timeBased", "minMax"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			config := &Config{
				Power: PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
				},
				Metrics: MetricsConfig{
					SamplingInterval:     "30s",
					DownsamplingStrategy: strategy,
				},
			}

			err := config.Validate()
			if err != nil {
				t.Errorf("Validate() unexpected error with strategy %s: %v", strategy, err)
			}
		})
	}
}

func TestValidateWithEmptyDownsamplingStrategy(t *testing.T) {
	config := &Config{
		Power: PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
		},
		Metrics: MetricsConfig{
			SamplingInterval:     "30s",
			DownsamplingStrategy: "", // Empty - should be valid
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() unexpected error with empty downsampling strategy: %v", err)
	}
}
