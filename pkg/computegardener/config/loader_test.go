package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestLoadFromEnvWithHardwareProfiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hw-profiles-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	hwProfilesYAML := `
cpuProfiles:
  test-cpu:
    idlePower: 50
    maxPower: 200
gpuProfiles:
  test-gpu:
    idlePower: 30
    maxPower: 150
cloudInstanceMapping:
  aws:
    test-instance:
      cpuModel: test-cpu
`
	hwPath := filepath.Join(tempDir, "hw-profiles.yaml")
	if err := os.WriteFile(hwPath, []byte(hwProfilesYAML), 0644); err != nil {
		t.Fatalf("Failed to write hw profiles: %v", err)
	}

	os.Setenv("HARDWARE_PROFILES_PATH", hwPath)
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("HARDWARE_PROFILES_PATH")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Power.HardwareProfiles == nil {
		t.Error("Expected hardware profiles to be loaded")
	}
	if _, ok := cfg.Power.HardwareProfiles.CPUProfiles["test-cpu"]; !ok {
		t.Error("Expected test-cpu profile")
	}
}

func TestLoadFromEnvWithInvalidHardwareProfiles(t *testing.T) {
	os.Setenv("HARDWARE_PROFILES_PATH", "/nonexistent/path.yaml")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("HARDWARE_PROFILES_PATH")
		os.Unsetenv("CARBON_ENABLED")
	}()

	// Should not fail, just log error and continue
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Power.HardwareProfiles != nil {
		t.Error("Expected no hardware profiles when file not found")
	}
}

func TestLoadFromEnvWithPricingSchedules(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pricing-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	schedulesYAML := `
schedules:
  - name: weekday-peak
    dayOfWeek: 1-5
    startTime: 14:00
    endTime: 19:00
    peakRate: 0.30
    offPeakRate: 0.15
`
	schedPath := filepath.Join(tempDir, "schedules.yaml")
	if err := os.WriteFile(schedPath, []byte(schedulesYAML), 0644); err != nil {
		t.Fatalf("Failed to write schedules: %v", err)
	}

	os.Setenv("PRICING_ENABLED", "true")
	os.Setenv("PRICING_SCHEDULES_PATH", schedPath)
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("PRICING_ENABLED")
		os.Unsetenv("PRICING_SCHEDULES_PATH")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if len(cfg.Pricing.Schedules) != 1 {
		t.Errorf("Expected 1 schedule, got %d", len(cfg.Pricing.Schedules))
	}
	if cfg.Pricing.Schedules[0].Name != "weekday-peak" {
		t.Errorf("Expected schedule name 'weekday-peak', got %s", cfg.Pricing.Schedules[0].Name)
	}
}

func TestLoadFromEnvWithInvalidPricingSchedules(t *testing.T) {
	os.Setenv("PRICING_ENABLED", "true")
	os.Setenv("PRICING_SCHEDULES_PATH", "/nonexistent/schedules.yaml")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("PRICING_ENABLED")
		os.Unsetenv("PRICING_SCHEDULES_PATH")
		os.Unsetenv("CARBON_ENABLED")
	}()

	_, err := LoadFromEnv()
	if err == nil {
		t.Error("Expected error when pricing schedules file not found")
	}
}

func TestLoadFromEnvWithAlmanac(t *testing.T) {
	os.Setenv("ALMANAC_ENABLED", "true")
	os.Setenv("ALMANAC_URL", "http://almanac.svc:8080")
	os.Setenv("ALMANAC_DEFAULT_CARBON_WEIGHT", "0.7")
	os.Setenv("ALMANAC_DEFAULT_PRICE_WEIGHT", "0.3")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("ALMANAC_ENABLED")
		os.Unsetenv("ALMANAC_URL")
		os.Unsetenv("ALMANAC_DEFAULT_CARBON_WEIGHT")
		os.Unsetenv("ALMANAC_DEFAULT_PRICE_WEIGHT")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if !cfg.Almanac.Enabled {
		t.Error("Expected Almanac.Enabled to be true")
	}
	if cfg.Almanac.URL != "http://almanac.svc:8080" {
		t.Errorf("Expected Almanac.URL to be 'http://almanac.svc:8080', got %s", cfg.Almanac.URL)
	}
	if cfg.Almanac.DefaultCarbonWeight != 0.7 {
		t.Errorf("Expected DefaultCarbonWeight 0.7, got %f", cfg.Almanac.DefaultCarbonWeight)
	}
}

