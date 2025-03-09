package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
)

// PrometheusGPUMetricsClient implements GPUMetricsClient using Prometheus
type PrometheusGPUMetricsClient struct {
	client       v1.API
	queryTimeout time.Duration
	metricsPrefix string // The prefix for metrics (e.g., compute_gardener_gpu)
}

// NewPrometheusGPUMetricsClient creates a new Prometheus-based GPU metrics client
func NewPrometheusGPUMetricsClient(prometheusURL string) (*PrometheusGPUMetricsClient, error) {
	// Initialize Prometheus client
	cfg := api.Config{
		Address: prometheusURL,
	}
	
	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating Prometheus client: %v", err)
	}
	
	return &PrometheusGPUMetricsClient{
		client:       v1.NewAPI(client),
		queryTimeout: 30 * time.Second,
		metricsPrefix: "compute_gardener_gpu", // Default prefix for our metrics
	}, nil
}

// GetPodGPUUtilization gets GPU utilization for a specific pod
func (c *PrometheusGPUMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()
	
	// Construct the query for the specific pod's GPU utilization
	query := fmt.Sprintf(`avg(%s_utilization_percent{pod="%s", namespace="%s"})`, 
		c.metricsPrefix, name, namespace)
	
	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("error querying Prometheus for GPU utilization: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus query", 
			"warnings", warnings,
			"query", query)
	}
	
	// Extract the result
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		if len(vector) == 0 {
			// No data available
			klog.V(2).InfoS("No GPU utilization data available for pod",
				"namespace", namespace, 
				"name", name)
			return 0, nil
		}
		
		// Return the average utilization (convert from percentage to 0-1 scale)
		return float64(vector[0].Value) / 100.0, nil
	}
	
	return 0, fmt.Errorf("unexpected result type from Prometheus: %s", result.Type().String())
}

// ListPodsGPUUtilization gets GPU utilization for all pods with GPUs
func (c *PrometheusGPUMetricsClient) ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()
	
	// Query that gets average GPU utilization for each pod
	query := fmt.Sprintf(`avg by (pod, namespace) (%s_utilization_percent)`, c.metricsPrefix)
	
	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus for GPU utilization: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus query", 
			"warnings", warnings,
			"query", query)
	}
	
	// Process results
	utilizations := make(map[string]float64)
	
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		
		// Process each result
		for _, sample := range vector {
			// Extract pod and namespace from the metric labels
			pod, ok1 := sample.Metric["pod"]
			namespace, ok2 := sample.Metric["namespace"]
			
			if !ok1 || !ok2 {
				klog.V(2).InfoS("Missing pod or namespace label in GPU utilization metric",
					"metric", sample.Metric.String())
				continue
			}
			
			// Construct the key in the format namespace/pod
			key := fmt.Sprintf("%s/%s", namespace, pod)
			
			// Convert from percentage to 0-1 scale
			utilizations[key] = float64(sample.Value) / 100.0
		}
	}
	
	return utilizations, nil
}

// GetPodGPUPower gets GPU power for a specific pod
func (c *PrometheusGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()
	
	// Construct the query for the specific pod's GPU power
	query := fmt.Sprintf(`avg(%s_power_watts{pod="%s", namespace="%s"})`, 
		c.metricsPrefix, name, namespace)
	
	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("error querying Prometheus for GPU power: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus query", 
			"warnings", warnings,
			"query", query)
	}
	
	// Extract the result
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		if len(vector) == 0 {
			// No data available
			klog.V(2).InfoS("No GPU power data available for pod",
				"namespace", namespace, 
				"name", name)
			return 0, nil
		}
		
		// Return the average power in watts
		return float64(vector[0].Value), nil
	}
	
	return 0, fmt.Errorf("unexpected result type from Prometheus: %s", result.Type().String())
}

// ListPodsGPUPower gets GPU power for all pods with GPUs
func (c *PrometheusGPUMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()
	
	// Query that gets average GPU power for each pod
	query := fmt.Sprintf(`avg by (pod, namespace) (%s_power_watts)`, c.metricsPrefix)
	
	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus for GPU power: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus query", 
			"warnings", warnings,
			"query", query)
	}
	
	// Process results
	powers := make(map[string]float64)
	
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		
		// Process each result
		for _, sample := range vector {
			// Extract pod and namespace from the metric labels
			pod, ok1 := sample.Metric["pod"]
			namespace, ok2 := sample.Metric["namespace"]
			
			if !ok1 || !ok2 {
				klog.V(2).InfoS("Missing pod or namespace label in GPU power metric",
					"metric", sample.Metric.String())
				continue
			}
			
			// Construct the key in the format namespace/pod
			key := fmt.Sprintf("%s/%s", namespace, pod)
			
			// Store the power value in watts
			powers[key] = float64(sample.Value)
		}
	}
	
	return powers, nil
}

