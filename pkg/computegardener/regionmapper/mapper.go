package regionmapper

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/regionmapper/regions"
)

// RegionMapper provides mapping functionality for cloud regions
type RegionMapper struct {
	config *Config

	// Kubernetes client for CloudInfo detection
	client kubernetes.Interface

	// Maps cloud provider region IDs to region info
	awsRegionMap   map[string]RegionInfo
	gcpRegionMap   map[string]RegionInfo
	azureRegionMap map[string]RegionInfo

	// Maps for quick lookup of Electricity Maps zones by provider and region
	electricityMapsZoneMap map[string]map[string]string // provider -> region -> zone
}

// NewRegionMapper creates a new mapper with default mappings
func NewRegionMapper(client kubernetes.Interface) *RegionMapper {
	mapper := &RegionMapper{
		config: &Config{
			DefaultElectricityMapsZone: "US-CAL-CISO",
			DefaultTimeZone:            "America/Los_Angeles",
			DefaultPUE:                 1.2,
		},
		client:                 client,
		electricityMapsZoneMap: make(map[string]map[string]string),
	}

	// Initialize with default region mappings
	mapper.initDefaultMappings()

	return mapper
}

// NewRegionMapperWithConfig creates a new mapper with the provided configuration
func NewRegionMapperWithConfig(client kubernetes.Interface, config *Config) *RegionMapper {
	mapper := NewRegionMapper(client)

	// Update with provided configuration
	if config != nil {
		mapper.config = config

		// Apply region overrides from config
		mapper.applyRegionOverrides(config.RegionOverrides)
	}

	return mapper
}

// initDefaultMappings initializes the mapper with default region mappings
func (m *RegionMapper) initDefaultMappings() {
	// Copy region data from the regions package
	m.awsRegionMap = convertRegionMap(regions.AWSRegionInfo)
	m.gcpRegionMap = convertRegionMap(regions.GCPRegionInfo)
	m.azureRegionMap = convertRegionMap(regions.AzureRegionInfo)

	// Initialize electricity maps zone lookup maps
	m.electricityMapsZoneMap[ProviderAWS] = make(map[string]string)
	m.electricityMapsZoneMap[ProviderGCP] = make(map[string]string)
	m.electricityMapsZoneMap[ProviderAzure] = make(map[string]string)

	// Populate lookup maps
	for region, info := range m.awsRegionMap {
		m.electricityMapsZoneMap[ProviderAWS][region] = info.ElectricityMapsZone
	}
	for region, info := range m.gcpRegionMap {
		m.electricityMapsZoneMap[ProviderGCP][region] = info.ElectricityMapsZone
	}
	for region, info := range m.azureRegionMap {
		m.electricityMapsZoneMap[ProviderAzure][region] = info.ElectricityMapsZone
	}

	klog.V(2).InfoS("Region mapper initialized",
		"awsRegions", len(m.awsRegionMap),
		"gcpRegions", len(m.gcpRegionMap),
		"azureRegions", len(m.azureRegionMap))
}

// convertRegionMap converts from the regions package format to our internal format
func convertRegionMap(sourceMap map[string]regions.RegionInfo) map[string]RegionInfo {
	result := make(map[string]RegionInfo, len(sourceMap))

	for k, v := range sourceMap {
		result[k] = RegionInfo{
			ElectricityMapsZone:  v.ElectricityMapsZone,
			TimeZone:             v.TimeZone,
			ISO:                  v.ISO,
			DefaultPUE:           v.DefaultPUE,
			Country:              v.Country,
			ElectricityPriceZone: v.ElectricityPriceZone,
			CloudProvider:        v.CloudProvider,
			CloudRegion:          v.CloudRegion,
			Metadata:             v.Metadata,
		}
	}

	return result
}

// applyRegionOverrides applies custom region mappings from configuration
func (m *RegionMapper) applyRegionOverrides(overrides []RegionOverride) {
	for _, override := range overrides {
		provider := strings.ToLower(override.Provider)
		region := override.Region

		// Create RegionInfo from override
		info := RegionInfo{
			ElectricityMapsZone: override.ElectricityMapsZone,
			TimeZone:            override.TimeZone,
			ISO:                 override.ISO,
			DefaultPUE:          override.PUE,
			CloudProvider:       provider,
			CloudRegion:         region,
		}

		// Apply override to the correct provider map
		switch provider {
		case ProviderAWS:
			m.awsRegionMap[region] = info
			m.electricityMapsZoneMap[ProviderAWS][region] = info.ElectricityMapsZone
		case ProviderGCP:
			m.gcpRegionMap[region] = info
			m.electricityMapsZoneMap[ProviderGCP][region] = info.ElectricityMapsZone
		case ProviderAzure:
			m.azureRegionMap[region] = info
			m.electricityMapsZoneMap[ProviderAzure][region] = info.ElectricityMapsZone
		default:
			klog.V(2).InfoS("Unknown provider in region override",
				"provider", provider,
				"region", region)
		}

		klog.V(2).InfoS("Applied region override",
			"provider", provider,
			"region", region,
			"electricityMapsZone", info.ElectricityMapsZone)
	}
}

