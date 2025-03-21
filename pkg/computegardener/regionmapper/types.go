// Package regionmapper provides mapping functionality for cloud regions
// to various relevant grid and location data like electricity markets
// and carbon intensity zones.
package regionmapper

// RegionInfo contains metadata about a cloud region
type RegionInfo struct {
	// ElectricityMapsZone is the corresponding zone ID in Electricity Maps API
	ElectricityMapsZone string

	// TimeZone for this region (useful for TOU pricing)
	TimeZone string

	// ISO refers to the electricity Independent System Operator
	ISO string

	// DefaultPUE is the typical PUE value for this region
	DefaultPUE float64

	// Country code for this region
	Country string

	// ElectricityPriceZone is the zone used for pricing data (if different from ElectricityMapsZone)
	ElectricityPriceZone string

	// CloudProvider is the name of the cloud provider (aws, gcp, azure)
	CloudProvider string

	// CloudRegion is the region identifier in the cloud provider
	CloudRegion string

	// Additional metadata as needed
	Metadata map[string]string
}

// CloudProviderInfo contains information about a cloud provider
type CloudProviderInfo struct {
	// Name is the standardized name of the provider (aws, gcp, azure)
	Name string

	// ProviderIDPrefix is the prefix used in node.spec.providerID (e.g., "aws://")
	ProviderIDPrefix string

	// LabelSelectors are label keys that can be used to identify this provider
	LabelSelectors []string

	// RegionLabelKey is the label key containing the region (if not using standard topology.kubernetes.io/region)
	RegionLabelKey string
}

// Config contains configuration for the region mapper
type Config struct {
	// RegionOverrides allows overriding the default mappings
	RegionOverrides []RegionOverride `yaml:"regionOverrides"`

	// DefaultElectricityMapsZone is the fallback zone when mapping cannot be determined
	DefaultElectricityMapsZone string `yaml:"defaultElectricityMapsZone"`

	// DefaultTimeZone is the fallback timezone when mapping cannot be determined
	DefaultTimeZone string `yaml:"defaultTimeZone"`

	// DefaultPUE is the fallback PUE when mapping cannot be determined
	DefaultPUE float64 `yaml:"defaultPUE"`
}

// RegionOverride defines a custom mapping for a specific cloud region
type RegionOverride struct {
	// Provider is the cloud provider (aws, gcp, azure)
	Provider string `yaml:"provider"`

	// Region is the cloud region identifier
	Region string `yaml:"region"`

	// ElectricityMapsZone is the Electricity Maps zone to use
	ElectricityMapsZone string `yaml:"electricityMapsZone"`

	// TimeZone is the time zone to use
	TimeZone string `yaml:"timeZone"`

	// ISO is the electricity market/ISO to use
	ISO string `yaml:"iso"`

	// PUE is the Power Usage Effectiveness value for this region
	PUE float64 `yaml:"pue"`
}