func TestLoadFromEnvWithInvalidAlmanacWeights(t *testing.T) {
	os.Setenv("ALMANAC_ENABLED", "true")
	os.Setenv("ALMANAC_URL", "http://almanac.svc:8080")
	os.Setenv("ALMANAC_DEFAULT_CARBON_WEIGHT", "0.5")
	os.Setenv("ALMANAC_DEFAULT_PRICE_WEIGHT", "0.2") // Sum != 1.0
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("ALMANAC_ENABLED")
		os.Unsetenv("ALMANAC_URL")
		os.Unsetenv("ALMANAC_DEFAULT_CARBON_WEIGHT")
		os.Unsetenv("ALMANAC_DEFAULT_PRICE_WEIGHT")
		os.Unsetenv("CARBON_ENABLED")
	}()

	_, err := LoadFromEnv()
	if err == nil {
		t.Error("Expected error when almanac weights don't sum to 1.0")
	}
}

func TestLoadFromEnvWithPrometheus(t *testing.T) {
	os.Setenv("PROMETHEUS_URL", "http://prometheus.monitoring:9090")
	os.Setenv("PROMETHEUS_QUERY_TIMEOUT", "45s")
	os.Setenv("PROMETHEUS_USE_DCGM", "false")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("PROMETHEUS_URL")
		os.Unsetenv("PROMETHEUS_QUERY_TIMEOUT")
		os.Unsetenv("PROMETHEUS_USE_DCGM")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Metrics.Prometheus == nil {
		t.Error("Expected Prometheus config to be loaded")
	} else {
		if cfg.Metrics.Prometheus.URL != "http://prometheus.monitoring:9090" {
			t.Errorf("Expected Prometheus URL, got %s", cfg.Metrics.Prometheus.URL)
		}
		if cfg.Metrics.Prometheus.QueryTimeout != "45s" {
			t.Errorf("Expected QueryTimeout 45s, got %s", cfg.Metrics.Prometheus.QueryTimeout)
		}
		if cfg.Metrics.Prometheus.UseDCGM {
			t.Error("Expected UseDCGM to be false")
		}
	}
}

func TestLoadFromEnvWithPrometheusCompletionDelay(t *testing.T) {
	os.Setenv("PROMETHEUS_URL", "http://prometheus.monitoring:9090")
	os.Setenv("PROMETHEUS_COMPLETION_DELAY", "60s")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("PROMETHEUS_URL")
		os.Unsetenv("PROMETHEUS_COMPLETION_DELAY")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Metrics.Prometheus == nil {
		t.Error("Expected Prometheus config to be loaded")
	}
}

