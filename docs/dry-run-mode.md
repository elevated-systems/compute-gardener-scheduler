# Dry-Run Mode: Try Before You Buy

Dry-run mode provides a low-risk way to evaluate Compute Gardener's carbon and price-aware scheduling capabilities **without affecting your actual workloads**. Think of it as a "read-only" observability layer that shows you what the scheduler would do, without actually doing it.

## Why Dry-Run Mode?

Installing a secondary scheduler can feel risky. What if there are bugs? What if the carbon/price data sources are unreliable? How much could you actually save?

Dry-run mode answers these questions by:

- **Evaluating every pod creation** using the same logic as the scheduler
- **Recording metrics** about potential delays and savings
- **Not affecting pod scheduling** - your existing scheduler continues to work normally
- **Scoping to specific namespaces** - test on development workloads first
- **Providing two output modes** - Prometheus metrics or pod annotations

This lets you build confidence in the scheduler's behavior and quantify potential savings before committing to using it in production.

## How It Works

Dry-run mode uses a Kubernetes [MutatingWebhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook) that intercepts pod creation events:

1. **Webhook evaluation**: When a pod is created, the webhook evaluates carbon intensity and electricity prices using the same logic as the scheduler
2. **Decision recording**: Records whether the scheduler would delay the pod, and why
3. **Completion tracking**: Watches for pod completion to calculate actual runtime and energy consumption
4. **Savings estimation**: Calculates conservative savings estimates based on actual runtime

The webhook operates in one of two modes:

### Metrics Mode (Default)
Records evaluation data as Prometheus metrics only. No modifications to pods.

**Available metrics:**
- `compute_gardener_dryrun_pods_evaluated_total` - Total pods evaluated
- `compute_gardener_dryrun_pods_would_delay_total` - Pods that would have been delayed
- `compute_gardener_dryrun_estimated_carbon_savings_gco2eq_total` - Estimated carbon savings (gCO2eq)
- `compute_gardener_dryrun_estimated_cost_savings_usd_total` - Estimated cost savings (USD)
- `compute_gardener_dryrun_actual_carbon_savings_gco2eq_total` - Actual savings using real runtime
- `compute_gardener_dryrun_actual_cost_savings_usd_total` - Actual cost savings using real runtime
- `compute_gardener_dryrun_pods_completed_total` - Pods that completed
- `compute_gardener_dryrun_pod_runtime_hours` - Histogram of pod runtimes
- `compute_gardener_dryrun_pod_energy_consumption_kwh` - Histogram of energy consumption
- `compute_gardener_dryrun_current_carbon_intensity_gco2eq_per_kwh` - Current carbon intensity
- `compute_gardener_dryrun_current_electricity_price_usd_per_kwh` - Current electricity price

### Annotate Mode
Adds evaluation results as annotations to pods. Useful for inspecting individual pod decisions with `kubectl describe`.

**Added annotations:**
- `compute-gardener-scheduler.kubernetes.io/dry-run-evaluated: "true"`
- `compute-gardener-scheduler.kubernetes.io/dry-run-would-delay: "true|false"`
- `compute-gardener-scheduler.kubernetes.io/dry-run-delay-type: "carbon|price|both"`
- `compute-gardener-scheduler.kubernetes.io/dry-run-reason: "<human-readable explanation>"`
- `compute-gardener-scheduler.kubernetes.io/dry-run-carbon-intensity: "<gCO2eq/kWh>"`
- `compute-gardener-scheduler.kubernetes.io/dry-run-estimated-carbon-savings-gco2: "<gCO2eq>"`

## Installation

### Prerequisites

