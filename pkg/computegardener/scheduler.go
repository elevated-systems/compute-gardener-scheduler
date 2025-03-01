package computegardener

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	metricsv1beta1client "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	schedulercache "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/cache"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/clock"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/pricing"
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "ComputeGardenerScheduler"
)

// ComputeGardenerScheduler is a scheduler plugin that implements carbon and price-aware scheduling
type ComputeGardenerScheduler struct {
	handle framework.Handle
	config *config.Config

	// Components
	apiClient   *api.Client
	cache       *schedulercache.Cache
	pricingImpl pricing.Implementation
	carbonImpl       carbon.Implementation
	clock            clock.Clock
	hardwareProfiler *metrics.HardwareProfiler

	// Metrics components
	coreMetricsClient metrics.CoreMetricsClient
	gpuMetricsClient  metrics.GPUMetricsClient
	metricsStore      metrics.PodMetricsStorage

	// Scheduler state
	startTime time.Time
	stopCh    chan struct{}
}

var (
	_ framework.PreFilterPlugin = &ComputeGardenerScheduler{}
	_ framework.Plugin          = &ComputeGardenerScheduler{}
)

// New initializes a new plugin and returns it
func New(ctx context.Context, obj runtime.Object, h framework.Handle) (framework.Plugin, error) {
	// Load and validate configuration
	cfg, err := config.Load(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	// Initialize components
	apiClient := api.NewClient(
		cfg.Carbon.APIConfig,
		cfg.Cache,
	)
	dataCache := schedulercache.New(cfg.Cache.CacheTTL, cfg.Cache.MaxCacheAge)
	
	// Initialize hardware profiler if hardware profiles are configured
	var hardwareProfiler *metrics.HardwareProfiler
	if cfg.Power.HardwareProfiles != nil {
		hardwareProfiler = metrics.NewHardwareProfiler(cfg.Power.HardwareProfiles)
		klog.V(2).InfoS("Hardware profiler initialized with profiles", 
			"cpuProfiles", len(cfg.Power.HardwareProfiles.CPUProfiles),
			"gpuProfiles", len(cfg.Power.HardwareProfiles.GPUProfiles),
			"memProfiles", len(cfg.Power.HardwareProfiles.MemProfiles))
	}

	// Initialize pricing implementation if enabled
	pricingImpl, err := pricing.Factory(cfg.Pricing)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pricing implementation: %v", err)
	}

	// Initialize carbon implementation if enabled
	var carbonImpl carbon.Implementation
	if cfg.Carbon.Enabled {
		carbonImpl = carbon.New(&cfg.Carbon, apiClient)
	}

	// Setup metrics clients - if metrics server is not available, they'll be nil
	var coreMetricsClient metrics.CoreMetricsClient
	var gpuMetricsClient metrics.GPUMetricsClient
	var metricsStore metrics.PodMetricsStorage

	// Setup downsampling strategy based on config
	var downsamplingStrategy metrics.DownsamplingStrategy
	switch cfg.Metrics.DownsamplingStrategy {
	case "lttb":
		downsamplingStrategy = &metrics.LTTBDownsampling{}
	case "timeBased":
		downsamplingStrategy = &metrics.SimpleTimeBasedDownsampling{}
	case "minMax":
		downsamplingStrategy = &metrics.MinMaxDownsampling{}
	default:
		// Default to time-based if not specified
		downsamplingStrategy = &metrics.SimpleTimeBasedDownsampling{}
	}

	// Initialize metrics store with the configured retention parameters
	if cfg.Metrics.SamplingInterval != "" {
		var retentionTime time.Duration
		if cfg.Metrics.PodRetention != "" {
			if retention, err := time.ParseDuration(cfg.Metrics.PodRetention); err == nil {
				retentionTime = retention
			} else {
				klog.ErrorS(err, "Invalid completed pod retention time, using default of 1h")
				retentionTime = 1 * time.Hour
			}
		} else {
			retentionTime = 1 * time.Hour
		}

		metricsStore = metrics.NewInMemoryStore(
			5*time.Minute, // Cleanup period
			retentionTime,
			cfg.Metrics.MaxSamplesPerPod,
			downsamplingStrategy,
		)

		// Try to initialize metrics client (will be nil if metrics-server not available)
		// We'll log at startup but continue even if it's not available
		if client, err := createMetricsClient(h); err == nil {
			coreMetricsClient = metrics.NewK8sMetricsClient(client)
		} else {
			klog.ErrorS(err, "Failed to initialize metrics-server client, energy metrics will be limited")
		}

		// Initialize a null GPU metrics client (stub for future implementation)
		gpuMetricsClient = metrics.NewNullGPUMetricsClient()
	}

	scheduler := &ComputeGardenerScheduler{
		handle:           h,
		config:           cfg,
		apiClient:        apiClient,
		cache:            dataCache,
		pricingImpl:      pricingImpl,
		carbonImpl:       carbonImpl,
		clock:            clock.RealClock{},
		hardwareProfiler: hardwareProfiler,

		// Metrics components
		coreMetricsClient: coreMetricsClient,
		gpuMetricsClient:  gpuMetricsClient,
		metricsStore:      metricsStore,

		startTime: time.Now(),
		stopCh:    make(chan struct{}),
	}

	// Start health check worker
	go scheduler.healthCheckWorker(ctx)

	// Start metrics collection worker if enabled
	if scheduler.config.Metrics.SamplingInterval != "" {
		go scheduler.metricsCollectionWorker(ctx)
	} else {
		klog.V(2).InfoS("Metrics collection worker disabled - no sampling interval configured")
	}

	// Register pod informer to track completion
	h.SharedInformerFactory().Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				newPod := newObj.(*v1.Pod)

				// Check for completion or failure
				isCompleted := false
				switch {
				case newPod.Status.Phase == v1.PodSucceeded:
					isCompleted = true
				case newPod.Status.Phase == v1.PodFailed:
					isCompleted = true
				case len(newPod.Status.ContainerStatuses) > 0:
					allTerminated := true
					for _, status := range newPod.Status.ContainerStatuses {
						if status.State.Terminated == nil {
							allTerminated = false
							break
						}
					}
					if allTerminated {
						isCompleted = true
					}
				}

				if isCompleted {
					scheduler.handlePodCompletion(newPod)
				}
			},
		},
	)

	// Register node handlers for initialization and cleanup
	h.SharedInformerFactory().Core().V1().Nodes().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				node, ok := obj.(*v1.Node)
				if !ok {
					return
				}
				// Initialize node with hardware profile if needed
				if scheduler.hardwareProfiler != nil {
					// Automatically detect hardware and update cache
					if profile, err := scheduler.hardwareProfiler.DetectNodePowerProfile(node); err == nil {
						klog.V(2).InfoS("Automatically detected node hardware profile", 
							"node", node.Name, 
							"idlePower", profile.IdlePower, 
							"maxPower", profile.MaxPower)
					}
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				// Check if changes occurred that might affect hardware profile
				oldNode, ok1 := oldObj.(*v1.Node)
				newNode, ok2 := newObj.(*v1.Node)
				if !ok1 || !ok2 {
					return
				}
				
				// Refresh hardware profile if node specs changed
				if scheduler.hardwareProfiler != nil && nodeSpecsChanged(oldNode, newNode) {
					klog.V(2).InfoS("Node specs changed, refreshing hardware profile", "node", newNode.Name)
					scheduler.hardwareProfiler.RefreshNodeCache(newNode)
				}
			},
			DeleteFunc: func(obj interface{}) {
				// Handle possible node deletion
				if node, ok := obj.(*v1.Node); ok && scheduler.hardwareProfiler != nil {
					// Could clean up any node-specific cached data here if needed
					klog.V(2).InfoS("Node deleted", "node", node.Name)
				}
				
				klog.V(2).InfoS("Handling shutdown", "plugin", scheduler.Name())
				scheduler.Close()
			},
		},
	)

	return scheduler, nil
}