func TestLoadNodePowerConfig(t *testing.T) {
	// Clean up any existing NODE_POWER_CONFIG env vars
	for _, env := range os.Environ() {
		if len(env) > 17 && env[:17] == "NODE_POWER_CONFIG_" {
			parts := strings.SplitN(env, "=", 2)
			os.Unsetenv(parts[0])
		}
	}

	// Test with no node power config
	config := loadNodePowerConfig()
	if len(config) != 0 {
		t.Errorf("Expected empty config, got %d entries", len(config))
	}

	// Test with single node
	os.Setenv("NODE_POWER_CONFIG_worker1", "idle:100,max:400")
	config = loadNodePowerConfig()
	if len(config) != 1 {
		t.Errorf("Expected 1 node config, got %d", len(config))
	}
	np, ok := config["worker1"]
	if !ok {
		t.Error("Expected worker1 in config")
	}
	if np.IdlePower != 100 {
		t.Errorf("Expected idle power 100, got %f", np.IdlePower)
	}
	if np.MaxPower != 400 {
		t.Errorf("Expected max power 400, got %f", np.MaxPower)
	}

	// Test with multiple nodes
	os.Setenv("NODE_POWER_CONFIG_worker2", "idle:150,max:500")
	config = loadNodePowerConfig()
	if len(config) != 2 {
		t.Errorf("Expected 2 node configs, got %d", len(config))
	}

	// Test with invalid values (max < idle should be skipped)
	os.Setenv("NODE_POWER_CONFIG_invalid", "idle:400,max:100")
	config = loadNodePowerConfig()
	if len(config) != 2 {
		t.Errorf("Expected 2 valid node configs (invalid should be skipped), got %d", len(config))
	}

	// Test with missing values (should be skipped)
	os.Setenv("NODE_POWER_CONFIG_partial", "idle:100")
	config = loadNodePowerConfig()
	if len(config) != 2 {
		t.Errorf("Expected 2 valid node configs (partial should be skipped), got %d", len(config))
	}

	// Clean up
	os.Unsetenv("NODE_POWER_CONFIG_worker1")
	os.Unsetenv("NODE_POWER_CONFIG_worker2")
	os.Unsetenv("NODE_POWER_CONFIG_invalid")
	os.Unsetenv("NODE_POWER_CONFIG_partial")
}

func TestLoadHardwareProfiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hw-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test with empty path
	profiles, err := LoadHardwareProfiles("")
	if err != nil {
		t.Fatalf("LoadHardwareProfiles() error with empty path: %v", err)
	}
	if profiles != nil {
		t.Error("Expected nil profiles with empty path")
	}

	// Test with nonexistent file
	_, err = LoadHardwareProfiles("/nonexistent/file.yaml")
	if err == nil {
		t.Error("Expected error with nonexistent file")
	}

	// Test with invalid YAML
	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatalf("Failed to write invalid yaml: %v", err)
	}
	_, err = LoadHardwareProfiles(invalidPath)
	if err == nil {
		t.Error("Expected error with invalid YAML")
	}

	// Test with valid profiles
	validYAML := `
cpuProfiles:
  test-cpu:
    idlePower: 50
    maxPower: 200
cloudInstanceMapping:
  aws:
    m5.large:
      cpuModel: test-cpu
`
	validPath := filepath.Join(tempDir, "valid.yaml")
	if err := os.WriteFile(validPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to write valid yaml: %v", err)
	}
	profiles, err = LoadHardwareProfiles(validPath)
	if err != nil {
		t.Fatalf("LoadHardwareProfiles() error with valid file: %v", err)
	}
	if len(profiles.CPUProfiles) != 1 {
		t.Errorf("Expected 1 CPU profile, got %d", len(profiles.CPUProfiles))
	}

	// Test with no CPU profiles (should error)
	noCPUYAML := `
gpuProfiles:
  test-gpu:
    idlePower: 30
    maxPower: 150
`
	noCPUPath := filepath.Join(tempDir, "no-cpu.yaml")
	if err := os.WriteFile(noCPUPath, []byte(noCPUYAML), 0644); err != nil {
		t.Fatalf("Failed to write no-cpu yaml: %v", err)
	}
	_, err = LoadHardwareProfiles(noCPUPath)
	if err == nil {
		t.Error("Expected error with no CPU profiles")
	}
}

