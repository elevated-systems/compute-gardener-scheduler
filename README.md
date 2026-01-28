[![Go Report Card](https://goreportcard.com/badge/github.com/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/github.com/elevated-systems/compute-gardener-scheduler) [![Test Coverage](https://byob.yarr.is/elevated-systems/compute-gardener-scheduler/coverage)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/test.yml) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Release%20Charts/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/release-charts.yml) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE)

<img src="./docs/img/logo.png" width="400" height="400" />

# Compute Gardener Scheduler

A Kubernetes scheduler plugin that makes your cluster carbon and cost-aware. Delay flexible workloads to cleaner, cheaper times—automatically.

Built on the [Kubernetes Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins) framework.

## Why Compute Gardener?

Many Kubernetes workloads don't need to run *right now*. ML training jobs, batch processing, CI pipelines—these can often wait minutes or hours. Compute Gardener uses that flexibility to:

- **Reduce carbon emissions** by scheduling during low-carbon periods using real-time grid data
- **Lower electricity costs** by avoiding peak pricing windows
- **Track actual savings** with comprehensive metrics and a Grafana dashboard

The scheduler monitors carbon intensity (via [Electricity Maps](https://api-portal.electricitymaps.com/)) and electricity prices, holding pods until conditions improve—or releasing them when your configured max delay is reached.

## Features

- **Carbon-Aware Scheduling**: Real-time carbon intensity data with configurable thresholds
- **Price-Aware Scheduling**: Time-of-use electricity pricing with peak/off-peak detection
- **Accurate Power Modeling**: Hardware-specific profiles for CPUs and GPUs, datacenter PUE, Kepler integration
- **Energy Budgets**: Set kWh limits per workload with configurable enforcement actions
- **Cloud Region Mapping**: Auto-maps AWS, GCP, Azure regions to carbon intensity zones
- **Observability**: Prometheus metrics, Grafana dashboard, per-job emissions tracking
- **Namespace Policies**: Apply default thresholds and budgets at namespace level

## Quick Start

```bash
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update

helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

Then schedule pods with `schedulerName: compute-gardener-scheduler`. See the [Getting Started Guide](./docs/getting-started.md) for detailed setup.

## Dry-Run Mode (Preview)

Not ready to install a secondary scheduler? **Dry-run mode** lets you evaluate potential savings without affecting your workloads.

It installs a lightweight admission webhook that evaluates every pod using the same carbon/price logic, recording what *would* happen via Prometheus metrics. Track potential savings, validate data quality for your region, and build confidence—all before committing to the scheduler.

```bash
helm install compute-gardener ./manifests/install/charts/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set dryRun.enabled=true \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

See [Dry-Run Mode Documentation](./docs/dry-run-mode.md) for details.

## Documentation

- [Getting Started Guide](./docs/getting-started.md) - Installation and configuration
- [Carbon Calculations](./docs/carbon-calculations.md) - How emissions and savings are computed
- [Hardware Profiles](./docs/hardware-profiles.md) - Power modeling configuration
- [Project Roadmap](./docs/roadmap.md) - What's next

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) and our [open issues](https://github.com/elevated-systems/compute-gardener-scheduler/issues).
