package regions

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
