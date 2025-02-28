package computegardener

import (
	"context"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/computegardener/cache"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/carbon"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing/mock"
)

// testConfig wraps config.Config to implement runtime.Object
type testConfig struct {
	config.Config
}

func (c *testConfig) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

func (c *testConfig) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

// mockHandle implements framework.Handle for testing
type mockHandle struct {
	framework.Handle
}

func (m *mockHandle) KubeConfig() *rest.Config {
	return nil
}

func (m *mockHandle) ClientSet() kubernetes.Interface {
	return &mockClientSet{}
}

// mockClientSet implements kubernetes.Interface for testing
type mockClientSet struct {
	kubernetes.Interface
}

func (m *mockClientSet) CoreV1() corev1.CoreV1Interface {
	return &mockCoreV1{}
}

// mockCoreV1 implements corev1.CoreV1Interface for testing
type mockCoreV1 struct {
	corev1.CoreV1Interface
}

func setupTest(_ *testing.T) func() {
	return func() {
		legacyregistry.Reset()
	}
}

func newTestScheduler(cfg *config.Config, carbonIntensity float64, rate float64, mockTime time.Time) *ComputeGardenerScheduler {
	mockClient := api.NewClient(config.APIConfig{
		ElectricityMapKey:    "mock-key",
		ElectricityMapRegion: "mock-region",
		Timeout:              time.Second,
		RateLimit:            10,
		ElectricityMapURL:    "http://mock-url/",
	})

	cache := schedulercache.New(time.Minute, time.Hour)
	cache.Set(cfg.API.ElectricityMapRegion, &api.ElectricityData{
		CarbonIntensity: carbonIntensity,
		Timestamp:       mockTime,
	})

	var carbonImpl carbon.Implementation
	if cfg.Carbon.Enabled {
		carbonImpl = carbon.New(&cfg.Carbon, mockClient)
	}

	return &ComputeGardenerScheduler{
		handle:       &mockHandle{},
		config:       cfg,
		apiClient:    mockClient,
		cache:        cache,
		pricingImpl:  mock.New(rate),
		carbonImpl:   carbonImpl,
		clock:        clock.NewMockClock(mockTime),
		powerMetrics: sync.Map{},
	}
}

func TestNew(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	tests := []struct {
		name    string
		obj     runtime.Object
		wantErr bool
	}{
		{
			name: "valid config",
			obj: &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						ElectricityMapKey: "test-key",
					},
					Carbon: config.CarbonConfig{
						IntensityThreshold: 200,
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "nil config",
			obj:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.obj, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPreFilter(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonEnabled   bool
		carbonIntensity float64
		threshold       float64
		electricityRate float64
		maxDelay        time.Duration
		podCreationTime time.Time
		wantStatus      *framework.Status
	}{
		{
			name: "pod should schedule - carbon disabled",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonEnabled:   false,
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should not schedule - carbon enabled, over threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name: "pod should schedule - opted out",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						"compute-gardener-scheduler.kubernetes.io/skip": "true",
					},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should schedule - max delay exceeded",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime.Add(-25 * time.Hour)),
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 250,
			threshold:       200,
			maxDelay:        24 * time.Hour,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, "maximum scheduling delay exceeded"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						ElectricityMapKey:    "test-key",
						ElectricityMapRegion: "test-region",
					},
					Carbon: config.CarbonConfig{
						Enabled:            tt.carbonEnabled,
						IntensityThreshold: tt.threshold,
					},
					Scheduling: config.SchedulingConfig{
						MaxSchedulingDelay: tt.maxDelay,
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, tt.electricityRate, tt.podCreationTime)

			result, status := scheduler.PreFilter(context.Background(), nil, tt.pod)
			if result != nil {
				t.Errorf("PreFilter() expected nil result, got %v", result)
			}
			if status.Code() != tt.wantStatus.Code() || status.Message() != tt.wantStatus.Message() {
				t.Errorf("PreFilter() status = %v, want %v", status, tt.wantStatus)
			}
		})
	}
}

func TestCheckPricingConstraints(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		pod        *v1.Pod
		rate       float64
		schedules  []config.Schedule
		wantStatus *framework.Status
	}{
		{
			name: "under off-peak rate",
			pod:  &v1.Pod{},
			rate: 0.12,
			schedules: []config.Schedule{
				{
					DayOfWeek:   "0,1,2,3,4,5,6",
					StartTime:   "00:00",
					EndTime:     "23:59",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
			wantStatus: framework.NewStatus(framework.Success, ""),
		},
		{
			name: "over off-peak rate",
			pod:  &v1.Pod{},
			rate: 0.18,
			schedules: []config.Schedule{
				{
					DayOfWeek:   "0,1,2,3,4,5,6",
					StartTime:   "00:00",
					EndTime:     "23:59",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.180/kWh) exceeds threshold ($0.150/kWh)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Pricing: config.PricingConfig{
						Enabled:   true,
						Provider:  "tou",
						Schedules: tt.schedules,
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, 0, tt.rate, baseTime)

			got := scheduler.checkPricingConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkPricingConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestHandlePodCompletion(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonEnabled   bool
		carbonIntensity float64
		duration        time.Duration
		containerCPU    float64
	}{
		{
			name: "pod completion with carbon enabled",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: v1.PodSpec{
					NodeName: "test-node",
				},
				Status: v1.PodStatus{
					StartTime: &metav1.Time{Time: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Terminated: &v1.ContainerStateTerminated{},
							},
						},
					},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 200,
			duration:        time.Hour,
			containerCPU:    0.5,
		},
		{
			name: "pod completion with carbon disabled",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-2",
				},
				Spec: v1.PodSpec{
					NodeName: "test-node",
				},
				Status: v1.PodStatus{
					StartTime: &metav1.Time{Time: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Terminated: &v1.ContainerStateTerminated{},
							},
						},
					},
				},
			},
			carbonEnabled:   false,
			carbonIntensity: 200,
			duration:        time.Hour,
			containerCPU:    0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Carbon: config.CarbonConfig{
						Enabled: tt.carbonEnabled,
					},
					Power: config.PowerConfig{
						DefaultIdlePower: 100,
						DefaultMaxPower:  400,
					},
				},
			}

			mockTime := tt.pod.Status.StartTime.Time.Add(tt.duration)
			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, 0, mockTime)

			// Test handlePodCompletion
			scheduler.handlePodCompletion(tt.pod)

			// Note: In a real test we would verify metrics were recorded correctly
			// For now we're just verifying the function runs without error
		})
	}
}
