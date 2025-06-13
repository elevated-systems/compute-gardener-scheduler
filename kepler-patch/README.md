# Kepler Metrics Filtering for Compute Gardener Scheduler

This directory contains a patch to configure Kepler to only collect metrics for pods scheduled by the compute-gardener-scheduler (or a rough proxy scheduler use), dramatically reducing Prometheus storage usage.

## Problem

Kepler (Kubernetes Efficient Power Level Exporter) collects detailed energy metrics for all workloads in your cluster. While valuable, this can generate massive amounts of time-series data that overwhelms Prometheus storage, especially in large clusters or with limited addl resources (esp. disk) to monitoring.

If you're using Kepler primarily to support compute-gardener-scheduler's energy-aware scheduling, you likely only need metrics for the workloads that the scheduler manages - not every pod in the cluster.

## Solution

This patch adds Prometheus metric relabeling rules to the Kepler ServiceMonitor that:

- **Filters by scheduler**: Only keeps metrics for pods scheduled by `compute-gardener-scheduler`
  - Kepler doesn't seem to currently provide schedulerName as a metrics dimension, so we're currently only roughly facilitating scheduler aware filtering by way of capturing metrics only from pods run in particular namespaces (`compute-gardener-workloads` by default)
- **Excludes system namespaces**: Drops metrics from `kube-*`, `cattle-*`, etc.

**Expected impact**: Huge reduction in Kepler metrics volume.

## Usage

### Prerequisites

1. Kepler is already installed in your cluster (via any method - Helm, manifests, etc.)
2. Prometheus is configured to scrape Kepler metrics via a ServiceMonitor
3. You have `kubectl` access to patch ServiceMonitors

### Apply the Patch

1. **Identify your monitoring namespace**:
   ```bash
   kubectl get servicemonitor -A | grep kepler
   ```

2. **Apply the patch**:
   ```bash
   kubectl patch servicemonitor kepler-exporter -n <monitoring-namespace> \
     --patch-file=kepler-servicemonitor-patch.yaml --type=merge
   ```

3. **Verify the patch was applied**:
   ```bash
   kubectl get servicemonitor kepler-exporter -n <monitoring-namespace> -o yaml
   ```
   
   You should see a `metricRelabelings` section in the ServiceMonitor.

### Verification

1. **Check Prometheus targets**:
   - Navigate to Prometheus UI → Status → Targets
   - Find the `kepler-exporter` target
   - Verify it's still `UP` after the patch

2. **Monitor metrics volume**:
   - In Prometheus UI, query: `count by (__name__)({__name__=~"kepler_container_.*"})`
   - You should see significantly fewer metric series

3. **Test with compute-gardener workloads**:
   - Deploy a pod into the `compute-gardener-scheduler` namespace.
   - Verify its energy metrics appear in Prometheus

## Common Monitoring Namespace Names

- **Standard Prometheus Operator**: `monitoring`
- **Rancher Monitoring**: `cattle-monitoring-system`
- **Cloud providers**: Check your specific setup

## Troubleshooting

### ServiceMonitor patch fails
```bash
# Check if ServiceMonitor exists and get exact name/namespace
kubectl get servicemonitor -A | grep kepler

# Verify the patch file syntax
kubectl patch servicemonitor kepler-exporter -n <namespace> \
  --patch-file=kepler-servicemonitor-patch.yaml --type=merge --dry-run=server
```

## Reverting

To remove the filtering and return to collecting all Kepler metrics:

```bash
kubectl patch servicemonitor kepler-exporter -n <monitoring-namespace> \
  --type='merge' -p='{"spec":{"endpoints":[{"port":"http","interval":"30s","scheme":"http","relabelings":[{"action":"replace","regex":"(.*)","replacement":"$1","sourceLabels":["__meta_kubernetes_pod_node_name"],"targetLabel":"instance"}]}]}}'
```

Or simply reinstall Kepler to restore the original ServiceMonitor configuration.

## Background

This solution was developed to address Prometheus storage challenges caused by Kepler generating 50+ GB of WAL data per week in production clusters. The filtering approach maintains full functionality for compute-gardener-scheduler while dramatically reducing storage overhead.

For more details, see the compute-gardener-scheduler documentation on energy-aware scheduling and power monitoring integration.