func TestLoadPrometheusConfig(t *testing.T) {
	// Clean up env vars
	os.Unsetenv("PROMETHEUS_URL")
	os.Unsetenv("PROMETHEUS_QUERY_TIMEOUT")
	os.Unsetenv("PROMETHEUS_USE_DCGM")
	os.Unsetenv("PROMETHEUS_DCGM_POWER_METRIC")

	// Test with no URL (should return nil)
	config := loadPrometheusConfig()
	if config != nil {
		t.Error("Expected nil config when PROMETHEUS_URL not set")
	}

	// Test with URL only (should use defaults)
	os.Setenv("PROMETHEUS_URL", "http://prometheus:9090")
	config = loadPrometheusConfig()
	if config == nil {
		t.Fatal("Expected config when PROMETHEUS_URL is set")
	}
	if config.URL != "http://prometheus:9090" {
		t.Errorf("Expected URL 'http://prometheus:9090', got %s", config.URL)
	}
	if config.QueryTimeout != "30s" {
		t.Errorf("Expected default QueryTimeout '30s', got %s", config.QueryTimeout)
	}
	if !config.UseDCGM {
		t.Error("Expected default UseDCGM to be true")
	}
	if config.DCGMPowerMetric != "DCGM_FI_DEV_POWER_USAGE" {
		t.Errorf("Expected default DCGMPowerMetric, got %s", config.DCGMPowerMetric)
	}

	// Test with custom settings
	os.Setenv("PROMETHEUS_QUERY_TIMEOUT", "60s")
	os.Setenv("PROMETHEUS_USE_DCGM", "false")
	os.Setenv("PROMETHEUS_DCGM_POWER_METRIC", "custom_metric")
	config = loadPrometheusConfig()
	if config.QueryTimeout != "60s" {
		t.Errorf("Expected QueryTimeout '60s', got %s", config.QueryTimeout)
	}
	if config.UseDCGM {
		t.Error("Expected UseDCGM to be false")
	}
	if config.DCGMPowerMetric != "custom_metric" {
		t.Errorf("Expected DCGMPowerMetric 'custom_metric', got %s", config.DCGMPowerMetric)
	}

	// Clean up
	os.Unsetenv("PROMETHEUS_URL")
	os.Unsetenv("PROMETHEUS_QUERY_TIMEOUT")
	os.Unsetenv("PROMETHEUS_USE_DCGM")
	os.Unsetenv("PROMETHEUS_DCGM_POWER_METRIC")
}

func TestLoadFromEnvDefaults(t *testing.T) {
	// Clean up all config env vars
	os.Unsetenv("CARBON_ENABLED")
	os.Unsetenv("PRICING_ENABLED")
	os.Unsetenv("ALMANAC_ENABLED")
	os.Unsetenv("PROMETHEUS_URL")
	os.Unsetenv("HARDWARE_PROFILES_PATH")
	// Set API key so carbon validation passes with default enabled=true
	os.Setenv("ELECTRICITY_MAP_API_KEY", "test-key")
	defer func() {
		os.Unsetenv("ELECTRICITY_MAP_API_KEY")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error with defaults: %v", err)
	}

	// Check defaults
	if cfg.Carbon.Enabled != true {
		t.Errorf("Expected default Carbon.Enabled=true, got %v", cfg.Carbon.Enabled)
	}
	if cfg.Carbon.Provider != "electricity-maps-api" {
		t.Errorf("Expected default Carbon.Provider='electricity-maps-api', got %s", cfg.Carbon.Provider)
	}
	if cfg.Carbon.IntensityThreshold != 150.0 {
		t.Errorf("Expected default Carbon.IntensityThreshold=150.0, got %f", cfg.Carbon.IntensityThreshold)
	}
	if cfg.Pricing.Enabled != false {
		t.Errorf("Expected default Pricing.Enabled=false, got %v", cfg.Pricing.Enabled)
	}
	if cfg.Almanac.Enabled != false {
		t.Errorf("Expected default Almanac.Enabled=false, got %v", cfg.Almanac.Enabled)
	}
	if cfg.Almanac.FailOpen != true {
		t.Errorf("Expected default Almanac.FailOpen=true, got %v", cfg.Almanac.FailOpen)
	}
	if cfg.Metrics.SamplingInterval != "30s" {
		t.Errorf("Expected default SamplingInterval='30s', got %s", cfg.Metrics.SamplingInterval)
	}
	if cfg.Metrics.Prometheus != nil {
		t.Error("Expected default Prometheus=nil")
	}
}

