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

### Recommended Components

- **Metrics Server**: Highly recommended but not strictly required. Without Metrics Server, the scheduler won't be able to collect real-time node utilization data, resulting in less accurate energy usage estimates. Core carbon-aware and price-aware scheduling will still function.

- **Prometheus**: Highly recommended but not strictly required. Without Prometheus, you won't be able to visualize scheduler performance metrics or validate carbon/cost savings. The scheduler will continue to function, but you'll miss valuable insights into its operation.

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
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY \
  --set priceAware.enabled=true

# Installation without metrics (for clusters without Prometheus Operator)
# This is a more lightweight installation and doesn't require ServiceMonitor CRDs
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY \
  --set metrics.enabled=false
```

#### Installation on Managed Kubernetes Services

**Note on Simplified/Managed Cluster Types:**
Simplified cluster types like GKE Autopilot and EKS Fargate have limitations that may prevent using custom schedulers:

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
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.serviceMonitor.enabled` | Enable ServiceMonitor for Prometheus | `true` |
| `metrics.serviceMonitor.namespace` | ServiceMonitor namespace | `cattle-monitoring-system` |
| `samplePod.enabled` | Deploy a sample pod to showcase scheduler | `true` |
| `samplePod.image` | Image for the sample pod | `busybox:latest` |

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

## Usage Modes

### Second Scheduler Mode (Default)

By default, the compute-gardener-scheduler installs as a second scheduler alongside the default Kubernetes scheduler. Pods must explicitly specify the scheduler name to use it:

```yaml
spec:
  schedulerName: compute-gardener-scheduler
```

This mode is compatible with all Kubernetes distributions, including managed Kubernetes services.

### Primary Scheduler Mode

For clusters that allow replacing the default scheduler, you can modify your deployment to use the compute-gardener-scheduler as the primary scheduler by updating kube-scheduler configuration. This is not compatible with many cloud providers' default cluster configurations.