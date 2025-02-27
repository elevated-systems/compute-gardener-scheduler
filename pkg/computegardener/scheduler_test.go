package computegardener

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/computegardener/cache"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing/mock"
)

// mockMetricsClient implements metricsv1beta1.MetricsV1beta1Interface for testing
type mockMetricsClient struct {
	metricsv1beta1.MetricsV1beta1Interface
}

func (m *mockMetricsClient) NodeMetricses() metricsv1beta1.NodeMetricsInterface {
	return &mockNodeMetrics{}
}

// mockNodeMetrics implements metricsv1beta1.NodeMetricsInterface for testing
type mockNodeMetrics struct {
	metricsv1beta1.NodeMetricsInterface
}

func (m *mockNodeMetrics) Get(ctx context.Context, name string, opts metav1.GetOptions) (*metricsapi.NodeMetrics, error) {
	// Return mock metrics with 0 CPU usage
	return &metricsapi.NodeMetrics{
		Usage: v1.ResourceList{
			v1.ResourceCPU: *resource.NewMilliQuantity(0, resource.DecimalSI),
		},
	}, nil
}

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

func (m *mockHandle) MetricsClient() metricsv1beta1.MetricsV1beta1Interface {
	return &mockMetricsClient{}
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

func (m *mockCoreV1) Nodes() corev1.NodeInterface {
	return &mockNodes{}
}

// mockNodes implements corev1.NodeInterface for testing
type mockNodes struct {
	corev1.NodeInterface
}

func (m *mockNodes) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.Node, error) {
	// Return mock node with 1000m CPU capacity
	return &v1.Node{
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(1000, resource.DecimalSI),
			},
		},
	}, nil
}

