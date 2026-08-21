package computegardener

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	testingmocks "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/testing"
)

func TestGetCarbonIntensityMode(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "no annotation defaults to threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			expected: "threshold",
		},
		{
			name: "forecast mode",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						common.AnnotationCarbonIntensityMode: "forecast",
					},
				},
			},
			expected: "forecast",
		},
		{
			name: "threshold mode explicit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						common.AnnotationCarbonIntensityMode: "threshold",
					},
				},
			},
			expected: "threshold",
		},
		{
			name: "invalid mode defaults to threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						common.AnnotationCarbonIntensityMode: "invalid-mode",
					},
				},
			},
			expected: "threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := newTestScheduler(&config.Config{
				Carbon: config.CarbonConfig{
					Enabled: true,
					APIConfig: config.ElectricityMapsAPIConfig{
						Region: "test-region",
					},
				},
			}, 100.0, 0.1, baseTime)

			mode := cs.getCarbonIntensityMode(tt.pod)
			if mode != tt.expected {
				t.Errorf("getCarbonIntensityMode() = %q, want %q", mode, tt.expected)
			}
		})
	}
}

// mockCarbonImplForForecast is a carbon impl that returns configurable forecast
type mockCarbonImplForForecast struct {
	carbon.Implementation
	forecast    *api.ElectricityForecast
	forecastErr error
}

func (m *mockCarbonImplForForecast) GetForecast(ctx context.Context, horizonHours int) (*api.ElectricityForecast, error) {
	if m.forecastErr != nil {
		return nil, m.forecastErr
	}
	return m.forecast, nil
}

func TestFindForecastWindow(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name               string
		currentIntensity   float64
		threshold          float64
		forecast           *api.ElectricityForecast
		forecastErr        error
		maxDelay           time.Duration
		wantWait           bool
		wantReasonContains string
	}{
		{
			name:             "forecast API error falls back to threshold",
			currentIntensity: 250,
			threshold:        200,
			forecastErr:      errors.New("forecast API failed"),
			maxDelay:         time.Hour,
			wantWait:         false, // ok=false means fall back, caller uses threshold path
		},
		{
			name:             "forecast has acceptable window within max delay",
			currentIntensity: 250,
			threshold:        200,
			forecast: &api.ElectricityForecast{
				Zone: "test-region",
				Data: []api.ForecastData{
					{Datetime: time.Now().Add(30 * time.Minute), CarbonIntensity: 150},
					{Datetime: time.Now().Add(1 * time.Hour), CarbonIntensity: 120},
				},
			},
			maxDelay:           2 * time.Hour,
			wantWait:           true,
			wantReasonContains: "acceptable window",
		},
		{
			name:             "forecast has no acceptable window but pod not overdue",
			currentIntensity: 250,
			threshold:        200,
			forecast: &api.ElectricityForecast{
				Zone: "test-region",
				Data: []api.ForecastData{
					{Datetime: time.Now().Add(30 * time.Minute), CarbonIntensity: 250},
				},
			},
			maxDelay:           2 * time.Hour,
			wantWait:           true,
			wantReasonContains: "no acceptable forecast windows found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := newTestScheduler(&config.Config{
				Carbon: config.CarbonConfig{
					Enabled: true,
					APIConfig: config.ElectricityMapsAPIConfig{
						Region: "test-region",
					},
				},
				Scheduling: config.SchedulingConfig{
					MaxSchedulingDelay: tt.maxDelay,
				},
			}, 250.0, 0.1, baseTime)

			cs.carbonImpl = &mockCarbonImplForForecast{
				Implementation: testingmocks.NewMockCarbon(250),
				forecast:       tt.forecast,
				forecastErr:    tt.forecastErr,
			}

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(baseTime),
					UID:               "test-pod-uid",
				},
			}

			wait, reason, _, ok := cs.findForecastWindow(context.Background(), pod, tt.currentIntensity, tt.threshold)

			if tt.forecastErr != nil {
				// Should signal fallback (ok=false)
				if ok {
					t.Errorf("findForecastWindow() ok = true, want false (fallback) for forecast error")
				}
				return
			}

			if !ok {
				t.Fatalf("findForecastWindow() ok = false, want true")
			}

			if wait != tt.wantWait {
				t.Errorf("findForecastWindow() wait = %v, want %v", wait, tt.wantWait)
			}

			if tt.wantReasonContains != "" && !strings.Contains(reason, tt.wantReasonContains) {
				t.Errorf("findForecastWindow() reason = %q, want it to contain %q",
					reason, tt.wantReasonContains)
			}
		})
	}
}

