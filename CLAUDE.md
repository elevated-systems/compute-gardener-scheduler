# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

**Always use Makefile targets before ad hoc go build/test commands:**

```bash
# Build the scheduler binary
make build

# Run unit tests with envtest setup
make unit-test

# Run tests with coverage report
make unit-test-coverage

# Build and push container image (set registry in Makefile first)
make build-push-image

# Verify code quality (gofmt, gomod, CRD generation)
make verify

# Update go modules
make update-gomod
```

**Alternative test execution:**
- Individual test: `make unit-test ARGS="-run TestSpecificFunction"`
- If Makefile targets fail, use: `go test ./cmd/... ./pkg/... ./apis/...`

## Architecture Overview

This project is a **Kubernetes scheduler plugin** that extends the default scheduler with carbon and price-aware scheduling capabilities. It's forked from kubernetes-sigs/scheduler-plugins.

### Core Components

**Main Plugin Structure:**
- `cmd/scheduler/main.go` - Entry point that registers the ComputeGardenerScheduler plugin
- `pkg/computegardener/scheduler.go` - Main plugin implementing Kubernetes scheduler framework interfaces (PreFilter, Filter)

**Key Subsystems:**
- **Carbon Scheduler** (`pkg/computegardener/carbon/`) - Integrates with Electricity Maps API for real-time carbon intensity data
- **Price Scheduler** (`pkg/computegardener/price/`) - Time-of-use electricity pricing with configurable schedules
- **Hardware Profiler** (`pkg/computegardener/config/types.go`) - Power modeling with PUE considerations, CPU/GPU profiles, and cloud instance mappings
- **Metrics System** (`pkg/computegardener/metrics/`) - Power consumption tracking, Prometheus integration, DCGM/Kepler support
- **Region Mapper** (`pkg/computegardener/regionmapper/`) - Maps cloud provider regions to Electricity Maps regions
- **API Cache** (`pkg/computegardener/cache/`) - Caches external API responses to reduce calls
- **Pod Completion Tracker** (`pkg/computegardener/pod_completion.go`) - Energy budget tracking and workload energy usage

### Scheduling Decision Flow

**PreFilter Stage:**
1. Check pod scheduling delay limits and opt-out annotations
2. Evaluate pricing thresholds if price-aware scheduling enabled
3. Evaluate carbon intensity thresholds if carbon-aware scheduling enabled

**Filter Stage:**
1. Recheck scheduling delay and opt-out conditions
2. Apply hardware efficiency filtering based on power consumption models
3. Calculate effective power with PUE factors

### Configuration Architecture

**Power Profiles** (`pkg/computegardener/config/types.go`):
- Hardware-specific power models for CPUs, GPUs, memory
- Cloud instance type mappings to hardware components
- NFD (Node Feature Discovery) label integration
- Workload type classifications for GPU power scaling

**Scheduling Policies:**
- Pod-level annotations for custom thresholds and opt-outs
- Namespace-level energy policies and budgets
- Time-of-use pricing schedules with timezone support

## Key Files and Their Purpose

- `pkg/computegardener/scheduler.go` - Main scheduler plugin logic
- `pkg/computegardener/config/types.go` - Configuration types and power profiles
- `pkg/computegardener/metrics_collection.go` - Power/energy metrics collection
- `pkg/computegardener/pod_completion.go` - Energy budget enforcement
- `pkg/computegardener/regionmapper/` - Cloud provider region mapping
- `apis/scheduling/v1alpha1/types.go` - Custom Kubernetes API types

## Development Setup

**Dependencies:**
- Go 1.24.0+
- Kubernetes 1.31.2 APIs
- Controller-runtime for Kubernetes integration
- Prometheus client for metrics
- Carbon Aware CloudInfo for region mapping

**Testing Environment:**
- Uses envtest for Kubernetes API integration testing
- Prometheus and DCGM mocks for metrics testing
- Extensive unit tests with coverage tracking

**Development Assistance:**
- When crafting overviews and commit log messages, please omit "co-authorship" and other AI/agent/claude references

## Important Annotations

**Pod Scheduling Control:**
- `compute-gardener-scheduler.kubernetes.io/skip: "true"` - Opt out of scheduling
- `compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"` - Custom carbon threshold
- `compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"` - Custom price threshold
- `compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"` - Max scheduling delay
- `compute-gardener-scheduler.kubernetes.io/energy-budget-kwh: "5.0"` - Energy budget

**Namespace Policies:**
- Label: `compute-gardener-scheduler.kubernetes.io/energy-policies: "enabled"`
- Annotation: `compute-gardener-scheduler.kubernetes.io/policy-carbon-intensity-threshold: "200"`