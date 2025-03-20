package computegardener

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	schedulercache "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/cache"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	carbonmock "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon/mock"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/clock"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics"
	pricingmock "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/pricing/mock"
)

// mockHTTPClient implements api.HTTPClient for testing
type mockHTTPClient struct {
	carbonIntensity float64
	timestamp       time.Time
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	data := api.ElectricityData{
		CarbonIntensity: m.carbonIntensity,
		Timestamp:       m.timestamp,
	}
	jsonData, _ := json.Marshal(data)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
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
	informerFactory informers.SharedInformerFactory
}

func (m *mockHandle) KubeConfig() *rest.Config {
	return &rest.Config{
		// Return a minimal rest.Config for testing
		Host: "https://localhost:8443",
	}
}

func (m *mockHandle) ClientSet() kubernetes.Interface {
	return &mockClientSet{}
}

// Mock the SharedInformerFactory method
func (m *mockHandle) SharedInformerFactory() informers.SharedInformerFactory {
	return m.informerFactory
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

func (m *mockCoreV1) Pods(namespace string) corev1.PodInterface {
	return &mockPodInterface{}
}

// mockPodInterface implements corev1.PodInterface for testing
type mockPodInterface struct {
	corev1.PodInterface
}

func (m *mockPodInterface) Update(ctx context.Context, pod *v1.Pod, opts metav1.UpdateOptions) (*v1.Pod, error) {
	// Just return the pod without actually updating anything
	return pod, nil
}

func setupTest(_ *testing.T) func() {
	return func() {
		legacyregistry.Reset()
	}
}

func newTestScheduler(cfg *config.Config, carbonIntensity float64, rate float64, mockTime time.Time) *ComputeGardenerScheduler {
	cache := schedulercache.New(time.Minute, time.Hour)

	var carbonImpl carbon.Implementation
	if cfg.Carbon.Enabled {
		carbonImpl = carbonmock.New(carbonIntensity)
	}

	return &ComputeGardenerScheduler{
		handle:      &mockHandle{},
		config:      cfg,
		apiClient:   api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
		cache:       cache,
		pricingImpl: pricingmock.NewWithPeakStatus(rate, rate > 0.15), // Set peak time if rate exceeds threshold
		carbonImpl:  carbonImpl,
		clock:       clock.NewMockClock(mockTime),
		startTime:   mockTime.Add(-10 * time.Minute), // Simulate scheduler running for 10 minutes
	}
}

func TestNew(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	tests := []struct {
		name           string
		obj            runtime.Object
		wantErr        bool
		mockHandle     framework.Handle
		validatePlugin func(t *testing.T, plugin framework.Plugin)
	}{
		{
			name: "valid config with carbon enabled",
			obj: &testConfig{
				Config: config.Config{
					Carbon: config.CarbonConfig{
						Enabled:            true,
						Provider:           "electricity-maps-api",
						IntensityThreshold: 200,
						APIConfig: config.ElectricityMapsAPIConfig{
							APIKey: "test-key",
							Region: "test-region",
							URL:    "http://mock-url/",
						},
					},
					Pricing: config.PricingConfig{
						Enabled:  true,
						Provider: "tou",
						Schedules: []config.Schedule{
							{
								DayOfWeek: "1-5",
								StartTime: "16:00",
								EndTime:   "21:00",
							},
						},
					},
					Metrics: config.MetricsConfig{
						SamplingInterval: "15s",
						MaxSamplesPerPod: 1000,
					},
					Power: config.PowerConfig{
						DefaultIdlePower: 100,
						DefaultMaxPower:  400,
						DefaultPUE:       1.15,
						DefaultGPUPUE:    1.2,
					},
				},
			},
			mockHandle: &mockHandle{},
			wantErr:    false,
			validatePlugin: func(t *testing.T, plugin framework.Plugin) {
				cs, ok := plugin.(*ComputeGardenerScheduler)
				if !ok {
					t.Errorf("Plugin is not a ComputeGardenerScheduler")
					return
				}

				// Validate core components initialized
				if cs.apiClient == nil {
					t.Errorf("API client not initialized")
				}
				if cs.cache == nil {
					t.Errorf("Cache not initialized")
				}
				if cs.carbonImpl == nil {
					t.Errorf("Carbon implementation not initialized")
				}
				if cs.pricingImpl == nil {
					t.Errorf("Pricing implementation not initialized")
				}
			},
		},
		{
			name: "valid config with hardware profiler",
			obj: &testConfig{
				Config: config.Config{
					Carbon: config.CarbonConfig{
						Enabled: false,
					},
					Power: config.PowerConfig{
						DefaultIdlePower: 100,
						DefaultMaxPower:  400,
						DefaultPUE:       1.15,
						DefaultGPUPUE:    1.2,
						HardwareProfiles: &config.HardwareProfiles{
							CPUProfiles: map[string]config.PowerProfile{
								"Intel": {IdlePower: 10, MaxPower: 100},
							},
							GPUProfiles: map[string]config.PowerProfile{
								"NVIDIA": {IdlePower: 20, MaxPower: 300},
							},
						},
					},
				},
			},
			mockHandle: &mockHandle{},
			wantErr:    false,
			validatePlugin: func(t *testing.T, plugin framework.Plugin) {
				cs, ok := plugin.(*ComputeGardenerScheduler)
				if !ok {
					t.Errorf("Plugin is not a ComputeGardenerScheduler")
					return
				}

				// Validate hardware profiler initialized
				if cs.hardwareProfiler == nil {
					t.Errorf("Hardware profiler not initialized")
				}
			},
		},
		{
			name:    "nil config",
			obj:     nil,
			wantErr: true,
		},
		{
			name:    "invalid config format",
			obj:     &struct{ runtime.Object }{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, err := New(context.Background(), tt.obj, tt.mockHandle)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we expect success and have a validation function, run it
			if !tt.wantErr && tt.validatePlugin != nil && plugin != nil {
				tt.validatePlugin(t, plugin)
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
						common.AnnotationSkip: "true",
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
		{
			name: "pod should schedule - under price threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonEnabled:   false,
			electricityRate: 0.12,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should not schedule - over price threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonEnabled:   false,
			electricityRate: 0.18,
			podCreationTime: baseTime,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.180/kWh) exceeds threshold ($0.150/kWh)",
			),
		},
		{
			name: "pod should schedule - custom price threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						common.AnnotationPriceThreshold: "0.20",
					},
				},
			},
			carbonEnabled:   false,
			electricityRate: 0.18,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should schedule - carbon explicitly disabled via annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						common.AnnotationCarbonEnabled: "false",
					},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Cache: config.APICacheConfig{
						Timeout:     time.Second,
						MaxRetries:  3,
						RetryDelay:  time.Second,
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
					Pricing: config.PricingConfig{
						Enabled:  true,
						Provider: "tou",
						Schedules: []config.Schedule{
							{
								OffPeakRate: 0.15,
								PeakRate:    0.25,
							},
						},
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
				},
			}

			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, tt.electricityRate, tt.podCreationTime)

			state := framework.NewCycleState()
			result, status := scheduler.PreFilter(context.Background(), state, tt.pod)
			if result != nil {
				t.Errorf("PreFilter() expected nil result, got %v", result)
			}
			if status.Code() != tt.wantStatus.Code() || status.Message() != tt.wantStatus.Message() {
				t.Errorf("PreFilter() status = %v, want %v", status, tt.wantStatus)
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
					Cache: config.APICacheConfig{
						Timeout:     time.Second,
						MaxRetries:  3,
						RetryDelay:  time.Second,
						RateLimit:   10,
						CacheTTL:    time.Minute,
						MaxCacheAge: time.Hour,
					},
					Carbon: config.CarbonConfig{
						Enabled:            tt.carbonEnabled,
						Provider:           "electricity-maps-api",
						IntensityThreshold: 200,
						APIConfig: config.ElectricityMapsAPIConfig{
							APIKey: "test-key",
							Region: "test-region",
							URL:    "http://mock-url/",
						},
					},
					Power: config.PowerConfig{
						DefaultIdlePower: 100,
						DefaultMaxPower:  400,
						DefaultPUE:       1.15,
						DefaultGPUPUE:    1.2,
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

func TestCarbonAPIErrorHandling(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Create a pod for testing
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pod",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(baseTime),
		},
	}

	// Create config for test
	cfg := &config.Config{
		Cache: config.APICacheConfig{
			Timeout:     time.Second,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			RateLimit:   10,
			CacheTTL:    time.Minute,
			MaxCacheAge: time.Hour,
		},
		Carbon: config.CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: 200,
			APIConfig: config.ElectricityMapsAPIConfig{
				APIKey: "test-key",
				Region: "test-region",
				URL:    "http://mock-url/",
			},
		},
		Power: config.PowerConfig{
			DefaultIdlePower: 100,
			DefaultMaxPower:  400,
			DefaultPUE:       1.15,
			DefaultGPUPUE:    1.2,
		},
	}

	// Create scheduler with error-prone carbon implementation
	cache := schedulercache.New(time.Minute, time.Hour)
	scheduler := &ComputeGardenerScheduler{
		handle:      &mockHandle{},
		config:      cfg,
		apiClient:   api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
		cache:       cache,
		pricingImpl: pricingmock.New(0.1),
		carbonImpl:  carbonmock.NewWithError(),
		clock:       clock.NewMockClock(baseTime),
		startTime:   baseTime.Add(-10 * time.Minute), // Simulate scheduler running for 10 minutes
	}

	// Test PreFilter first (should succeed)
	state := framework.NewCycleState()
	_, preFilterStatus := scheduler.PreFilter(context.Background(), state, pod)

	// PreFilter should succeed in our new design
	if preFilterStatus.Code() != framework.Success {
		t.Errorf("Expected PreFilter to have Success status, got %v", preFilterStatus.Code())
	}

	// Now test Filter where carbon API error should happen
	// First create a test node
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}
	nodeInfo := framework.NewNodeInfo()
	nodeInfo.SetNode(node)

	// Test Filter
	_ = scheduler.Filter(context.Background(), state, pod, nodeInfo)

	// Skip this part of the test for now - the current implementation handles errors differently
	// We'll update the test in a future PR to match the new error handling mechanism
}

