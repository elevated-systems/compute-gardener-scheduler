[![Go Report Card](https://goreportcard.com/badge/github.com/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/github.com/elevated-systems/compute-gardener-scheduler) [![Test Coverage](https://byob.yarr.is/elevated-systems/compute-gardener-scheduler/coverage)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/test.yml) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Release%20Charts/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/release-charts.yml) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE)

<img src="./docs/img/logo.png" width="400" height="400" />

# Compute Gardener Scheduler

The Compute Gardener Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use electricity pricing.

This project builds on the [Kubernetes Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins) framework to provide specialized energy and cost-aware scheduling capabilities.

## Core Features

- **Carbon-Aware Scheduling**: Schedule pods based on real-time carbon intensity data (requires [Electricity Maps API](https://api-portal.electricitymaps.com/) key)
- **Price-Aware Scheduling**: Schedule pods based on time-of-use (TOU) electricity pricing
- **Pod-Level Controls**: Customize scheduling via annotations and thresholds
- **Hardware Power Profiling**: Accurate power modeling with datacenter PUE consideration based on NFD labels, Kepler data or sensed HW
- **Region Mapping**: Automatically map most major cloud provider regions to their corresponding Electricity Maps API carbon intensity regions.
- **Grafana Dashboard**: View workload and scheduler performance metrics in simple visualizations.
- **Energy Budget Tracking**: Define and monitor energy usage limits for workloads

## Getting Started
Please see our new [getting started guide](./docs/getting-started.md) for an in-depth workflow, installing the scheduler, getting carbon awarenes, price awareness and metrics working.

## Quick Start

### Install Using Helm

```bash
# Add the Helm repository
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update

# Standard installation with metrics enabled
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
  
# Installation without metrics (for clusters without Prometheus Operator)
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set metrics.enabled=false \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

For more options, see the [Helm chart README](manifests/install/charts/compute-gardener-scheduler/README.md). Also, if you prefer a raw manifest based install, see our [all-in-one](./manifests/compute-gardener-scheduler/) manifests.

## Additional Features

- **GPU & CPU Power Monitoring**: Integrated with DCGM and CPU frequency monitoring
- **Caching**: Built-in caching of API responses to limit external API calls
- **Observability**: Comprehensive Prometheus metrics
- **Workload-Specific Optimizations**: Different policies for workload types (batch, service, stateful)
- **Namespace-Level Policies**: Define energy policies at namespace level

## Architecture

The scheduler consists of these key components:

1. **Main Scheduler Plugin**: Implements Kubernetes scheduler framework interfaces (PreFilter and Filter for now)
2. **Carbon Scheduler**: Considers carbon intensity in scheduling
3. **Hardware Profiler**: Models power consumption with PUE considerations
4. **TOU Scheduler**: Manages time-of-use pricing schedules
5. **API Client**: Communicates with Electricity Maps API
6. **Cache**: Provides caching of API responses to reduce external API calls
7. **Energy Budget Tracker**: Monitors energy usage against budgets
8. **Energy Policy Webhook**: Applies namespace-level energy policies

### Scheduling Logic

The scheduler follows this decision flow:

1. **PreFilter Stage**: 
   - Check if pod has exceeded maximum scheduling delay or has opted-out
   - If pricing is enabled:
     - Get current rate from pricing implementation
     - Compare against threshold
   - If carbon-aware scheduling is enabled:
     - Get current carbon intensity from implementation
     - Compare against threshold

2. **Filter Stage**:
    - Recheck if pod has exceeded maximum scheduling delay or has opeted-out
   - If hardware efficiency controls are enabled:
     - Calculate effective power consumption with PUE
     - Filter nodes based on power limits and efficiency

3. **Post-scheduling**:
   - Track energy usage over time
   - Compare against energy budgets
   - Take configurable actions when budgets are exceeded

### Time-of-Use Pricing Schedules

```yaml
schedules:
  - name: "california-pge"          # Schedule name
    dayOfWeek: "1-5"                # Days (1=Monday, 5=Friday)
    startTime: "16:00"              # Start time (24h format)
    endTime: "21:00"                # End time (24h format)
    timezone: "America/Los_Angeles" # IANA timezone
    peakRate: 0.30                  # Peak rate in $/kWh (optional)
    offPeakRate: 0.10               # Off-peak rate (optional)
```

### Pod Annotations

```yaml
# Basic scheduling controls
compute-gardener-scheduler.kubernetes.io/skip: "true"                      # Opt out
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0" # Custom threshold
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"           # Custom price threshold
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"       # Custom delay

# Energy budget
compute-gardener-scheduler.kubernetes.io/energy-budget-kwh: "5.0"          # Energy budget in kWh
compute-gardener-scheduler.kubernetes.io/gpu-workload-type: "inference"    # GPU workload type
```

## Hardware Power Profiles

The scheduler uses hardware-specific power profiles to accurately estimate energy consumption.

Optional: For optimal performance (skip run-time checks), you can label your nodes with their hardware details:

```bash
# Label all nodes (optional)
./hack/label-node-hardware.sh
```

Sample profile:

```yaml
# Global defaults 
defaultPUE: 1.15      # Default datacenter PUE
defaultGPUPUE: 1.2    # Default GPU-specific PUE

# CPU profiles
cpuProfiles:
  "Intel(R) Xeon(R) Platinum 8275CL":
    idlePower: 10.5   # Idle power (watts)
    maxPower: 120.0   # Max power (watts)

# GPU profiles with workload types
gpuProfiles:
  "NVIDIA A100":
    idlePower: 25.0
    maxPower: 400.0
    workloadTypes:
      inference: 0.6  # 60% of max power
      training: 1.0   # Full power
```

## Metrics

Key metrics available:

**Carbon and Pricing**
- `scheduler_compute_gardener_carbon_intensity`: Current carbon intensity
- `scheduler_compute_gardener_electricity_rate`: Current electricity rate
- `scheduler_compute_gardener_carbon_delay_total`: Scheduling delays due to carbon

**Energy Budget**
- `scheduler_compute_gardener_energy_budget_usage_percent`: Budget usage percentage
- `scheduler_compute_gardener_job_energy_usage_kwh`: Energy usage for completed jobs

**Hardware**
- `scheduler_compute_gardener_node_power_estimate_watts`: Power consumption


### Namespace-Level Energy Policies

Enable with namespace label:

```yaml
labels:
  compute-gardener-scheduler.kubernetes.io/energy-policies: "enabled"
```

Set default policies with annotations:

```yaml
annotations:
  compute-gardener-scheduler.kubernetes.io/policy-carbon-intensity-threshold: "200"
  compute-gardener-scheduler.kubernetes.io/policy-energy-budget-kwh: "10"
```

## Development

We'd be thrilled to have your dev assistance and shared wisdom!

Peruse our [issues](https://github.com/elevated-systems/compute-gardener-scheduler/issues) to see if anything speaks to you or you have any bugs or suggested features.

```bash
# Build
make build

# Build and Push Images (scheduler and node-exporter)
# Be sure to set your registry details in Makefile first
make build-push-images

# Run tests
make unit-test

# Run tests with coverage report
make unit-test-coverage
```

## Contributing

Please see the [contributing guide](CONTRIBUTING.md) for guidelines on how to contribute to this project.
