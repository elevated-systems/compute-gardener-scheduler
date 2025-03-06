# Compute Gardener Scheduler Manifests

This directory contains Kubernetes manifests for deploying the Compute Gardener Scheduler.

**Note:** The scheduler is deployed to the `compute-gardener` namespace by default and runs as a second scheduler alongside the default Kubernetes scheduler. This makes it compatible with all Kubernetes distributions, including managed Kubernetes services like GKE Autopilot, EKS Fargate, and AKS with Virtual Nodes.

## Components

The deployment consists of several key components:

1. **ServiceAccount**: Required permissions for the scheduler
2. **RBAC**: Role bindings for scheduler permissions
3. **Secret**: API key for Electricity Map
4. **ConfigMaps**: Configuration for the scheduler and TOU pricing schedules
5. **Deployment**: The scheduler deployment itself

## Configuration

### Scheduler Configuration

The scheduler configuration is managed through a ConfigMap (`compute-gardener-scheduler-config`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: compute-gardener-scheduler-config
  namespace: compute-gardener
data:
  compute-gardener-scheduler-config.yaml: |
    apiVersion: kubescheduler.config.k8s.io/v1
    kind: KubeSchedulerConfiguration
    profiles:
      # SCHEDULER MODE:
      # - Secondary mode (current): Only schedules pods that explicitly request this scheduler
      # - Primary mode: Change schedulerName to "default-scheduler" to handle ALL pods (not recommended)
      - schedulerName: compute-gardener-scheduler
        plugins:
          preFilter:
            enabled:
              - name: ComputeGardenerScheduler
    leaderElection:
      leaderElect: false
```

#### Scheduler Modes

By default, the scheduler runs in **Secondary Mode**, where it only handles pods that explicitly set `schedulerName: compute-gardener-scheduler` in their spec. This is the recommended mode for most environments.

For advanced users, the scheduler can be configured to run in **Primary Mode** by changing the `schedulerName` value to `default-scheduler` in the ConfigMap. In this mode, the scheduler will handle ALL pods in the cluster. This is NOT recommended for most installations as it will cause ALL pods to be subject to carbon/price-based scheduling policies, potentially delaying critical system components.

### Time-of-Use Pricing Schedules

Pricing schedules are configured through a ConfigMap (`compute-gardener-pricing-schedules`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: compute-gardener-pricing-schedules
  namespace: kube-system
data:
  schedules.yaml: |
    # Time-of-use pricing schedule configuration
    # Format: day-of-week start-time end-time
    # day-of-week: 0-6 (Sunday=0)
    # time format: HH:MM in 24-hour format
    schedules:
      # Monday-Friday peak pricing periods (4pm-9pm)
      - dayOfWeek: "1-5"
        startTime: "16:00"
        endTime: "21:00"
      # Weekend peak pricing periods (1pm-7pm)
      - dayOfWeek: "0,6" 
        startTime: "13:00"
        endTime: "19:00"
```

### API Key Configuration

The Electricity Map API key is stored in a Kubernetes secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: compute-gardener-scheduler-secrets
  namespace: kube-system
type: Opaque
data:
  electricity-map-api-key: <base64-encoded-api-key>
```

### Environment Variables

The scheduler is configured through environment variables in the deployment:

Compute Gardener Configuration:
- `ELECTRICITY_MAP_API_KEY`: API key from secret (required)
- `CARBON_ENABLED`: Enable carbon-aware scheduling ("true"/"false", default: "true")
- `CARBON_INTENSITY_THRESHOLD`: Base carbon intensity threshold (gCO2/kWh)
- `MAX_SCHEDULING_DELAY`: Maximum time to delay pod scheduling
- `HARDWARE_PROFILES_PATH`: Path to hardware profiles configuration file

Time-of-Use Pricing Configuration:
- `PRICING_ENABLED`: Enable price-aware scheduling ("true"/"false", default: "false")
- `PRICING_PROVIDER`: Set to "tou" for time-of-use pricing
- `PRICING_BASE_RATE`: Base electricity rate ($/kWh)
- `PRICING_PEAK_RATE`: Peak rate multiplier (e.g., 1.5 for 50% higher)
- `PRICING_MAX_DELAY`: Maximum delay for price-based scheduling
- `PRICING_SCHEDULES_PATH`: Path to pricing schedules configuration file

### Hardware Profiles

The scheduler uses hardware profiles to estimate power consumption of different CPU and GPU models. These profiles are defined in the `compute-gardener-scheduler-hw-profiles.yaml` ConfigMap:

```yaml
cpuProfiles:
  "Intel(R) Xeon(R) Platinum 8275CL":
    idlePower: 10.5         # Power consumption at idle (watts)
    maxPower: 120.0         # Power consumption at full load (watts)
    numCores: 24            # Number of CPU cores
    baseFrequencyGHz: 2.5   # Base CPU frequency in GHz
    powerScaling: "quadratic" # Power scaling model (linear, quadratic, cubic)
    frequencyRangeGHz:      # Supported frequency range
      min: 1.2
      max: 3.6

gpuProfiles:
  "NVIDIA A100":
    idlePower: 25.0  # Power consumption at idle (watts)
    maxPower: 400.0  # Power consumption at full load (watts)

memProfiles:
  "DDR4-2666 ECC":
    idlePowerPerGB: 0.125   # Idle power per GB of memory (watts)
    maxPowerPerGB: 0.375    # Max power per GB of memory (watts)
    baseIdlePower: 1.0      # Base power for memory controller (watts)
```

The hardware profiles work in conjunction with the CPU frequency exporter to provide more accurate power estimates based on actual CPU frequencies. You can customize these profiles to match your specific hardware or add new profiles for hardware not included in the default configuration.

## Deployment

### Prerequisites

If you're using ServiceMonitor for Prometheus integration (standard installation), you'll need to install the Prometheus Operator CRDs first:

```bash
kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml
```

### Installation Options

There are two installation options:

#### 1. Standard Installation (with Prometheus metrics)

```bash
# Create namespace
kubectl create namespace compute-gardener

# Create API key secret
kubectl create secret generic compute-gardener-scheduler-secrets \
  --from-literal=electricity-map-api-key=YOUR_API_KEY \
  -n compute-gardener

# Deploy scheduler with metrics
kubectl apply -f compute-gardener-scheduler.yaml
kubectl apply -f compute-gardener-scheduler-hw-profiles.yaml
```

To uninstall:

```bash
kubectl delete -f compute-gardener-scheduler.yaml
kubectl delete -f compute-gardener-scheduler-hw-profiles.yaml
```

#### 2. Minimal Installation (without Prometheus metrics)

For a more lightweight installation or for clusters without Prometheus:

```bash
# Create namespace
kubectl create namespace compute-gardener

# Create API key secret
kubectl create secret generic compute-gardener-scheduler-secrets \
  --from-literal=electricity-map-api-key=YOUR_API_KEY \
  -n compute-gardener

# Deploy scheduler without metrics integration
kubectl apply -f compute-gardener-scheduler-no-metrics.yaml
kubectl apply -f compute-gardener-scheduler-hw-profiles.yaml
```

To uninstall:

```bash
kubectl delete -f compute-gardener-scheduler-no-metrics.yaml
kubectl delete -f compute-gardener-scheduler-hw-profiles.yaml
```

## Using the Scheduler

### Pod Configuration

To use the compute-gardener scheduler for a pod, set the scheduler name in the pod spec:

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

### Pod Annotations

Pods can control scheduling behavior using annotations:

```yaml
metadata:
  annotations:
    # Opt out of compute-gardener scheduling
    compute-gardener-scheduler.kubernetes.io/skip: "true"
    
    # Disable carbon-aware scheduling for this pod only
    compute-gardener-scheduler.kubernetes.io/carbon-enabled: "false"
    
    # Set custom carbon intensity threshold
    compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"
    
    # Set custom price threshold
    compute-gardener-scheduler.kubernetes.io/price-threshold: "0.15"
```

## Monitoring

The scheduler exposes metrics and health checks on port 10259 (HTTPS):

- Health checks: `/healthz` 
- Metrics: `/metrics`

The deployment includes both a Service and ServiceMonitor for Prometheus integration. While Prometheus is not strictly required for the scheduler to function, it is highly recommended for monitoring performance and validating carbon/cost savings. Without Prometheus, you'll miss valuable insights into how the scheduler is performing and won't have visibility into the actual emissions and cost reductions achieved.

Available metrics include:

- `scheduler_compute_gardener_carbon_intensity`: Current carbon intensity (gCO2eq/kWh) for a given region
- `scheduler_compute_gardener_electricity_rate`: Current electricity rate ($/kWh) for a given location
- `scheduler_compute_gardener_scheduling_attempt_total`: Number of attempts to schedule pods by result
- `scheduler_compute_gardener_pod_scheduling_duration_seconds`: Latency for scheduling attempts
- `scheduler_compute_gardener_estimated_savings`: Estimated savings from scheduling (carbon, cost)
- `scheduler_compute_gardener_price_delay_total`: Number of scheduling delays due to price thresholds
- `scheduler_compute_gardener_carbon_delay_total`: Number of scheduling delays due to carbon intensity thresholds
- `scheduler_compute_gardener_node_cpu_usage_cores`: CPU usage on nodes at baseline and completion
- `scheduler_compute_gardener_node_power_estimate_watts`: Estimated node power consumption
- `scheduler_compute_gardener_job_energy_usage_kwh`: Estimated energy usage for completed jobs
- `scheduler_compute_gardener_job_carbon_emissions_grams`: Estimated carbon emissions for completed jobs
- `scheduler_compute_gardener_scheduling_efficiency`: Scheduling efficiency metrics (carbon/cost improvements)

### Metrics Collection Components

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
  namespace: monitoring  # Standard Prometheus namespace, change if your cluster uses a different one
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

## Resource Requirements

The scheduler has modest resource requests/limits:
```yaml
resources:
  requests:
      cpu: 50m
      memory: 128Mi
    limits:
      cpu: 200m
      memory: 256Mi
```

## Security Context

The scheduler runs with non-root privileges:
```yaml
securityContext:
  privileged: false
```

## Troubleshooting

Common issues and solutions:

1. **Scheduler not starting**: Check the scheduler logs:
```bash
kubectl logs -n compute-gardener -l component=scheduler
```

2. **Pods not scheduling**: Verify the pod's schedulerName matches:
```bash
kubectl get pod <pod-name> -o yaml | grep schedulerName
```

3. **API errors**: Check API key secret:
```bash
kubectl get secret -n compute-gardener compute-gardener-scheduler-secrets -o yaml
```

4. **Zero or negative energy values**: If you see log messages like:
```
Warning: Zero or negative energy value being recorded
```

There are several potential causes:
- The pod ran for too short a duration to collect meaningful metrics
- Metrics Server is not installed or functioning properly
- The pod's CPU/memory usage was too low to register significant energy consumption

Solutions:
- Use the example pods in the `/examples` directory, which run longer workloads
- Verify Metrics Server is installed and functioning properly:
```bash
kubectl get apiservices | grep metrics
kubectl get --raw "/apis/metrics.k8s.io/v1beta1/nodes" | jq
```
- For testing, increase pod resource requests/limits to generate more measurable metrics

5. **Carbon-aware scheduling not working**: Check carbon configuration:
```bash
# Verify carbon-aware scheduling is enabled
kubectl get deployment -n compute-gardener compute-gardener-scheduler -o yaml | grep CARBON

# Check scheduler logs for carbon-related issues
kubectl logs -n compute-gardener -l component=scheduler | grep carbon

# Verify API responses
kubectl logs -n compute-gardener -l component=scheduler | grep "carbon intensity"
```

6. **TOU pricing not working**: Verify pricing schedules configuration:
```bash
kubectl get configmap -n compute-gardener compute-gardener-pricing-schedules -o yaml

# Check environment variables
kubectl get deployment -n compute-gardener compute-gardener-scheduler -o yaml | grep PRICING

# Check scheduler logs
kubectl logs -n compute-gardener -l component=scheduler | grep pricing
```

7. **Scheduler monitoring pods it shouldn't manage**: This can happen if the scheduler is configured in primary mode. 
Check the ConfigMap configuration:
```bash
kubectl get configmap -n compute-gardener compute-gardener-scheduler-config -o yaml
```
If you see `schedulerName: default-scheduler`, the scheduler is running in primary mode. Update the ConfigMap to use `compute-gardener-scheduler` instead for secondary mode operation.

8. **ServiceMonitor issues**: If you're having issues with Prometheus monitoring:
```bash
# Check if ServiceMonitor CRD is installed
kubectl get crd | grep servicemonitors

# Install ServiceMonitor CRD if missing
kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml

# Or use the no-metrics version instead
kubectl apply -f compute-gardener-scheduler-no-metrics.yaml
```