// Name returns the name of the plugin
func (cs *ComputeGardenerScheduler) Name() string {
	return Name
}

// PreFilter implements the PreFilter interface
func (cs *ComputeGardenerScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	startTime := cs.clock.Now()
	defer func() {
		PodSchedulingLatency.WithLabelValues("total").Observe(cs.clock.Since(startTime).Seconds())
	}()

	// Check if pod has been waiting too long
	if cs.hasExceededMaxDelay(pod) {
		SchedulingAttempts.WithLabelValues("max_delay_exceeded").Inc()
		return nil, framework.NewStatus(framework.Success, "maximum scheduling delay exceeded")
	}

	// Check if pod has annotation to opt-out
	if cs.isOptedOut(pod) {
		SchedulingAttempts.WithLabelValues("skipped").Inc()
		return nil, framework.NewStatus(framework.Success, "")
	}

	// Check pricing constraints if enabled
	if cs.config.Pricing.Enabled && cs.pricingImpl != nil {
		if status := cs.pricingImpl.CheckPriceConstraints(pod, cs.clock.Now()); !status.IsSuccess() {
			// Record metrics for price-based delay
			rate := cs.pricingImpl.GetCurrentRate(cs.clock.Now())
			threshold := cs.config.Pricing.Schedules[0].OffPeakRate // Default threshold
			if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/price-threshold"]; ok {
				if t, err := strconv.ParseFloat(val, 64); err == nil {
					threshold = t
				}
			}

			period := "peak"
			if rate <= threshold {
				period = "off-peak"
			}
			PriceBasedDelays.WithLabelValues(period).Inc()
			savings := rate - threshold
			EstimatedSavings.WithLabelValues("cost", "dollars").Add(savings)
			ElectricityRateGauge.WithLabelValues("tou", period).Set(rate)

			return nil, status
		}
	}

	// Check carbon intensity constraints if enabled
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		// Check if pod has annotation to disable carbon-aware scheduling
		if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/carbon-enabled"]; ok && val == "false" {
			// Skip carbon check if explicitly disabled for this pod
		} else {
			// Get threshold from pod annotation or use configured threshold
			threshold := cs.config.Carbon.IntensityThreshold

			if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
				if t, err := strconv.ParseFloat(val, 64); err == nil {
					threshold = t
				} else {
					klog.ErrorS(err, "Invalid carbon intensity threshold annotation",
						"pod", pod.Name,
						"namespace", pod.Namespace,
						"value", val)
					return nil, framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation")
				}
			}

			if status := cs.carbonImpl.CheckIntensityConstraints(ctx, threshold); !status.IsSuccess() {
				// Record metrics for carbon-based delay
				if intensity, err := cs.carbonImpl.GetCurrentIntensity(ctx); err == nil {
					CarbonIntensityGauge.WithLabelValues(cs.config.Carbon.APIConfig.Region).Set(intensity)
					if intensity > threshold {
						CarbonBasedDelays.WithLabelValues(cs.config.Carbon.APIConfig.Region).Inc()
						savings := intensity - threshold
						EstimatedSavings.WithLabelValues("carbon", "gCO2eq").Add(savings)
					}
				}
				return nil, status
			}
		}
	}

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (cs *ComputeGardenerScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Close cleans up resources
func (cs *ComputeGardenerScheduler) Close() error {
	close(cs.stopCh)
	cs.apiClient.Close()
	cs.cache.Close()

	// Close metrics store if configured
	if cs.metricsStore != nil {
		cs.metricsStore.Close()
	}

	return nil
}

// nodeSpecsChanged determines if any node specs changed that might affect hardware profile
func nodeSpecsChanged(oldNode, newNode *v1.Node) bool {
	// Check for instance type changes
	if oldNode.Labels["node.kubernetes.io/instance-type"] != newNode.Labels["node.kubernetes.io/instance-type"] {
		return true
	}
	
	// Check for architecture changes
	if oldNode.Labels["kubernetes.io/arch"] != newNode.Labels["kubernetes.io/arch"] {
		return true
	}
	
	// Check for capacity changes
	oldCPU := oldNode.Status.Capacity.Cpu().Value()
	newCPU := newNode.Status.Capacity.Cpu().Value()
	if oldCPU != newCPU {
		return true
	}
	
	oldMem := oldNode.Status.Capacity.Memory().Value()
	newMem := newNode.Status.Capacity.Memory().Value()
	if oldMem != newMem {
		return true
	}
	
	// Check for GPU changes
	oldGPU, oldHasGPU := oldNode.Status.Capacity["nvidia.com/gpu"]
	newGPU, newHasGPU := newNode.Status.Capacity["nvidia.com/gpu"]
	
	if oldHasGPU != newHasGPU {
		return true
	}
	
	if oldHasGPU && newHasGPU && oldGPU.Value() != newGPU.Value() {
		return true
	}
	
	// CPU/GPU model labels changed
	if oldNode.Labels["node.kubernetes.io/cpu-model"] != newNode.Labels["node.kubernetes.io/cpu-model"] {
		return true
	}
	
	if oldNode.Labels["node.kubernetes.io/gpu-model"] != newNode.Labels["node.kubernetes.io/gpu-model"] {
		return true
	}
	
	return false
}

// createMetricsClient creates a client for the Kubernetes metrics API
func createMetricsClient(handle framework.Handle) (metricsv1beta1client.PodMetricsInterface, error) {
	config := *handle.KubeConfig()

	// Create a metrics client using the scheduler's kubeconfig
	metricsClient, err := metricsv1beta1client.NewForConfig(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics client: %v", err)
	}

	// Return the pod metrics interface
	return metricsClient.PodMetricses(""), nil
}

func (cs *ComputeGardenerScheduler) hasExceededMaxDelay(pod *v1.Pod) bool {
	if creationTime := pod.CreationTimestamp; !creationTime.IsZero() {
		// Check for pod-level max delay annotation
		maxDelay := cs.config.Scheduling.MaxSchedulingDelay
		if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/max-scheduling-delay"]; ok {
			if d, err := time.ParseDuration(val); err == nil {
				maxDelay = d
			} else {
				klog.ErrorS(err, "Invalid max scheduling delay annotation",
					"pod", pod.Name,
					"namespace", pod.Namespace,
					"value", val)
			}
		}
		return cs.clock.Since(creationTime.Time) > maxDelay
	}
	return false
}

