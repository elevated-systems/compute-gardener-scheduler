package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromEnv(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create valid YAML config file
	validConfigYAML := `
cache:
  timeout: 5s
  maxRetries: 3
  retryDelay: 100ms
  rateLimit: 10
  cacheTTL: 30m
  maxCacheAge: 24h
scheduling:
  maxSchedulingDelay: 30m
  enablePodPriorities: true
carbon:
  enabled: true
  provider: electricity-maps-api
  carbonIntensityThreshold: 200
  api:
    apiKey: test-key
    url: https://example.com/
    region: test-region
pricing:
  enabled: true
  provider: tou
  schedules:
    - name: peak
      dayOfWeek: 1-5
      startTime: 14:00
      endTime: 19:00
      timezone: America/Los_Angeles
      peakRate: 0.30
      offPeakRate: 0.15
power:
  defaultIdlePower: 100
  defaultMaxPower: 400
  defaultPUE: 1.15
  defaultGPUPUE: 1.25
metrics:
  samplingInterval: 30s
  maxSamplesPerPod: 1000
  podRetention: 1h
  downsamplingStrategy: lttb
`
	validConfigPath := filepath.Join(tempDir, "valid-config.yaml")
	if err := os.WriteFile(validConfigPath, []byte(validConfigYAML), 0644); err != nil {
		t.Fatalf("Failed to write valid config file: %v", err)
	}

	// Create invalid YAML config file
	invalidConfigYAML := `
cache: invalid-yaml
  timeout: [not-a-duration]
`
	invalidConfigPath := filepath.Join(tempDir, "invalid-config.yaml")
	if err := os.WriteFile(invalidConfigPath, []byte(invalidConfigYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid config file: %v", err)
	}

	// Create a valid schedules file
	validSchedulesYAML := `
schedules:
  - name: peak
    dayOfWeek: 1-5
    startTime: 14:00
    endTime: 19:00
    timezone: America/Los_Angeles
    peakRate: 0.30
    offPeakRate: 0.15
  - name: weekend
    dayOfWeek: 0,6
    startTime: 10:00
    endTime: 18:00
    timezone: America/Los_Angeles
    peakRate: 0.25
    offPeakRate: 0.12
`
	validSchedulesPath := filepath.Join(tempDir, "valid-schedules.yaml")
	if err := os.WriteFile(validSchedulesPath, []byte(validSchedulesYAML), 0644); err != nil {
		t.Fatalf("Failed to write valid schedules file: %v", err)
	}

	// Create an invalid schedules file
	invalidSchedulesYAML := `
schedules: [not-valid-yaml
`
	invalidSchedulesPath := filepath.Join(tempDir, "invalid-schedules.yaml")
	if err := os.WriteFile(invalidSchedulesPath, []byte(invalidSchedulesYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid schedules file: %v", err)
	}

	// Set environment variables for testing
	os.Setenv("CARBON_ENABLED", "false") // Set to false to avoid API key validation error
	os.Setenv("CARBON_INTENSITY_THRESHOLD", "250")
	os.Setenv("ELECTRICITY_MAP_API_KEY", "test-key-from-env")
	os.Setenv("PRICING_ENABLED", "true")
	os.Setenv("MAX_SCHEDULING_DELAY", "2h")
	os.Setenv("METRICS_SAMPLING_INTERVAL", "15s")

	defer func() {
		os.Unsetenv("CARBON_ENABLED")
		os.Unsetenv("CARBON_INTENSITY_THRESHOLD")
		os.Unsetenv("ELECTRICITY_MAP_API_KEY")
		os.Unsetenv("PRICING_ENABLED")
		os.Unsetenv("MAX_SCHEDULING_DELAY")
		os.Unsetenv("METRICS_SAMPLING_INTERVAL")
	}()

	// Test loading config from environment
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	// Verify environment variables were properly loaded
	if cfg.Carbon.Enabled {
		t.Errorf("Expected Carbon.Enabled to be false, got true")
	}

	if cfg.Carbon.IntensityThreshold != 250 {
		t.Errorf("Expected Carbon.IntensityThreshold to be 250, got %v", cfg.Carbon.IntensityThreshold)
	}

	if !cfg.Pricing.Enabled {
		t.Errorf("Expected Pricing.Enabled to be true")
	}

	if cfg.Scheduling.MaxSchedulingDelay != 2*time.Hour {
		t.Errorf("Expected MaxSchedulingDelay to be 2h, got %v", cfg.Scheduling.MaxSchedulingDelay)
	}

	if cfg.Metrics.SamplingInterval != "15s" {
		t.Errorf("Expected SamplingInterval to be 15s, got %v", cfg.Metrics.SamplingInterval)
	}
}

func TestLoadPriceSchedules(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "schedules-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a valid schedules file with consistent off-peak rates
	validSchedulesYAML := `
schedules:
  - name: peak
    dayOfWeek: 1-5
    startTime: 14:00
    endTime: 19:00
    timezone: America/Los_Angeles
    peakRate: 0.30
    offPeakRate: 0.15
  - name: weekend
    dayOfWeek: 0,6
    startTime: 10:00
    endTime: 18:00
    timezone: America/Los_Angeles
    peakRate: 0.25
    offPeakRate: 0.15
`
	validSchedulesPath := filepath.Join(tempDir, "valid-schedules.yaml")
	if err := os.WriteFile(validSchedulesPath, []byte(validSchedulesYAML), 0644); err != nil {
		t.Fatalf("Failed to write valid schedules file: %v", err)
	}

	// Create an invalid schedules file with bad YAML
	invalidYAMLPath := filepath.Join(tempDir, "invalid-yaml.yaml")
	if err := os.WriteFile(invalidYAMLPath, []byte("not valid yaml::["), 0644); err != nil {
		t.Fatalf("Failed to write invalid YAML file: %v", err)
	}

	// Create an invalid schedules file with valid YAML but invalid schedule
	invalidScheduleYAML := `
schedules:
  - name: invalid
    dayOfWeek: 1-7  # Invalid: day 7 is out of range
    startTime: 14:00
    endTime: 19:00
`
	invalidSchedulePath := filepath.Join(tempDir, "invalid-schedule.yaml")
	if err := os.WriteFile(invalidSchedulePath, []byte(invalidScheduleYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid schedule file: %v", err)
	}

	tests := []struct {
		name         string
		configPath   string
		expectErr    bool
		expectedLen  int
		expectedName string
	}{
		{
			name:         "valid schedules",
			configPath:   validSchedulesPath,
			expectErr:    false,
			expectedLen:  2,
			expectedName: "peak",
		},
		{
			name:       "nonexistent file",
			configPath: filepath.Join(tempDir, "nonexistent.yaml"),
			expectErr:  true,
		},
		{
			name:       "invalid yaml",
			configPath: invalidYAMLPath,
			expectErr:  true,
		},
		{
			name:       "invalid schedule",
			configPath: invalidSchedulePath,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Pricing: PriceConfig{
					Enabled:   true,
					Provider:  "tou",
					Schedules: []Schedule{},
				},
			}

			err := loadPricingSchedules(cfg, tt.configPath)
			if (err != nil) != tt.expectErr {
				t.Errorf("loadPricingSchedules() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr {
				if len(cfg.Pricing.Schedules) != tt.expectedLen {
					t.Errorf("Expected %d schedules, got %d", tt.expectedLen, len(cfg.Pricing.Schedules))
				}
				if cfg.Pricing.Schedules[0].Name != tt.expectedName {
					t.Errorf("Expected first schedule name %s, got %s", tt.expectedName, cfg.Pricing.Schedules[0].Name)
				}
			}
		})
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	// Test string values
	const envVar = "TEST_ENV_VAR"
	const defaultVal = "default"
	const testVal = "test-value"

	// Test with env var not set
	os.Unsetenv(envVar)
	if getEnvOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default value when env var not set")
	}

	// Test with env var set
	os.Setenv(envVar, testVal)
	if getEnvOrDefault(envVar, defaultVal) != testVal {
		t.Errorf("Expected env var value when set")
	}
	os.Unsetenv(envVar)
}

func TestGetBoolOrDefault(t *testing.T) {
	const envVar = "TEST_BOOL_VAR"
	const defaultVal = false

	// Test with env var not set
	os.Unsetenv(envVar)
	if getBoolOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default bool value when env var not set")
	}

	// Test with env var set to "true"
	os.Setenv(envVar, "true")
	if !getBoolOrDefault(envVar, defaultVal) {
		t.Errorf("Expected true when env var set to 'true'")
	}

	// Test with env var set to "1"
	os.Setenv(envVar, "1")
	if !getBoolOrDefault(envVar, defaultVal) {
		t.Errorf("Expected true when env var set to '1'")
	}

	// Test with env var set to something invalid
	os.Setenv(envVar, "not-a-bool")
	if getBoolOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default when env var set to invalid value")
	}
	os.Unsetenv(envVar)
}

