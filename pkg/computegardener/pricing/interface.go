package pricing

import (
	"fmt"
	"time"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing/tou"
)

// Implementation defines the interface for electricity pricing implementations
type Implementation interface {
	// GetCurrentRate returns the current electricity rate in $/kWh
	GetCurrentRate(now time.Time) float64
}

// Factory creates pricing implementations based on configuration
func Factory(config config.PricingConfig) (Implementation, error) {
	if !config.Enabled {
		return nil, nil
	}

	switch config.Provider {
	case "tou":
		return tou.New(config), nil
	default:
		return nil, fmt.Errorf("unknown pricing provider: %s", config.Provider)
	}
}