// GetRegionInfo returns detailed information for a cloud provider and region
func (m *RegionMapper) GetRegionInfo(provider, region string) (*RegionInfo, bool) {
	provider = strings.ToLower(provider)

	var info RegionInfo
	var found bool

	// Look up in the appropriate provider map
	switch provider {
	case ProviderAWS:
		info, found = m.awsRegionMap[region]
	case ProviderGCP:
		info, found = m.gcpRegionMap[region]
	case ProviderAzure:
		info, found = m.azureRegionMap[region]
	default:
		return nil, false
	}

	if !found {
		// Try prefix matching for subregions
		switch provider {
		case ProviderAWS:
			for awsRegion, regionInfo := range m.awsRegionMap {
				if strings.HasPrefix(region, awsRegion) {
					info = regionInfo
					found = true
					break
				}
			}
		case ProviderGCP:
			for gcpRegion, regionInfo := range m.gcpRegionMap {
				if strings.HasPrefix(region, gcpRegion) {
					info = regionInfo
					found = true
					break
				}
			}
		case ProviderAzure:
			for azureRegion, regionInfo := range m.azureRegionMap {
				if strings.HasPrefix(region, azureRegion) {
					info = regionInfo
					found = true
					break
				}
			}
		}
	}

	if found {
		result := info // Make a copy to avoid modifying the map entry
		return &result, true
	}

	return nil, false
}

// GetRegionInfoForNode retrieves region information for a specific node
func (m *RegionMapper) GetRegionInfoForNode(node *v1.Node) (*RegionInfo, bool) {
	provider, region, ok := m.detectProviderAndRegionWithFallback(node)
	if !ok {
		return nil, false
	}

	return m.GetRegionInfo(provider, region)
}

// GetElectricityMapsZone returns the Electricity Maps zone for a cloud provider and region
func (m *RegionMapper) GetElectricityMapsZone(provider, region string) (string, bool) {
	provider = strings.ToLower(provider)

	// Check direct mapping first (most efficient)
	if providerMap, exists := m.electricityMapsZoneMap[provider]; exists {
		if zone, found := providerMap[region]; found {
			return zone, true
		}

		// Try prefix matching for subregions
		for regPrefix, zone := range providerMap {
			if strings.HasPrefix(region, regPrefix) {
				return zone, true
			}
		}
	}

	// Fall back to getting the full region info
	if info, found := m.GetRegionInfo(provider, region); found {
		return info.ElectricityMapsZone, true
	}

	return "", false
}

// GetElectricityMapsZoneForNode returns the Electricity Maps zone for a Kubernetes node
func (m *RegionMapper) GetElectricityMapsZoneForNode(node *v1.Node) (string, bool) {
	provider, region, ok := m.detectProviderAndRegionWithFallback(node)
	if !ok {
		return "", false
	}

	if zone, found := m.GetElectricityMapsZone(provider, region); found {
		klog.V(3).InfoS("Found Electricity Maps zone for node",
			"node", node.Name,
			"provider", provider,
			"region", region,
			"zone", zone)
		return zone, true
	}

	// If no mapping is found, use the default
	if m.config.DefaultElectricityMapsZone != "" {
		klog.V(3).InfoS("Using default Electricity Maps zone for node",
			"node", node.Name,
			"provider", provider,
			"region", region,
			"zone", m.config.DefaultElectricityMapsZone)
		return m.config.DefaultElectricityMapsZone, true
	}

	return "", false
}

// GetTimeZone returns the time zone for a cloud provider and region
func (m *RegionMapper) GetTimeZone(provider, region string) (string, bool) {
	if info, found := m.GetRegionInfo(provider, region); found {
		return info.TimeZone, true
	}

	// Fall back to default if configured
	if m.config.DefaultTimeZone != "" {
		return m.config.DefaultTimeZone, true
	}

	return "", false
}

// GetTimeZoneForNode returns the time zone for a Kubernetes node
func (m *RegionMapper) GetTimeZoneForNode(node *v1.Node) (string, bool) {
	provider, region, ok := m.detectProviderAndRegionWithFallback(node)
	if !ok {
		return "", false
	}

	return m.GetTimeZone(provider, region)
}

// GetISO returns the electricity ISO/market for a cloud provider and region
func (m *RegionMapper) GetISO(provider, region string) (string, bool) {
	if info, found := m.GetRegionInfo(provider, region); found {
		return info.ISO, true
	}

	return "", false
}

// GetISOForNode returns the electricity ISO/market for a Kubernetes node
func (m *RegionMapper) GetISOForNode(node *v1.Node) (string, bool) {
	provider, region, ok := m.detectProviderAndRegionWithFallback(node)
	if !ok {
		return "", false
	}

	return m.GetISO(provider, region)
}

// GetPUE returns the default PUE value for a cloud provider and region
func (m *RegionMapper) GetPUE(provider, region string) (float64, bool) {
	if info, found := m.GetRegionInfo(provider, region); found {
		return info.DefaultPUE, true
	}

	// Fall back to default if configured
	if m.config.DefaultPUE > 0 {
		return m.config.DefaultPUE, true
	}

	return 0, false
}

// GetPUEForNode returns the default PUE value for a Kubernetes node
func (m *RegionMapper) GetPUEForNode(node *v1.Node) (float64, bool) {
	provider, region, ok := m.detectProviderAndRegionWithFallback(node)
	if !ok {
		return 0, false
	}

	return m.GetPUE(provider, region)
}
