package metrics

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics/powerprovider"
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

	// Check for PUE annotation on the node
	nodePUE := 0.0 // Will use default if not set
	if pueStr, ok := node.Annotations[common.AnnotationPUE]; ok {
		if pue, err := strconv.ParseFloat(pueStr, 64); err == nil && pue >= 1.0 {
			nodePUE = pue
			klog.V(2).InfoS("Using node-specific PUE from annotation", "node", node.Name, "pue", pue)
		} else {
			klog.V(2).InfoS("Invalid PUE annotation, will use default", "node", node.Name, "value", pueStr)
		}
	}

	// Check for GPU PUE annotation on the node
	nodeGPUPUE := 0.0 // Will use default if not set
	if gpuPueStr, ok := node.Annotations[common.AnnotationGPUPUE]; ok {
		if gpuPue, err := strconv.ParseFloat(gpuPueStr, 64); err == nil && gpuPue >= 1.0 {
			nodeGPUPUE = gpuPue
			klog.V(2).InfoS("Using node-specific GPU PUE from annotation", "node", node.Name, "gpuPue", gpuPue)
		} else {
			klog.V(2).InfoS("Invalid GPU PUE annotation, will use default", "node", node.Name, "value", gpuPueStr)
		}
	}

	// Strategy 1: Check cloud provider instance type
	if provider, instanceType, ok := getCloudInstanceInfo(node); ok {
		if hwComponents, exists := hp.lookupCloudInstance(provider, instanceType); exists {
			// Found a cloud instance mapping, compute the power profile from its components
			nodePower = hp.computePowerFromComponents(hwComponents)
			if nodePower != nil {
				// Apply PUE if specified
				if nodePUE > 0 {
					nodePower.PUE = nodePUE
				}

				// Apply GPU PUE if specified
				if nodeGPUPUE > 0 {
					nodePower.GPUPUE = nodeGPUPUE
				}

				klog.V(2).InfoS("Detected node power profile from cloud instance type",
					"node", node.Name, "provider", provider, "instanceType", instanceType)

				// Add to cache and return
				hp.cacheNodeProfile(nodeUID, nodePower)
				return nodePower, nil
			}
		}
	}

	// Strategy 2: Direct hardware inspection from node annotations or properties
	cpuModel, gpuModels := hp.detectNodeHardwareInfoFromSystem(node)

	if cpuModel != "" {
		if cpuProfile, exists := hp.config.CPUProfiles[cpuModel]; exists {
			// Create a basic power profile with CPU
			nodePower = &config.NodePower{
				IdlePower: cpuProfile.IdlePower,
				MaxPower:  cpuProfile.MaxPower,
			}

			// Add GPU power if available and in our profiles
			//TODO: confirm this and ensure handles multiple GPUs
			if len(gpuModels) > 0 {
				if gpuProfile, exists := hp.config.GPUProfiles[gpuModels[0]]; exists {
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

			// Apply PUE if specified
			if nodePUE > 0 {
				nodePower.PUE = nodePUE
			}

			// Apply GPU PUE if specified
			if nodeGPUPUE > 0 {
				nodePower.GPUPUE = nodeGPUPUE
			}

			// Add to cache and return
			hp.cacheNodeProfile(nodeUID, nodePower)
			return nodePower, nil
		}
	}

	// Get CPU model information for better error diagnostics
	var cpuModelInfo string
	cpuModelInfo, gpuModels = hp.detectNodeHardwareInfoFromSystem(node)

	// Log detailed diagnostics about why hardware profile detection failed
	var cpuProfileCount int
	var profileExists bool
	if hp.config != nil {
		cpuProfileCount = len(hp.config.CPUProfiles)
		if cpuModelInfo != "" {
			_, profileExists = hp.config.CPUProfiles[cpuModelInfo]
		}
	}

	klog.V(2).InfoS("Hardware profile detection failed",
		"node", node.Name,
		"cpuModel", cpuModelInfo,
		"gpuModels", gpuModels,
		"profilesConfigured", hp.config != nil,
		"cpuProfileCount", cpuProfileCount,
		"cpuModelInProfiles", profileExists)

	// Fall back to default values
	return nil, fmt.Errorf("hardware profile not found for node %s (CPU model: %s)", node.Name, cpuModelInfo)
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

// GetNodeHardwareInfo returns CPU and GPU models for a node
// This is a public method that can be used for logging and debugging
func (hp *HardwareProfiler) GetNodeHardwareInfo(node *v1.Node) (cpuModel string, gpuModels []string) {
	return hp.detectNodeHardwareInfoFromSystem(node)
}

// getCPUModelKey creates a lookup key for our CPU mapping table based on NFD labels
func (hp *HardwareProfiler) getCPUModelKey(node *v1.Node) string {
	family, hasFamily := node.Labels[common.NFDLabelCPUModelFamily]
	modelID, hasModelID := node.Labels[common.NFDLabelCPUModelID]

	if !hasFamily || !hasModelID {
		return ""
	}

	// The base key uses the NFD CPU model family and ID
	return fmt.Sprintf("%s-%s", family, modelID)
}

// identifyCPUModelFromNFDLabels tries to identify the CPU model from NFD labels
// Uses family, model, vendor and power state information to look up the specific CPU model
func (hp *HardwareProfiler) identifyCPUModelFromNFDLabels(node *v1.Node) string {
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

	// Check for power state information
	governor, hasGovernor := node.Labels[common.NFDLabelCPUPStateScalingGovernor]
	turbo, hasTurbo := node.Labels[common.NFDLabelCPUPStateTurbo]

	// Log the NFD information we found
	klog.V(2).InfoS("Found NFD CPU information",
		"node", node.Name,
		"vendorID", vendorID,
		"family", family,
		"modelID", modelID,
		"hasGovernor", hasGovernor,
		"governor", governor,
		"hasTurbo", hasTurbo,
		"turbo", turbo)

	// Get the mapping key for CPU identification
	baseKey := hp.getCPUModelKey(node)
	if baseKey == "" {
		return ""
	}

	// Look for known CPU models in our config-based mappings
	if hp.config != nil && hp.config.CPUModelMappings != nil {
		if vendorMapping, ok := hp.config.CPUModelMappings[vendorID]; ok {
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

// detectNodeHardwareInfoFromSystem determines hardware components from the node
// Uses the PowerProvider system to respect priorities between annotations and NFD labels
func (hp *HardwareProfiler) detectNodeHardwareInfoFromSystem(node *v1.Node) (cpuModel string, gpuModels []string) {
	// Get the best power provider based on priority order
	if provider, found := powerprovider.GetBestProvider(node); found {
		// Try to use the highest priority provider to get the hardware info
		klog.V(2).InfoS("Using power provider to detect hardware",
			"node", node.Name,
			"provider", provider.GetProviderName(),
			"priority", provider.GetPriority())

		if cpuModel, gpuModels = provider.GetNodeHardwareInfo(node); cpuModel != "" {
			klog.V(2).InfoS("Power provider detected hardware",
				"node", node.Name,
				"cpuModel", cpuModel,
				"gpuModels", gpuModels)

		}
	}

	// Fallback to NFD labels if power provider approach didn't work
	if cpuModel == "" {
		cpuModel = hp.identifyCPUModelFromNFDLabels(node)
		if cpuModel != "" {
			klog.V(2).InfoS("Identified CPU model from NFD labels", "node", node.Name, "model", cpuModel)
		} else {
			// Provide a generic fallback based on architecture and core count
			// Note: accurate power estimates require the cpu-info-exporter DaemonSet
			arch := node.Labels["kubernetes.io/arch"]
			cpuCores := node.Status.Capacity.Cpu().Value()

			// Use very generic model names that indicate architecture but not specific model
			// (this encourages users to deploy the cpu-info-exporter for accuracy)
			switch arch {
			case "amd64":
				cpuModel = fmt.Sprintf("Generic x86_64 (%d cores)", cpuCores)
				klog.V(2).InfoS("Using generic CPU model (cpu-exporter not detected)",
					"node", node.Name, "model", cpuModel, "cores", cpuCores)
			case "arm64":
				cpuModel = fmt.Sprintf("Generic ARM64 (%d cores)", cpuCores)
				klog.V(2).InfoS("Using generic CPU model (cpu-exporter not detected)",
					"node", node.Name, "model", cpuModel, "cores", cpuCores)
			default:
				cpuModel = fmt.Sprintf("Unknown architecture (%d cores)", cpuCores)
				klog.V(2).InfoS("Using generic CPU model (cpu-exporter not detected)",
					"node", node.Name, "model", cpuModel, "cores", cpuCores)
			}
		}
	}

	// Fallback check NFD labels for GPU information
	if len(gpuModels) == 0 {
		if model, ok := node.Labels[common.NvidiaLabelGPUProduct]; ok {
			// We have an NVIDIA GPU model from NFD labels
			gpuModels = append(gpuModels, model)
		} else if gpuCount, ok := node.Status.Capacity[common.NvidiaLabelBase]; ok && gpuCount.Value() > 0 {
			// If GPU exists but no annotation, determine from node characteristics
			// In production, consider adding a daemon that reports actual GPU model
			if strings.Contains(node.Name, "gpu") ||
				strings.Contains(node.Name, "p3") ||
				strings.Contains(node.Name, "g4") {
				gpuModels = append(gpuModels, "NVIDIA V100") // Common in AWS p3 instances
			} else if strings.Contains(node.Name, "a10") {
				gpuModels = append(gpuModels, "NVIDIA A10G")
			} else {
				gpuModels = append(gpuModels, "NVIDIA T4") // Common default
			}
		}
	}

	return cpuModel, gpuModels
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

// AdjustPowerForFrequency adjusts CPU power based on frequency scaling
// frequencyRatio is the ratio of current frequency to base frequency (e.g., 0.75 for 25% reduction)
func AdjustPowerForFrequency(basePower float64, frequencyRatio float64, scalingModel string) float64 {
	// Default to quadratic scaling if not specified
	if scalingModel == "" {
		scalingModel = "quadratic"
	}

	switch scalingModel {
	case "linear":
		// Power scales linearly with frequency (P ∝ f)
		return basePower * frequencyRatio
	case "quadratic":
		// Power scales with square of frequency (P ∝ f²)
		// This is a common approximation for many modern CPUs
		return basePower * frequencyRatio * frequencyRatio
	case "cubic":
		// Power scales with cube of frequency (P ∝ f³)
		// More aggressive for very high frequencies
		return basePower * frequencyRatio * frequencyRatio * frequencyRatio
	default:
		// Default to quadratic
		return basePower * frequencyRatio * frequencyRatio
	}
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
		memGB := float64(hwComponents.TotalMemory) / 1024 // Convert from MB to GB

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

// GetNodePowerProfile retrieves the power profile for a node, including PUE calculations
func (hp *HardwareProfiler) GetNodePowerProfile(node *v1.Node) (*config.NodePower, error) {
	// First get the base power profile
	profile, err := hp.DetectNodePowerProfile(node)
	if err != nil {
		return nil, err
	}

	// If PUE is not set, use the default PUE
	if profile.PUE == 0 {
		// Use default from constants
		profile.PUE = common.DefaultPUE
	}

	// If GPU PUE is not set, use the default GPU PUE
	if profile.GPUPUE == 0 && profile.MaxGPUPower > 0 {
		// Use default from constants
		profile.GPUPUE = common.DefaultGPUPUE
	}

	return profile, nil
}

// GetEffectivePower returns the total effective power in watts, including PUE overhead
func (hp *HardwareProfiler) GetEffectivePower(profile *config.NodePower, isIdle bool) float64 {
	if profile == nil {
		return 0
	}

	// Calculate CPU + memory power with standard PUE
	var cpuMemPower float64
	if isIdle {
		cpuMemPower = profile.IdlePower
	} else {
		cpuMemPower = profile.MaxPower
	}

	// Apply general PUE if set, otherwise assume optimal efficiency
	pue := profile.PUE
	if pue < 1.0 {
		pue = common.DefaultPUE // Use default if not set or invalid
	}

	// Add GPU power with GPU-specific PUE
	var gpuPower float64
	if isIdle {
		if profile.IdleGPUPower > 0 {
			gpuPower = profile.IdleGPUPower
		}
	} else {
		if profile.MaxGPUPower > 0 {
			gpuPower = profile.MaxGPUPower
		}
	}

	// Apply GPU PUE if set and GPU power exists
	gpuPue := profile.GPUPUE
	if gpuPue < 1.0 && gpuPower > 0 {
		gpuPue = common.DefaultGPUPUE // Use default from constants
	}

	// Calculate total effective power
	effectivePower := (cpuMemPower * pue)
	if gpuPower > 0 {
		effectivePower += (gpuPower * gpuPue)
	}

	return effectivePower
}

// NodeSpecsChanged determines if any node specs changed that might affect hardware profile
func NodeSpecsChanged(oldNode, newNode *v1.Node) bool {
	// Check for instance type changes
	if oldNode.Labels["node.kubernetes.io/instance-type"] != newNode.Labels["node.kubernetes.io/instance-type"] {
		return true
	}

	// Check for architecture changes
	if oldNode.Labels["kubernetes.io/arch"] != newNode.Labels["kubernetes.io/arch"] {
		return true
	}

	// Check for capacity changes
	oldCPU := oldNode.Status.Capacity.Cpu().Value()
	newCPU := newNode.Status.Capacity.Cpu().Value()
	if oldCPU != newCPU {
		return true
	}

	oldMem := oldNode.Status.Capacity.Memory().Value()
	newMem := newNode.Status.Capacity.Memory().Value()
	if oldMem != newMem {
		return true
	}

	// Check for GPU changes
	oldGPU, oldHasGPU := oldNode.Status.Capacity[common.NvidiaLabelBase]
	newGPU, newHasGPU := newNode.Status.Capacity[common.NvidiaLabelBase]

	if oldHasGPU != newHasGPU {
		return true
	}

	if oldHasGPU && newHasGPU && oldGPU.Value() != newGPU.Value() {
		return true
	}

	// Check for changes in CPU model information from NFD labels
	// Check family
	oldCPUFamily, oldHasCPUFamily := oldNode.Labels[common.NFDLabelCPUModelFamily]
	newCPUFamily, newHasCPUFamily := newNode.Labels[common.NFDLabelCPUModelFamily]
	if oldHasCPUFamily != newHasCPUFamily || (oldHasCPUFamily && newHasCPUFamily && oldCPUFamily != newCPUFamily) {
		klog.V(2).InfoS("CPU family label changed on node",
			"node", newNode.Name,
			"oldFamily", oldCPUFamily,
			"newFamily", newCPUFamily)
		return true
	}

	// Check model ID
	oldCPUModelID, oldHasCPUModelID := oldNode.Labels[common.NFDLabelCPUModelID]
	newCPUModelID, newHasCPUModelID := newNode.Labels[common.NFDLabelCPUModelID]
	if oldHasCPUModelID != newHasCPUModelID || (oldHasCPUModelID && newHasCPUModelID && oldCPUModelID != newCPUModelID) {
		klog.V(2).InfoS("CPU model ID label changed on node",
			"node", newNode.Name,
			"oldModelID", oldCPUModelID,
			"newModelID", newCPUModelID)
		return true
	}

	// Check vendor ID
	oldCPUVendorID, oldHasCPUVendorID := oldNode.Labels[common.NFDLabelCPUModelVendorID]
	newCPUVendorID, newHasCPUVendorID := newNode.Labels[common.NFDLabelCPUModelVendorID]
	if oldHasCPUVendorID != newHasCPUVendorID || (oldHasCPUVendorID && newHasCPUVendorID && oldCPUVendorID != newCPUVendorID) {
		klog.V(2).InfoS("CPU vendor ID label changed on node",
			"node", newNode.Name,
			"oldVendorID", oldCPUVendorID,
			"newVendorID", newCPUVendorID)
		return true
	}

	// Check CPU power state changes - these can significantly affect power consumption
	// Check CPU scaling governor (most impactful - powersave vs performance)
	oldGovernor, oldHasGovernor := oldNode.Labels[common.NFDLabelCPUPStateScalingGovernor]
	newGovernor, newHasGovernor := newNode.Labels[common.NFDLabelCPUPStateScalingGovernor]
	if oldHasGovernor != newHasGovernor || (oldHasGovernor && newHasGovernor && oldGovernor != newGovernor) {
		klog.V(2).InfoS("CPU scaling governor changed on node",
			"node", newNode.Name,
			"oldGovernor", oldGovernor,
			"newGovernor", newGovernor)
		return true
	}

	// Check CPU p-state status
	oldStatus, oldHasStatus := oldNode.Labels[common.NFDLabelCPUPStateStatus]
	newStatus, newHasStatus := newNode.Labels[common.NFDLabelCPUPStateStatus]
	if oldHasStatus != newHasStatus || (oldHasStatus && newHasStatus && oldStatus != newStatus) {
		klog.V(2).InfoS("CPU p-state status changed on node",
			"node", newNode.Name,
			"oldStatus", oldStatus,
			"newStatus", newStatus)
		return true
	}

	// Check CPU turbo status
	oldTurbo, oldHasTurbo := oldNode.Labels[common.NFDLabelCPUPStateTurbo]
	newTurbo, newHasTurbo := newNode.Labels[common.NFDLabelCPUPStateTurbo]
	if oldHasTurbo != newHasTurbo || (oldHasTurbo && newHasTurbo && oldTurbo != newTurbo) {
		klog.V(2).InfoS("CPU turbo status changed on node",
			"node", newNode.Name,
			"oldTurbo", oldTurbo,
			"newTurbo", newTurbo)
		return true
	}

	// CPU/GPU model labels changed (legacy)
	if oldNode.Labels["node.kubernetes.io/cpu-model"] != newNode.Labels["node.kubernetes.io/cpu-model"] {
		return true
	}

	if oldNode.Labels["node.kubernetes.io/gpu-model"] != newNode.Labels["node.kubernetes.io/gpu-model"] {
		return true
	}

	return false
}

// CalculateNodeEfficiency calculates an efficiency metric for the node
// This could be based on CPU type, frequency, cores per watt, etc.
func CalculateNodeEfficiency(node *v1.Node, powerProfile *config.NodePower) float64 {
	if powerProfile == nil || powerProfile.MaxPower == 0 {
		return 0
	}

	// Get effective max power including PUE
	effectivePower := powerProfile.MaxPower
	if powerProfile.PUE > 0 {
		effectivePower *= powerProfile.PUE
	}

	// Calculate a basic efficiency metric based on CPU cores and max power
	cpuCapacity := node.Status.Capacity.Cpu().Value()

	// Higher number means more cores per watt (more efficient)
	efficiency := float64(cpuCapacity) / effectivePower

	// Consider GPU efficiency if present
	if gpuCount, ok := node.Status.Capacity[common.NvidiaLabelBase]; ok && gpuCount.Value() > 0 {
		// If GPUs are present, factor into the efficiency calculation
		// This is a simple model - in real world, would be more complex
		gpuEfficiency := 0.0

		// If we have proper GPU power data, use it
		if powerProfile.MaxGPUPower > 0 {
			// Calculate GPU efficiency based on actual max power
			gpuEfficiency = 10.0 / powerProfile.MaxGPUPower
		} else {
			// Estimate based on GPU model
			gpuModel := "unknown"
			if model, ok := node.Labels["node.kubernetes.io/gpu-model"]; ok {
				gpuModel = model
			}

			// Assign efficiency based on known GPU models
			// These are just example values - should be based on real benchmarks
			switch {
			case strings.Contains(gpuModel, "V100"):
				gpuEfficiency = 1.8
			case strings.Contains(gpuModel, "A100"):
				gpuEfficiency = 2.5
			case strings.Contains(gpuModel, "T4"):
				gpuEfficiency = 1.5
			default:
				gpuEfficiency = 1.0
			}
		}

		// Weighted average of CPU and GPU efficiency
		efficiency = (efficiency + gpuEfficiency) / 2
	}

	return efficiency
}
