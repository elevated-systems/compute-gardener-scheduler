# Energy Policy Webhook

This webhook automatically applies energy policy annotations to pods based on namespace-level and workload-specific configurations. It allows cluster administrators to define default energy policies without requiring developers to understand the details of energy-efficient scheduling.

## How It Works

1. The webhook intercepts pod creation requests
2. It checks if the pod's namespace has energy policies enabled (via label)
3. It applies the namespace's policy annotations to the pod if not already set
4. Special workload-specific overrides can be applied based on pod owner type (Job, Deployment, etc.)

## Supported Annotations

The webhook automatically applies the following annotations:

- `compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold`
- `compute-gardener-scheduler.kubernetes.io/price-threshold`
- `compute-gardener-scheduler.kubernetes.io/energy-budget-kwh`
- `compute-gardener-scheduler.kubernetes.io/energy-budget-action`
- `compute-gardener-scheduler.kubernetes.io/gpu-workload-type`
- `compute-gardener-scheduler.kubernetes.io/max-power-watts`
- `compute-gardener-scheduler.kubernetes.io/min-efficiency`

## Configuring Energy Policies for a Namespace

To enable energy policies for a namespace:

1. Add the label: `compute-gardener-scheduler.kubernetes.io/energy-policies: "enabled"`
2. Add namespace-level policy annotations with the prefix `compute-gardener-scheduler.kubernetes.io/policy-...`
3. Optionally add workload-specific overrides with prefix `compute-gardener-scheduler.kubernetes.io/workload-TYPE-policy-...`

Example:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: green-compute
  labels:
    compute-gardener-scheduler.kubernetes.io/energy-policies: "enabled"
  annotations:
    # Default carbon intensity threshold for all pods
    compute-gardener-scheduler.kubernetes.io/policy-carbon-intensity-threshold: "200"
    
    # Override for batch jobs
    compute-gardener-scheduler.kubernetes.io/workload-batch-policy-energy-budget-kwh: "5"
    
    # Override for services 
    compute-gardener-scheduler.kubernetes.io/workload-service-policy-energy-budget-kwh: "10"
```

## Supported Workload Types

The webhook detects workload types based on owner references:

- `batch`: Jobs and CronJobs
- `service`: Deployments and ReplicaSets
- `stateful`: StatefulSets
- `system`: DaemonSets
- `generic`: Default for standalone pods

## Installation

1. Generate webhook certificates
2. Apply the deployment manifest:

```bash
kubectl apply -f deployment.yaml
```

## Developing

Build the webhook:

```bash
docker build -t energy-policy-webhook:latest -f Dockerfile .
```