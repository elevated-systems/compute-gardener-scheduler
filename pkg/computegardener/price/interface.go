package price

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price/tou"
)

// Implementation defines the interface for electricity pricing implementations
type Implementation interface {
	// GetCurrentRate returns the current electricity rate in $/kWh
	GetCurrentRate(now time.Time) float64
	
	// IsPeakTime checks if the given time is within a peak time window
	IsPeakTime(now time.Time) bool

	// CheckPriceConstraints checks if current electricity rate exceeds pod's threshold
	CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status
}

// Factory creates pricing implementations based on configuration
func Factory(config config.PriceConfig) (Implementation, error) {
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
