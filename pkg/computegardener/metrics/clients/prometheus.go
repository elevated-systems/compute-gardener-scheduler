package clients

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
)

// PrometheusMetricsClient implements metrics queries using Prometheus
// Used for both GPU and CPU metrics
type PrometheusMetricsClient struct {
	client        v1.API
	queryTimeout  time.Duration
	metricsPrefix string // The prefix for metrics (e.g., compute_gardener_gpu)
	// DCGM-specific settings
	useDCGM         bool   // Whether to use DCGM exporter metrics
	dcgmPowerMetric string // DCGM power metric name
	dcgmUtilMetric  string // DCGM utilization metric name
}

// SetUseDCGM configures whether to use DCGM metrics
func (c *PrometheusMetricsClient) SetUseDCGM(use bool) {
	c.useDCGM = use
	klog.V(2).InfoS("DCGM metrics usage configured", "useDCGM", use)
}

// SetDCGMPowerMetric sets the DCGM power metric name
func (c *PrometheusMetricsClient) SetDCGMPowerMetric(metric string) {
	c.dcgmPowerMetric = metric
	klog.V(2).InfoS("DCGM power metric configured", "metric", metric)
}

// SetDCGMUtilMetric sets the DCGM utilization metric name
func (c *PrometheusMetricsClient) SetDCGMUtilMetric(metric string) {
	c.dcgmUtilMetric = metric
	klog.V(2).InfoS("DCGM utilization metric configured", "metric", metric)
}

// GetNodeCPUTemperature gets current CPU core temperature for a node
func (c *PrometheusMetricsClient) GetNodeCPUTemperature(ctx context.Context, nodeName string) (float64, error) {
	// Standard node-exporter CPU core temperature metric
	return c.QueryNodeMetric(ctx, common.MetricCPUTemperatureQuery, nodeName)
}

// GetNodeGPUTemperature gets current GPU temperature for a node using DCGM
func (c *PrometheusMetricsClient) GetNodeGPUTemperature(ctx context.Context, nodeName string, tempType string) (float64, error) {
	if !c.useDCGM {
		return 0, fmt.Errorf("GPU temperature requires DCGM metrics")
	}

	var metricName string
	switch tempType {
	case "core":
		metricName = common.DCGMMetricGPUTempCore
	case "memory":
		metricName = common.DCGMMetricGPUTempMemory
	default:
		return 0, fmt.Errorf("unsupported GPU temperature type: %s", tempType)
	}

	return c.QueryNodeMetric(ctx, metricName, nodeName)
}

// GetDCGMPowerMetric returns the current DCGM power metric name
func (c *PrometheusMetricsClient) GetDCGMPowerMetric() string {
	return c.dcgmPowerMetric
}

// GetDCGMUtilMetric returns the current DCGM utilization metric name
func (c *PrometheusMetricsClient) GetDCGMUtilMetric() string {
	return c.dcgmUtilMetric
}

// NewPrometheusMetricsClient creates a new Prometheus-based GPU metrics client
// By default, it uses DCGM metrics if available
func NewPrometheusMetricsClient(prometheusURL string) (*PrometheusMetricsClient, error) {
	// Initialize Prometheus client
	cfg := api.Config{
		Address: prometheusURL,
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating Prometheus client: %v", err)
	}

	// Create with DCGM metrics enabled by default
	metricsClient := &PrometheusMetricsClient{
		client:        v1.NewAPI(client),
		queryTimeout:  30 * time.Second,
		metricsPrefix: "compute_gardener_gpu", // Still useful as fallback

		// Enable DCGM metrics by default
		useDCGM:         true,
		dcgmPowerMetric: common.DCGMMetricGPUPower,
		dcgmUtilMetric:  common.DCGMMetricGPUUtilization,
	}

	klog.InfoS("Created Prometheus GPU metrics client",
		"prometheusURL", prometheusURL,
		"usingDCGM", true,
		"powerMetric", metricsClient.dcgmPowerMetric,
		"utilMetric", metricsClient.dcgmUtilMetric)

	return metricsClient, nil
}

// QueryNodeMetric queries a single metric value for a node
func (c *PrometheusMetricsClient) QueryNodeMetric(ctx context.Context, metricName, nodeName string) (float64, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()

	// Construct the query for the node metric
	query := fmt.Sprintf(`%s{instance=~"%s.*"}`, metricName, nodeName)

	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("error querying Prometheus for node metric %s: %v", metricName, err)
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
			klog.V(2).InfoS("No metric data available for node",
				"node", nodeName,
				"metric", metricName)
			return 0, fmt.Errorf("no data available for metric %s on node %s", metricName, nodeName)
		}

		// Return the first value found
		return float64(vector[0].Value), nil
	}

	return 0, fmt.Errorf("unexpected result type from Prometheus: %s", result.Type().String())
}

