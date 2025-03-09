# Compute Gardener Node Exporter

The Compute Gardener Node Exporter is a component of the Compute Gardener Scheduler ecosystem that collects hardware-level metrics from Kubernetes nodes, with a focus on energy-related data. It runs as a DaemonSet, with one instance on each node in the cluster.

## Features

- **CPU Metrics Collection:**
  - CPU frequency monitoring (current, min, max, base)
  - CPU model detection and annotation
  
- **GPU Metrics Collection:**
  - GPU utilization percentage
  - GPU memory utilization percentage
  - GPU power consumption in watts
  - GPU model detection and annotation

## Metrics Exposed

All metrics are exposed on port 9100 with the prefix `compute_gardener`.

### CPU Metrics
- `compute_gardener_cpu_frequency_ghz` - Current CPU frequency in GHz per core
- `compute_gardener_cpu_frequency_static_ghz` - Static CPU frequency information (base, min, max) in GHz

### GPU Metrics
- `compute_gardener_gpu_count` - Number of GPUs detected on the node
- `compute_gardener_gpu_power_watts` - Current GPU power consumption in watts
- `compute_gardener_gpu_max_power_watts` - Maximum GPU power limit in watts
- `compute_gardener_gpu_utilization_percent` - Current GPU utilization percentage
- `compute_gardener_gpu_memory_utilization_percent` - Current GPU memory utilization percentage

## Node Annotations

The exporter also adds annotations to nodes to provide hardware information:

### CPU Annotations
- `compute-gardener-scheduler.kubernetes.io/cpu-model` - CPU model
- `compute-gardener-scheduler.kubernetes.io/cpu-base-frequency` - Base CPU frequency in GHz
- `compute-gardener-scheduler.kubernetes.io/cpu-min-frequency` - Minimum CPU frequency in GHz
- `compute-gardener-scheduler.kubernetes.io/cpu-max-frequency` - Maximum CPU frequency in GHz

### GPU Annotations
- `compute-gardener-scheduler.kubernetes.io/gpu-model` - GPU model name(s)
- `compute-gardener-scheduler.kubernetes.io/gpu-count` - Number of GPUs on the node
- `compute-gardener-scheduler.kubernetes.io/gpu-total-power` - Total GPU power in watts

## Requirements

- For GPU metrics: NVIDIA GPU with nvidia-smi available
- For CPU frequency metrics: Linux with access to `/sys/devices/system/cpu/`

## Deployment

The node exporter is deployed as a DaemonSet with privileged permissions to access system information:

```
kubectl apply -f manifests/node-exporter/daemonset.yaml
```

## Prometheus Integration

These metrics are designed to be collected by Prometheus. Add the following scrape configuration to your Prometheus setup:

```yaml
scrape_configs:
  - job_name: 'compute-gardener-node-exporter'
    kubernetes_sd_configs:
    - role: pod
    relabel_configs:
    - source_labels: [__meta_kubernetes_pod_label_app]
      action: keep
      regex: compute-gardener-node-exporter
    - source_labels: [__address__]
      action: replace
      regex: (.+)
      target_label: __address__
      replacement: $1:9100
```

## Scheduler Integration

The Compute Gardener Scheduler can use the metrics exposed by this exporter to make energy-aware scheduling decisions. Configure the scheduler to use Prometheus as the data source:

```yaml
metrics:
  prometheus:
    url: "http://prometheus.monitoring:9090"
    queryTimeout: "30s"
    completionDelay: "30s" # Wait 30s after pod completion to collect final metrics
```