// TestPreFilter_ForecastMode tests the end-to-end PreFilter behavior with forecast mode
func TestPreFilter_ForecastMode(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name              string
		carbonEnabled     bool
		carbonIntensity   float64
		threshold         float64
		forecastMode      bool
		forecastErr       bool
		maxDelay          time.Duration
		podCreationOffset time.Duration
		wantStatus        *framework.Status
	}{
		{
			name:              "forecast mode with clean forecast window - should delay",
			carbonEnabled:     true,
			carbonIntensity:   250,
			threshold:         200,
			forecastMode:      true,
			forecastErr:       false,
			maxDelay:          2 * time.Hour,
			podCreationOffset: 0,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"",
			),
		},
		{
			name:              "forecast mode with API error - falls back to threshold",
			carbonEnabled:     true,
			carbonIntensity:   250,
			threshold:         200,
			forecastMode:      true,
			forecastErr:       true,
			maxDelay:          2 * time.Hour,
			podCreationOffset: 0,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name:              "forecast mode max delay exceeded - allows pod",
			carbonEnabled:     true,
			carbonIntensity:   250,
			threshold:         200,
			forecastMode:      true,
			forecastErr:       false,
			maxDelay:          time.Hour,
			podCreationOffset: -2 * time.Hour,
			wantStatus: framework.NewStatus(
				framework.Success,
				"maximum scheduling delay exceeded",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Cache: config.APICacheConfig{
					Timeout:     time.Second,
					MaxRetries:  1,
					RetryDelay:  time.Millisecond,
					RateLimit:   10,
					CacheTTL:    time.Minute,
					MaxCacheAge: time.Hour,
				},
				Carbon: config.CarbonConfig{
					Enabled:            tt.carbonEnabled,
					Provider:           "electricity-maps-api",
					IntensityThreshold: tt.threshold,
					APIConfig: config.ElectricityMapsAPIConfig{
						APIKey: "test-key",
						Region: "test-region",
						URL:    "http://mock-url/",
					},
				},
				Pricing: config.PriceConfig{
					Enabled:  false,
					Provider: "tou",
				},
				Scheduling: config.SchedulingConfig{
					MaxSchedulingDelay: tt.maxDelay,
				},
				Power: config.PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					DefaultGPUPUE:    1.2,
				},
			}

			// Use baseTime as the "current" time; podCreationOffset shifts when the pod was created
			scheduler := newTestScheduler(cfg, tt.carbonIntensity, 0.1, baseTime)

			// Set up mock forecast behavior
			mockForecast := &api.ElectricityForecast{
				Zone: "test-region",
				Data: []api.ForecastData{
					{Datetime: time.Now().Add(30 * time.Minute), CarbonIntensity: 150},
					{Datetime: time.Now().Add(1 * time.Hour), CarbonIntensity: 120},
				},
			}

			mockCarbon := &mockCarbonImplForForecast{
				Implementation: testingmocks.NewMockCarbon(tt.carbonIntensity),
				forecast:       mockForecast,
			}
			if tt.forecastErr {
				mockCarbon.forecastErr = errors.New("forecast API failed")
			}
			scheduler.carbonImpl = mockCarbon

			// Pod creation time is baseTime + offset (negative offset = older pod)
			podCreationTime := baseTime.Add(tt.podCreationOffset)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "test-ns",
					CreationTimestamp: metav1.NewTime(podCreationTime),
					Annotations:       make(map[string]string),
				},
			}
			if tt.forecastMode {
				pod.Annotations[common.AnnotationCarbonIntensityMode] = "forecast"
			}

			state := framework.NewCycleState()
			_, status := scheduler.PreFilter(context.Background(), state, pod)

			// Verify the status code matches
			if status.Code() != tt.wantStatus.Code() {
				t.Errorf("PreFilter() status code = %v, want %v (message: %q)",
					status.Code(), tt.wantStatus.Code(), status.Message())
			}

			// For unschedulable with specific expected message, verify message matches
			if tt.wantStatus.Code() == framework.Unschedulable && tt.wantStatus.Message() != "" {
				if status.Message() != tt.wantStatus.Message() {
					t.Errorf("PreFilter() status message = %q, want %q",
						status.Message(), tt.wantStatus.Message())
				}
			}
		})
	}
}
