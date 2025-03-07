[![Go Report Card](https://goreportcard.com/badge/github.com/elevated-systems/compute-gardener-scheduler)](https://goreportcard.com/report/github.com/elevated-systems/compute-gardener-scheduler) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/blob/master/LICENSE) [![Build Status](https://github.com/elevated-systems/compute-gardener-scheduler/workflows/Release%20Charts/badge.svg)](https://github.com/elevated-systems/compute-gardener-scheduler/actions/workflows/release-charts.yml) [![GitHub release](https://img.shields.io/github/release/elevated-systems/compute-gardener-scheduler/all.svg?style=flat)](https://github.com/elevated-systems/compute-gardener-scheduler/releases)

# Compute Gardener Scheduler

The Compute Gardener Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use electricity pricing.

## Features

- **Carbon-Aware Scheduling** (Optional): Schedule pods based on real-time carbon intensity data from Electricity Map API or implement your own intensity source
- **Price-Aware Scheduling** (Optional): Schedule pods based on time-of-use electricity pricing schedules or implement your own pricing source
- **Energy Budget Tracking**: Define and monitor energy usage limits for workloads with configurable actions when exceeded
- **GPU Workload Classification**: Optimize power modeling based on workload type (inference, training, rendering)
- **Namespace-Level Policies**: Define energy policies at namespace level to automatically apply to all pods
- **Workload-Type Optimization**: Different policies for batch jobs, services, and stateful workloads
- **Hardware Power Profiling**: Accurate power modeling with datacenter PUE consideration
- **CPU Frequency Monitoring** (Optional): DaemonSet for accurate CPU power estimation, by node, based on dynamic frequency scaling
- **Pod-Level Controls**: Pods can opt-out or specify custom thresholds via annotations
- **Caching**: Built-in caching of API responses to limit external API calls
- **Observability**: Comprehensive Prometheus metrics for monitoring energy usage, carbon intensity, and cost savings

## Configuration

### Recommended Components

- **Metrics Server**: Highly recommended but not strictly required. Without Metrics Server, the scheduler won't be able to collect real-time node utilization data, resulting in less accurate energy usage estimates. Core carbon-aware and price-aware scheduling will still function using requested resources rather than actual usage.

- **Prometheus**: Highly recommended but not strictly required. Without Prometheus, you won't be able to visualize scheduler performance metrics or validate carbon/cost savings. The scheduler will continue to function, but you'll miss valuable insights into its operation and won't have visibility into the actual emissions and cost reductions achieved.

- **CPU Frequency Exporter**: Optional component that significantly improves power estimation accuracy. This DaemonSet monitors the actual CPU frequencies of each node, providing more precise data for power calculations. Without it, the scheduler will estimate power based on static CPU models, which may not accurately reflect dynamic frequency scaling behaviors.

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
HARDWARE_PROFILES_PATH=/path/to/hardware-profiles.yaml  # Path to hardware profiles ConfigMap

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
# Basic scheduling controls
compute-gardener-scheduler.kubernetes.io/skip: "true"                      # Opt out of compute-gardener scheduling
compute-gardener-scheduler.kubernetes.io/carbon-enabled: "false"           # Disable carbon-aware scheduling for this pod
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0" # Set custom carbon intensity threshold
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.12"           # Set custom price threshold
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"       # Set custom maximum scheduling delay

# Energy budget controls
compute-gardener-scheduler.kubernetes.io/energy-budget-kwh: "5.0"          # Set energy budget in kilowatt-hours
compute-gardener-scheduler.kubernetes.io/energy-budget-action: "notify"    # Action when budget exceeded: log, notify, annotate, label

# Hardware efficiency controls
compute-gardener-scheduler.kubernetes.io/max-power-watts: "300.0"          # Maximum power consumption threshold
compute-gardener-scheduler.kubernetes.io/min-efficiency: "0.8"             # Minimum efficiency requirement
compute-gardener-scheduler.kubernetes.io/gpu-workload-type: "inference"    # GPU workload type (inference, training, rendering)

# PUE configuration 
compute-gardener-scheduler.kubernetes.io/pue: "1.2"                        # Power Usage Effectiveness for datacenter
compute-gardener-scheduler.kubernetes.io/gpu-pue: "1.15"                   # GPU-specific Power Usage Effectiveness

# Node hardware labels (for improved energy profiles)
node.kubernetes.io/cpu-model: "Intel(R) Xeon(R) Platinum 8275CL"
node.kubernetes.io/gpu-model: "NVIDIA A100"
```

### Namespace-Level Energy Policies

The scheduler supports defining energy policies at the namespace level, which automatically apply to all pods in the namespace. This feature requires deploying the Energy Policy Webhook (included in the repo).

To enable namespace-level policies:

1. Label the namespace to enable energy policies:
   ```yaml
   labels:
     compute-gardener-scheduler.kubernetes.io/energy-policies: "enabled"
   ```

2. Add policy annotations to the namespace:
   ```yaml
   annotations:
     # Default carbon intensity threshold for all pods in this namespace
     compute-gardener-scheduler.kubernetes.io/policy-carbon-intensity-threshold: "200"
     
     # Default energy budget (in kWh) for all pods in this namespace
     compute-gardener-scheduler.kubernetes.io/policy-energy-budget-kwh: "10"
     
     # Default action when budget is exceeded
     compute-gardener-scheduler.kubernetes.io/policy-energy-budget-action: "notify"
   ```

3. Add workload-specific policy overrides:
   ```yaml
   annotations:
     # Energy budget for batch jobs (like training jobs)
     compute-gardener-scheduler.kubernetes.io/workload-batch-policy-energy-budget-kwh: "20"
     
     # GPU workload type for batch jobs
     compute-gardener-scheduler.kubernetes.io/workload-batch-policy-gpu-workload-type: "training"
     
     # Price threshold for service workloads (like APIs, web servers)
     compute-gardener-scheduler.kubernetes.io/workload-service-policy-price-threshold: "0.15"
     
     # Energy budget for service workloads
     compute-gardener-scheduler.kubernetes.io/workload-service-policy-energy-budget-kwh: "5"
   ```

This allows you to set up clean vs. dirty computing zones in your cluster, with different energy policies applied automatically.

## Installation

### Using Helm

The recommended way to deploy the Compute Gardener Scheduler is using Helm:

```bash
# Add the Helm repository
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update

# Install the scheduler
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace kube-system \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

To enable namespace-level energy policies, deploy the Energy Policy Webhook:

```bash
# First, generate webhook TLS certificates
./hack/generate-webhook-certs.sh

# Install the webhook
helm install energy-policy-webhook compute-gardener/energy-policy-webhook \
  --namespace kube-system \
  --set caBundle=$(cat webhooks/certs/ca.pem | base64 | tr -d '\n')
```

To uninstall:

```bash
helm uninstall compute-gardener-scheduler --namespace kube-system
helm uninstall energy-policy-webhook --namespace kube-system
```

For more detailed installation and configuration options, see the [Helm chart README](manifests/install/charts/compute-gardener-scheduler/README.md).

### Using YAML Manifests

Alternatively, you can deploy using the provided YAML manifests:

```bash
# Scheduler deployment
kubectl apply -f manifests/compute-gardener-scheduler/compute-gardener-scheduler.yaml

# Energy Policy Webhook (optional)
# First, generate TLS certificates and create a Secret
./hack/generate-webhook-certs.sh
kubectl create secret tls energy-policy-webhook-certs -n kube-system \
  --cert=webhooks/certs/server.pem \
  --key=webhooks/certs/server-key.pem

# Then apply the webhook manifest with the CA bundle
CA_BUNDLE=$(cat webhooks/certs/ca.pem | base64 | tr -d '\n') \
envsubst < webhooks/energy-policy/deployment.yaml | kubectl apply -f -
```

To uninstall:

```bash
kubectl delete -f manifests/compute-gardener-scheduler/compute-gardener-scheduler.yaml
kubectl delete -f webhooks/energy-policy/deployment.yaml
kubectl delete secret energy-policy-webhook-certs -n kube-system
```

## Hardware Power Profiles

The scheduler uses hardware-specific power profiles to accurately estimate and optimize energy consumption. This works through multiple layers:

1. **Hardware Profile Database**: A ConfigMap containing power profiles for various CPU, GPU, and memory types
2. **Cloud Instance Detection**: Automatically maps cloud instance types to their hardware components
3. **On-Premises Hardware Detection**: Uses node labels or runtime detection to identify hardware

### Labeling Nodes with Hardware Information

For optimal performance, you can label your nodes with their hardware details:

```bash
# Label all nodes in your cluster
./hack/label-node-hardware.sh

# Or label a specific node
./hack/label-node-hardware.sh worker-1
```

This adds standard Kubernetes labels that describe the hardware:
- `node.kubernetes.io/cpu-model`: CPU model string (e.g., "Intel(R) Core(TM) i5-6500 CPU @ 3.20GHz")
- `node.kubernetes.io/gpu-model`: GPU model if present (e.g., "NVIDIA GeForce RTX 3060")

The scheduler uses these labels as a fast path for hardware identification. Without labels, it will perform runtime detection and cache the results.

### Hardware Profile ConfigMap

The hardware profiles are defined in a YAML format with PUE (Power Usage Effectiveness) considerations:

```yaml
# Global PUE defaults
defaultPUE: 1.1       # Default datacenter PUE (typical range: 1.1-1.6)
defaultGPUPUE: 1.15   # Default GPU-specific PUE for power conversion losses

# CPU power profiles
cpuProfiles:
  "Intel(R) Xeon(R) Platinum 8275CL":
    idlePower: 10.5  # Idle power in watts
    maxPower: 120.0  # Max power in watts
  "Intel(R) Core(TM) i5-6500 CPU @ 3.20GHz":
    idlePower: 5.0
    maxPower: 65.0

# GPU power profiles with workload type coefficients
gpuProfiles:
  "NVIDIA A100":
    idlePower: 25.0       # Idle power in watts
    maxPower: 400.0       # Max power in watts at 100% utilization
    workloadTypes:        # Power coefficients for different workload types
      inference: 0.6      # Inference typically uses ~60% of max power at 100% utilization
      training: 1.0       # Training uses full power
      rendering: 0.9      # Rendering uses ~90% of max power at 100% utilization
  "NVIDIA GeForce GTX 1660":
    idlePower: 7.0
    maxPower: 125.0
    workloadTypes:
      inference: 0.5
      training: 0.9
      rendering: 0.8

# Memory power profiles
memProfiles:
  "DDR4-2666 ECC":
    idlePowerPerGB: 0.125  # Idle power per GB in watts
    maxPowerPerGB: 0.375   # Max power per GB in watts at full utilization
    baseIdlePower: 1.0     # Base power overhead in watts

# Cloud instance mappings to hardware components
cloudInstanceMapping:
  aws:
    "m5.large":
      cpuModel: "Intel(R) Xeon(R) Platinum 8175M"
      memoryType: "DDR4-2666 ECC"
      numCPUs: 2
      totalMemory: 8192  # in MB
    "p3.2xlarge":
      cpuModel: "Intel(R) Xeon(R) E5-2686 v4"
      gpuModel: "NVIDIA Tesla V100"
      memoryType: "DDR4-2666 ECC"
      numCPUs: 8
      numGPUs: 1
      totalMemory: 61440  # in MB
  gcp:
    "n2-standard-4":
      cpuModel: "Intel Cascade Lake"
      memoryType: "DDR4-3200"
      numCPUs: 4
      totalMemory: 16384  # in MB

# Node-specific configurations
nodePowerConfig:
  "worker-1":
    idlePower: 80.0   # Idle power in watts
    maxPower: 350.0   # Max power in watts
    pue: 1.12         # Custom PUE for this node's datacenter
  "gpu-worker-1":
    idlePower: 120.0
    maxPower: 450.0
    idleGPUPower: 30.0
    maxGPUPower: 320.0
    pue: 1.15
    gpuPue: 1.18      # GPU-specific PUE for this node
```

### How PUE is Applied

The scheduler uses PUE values to calculate the true power consumption of workloads, including datacenter overhead:

1. **Standard PUE**: Applied to CPU and memory power consumption to account for cooling, power distribution, etc.
2. **GPU-specific PUE**: Applied only to GPU power to account for GPU power conversion losses and additional cooling

This allows for more accurate modeling of total energy consumption, especially for AI/ML workloads that heavily utilize GPUs.

### GPU Workload Classification

The scheduler can optimize power calculations based on GPU workload type:

1. **Inference**: Typically uses less power than the GPU's theoretical maximum, even at 100% utilization
2. **Training**: Usually consumes maximum GPU power during training operations
3. **Rendering**: Has specific power profiles between inference and training

Pods can specify their GPU workload type via annotation, or namespaces can set defaults for different workload types.

## Metrics

The scheduler exports Prometheus metrics through a dedicated Service and ServiceMonitor:

- Health checks on port 10259 (HTTPS) path /healthz
- Metrics on port 10259 (HTTPS) path /metrics

The following metrics are available:

**Carbon and Pricing Metrics**
- `scheduler_compute_gardener_carbon_intensity`: Current carbon intensity (gCO2eq/kWh) for a given region
- `scheduler_compute_gardener_electricity_rate`: Current electricity rate ($/kWh) for a given location
- `scheduler_compute_gardener_carbon_delay_total`: Number of scheduling delays due to carbon intensity thresholds
- `scheduler_compute_gardener_price_delay_total`: Number of scheduling delays due to price thresholds

**Energy Budget Metrics**
- `scheduler_compute_gardener_energy_budget_usage_percent`: Percentage of energy budget used by workloads
- `scheduler_compute_gardener_energy_budget_exceeded_total`: Number of workloads that exceeded their energy budget
- `scheduler_compute_gardener_job_energy_usage_kwh`: Estimated energy usage for completed jobs
- `scheduler_compute_gardener_job_carbon_emissions_grams`: Estimated carbon emissions for completed jobs

**Hardware Efficiency Metrics**
- `scheduler_compute_gardener_node_pue`: Power Usage Effectiveness for nodes
- `scheduler_compute_gardener_node_efficiency`: Efficiency metric for nodes
- `scheduler_compute_gardener_power_filtered_nodes_total`: Nodes filtered due to power constraints

**Resource Utilization Metrics**
- `scheduler_compute_gardener_node_cpu_usage_cores`: CPU usage on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_memory_usage_bytes`: Memory usage on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_gpu_usage`: GPU utilization on nodes (baseline, current, final)
- `scheduler_compute_gardener_node_power_estimate_watts`: Estimated node power consumption (baseline, current, final)

**Scheduler Performance Metrics**
- `scheduler_compute_gardener_scheduling_attempt_total`: Number of attempts to schedule pods by result
- `scheduler_compute_gardener_pod_scheduling_duration_seconds`: Latency for scheduling attempts in seconds
- `scheduler_compute_gardener_estimated_savings`: Estimated savings from scheduling (carbon, cost)
- `scheduler_compute_gardener_scheduling_efficiency`: Scheduling efficiency metrics (carbon/cost improvements)
- `scheduler_compute_gardener_metrics_samples_stored`: Number of pod metrics samples currently stored
- `scheduler_compute_gardener_metrics_cache_size`: Number of pods being tracked in metrics cache

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
  namespace: monitoring  # Adjust to your Prometheus namespace
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

1. **Main Scheduler Plugin**: Implements the Kubernetes scheduler framework interfaces
2. **Energy Policy Webhook**: Applies namespace-level energy policies to pods
3. **Hardware Profiler**: Accurately models power consumption with PUE considerations
4. **Energy Budget Tracker**: Monitors real-time energy usage against budgets
5. **API Client**: Handles communication with Electricity Map API
6. **Cache**: Provides caching of API responses to reduce external API calls
7. **TOU Scheduler**: Manages time-of-use pricing schedules

### Scheduling Logic

The scheduler follows this enhanced decision flow:

1. **PreFilter Stage**: 
   - Check if pod has exceeded maximum scheduling delay
   - Check for opt-out annotations
   - Apply namespace energy policy annotations if needed

2. **Filter Stage**:
   - If pricing is enabled:
     - Get current rate from pricing implementation
     - Compare against threshold
   - If carbon-aware scheduling is enabled:
     - Get current carbon intensity from implementation
     - Compare against threshold
   - If hardware efficiency controls are enabled:
     - Calculate effective power consumption with PUE
     - Filter nodes based on power limits and efficiency

3. **Post-scheduling**:
   - Track energy usage over time
   - Compare against energy budgets
   - Take configurable actions when budgets are exceeded

### Energy Policy Webhook

The Energy Policy Webhook automates the application of energy policies to pods:

```
                           +---------------------+
                           | Kubernetes API      |
                           | Server              |
                           +----------+----------+
                                      |
                                      | Pod Creation
                                      | Request
                                      v
                           +----------+----------+
                           | Energy Policy       |
                           | Admission Webhook   |
                           +----------+----------+
                                      |
                                      | Add Annotations
                                      | Based on Namespace
                                      v
                           +----------+----------+
                           | Compute Gardener    |
                           | Scheduler           |
                           +---------------------+
```

This architecture separates policy definition (at namespace level) from policy enforcement (scheduler), making it easier to define energy policies for different workload types and teams.

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
