[![Go Report Card](https://goreportcard.com/badge/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/elevated-systems/compute-gardener-scheduler) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Build/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases)

# Compute Gardener Scheduler

The Compute Gardener Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use electricity pricing.

## Features

- **Carbon-Aware Scheduling** (Optional): Schedule pods based on real-time carbon intensity data from Electricity Map API or implement your own intensity source
- **Price-Aware Scheduling** (Optional): Schedule pods based on time-of-use electricity pricing schedules or implement your own pricing source
- **Flexible Configuration**: Extensive configuration options for fine-tuning scheduler behavior
- **Pod-Level Controls**: Pods can opt-out or specify custom thresholds via annotations
- **Caching**: Built-in caching of API responses to limit external API calls
- **Observability**: Prometheus metrics for monitoring carbon intensity, pricing, and scheduling decisions

## Configuration

### Prerequisites

- **Prometheus** (Optional): While not required for the scheduler to function, Prometheus is recommended for monitoring the scheduler's metrics. The scheduler exposes various metrics that help validate its behavior and performance.
- **Metrics Server** (Optional): Required for collecting real-time pod resource usage metrics to calculate accurate energy usage and carbon footprint. Without metrics-server, energy estimates will be less accurate and dependable.

### Environment Variables

The scheduler can be configured using the following environment variables:

```bash
# API Configuration
ELECTRICITY_MAP_API_KEY=<your-api-key>  # Required: Your API key for Electricity Map API
ELECTRICITY_MAP_API_URL=<api-url>       # Optional: Default is https://api.electricitymap.org/v3/carbon-intensity/latest?zone=
ELECTRICITY_MAP_API_REGION=<region>     # Optional: Default is US-CAL-CISO
API_TIMEOUT=10s                         # Optional: API request timeout
API_MAX_RETRIES=3                       # Optional: Maximum API retry attempts
API_RETRY_DELAY=1s                      # Optional: Delay between retries
API_RATE_LIMIT=10                       # Optional: API rate limit per minute
CACHE_TTL=5m                           # Optional: Cache TTL for API responses
MAX_CACHE_AGE=1h                       # Optional: Maximum age of cached data

# Scheduling Configuration
MAX_SCHEDULING_DELAY=24h               # Optional: Maximum pod scheduling delay
ENABLE_POD_PRIORITIES=false            # Optional: Enable pod priority-based scheduling

# Carbon Configuration
CARBON_ENABLED=true                    # Optional: Enable carbon-aware scheduling (default: true)
CARBON_INTENSITY_THRESHOLD=200.0       # Optional: Base carbon intensity threshold (gCO2/kWh)

# Pricing Configuration
PRICING_ENABLED=false                  # Optional: Enable TOU pricing
PRICING_PROVIDER=tou                   # Optional: Default is 'tou'
PRICING_SCHEDULES_PATH=/path/to/schedules.yaml  # Optional: Path to pricing schedules

# Node Power Configuration
NODE_DEFAULT_IDLE_POWER=100.0          # Default idle power consumption in watts
NODE_DEFAULT_MAX_POWER=400.0           # Default maximum power consumption in watts
NODE_POWER_CONFIG_worker1=idle:50,max:300  # Node-specific power settings

# Metrics Collection Configuration
METRICS_SAMPLING_INTERVAL=30s          # Interval for collecting pod metrics (e.g. "30s", "1m")
MAX_SAMPLES_PER_POD=500                # Maximum number of metrics samples to store per pod
COMPLETED_POD_RETENTION=1h             # How long to keep metrics for completed pods
DOWNSAMPLING_STRATEGY=timeBased        # Strategy for downsampling metrics (lttb, timeBased, minMax)

# Observability Configuration
LOG_LEVEL=info                        # Optional: Logging level
ENABLE_TRACING=false                  # Optional: Enable tracing
```

### Time-of-Use Pricing Schedules

Time-of-use pricing schedules are defined in a YAML file:

```yaml
schedules:
  # Monday-Friday peak pricing periods (4pm-9pm)
  - dayOfWeek: "1-5"
    startTime: "16:00"
    endTime: "21:00"
    peakRate: 0.30    # Peak electricity rate in $/kWh
    offPeakRate: 0.10 # Off-peak electricity rate in $/kWh
  # Weekend peak pricing periods (1pm-7pm)
  - dayOfWeek: "0,6"
    startTime: "13:00"
    endTime: "19:00"
    peakRate: 0.30    # Peak electricity rate in $/kWh
    offPeakRate: 0.10 # Off-peak electricity rate in $/kWh
```

### Pod Annotations

Pods can control scheduling behavior using the following annotations:

```yaml
# Opt out of compute-gardener scheduling
compute-gardener-scheduler.kubernetes.io/skip: "true"

# Disable carbon-aware scheduling for this pod only
compute-gardener-scheduler.kubernetes.io/carbon-enabled: "false"

# Set custom carbon intensity threshold
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"

# Set custom price threshold
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"

# Set custom maximum scheduling delay (e.g. "12h", "30m", "1h30m")
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"
```

## Installation

### Using Helm

The recommended way to deploy the Compute Gardener Scheduler is using Helm:

```bash
# Add the Helm repository
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update

# Install the chart
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace kube-system \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

For more detailed installation and configuration options, see the [Helm chart README](manifests/install/charts/compute-gardener-scheduler/README.md).

### Using YAML Manifests

Alternatively, you can deploy using the provided YAML manifests:

```bash
# First, update the API key in the manifest
# Then apply the manifest
kubectl apply -f manifests/compute-gardener-scheduler/compute-gardener-scheduler.yaml
```

## Metrics

The scheduler exports Prometheus metrics through a dedicated Service and ServiceMonitor:

- Health checks on port 10259 (HTTPS) path /healthz
- Metrics on port 10259 (HTTPS) path /metrics

The following metrics are available:

- `scheduler_compute_gardener_carbon_intensity`: Current carbon intensity (gCO2eq/kWh) for a given region
- `scheduler_compute_gardener_electricity_rate`: Current electricity rate ($/kWh) for a given location
- `scheduler_compute_gardener_scheduling_attempt_total`: Number of attempts to schedule pods by result
- `scheduler_compute_gardener_pod_scheduling_duration_seconds`: Latency for scheduling attempts
- `scheduler_compute_gardener_estimated_savings`: Estimated savings from scheduling (carbon, cost)
- `scheduler_compute_gardener_price_delay_total`: Number of scheduling delays due to price thresholds
- `scheduler_compute_gardener_carbon_delay_total`: Number of scheduling delays due to carbon intensity thresholds
- `scheduler_compute_gardener_node_cpu_usage_cores`: CPU usage on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_memory_usage_bytes`: Memory usage on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_gpu_usage`: GPU utilization on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_power_estimate_watts`: Estimated node power consumption (baseline, current, final)
- `scheduler_compute_gardener_metrics_samples_stored`: Number of pod metrics samples currently stored in cache
- `scheduler_compute_gardener_metrics_cache_size`: Number of pods being tracked in metrics cache
- `scheduler_compute_gardener_job_energy_usage_kwh`: Estimated energy usage for completed jobs
- `scheduler_compute_gardener_job_carbon_emissions_grams`: Estimated carbon emissions for completed jobs
- `scheduler_compute_gardener_scheduling_efficiency`: Scheduling efficiency metrics (carbon/cost improvements)

### Metrics Collection

The deployment includes both a Service and ServiceMonitor for Prometheus integration:

```yaml
# Service exposes the metrics endpoint
apiVersion: v1
kind: Service
metadata:
  name: compute-gardener-scheduler-metrics
  namespace: kube-system
  labels:
    component: scheduler
    tier: control-plane
spec:
  ports:
  - name: https
    port: 10259
    targetPort: 10259
    protocol: TCP
  selector:
    component: scheduler
    tier: control-plane

# ServiceMonitor configures Prometheus to scrape metrics
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: compute-gardener-scheduler-monitor
  namespace: cattle-monitoring-system  # Adjust to your Prometheus namespace
spec:
  selector:
    matchLabels:
      component: scheduler
      tier: control-plane
  endpoints:
  - port: https 
    scheme: https
    path: /metrics
    interval: 30s
```

Additionally, the Pod template includes Prometheus annotations for environments that use annotation-based discovery:

```yaml
annotations:
  prometheus.io/scrape: 'true'
  prometheus.io/port: '10259'
  prometheus.io/scheme: 'https'
  prometheus.io/path: '/metrics'
```

## Architecture

The scheduler consists of several key components:

1. **Main Scheduler**: Implements the Kubernetes scheduler framework interfaces
2. **API Client**: Handles communication with Electricity Map API
3. **Cache**: Provides caching of API responses to reduce external API calls
4. **TOU Scheduler**: Manages time-of-use pricing schedules

### Scheduling Logic

The scheduler follows this decision flow:

1. Check if pod has exceeded maximum scheduling delay
2. Check for opt-out annotations
3. If pricing is enabled:
   - Get current rate from pricing implementation
   - Compare against threshold
4. If carbon-aware scheduling is enabled:
   - Get current carbon intensity from implementation
   - Compare against threshold
5. Make scheduling decision

## Development

### Adding a New Carbon Intensity Source

To add a new carbon intensity source:

1. Create a new package under `carbon/`
2. Implement the `carbon.Implementation` interface:
   ```go
   type Implementation interface {
       // GetCurrentIntensity returns the current carbon intensity for the configured region
       GetCurrentIntensity(ctx context.Context) (float64, error)
       
       // CheckIntensityConstraints checks if current carbon intensity exceeds pod's threshold
       CheckIntensityConstraints(ctx context.Context, pod *v1.Pod) *framework.Status
   }
   ```
3. Add the implementation to the carbon factory
4. Add tests for the new implementation

### Running Tests

```bash
# Run all tests
make test

# Run specific test
go test -v ./pkg/computegardener/... -run TestName

# Run tests with coverage
make test-coverage
```

### Adding a New Pricing Implementation

To add a new pricing implementation:

1. Create a new package under `pricing/`
2. Implement the `pricing.Implementation` interface
3. Add the implementation to the pricing factory
4. Add tests for the new implementation

## Contributing

Please see the [contributing guide](CONTRIBUTING.md) for guidelines on how to contribute to this project.