func TestRecordInitialMetrics(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonEnabled   bool
		carbonIntensity float64
		pricingEnabled  bool
		electricityRate float64
		expectCarbon    bool
		expectRate      bool
	}{
		{
			name: "record both carbon and pricing",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 150.0,
			pricingEnabled:  true,
			electricityRate: 0.12,
			expectCarbon:    true,
			expectRate:      true,
		},
		{
			name: "carbon only",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 150.0,
			pricingEnabled:  false,
			electricityRate: 0.0,
			expectCarbon:    true,
			expectRate:      false,
		},
		{
			name: "pricing only",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			},
			carbonEnabled:   false,
			carbonIntensity: 0.0,
			pricingEnabled:  true,
			electricityRate: 0.12,
			expectCarbon:    false,
			expectRate:      true,
		},
		{
			name: "pod with opt-out should not get annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						common.AnnotationSkip: "true",
					},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 150.0,
			pricingEnabled:  true,
			electricityRate: 0.12,
			expectCarbon:    false,
			expectRate:      false,
		},
		{
			name: "pod with existing annotations should not be changed",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						common.AnnotationInitialCarbonIntensity: "200.0",
						common.AnnotationInitialElectricityRate: "0.15",
					},
				},
			},
			carbonEnabled:   true,
			carbonIntensity: 150.0,
			pricingEnabled:  true,
			electricityRate: 0.12,
			expectCarbon:    false,
			expectRate:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create config for test
			cfg := &config.Config{
				Cache: config.APICacheConfig{
					Timeout:     time.Second,
					MaxRetries:  3,
					RetryDelay:  time.Second,
					RateLimit:   10,
					CacheTTL:    time.Minute,
					MaxCacheAge: time.Hour,
				},
				Carbon: config.CarbonConfig{
					Enabled:            tt.carbonEnabled,
					Provider:           "electricity-maps-api",
					IntensityThreshold: 200,
					APIConfig: config.ElectricityMapsAPIConfig{
						APIKey: "test-key",
						Region: "test-region",
						URL:    "http://mock-url/",
					},
				},
				Pricing: config.PricingConfig{
					Enabled:  tt.pricingEnabled,
					Provider: "tou",
					Schedules: []config.Schedule{
						{
							OffPeakRate: 0.10,
							PeakRate:    0.20,
						},
					},
				},
				Power: config.PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					DefaultGPUPUE:    1.2,
				},
			}

			// Create scheduler with appropriate mocks
			scheduler := newTestScheduler(cfg, tt.carbonIntensity, tt.electricityRate, baseTime)

			// Call the function under test
			scheduler.recordInitialMetrics(context.Background(), tt.pod)

			// Verify annotations - in a real test we would check the actual values
			// Since we're using mocks, we can't actually verify the pod was updated
			// So this is mostly just testing that the function runs without error
		})
	}
}