func TestLoadFromEnvWithCarbonEnabled(t *testing.T) {
	os.Setenv("CARBON_ENABLED", "true")
	os.Setenv("ELECTRICITY_MAP_API_KEY", "test-api-key")
	os.Setenv("CARBON_INTENSITY_THRESHOLD", "300")
	defer func() {
		os.Unsetenv("CARBON_ENABLED")
		os.Unsetenv("ELECTRICITY_MAP_API_KEY")
		os.Unsetenv("CARBON_INTENSITY_THRESHOLD")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	if !cfg.Carbon.Enabled {
		t.Error("Expected Carbon.Enabled to be true")
	}
	if cfg.Carbon.APIConfig.APIKey != "test-api-key" {
		t.Errorf("Expected APIKey 'test-api-key', got %s", cfg.Carbon.APIConfig.APIKey)
	}
	if cfg.Carbon.IntensityThreshold != 300 {
		t.Errorf("Expected IntensityThreshold 300, got %f", cfg.Carbon.IntensityThreshold)
	}
}

func TestLoadFromEnvMissingAPIKey(t *testing.T) {
	os.Setenv("CARBON_ENABLED", "true")
	os.Setenv("ELECTRICITY_MAP_API_KEY", "")
	defer func() {
		os.Unsetenv("CARBON_ENABLED")
		os.Unsetenv("ELECTRICITY_MAP_API_KEY")
	}()

	_, err := LoadFromEnv()
	if err == nil {
		t.Error("Expected error when carbon enabled but API key missing")
	}
}

func TestLoadFromEnvWithInvalidInt(t *testing.T) {
	os.Setenv("API_MAX_RETRIES", "not-a-number")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("API_MAX_RETRIES")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	// Should use default of 3
	if cfg.Cache.MaxRetries != 3 {
		t.Errorf("Expected default MaxRetries=3, got %d", cfg.Cache.MaxRetries)
	}
}

func TestLoadFromEnvWithInvalidDuration(t *testing.T) {
	os.Setenv("API_TIMEOUT", "not-a-duration")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("API_TIMEOUT")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	// Should use default of 10s
	if cfg.Cache.Timeout != 10*time.Second {
		t.Errorf("Expected default Timeout=10s, got %v", cfg.Cache.Timeout)
	}
}

func TestLoadWithNilObject(t *testing.T) {
	os.Setenv("CARBON_ENABLED", "false")
	defer os.Unsetenv("CARBON_ENABLED")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}
	if cfg == nil {
		t.Error("Load(nil) should return config from env")
	}
}

func TestLoadFromEnvWithPricingScheduleWithDifferentOffPeakRates(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pricing-rates-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	schedulesYAML := `
schedules:
  - name: weekday
    dayOfWeek: 1-5
    startTime: 14:00
    endTime: 19:00
    peakRate: 0.30
    offPeakRate: 0.15
  - name: weekend
    dayOfWeek: 0,6
    startTime: 10:00
    endTime: 16:00
    peakRate: 0.25
    offPeakRate: 0.10
`
	schedPath := filepath.Join(tempDir, "schedules.yaml")
	if err := os.WriteFile(schedPath, []byte(schedulesYAML), 0644); err != nil {
		t.Fatalf("Failed to write schedules: %v", err)
	}

	os.Setenv("PRICING_ENABLED", "true")
	os.Setenv("PRICING_SCHEDULES_PATH", schedPath)
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("PRICING_ENABLED")
		os.Unsetenv("PRICING_SCHEDULES_PATH")
		os.Unsetenv("CARBON_ENABLED")
	}()

	_, err = LoadFromEnv()
	if err == nil {
		t.Error("Expected error when schedules have different off-peak rates")
	}
}

func TestLoadFromEnvWithInvalidFloat(t *testing.T) {
	os.Setenv("NODE_DEFAULT_IDLE_POWER", "not-a-float")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("NODE_DEFAULT_IDLE_POWER")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	// Should use default of 100.0
	if cfg.Power.DefaultIdlePower != 100.0 {
		t.Errorf("Expected default DefaultIdlePower=100.0, got %f", cfg.Power.DefaultIdlePower)
	}
}

