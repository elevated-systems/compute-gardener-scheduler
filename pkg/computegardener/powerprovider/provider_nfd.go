package powerprovider

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// NFDPowerProvider provides power information based on Node Feature Discovery labels
type NFDPowerProvider struct{}

// Make sure NFDPowerProvider implements PowerInfoProvider
var _ PowerInfoProvider = &NFDPowerProvider{}

func init() {
	// Register this provider
	RegisterProvider(&NFDPowerProvider{})
}

// IsAvailable checks if this provider can provide power data for the given node
func (p *NFDPowerProvider) IsAvailable(node *v1.Node) bool {
	// This provider is available if the node has NFD CPU model labels
	_, hasFamily := node.Labels[common.NFDLabelCPUModelFamily]
	_, hasModelID := node.Labels[common.NFDLabelCPUModelID]
	_, hasVendorID := node.Labels[common.NFDLabelCPUModelVendorID]

	return hasFamily && hasModelID && hasVendorID
}

// GetPriority returns the priority of this provider
func (p *NFDPowerProvider) GetPriority() int {
	return PRIORITY_NFD
}

// GetProviderType returns whether this provider uses measured or estimated data
func (p *NFDPowerProvider) GetProviderType() PowerDataType {
	return PowerDataTypeEstimated
}

// GetProviderName returns a human-readable identifier for the provider
func (p *NFDPowerProvider) GetProviderName() string {
	return "NFD-Based"
}

func (p *NFDPowerProvider) GetNodeHardwareInfo(node *v1.Node) (string, []string) {
	// NOT IMPLEMENTED
	return "", []string{}
}

// GetNodePowerInfo returns power information for a node
func (p *NFDPowerProvider) GetNodePowerInfo(node *v1.Node, hwConfig *config.HardwareProfiles) (*config.NodePower, error) {
	if hwConfig == nil {
		return nil, fmt.Errorf("hardware profiles not configured")
	}

	// Extract hardware info
	cpuModel, gpuModel := p.detectNodeHardwareInfo(node, hwConfig)

	if cpuModel == "" {
		return nil, fmt.Errorf("could not determine CPU model from NFD labels")
	}

	// Look up CPU power profile
	cpuProfile, exists := hwConfig.CPUProfiles[cpuModel]
	if !exists {
		return nil, fmt.Errorf("no power profile found for CPU model: %s", cpuModel)
	}

	// Create a basic power profile with CPU
	nodePower := &config.NodePower{
		IdlePower: cpuProfile.IdlePower,
		MaxPower:  cpuProfile.MaxPower,
	}

	// Add GPU power if available and in our profiles
	if gpuModel != "" && gpuModel != "none" {
		if gpuProfile, exists := hwConfig.GPUProfiles[gpuModel]; exists {
			nodePower.IdleGPUPower = gpuProfile.IdlePower
			nodePower.MaxGPUPower = gpuProfile.MaxPower
		}
	}

	// Add memory power estimate based on node's memory capacity
	memBytes := node.Status.Capacity.Memory().Value()
	if memBytes > 0 {
		// Add a basic memory power estimate
		memGB := float64(memBytes) / (1024 * 1024 * 1024)
		nodePower.IdlePower += estimateMemoryPower(memGB, false) // false = idle
		nodePower.MaxPower += estimateMemoryPower(memGB, true)   // true = max
	}

	// Apply default PUE and GPU PUE values
	nodePower.PUE = common.DefaultPUE
	if nodePower.MaxGPUPower > 0 {
		nodePower.GPUPUE = common.DefaultGPUPUE
	}

	return nodePower, nil
}

