# Compute Gardener Scheduler Helm Chart

This Helm chart deploys the Compute Gardener Scheduler, a Kubernetes scheduler plugin that enables carbon-aware and price-aware scheduling decisions.

## Features

- **Carbon-aware scheduling**: Delay workload scheduling based on carbon intensity thresholds
- **Price-aware scheduling**: Schedule workloads during off-peak pricing periods
- **Metrics integration**: Monitor scheduler performance with Prometheus

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+

### Required Components

- **Prometheus Operator CRDs** (if using metrics): Required if metrics.enabled=true. Install with:
  ```bash
  kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml
  ```
  Or disable metrics with `--set metrics.enabled=false`
  
  **Note for GKE users**: GKE uses its own monitoring CRDs. For GKE, enable the GKE-specific monitoring with `--set metrics.gke=true` instead of installing the Prometheus Operator CRDs. This will use `PodMonitoring` from the `monitoring.googleapis.com/v1` API.

### Recommended Components

- **Metrics Server**: Highly recommended but not strictly required. Without Metrics Server, the scheduler won't be able to collect real-time node utilization data, resulting in less accurate energy usage estimates. Core carbon-aware and price-aware scheduling will still function.

- **Prometheus**: Highly recommended but not strictly required. Without Prometheus, you won't be able to visualize scheduler performance metrics or validate carbon/cost savings. The scheduler will continue to function, but you'll miss valuable insights into its operation. Prometheus is also essential for CPU frequency and GPU power data collection.

- **Node Exporter**: Required for accurate power estimation. The scheduler depends on standard Prometheus node-exporter to collect CPU frequency data for dynamic frequency scaling calculations. Most Prometheus installations include node-exporter by default.

- **GPU Metrics Collection**: Optional component for NVIDIA GPU monitoring. When enabled, it provides accurate GPU power consumption data via Prometheus. The scheduler integrates with industry-standard GPU metrics to capture real-time GPU power usage with per-device granularity.
  
  **GPU Support Requirements:** For GPU metrics collection, nodes must have NVIDIA GPUs with NVIDIA drivers installed. The scheduler will automatically detect GPU nodes when GPU metrics are available through Prometheus.

## Installation

### Add the chart repository

```bash
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update
```

### Install the chart

```bash
# Basic installation 
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY

# Installation with price-aware scheduling enabled
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set priceAware.enabled=true \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY

# Installation with GPU monitoring for comprehensive power estimation
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set metrics.gpuMetrics.enabled=true \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY

# Installation without metrics (for clusters without Prometheus Operator)
# This is a more lightweight installation and doesn't require ServiceMonitor CRDs
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set metrics.enabled=false \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

### Uninstall the chart

```bash
helm uninstall compute-gardener-scheduler --namespace compute-gardener
```

#### Installation on Managed Kubernetes Services

**Note on Simplified/Managed Cluster Types:**
Simplified cluster types like GKE Autopilot and EKS Fargate have limitations that may prevent using custom schedulers:

- **GKE** considerations:
  - **GKE Autopilot** does not support custom schedulers at all. You must use GKE Standard clusters:
    ```bash
    # Create a GKE Standard cluster in a region with typically lower carbon intensity
    # The us-west1 (Oregon) region uses significant renewable energy
    gcloud container clusters create compute-gardener-cluster \
      --num-nodes=1 \
      --disk-size=100 \
      --machine-type=e2-standard-2 \
      --zone=us-west1-a
    ```
  - **GKE Standard** with Google Cloud Managed Service for Prometheus: 
    ```bash
    # Install with GKE-specific monitoring
    helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
      --namespace compute-gardener \
      --create-namespace \
      --set metrics.gke=true \
      --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
    ```
    This will use `PodMonitoring` resources from the `monitoring.googleapis.com/v1` API instead of the standard Prometheus Operator `ServiceMonitor` resources.

- **EKS Fargate** has similar limitations with pod scheduling. Consider using regular EKS with EC2 nodes.

- **AKS** works best with custom schedulers when not using the Virtual Node feature.

For any managed Kubernetes service that supports custom schedulers, use the standard installation:

```bash
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

