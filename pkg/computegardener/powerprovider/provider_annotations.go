package powerprovider

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// AnnotationPowerProvider provides power information based on node annotations
type AnnotationPowerProvider struct{}

// Make sure AnnotationPowerProvider implements PowerInfoProvider
var _ PowerInfoProvider = &AnnotationPowerProvider{}

func init() {
	// Register this provider
	RegisterProvider(&AnnotationPowerProvider{})
}

// IsAvailable checks if this provider can provide power data for the given node
func (p *AnnotationPowerProvider) IsAvailable(node *v1.Node) bool {
	// This provider is available if the node has CPU model annotation
	_, hasCPUModel := node.Annotations[common.AnnotationCPUModel]
	return hasCPUModel
}

// GetPriority returns the priority of this provider
func (p *AnnotationPowerProvider) GetPriority() int {
	return PRIORITY_ANNOTATION
}

// GetProviderType returns whether this provider uses measured or estimated data
func (p *AnnotationPowerProvider) GetProviderType() PowerDataType {
	return PowerDataTypeEstimated
}

// GetProviderName returns a human-readable identifier for the provider
func (p *AnnotationPowerProvider) GetProviderName() string {
	return "Annotation-Based"
}

// GetNodePowerInfo returns power information for a node
func (p *AnnotationPowerProvider) GetNodePowerInfo(node *v1.Node, hwConfig *config.HardwareProfiles) (*config.NodePower, error) {
	if hwConfig == nil {
		return nil, fmt.Errorf("hardware profiles not configured")
	}

	// Extract hardware info from annotations
	cpuModel, gpuModels := p.GetNodeHardwareInfo(node)
	if cpuModel == "" {
		return nil, fmt.Errorf("CPU model annotation missing or empty")
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

	// Handle GPUs
	if len(gpuModels) > 0 {
		// Sum power for all GPU models
		// Note: In a future version, we could get more sophisticated with handling heterogeneous GPUs
		for _, gpuModel := range gpuModels {
			if gpuModel != "" && gpuModel != "none" {
				if gpuProfile, exists := hwConfig.GPUProfiles[gpuModel]; exists {
					// Add power for this GPU type
					if nodePower.IdleGPUPower == 0 {
						// First GPU, just use its values directly
						nodePower.IdleGPUPower = gpuProfile.IdlePower
						nodePower.MaxGPUPower = gpuProfile.MaxPower
					} else {
						// Additional GPUs, add their power
						nodePower.IdleGPUPower += gpuProfile.IdlePower
						nodePower.MaxGPUPower += gpuProfile.MaxPower
					}
					klog.V(2).InfoS("Added GPU power", "node", node.Name, "model", gpuModel,
						"idlePower", gpuProfile.IdlePower, "maxPower", gpuProfile.MaxPower)
				} else {
					klog.V(2).InfoS("GPU model not found in profiles", "node", node.Name, "model", gpuModel)
				}
			}
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

	// Check for PUE annotation
	if pueStr, ok := node.Annotations[common.AnnotationPUE]; ok {
		if pue, err := strconv.ParseFloat(pueStr, 64); err == nil && pue >= 1.0 {
			nodePower.PUE = pue
		}
	} else {
		// Apply default PUE
		nodePower.PUE = common.DefaultPUE
	}

	// Check for GPU PUE annotation
	if gpuPueStr, ok := node.Annotations[common.AnnotationGPUPUE]; ok {
		if gpuPue, err := strconv.ParseFloat(gpuPueStr, 64); err == nil && gpuPue >= 1.0 {
			nodePower.GPUPUE = gpuPue
		}
	} else if nodePower.MaxGPUPower > 0 {
		// Apply default GPU PUE if we have GPUs
		nodePower.GPUPUE = common.DefaultGPUPUE
	}

	return nodePower, nil
}

// GetNodeHardwareInfo extracts CPU and GPU information from annotations
func (p *AnnotationPowerProvider) GetNodeHardwareInfo(node *v1.Node) (cpuModel string, gpuModels []string) {
	// Get CPU model
	if model, ok := node.Annotations[common.AnnotationCPUModel]; ok && model != "" {
		cpuModel = model
		klog.V(2).InfoS("Using CPU model from annotation", "node", node.Name, "model", cpuModel)
	}

	// Get GPU models - support comma-separated list for heterogeneous nodes
	if gpuModelStr, ok := node.Annotations[common.AnnotationGPUModel]; ok && gpuModelStr != "" && gpuModelStr != "none" {
		// Split by commas in case there are multiple models
		gpuModels = strings.Split(gpuModelStr, ",")
		for i := range gpuModels {
			gpuModels[i] = strings.TrimSpace(gpuModels[i])
		}
		klog.V(2).InfoS("Using GPU model(s) from annotation", "node", node.Name, "models", gpuModels)
	}

	return cpuModel, gpuModels
}
