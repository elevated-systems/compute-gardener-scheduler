package powerprovider

import (
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// PowerDataType indicates whether power data is based on real-time measurements or estimations
type PowerDataType string

const (
	// PowerDataTypeMeasured indicates the power data comes from actual measurements
	PowerDataTypeMeasured PowerDataType = "Measured"

	// PowerDataTypeEstimated indicates the power data is estimated based on device characteristics
	PowerDataTypeEstimated PowerDataType = "Estimated"
)

// PowerInfoProvider defines the interface for getting power information from a node
type PowerInfoProvider interface {
	// IsAvailable checks if this provider can provide power data for the given node
	IsAvailable(node *v1.Node) bool

	// GetPriority returns the priority of this provider (higher = more preferred)
	GetPriority() int

	// GetNodePowerInfo returns power information for a node
	GetNodePowerInfo(node *v1.Node, hwConfig *config.HardwareProfiles) (*config.NodePower, error)

	// GetProviderType returns whether this provider uses measured or estimated data
	GetProviderType() PowerDataType

	// GetProviderName returns a human-readable identifier for the provider
	GetProviderName() string
}

// Registry is a registry of all power providers
var Registry []PowerInfoProvider

// RegisterProvider adds a provider to the registry
func RegisterProvider(provider PowerInfoProvider) {
	Registry = append(Registry, provider)
	klog.V(4).Info("Registered power provider", "provider", provider.GetProviderName())
}

// GetAvailableProviders returns all available providers for a node in priority order
func GetAvailableProviders(node *v1.Node) []PowerInfoProvider {
	var availableProviders []PowerInfoProvider
	for _, provider := range Registry {
		if provider.IsAvailable(node) {
			availableProviders = append(availableProviders, provider)
		}
	}

	// Sort by priority (highest first)
	sort.Slice(availableProviders, func(i, j int) bool {
		return availableProviders[i].GetPriority() > availableProviders[j].GetPriority()
	})

	return availableProviders
}

// GetBestProvider returns the highest priority provider available for the node
func GetBestProvider(node *v1.Node) (PowerInfoProvider, bool) {
	providers := GetAvailableProviders(node)
	if len(providers) > 0 {
		return providers[0], true
	}
	return nil, false
}
