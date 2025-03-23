package price

import (
	"fmt"
	"testing"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

func TestFactory(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.PriceConfig
		expectNil   bool
		expectError bool
	}{
		{
			name: "disabled pricing",
			cfg: config.PriceConfig{
				Enabled: false,
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name: "valid tou provider",
			cfg: config.PriceConfig{
				Enabled:  true,
				Provider: "tou",
				Schedules: []config.Schedule{
					{
						Name:      "test-schedule",
						DayOfWeek: "1-5",
						StartTime: "10:00",
						EndTime:   "16:00",
					},
				},
			},
			expectNil:   false,
			expectError: false,
		},
		{
			name: "unknown provider",
			cfg: config.PriceConfig{
				Enabled:  true,
				Provider: "unknown",
			},
			expectNil:   true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl, err := Factory(tt.cfg)

			// Check error
			if (err != nil) != tt.expectError {
				t.Errorf("Factory() error = %v, expectError %v", err, tt.expectError)
				return
			}

			// Check if implementation is nil
			if (impl == nil) != tt.expectNil {
				t.Errorf("Factory() result = %v, expectNil %v", impl, tt.expectNil)
			}
		})
	}
}

// TestFactoryWithProvider ensures the factory returns the correct implementation type
func TestFactoryWithProvider(t *testing.T) {
	cfg := config.PriceConfig{
		Enabled:  true,
		Provider: "tou",
		Schedules: []config.Schedule{
			{
				Name:      "test-schedule",
				DayOfWeek: "1-5",
				StartTime: "10:00",
				EndTime:   "16:00",
			},
		},
	}

	impl, err := Factory(cfg)
	if err != nil {
		t.Fatalf("Factory() error = %v", err)
	}

	// TOU implementation should be type *tou.Scheduler
	if impl == nil {
		t.Fatal("Factory() returned nil implementation")
	}

	// Check the type name (this is a basic check since we can't directly import tou to avoid circular imports)
	typeName := getTypeName(impl)
	if typeName != "*tou.Scheduler" {
		t.Errorf("Factory() returned wrong implementation type = %v, want *tou.Scheduler", typeName)
	}
}

// Helper function to get the type name of an interface
func getTypeName(i interface{}) string {
	if i == nil {
		return "nil"
	}
	
	// This is a simple hack to check if it's a tou.Scheduler
	if fmt.Sprintf("%T", i) == "*tou.Scheduler" {
		return "*tou.Scheduler"
	}
	
	return fmt.Sprintf("%T", i)
}