// NewLegacyPrometheusMetricsClient creates a Prometheus client that uses the legacy
// custom metrics format instead of DCGM metrics
func NewLegacyPrometheusMetricsClient(prometheusURL string) (*PrometheusMetricsClient, error) {
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

	return &PrometheusMetricsClient{
		client:        v1.NewAPI(client),
		queryTimeout:  30 * time.Second,
		metricsPrefix: "compute_gardener_gpu",
		useDCGM:       false,
	}, nil
}

// GetPodGPUUtilization gets GPU utilization for a specific pod
func (c *PrometheusMetricsClient) GetPodGPUUtilization(ctx context.Context, namespace, name string) (float64, error) {
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
func (c *PrometheusMetricsClient) ListPodsGPUUtilization(ctx context.Context) (map[string]float64, error) {
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
func (c *PrometheusMetricsClient) GetPodGPUPower(ctx context.Context, namespace, name string) (float64, error) {
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
func (c *PrometheusMetricsClient) ListPodsGPUPower(ctx context.Context) (map[string]float64, error) {
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
func (c *PrometheusMetricsClient) GetPodHistoricalGPUMetrics(ctx context.Context, namespace, name string, startTime, endTime time.Time) (*PodGPUMetricsHistory, error) {
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
	TempCore    []float64 // GPU core temperature (°C) from DCGM_FI_DEV_GPU_TEMP
	TempMemory  []float64 // GPU memory temperature (°C) from DCGM_FI_DEV_MEM_TEMP
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

// QueryGPUInstanceLabels queries DCGM metrics to get a mapping of GPU UUIDs to node names
func (c *PrometheusMetricsClient) QueryGPUInstanceLabels(ctx context.Context) (map[string]string, error) {
	if !c.useDCGM {
		return nil, fmt.Errorf("GPU instance label queries require DCGM metrics")
	}

	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()

	// Query DCGM power metrics to get UUID to instance mapping
	query := c.dcgmPowerMetric

	// Execute the query
	result, warnings, err := c.client.Query(queryCtx, query, time.Now())
	if err != nil {
		klog.V(1).InfoS("Error querying Prometheus for GPU instance labels",
			"error", err,
			"query", query)
		return nil, fmt.Errorf("error querying Prometheus for GPU instance labels: %v", err)
	}

	// Log any warnings
	if len(warnings) > 0 {
		klog.V(2).InfoS("Warnings received from Prometheus query",
			"warnings", warnings,
			"query", query)
	}

	klog.V(2).InfoS("Prometheus query result for GPU instance labels",
		"query", query,
		"resultType", result.Type().String(),
		"resultString", result.String())

	// Process results to build UUID -> node mapping
	uuidToNode := make(map[string]string)

	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		klog.V(2).InfoS("Processing DCGM vector result for GPU mapping",
			"sampleCount", len(vector))

		for i, sample := range vector {
			klog.V(3).InfoS("Processing DCGM sample",
				"sampleIndex", i,
				"metric", sample.Metric.String(),
				"value", sample.Value)

			// Extract UUID from the metric labels
			uuid, hasUUID := sample.Metric["UUID"]
			instance, hasInstance := sample.Metric["instance"]

			if !hasUUID {
				klog.V(2).InfoS("Missing UUID label in DCGM metric",
					"metric", sample.Metric.String(),
					"availableLabels", func() []string {
						labels := make([]string, 0, len(sample.Metric))
						for k := range sample.Metric {
							labels = append(labels, string(k))
						}
						return labels
					}())
				continue
			}

			if !hasInstance {
				klog.V(2).InfoS("Missing instance label in DCGM metric",
					"UUID", uuid,
					"metric", sample.Metric.String(),
					"availableLabels", func() []string {
						labels := make([]string, 0, len(sample.Metric))
						for k := range sample.Metric {
							labels = append(labels, string(k))
						}
						return labels
					}())
				continue
			}

			// Extract node name from instance label
			// Instance can be in formats like "node1:9400" or just "node1"
			nodeName := string(instance)
			if colonIdx := strings.Index(nodeName, ":"); colonIdx > 0 {
				nodeName = nodeName[:colonIdx]
			}

			uuidToNode[string(uuid)] = nodeName

			klog.V(3).InfoS("Mapped GPU UUID to node",
				"UUID", uuid,
				"instance", instance,
				"nodeName", nodeName)
		}
	}

	klog.V(2).InfoS("Built GPU UUID to node mapping from DCGM metrics",
		"mappingCount", len(uuidToNode),
		"mappings", uuidToNode)

	return uuidToNode, nil
}
