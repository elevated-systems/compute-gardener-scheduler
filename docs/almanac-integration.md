# Almanac Scoring API Integration

This document describes how to configure and use the compute-gardener-almanac scoring API integration in the scheduler.

## Overview

The Almanac scoring API provides blended carbon-cost optimization scores for scheduling decisions. Instead of making separate calls to the Electricity Maps API for carbon intensity and evaluating time-of-use pricing locally, the scheduler can query the Almanac API for a unified optimization score that considers both factors.

## Architecture

When Almanac scoring is enabled and a pod opts in via annotations:

1. **Filter Phase**: The scheduler extracts cloud provider information from node labels
2. **API Call**: Makes a POST request to `/v1/score` with provider, region, instance type, and optimization weights
3. **Decision**: Compares the returned score against a threshold to decide whether to proceed with scheduling

## Configuration

### Scheduler Configuration

Add the almanac section to your scheduler configuration (in `values.yaml` for Helm deployments):

```yaml
almanac:
  enabled: true
  url: "http://almanac-service.default.svc.cluster.local:8080"
  timeout: "10s"

  # Default optimization weights (can be overridden per-pod)
  defaultCarbonWeight: 0.6  # 60% weight on carbon reduction
  defaultPriceWeight: 0.4   # 40% weight on cost savings

  # Default score threshold for proceeding (0.0-1.0)
  defaultScoreThreshold: 0.7

  # Fallback values when node labels are unavailable
  defaultProvider: "aws"
  defaultRegion: "us-west-2"
  defaultInstanceType: "m5.xlarge"

  # Fail-open behavior: allow scheduling if Almanac API is unavailable
  failOpen: true
```

### Pod Annotations

Pods must opt-in to Almanac scoring using annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-workload
  annotations:
    # Enable Almanac scoring for this pod
    compute-gardener-scheduler.kubernetes.io/almanac-enabled: "true"

    # Optional: Override default weights
    compute-gardener-scheduler.kubernetes.io/almanac-carbon-weight: "0.8"
    compute-gardener-scheduler.kubernetes.io/almanac-price-weight: "0.2"

    # Optional: Override default threshold
    compute-gardener-scheduler.kubernetes.io/almanac-score-threshold: "0.75"
spec:
  schedulerName: compute-gardener-scheduler
  containers:
  - name: workload
    image: myapp:latest
```

## Node Information Requirements

The scheduler extracts cloud provider information from standard Kubernetes node labels:

- **Region**: `topology.kubernetes.io/region`
- **Zone**: `topology.kubernetes.io/zone`
- **Instance Type**: `node.kubernetes.io/instance-type` or `beta.kubernetes.io/instance-type`
- **Provider**: Inferred from `spec.providerID` or instance type patterns

### Example Node Labels (AWS EKS)

```yaml
apiVersion: v1
kind: Node
metadata:
  labels:
    topology.kubernetes.io/region: us-west-2
    topology.kubernetes.io/zone: us-west-2a
    node.kubernetes.io/instance-type: m5.xlarge
spec:
  providerID: aws:///us-west-2a/i-1234567890abcdef0
```

### Example Node Labels (GCP GKE)

```yaml
apiVersion: v1
kind: Node
metadata:
  labels:
    topology.kubernetes.io/region: us-central1
    topology.kubernetes.io/zone: us-central1-a
    node.kubernetes.io/instance-type: n1-standard-4
spec:
  providerID: gce://my-project/us-central1-a/instance-name
