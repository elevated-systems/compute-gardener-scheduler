# Price-Aware Scheduling

This package provides time-of-use (TOU) based price-aware scheduling for the Compute Gardener Scheduler.

## Overview

The pricing package enables scheduling decisions based on time-of-use electricity rates, allowing workloads to be shifted to periods of lower electricity costs. This is particularly useful for organizations with TOU electricity pricing from their utility providers.

## Implementation

The pricing implementation consists of two main components:

1. **Interface**: Defines the common interface for pricing implementations
2. **TOU Scheduler**: Implements time-of-use based pricing

### Interface

The pricing interface is defined in `interface.go`:

```go
type Implementation interface {
    // GetCurrentRate returns the current electricity rate in $/kWh
    GetCurrentRate(now time.Time) float64
}
```

### Time-of-Use Implementation

The TOU implementation (`tou/scheduler.go`) provides time-based electricity pricing:

- Configurable peak and off-peak rates
- Flexible schedule definition
- Support for different rates by day of week
- Simple configuration via ConfigMap

## Configuration

The TOU pricing configuration consists of two parts:

**Schedule Configuration**:
```yaml
schedules:
  # Monday-Friday peak pricing periods (4pm-9pm)
  - dayOfWeek: "1-5"
    startTime: "16:00"
    endTime: "21:00"
    peakRate: 0.30    # Peak electricity rate in $/kWh
    offPeakRate: 0.10 # Off-peak electricity rate in $/kWh
  # Weekend peak pricing periods (1pm-7pm)
  - dayOfWeek: "0,6"
    startTime: "13:00"
    endTime: "19:00"
    peakRate: 0.30    # Peak electricity rate in $/kWh
    offPeakRate: 0.10 # Off-peak electricity rate in $/kWh
```

Note: The maximum scheduling delay is configured globally in the scheduler configuration.

## Adding New Implementations

To implement a new pricing strategy:

1. Create a new package under `pricing/`
2. Implement the `Implementation` interface
3. Add the implementation to the factory in `interface.go`
4. Add tests for the new implementation

Example implementation:

```go
package custom

import (
    "time"
    "sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
)

type CustomPricing struct {
    config config.PricingConfig
}

func New(config config.PricingConfig) *CustomPricing {
    return &CustomPricing{
        config: config,
    }
}

func (p *CustomPricing) GetCurrentRate(now time.Time) float64 {
    // Implement custom pricing logic
    return rate
}
```

## Testing

The package includes several test utilities:

1. **Mock Implementation**: For testing scheduler behavior
2. **Test Fixtures**: Common test schedules and configurations
3. **Time Utilities**: Helpers for time-based testing

Example test:
```go
func TestTOUScheduler(t *testing.T) {
    cfg := config.PricingConfig{
        Schedules: []config.Schedule{
            {
                DayOfWeek: "1-5",
                StartTime: "16:00",
                EndTime: "21:00",
                PeakRate: 0.30,
                OffPeakRate: 0.10,
            },
        },
    }

    scheduler := tou.New(cfg)
    
    // Test peak period
    peakTime := time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC) // Monday 5 PM
    rate := scheduler.GetCurrentRate(peakTime)
    if rate != 0.30 { // Should be peak rate during peak hours
        t.Errorf("Expected peak rate 0.30, got %f", rate)
    }
}
```

## Best Practices

1. **Schedule Design**
   - Keep schedules simple and predictable
   - Align with utility TOU periods
   - Consider workload patterns

2. **Rate Configuration**
   - Set appropriate peak and off-peak rates
   - Align rates with utility pricing
   - Document rate sources

3. **Testing**
   - Test boundary conditions
   - Verify holiday handling
   - Check daylight saving transitions

4. **Monitoring**
   - Track rate changes
   - Monitor scheduling decisions
   - Record cost savings

## Troubleshooting

Common issues and solutions:

1. **Unexpected Rates**
   - Verify schedule configuration
   - Check time zone handling
   - Validate rate calculations

2. **Schedule Issues**
   - Confirm day of week format
   - Check time format (24-hour)
   - Verify schedule overlaps

3. **Performance**
   - Monitor scheduling latency
   - Check rate calculation overhead
   - Verify caching if implemented

4. **Configuration**
   - Validate ConfigMap format
   - Check environment variables
   - Verify schedule loading
