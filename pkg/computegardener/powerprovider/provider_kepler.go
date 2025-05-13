package powerprovider

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/clients"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// KeplerPowerProvider provides power information from Kepler metrics
type KeplerPowerProvider struct {
	// Prometheus client for querying Kepler metrics
	promClient *clients.PrometheusMetricsClient
}

// Make sure KeplerPowerProvider implements PowerInfoProvider
var _ PowerInfoProvider = &KeplerPowerProvider{}

// NewKeplerPowerProvider creates a new Kepler-based power provider
func NewKeplerPowerProvider(promClient *clients.PrometheusMetricsClient) *KeplerPowerProvider {
	provider := &KeplerPowerProvider{
		promClient: promClient,
	}
	RegisterProvider(provider)
	return provider
}

// IsAvailable checks if this provider can provide power data for the given node
func (p *KeplerPowerProvider) IsAvailable(node *v1.Node) bool {
	// This provider is available if we can query Kepler metrics for this node
	if p.promClient == nil {
		return false
	}

	// Check if node appears in Kepler metrics
	nodeName := node.Name
	cpuPower, err := p.queryCurrentPowerMetric(nodeName, "cpu")
	if err != nil || cpuPower <= 0 {
		klog.V(2).InfoS("Kepler provider not available for node (no CPU power metrics)",
			"node", nodeName, "error", err)
		return false
	}

	return true
}

// GetPriority returns the priority of this provider
func (p *KeplerPowerProvider) GetPriority() int {
	return PRIORITY_KEPLER_MEASURED
}

// GetProviderType returns whether this provider uses measured or estimated data
func (p *KeplerPowerProvider) GetProviderType() PowerDataType {
	return PowerDataTypeMeasured
}

// GetProviderName returns a human-readable identifier for the provider
func (p *KeplerPowerProvider) GetProviderName() string {
	return "Kepler-Measured"
}

func (p *KeplerPowerProvider) GetNodeHardwareInfo(node *v1.Node) (string, []string) {
	// NOT IMPLEMENTED
	return "", []string{}
}

// GetNodePowerInfo returns power information for a node
func (p *KeplerPowerProvider) GetNodePowerInfo(node *v1.Node, hwConfig *config.HardwareProfiles) (*config.NodePower, error) {
	if p.promClient == nil {
		return nil, fmt.Errorf("Prometheus client not configured for Kepler provider")
	}

	nodeName := node.Name
	// Get current power measurements
	cpuPower, err := p.queryCurrentPowerMetric(nodeName, "cpu")
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU power for node %s: %v", nodeName, err)
	}

	// Get DRAM/memory power
	memPower, _ := p.queryCurrentPowerMetric(nodeName, "dram")
	// Memory power might be 0 if not measured, that's OK

	// Get GPU power if available
	gpuPower, _ := p.queryCurrentPowerMetric(nodeName, "gpu")
	// GPU power might be 0 if no GPU or not measured, that's OK

	// Get historical min/max values for better idle/max estimates
	cpuIdlePower, cpuMaxPower := p.queryPowerRange(nodeName, "cpu", 24*time.Hour)
	memIdlePower, memMaxPower := p.queryPowerRange(nodeName, "dram", 24*time.Hour)
	gpuIdlePower, gpuMaxPower := p.queryPowerRange(nodeName, "gpu", 24*time.Hour)

	// Use current values as fallbacks if historical data not available
	if cpuIdlePower <= 0 {
		cpuIdlePower = cpuPower * 0.5 // Estimate idle as 50% of current if no data
	}
	if cpuMaxPower <= 0 {
		cpuMaxPower = cpuPower * 1.5 // Estimate max as 150% of current if no data
	}

	// Create power profile with measured values
	nodePower := &config.NodePower{
		IdlePower:    cpuIdlePower + memIdlePower,
		MaxPower:     cpuMaxPower + memMaxPower,
		IdleGPUPower: gpuIdlePower,
		MaxGPUPower:  gpuMaxPower,
	}

	// Check for PUE annotation or use default
	if pueStr, ok := node.Annotations[common.AnnotationPUE]; ok {
		if pue, err := p.parseFloat(pueStr); err == nil && pue >= 1.0 {
			nodePower.PUE = pue
		} else {
			nodePower.PUE = common.DefaultPUE
		}
	} else {
		nodePower.PUE = common.DefaultPUE
	}

	// Check for GPU PUE annotation or use default
	if gpuPueStr, ok := node.Annotations[common.AnnotationGPUPUE]; ok {
		if gpuPue, err := p.parseFloat(gpuPueStr); err == nil && gpuPue >= 1.0 {
			nodePower.GPUPUE = gpuPue
		} else if nodePower.MaxGPUPower > 0 {
			nodePower.GPUPUE = common.DefaultGPUPUE
		}
	} else if nodePower.MaxGPUPower > 0 {
		nodePower.GPUPUE = common.DefaultGPUPUE
	}

	// Log the measurements for debugging
	klog.V(2).InfoS("Kepler power measurements for node",
		"node", nodeName,
		"currentCPU", cpuPower,
		"currentMem", memPower,
		"currentGPU", gpuPower,
		"idleCPU", cpuIdlePower,
		"maxCPU", cpuMaxPower)

	return nodePower, nil
}

// queryCurrentPowerMetric queries the current power consumption for a component
func (p *KeplerPowerProvider) queryCurrentPowerMetric(nodeName, component string) (float64, error) {
	// Example query: kepler_node_component_power_watts{instance="node1",component="cpu"}
	metricName := fmt.Sprintf(`kepler_node_component_power_watts{component="%s"}`, component)

	// Create context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return p.promClient.QueryNodeMetric(ctx, metricName, nodeName)
}

// queryPowerRange gets min/max power values over a time range
func (p *KeplerPowerProvider) queryPowerRange(nodeName, component string, lookback time.Duration) (min, max float64) {
	// Query for minimum (idle) power
	minMetric := fmt.Sprintf(`min_over_time(kepler_node_component_power_watts{component="%s"}[%s])`,
		component, formatDuration(lookback))

	// Query for maximum power
	maxMetric := fmt.Sprintf(`max_over_time(kepler_node_component_power_watts{component="%s"}[%s])`,
		component, formatDuration(lookback))

	// Use context with timeout for the queries
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query min power
	minPower, err := p.promClient.QueryNodeMetric(ctx, minMetric, nodeName)
	if err == nil {
		min = minPower
	}

	// Create new context for max query
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	// Query max power
	maxPower, err := p.promClient.QueryNodeMetric(ctx2, maxMetric, nodeName)
	if err == nil {
		max = maxPower
	}

	return min, max
}

// parseFloat safely parses a string to float64
func (p *KeplerPowerProvider) parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// formatDuration formats a duration for Prometheus time range
func formatDuration(d time.Duration) string {
	// Convert to seconds for simplicity
	seconds := int(d.Seconds())
	return fmt.Sprintf("%ds", seconds)
}