// GetPodHistoricalGPUMetrics gets historical GPU metrics for pod over time window
func (c *PrometheusGPUMetricsClient) GetPodHistoricalGPUMetrics(ctx context.Context, namespace, name string, startTime, endTime time.Time) (*PodGPUMetricsHistory, error) {
	// Create a context with timeout (longer timeout for range queries)
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout*2)
	defer cancel()
	
	// Use a step size that's appropriate for the time range
	// For longer periods, we use a larger step to reduce data volume
	timeRange := endTime.Sub(startTime)
	var step time.Duration
	
	if timeRange < 10*time.Minute {
		step = 15 * time.Second
	} else if timeRange < 3*time.Hour {
		step = 1 * time.Minute
	} else {
		step = 5 * time.Minute
	}
	
	// Create range queries for utilization and power
	utilizationQuery := fmt.Sprintf(`avg(%s_utilization_percent{pod="%s", namespace="%s"})`, 
		c.metricsPrefix, name, namespace)
	
	powerQuery := fmt.Sprintf(`avg(%s_power_watts{pod="%s", namespace="%s"})`, 
		c.metricsPrefix, name, namespace)
	
	// Execute utilization range query
	utilizationResult, warnings, err := c.client.QueryRange(
		queryCtx,
		utilizationQuery,
		v1.Range{
			Start: startTime,
			End:   endTime,
			Step:  step,
		},
	)
	
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus for GPU utilization history: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus utilization range query", 
			"warnings", warnings,
			"query", utilizationQuery)
	}
	
	// Execute power range query
	powerResult, warnings, err := c.client.QueryRange(
		queryCtx,
		powerQuery,
		v1.Range{
			Start: startTime,
			End:   endTime,
			Step:  step,
		},
	)
	
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus for GPU power history: %v", err)
	}
	
	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus power range query", 
			"warnings", warnings,
			"query", powerQuery)
	}
	
	// Process the results and create a history record
	history := &PodGPUMetricsHistory{
		PodName:     name,
		Namespace:   namespace,
		Timestamps:  []time.Time{},
		Utilization: []float64{},
		Power:       []float64{},
	}
	
	// Process utilization data
	if utilizationResult.Type() == model.ValMatrix {
		matrix := utilizationResult.(model.Matrix)
		
		if len(matrix) > 0 && len(matrix[0].Values) > 0 {
			// Get the first series (assuming there's only one for the avg)
			series := matrix[0]
			
			// Pre-allocate slices based on the number of samples
			history.Timestamps = make([]time.Time, len(series.Values))
			history.Utilization = make([]float64, len(series.Values))
			
			// Extract the values
			for i, point := range series.Values {
				history.Timestamps[i] = time.Unix(int64(point.Timestamp)/1000, 0)
				history.Utilization[i] = float64(point.Value)
			}
		}
	}
	
	// Process power data
	if powerResult.Type() == model.ValMatrix {
		matrix := powerResult.(model.Matrix)
		
		if len(matrix) > 0 && len(matrix[0].Values) > 0 {
			// Get the first series (assuming there's only one for the avg)
			series := matrix[0]
			
			// Ensure we have space for the power values
			if len(history.Power) < len(series.Values) {
				history.Power = make([]float64, len(series.Values))
			}
			
			// If we don't have timestamps yet (no utilization data), create them
			if len(history.Timestamps) == 0 {
				history.Timestamps = make([]time.Time, len(series.Values))
				for i, point := range series.Values {
					history.Timestamps[i] = time.Unix(int64(point.Timestamp)/1000, 0)
				}
			}
			
			// Extract the power values
			for i, point := range series.Values {
				// Only add power values that correspond to our timestamps
				if i < len(history.Timestamps) {
					history.Power[i] = float64(point.Value)
				}
			}
		}
	}
	
	// If we have no data, log a warning
	if len(history.Timestamps) == 0 {
		klog.V(2).InfoS("No historical GPU metrics found for pod",
			"namespace", namespace,
			"name", name,
			"startTime", startTime,
			"endTime", endTime)
	} else {
		klog.V(2).InfoS("Retrieved historical GPU metrics for pod",
			"namespace", namespace,
			"name", name,
			"dataPoints", len(history.Timestamps),
			"startTime", history.Timestamps[0],
			"endTime", history.Timestamps[len(history.Timestamps)-1])
	}
	
	return history, nil
}

// PodGPUMetricsHistory contains time series of GPU metrics for a pod
type PodGPUMetricsHistory struct {
	PodName     string
	Namespace   string
	Timestamps  []time.Time
	Utilization []float64
	Power       []float64
}

// CalculateAverageGPUMetrics computes average utilization and power over the time window
func (h *PodGPUMetricsHistory) CalculateAverageGPUMetrics() (utilization, power float64) {
	if len(h.Timestamps) == 0 {
		return 0, 0
	}
	
	var totalUtil, totalPower float64
	
	for i := range h.Timestamps {
		totalUtil += h.Utilization[i]
		totalPower += h.Power[i]
	}
	
	return totalUtil / float64(len(h.Timestamps)), totalPower / float64(len(h.Timestamps))
}

// CalculateTotalEnergy computes total GPU energy usage in watt-hours
func (h *PodGPUMetricsHistory) CalculateTotalEnergy() float64 {
	if len(h.Timestamps) < 2 {
		return 0
	}
	
	totalEnergy := 0.0
	
	// Use trapezoid rule for integration
	for i := 1; i < len(h.Timestamps); i++ {
		// Time difference in hours
		dt := h.Timestamps[i].Sub(h.Timestamps[i-1]).Hours()
		
		// Average power during this interval
		avgPower := (h.Power[i] + h.Power[i-1]) / 2
		
		// Energy in watt-hours
		energy := avgPower * dt
		
		totalEnergy += energy
	}
	
	return totalEnergy
}

// ConvertToStandardFormat converts the GPU metrics history to standard PodMetricsRecord format
// that can be used with the common calculation utilities
func (h *PodGPUMetricsHistory) ConvertToStandardFormat() []PodMetricsRecord {
	if len(h.Timestamps) == 0 {
		return nil
	}
	
	records := make([]PodMetricsRecord, len(h.Timestamps))
	
	for i := range h.Timestamps {
		records[i] = PodMetricsRecord{
			Timestamp:     h.Timestamps[i],
			GPU:           h.Utilization[i] / 100.0, // Convert percentage to 0-1 range
			PowerEstimate: h.Power[i],               // GPU power in watts
			// CPU and Memory will be 0 as this is GPU-specific
		}
	}
	
	return records
}