# Carbon and Cost Calculations

This document explains how the Compute Gardener Scheduler calculates carbon emissions and cost metrics, and how it estimates savings from carbon and price-aware scheduling decisions.

## Table of Contents

- [Overview](#overview)
- [Key Concepts](#key-concepts)
- [Carbon Intensity Tracking](#carbon-intensity-tracking)
- [Scheduler Savings Calculation](#scheduler-savings-calculation)
- [Actual Carbon Emissions Calculation](#actual-carbon-emissions-calculation)
- [Example Scenarios](#example-scenarios)
- [Metrics Reference](#metrics-reference)

## Overview

The scheduler tracks two distinct but related metrics for carbon awareness:

1. **Scheduler Savings** - Measures the benefit of delaying jobs to run during cleaner energy periods
2. **Actual Carbon Emissions** - Measures the actual carbon consumed during job execution

These are calculated differently and serve different purposes.

## Key Concepts

### Three Key Time Points

The scheduler tracks carbon intensity (gCO2/kWh) at three important moments:

1. **Initial Intensity** - When the pod is FIRST delayed due to exceeding the carbon threshold
   - Stored in annotation: `compute-gardener-scheduler.kubernetes.io/initial-carbon-intensity`
   - Represents the carbon intensity that triggered the delay

2. **Bind-Time Intensity** - When the pod actually STARTS executing (binds to a node)
   - Stored in annotation: `bind-time-carbon-intensity`
   - Represents the carbon intensity when the pod passed all filters and was scheduled

3. **Time-Series Intensity** - Carbon intensity throughout the pod's execution
   - Collected every ~15 seconds in `PodMetricsRecord.CarbonIntensity`
   - Used for calculating actual carbon emissions

### Why Not Use "End-of-Execution" Intensity?

**The carbon intensity at pod completion is meaningless for scheduler savings calculations.**

Consider this scenario:
- Pod seen at 350 gCO2/kWh, delayed
- Pod scheduled at 200 gCO2/kWh (intensity improved)
- Pod completes 30 minutes later at 180 gCO2/kWh

The scheduler's contribution was delaying until intensity dropped from 350 → 200. The further drop to 180 during execution is not attributable to the scheduling decision.

Using end-of-execution intensity would incorrectly credit the scheduler with savings from intensity changes that occurred during execution, not due to the delay decision.

## Carbon Intensity Tracking

### Data Source

Carbon intensity data comes from the [Electricity Maps API](https://api-portal.electricitymaps.com/), which provides real-time grid carbon intensity (gCO2eq/kWh) for different regions.

### Collection Points

```
POD LIFECYCLE          CARBON TRACKING
──────────────────────────────────────────────────────────

Pod Created
    ↓
PreFilter Check
    ↓
Intensity > Threshold? ──YES→ Set initial-carbon-intensity ← INITIAL
    ↓ NO                       Mark pod as carbon-delayed
    ↓                          Return Unschedulable
Filter Checks
    ↓
All Pass?
    ↓ YES
Bind to Node ────────────────→ Set bind-time-carbon-intensity ← BIND-TIME
    ↓
Pod Executing ───────────────→ Collect metrics every ~15s
    ↓                          Each record contains:
    ↓                          - Timestamp
    ↓                          - Power estimate
    ↓                          - Carbon intensity ← TIME-SERIES
    ↓
Pod Completes ───────────────→ Calculate savings & emissions
```

## Scheduler Savings Calculation

### Purpose

Measures the carbon reduction (or increase) attributable to the scheduler's decision to delay the pod using a time-series counterfactual analysis.

### Methodology

The scheduler calculates savings by comparing two scenarios with equal precision:

1. **Actual Emissions**: What actually happened (calculated from time-series data)
2. **Counterfactual Emissions**: What would have happened if the job ran during the delay period

Both use the same power profile (since the job's computation is intrinsic) but different carbon intensity time-series.

### Formula

```
Counterfactual Emissions (gCO2) = ∑ [Energy_interval × Historical_Intensity_interval]
Actual Emissions (gCO2)         = ∑ [Energy_interval × Execution_Intensity_interval]
Carbon Savings (gCO2)           = Counterfactual - Actual
```

Where:
- `Energy_interval` = Same power profile for both (trapezoid integration)
- `Historical_Intensity_interval` = Carbon intensity from Prometheus during [initial_time, initial_time + execution_duration]
- `Execution_Intensity_interval` = Carbon intensity during actual execution

### When Calculated

Only calculated if:
1. Pod was **actually delayed** by carbon constraints (tracked in `carbonDelayedPods` map)
2. Initial and bind timestamps are available
3. Prometheus has historical carbon intensity data for the delay period

### Example with Time-Series Data

```
Job delayed at 10:00 AM, ran at 11:30 AM, finished at 1:00 PM (90 min execution)

Actual Emissions (11:30 AM - 1:00 PM):
  Power: 250W, 300W, 280W, 260W... (from metrics)
  Intensity: 250, 245, 260, 255... gCO2/kWh (from Prometheus)
  Actual = ∫(power × intensity) dt = 410 gCO2

Counterfactual Emissions (10:00 AM - 11:30 AM with same power):
  Power: 250W, 300W, 280W, 260W... (same as actual)
  Intensity: 450, 440, 430, 420... gCO2/kWh (historical from Prometheus)
  Counterfactual = ∫(power × historical_intensity) dt = 620 gCO2

Savings = 620 - 410 = 210 gCO2 saved
```

### Key Advantages Over Simple Methodology

**Old approach** (before this enhancement):
```
Savings = (initial_intensity - bind_intensity) × total_energy
        = (450 - 250) × 2.5 = 500 gCO2  ← Crude estimate
```

**New approach** (time-series counterfactual):
```
Savings = counterfactual_emissions - actual_emissions
        = 620 - 410 = 210 gCO2  ← Accounts for intensity variations
```

The new approach:
- Uses the same granular methodology for both actual and counterfactual
- Accounts for grid intensity variations during both periods
- Provides justified precision by using real historical data
- Avoids false precision from mixing methodologies

### Implementation

See:
- `processPodCompletionMetrics()` in `pkg/computegardener/pod_completion.go:194-245`
- `calculateCounterfactualCarbonEmissions()` in `pkg/computegardener/pod_completion.go:458-624`

```go
// Query historical intensity from Prometheus for delay period
historicalIntensity := QueryHistoricalCarbonIntensity(initialTime, counterfactualEnd)

// Calculate counterfactual emissions using same power profile
for each interval in execution:
    energy_interval = trapezoid_integration(power)
    historical_intensity_avg = match_historical_intensity(interval)
    counterfactual += energy_interval × historical_intensity_avg

// Calculate savings
savings = counterfactual - actual_emissions
```

## Actual Carbon Emissions Calculation

### Purpose

Measures the actual carbon emissions during job execution, accounting for varying grid intensity over time.

### Formula

```
Carbon Emissions (gCO2) = ∑ [Energy_interval × Avg_Carbon_Intensity_interval]

Where:
  Energy_interval = ((Power_current + Power_previous) / 2) × ΔTime_hours / 1000
  Avg_Carbon_Intensity_interval = (Intensity_current + Intensity_previous) / 2
```

This uses the **trapezoid rule** for numerical integration, accounting for:
- Power variations over time
- **Carbon intensity variations over time** ← Key difference from savings

### Why Time-Series Data?

Long-running jobs may execute during periods of significantly varying grid carbon intensity. Using a fixed intensity value (from start or end) would be inaccurate.

Example: A 4-hour job during morning hours:
```
Time      Power    Intensity   Interval Energy   Interval Carbon
06:00     250W     450 gCO2    -                 -
07:00     300W     380 gCO2    0.275 kWh         114.1 gCO2
08:00     280W     320 gCO2    0.290 kWh         101.5 gCO2
09:00     260W     280 gCO2    0.270 kWh         81.0 gCO2
10:00     250W     250 gCO2    0.255 kWh         67.7 gCO2

Total Energy: 1.09 kWh
Total Carbon: 364.3 gCO2 (using time-series data)

If we incorrectly used fixed start intensity:
Total Carbon: 1.09 × 450 = 490.5 gCO2 (overestimate by 35%)

If we incorrectly used fixed end intensity:
Total Carbon: 1.09 × 250 = 272.5 gCO2 (underestimate by 25%)
```

### Implementation

See `CalculateTotalCarbonEmissions()` in `pkg/computegardener/metrics/utils.go:136-179`

```go
// Integrate over time series using trapezoid rule
for i := 1; i < len(records); i++ {
    current := records[i]
    previous := records[i-1]

    deltaHours := current.Timestamp.Sub(previous.Timestamp).Hours()
    avgPower := (current.PowerEstimate + previous.PowerEstimate) / 2
    intervalEnergy := (avgPower * deltaHours) / 1000

    // Uses actual intensity values at each timestamp
    avgCarbonIntensity := (current.CarbonIntensity + previous.CarbonIntensity) / 2
    intervalCarbon := intervalEnergy * avgCarbonIntensity

    totalCarbonEmissions += intervalCarbon
}
```

## Example Scenarios

### Scenario 1: Successful Delay with Clean Energy

```
Timeline:
10:00 AM - Pod created, intensity = 450 gCO2/kWh
           Threshold = 300 gCO2/kWh
           Pod DELAYED (initial-carbon-intensity = 450)

11:30 AM - Intensity drops to 250 gCO2/kWh
           Pod SCHEDULED (bind-time-carbon-intensity = 250)

11:30 AM - 1:00 PM - Pod executes
           Time-series intensities: 250, 245, 260, 255 gCO2/kWh
           Total energy: 3.2 kWh

Results:
- Scheduler Savings: (450 - 250) × 3.2 = 640 gCO2 saved
- Actual Emissions: ~816 gCO2 (using time-series: avg ~255 × 3.2)
- Counterfactual (if run immediately): 450 × 3.2 = 1,440 gCO2
```

### Scenario 2: Delayed But No Improvement

```
Timeline:
10:00 AM - Pod created, intensity = 350 gCO2/kWh
           Threshold = 300 gCO2/kWh
           Pod DELAYED (initial-carbon-intensity = 350)

10:30 AM - Intensity still 350 gCO2/kWh
           Max delay reached, pod SCHEDULED anyway
           (bind-time-carbon-intensity = 350)

10:30 AM - 11:00 AM - Pod executes
           Time-series intensities: 350, 340, 330 gCO2/kWh
           Total energy: 1.5 kWh

Results:
- Scheduler Savings: (350 - 350) × 1.5 = 0 gCO2 (no benefit)
- Actual Emissions: ~510 gCO2 (using time-series: avg ~340 × 1.5)
```

### Scenario 3: Immediate Execution (No Delay)

```
Timeline:
2:00 PM - Pod created, intensity = 200 gCO2/kWh
          Threshold = 300 gCO2/kWh
          Pod immediately SCHEDULED (no delay, no initial annotation)

2:00 PM - 3:00 PM - Pod executes
          Time-series intensities: 200, 210, 220, 215 gCO2/kWh
          Total energy: 2.0 kWh

Results:
- Scheduler Savings: Not calculated (pod was never carbon-delayed)
- Actual Emissions: ~425 gCO2 (using time-series: avg ~212.5 × 2.0)
```

## Cost Savings Calculation

The same methodology applies to electricity cost savings, using electricity rates ($/kWh) instead of carbon intensity:

```
Cost Savings ($) = (Initial Rate - Bind-Time Rate) × Total Energy (kWh)
```

Annotations:
- `compute-gardener-scheduler.kubernetes.io/initial-electricity-rate`
- `bind-time-electricity-rate`

## Metrics Reference

### Prometheus Metrics

**Scheduler Effectiveness Metrics**
```promql
# Carbon savings (can be positive or negative)
compute_gardener_scheduler_estimated_savings{type="carbon",unit="grams_co2"}

# Cost savings (can be positive or negative)
compute_gardener_scheduler_estimated_savings{type="cost",unit="dollars"}

# Intensity difference (initial - bind)
compute_gardener_scheduler_scheduling_efficiency{metric="carbon_intensity_delta"}
```

**Actual Usage Metrics**
```promql
# Total energy consumed
compute_gardener_scheduler_job_energy_usage_kwh

# Total carbon emissions (actual, using time-series data)
compute_gardener_scheduler_job_carbon_emissions_grams

# Counterfactual carbon emissions (what would have happened)
compute_gardener_scheduler_job_counterfactual_carbon_emissions_grams

# GPU-specific energy
compute_gardener_scheduler_job_gpu_energy_usage_kwh
```

**Current Conditions**
```promql
# Current carbon intensity
compute_gardener_scheduler_carbon_intensity

# Current electricity rate
compute_gardener_scheduler_electricity_rate

# Delay counts
compute_gardener_scheduler_carbon_delay_total
compute_gardener_scheduler_price_delay_total
```

## Code References

- **Savings Calculation**: `pkg/computegardener/pod_completion.go:194-322`
- **Emissions Calculation**: `pkg/computegardener/metrics/utils.go:136-179`
- **Energy Calculation**: `pkg/computegardener/metrics/utils.go:8-53`
- **Metrics Collection**: `pkg/computegardener/metrics_collection.go`
- **Initial Intensity Tracking**: `pkg/computegardener/scheduler.go:931-982`
- **Bind-Time Intensity Tracking**: `pkg/computegardener/scheduler.go:741-798`

## Limitations

### In-Memory Delay State

The `carbonDelayedPods` map tracking whether a pod was delayed is stored in memory. If the scheduler restarts while a pod is running, savings calculations won't be performed for that pod after completion.

**Workaround**: Consider persisting delay state as a pod annotation if scheduler restart resilience is critical.

### Missing Bind-Time Annotation

If the `bind-time-carbon-intensity` annotation is missing (e.g., from scheduler restart), the system falls back to using the first metrics record's intensity. If no metrics records exist, savings calculation is skipped and an error is logged.

## Best Practices

1. **Monitor Both Metrics**: Track both scheduler savings and actual emissions for a complete picture
2. **Set Appropriate Thresholds**: Carbon intensity thresholds should reflect your regional grid characteristics
3. **Account for Delays**: Use `max-scheduling-delay` annotations to prevent indefinite delays
4. **Validate Savings**: Negative savings indicate the scheduler scheduled during higher intensity (acceptable if max delay reached)
5. **Use Time-Series Data**: Ensure Prometheus is collecting metrics every ~15 seconds for accurate emissions tracking

## Related Documentation

- [Getting Started Guide](./getting-started.md)
- [Main README](../README.md)
- [Price-Aware Scheduling](../pkg/computegardener/price/README.md)