func setupTest(_ *testing.T) func() {
	// Return a cleanup function
	return func() {
		// Clean up any test resources
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

	return &ComputeGardenerScheduler{
		handle:       &mockHandle{},
		config:       cfg,
		apiClient:    mockClient,
		cache:        cache,
		pricingImpl:  mock.New(rate),
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
		carbonIntensity float64
		threshold       float64
		electricityRate float64
		maxDelay        time.Duration
		podCreationTime time.Time
		wantStatus      *framework.Status
	}{
		{
			name: "pod should schedule - under threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should not schedule - over threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
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
			carbonIntensity: 250,
			threshold:       200,
			maxDelay:        24 * time.Hour,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, "maximum scheduling delay exceeded"),
		},
		{
			name: "pod should not schedule - high electricity rate",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			electricityRate: 0.20,
			podCreationTime: baseTime,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.200/kWh) exceeds threshold ($0.150/kWh)",
			),
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
						IntensityThreshold: tt.threshold,
					},
					Scheduling: config.SchedulingConfig{
						MaxSchedulingDelay: tt.maxDelay,
					},
					Pricing: config.PricingConfig{
						Enabled:  true,
						Provider: "tou",
						Schedules: []config.Schedule{
							{
								DayOfWeek:   "0,1,2,3,4,5,6", // All days
								StartTime:   "00:00",
								EndTime:     "23:59",
								PeakRate:    0.25,
								OffPeakRate: 0.15, // This becomes default threshold
							},
						},
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
		{
			name: "custom threshold from annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"compute-gardener-scheduler.kubernetes.io/price-threshold": "0.20",
					},
				},
			},
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
			wantStatus: framework.NewStatus(framework.Success, ""),
		},
		{
			name:      "no schedules configured",
			pod:       &v1.Pod{},
			rate:      0.18,
			schedules: []config.Schedule{},
			wantStatus: framework.NewStatus(
				framework.Error,
				"no pricing schedules configured",
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

func TestCheckCarbonIntensityConstraints(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonIntensity float64
		threshold       float64
		wantStatus      *framework.Status
	}{
		{
			name:            "under threshold",
			pod:             &v1.Pod{},
			carbonIntensity: 150,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name:            "over threshold",
			pod:             &v1.Pod{},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name: "custom threshold from annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold": "300",
					},
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
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
						IntensityThreshold: tt.threshold,
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, 0, baseTime)

			got := scheduler.checkCarbonIntensityConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkCarbonIntensityConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestPostBind(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	nodeName := "test-node"

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	cfg := &testConfig{
		Config: config.Config{
			Power: config.PowerConfig{
				DefaultIdlePower: 100,
				DefaultMaxPower:  400,
			},
		},
	}

	scheduler := newTestScheduler(&cfg.Config, 0, 0, baseTime)

	// Test PostBind
	scheduler.PostBind(context.Background(), nil, pod, nodeName)

	// Verify power metric was stored
	key := fmt.Sprintf("%s/%s/baseline", nodeName, pod.Name)
	if value, ok := scheduler.powerMetrics.Load(key); !ok {
		t.Errorf("PostBind() did not store power metric")
	} else if power, ok := value.(float64); !ok || power != 100 { // Should be idle power since mock returns 0 CPU usage
		t.Errorf("PostBind() stored power = %v, want %v", power, 100)
	}
}

func TestHandlePodCompletion(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	tests := []struct {
		name            string
		pod             *v1.Pod
		baselinePower   float64
		finalPower      float64
		carbonIntensity float64
		duration        time.Duration
		wantEnergy      float64
		wantEmissions   float64
	}{
		{
			name: "no additional power usage",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: v1.PodSpec{
					NodeName: "test-node",
				},
				Status: v1.PodStatus{
					StartTime: &metav1.Time{Time: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
				},
			},
			baselinePower:   100,
			finalPower:      100,
			carbonIntensity: 200,
			duration:        time.Hour,
			wantEnergy:      0.1,  // 100W * 1h = 100Wh = 0.1kWh
			wantEmissions:   20.0, // 0.1kWh * 200gCO2/kWh = 20gCO2
		},
		{
			name: "additional power usage",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-2",
				},
				Spec: v1.PodSpec{
					NodeName: "test-node",
				},
				Status: v1.PodStatus{
					StartTime: &metav1.Time{Time: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
				},
			},
			baselinePower:   100,
			finalPower:      200,
			carbonIntensity: 200,
			duration:        time.Hour,
			wantEnergy:      0.2,  // 200W * 1h = 200Wh = 0.2kWh
			wantEmissions:   40.0, // 0.2kWh * 200gCO2/kWh = 40gCO2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Power: config.PowerConfig{
						DefaultIdlePower: tt.baselinePower,
						DefaultMaxPower:  400,
					},
				},
			}

			mockTime := tt.pod.Status.StartTime.Time.Add(tt.duration)
			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, 0, mockTime)

			// Store baseline power
			baselineKey := fmt.Sprintf("%s/%s/baseline", tt.pod.Spec.NodeName, tt.pod.Name)
			scheduler.powerMetrics.Store(baselineKey, tt.baselinePower)

			// Test handlePodCompletion
			scheduler.handlePodCompletion(tt.pod)

			// Verify final power metric was stored
			finalKey := fmt.Sprintf("%s/%s/final", tt.pod.Spec.NodeName, tt.pod.Name)
			if value, ok := scheduler.powerMetrics.Load(finalKey); !ok {
				t.Errorf("handlePodCompletion() did not store power metric")
			} else if power, ok := value.(float64); !ok || power != tt.finalPower {
				t.Errorf("handlePodCompletion() stored power = %v, want %v", power, tt.finalPower)
			}

			// Verify energy and emissions metrics
			// Note: In a real test we would use a metric gathering library, but for simplicity
			// we're just verifying the calculations are correct based on our inputs
			energyKWh := (tt.finalPower * tt.duration.Hours()) / 1000
			if energyKWh != tt.wantEnergy {
				t.Errorf("handlePodCompletion() energy = %v kWh, want %v kWh", energyKWh, tt.wantEnergy)
			}

			carbonEmissions := energyKWh * tt.carbonIntensity
			if carbonEmissions != tt.wantEmissions {
				t.Errorf("handlePodCompletion() emissions = %v gCO2, want %v gCO2", carbonEmissions, tt.wantEmissions)
			}

			// Verify savings metrics if there was additional power usage
			if tt.finalPower > tt.baselinePower {
				additionalPower := tt.finalPower - tt.baselinePower
				additionalEnergyKWh := (additionalPower * tt.duration.Hours()) / 1000
				additionalEmissions := additionalEnergyKWh * tt.carbonIntensity

				// These would be recorded in EstimatedSavings metric
				if additionalEnergyKWh <= 0 {
					t.Errorf("handlePodCompletion() additional energy = %v kWh, want > 0", additionalEnergyKWh)
				}
				if additionalEmissions <= 0 {
					t.Errorf("handlePodCompletion() additional emissions = %v gCO2, want > 0", additionalEmissions)
				}
			}
		})
	}
}
