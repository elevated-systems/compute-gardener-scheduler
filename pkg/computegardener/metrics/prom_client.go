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
	client        v1.API
	queryTimeout  time.Duration
	metricsPrefix string // The prefix for metrics (e.g., compute_gardener_gpu)
	// DCGM-specific settings
	useDCGM         bool   // Whether to use DCGM exporter metrics
	dcgmPowerMetric string // DCGM power metric name
	dcgmUtilMetric  string // DCGM utilization metric name
}

// SetUseDCGM configures whether to use DCGM metrics
func (c *PrometheusGPUMetricsClient) SetUseDCGM(use bool) {
	c.useDCGM = use
	klog.V(2).InfoS("DCGM metrics usage configured", "useDCGM", use)
}

// SetDCGMPowerMetric sets the DCGM power metric name
func (c *PrometheusGPUMetricsClient) SetDCGMPowerMetric(metric string) {
	c.dcgmPowerMetric = metric
	klog.V(2).InfoS("DCGM power metric configured", "metric", metric)
}

// SetDCGMUtilMetric sets the DCGM utilization metric name
func (c *PrometheusGPUMetricsClient) SetDCGMUtilMetric(metric string) {
	c.dcgmUtilMetric = metric
	klog.V(2).InfoS("DCGM utilization metric configured", "metric", metric)
}

// GetDCGMPowerMetric returns the current DCGM power metric name
func (c *PrometheusGPUMetricsClient) GetDCGMPowerMetric() string {
	return c.dcgmPowerMetric
}

// GetDCGMUtilMetric returns the current DCGM utilization metric name
func (c *PrometheusGPUMetricsClient) GetDCGMUtilMetric() string {
	return c.dcgmUtilMetric
}

// NewPrometheusGPUMetricsClient creates a new Prometheus-based GPU metrics client
// By default, it uses DCGM metrics if available
func NewPrometheusGPUMetricsClient(prometheusURL string) (*PrometheusGPUMetricsClient, error) {
	// Initialize Prometheus client
	cfg := api.Config{
		Address: prometheusURL,
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating Prometheus client: %v", err)
	}

	// Create with DCGM metrics enabled by default
	metricsClient := &PrometheusGPUMetricsClient{
		client:        v1.NewAPI(client),
		queryTimeout:  30 * time.Second,
		metricsPrefix: "compute_gardener_gpu", // Still useful as fallback

		// Enable DCGM metrics by default
		useDCGM:         true,
		dcgmPowerMetric: "DCGM_FI_DEV_POWER_USAGE",
		dcgmUtilMetric:  "DCGM_FI_DEV_GPU_UTIL",
	}

	klog.InfoS("Created Prometheus GPU metrics client",
		"prometheusURL", prometheusURL,
		"usingDCGM", true,
		"powerMetric", metricsClient.dcgmPowerMetric,
		"utilMetric", metricsClient.dcgmUtilMetric)

	return metricsClient, nil
}

// NewLegacyPrometheusGPUMetricsClient creates a Prometheus client that uses the legacy
// custom metrics format instead of DCGM metrics
func NewLegacyPrometheusGPUMetricsClient(prometheusURL string) (*PrometheusGPUMetricsClient, error) {
	// Initialize Prometheus client
	cfg := api.Config{
		Address: prometheusURL,
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating Prometheus client: %v", err)
	}

	klog.InfoS("Created legacy Prometheus GPU metrics client (without DCGM)",
		"prometheusURL", prometheusURL)

	return &PrometheusGPUMetricsClient{
		client:        v1.NewAPI(client),
		queryTimeout:  30 * time.Second,
		metricsPrefix: "compute_gardener_gpu",
		useDCGM:       false,
	}, nil
}

