# Compute Gardener Scheduler Decision Tree

This document provides a state snapshot of the Compute Gardener Scheduler's decision-making process for scheduling pods in a Kubernetes cluster with carbon and price awareness.

## Overall Architecture

The scheduler implements the Kubernetes scheduler framework with two main stages:
1. **PreFilter**: Initial checks and constraint evaluations
2. **Filter**: Node-specific evaluations

## Decision Tree Flow

### 1. PreFilter Stage

```
Start: Pod enters scheduling queue
                    ↓
Check if pod has exceeded maximum scheduling delay
                    ↓
Check if pod has opted out (skip annotation)
                    ↓
Apply namespace energy budget (if applicable)
                    ↓
Is Pricing Enabled?
        ↙                    ↘
     Yes                      No
       ↓                      ↓
Check price constraints   Is Carbon Enabled?
       ↓                        ↙        ↘
Delay scheduling if         Yes          No
peak time and not           ↓            ↓
allowed by threshold   Check carbon    Proceed to
                       constraints    Filter stage
                       ↓
                   Delay scheduling if
                   intensity exceeds
                   threshold
                    ↓
Proceed to Filter stage
```

### 2. Filter Stage

```
Start: Evaluate each node for pod placement
                    ↓
Check if pod has opted out (skip annotation)
                    ↓
Is Hardware Profiling Enabled?
        ↙                    ↘
     Yes                      No
       ↓                      ↓
Check node power profile   Proceed with
and efficiency metrics    basic filtering
       ↓
Check max power requirements
(annotation-based)
       ↓
Check minimum efficiency
requirements (annotation-based)
       ↓
Proceed with node scoring
```

## Detailed Decision Points

### Carbon-Aware Scheduling Logic

1. **Configuration Check**: 
   - Is carbon-aware scheduling enabled in config?
   - Is Electricity Maps API properly configured?

2. **Intensity Threshold Evaluation**:
   - Use global threshold from config or pod-specific threshold from annotation
   - Fetch current carbon intensity from API (with caching)
   - Compare current intensity with threshold
   - If intensity > threshold: Delay scheduling
   - If intensity ≤ threshold: Allow scheduling

3. **Special Cases**:
   - Pod can explicitly enable/disable carbon checks via annotation
   - Failed API calls default to allowing scheduling

### Price-Aware Scheduling Logic

1. **Configuration Check**:
   - Is price-aware scheduling enabled in config?
   - Are TOU schedules properly configured?

2. **Time Evaluation**:
   - Check current time against configured schedules
   - Determine if current time is in peak or off-peak period

3. **Threshold Evaluation**:
   - Check if pod has price threshold annotation
   - If no annotation: Default behavior (delay during peak times)
   - If annotation: Compare threshold with current peak rate
   - If current time is peak and threshold < peak rate: Delay scheduling
   - Otherwise: Allow scheduling

### Hardware Efficiency Filtering Logic

1. **Hardware Profile Check**:
   - Check if node has hardware profile information
   - Calculate effective power with PUE considerations

2. **Power Requirements**:
   - Check pod's max power annotation
   - Compare with node's effective max power
   - Filter out nodes that exceed power requirements

3. **Efficiency Requirements**:
   - Check pod's minimum efficiency annotation
   - Calculate node efficiency metrics
   - Filter out nodes below efficiency requirements

## Pod Annotation Controls

Users can influence scheduling decisions through pod annotations:

### Carbon Controls
- `compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold`: Custom carbon intensity threshold
- `compute-gardener-scheduler.kubernetes.io/carbon-enabled`: Explicitly enable/disable carbon checks

### Price Controls
- `compute-gardener-scheduler.kubernetes.io/price-threshold`: Custom price threshold for peak time scheduling

### Hardware Controls
- `compute-gardener-scheduler.kubernetes.io/max-power-watts`: Maximum power consumption allowed
- `compute-gardener-scheduler.kubernetes.io/min-efficiency`: Minimum efficiency requirement
- `compute-gardener-scheduler.kubernetes.io/gpu-workload-type`: GPU workload type for power modeling

### General Controls
- `compute-gardener-scheduler.kubernetes.io/skip`: Opt out of Compute Gardener scheduling
- `compute-gardener-scheduler.kubernetes.io/max-scheduling-delay`: Custom maximum scheduling delay

## Key Metrics Tracked

- Carbon intensity levels
- Electricity rates
- Scheduling delays (carbon-based and price-based)
- Node power consumption estimates
- Node efficiency metrics
- Energy budget usage

This decision tree represents the core logic of the Compute Gardener Scheduler, enabling carbon and price-aware scheduling while providing user controls through annotations.