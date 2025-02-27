[![Go Report Card](https://goreportcard.com/badge/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/elevated-systems/compute-gardener-scheduler) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Build/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases)

# Compute Gardener Scheduler

The Compute Gardener Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use electricity pricing.

## Features

- **Carbon-Aware Scheduling**: Schedule pods based on real-time carbon intensity data from Electricity Map API
- **Price-Aware Scheduling**: Schedule pods based on time-of-use electricity pricing schedules
- **Flexible Configuration**: Extensive configuration options for fine-tuning scheduler behavior
- **Pod-Level Controls**: Pods can opt-out or specify custom thresholds via annotations
- **Caching**: Built-in caching of API responses to limit external API calls
- **Observability**: Prometheus metrics for monitoring carbon intensity, pricing, and scheduling decisions

## Configuration

### Prerequisites

- **metrics-server**: The scheduler requires [metrics-server](https://github.com/kubernetes-sigs/metrics-server) to be installed and running in your cluster to collect CPU metrics for power estimation. Without metrics-server, power-related metrics will report as 0.

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
CARBON_INTENSITY_THRESHOLD=200.0        # Optional: Base carbon intensity threshold (gCO2/kWh)

# Pricing Configuration
PRICING_ENABLED=false                  # Optional: Enable TOU pricing
PRICING_PROVIDER=tou                   # Optional: Default is 'tou'
PRICING_SCHEDULES_PATH=/path/to/schedules.yaml  # Optional: Path to pricing schedules

# Node Power Configuration
NODE_DEFAULT_IDLE_POWER=100.0          # Default idle power consumption in watts
NODE_DEFAULT_MAX_POWER=400.0           # Default maximum power consumption in watts
NODE_POWER_CONFIG_worker1=idle:50,max:300  # Node-specific power settings

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

# Set custom carbon intensity threshold
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"

# Set custom price threshold
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"

# Set custom maximum scheduling delay (e.g. "12h", "30m", "1h30m")
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"
```

## Metrics

The scheduler exports Prometheus metrics through the kube-scheduler's secure endpoints:

- Health checks on port 10259 (HTTPS)
- Metrics on port 10260 (HTTPS)

The following metrics are available at https://[scheduler-pod]:10260/metrics:

- `scheduler_compute_gardener_carbon_intensity`: Current carbon intensity (gCO2eq/kWh) for a given region
- `scheduler_compute_gardener_electricity_rate`: Current electricity rate ($/kWh) for a given location
- `scheduler_compute_gardener_scheduling_attempt_total`: Number of attempts to schedule pods by result
- `scheduler_compute_gardener_pod_scheduling_duration_seconds`: Latency for scheduling attempts
- `scheduler_compute_gardener_estimated_savings`: Estimated savings from scheduling (carbon, cost)
- `scheduler_compute_gardener_price_delay_total`: Number of scheduling delays due to price thresholds
- `scheduler_compute_gardener_node_cpu_usage_cores`: CPU usage on nodes at baseline and completion
- `scheduler_compute_gardener_node_power_estimate_watts`: Estimated node power consumption
- `scheduler_compute_gardener_job_energy_usage_kwh`: Estimated energy usage for completed jobs
- `scheduler_compute_gardener_job_carbon_emissions_grams`: Estimated carbon emissions for completed jobs
- `scheduler_compute_gardener_scheduling_efficiency`: Scheduling efficiency metrics (carbon/cost improvements)

All metrics are automatically registered with the kube-scheduler's metrics registry and exposed through its secure metrics endpoint. The scheduler deployment includes Prometheus annotations for automatic service discovery:

```yaml
annotations:
  prometheus.io/scrape: 'true'
  prometheus.io/port: '10260'
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
   - Get current rate from TOU schedule
   - Compare against threshold
4. Get current carbon intensity
5. Compare against threshold
6. Make scheduling decision

## Development

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
