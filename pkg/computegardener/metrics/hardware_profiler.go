package metrics

import (
	"fmt"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// HardwareProfiler provides methods to detect and compute power profiles for nodes
type HardwareProfiler struct {
	config       *config.HardwareProfiles
	profileCache map[string]*config.NodePower // Cache of node UID -> power profile
	cacheMutex   sync.RWMutex                 // Mutex for thread-safe cache access
}

// NewHardwareProfiler creates a new hardware profiler with the given configuration
func NewHardwareProfiler(profiles *config.HardwareProfiles) *HardwareProfiler {
	return &HardwareProfiler{
		config:       profiles,
		profileCache: make(map[string]*config.NodePower),
	}
}

// DetectNodePowerProfile determines the power profile for a node
func (hp *HardwareProfiler) DetectNodePowerProfile(node *v1.Node) (*config.NodePower, error) {
	if hp.config == nil {
		return nil, fmt.Errorf("hardware profiles not configured")
	}

	// Check if we already have a cached profile for this node
	nodeUID := string(node.UID)
	hp.cacheMutex.RLock()
	if cachedProfile, exists := hp.profileCache[nodeUID]; exists {
		hp.cacheMutex.RUnlock()
		klog.V(2).InfoS("Using cached power profile for node", "node", node.Name)
		return cachedProfile, nil
	}
	hp.cacheMutex.RUnlock()

	// If not in cache, detect the hardware profile
	var nodePower *config.NodePower

	// Strategy 1: Check cloud provider instance type
	if provider, instanceType, ok := getCloudInstanceInfo(node); ok {
		if hwComponents, exists := hp.lookupCloudInstance(provider, instanceType); exists {
			// Found a cloud instance mapping, compute the power profile from its components
			nodePower = hp.computePowerFromComponents(hwComponents)
			if nodePower != nil {
				klog.V(2).InfoS("Detected node power profile from cloud instance type", 
					"node", node.Name, "provider", provider, "instanceType", instanceType)
				
				// Add to cache and return
				hp.cacheNodeProfile(nodeUID, nodePower)
				return nodePower, nil
			}
		}
	}
	
	// Strategy 2: Direct hardware inspection from node annotations or properties
	cpuModel, gpuModel := hp.detectNodeHardwareInfoFromSystem(node)

	if cpuModel != "" {
		if cpuProfile, exists := hp.config.CPUProfiles[cpuModel]; exists {
			// Create a basic power profile with CPU
			nodePower = &config.NodePower{
				IdlePower: cpuProfile.IdlePower,
				MaxPower:  cpuProfile.MaxPower,
			}

			// Add GPU power if available and in our profiles
			if gpuModel != "" && gpuModel != "none" {
				if gpuProfile, exists := hp.config.GPUProfiles[gpuModel]; exists {
					nodePower.IdleGPUPower = gpuProfile.IdlePower
					nodePower.MaxGPUPower = gpuProfile.MaxPower
				}
			}

			// Add memory power estimate based on node's memory capacity
			memBytes := node.Status.Capacity.Memory().Value()
			if memBytes > 0 {
				// Add a basic memory power estimate
				// This is simplified; ideally we'd determine memory type
				memGB := float64(memBytes) / (1024 * 1024 * 1024)
				nodePower.IdlePower += estimateMemoryPower(memGB, false) // false = idle
				nodePower.MaxPower += estimateMemoryPower(memGB, true)   // true = max
			}

			// Add to cache and return
			hp.cacheNodeProfile(nodeUID, nodePower)
			return nodePower, nil
		}
	}

	// Fall back to default values
	return nil, fmt.Errorf("hardware profile not found for node %s", node.Name)
}

// lookupCloudInstance finds hardware components for a cloud instance
func (hp *HardwareProfiler) lookupCloudInstance(provider, instanceType string) (*config.HardwareComponents, bool) {
	// Normalize provider name
	provider = strings.ToLower(provider)

	// Look up the instance type in the mapping
	if providerMap, exists := hp.config.CloudInstanceMapping[provider]; exists {
		if components, exists := providerMap[instanceType]; exists {
			return &components, true
		}
	}

	return nil, false
}

// cacheNodeProfile stores a node's power profile in the cache
func (hp *HardwareProfiler) cacheNodeProfile(nodeUID string, profile *config.NodePower) {
	hp.cacheMutex.Lock()
	defer hp.cacheMutex.Unlock()
	hp.profileCache[nodeUID] = profile
}

// detectNodeHardwareInfoFromSystem determines hardware components from the node
// Uses annotations if available, otherwise checks and caches details at runtime
func (hp *HardwareProfiler) detectNodeHardwareInfoFromSystem(node *v1.Node) (cpuModel string, gpuModel string) {
	// Extract CPU info from node annotations or make educated guesses based on capacity
	// For Kubernetes, node annotations provide the most accurate identification

	// Check if node has CPU information in its annotations
	if model, ok := node.Annotations["compute-gardener-scheduler.kubernetes.io/cpu-model"]; ok {
		cpuModel = model
	} else if arch, ok := node.Labels["kubernetes.io/arch"]; ok {
		// Make an educated guess based on architecture and capacity
		cpuCores := node.Status.Capacity.Cpu().Value()
		switch arch {
		case "amd64":
			// These are placeholder mappings, a real implementation would be more sophisticated
			if cpuCores >= 32 {
				cpuModel = "AMD EPYC 7763" // High core count suggests EPYC
			} else if cpuCores >= 16 {
				cpuModel = "AMD EPYC 7571"
			} else {
				cpuModel = "AMD EPYC 7R13"
			}
		case "arm64":
			cpuModel = "ARM Neoverse N1"
		default:
			// Default to a common Intel CPU model based on core count
			if cpuCores >= 32 {
				cpuModel = "Intel(R) Xeon(R) Platinum 8168"
			} else if cpuCores >= 16 {
				cpuModel = "Intel(R) Xeon(R) Platinum 8124M"
			} else {
				cpuModel = "Intel(R) Xeon(R) E5-2686 v4"
			}
		}
	}

	// For GPUs, check if node has NVIDIA GPUs allocated
	// First check node annotations for accurate GPU information
	if model, ok := node.Annotations["compute-gardener-scheduler.kubernetes.io/gpu-model"]; ok {
		// If annotation explicitly says "none", treat as no GPU
		if model != "none" {
			gpuModel = model
		}
	} else if gpuCount, ok := node.Status.Capacity["nvidia.com/gpu"]; ok && gpuCount.Value() > 0 {
		// If GPU exists but no annotation, determine from node characteristics
		// In production, consider adding a daemon that reports actual GPU model
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

// estimateMemoryPower provides a simple estimate of memory power consumption based on capacity
func estimateMemoryPower(memoryGB float64, isMax bool) float64 {
	if isMax {
		// At maximum load, memory uses approximately 0.3-0.4W per GB plus base
		return 1.0 + (0.35 * memoryGB) // Base + per GB estimate
	} else {
		// At idle, memory uses approximately 0.1-0.15W per GB plus base
		return 1.0 + (0.125 * memoryGB) // Base + per GB estimate
	}
}

// getCloudInstanceInfo extracts cloud provider and instance type from node labels
func getCloudInstanceInfo(node *v1.Node) (string, string, bool) {
	// Check for instance type label
	instanceType, hasInstanceType := node.Labels["node.kubernetes.io/instance-type"]
	if !hasInstanceType {
		return "", "", false
	}

	// Determine provider
	providerID := node.Spec.ProviderID
	if providerID == "" {
		// Use heuristics to guess provider from instance type prefix
		if strings.HasPrefix(instanceType, "m5.") || strings.HasPrefix(instanceType, "c5.") || strings.HasPrefix(instanceType, "p3.") {
			return "aws", instanceType, true
		} else if strings.HasPrefix(instanceType, "n2-") || strings.HasPrefix(instanceType, "c2-") || strings.HasPrefix(instanceType, "a2-") {
			return "gcp", instanceType, true
		} else if strings.HasPrefix(instanceType, "Standard_") {
			return "azure", instanceType, true
		}
		return "", "", false
	}

	// Extract provider from the provider ID
	// Format: <provider>://<path>
	parts := strings.SplitN(providerID, "://", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	provider := parts[0]
	switch provider {
	case "aws":
		return "aws", instanceType, true
	case "gce":
		return "gcp", instanceType, true
	case "azure":
		return "azure", instanceType, true
	default:
		return provider, instanceType, true
	}
}

// ClearCache clears the hardware profile cache
func (hp *HardwareProfiler) ClearCache() {
	hp.cacheMutex.Lock()
	defer hp.cacheMutex.Unlock()
	hp.profileCache = make(map[string]*config.NodePower)
	klog.V(2).InfoS("Hardware profile cache cleared")
}

// computePowerFromComponents calculates a power profile from hardware components
func (hp *HardwareProfiler) computePowerFromComponents(hwComponents *config.HardwareComponents) *config.NodePower {
	// Check if we have required components
	if hwComponents.CPUModel == "" {
		return nil
	}
	
	// Look up CPU power profile
	cpuProfile, exists := hp.config.CPUProfiles[hwComponents.CPUModel]
	if !exists {
		return nil
	}
	
	// Create basic power profile with CPU
	nodePower := &config.NodePower{
		IdlePower: cpuProfile.IdlePower,
		MaxPower:  cpuProfile.MaxPower,
	}
	
	// Add GPU power if available
	if hwComponents.GPUModel != "" {
		if gpuProfile, exists := hp.config.GPUProfiles[hwComponents.GPUModel]; exists {
			nodePower.IdleGPUPower = gpuProfile.IdlePower
			nodePower.MaxGPUPower = gpuProfile.MaxPower
		}
	}
	
	// Add memory power if memory information exists
	if hwComponents.MemoryType != "" && hwComponents.TotalMemory > 0 {
		memGB := float64(hwComponents.TotalMemory) / 1024  // Convert from MB to GB
		
		// If we have a specific memory profile, use it
		if memProfile, exists := hp.config.MemProfiles[hwComponents.MemoryType]; exists {
			// Calculate memory power using the profile
			nodePower.IdlePower += memProfile.BaseIdlePower + (memProfile.IdlePowerPerGB * memGB)
			nodePower.MaxPower += memProfile.BaseIdlePower + (memProfile.MaxPowerPerGB * memGB)
		} else {
			// Use general memory estimation
			nodePower.IdlePower += estimateMemoryPower(memGB, false)
			nodePower.MaxPower += estimateMemoryPower(memGB, true)
		}
	}
	
	return nodePower
}

func (hp *HardwareProfiler) RefreshNodeCache(node *v1.Node) {
	nodeUID := string(node.UID)

	// Remove existing entry if any
	hp.cacheMutex.Lock()
	delete(hp.profileCache, nodeUID)
	hp.cacheMutex.Unlock()

	// Attempt to detect and cache a new profile
	if profile, err := hp.DetectNodePowerProfile(node); err == nil {
		hp.cacheNodeProfile(nodeUID, profile)
		klog.V(2).InfoS("Refreshed hardware profile for node", "node", node.Name)
	} else {
		klog.V(2).InfoS("Failed to refresh hardware profile for node", "node", node.Name, "error", err)
	}
}