func TestGetIntOrDefault(t *testing.T) {
	const envVar = "TEST_INT_VAR"
	const defaultVal = 42
	const testVal = 100

	// Test with env var not set
	os.Unsetenv(envVar)
	if getIntOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default int value when env var not set")
	}

	// Test with env var set
	os.Setenv(envVar, "100")
	if getIntOrDefault(envVar, defaultVal) != testVal {
		t.Errorf("Expected %d when env var set to '%d'", testVal, testVal)
	}

	// Test with env var set to invalid value
	os.Setenv(envVar, "not-an-int")
	if getIntOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default when env var set to invalid value")
	}
	os.Unsetenv(envVar)
}

func TestGetFloatOrDefault(t *testing.T) {
	const envVar = "TEST_FLOAT_VAR"
	const defaultVal = 3.14
	const testVal = 2.718

	// Test with env var not set
	os.Unsetenv(envVar)
	if getFloatOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default float value when env var not set")
	}

	// Test with env var set
	os.Setenv(envVar, "2.718")
	if getFloatOrDefault(envVar, defaultVal) != testVal {
		t.Errorf("Expected %f when env var set to '%f'", testVal, testVal)
	}

	// Test with env var set to invalid value
	os.Setenv(envVar, "not-a-float")
	if getFloatOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default when env var set to invalid value")
	}
	os.Unsetenv(envVar)
}

func TestGetDurationOrDefault(t *testing.T) {
	const envVar = "TEST_DURATION_VAR"
	defaultVal := 10 * time.Second
	testVal := 5 * time.Minute

	// Test with env var not set
	os.Unsetenv(envVar)
	if getDurationOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default duration value when env var not set")
	}

	// Test with env var set
	os.Setenv(envVar, "5m")
	if getDurationOrDefault(envVar, defaultVal) != testVal {
		t.Errorf("Expected %v when env var set to '%v'", testVal, testVal)
	}

	// Test with env var set to invalid value
	os.Setenv(envVar, "not-a-duration")
	if getDurationOrDefault(envVar, defaultVal) != defaultVal {
		t.Errorf("Expected default when env var set to invalid value")
	}
	os.Unsetenv(envVar)
}