// detectNodeHardwareInfo determines hardware components from the node's NFD labels
func (p *NFDPowerProvider) detectNodeHardwareInfo(node *v1.Node, hwConfig *config.HardwareProfiles) (cpuModel string, gpuModel string) {
	// Try to use NFD CPU labels to identify the CPU model
	cpuModel = p.identifyCPUModelFromNFDLabels(node, hwConfig)
	if cpuModel != "" {
		klog.V(2).InfoS("Identified CPU model from NFD labels", "node", node.Name, "model", cpuModel)
	} else {
		// Provide a generic fallback based on architecture and core count
		arch := node.Labels["kubernetes.io/arch"]
		cpuCores := node.Status.Capacity.Cpu().Value()

		// Use very generic model names that indicate architecture but not specific model
		switch arch {
		case "amd64":
			cpuModel = fmt.Sprintf("Generic x86_64 (%d cores)", cpuCores)
			klog.V(2).InfoS("Using generic CPU model from NFD provider", "node", node.Name, "model", cpuModel)
		case "arm64":
			cpuModel = fmt.Sprintf("Generic ARM64 (%d cores)", cpuCores)
			klog.V(2).InfoS("Using generic CPU model from NFD provider", "node", node.Name, "model", cpuModel)
		default:
			cpuModel = fmt.Sprintf("Unknown architecture (%d cores)", cpuCores)
			klog.V(2).InfoS("Using generic CPU model from NFD provider", "node", node.Name, "model", cpuModel)
		}
	}

	// For GPUs, check if node has NVIDIA GPUs allocated
	// First check NFD labels for accurate GPU information
	if model, ok := node.Labels[common.NvidiaLabelGPUProduct]; ok {
		// We have an NVIDIA GPU model from NFD labels
		gpuModel = model
	} else if gpuCount, ok := node.Status.Capacity[common.NvidiaLabelBase]; ok && gpuCount.Value() > 0 {
		// If GPU exists but no annotation, determine from node characteristics
		if strings.Contains(node.Name, "gpu") ||
			strings.Contains(node.Name, "p3") ||
			strings.Contains(node.Name, "g4") {
			gpuModel = "NVIDIA V100" // Common in AWS p3 instances
		} else if strings.Contains(node.Name, "a10") {
			gpuModel = "NVIDIA A10G"
		} else {
			gpuModel = "NVIDIA T4" // Common default
		}
	}

	return cpuModel, gpuModel
}

// getCPUModelKey creates a lookup key for our CPU mapping table based on NFD labels
func (p *NFDPowerProvider) getCPUModelKey(node *v1.Node) string {
	family, hasFamily := node.Labels[common.NFDLabelCPUModelFamily]
	modelID, hasModelID := node.Labels[common.NFDLabelCPUModelID]

	if !hasFamily || !hasModelID {
		return ""
	}

	// The base key uses the NFD CPU model family and ID
	return fmt.Sprintf("%s-%s", family, modelID)
}

// identifyCPUModelFromNFDLabels tries to identify the CPU model from NFD labels
func (p *NFDPowerProvider) identifyCPUModelFromNFDLabels(node *v1.Node, hwConfig *config.HardwareProfiles) string {
	// Extract the NFD CPU model information - family, model ID, and vendor
	family, hasFamily := node.Labels[common.NFDLabelCPUModelFamily]
	modelID, hasModelID := node.Labels[common.NFDLabelCPUModelID]
	vendorID, hasVendorID := node.Labels[common.NFDLabelCPUModelVendorID]

	if !hasFamily || !hasModelID || !hasVendorID {
		klog.V(2).InfoS("Missing NFD CPU information",
			"node", node.Name,
			"hasFamily", hasFamily,
			"hasModelID", hasModelID,
			"hasVendorID", hasVendorID)
		return ""
	}

	// Log the NFD information we found
	klog.V(2).InfoS("Found NFD CPU information",
		"node", node.Name,
		"vendorID", vendorID,
		"family", family,
		"modelID", modelID)

	// Get the mapping key for CPU identification
	baseKey := p.getCPUModelKey(node)
	if baseKey == "" {
		return ""
	}

	// Use hardware config from the method parameters
	if hwConfig != nil && hwConfig.CPUModelMappings != nil {
		// Look for known CPU models in the hardware profiles
		if vendorMapping, ok := hwConfig.CPUModelMappings[vendorID]; ok {
			// Try direct model lookup
			if cpuModel, ok := vendorMapping[baseKey]; ok {
				return cpuModel
			}

			// Try family-only fallback (least specific)
			if cpuModel, ok := vendorMapping[family]; ok {
				return cpuModel
			}
		}
	}

	// If we can't determine from NFD labels, construct a generic model name
	cpuCores := node.Status.Capacity.Cpu().Value()
	return fmt.Sprintf("%s CPU Family %s Model %s (%d cores)",
		vendorID, family, modelID, cpuCores)
}