func TestLoadFromEnvWithSchedulingConfig(t *testing.T) {
	os.Setenv("MAX_SCHEDULING_DELAY", "48h")
	os.Setenv("ENABLE_POD_PRIORITIES", "true")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("MAX_SCHEDULING_DELAY")
		os.Unsetenv("ENABLE_POD_PRIORITIES")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	if cfg.Scheduling.MaxSchedulingDelay != 48*time.Hour {
		t.Errorf("Expected MaxSchedulingDelay=48h, got %v", cfg.Scheduling.MaxSchedulingDelay)
	}
	if !cfg.Scheduling.EnablePodPriorities {
		t.Error("Expected EnablePodPriorities=true")
	}
}

func TestLoadFromEnvWithCacheConfig(t *testing.T) {
	os.Setenv("API_TIMEOUT", "30s")
	os.Setenv("API_MAX_RETRIES", "5")
	os.Setenv("API_RETRY_DELAY", "2s")
	os.Setenv("API_RATE_LIMIT", "20")
	os.Setenv("CACHE_TTL", "10m")
	os.Setenv("MAX_CACHE_AGE", "2h")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("API_TIMEOUT")
		os.Unsetenv("API_MAX_RETRIES")
		os.Unsetenv("API_RETRY_DELAY")
		os.Unsetenv("API_RATE_LIMIT")
		os.Unsetenv("CACHE_TTL")
		os.Unsetenv("MAX_CACHE_AGE")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	if cfg.Cache.Timeout != 30*time.Second {
		t.Errorf("Expected Timeout=30s, got %v", cfg.Cache.Timeout)
	}
	if cfg.Cache.MaxRetries != 5 {
		t.Errorf("Expected MaxRetries=5, got %d", cfg.Cache.MaxRetries)
	}
	if cfg.Cache.RetryDelay != 2*time.Second {
		t.Errorf("Expected RetryDelay=2s, got %v", cfg.Cache.RetryDelay)
	}
	if cfg.Cache.RateLimit != 20 {
		t.Errorf("Expected RateLimit=20, got %d", cfg.Cache.RateLimit)
	}
	if cfg.Cache.CacheTTL != 10*time.Minute {
		t.Errorf("Expected CacheTTL=10m, got %v", cfg.Cache.CacheTTL)
	}
	if cfg.Cache.MaxCacheAge != 2*time.Hour {
		t.Errorf("Expected MaxCacheAge=2h, got %v", cfg.Cache.MaxCacheAge)
	}
}

func TestLoadFromEnvWithMetricsConfig(t *testing.T) {
	os.Setenv("METRICS_SAMPLING_INTERVAL", "60s")
	os.Setenv("MAX_SAMPLES_PER_POD", "1000")
	os.Setenv("COMPLETED_POD_RETENTION", "2h")
	os.Setenv("DOWNSAMPLING_STRATEGY", "lttb")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("METRICS_SAMPLING_INTERVAL")
		os.Unsetenv("MAX_SAMPLES_PER_POD")
		os.Unsetenv("COMPLETED_POD_RETENTION")
		os.Unsetenv("DOWNSAMPLING_STRATEGY")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	if cfg.Metrics.SamplingInterval != "60s" {
		t.Errorf("Expected SamplingInterval=60s, got %s", cfg.Metrics.SamplingInterval)
	}
	if cfg.Metrics.MaxSamplesPerPod != 1000 {
		t.Errorf("Expected MaxSamplesPerPod=1000, got %d", cfg.Metrics.MaxSamplesPerPod)
	}
	if cfg.Metrics.PodRetention != "2h" {
		t.Errorf("Expected PodRetention=2h, got %s", cfg.Metrics.PodRetention)
	}
	if cfg.Metrics.DownsamplingStrategy != "lttb" {
		t.Errorf("Expected DownsamplingStrategy=lttb, got %s", cfg.Metrics.DownsamplingStrategy)
	}
}

