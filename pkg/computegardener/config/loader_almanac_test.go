package config

import "testing"

// TestLoadFromEnvAlmanacFull verifies every ALMANAC_* env var maps to the
// matching AlmanacConfig field, including the cloud-default fields added for
// node-label fallback.
func TestLoadFromEnvAlmanacFull(t *testing.T) {
	// Carbon defaults to enabled and requires an EM API key to validate.
	t.Setenv("CARBON_ENABLED", "false")
	t.Setenv("ALMANAC_ENABLED", "true")
	t.Setenv("ALMANAC_URL", "http://almanac:8080")
	t.Setenv("ALMANAC_TIMEOUT", "15s")
	t.Setenv("ALMANAC_DEFAULT_SCORE_THRESHOLD", "0.75")
	t.Setenv("ALMANAC_DEFAULT_CARBON_WEIGHT", "0.4")
	t.Setenv("ALMANAC_DEFAULT_PRICE_WEIGHT", "0.6")
	t.Setenv("ALMANAC_FAIL_OPEN", "false")
	t.Setenv("ALMANAC_DEFAULT_PROVIDER", "aws")
	t.Setenv("ALMANAC_DEFAULT_REGION", "us-west-2")
	t.Setenv("ALMANAC_DEFAULT_INSTANCE_TYPE", "m5.large")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if !cfg.Almanac.Enabled {
		t.Error("Enabled = false, want true")
	}
	if cfg.Almanac.URL != "http://almanac:8080" {
		t.Errorf("URL = %q, want %q", cfg.Almanac.URL, "http://almanac:8080")
	}
	if cfg.Almanac.Timeout != "15s" {
		t.Errorf("Timeout = %q, want %q", cfg.Almanac.Timeout, "15s")
	}
	if cfg.Almanac.DefaultScoreThreshold != 0.75 {
		t.Errorf("DefaultScoreThreshold = %v, want 0.75", cfg.Almanac.DefaultScoreThreshold)
	}
	if cfg.Almanac.DefaultCarbonWeight != 0.4 || cfg.Almanac.DefaultPriceWeight != 0.6 {
		t.Errorf("weights = (%v, %v), want (0.4, 0.6)", cfg.Almanac.DefaultCarbonWeight, cfg.Almanac.DefaultPriceWeight)
	}
	if cfg.Almanac.FailOpen {
		t.Error("FailOpen = true, want false")
	}
	if cfg.Almanac.DefaultProvider != "aws" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.Almanac.DefaultProvider, "aws")
	}
	if cfg.Almanac.DefaultRegion != "us-west-2" {
		t.Errorf("DefaultRegion = %q, want %q", cfg.Almanac.DefaultRegion, "us-west-2")
	}
	if cfg.Almanac.DefaultInstanceType != "m5.large" {
		t.Errorf("DefaultInstanceType = %q, want %q", cfg.Almanac.DefaultInstanceType, "m5.large")
	}
}

// TestLoadFromEnvAlmanacDefaults verifies the safe defaults when no ALMANAC_*
// vars are set: feature off, fail-open, 0.7 threshold, 0.6/0.4 weights, 10s timeout.
func TestLoadFromEnvAlmanacDefaults(t *testing.T) {
	t.Setenv("CARBON_ENABLED", "false")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.Almanac.Enabled {
		t.Error("Enabled = true, want false (opt-in feature)")
	}
	if !cfg.Almanac.FailOpen {
		t.Error("FailOpen = false, want true (safe default)")
	}
	if cfg.Almanac.DefaultScoreThreshold != 0.7 {
		t.Errorf("DefaultScoreThreshold = %v, want 0.7", cfg.Almanac.DefaultScoreThreshold)
	}
	if cfg.Almanac.DefaultCarbonWeight != 0.6 || cfg.Almanac.DefaultPriceWeight != 0.4 {
		t.Errorf("default weights = (%v, %v), want (0.6, 0.4)", cfg.Almanac.DefaultCarbonWeight, cfg.Almanac.DefaultPriceWeight)
	}
	if cfg.Almanac.Timeout != "10s" {
		t.Errorf("Timeout = %q, want %q", cfg.Almanac.Timeout, "10s")
	}
}

// TestLoadFromEnvAlmanacInvalidTimeout verifies a malformed ALMANAC_TIMEOUT is
// rejected by validation (LoadFromEnv runs Validate).
func TestLoadFromEnvAlmanacInvalidTimeout(t *testing.T) {
	t.Setenv("CARBON_ENABLED", "false")
	t.Setenv("ALMANAC_ENABLED", "true")
	t.Setenv("ALMANAC_URL", "http://almanac:8080")
	t.Setenv("ALMANAC_TIMEOUT", "not-a-duration")

	if _, err := LoadFromEnv(); err == nil {
		t.Error("LoadFromEnv() = nil error, want error for invalid ALMANAC_TIMEOUT")
	}
}
