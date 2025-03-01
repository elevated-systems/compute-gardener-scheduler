# Compute Gardener Scheduler Helm Chart

This Helm chart deploys the Compute Gardener Scheduler, a Kubernetes scheduler plugin that enables carbon-aware and price-aware scheduling decisions.

## Features

- **Carbon-aware scheduling**: Delay workload scheduling based on carbon intensity thresholds
- **Price-aware scheduling**: Schedule workloads during off-peak pricing periods
- **Metrics integration**: Monitor scheduler performance with Prometheus

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+
- Metrics API enabled in your cluster (for node metrics)
- Prometheus Operator (optional, for ServiceMonitor support)

## Installation

### Add the chart repository

```bash
helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
helm repo update
```

### Install the chart

```bash
# Basic installation
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler-helm \
  --namespace kube-system \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY

# Installation with price-aware scheduling enabled
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler-helm \
  --namespace kube-system \
  --set carbonAware.electricityMap.apiKey=YOUR_API_KEY \
  --set priceAware.enabled=true
```

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
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler-helm \
  --namespace kube-system \
  --set scheduler.replicaCount=2 \
  --set carbonAware.carbonIntensityThreshold=180.0
```

Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example:
```bash
helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler-helm \
  --namespace kube-system \
  -f values.yaml
```