**Choosing Low-Carbon Regions:**

For the best carbon-aware scheduling results, consider creating your clusters in regions with lower carbon intensity:

| Cloud Provider | Low-Carbon Regions                                        |
|----------------|----------------------------------------------------------|
| GCP            | us-west1 (Oregon), europe-north1 (Finland)               |
| AWS            | us-west-2 (Oregon), eu-north-1 (Stockholm)               |
| Azure          | westus2 (Washington), northeurope (Ireland)              |

These regions typically have significant renewable energy sources and lower carbon intensities.

If the sample pod fails to schedule, this may indicate that your cluster does not support custom schedulers.

## Using the Scheduler

To use the compute-gardener-scheduler for your workloads, specify the scheduler name in your pod specification:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  schedulerName: compute-gardener-scheduler
  containers:
  - name: my-container
    image: my-image
```

**Note:** The scheduler works across namespaces - your workloads can be in any namespace (not just in the compute-gardener namespace). The installation includes a sample pod in the `default` namespace to demonstrate this capability.

### Carbon-Aware Scheduling

To customize carbon-aware scheduling for a specific pod, use annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-carbon-aware-pod
  annotations:
    compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "150.0"
    compute-gardener-scheduler.kubernetes.io/skip: "false"
spec:
  schedulerName: compute-gardener-scheduler
  containers:
  - name: my-container
    image: my-image
```

### Price-Aware Scheduling

To customize price-aware scheduling for a specific pod, use annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-price-aware-pod
  annotations:
    compute-gardener-scheduler.kubernetes.io/max-price: "0.15"
    compute-gardener-scheduler.kubernetes.io/skip-price: "false"
spec:
  schedulerName: compute-gardener-scheduler
  containers:
  - name: my-container
    image: my-image
```

## Configuration

The following table lists the configurable parameters of the Compute Gardener Scheduler chart and their default values.

| Parameter | Description | Default |
| --------- | ----------- | ------- |
| `createNamespace` | Create namespace if it doesn't exist | `true` |
| `scheduler.name` | Name of the scheduler | `compute-gardener-scheduler` |
| `scheduler.image` | Scheduler container image | `docker.io/dmasselink/compute-gardener-scheduler:v0.1.2-1d5dddd` |
| `scheduler.imagePullPolicy` | Image pull policy | `IfNotPresent` |
| `scheduler.replicaCount` | Number of scheduler replicas | `1` |
| `scheduler.leaderElect` | Enable leader election | `false` |
| `carbonAware.enabled` | Enable carbon-aware scheduling | `true` |
| `carbonAware.carbonIntensityThreshold` | Default carbon intensity threshold | `200.0` |
| `carbonAware.maxSchedulingDelay` | Maximum delay for scheduling | `24h` |
| `carbonAware.electricityMap.apiKey` | ElectricityMap API key | `YOUR_ELECTRICITY_MAP_API_KEY` |
| `priceAware.enabled` | Enable price-aware scheduling | `false` |
| `priceAware.provider` | Pricing provider | `tou` |
| `priceAware.schedules` | List of Time-of-Use schedules (see below) | `[]` |
| `hardwareProfiles.enabled` | Enable hardware profiles | `true` |
| `hardwareProfiles.mountPath` | Path to mount the hardware profiles | `/etc/kubernetes/compute-gardener-scheduler/hardware-profiles` |
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.serviceMonitor.enabled` | Enable ServiceMonitor for Prometheus | `true` |
| `metrics.serviceMonitor.namespace` | ServiceMonitor namespace | `cattle-monitoring-system` |
| `metrics.gke` | Use GKE-specific PodMonitoring instead of ServiceMonitor | `false` |
| `samplePod.enabled` | Deploy a sample pod to showcase scheduler | `true` |
| `samplePod.image` | Image for the sample pod | `busybox:latest` |

### Time-of-Use Schedule Configuration

The Time-of-Use pricing configuration supports multiple schedules, each with its own timezone. Each schedule consists of:

