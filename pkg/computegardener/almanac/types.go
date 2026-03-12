package almanac

import "time"

// Constraints represents binary/filter requirements that must be satisfied
// These are hard constraints, not weighted objectives
type Constraints struct {
	// Compliance requirements (e.g., "GDPR", "HIPAA", "SOC2")
	ComplianceRequired []string `json:"complianceRequired,omitempty"`

	// Data residency requirements (e.g., "EU", "US", "APAC")
	DataResidency []string `json:"dataResidency,omitempty"`

	// Maximum acceptable latency in milliseconds
	MaxLatencyMs int `json:"maxLatencyMs,omitempty"`

	// Minimum availability requirement (0.0 to 1.0, e.g., 0.99 for 99%)
	MinAvailability float64 `json:"minAvailability,omitempty"`

	// GPU type requirements (e.g., "A100", "H100", "V100")
	GPUTypes []string `json:"gpuTypes,omitempty"`

	// Allowed regions (empty means all allowed)
	AllowedRegions []string `json:"allowedRegions,omitempty"`

	// Blocked regions (takes precedence over AllowedRegions)
	BlockedRegions []string `json:"blockedRegions,omitempty"`
}

// IsEmpty returns true if no constraints are set
func (c *Constraints) IsEmpty() bool {
	if c == nil {
		return true
	}
	return len(c.ComplianceRequired) == 0 &&
		len(c.DataResidency) == 0 &&
		c.MaxLatencyMs == 0 &&
		c.MinAvailability == 0 &&
		len(c.GPUTypes) == 0 &&
		len(c.AllowedRegions) == 0 &&
		len(c.BlockedRegions) == 0
}

// ScoreComponents holds the individual signal scores
type ScoreComponents struct {
	CarbonScore  float64            `json:"carbonScore"`  // 0-1, higher = cleaner
	PriceScore   float64            `json:"priceScore"`   // 0-1, higher = cheaper
	BlendWeights map[string]float64 `json:"blendWeights"` // Weights used in blending
}

// RawSignals contains the raw data used for scoring
type RawSignals struct {
	CarbonIntensity     float64 `json:"carbonIntensityGCO2kWh"`
	SpotPrice           float64 `json:"spotPriceUSDHour,omitempty"`
	OnDemandPrice       float64 `json:"onDemandPriceUSDHour,omitempty"`
	OnDemandEstimated   bool    `json:"onDemandEstimated,omitempty"`   // True if on-demand was estimated from spot
	InstanceType        string  `json:"instanceType,omitempty"`
	InstanceTypeDefault bool    `json:"instanceTypeDefault,omitempty"` // True if default was used
}

// ScoreResult is the blended output
type ScoreResult struct {
	Zone              string          `json:"zone"`
	OptimizationScore float64         `json:"optimizationScore"` // 0-1, higher = run now
	Components        ScoreComponents `json:"components"`
	RawValues         RawSignals      `json:"rawValues"`
	Recommendation    Recommendation  `json:"recommendation"` // PROCEED|WAIT|OPTIMAL
	Timestamp         time.Time       `json:"timestamp"`
}

// Recommendation indicates the suggested scheduling action
type Recommendation string

// Recommendation constants
const (
	RecommendProceed Recommendation = "PROCEED" // Score > 0.7
	RecommendWait    Recommendation = "WAIT"    // Score < 0.4
	RecommendOptimal Recommendation = "OPTIMAL" // Score > 0.85
)

// ShouldProceed returns true if the recommendation is PROCEED or OPTIMAL
func (sr *ScoreResult) ShouldProceed() bool {
	return sr.Recommendation == RecommendProceed || sr.Recommendation == RecommendOptimal
}

// IsOptimal returns true if the recommendation is OPTIMAL
func (sr *ScoreResult) IsOptimal() bool {
	return sr.Recommendation == RecommendOptimal
}

// ShouldWait returns true if the recommendation is WAIT
func (sr *ScoreResult) ShouldWait() bool {
	return sr.Recommendation == RecommendWait
}

// ScoreRequest represents a request to the scoring API
type ScoreRequest struct {
	// Zone is the electricity region/zone identifier (e.g., "US-CAL-CISO")
	// Alternative: use Provider + Region for cloud-native identifiers
	Zone string `json:"zone,omitempty"`

	// Provider is the cloud provider (e.g., "aws", "gcp", "azure")
	Provider string `json:"provider,omitempty"`

	// Region is the cloud provider region (e.g., "us-west-2", "europe-west1")
	Region string `json:"region,omitempty"`

	// InstanceType is the cloud instance type for pricing (e.g., "g5.xlarge")
	InstanceType string `json:"instanceType,omitempty"`

	// Weights defines the optimization objectives (must sum to 1.0)
	// Example: {"carbon": 0.6, "price": 0.4}
	Weights map[string]float64 `json:"weights"`

	// Constraints defines hard requirements (optional, for future use)
	Constraints *Constraints `json:"constraints,omitempty"`
}
