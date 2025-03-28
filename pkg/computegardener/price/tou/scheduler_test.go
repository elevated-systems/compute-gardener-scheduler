package tou

import (
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

func TestNew(t *testing.T) {
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

	scheduler := New(cfg)
	if scheduler == nil {
		t.Fatal("New() returned nil")
	}

	// Validate the config was set correctly
	if len(scheduler.config.Schedules) != 1 {
		t.Errorf("New() didn't set config correctly, got %d schedules, want 1", len(scheduler.config.Schedules))
	}
}

func TestIsPeakTime(t *testing.T) {
	// Create a config with weekday (Monday-Friday) peak hours from 10:00-16:00
	cfg := config.PriceConfig{
		Enabled:  true,
		Provider: "tou",
		Schedules: []config.Schedule{
			{
				Name:      "test-schedule",
				DayOfWeek: "1-5", // Monday-Friday
				StartTime: "10:00",
				EndTime:   "16:00",
				Timezone:  "UTC",
			},
		},
	}

	scheduler := New(cfg)

	tests := []struct {
		name     string
		time     time.Time
		expected bool
	}{
		{
			name:     "weekday within peak hours",
			time:     time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			expected: true,
		},
		{
			name:     "weekday before peak hours",
			time:     time.Date(2023, 5, 1, 9, 0, 0, 0, time.UTC), // Monday 9:00
			expected: false,
		},
		{
			name:     "weekday after peak hours",
			time:     time.Date(2023, 5, 1, 17, 0, 0, 0, time.UTC), // Monday 17:00
			expected: false,
		},
		{
			name:     "weekend during peak hours time",
			time:     time.Date(2023, 5, 6, 12, 0, 0, 0, time.UTC), // Saturday 12:00
			expected: false,
		},
		{
			name:     "at peak start boundary",
			time:     time.Date(2023, 5, 1, 10, 0, 0, 0, time.UTC), // Monday 10:00
			expected: true,
		},
		{
			name:     "at peak end boundary",
			time:     time.Date(2023, 5, 1, 16, 0, 0, 0, time.UTC), // Monday 16:00
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scheduler.IsPeakTime(tt.time)
			if result != tt.expected {
				t.Errorf("IsPeakTime() = %v, want %v for time %v", result, tt.expected, tt.time)
			}
		})
	}
}

func TestGetCurrentRate(t *testing.T) {
	// Create a config with rates
	cfg := config.PriceConfig{
		Enabled:  true,
		Provider: "tou",
		Schedules: []config.Schedule{
			{
				Name:        "test-schedule",
				DayOfWeek:   "1-5", // Monday-Friday
				StartTime:   "10:00",
				EndTime:     "16:00",
				Timezone:    "UTC",
				PeakRate:    0.30,
				OffPeakRate: 0.15,
			},
		},
	}

	scheduler := New(cfg)

	tests := []struct {
		name         string
		time         time.Time
		expectedRate float64
	}{
		{
			name:         "weekday within peak hours",
			time:         time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			expectedRate: 0.30,                                         // Peak rate
		},
		{
			name:         "weekday outside peak hours",
			time:         time.Date(2023, 5, 1, 9, 0, 0, 0, time.UTC), // Monday 9:00
			expectedRate: 0.15,                                        // Off-peak rate
		},
		{
			name:         "weekend",
			time:         time.Date(2023, 5, 6, 12, 0, 0, 0, time.UTC), // Saturday 12:00
			expectedRate: 0.15,                                         // Off-peak rate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := scheduler.GetCurrentRate(tt.time)
			if rate != tt.expectedRate {
				t.Errorf("GetCurrentRate() = %v, want %v for time %v", rate, tt.expectedRate, tt.time)
			}
		})
	}
}

func TestCheckPriceConstraints(t *testing.T) {
	// Create a config with rates
	cfg := config.PriceConfig{
		Enabled:  true,
		Provider: "tou",
		Schedules: []config.Schedule{
			{
				Name:        "test-schedule",
				DayOfWeek:   "1-5", // Monday-Friday
				StartTime:   "10:00",
				EndTime:     "16:00",
				Timezone:    "UTC",
				PeakRate:    0.30,
				OffPeakRate: 0.15,
			},
		},
	}

	scheduler := New(cfg)

	tests := []struct {
		name           string
		time           time.Time
		pod            *v1.Pod
		expectedStatus framework.Code
	}{
		{
			name: "pod with no threshold during peak time",
			time: time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "pod with high threshold during peak time",
			time: time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						common.AnnotationPriceThreshold: "0.40", // Higher than peak rate
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "pod with low threshold during peak time",
			time: time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						common.AnnotationPriceThreshold: "0.20", // Lower than peak rate
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "pod with invalid threshold",
			time: time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC), // Monday 12:00
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						common.AnnotationPriceThreshold: "invalid",
					},
				},
			},
			expectedStatus: framework.Error,
		},
		{
			name: "pod during off-peak time",
			time: time.Date(2023, 5, 1, 9, 0, 0, 0, time.UTC), // Monday 9:00
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectedStatus: framework.Success,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := scheduler.CheckPriceConstraints(tt.pod, tt.time)
			if status.Code() != tt.expectedStatus {
				t.Errorf("CheckPriceConstraints() = %v, want %v for time %v and pod %v",
					status.Code(), tt.expectedStatus, tt.time, tt.pod.Name)
				t.Logf("Status message: %s", status.Message())
			}
		})
	}
}

func TestContainsDay(t *testing.T) {
	tests := []struct {
		days     string
		day      string
		expected bool
	}{
		{"1,2,3", "2", true},
		{"1,2,3", "4", false},
		{"1-5", "3", true},
		{"1-5", "0", false},
		{"1-5", "6", false},
		{"0,6", "0", true},
		{"0,6", "6", true},
		{"0,6", "3", false},
		{"0-6", "3", true},
		{"invalid", "3", false},
		{"1-invalid", "3", false},
		{"invalid-5", "3", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s contains %s", tt.days, tt.day), func(t *testing.T) {
			result := containsDay(tt.days, tt.day)
			if result != tt.expected {
				t.Errorf("containsDay(%s, %s) = %v, want %v", tt.days, tt.day, result, tt.expected)
			}
		})
	}
}

func TestDescribeDays(t *testing.T) {
	tests := []struct {
		daySpec  string
		expected string
	}{
		{"1-5", "weekdays"},
		{"0,6", "weekends"},
		{"0-6", "all days"},
		{"0", "Sunday"},
		{"1", "Monday"},
		{"1,3,5", "Monday, Wednesday, Friday"},
		{"1-3", "Monday-Wednesday"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.daySpec, func(t *testing.T) {
			result := describeDays(tt.daySpec)
			if result != tt.expected {
				t.Errorf("describeDays(%s) = %s, want %s", tt.daySpec, result, tt.expected)
			}
		})
	}
}
