# Compute Gardener Scheduler Manifests

This directory contains Kubernetes manifests for deploying the Compute Gardener Scheduler.

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
  namespace: kube-system
data:
  compute-gardener-scheduler-config.yaml: |
    apiVersion: kubescheduler.config.k8s.io/v1
    kind: KubeSchedulerConfiguration
    profiles:
      - schedulerName: compute-gardener-scheduler
        plugins:
          preFilter:
            enabled:
              - name: ComputeGardenerScheduler
    leaderElection:
      leaderElect: false
```

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

Time-of-Use Pricing Configuration:
- `PRICING_ENABLED`: Enable price-aware scheduling ("true"/"false", default: "false")
- `PRICING_PROVIDER`: Set to "tou" for time-of-use pricing
- `PRICING_BASE_RATE`: Base electricity rate ($/kWh)
- `PRICING_PEAK_RATE`: Peak rate multiplier (e.g., 1.5 for 50% higher)
- `PRICING_MAX_DELAY`: Maximum delay for price-based scheduling
- `PRICING_SCHEDULES_PATH`: Path to pricing schedules configuration file

## Deployment

1. Create the API key secret:
```bash
kubectl create secret generic compute-gardener-scheduler-secrets \
  --from-literal=electricity-map-api-key=YOUR_API_KEY \
  -n kube-system
```

2. Create the required ConfigMaps:
```bash
kubectl apply -f compute-gardener-scheduler-config.yaml
kubectl apply -f compute-gardener-pricing-schedules.yaml
```

3. Deploy the scheduler:
```bash
kubectl apply -f compute-gardener-scheduler.yaml
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

The deployment includes both a Service and ServiceMonitor for Prometheus integration. To monitor the scheduler effectively, you need Prometheus installed in your cluster.

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
  namespace: cattle-monitoring-system  # Adjust to your Prometheus namespace
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

The scheduler has modest resource requirements:
```yaml
resources:
  requests:
    cpu: '0.1'
    memory: '256Mi'
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
kubectl logs -n kube-system -l component=scheduler
```

2. **Pods not scheduling**: Verify the pod's schedulerName matches:
```bash
kubectl get pod <pod-name> -o yaml | grep schedulerName
```

3. **API errors**: Check API key secret:
```bash
kubectl get secret -n kube-system compute-gardener-scheduler-secrets -o yaml
```

4. **Carbon-aware scheduling not working**: Check carbon configuration:
```bash
# Verify carbon-aware scheduling is enabled
kubectl get deployment -n kube-system compute-gardener-scheduler -o yaml | grep CARBON

# Check scheduler logs for carbon-related issues
kubectl logs -n kube-system -l component=scheduler | grep carbon

# Verify API responses
kubectl logs -n kube-system -l component=scheduler | grep "carbon intensity"
```

5. **TOU pricing not working**: Verify pricing schedules configuration:
```bash
kubectl get configmap -n kube-system compute-gardener-pricing-schedules -o yaml

# Check environment variables
kubectl get deployment -n kube-system compute-gardener-scheduler -o yaml | grep PRICING

# Check scheduler logs
kubectl logs -n kube-system -l component=scheduler | grep pricing