func TestLoadFromEnvWithPowerConfig(t *testing.T) {
	os.Setenv("NODE_DEFAULT_IDLE_POWER", "150")
	os.Setenv("NODE_DEFAULT_MAX_POWER", "600")
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("NODE_DEFAULT_IDLE_POWER")
		os.Unsetenv("NODE_DEFAULT_MAX_POWER")
		os.Unsetenv("CARBON_ENABLED")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}
	if cfg.Power.DefaultIdlePower != 150.0 {
		t.Errorf("Expected DefaultIdlePower=150.0, got %f", cfg.Power.DefaultIdlePower)
	}
	if cfg.Power.DefaultMaxPower != 600.0 {
		t.Errorf("Expected DefaultMaxPower=600.0, got %f", cfg.Power.DefaultMaxPower)
	}
}

func TestLoadFromEnvWithInvalidPowerConfig(t *testing.T) {
	os.Setenv("NODE_DEFAULT_IDLE_POWER", "500")
	os.Setenv("NODE_DEFAULT_MAX_POWER", "200") // Less than idle
	os.Setenv("CARBON_ENABLED", "false")
	defer func() {
		os.Unsetenv("NODE_DEFAULT_IDLE_POWER")
		os.Unsetenv("NODE_DEFAULT_MAX_POWER")
		os.Unsetenv("CARBON_ENABLED")
	}()

	_, err := LoadFromEnv()
	if err == nil {
		t.Error("Expected error when max power is less than idle power")
	}
}

// testConfigHolder wraps a Config so Load's reflection branch can be exercised.
// It satisfies runtime.Object minimally; Load only reflects over its fields,
// so ObjectMeta is not needed.
type testConfigHolder struct {
	Config Config
}

func (testConfigHolder) DeepCopyObject() runtime.Object   { return nil }
func (testConfigHolder) GetObjectKind() schema.ObjectKind { return nil }

// testEmptyHolder is a runtime.Object with no Config field, used to exercise
// Load's "no field found" -> LoadFromEnv fallback branch.
type testEmptyHolder struct{}

func (testEmptyHolder) DeepCopyObject() runtime.Object   { return nil }
func (testEmptyHolder) GetObjectKind() schema.ObjectKind { return nil }

// TestLoadWithRuntimeObject exercises Load's reflection path: passing a
// non-nil runtime.Object whose struct carries a Config field, for both valid
// and invalid embedded configs, plus an object with no Config field (env
// fallback).
func TestLoadWithRuntimeObject(t *testing.T) {
	// Fallback env so the no-Config-field branch has something to load.
	os.Setenv("CARBON_ENABLED", "false")
	defer os.Unsetenv("CARBON_ENABLED")

	// 1) Valid embedded config returned by reference after Validate.
	valid := testConfigHolder{
		Config: Config{
			Carbon: CarbonConfig{Enabled: false},
			Power:  PowerConfig{DefaultIdlePower: 100, DefaultMaxPower: 400},
		},
	}
	cfg, err := Load(&valid)
	if err != nil {
		t.Fatalf("Load(valid holder) error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load(valid holder) returned nil config")
	}
	if cfg.Power.DefaultIdlePower != 100 || cfg.Power.DefaultMaxPower != 400 {
		t.Errorf("Expected embedded config values, got idle=%v max=%v",
			cfg.Power.DefaultIdlePower, cfg.Power.DefaultMaxPower)
	}

	// 2) Invalid embedded config -> error.
	invalid := testConfigHolder{
		Config: Config{
			Carbon: CarbonConfig{Enabled: false},
			Power:  PowerConfig{DefaultIdlePower: 100, DefaultMaxPower: 50},
		},
	}
	if _, err := Load(&invalid); err == nil {
		t.Error("Expected error for invalid embedded config")
	}

	// 3) Object with no Config field -> reflection finds nothing ->
	// LoadFromEnv fallback.
	empty := testEmptyHolder{}
	cfg, err = Load(&empty)
	if err != nil {
		t.Fatalf("Load(empty holder) error: %v", err)
	}
	if cfg == nil {
		t.Error("Load(empty holder) should fall back to env config")
	}
}