```

## API Request/Response

### Request Format

```json
{
  "provider": "aws",
  "region": "us-west-1",
  "instanceType": "m5.xlarge",
  "weights": {
    "carbon": 0.6,
    "price": 0.4
  }
}
```

### Response Format

```json
{
  "zone": "US-CAL-CISO",
  "optimizationScore": 0.706765,
  "components": {
    "carbonScore": 0.732727,
    "priceScore": 0.680804,
    "blendWeights": {
      "carbon": 0.6,
      "price": 0.4
    }
  },
  "rawValues": {
    "carbonIntensityGCO2kWh": 197,
    "spotPriceUSDHour": 0.0715,
    "onDemandPriceUSDHour": 0.224,
    "instanceType": "m5.xlarge"
  },
  "recommendation": "PROCEED",
  "timestamp": "2026-02-06T10:00:00Z"
}
```

### Recommendations

- `OPTIMAL` (score > 0.85): Excellent conditions for scheduling
- `PROCEED` (score > 0.7): Good conditions, proceed with scheduling
- `WAIT` (score < 0.4): Poor conditions, defer scheduling

## Scheduling Behavior

1. **When almanac is enabled and pod opts in**:
   - Scheduler calls Almanac API for each node in Filter phase
   - If score < threshold: node is filtered out (Unschedulable)
   - If score >= threshold: node passes filter
   - Threshold can be overridden per-pod via annotation

2. **When Almanac API is unavailable**:
   - If `failOpen: true`: Allow scheduling (default)
   - If `failOpen: false`: Block scheduling (fail-closed)

3. **When pod doesn't opt in**:
   - Falls back to existing carbon/price checks if enabled
   - Otherwise, no special scheduling constraints

## Deployment Example

1. **Deploy Almanac Service** (in your cluster or externally accessible):

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: almanac-service
  namespace: default
spec:
  selector:
    app: almanac
  ports:
  - port: 8080
    targetPort: 8080
EOF
```

2. **Update Scheduler ConfigMap**:

Add Almanac configuration to the scheduler's plugin args ConfigMap.

3. **Deploy Test Pod**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: almanac-test
  annotations:
    compute-gardener-scheduler.kubernetes.io/almanac-enabled: "true"
    compute-gardener-scheduler.kubernetes.io/almanac-carbon-weight: "0.7"
    compute-gardener-scheduler.kubernetes.io/almanac-price-weight: "0.3"
spec:
  schedulerName: compute-gardener-scheduler
  containers:
  - name: busybox
    image: busybox
    command: ["sleep", "3600"]
```

## Monitoring and Debugging

### Check Scheduler Logs

```bash
kubectl logs -n compute-gardener <scheduler-pod> | grep -i almanac
```

Look for messages like:
- `Almanac scoring enabled` - Confirms almanac is configured
- `Making Almanac scoring API request` - API call being made
- `Received scoring result` - Successful API response
- `Node passed Almanac scoring check` - Node approved
- `Node filtered by almanac score` - Node rejected due to low score

### Verify Node Labels

```bash
kubectl get nodes -o json | jq '.items[] | {name: .metadata.name, labels: .metadata.labels}'
```

Ensure nodes have the required topology labels.

### Test Almanac API Directly

```bash
curl -X POST http://almanac-service:8080/v1/score \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aws",
    "region": "us-west-1",
    "instanceType": "m5.xlarge",
    "weights": {
      "carbon": 0.5,
      "price": 0.5
    }
  }' | jq .
```

## Migration from Separate Carbon/Price Checks

If you're currently using separate carbon and price checks:

1. **Keep existing configuration** - Almanac is opt-in per pod
2. **Test with a few pods first** - Add almanac annotations to test workloads
3. **Compare behavior** - Monitor scheduling decisions
4. **Gradual rollout** - Incrementally add annotations to more workloads
5. **Eventually disable** - Once validated, can disable separate checks

## Troubleshooting

### Pod stays pending with almanac enabled

1. Check scheduler logs for scoring failures
2. Verify Almanac API is accessible from scheduler pod
3. Confirm node labels are present
4. Try lowering score threshold temporarily
5. Set `failOpen: true` to allow scheduling if API is down

### Scores always too low

1. Adjust weights to prioritize carbon or price based on current conditions
2. Lower the score threshold
3. Check raw carbon intensity and pricing values in API response

### Node info extraction fails

1. Verify standard K8s labels are present on nodes
2. Check if provider ID format is recognized
3. Set default provider/region in scheduler config as fallback

## Related Documentation

- [Almanac API Documentation](https://github.com/elevated-systems/compute-gardener-almanac)
- [Scheduler Configuration](../README.md)
- [Pod Annotations Reference](./annotations.md)