// GetPodGPUUtilization gets GPU utilization for a specific pod
func (c *PrometheusGPUMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()

	// Construct the query for the specific pod's GPU utilization
	var query string
	if c.useDCGM {
		// DCGM metrics query - we need to query all GPU metrics since they're not labeled by workload pod
		query = fmt.Sprintf(`avg(%s)`, c.dcgmUtilMetric)
		klog.V(2).InfoS("Using DCGM for GPU utilization - note: this will attribute ALL GPU utilization to this pod",
			"pod", name, "namespace", namespace, "query", query)
	} else {
		// Custom metrics query
		query = fmt.Sprintf(`avg(%s_utilization_percent{pod="%s", namespace="%s"})`,
			c.metricsPrefix, name, namespace)
	}

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
	var query string
	if c.useDCGM {
		// DCGM metrics query - we can't filter by workload pod, so we'll get metrics by GPU device
		query = fmt.Sprintf(`avg by (UUID) (%s)`, c.dcgmUtilMetric)
		klog.V(2).InfoS("Using DCGM utilization metric by GPU UUID", "metric", c.dcgmUtilMetric)
	} else {
		// Custom metrics query
		query = fmt.Sprintf(`avg by (pod, namespace) (%s_utilization_percent)`, c.metricsPrefix)
	}

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

		// Process each result
		for _, sample := range vector {
			if c.useDCGM {
				// For DCGM metrics, extract GPU UUID from the metric labels
				uuid, ok := sample.Metric["UUID"]

				if !ok {
					klog.V(2).InfoS("Missing UUID label in DCGM GPU utilization metric",
						"metric", sample.Metric.String())
					continue
				}

				// Use UUID as the key - this will be matched with pods using the GPU
				key := fmt.Sprintf("gpu/%s", uuid)

				// Convert from percentage to 0-1 scale
				utilizations[key] = float64(sample.Value) / 100.0

				klog.V(2).InfoS("Recorded GPU utilization",
					"UUID", uuid,
					"utilization", float64(sample.Value),
					"normalized", float64(sample.Value)/100.0)
			} else {
				// For custom metrics, extract pod and namespace
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
	}

	return utilizations, nil
}

// GetPodGPUPower gets GPU power for a specific pod
func (c *PrometheusGPUMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()

	// Construct the query for the specific pod's GPU power
	var query string
	if c.useDCGM {
		// DCGM metrics query - we need to query all GPU metrics since they're not labeled by workload pod
		query = fmt.Sprintf(`avg(%s)`, c.dcgmPowerMetric)
		klog.V(2).InfoS("Using DCGM for GPU power - note: this will attribute ALL GPU power to this pod",
			"pod", name, "namespace", namespace, "query", query)
	} else {
		// Custom metrics query
		query = fmt.Sprintf(`avg(%s_power_watts{pod="%s", namespace="%s"})`,
			c.metricsPrefix, name, namespace)
	}

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
	var query string
	if c.useDCGM {
		// DCGM metrics query
		query = fmt.Sprintf(`avg by (UUID) (%s)`, c.dcgmPowerMetric)
		klog.V(2).InfoS("Using DCGM power metric by GPU UUID", "metric", c.dcgmPowerMetric)
	} else {
		// Custom metrics query
		query = fmt.Sprintf(`avg by (pod, namespace) (%s_power_watts)`, c.metricsPrefix)
	}

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

		// Process each result
		for _, sample := range vector {
			if c.useDCGM {
				// For DCGM metrics, extract GPU UUID from the metric labels
				uuid, ok := sample.Metric["UUID"]

				if !ok {
					klog.V(2).InfoS("Missing UUID label in DCGM GPU power metric",
						"metric", sample.Metric.String())
					continue
				}

				// Use UUID as the key - this will be matched with pods using the GPU
				key := fmt.Sprintf("gpu/%s", uuid)

				// Store the power value in watts
				powers[key] = float64(sample.Value)

				klog.V(2).InfoS("Recorded GPU power",
					"UUID", uuid,
					"power", float64(sample.Value))
			} else {
				// For custom metrics, extract pod and namespace
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

	var utilizationQuery, powerQuery string

	if c.useDCGM {
		// Use DCGM metrics - we need to query all GPU metrics since they are not labeled by workload pod
		utilizationQuery = fmt.Sprintf(`avg(%s)`,
			c.dcgmUtilMetric)

		powerQuery = fmt.Sprintf(`avg(%s)`,
			c.dcgmPowerMetric)

		klog.V(2).InfoS("Using DCGM metrics for historical GPU data - note: this will attribute ALL GPU utilization to this pod",
			"pod", name,
			"namespace", namespace,
			"powerMetric", c.dcgmPowerMetric,
			"utilMetric", c.dcgmUtilMetric)
	} else {
		// Use our custom metrics
		utilizationQuery = fmt.Sprintf(`avg(%s_utilization_percent{pod="%s", namespace="%s"})`,
			c.metricsPrefix, name, namespace)

		powerQuery = fmt.Sprintf(`avg(%s_power_watts{pod="%s", namespace="%s"})`,
			c.metricsPrefix, name, namespace)
	}

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
