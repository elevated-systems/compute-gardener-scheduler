[![Go Report Card](https://goreportcard.com/badge/github.com/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/github.com/elevated-systems/compute-gardener-scheduler) [![Test Coverage](https://byob.yarr.is/elevated-systems/compute-gardener-scheduler/coverage)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/test.yml) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Release%20Charts/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/release-charts.yml) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE)

<img src="./docs/img/logo.png" width="400" height="400" />

# Compute Gardener Scheduler

A Kubernetes scheduler plugin enabling carbon and price-aware scheduling based on real-time carbon intensity data and time-of-use electricity pricing. Built on the [Kubernetes Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins) framework.

## Features

- **Carbon-Aware Scheduling**: Schedule pods based on real-time carbon intensity ([Electricity Maps API](https://api-portal.electricitymaps.com/))
- **Price-Aware Scheduling**: Time-of-use (TOU) electricity pricing support
- **Hardware Power Profiling**: Accurate power modeling with datacenter PUE via NFD labels, Kepler, or runtime detection
- **Region Mapping**: Auto-map major cloud provider regions to carbon intensity zones
- **Energy Budgets**: Define and monitor energy usage limits per workload
- **Grafana Dashboard**: Visualize scheduler metrics, carbon intensity, and savings
- **Namespace Policies**: Apply energy policies at the namespace level

## Quick Start

```bash
# Add the Helm repository
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update

# Install with carbon awareness
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

For detailed setup, see our [getting started guide](./docs/getting-started.md). For all Helm options, see the [chart README](manifests/install/charts/compute-gardener-scheduler/README.md).

## Dry-Run Mode (Preview)

Not ready to install a secondary scheduler? **Dry-run mode** lets you evaluate potential savings without affecting workloads.

Dry-run mode installs a lightweight admission webhook that:
- Evaluates every pod using the same carbon/price logic as the scheduler
- Records what *would* happen via Prometheus metrics or pod annotations
- Tracks completed pods to calculate actual savings potential
- Scopes to specific namespaces for safe experimentation

This answers key questions before committing: *How much could we actually save? Is the carbon data reliable for our region? What would the scheduler do with our workloads?*

```bash
# Install dry-run mode only
helm install compute-gardener ./manifests/install/charts/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set dryRun.enabled=true \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

See [Dry-Run Mode Documentation](./docs/dry-run-mode.md) for details.

## Pod Annotations

Control scheduling behavior per-pod:

```yaml
compute-gardener-scheduler.kubernetes.io/skip: "true"                        # Opt out
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0" # gCO2eq/kWh
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"             # $/kWh
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"         # Max wait
compute-gardener-scheduler.kubernetes.io/energy-budget-kwh: "5.0"            # Energy limit
```

## Metrics

Key Prometheus metrics:

- `compute_gardener_scheduler_carbon_intensity` / `electricity_rate`: Current conditions
- `compute_gardener_scheduler_estimated_savings`: Carbon/cost savings with `method` label (timeseries or simple)
- `compute_gardener_scheduler_job_carbon_emissions_grams`: Actual emissions per job
- `compute_gardener_scheduler_energy_budget_usage_percent`: Budget utilization

See [Carbon Calculations](./docs/carbon-calculations.md) for methodology details.

## Documentation

- [Getting Started Guide](./docs/getting-started.md)
- [Carbon Calculations](./docs/carbon-calculations.md)
- [Hardware Profiles](./docs/hardware-profiles.md)
- [Project Roadmap](./docs/roadmap.md)

## Development

```bash
make build           # Build binaries
make build-push-image # Build and push container image
make unit-test       # Run tests
make unit-test-coverage # Tests with coverage report
```

See our [issues](https://github.com/elevated-systems/compute-gardener-scheduler/issues) for ways to contribute.

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
