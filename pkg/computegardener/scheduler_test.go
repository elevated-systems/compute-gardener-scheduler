package computegardener

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
	carbonmock "sigs.k8s.io/scheduler-plugins/pkg/computegardener/carbon/mock"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
	pricingmock "sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing/mock"
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
	cache := schedulercache.New(time.Minute, time.Hour)

	var carbonImpl carbon.Implementation
	if cfg.Carbon.Enabled {
		carbonImpl = carbonmock.New(carbonIntensity)
	}

	return &ComputeGardenerScheduler{
		handle:       &mockHandle{},
		config:       cfg,
		apiClient:    api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
		cache:        cache,
		pricingImpl:  pricingmock.New(rate),
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
						"compute-gardener-scheduler.kubernetes.io/price-threshold": "0.20",
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
						"compute-gardener-scheduler.kubernetes.io/carbon-enabled": "false",
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
								PeakRate: 0.25,
							},
						},
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
	}

	// Create scheduler with error-prone carbon implementation
	cache := schedulercache.New(time.Minute, time.Hour)
	scheduler := &ComputeGardenerScheduler{
		handle:       &mockHandle{},
		config:       cfg,
		apiClient:    api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
		cache:        cache,
		pricingImpl:  pricingmock.New(0.1),
		carbonImpl:   carbonmock.NewWithError(),
		clock:        clock.NewMockClock(baseTime),
		powerMetrics: sync.Map{},
	}

	// Test PreFilter
	_, status := scheduler.PreFilter(context.Background(), nil, pod)

	// Verify that we got an error status
	if status.Code() != framework.Error {
		t.Errorf("Expected Error status, got %v", status.Code())
	}

	// Verify that the error message contains the expected text
	expectedErrText := "carbon API error"
	if status.Message() == "" || !strings.Contains(status.Message(), expectedErrText) {
		t.Errorf("Expected error message containing '%s', got '%s'", expectedErrText, status.Message())
	}
}

func TestHealthCheck(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	
	tests := []struct {
		name           string
		carbonEnabled  bool
		carbonWithError bool
		cacheRegions   []string
		expectError    bool
	}{
		{
			name:          "healthy system - carbon enabled",
			carbonEnabled: true,
			carbonWithError: false,
			cacheRegions:  []string{"test-region"},
			expectError:   false,
		},
		{
			name:          "carbon API error",
			carbonEnabled: true,
			carbonWithError: true,
			cacheRegions:  []string{"test-region"},
			expectError:   true,
		},
		{
			name:          "no cache regions",
			carbonEnabled: true,
			carbonWithError: false,
			cacheRegions:  []string{},
			expectError:   true,
		},
		{
			name:          "carbon disabled - healthy",
			carbonEnabled: false,
			carbonWithError: false,
			cacheRegions:  []string{"test-region"},
			expectError:   false,
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
							PeakRate: 0.20,
						},
					},
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
				handle:       &mockHandle{},
				config:       cfg,
				apiClient:    api.NewClient(cfg.Carbon.APIConfig, cfg.Cache),
				cache:        cache,
				pricingImpl:  pricingmock.New(0.1),
				carbonImpl:   carbonImpl,
				clock:        clock.NewMockClock(baseTime),
				powerMetrics: sync.Map{},
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