- Kubernetes 1.31+
- Helm 3.x
- [cert-manager](https://cert-manager.io/) (for webhook TLS certificates)
- Prometheus (optional, for metrics mode)

### Quick Start

1. **Install with dry-run mode enabled:**

```bash
helm install compute-gardener ./manifests/install/charts/compute-gardener-scheduler \
  --namespace compute-gardener \
  --create-namespace \
  --set dryRun.enabled=true \
  --set carbonAware.enabled=true \
  --set-string carbonAware.electricityMap.apiKey=YOUR_API_KEY
```

2. **Verify the dry-run webhook is running:**

```bash
kubectl get pods -n compute-gardener
kubectl get mutatingwebhookconfigurations compute-gardener-dryrun
```

3. **Check metrics** (if using metrics mode):

```bash
kubectl port-forward -n compute-gardener svc/compute-gardener-dryrun-metrics 8080:8080
curl http://localhost:8080/metrics | grep compute_gardener_dryrun
```

### Configuration Options

```yaml
dryRun:
  enabled: true
  mode: "metrics"  # or "annotate"

  # Namespace scoping - gradually roll out evaluation
  watchNamespaces: []  # Empty = all namespaces
  # Example gradual rollout:
  # watchNamespaces:
  #   - "development"  # Start here
  #   - "staging"      # Add after observing dev
  #   - "production"   # Add last

  # Carbon/price settings inherited from carbonAware and priceAware sections
```

**Key settings:**
- `mode`: "metrics" (Prometheus only) or "annotate" (add pod annotations)
- `watchNamespaces`: Empty list watches all namespaces, or specify a whitelist for scoped evaluation
- Carbon/price thresholds are inherited from the main `carbonAware` and `priceAware` configuration

## Understanding the Metrics

### Estimated vs Actual Savings

Dry-run provides two types of savings calculations:

**Estimated savings** (`estimated_*_total`):
- Calculated at pod creation time
- Uses **estimated runtime** from pod annotations or defaults
- Conservative assumption: pod would run at threshold values (not current)

**Actual savings** (`actual_*_total`):
- Calculated at pod completion time
- Uses **real runtime** from pod lifecycle
- Still conservative: assumes pod would have run at threshold values

The "actual" metrics are more accurate because they use real runtime data, but both are conservative estimates.

### Example: Reading Carbon Savings

```promql
# Total estimated carbon savings (all time)
compute_gardener_dryrun_estimated_carbon_savings_gco2eq_total

# Rate of carbon savings (per hour)
rate(compute_gardener_dryrun_estimated_carbon_savings_gco2eq_total[1h])

# Percentage of pods that would be delayed
(
  compute_gardener_dryrun_pods_would_delay_total
  /
  compute_gardener_dryrun_pods_evaluated_total
) * 100
```

### Example: Grafana Dashboard Query

```promql
# Carbon savings by namespace
sum(compute_gardener_dryrun_actual_carbon_savings_gco2eq_total) by (namespace)

# Cost savings by namespace (last 24h)
increase(compute_gardener_dryrun_actual_cost_savings_usd_total[24h])
```

## Annotate Mode Examples

Create a pod and inspect annotations:

```bash
kubectl run test-pod --image=busybox --command -- sleep 3600

kubectl describe pod test-pod | grep compute-gardener
```

Example output:
```
Annotations:  compute-gardener-scheduler.kubernetes.io/dry-run-evaluated: true
              compute-gardener-scheduler.kubernetes.io/dry-run-would-delay: true
              compute-gardener-scheduler.kubernetes.io/dry-run-delay-type: carbon
              compute-gardener-scheduler.kubernetes.io/dry-run-reason: Carbon intensity (450.23 gCO2eq/kWh) exceeds threshold (200.00 gCO2eq/kWh)
              compute-gardener-scheduler.kubernetes.io/dry-run-carbon-intensity: 450.23
              compute-gardener-scheduler.kubernetes.io/dry-run-carbon-threshold: 200.00
              compute-gardener-scheduler.kubernetes.io/dry-run-estimated-carbon-savings-gco2: 125.12
```

## Opting Out

Pods can opt out of dry-run evaluation using the same annotation as the scheduler:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: urgent-pod
  annotations:
    compute-gardener-scheduler.kubernetes.io/skip: "true"
spec:
  # ...
```

## Transitioning to Active Scheduling

Once you're confident in the dry-run metrics and understand the potential savings:

1. **Review the data**: Look at `pods_would_delay_total` and savings metrics
2. **Start small**: Enable the scheduler for a development namespace first
3. **Gradual rollout**: Use `schedulerName: compute-gardener-scheduler` on specific workloads
4. **Monitor both**: Keep dry-run enabled to compare scheduler behavior vs predictions

## Limitations

- **Conservative estimates**: Assumes pods would run at threshold values, not current conditions
- **No scheduling delay simulation**: Doesn't account for when better conditions might occur
- **Webhook overhead**: Small latency added to pod creation (typically <100ms)
- **Storage**: In-memory tracking of pending pods (lost on restart)

## Troubleshooting

### Webhook not receiving events

Check webhook configuration:
```bash
kubectl get mutatingwebhookconfiguration compute-gardener-dryrun -o yaml
```

Verify service and endpoints:
```bash
kubectl get svc -n compute-gardener compute-gardener-dryrun-webhook
kubectl get endpoints -n compute-gardener compute-gardener-dryrun-webhook
```

### Certificate issues

Check cert-manager Certificate:
```bash
kubectl get certificate -n compute-gardener
kubectl describe certificate compute-gardener-dryrun-webhook-cert -n compute-gardener
```

### No metrics appearing

1. Verify pods are being created in watched namespaces
2. Check dry-run pod logs: `kubectl logs -n compute-gardener -l app=compute-gardener-dryrun`
3. Verify Prometheus is scraping: Check ServiceMonitor configuration

### Metrics show zero savings

This is normal if:
- Current carbon intensity is below threshold
- Current electricity prices are below threshold (if price-aware enabled)
- Pods are opted out with `skip: "true"` annotation

## Next Steps

- Read [Getting Started](getting-started.md) to understand the full scheduler
- Review [Carbon-Aware Scheduling](../pkg/computegardener/README.md) for carbon intensity details
- Check [Price-Aware Scheduling](../pkg/computegardener/price/README.md) for TOU pricing
