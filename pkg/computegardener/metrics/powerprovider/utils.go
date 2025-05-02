package powerprovider

// Provider priority constants
// These define the relative priorities of different power providers
// Higher priority providers will be preferred over lower priority ones
const (
	// Highest priority: for providers with measured data
	PRIORITY_KEPLER_MEASURED = 100

	// Medium-high priority: for providers with well-known hardware configurations
	// via annotations, used when accurate power data is needed for specific configurations
	PRIORITY_ANNOTATION = 80

	// Medium priority: for providers using NFD-based information
	PRIORITY_NFD = 50

	// Low priority: fallback providers based on minimal information
	PRIORITY_FALLBACK = 10
)

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