func (cs *ComputeGardenerScheduler) isOptedOut(pod *v1.Pod) bool {
	return pod.Annotations["compute-gardener-scheduler.kubernetes.io/skip"] == "true"
}

func (cs *ComputeGardenerScheduler) healthCheckWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			if err := cs.healthCheck(ctx); err != nil {
				klog.ErrorS(err, "Health check failed")
			}
		}
	}
}

func (cs *ComputeGardenerScheduler) healthCheck(ctx context.Context) error {
	// Check cache health
	regions := cs.cache.GetRegions()

	// Evaluate cache state
	emptyCache := len(regions) == 0
	hasFreshData := false

	if !emptyCache {
		// Check if any region has fresh data
		for _, region := range regions {
			if _, fresh := cs.cache.Get(region); fresh {
				hasFreshData = true
				break
			}
		}
	}

	// Only enforce cache health checks if carbon is enabled and cache should be initialized
	// An empty cache is allowed and normal during initial startup or when carbon features aren't being used
	if cs.config.Carbon.Enabled && !emptyCache && !hasFreshData {
		// If we have regions but no fresh data, that's a problem
		return fmt.Errorf("cache health check failed: no fresh data available")
	}

	// If carbon awareness enabled, verify API health
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		_, err := cs.carbonImpl.GetCurrentIntensity(ctx)
		if err != nil {
			return fmt.Errorf("carbon API health check failed: %v", err)
		}
	}

	// If pricing enabled, verify we can get current rate
	if cs.config.Pricing.Enabled && cs.pricingImpl != nil {
		rate := cs.pricingImpl.GetCurrentRate(cs.clock.Now())
		if rate < 0 {
			return fmt.Errorf("pricing health check failed: invalid rate returned")
		}
	}

	return nil
}