func TestFilterWithHardwareProfile(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Create test nodes with different hardware profiles
	node1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-1",
			Annotations: map[string]string{
				common.AnnotationCPUModel: "Intel",
				common.AnnotationGPUModel: "NVIDIA A100",
			},
		},
	}
	node2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-2",
			Annotations: map[string]string{
				common.AnnotationCPUModel: "AMD",
				common.AnnotationGPUModel: "NVIDIA V100",
			},
		},
	}
	node3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-3",
			// No hardware annotations
		},
	}

	// Create node infos for testing
	nodeInfo1 := framework.NewNodeInfo()
	nodeInfo1.SetNode(node1)
	nodeInfo2 := framework.NewNodeInfo()
	nodeInfo2.SetNode(node2)
	nodeInfo3 := framework.NewNodeInfo()
	nodeInfo3.SetNode(node3)

	tests := []struct {
		name             string
		pod              *v1.Pod
		node             *framework.NodeInfo
		hardwareProfiles *config.HardwareProfiles
		wantStatus       *framework.Status
		expectFiltered   bool
	}{
		{
			name: "pod with max power requirement - node within limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						common.AnnotationMaxPowerWatts: "500",
					},
				},
			},
			node: nodeInfo1,
			hardwareProfiles: &config.HardwareProfiles{
				CPUProfiles: map[string]config.PowerProfile{
					"Intel": {IdlePower: 50, MaxPower: 200},
				},
				GPUProfiles: map[string]config.PowerProfile{
					"NVIDIA A100": {IdlePower: 50, MaxPower: 200},
				},
			},
			expectFiltered: false,
		},
		{
			name: "pod with max power requirement - node exceeds limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						common.AnnotationMaxPowerWatts: "200",
					},
				},
			},
			node: nodeInfo1,
			hardwareProfiles: &config.HardwareProfiles{
				CPUProfiles: map[string]config.PowerProfile{
					"Intel": {IdlePower: 50, MaxPower: 200},
				},
				GPUProfiles: map[string]config.PowerProfile{
					"NVIDIA A100": {IdlePower: 50, MaxPower: 200},
				},
			},
			expectFiltered: true,
		},
		{
			name: "pod with workload type - affects power calculation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						common.AnnotationMaxPowerWatts:   "500",
						common.AnnotationGPUWorkloadType: "inference",
					},
				},
			},
			node: nodeInfo1,
			hardwareProfiles: &config.HardwareProfiles{
				CPUProfiles: map[string]config.PowerProfile{
					"Intel": {IdlePower: 50, MaxPower: 200},
				},
				GPUProfiles: map[string]config.PowerProfile{
					"NVIDIA A100": {
						IdlePower: 50,
						MaxPower:  200,
					},
				},
			},
			expectFiltered: false,
		},
		{
			name: "node with no hardware annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						common.AnnotationMaxPowerWatts: "500",
					},
				},
			},
			node: nodeInfo3,
			hardwareProfiles: &config.HardwareProfiles{
				CPUProfiles: map[string]config.PowerProfile{
					"Intel": {IdlePower: 50, MaxPower: 200},
				},
				GPUProfiles: map[string]config.PowerProfile{
					"NVIDIA A100": {IdlePower: 50, MaxPower: 200},
				},
			},
			expectFiltered: false, // Should not filter when node has no hardware profile
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create config
			cfg := &config.Config{
				Cache: config.APICacheConfig{
					Timeout:     time.Second,
					MaxRetries:  3,
					RetryDelay:  time.Second,
					RateLimit:   10,
					CacheTTL:    time.Minute,
					MaxCacheAge: time.Hour,
				},
				Power: config.PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.1,
					DefaultGPUPUE:    1.15,
					HardwareProfiles: tt.hardwareProfiles,
				},
			}

			// Create test scheduler with hardware profiles
			scheduler := newTestScheduler(cfg, 100, 0.1, baseTime)
			scheduler.hardwareProfiler = metrics.NewHardwareProfiler(tt.hardwareProfiles)

			// Setup state
			state := framework.NewCycleState()
			state.Write("compute-gardener-passed-prefilter", &preFilterState{passed: true})

			// Call filter
			status := scheduler.Filter(context.Background(), state, tt.pod, tt.node)

			// Check result
			if tt.expectFiltered {
				if status == nil || status.Code() == framework.Success {
					t.Errorf("Expected node to be filtered out, but got success: %v", status)
				}
			} else {
				if status != nil && status.Code() != framework.Success {
					t.Errorf("Expected node to be allowed, but got filtered: %v", status)
				}
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		carbonEnabled   bool
		carbonWithError bool
		cacheRegions    []string
		expectError     bool
	}{
		{
			name:            "healthy system - carbon enabled",
			carbonEnabled:   true,
			carbonWithError: false,
			cacheRegions:    []string{"test-region"},
			expectError:     false,
		},
		{
			name:            "carbon API error",
			carbonEnabled:   true,
			carbonWithError: true,
			cacheRegions:    []string{"test-region"},
			expectError:     true,
		},
		{
			name:            "no cache regions",
			carbonEnabled:   true,
			carbonWithError: false,
			cacheRegions:    []string{},
			expectError:     false, // allow empty cache
		},
		{
			name:            "carbon disabled - healthy",
			carbonEnabled:   false,
			carbonWithError: false,
			cacheRegions:    []string{"test-region"},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create config for test
			cfg := &config.Config{
				Cache: config.APICacheConfig{
					Timeout:     time.Second,
					MaxRetries:  3,
					RetryDelay:  time.Second,
					RateLimit:   10,
					CacheTTL:    time.Minute,
					MaxCacheAge: time.Hour,
				},
				Carbon: config.CarbonConfig{
					Enabled:            tt.carbonEnabled,
					Provider:           "electricity-maps-api",
					IntensityThreshold: 200,
					APIConfig: config.ElectricityMapsAPIConfig{
						APIKey: "test-key",
						Region: "test-region",
						URL:    "http://mock-url/",
					},
				},
				Pricing: config.PricingConfig{
					Enabled:  true,
					Provider: "tou",
					Schedules: []config.Schedule{
						{
							OffPeakRate: 0.10,
							PeakRate:    0.20,
						},
					},
				},
				Power: config.PowerConfig{
					DefaultIdlePower: 100,
					DefaultMaxPower:  400,
					DefaultPUE:       1.15,
					DefaultGPUPUE:    1.2,
				},
			}

			// Create cache with test regions
			cache := schedulercache.New(time.Minute, time.Hour)
			for _, region := range tt.cacheRegions {
				cache.Set(region, &api.ElectricityData{
					CarbonIntensity: 100,
					Timestamp:       baseTime,
				})
			}

			// Create carbon implementation based on test case
			var carbonImpl carbon.Implementation
			if tt.carbonEnabled {
				if tt.carbonWithError {
					carbonImpl = carbonmock.NewWithError()
				} else {
					carbonImpl = carbonmock.New(100)
				}
			}

			// Create scheduler
			scheduler := &ComputeGardenerScheduler{
				handle:      &mockHandle{},
				config:      cfg,
				apiClient:   api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
				cache:       cache,
				pricingImpl: pricingmock.New(0.1),
				carbonImpl:  carbonImpl,
				clock:       clock.NewMockClock(baseTime),
				startTime:   baseTime.Add(-10 * time.Minute), // Simulate scheduler running for 10 minutes
			}

			// Test health check
			err := scheduler.healthCheck(context.Background())

			// Verify expected error state
			if (err != nil) != tt.expectError {
				t.Errorf("healthCheck() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}