| Field | Description | Example |
|-------|-------------|---------|
| `name` | Unique name to identify this schedule | `"california-pge"` |
| `dayOfWeek` | Days this schedule applies to (0=Sunday, 1=Monday, etc.) | `"1-5"` for weekdays |
| `startTime` | Start time of peak period (24h format) | `"16:00"` |
| `endTime` | End time of peak period (24h format) | `"21:00"` |
| `timezone` | IANA timezone identifier | `"America/Los_Angeles"` |
| `peakRate` | Optional: Peak electricity rate in $/kWh | `0.30` |
| `offPeakRate` | Optional: Off-peak electricity rate in $/kWh | `0.10` |

Example configuration:

```yaml
priceAware:
  enabled: true
  provider: "tou"
  schedules:
    - name: "california-pge-weekday"
      dayOfWeek: "1-5"
      startTime: "16:00"
      endTime: "21:00"
      timezone: "America/Los_Angeles"
      peakRate: 0.30
      offPeakRate: 0.10
    - name: "california-pge-weekend"
      dayOfWeek: "0,6"
      startTime: "17:00"
      endTime: "20:00"
      timezone: "America/Los_Angeles"
      peakRate: 0.25
      offPeakRate: 0.10
```

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`.

For example:
```bash
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --set scheduler.replicaCount=2 \
  --set carbonAware.carbonIntensityThreshold=180.0
```

Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example:
```bash
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  -f values.yaml
```

## Scheduler Configuration

### Scheduler Mode

#### Secondary Scheduler Mode (Default, Recommended)

By default, the compute-gardener-scheduler installs as a secondary scheduler alongside the default Kubernetes scheduler. Pods must explicitly specify the scheduler name to use it:

```yaml
spec:
  schedulerName: compute-gardener-scheduler
```

This mode is compatible with all Kubernetes distributions, including managed Kubernetes services, and is the recommended approach for most environments.

```bash
# Install as a secondary scheduler (default)
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

**IMPORTANT:** If you see logs for pods that don't explicitly specify this scheduler name, your installation may be incorrectly configured. Check your cluster configuration or reinstall the chart.

#### Primary Scheduler Mode (Advanced, Not Recommended)

**NOT RECOMMENDED FOR MOST INSTALLATIONS**: The scheduler can also be configured to act as the primary scheduler, handling ALL pods in the cluster, even those that don't explicitly specify a schedulerName.

```bash
# Install as the primary scheduler (NOT RECOMMENDED for most installations)
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set scheduler.mode=primary \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

**WARNING**: In primary mode, ALL pods in your cluster will be processed by compute-gardener-scheduler policies, potentially causing:
- Deployment/StatefulSet/ReplicaSet pods to be delayed due to carbon/price thresholds
- Unexpected scheduling behavior for system components
- Conflicts with cloud provider expectations about scheduling behavior

Use primary mode ONLY if:
- You fully understand the implications for your entire workload set
- You want ALL containers in your cluster to be subject to carbon/price-aware scheduling
- You're running in a non-production environment or specialized cluster dedicated to deferrable workloads

To verify which scheduler is handling your pods, you can check the pod events:

```bash
kubectl describe pod [pod-name] | grep "Successfully assigned"
```

If you installed in primary mode and need to revert:

```bash
# Uninstall the compute-gardener-scheduler
helm uninstall compute-gardener-scheduler --namespace compute-gardener
# The default Kubernetes scheduler will automatically resume handling all pods
```

### High Availability Configuration

By default, the scheduler runs as a single replica, which is suitable for most environments. For high-availability deployments:

1. **Leader Election**: Required when running multiple scheduler replicas to ensure only one instance actively makes scheduling decisions.

   The `scheduler.leaderElect` parameter must be set to `true` when using multiple replicas:

   ```bash
   # High availability configuration with leader election
   helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
     --namespace compute-gardener \
     --create-namespace \
     --set scheduler.leaderElect=true \
     --set scheduler.replicaCount=2 \
     --set carbonAware.electricityMap.apiKey=YOUR_API_KEY
   ```

   **CAUTION**: Never increase `replicaCount` without enabling `leaderElect`, as this will cause scheduling conflicts.