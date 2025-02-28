# Compute Gardener Scheduler Examples

This directory contains example manifests demonstrating various ways to use the Compute Gardener Scheduler.

## Basic Examples

1. [basic-pod.yaml](basic-pod.yaml) - Simple pod using the compute-gardener scheduler
2. [carbon-threshold-pod.yaml](carbon-threshold-pod.yaml) - Pod with custom carbon intensity threshold
3. [price-aware-pod.yaml](price-aware-pod.yaml) - Pod with price-aware scheduling
4. [price-only-pod.yaml](price-only-pod.yaml) - Pod using only price-aware scheduling (carbon disabled)
5. [opt-out-pod.yaml](opt-out-pod.yaml) - Pod that opts out of carbon/price-aware scheduling

## Advanced Examples

1. [deployment.yaml](deployment.yaml) - Deployment using the compute-gardener scheduler
2. [statefulset.yaml](statefulset.yaml) - StatefulSet with higher thresholds for database workloads
3. [job.yaml](job.yaml) - One-time batch job with scheduling flexibility
4. [cronjob.yaml](cronjob.yaml) - Recurring batch job scheduled during off-peak hours

## Batch Processing Examples

### One-Time Jobs

The [job.yaml](job.yaml) example demonstrates a one-time batch processing task that can be delayed based on carbon intensity and electricity prices:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: compute-gardener-job
spec:
  backoffLimit: 2
  template:
    metadata:
      annotations:
        compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "350.0"
        compute-gardener-scheduler.kubernetes.io/price-threshold: "0.25"
    spec:
      schedulerName: compute-gardener-scheduler
      containers:
      - name: data-processor
        image: data-processor:1.0
```

### Recurring Jobs

The [cronjob.yaml](cronjob.yaml) example shows a recurring batch job scheduled during typical off-peak hours:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: compute-gardener-batch-job
spec:
  schedule: "0 2 * * *"  # Run at 2 AM daily
  jobTemplate:
    spec:
      template:
        metadata:
          annotations:
            compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "400.0"
            compute-gardener-scheduler.kubernetes.io/price-threshold: "0.30"
```

## Stateful Workloads

The [statefulset.yaml](statefulset.yaml) example shows how to configure carbon and price awareness for stateful workloads:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: compute-gardener-statefulset
spec:
  template:
    metadata:
      annotations:
        compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "350.0"
        compute-gardener-scheduler.kubernetes.io/price-threshold: "0.25"
```

## Threshold Guidelines

Different workload types typically use different threshold configurations:

1. **Interactive Services** (e.g., web servers)
   - Lower thresholds
   - Shorter max delays
   - Consider opting out if latency-sensitive

2. **Batch Jobs**
   - Higher thresholds
   - Longer max delays
   - Schedule during off-peak hours

3. **Stateful Services** (e.g., databases)
   - Moderate to high thresholds
   - Moderate max delays
   - Consider data replication requirements

## Best Practices

1. **Resource Requests**
   - Always specify resource requests
   - Set appropriate limits
   - Consider workload characteristics

2. **Scheduling Delays**
   - Set appropriate max delays
   - Use higher thresholds for flexible workloads
   - Consider business requirements

3. **Peak Hours**
   - Schedule batch jobs during off-peak
   - Use CronJobs for recurring tasks
   - Adjust thresholds based on time periods

4. **Data Access**
   - Use appropriate volume types
   - Consider data locality
   - Plan for scheduling delays

## Usage

1. Apply any example:
```bash
kubectl apply -f examples/basic-pod.yaml
```

2. Check scheduling status:
```bash
kubectl get pod <pod-name>
```

3. View scheduling decisions:
```bash
kubectl describe pod <pod-name>
```

## Annotations Reference

1. **General Scheduler**
```yaml
compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "24h"
compute-gardener-scheduler.kubernetes.io/skip: "false"
```

2. **Carbon Intensity**
```yaml
compute-gardener-scheduler.kubernetes.io/carbon-enabled: "true"
compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "200.0"
```

3. **Pricing**
```yaml
compute-gardener-scheduler.kubernetes.io/price-threshold: "0.15"
```

## Common Patterns

1. **Development Workloads**
```yaml
annotations:
  compute-gardener-scheduler.kubernetes.io/skip: "true"
```

2. **Batch Processing**
```yaml
annotations:
  compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "400.0"
  compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "12h"
```

3. **Production Services**
```yaml
annotations:
  compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"
  compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "1h"
```

4. **Price-Only Workloads**
```yaml
annotations:
  compute-gardener-scheduler.kubernetes.io/carbon-enabled: "false"
  compute-gardener-scheduler.kubernetes.io/price-threshold: "0.15"
  compute-gardener-scheduler.kubernetes.io/max-scheduling-delay: "6h"
