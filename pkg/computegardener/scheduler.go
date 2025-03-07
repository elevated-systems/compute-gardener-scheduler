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
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/metrics"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/pricing"
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "ComputeGardenerScheduler"
)

// preFilterState is used to record that a pod passed the PreFilter phase
type preFilterState struct {
	passed bool
}

// Clone implements the StateData interface
func (s *preFilterState) Clone() framework.StateData {
	return &preFilterState{
		passed: s.passed,
	}
}

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
	_ framework.FilterPlugin    = &ComputeGardenerScheduler{}
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

	// Register pod informer with filtering to only track completion for pods using our scheduler
	h.SharedInformerFactory().Core().V1().Pods().Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				pod, ok := obj.(*v1.Pod)
				if !ok {
					return false
				}
				return pod.Spec.SchedulerName == Name
			},
			Handler: cache.ResourceEventHandlerFuncs{
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
						cpuModel, gpuModel := scheduler.hardwareProfiler.GetNodeHardwareInfo(node)
						memGB := float64(node.Status.Capacity.Memory().Value()) / (1024 * 1024 * 1024)
						klog.V(2).InfoS("Automatically detected node hardware profile", 
							"node", node.Name, 
							"idlePower", profile.IdlePower, 
							"maxPower", profile.MaxPower,
							"cpuModel", cpuModel,
							"gpuModel", gpuModel != "" && gpuModel != "none",
							"pue", profile.PUE,
							"memGB", int(memGB))
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
				if scheduler.hardwareProfiler != nil && metrics.NodeSpecsChanged(oldNode, newNode) {
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
		PodSchedulingLatency.WithLabelValues("prefilter").Observe(cs.clock.Since(startTime).Seconds())
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
	
	// Apply namespace-level energy budget if pod doesn't have one
	if err := cs.applyNamespaceEnergyBudget(ctx, pod); err != nil {
		klog.ErrorS(err, "Failed to apply namespace energy budget", "pod", klog.KObj(pod))
	}

	// Store any pod-specific information in cycle state for Filter stage
	// Currently we're keeping this lightweight - just marking that the pod passed PreFilter
	state.Write("compute-gardener-passed-prefilter", &preFilterState{passed: true})

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (cs *ComputeGardenerScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Filter implements the Filter interface
func (cs *ComputeGardenerScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	// Log start of filter for debugging
	klog.V(2).InfoS("Filter starting", 
		"pod", klog.KObj(pod), 
		"node", nodeInfo.Node().Name, 
		"schedulerName", pod.Spec.SchedulerName)
	startTime := cs.clock.Now()
	defer func() {
		PodSchedulingLatency.WithLabelValues("filter").Observe(cs.clock.Since(startTime).Seconds())
	}()

	// Verify the pod passed PreFilter (should always be true, but check anyway)
	v, err := state.Read("compute-gardener-passed-prefilter")
	if err != nil || v == nil {
		klog.V(2).InfoS("Pod did not pass prefilter stage", "pod", klog.KObj(pod))
		return framework.NewStatus(framework.Error, "Pod did not pass prefilter stage")
	}
	s, ok := v.(*preFilterState)
	if !ok || !s.passed {
		klog.V(2).InfoS("Invalid or failed prefilter state", "pod", klog.KObj(pod))
		return framework.NewStatus(framework.Error, "Invalid prefilter state")
	}

	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	// Check pricing constraints if enabled
	if cs.config.Pricing.Enabled && cs.pricingImpl != nil {
		if status := cs.pricingImpl.CheckPriceConstraints(pod, cs.clock.Now()); !status.IsSuccess() {
			// Record metrics for price-based delay
			rate := cs.pricingImpl.GetCurrentRate(cs.clock.Now())
			threshold := cs.config.Pricing.Schedules[0].OffPeakRate // Default threshold
			if val, ok := pod.Annotations[common.AnnotationPriceThreshold]; ok {
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

			return status
		}
	}

	// Check carbon intensity constraints if enabled
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		klog.V(2).InfoS("Checking carbon intensity constraints", 
			"pod", klog.KObj(pod), 
			"region", cs.config.Carbon.APIConfig.Region,
			"enabled", cs.config.Carbon.Enabled,
			"schedulerName", pod.Spec.SchedulerName,
			"useOurScheduler", pod.Spec.SchedulerName == Name)
			
		// Check if pod is using our scheduler
		if pod.Spec.SchedulerName != Name {
			klog.V(2).InfoS("Skipping carbon check for pod using different scheduler",
				"pod", klog.KObj(pod),
				"schedulerName", pod.Spec.SchedulerName,
				"ourSchedulerName", Name)
		} else if val, ok := pod.Annotations[common.AnnotationCarbonEnabled]; ok && val == "false" {
			// Skip carbon check if explicitly disabled for this pod
			klog.V(2).InfoS("Carbon check disabled by pod annotation", "pod", klog.KObj(pod))
		} else {
			// Get threshold from pod annotation or use configured threshold
			threshold := cs.config.Carbon.IntensityThreshold

			if val, ok := pod.Annotations[common.AnnotationCarbonIntensityThreshold]; ok {
				if t, err := strconv.ParseFloat(val, 64); err == nil {
					threshold = t
					klog.V(2).InfoS("Using custom carbon threshold from annotation", 
						"pod", klog.KObj(pod), 
						"threshold", threshold)
				} else {
					klog.ErrorS(err, "Invalid carbon intensity threshold annotation",
						"pod", pod.Name,
						"namespace", pod.Namespace,
						"value", val)
					return framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation")
				}
			}

			// Get current intensity for debugging
			if intensity, err := cs.carbonImpl.GetCurrentIntensity(ctx); err == nil {
				klog.V(2).InfoS("Current carbon intensity", 
					"region", cs.config.Carbon.APIConfig.Region,
					"intensity", intensity, 
					"threshold", threshold,
					"exceeds", intensity > threshold)
				
				// Record the intensity in metrics regardless of decision
				CarbonIntensityGauge.WithLabelValues(cs.config.Carbon.APIConfig.Region).Set(intensity)
			} else {
				klog.ErrorS(err, "Failed to get carbon intensity", 
					"region", cs.config.Carbon.APIConfig.Region)
			}

			// First get the current intensity directly to log it
			intensity, err := cs.carbonImpl.GetCurrentIntensity(ctx)
			if err == nil {
				klog.V(2).InfoS("Carbon intensity check decision", 
					"pod", klog.KObj(pod),
					"intensity", intensity, 
					"threshold", threshold,
					"exceeded", intensity > threshold)
				
				// Direct comparison for better clarity in logs
				if intensity > threshold {
					klog.V(2).InfoS("BLOCKING POD: Carbon intensity exceeds threshold", 
						"pod", klog.KObj(pod),
						"intensity", intensity, 
						"threshold", threshold)
					CarbonBasedDelays.WithLabelValues(cs.config.Carbon.APIConfig.Region).Inc()
					savings := intensity - threshold
					EstimatedSavings.WithLabelValues("carbon", "gCO2eq").Add(savings)
					
					// Return unschedulable status directly
					msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
					return framework.NewStatus(framework.Unschedulable, msg)
				} else {
					klog.V(2).InfoS("ALLOWING POD: Carbon intensity within threshold", 
						"pod", klog.KObj(pod),
						"intensity", intensity, 
						"threshold", threshold)
				}
			} else {
				// Fall back to implementation check if direct check failed
				klog.ErrorS(err, "Direct intensity check failed, falling back to implementation", 
					"pod", klog.KObj(pod))
				
				if status := cs.carbonImpl.CheckIntensityConstraints(ctx, threshold); !status.IsSuccess() {
					return status
				}
			}
			}
		}
	} else {
		klog.V(2).InfoS("Carbon awareness check skipped", "pod", klog.KObj(pod),
			"carbonEnabled", cs.config.Carbon.Enabled, 
			"carbonImplNil", cs.carbonImpl == nil)
	}

	// Check hardware profile energy efficiency if available
	if cs.hardwareProfiler != nil {
		// Get node's power profile
		powerProfile, err := cs.hardwareProfiler.GetNodePowerProfile(node)
		if err == nil && powerProfile != nil {
			// Record PUE metric for the node
			NodePUE.WithLabelValues(node.Name).Set(powerProfile.PUE)
			
			// Calculate and record node efficiency
			nodeEfficiency := metrics.CalculateNodeEfficiency(node, powerProfile)
			NodeEfficiency.WithLabelValues(node.Name).Set(nodeEfficiency)
			
			// If pod has energy efficiency annotations, check if this node meets requirements
			if val, ok := pod.Annotations[common.AnnotationMaxPowerWatts]; ok {
				maxPower, err := strconv.ParseFloat(val, 64)
				if err == nil {
					// Check if pod has a GPU workload type specified
					workloadType := ""
					if wt, ok := pod.Annotations[common.AnnotationGPUWorkloadType]; ok {
						workloadType = wt
					}
					
					// Calculate effective power with PUE, considering workload type
					effectiveMaxPower := cs.hardwareProfiler.GetEffectivePower(powerProfile, false)
					
					// Apply workload type coefficient if specified
					if workloadType != "" && powerProfile.GPUWorkloadTypes != nil {
						if coefficient, ok := powerProfile.GPUWorkloadTypes[workloadType]; ok && coefficient > 0 {
							// Adjust GPU power based on workload type coefficient
							if powerProfile.MaxGPUPower > 0 {
								// Calculate adjusted max power by applying coefficient to GPU power only
								adjustedGPUPower := powerProfile.MaxGPUPower * coefficient
								klog.V(2).InfoS("Adjusting GPU power for workload type", 
									"node", node.Name,
									"workloadType", workloadType,
									"originalGPUPower", powerProfile.MaxGPUPower,
									"adjustedGPUPower", adjustedGPUPower,
									"coefficient", coefficient)
								
								// Recalculate effective power with workload-adjusted GPU power
								effectiveMaxPower = (powerProfile.MaxPower * powerProfile.PUE) + 
									(adjustedGPUPower * powerProfile.GPUPUE)
							}
						}
					}
					
					if effectiveMaxPower > maxPower {
						// Node's max power exceeds pod's requirement
						klog.V(2).InfoS("Filtered node due to power requirements", 
							"node", node.Name, 
							"nodePower", effectiveMaxPower, 
							"podMaxPower", maxPower,
							"basePower", powerProfile.MaxPower,
							"pue", powerProfile.PUE,
							"workloadType", workloadType)
						
						// Record the filtering in metrics
						PowerFilteredNodes.WithLabelValues("max_power").Inc()
						
						return framework.NewStatus(framework.Unschedulable, 
							fmt.Sprintf("node power profile (%v W) exceeds pod max power requirement (%v W)", 
								effectiveMaxPower, maxPower))
					}
				}
			}

			// Check power efficiency ratio if specified
			if val, ok := pod.Annotations[common.AnnotationMinEfficiency]; ok {
				minEfficiency, err := strconv.ParseFloat(val, 64)
				if err == nil {
					if nodeEfficiency < minEfficiency {
						klog.V(2).InfoS("Filtered node due to efficiency requirements", 
							"node", node.Name, 
							"nodeEfficiency", nodeEfficiency, 
							"minEfficiency", minEfficiency)
						
						// Record the filtering in metrics
						PowerFilteredNodes.WithLabelValues("efficiency").Inc()
						
						return framework.NewStatus(framework.Unschedulable, 
							"node efficiency below pod's minimum requirement")
					}
				}
			}
		} else {
			if !cs.config.Carbon.Enabled {
				klog.V(2).InfoS("Carbon-aware scheduling disabled in config", "pod", klog.KObj(pod))
			}
			if cs.carbonImpl == nil {
				klog.V(2).InfoS("Carbon implementation is nil", "pod", klog.KObj(pod))
			}
		}
	}

	return framework.NewStatus(framework.Success, "")
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

// applyNamespaceEnergyBudget checks for namespace level energy budget policies and applies them to the pod if needed
func (cs *ComputeGardenerScheduler) applyNamespaceEnergyBudget(ctx context.Context, pod *v1.Pod) error {
	// TODO: Implement namespace energy budget policy application
	// This is a placeholder implementation
	return nil
}

func (cs *ComputeGardenerScheduler) hasExceededMaxDelay(pod *v1.Pod) bool {
	if creationTime := pod.CreationTimestamp; !creationTime.IsZero() {
		// Check for pod-level max delay annotation
		maxDelay := cs.config.Scheduling.MaxSchedulingDelay
		if val, ok := pod.Annotations[common.AnnotationMaxSchedulingDelay]; ok {
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
	return pod.Annotations[common.AnnotationSkip] == "true"
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