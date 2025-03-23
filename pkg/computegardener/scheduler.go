package computegardener

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "ComputeGardenerScheduler"
	// SchedulerName is the name used in pod specs to request this scheduler
	SchedulerName = "compute-gardener-scheduler"
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
	apiClient        *api.Client
	cache            *schedulercache.Cache
	priceImpl      price.Implementation
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

	// Metrics deduplication - track pods we've already counted for delay metrics
	delayedPods map[string]bool // Map of pod UIDs to prevent double-counting of delays
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

	// Initialize components - create cache first so it can be used by API client
	dataCache := schedulercache.New(cfg.Cache.CacheTTL, cfg.Cache.MaxCacheAge)

	// Initialize API client with cache
	apiClient := api.NewClient(
		cfg.Carbon.APIConfig,
		cfg.Cache,
		api.WithCache(dataCache),
	)

	// Initialize hardware profiler if hardware profiles are configured
	var hardwareProfiler *metrics.HardwareProfiler
	if cfg.Power.HardwareProfiles != nil {
		hardwareProfiler = metrics.NewHardwareProfiler(cfg.Power.HardwareProfiles)
		klog.V(2).InfoS("Hardware profiler initialized with profiles",
			"cpuProfiles", len(cfg.Power.HardwareProfiles.CPUProfiles),
			"gpuProfiles", len(cfg.Power.HardwareProfiles.GPUProfiles),
			"memProfiles", len(cfg.Power.HardwareProfiles.MemProfiles))

		// Log detailed information about CPU profiles
		for model, profile := range cfg.Power.HardwareProfiles.CPUProfiles {
			klog.V(2).InfoS("CPU profile loaded",
				"model", model,
				"idlePower", profile.IdlePower,
				"maxPower", profile.MaxPower)
		}

		// Log default power values
		klog.V(2).InfoS("Default power values configured",
			"defaultIdlePower", cfg.Power.DefaultIdlePower,
			"defaultMaxPower", cfg.Power.DefaultMaxPower)
	}

	// Initialize pricing implementation if enabled
	priceImpl, err := price.Factory(cfg.Pricing)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pricing implementation: %v", err)
	}

	if cfg.Pricing.Enabled {
		klog.InfoS("Price-aware scheduling enabled",
			"provider", cfg.Pricing.Provider,
			"numSchedules", len(cfg.Pricing.Schedules))

		for i, schedule := range cfg.Pricing.Schedules {
			klog.InfoS("Loaded pricing schedule",
				"index", i,
				"dayOfWeek", schedule.DayOfWeek,
				"startTime", schedule.StartTime,
				"endTime", schedule.EndTime,
				"peakRate", schedule.PeakRate,
				"offPeakRate", schedule.OffPeakRate)
		}
	} else {
		klog.InfoS("Price-aware scheduling disabled")
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

		// Initialize GPU metrics client - use Prometheus if configured, otherwise use null client
		if cfg.Metrics.Prometheus != nil && cfg.Metrics.Prometheus.URL != "" {
			klog.InfoS("Initializing Prometheus GPU metrics client",
				"url", cfg.Metrics.Prometheus.URL)

			promClient, err := metrics.NewPrometheusGPUMetricsClient(cfg.Metrics.Prometheus.URL)
			if err != nil {
				klog.ErrorS(err, "Failed to initialize Prometheus GPU metrics client, falling back to null implementation")
				gpuMetricsClient = metrics.NewNullGPUMetricsClient()
			} else {
				// Configure DCGM metrics if settings are provided
				if cfg.Metrics.Prometheus.DCGMPowerMetric != "" {
					promClient.SetDCGMPowerMetric(cfg.Metrics.Prometheus.DCGMPowerMetric)
				}
				if cfg.Metrics.Prometheus.DCGMUtilMetric != "" {
					promClient.SetDCGMUtilMetric(cfg.Metrics.Prometheus.DCGMUtilMetric)
				}

				// Set useDCGM based on config (default to true if not explicitly disabled)
				useDCGM := true // Default to true
				// Only change if explicitly set to false
				if cfg.Metrics.Prometheus.UseDCGM == false {
					useDCGM = false
				}
				promClient.SetUseDCGM(useDCGM)

				klog.InfoS("Prometheus GPU metrics client configured with DCGM",
					"useDCGM", useDCGM,
					"powerMetric", promClient.GetDCGMPowerMetric(),
					"utilMetric", promClient.GetDCGMUtilMetric())

				gpuMetricsClient = promClient
			}
		} else {
			// Initialize a null GPU metrics client as fallback
			klog.V(2).InfoS("No Prometheus URL configured, using null GPU metrics client")
			gpuMetricsClient = metrics.NewNullGPUMetricsClient()
		}
	}

	scheduler := &ComputeGardenerScheduler{
		handle:           h,
		config:           cfg,
		apiClient:        apiClient,
		cache:            dataCache,
		priceImpl:      priceImpl,
		carbonImpl:       carbonImpl,
		clock:            clock.RealClock{},
		hardwareProfiler: hardwareProfiler,

		// Metrics components
		coreMetricsClient: coreMetricsClient,
		gpuMetricsClient:  gpuMetricsClient,
		metricsStore:      metricsStore,

		startTime: time.Now(),
		stopCh:    make(chan struct{}),

		// Initialize delay tracking map
		delayedPods: make(map[string]bool),
	}

	// Start health check worker
	go scheduler.healthCheckWorker(ctx)

	// Start metrics collection worker if enabled
	if scheduler.config.Metrics.SamplingInterval != "" {
		go scheduler.metricsCollectionWorker(ctx)
	} else {
		klog.V(2).InfoS("Metrics collection worker disabled - no sampling interval configured")
	}
	
	// Start carbon intensity cache refresh worker if carbon is enabled
	if scheduler.config.Carbon.Enabled && scheduler.carbonImpl != nil {
		go scheduler.carbonCacheRefreshWorker(ctx)
	}

	// Register pod informer with filtering to only track completion for pods using our scheduler
	if h.SharedInformerFactory() != nil {
		h.SharedInformerFactory().Core().V1().Pods().Informer().AddEventHandler(
			cache.FilteringResourceEventHandler{
				FilterFunc: func(obj interface{}) bool {
					pod, ok := obj.(*v1.Pod)
					if !ok {
						// Handle tombstone objects
						tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
						if !ok {
							return false
						}
						pod, ok = tombstone.Obj.(*v1.Pod)
						if !ok {
							return false
						}
					}
					return pod.Spec.SchedulerName == SchedulerName || pod.Spec.SchedulerName == Name
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
							klog.V(2).InfoS("Pod completed, handling completion",
								"pod", klog.KObj(newPod),
								"phase", newPod.Status.Phase,
								"nodeName", newPod.Spec.NodeName)
							scheduler.handlePodCompletion(newPod)
						}
					},
					DeleteFunc: func(obj interface{}) {
						// Handle pod deletion (like when scaling down deployments)
						var pod *v1.Pod
						var ok bool

						// Extract the pod from the object (which might be a tombstone)
						pod, ok = obj.(*v1.Pod)
						if !ok {
							// When a delete is observed, we sometimes get a DeletedFinalStateUnknown
							tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
							if !ok {
								klog.V(2).InfoS("Error decoding object, invalid type")
								return
							}
							pod, ok = tombstone.Obj.(*v1.Pod)
							if !ok {
								klog.V(2).InfoS("Error decoding object tombstone, invalid type")
								return
							}
						}

						// Skip if pod doesn't have a node assigned
						if pod.Spec.NodeName == "" {
							return
						}

						// Check if this pod was already processed for completion
						if scheduler.metricsStore != nil {
							if history, found := scheduler.metricsStore.GetHistory(string(pod.UID)); found && history.Completed {
								klog.V(2).InfoS("Pod deletion detected, but already processed for completion",
									"pod", klog.KObj(pod),
									"phase", pod.Status.Phase)
								return
							}
						}

						klog.V(2).InfoS("Pod deleted, handling as completion",
							"pod", klog.KObj(pod),
							"phase", pod.Status.Phase,
							"nodeName", pod.Spec.NodeName)
						scheduler.handlePodCompletion(pod)
					},
				},
			},
		)
	} else {
		klog.V(2).InfoS("Skipping pod informer setup when handler nil (ex: testing)")
	}

	// Register node handlers for initialization and cleanup
	if h.SharedInformerFactory() != nil {
		h.SharedInformerFactory().Core().V1().Nodes().Informer().AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					node, ok := obj.(*v1.Node)
					if !ok {
						return
					}
					// Initialize node with hardware profile if needed
					if scheduler.hardwareProfiler != nil {
						// Get node power profile with defaults applied
						if profile, err := scheduler.hardwareProfiler.GetNodePowerProfile(node); err == nil {
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
	} else {
		klog.V(2).InfoS("Skipping node setup when handler nil (ex: testing)")
	}
	return scheduler, nil
}

// Name returns the name of the plugin
func (cs *ComputeGardenerScheduler) Name() string {
	return Name
}

// PreFilter implements the PreFilter interface
func (cs *ComputeGardenerScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	klog.V(2).InfoS("PreFilter starting",
		"pod", klog.KObj(pod),
		"schedulerName", pod.Spec.SchedulerName,
		"hasGPU", hasGPURequest(pod))

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

	// Check pricing constraints if enabled
	if cs.config.Pricing.Enabled && cs.priceImpl != nil && !cs.isOptedOut(pod) {
		klog.V(3).InfoS("Checking price constraints in PreFilter",
			"pod", klog.KObj(pod),
			"enabled", cs.config.Pricing.Enabled)

		if status := cs.priceImpl.CheckPriceConstraints(pod, cs.clock.Now()); !status.IsSuccess() {
			klog.V(2).InfoS("Price constraints check failed in PreFilter",
				"pod", klog.KObj(pod),
				"status", status.Message())

			// Get current rate and period
			rate := cs.priceImpl.GetCurrentRate(cs.clock.Now())
			period := "peak"
			if !cs.priceImpl.IsPeakTime(cs.clock.Now()) {
				period = "off-peak"
			}
			ElectricityRateGauge.WithLabelValues("tou", period).Set(rate)

			// Record metrics for price-based delay - only count each pod once
			podUID := string(pod.UID)
			if !cs.delayedPods[podUID] {
				// Increment the delay counter only for new pods
				PriceBasedDelays.WithLabelValues(period).Inc()

				// Mark this pod as counted so we don't count it again
				cs.delayedPods[podUID] = true
				klog.V(2).InfoS("Recorded new price-based delay",
					"pod", klog.KObj(pod),
					"podUID", podUID)
			} else {
				klog.V(3).InfoS("Skipping duplicate price-based delay count",
					"pod", klog.KObj(pod),
					"podUID", podUID)
			}

			return nil, status
		}
	}

	// Check carbon intensity constraints if enabled
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil && !cs.isOptedOut(pod) {
		// Check if carbon constraint check is disabled for this pod
		if val, ok := pod.Annotations[common.AnnotationCarbonEnabled]; ok {
			if enabled, _ := strconv.ParseBool(val); !enabled {
				klog.V(2).InfoS("Carbon intensity check disabled via annotation",
					"pod", klog.KObj(pod))
			} else {
				// Process with carbon intensity check since it's explicitly enabled
				if result, status := cs.applyCarbonIntensityCheck(ctx, pod); !status.IsSuccess() {
					return result, status
				}
			}
		} else {
			// No explicit annotation, apply default carbon check
			if result, status := cs.applyCarbonIntensityCheck(ctx, pod); !status.IsSuccess() {
				return result, status
			}
		}
	}

	// Store any pod-specific information in cycle state for Filter stage
	// Currently we're keeping this lightweight - just marking that the pod passed PreFilter
	state.Write("compute-gardener-passed-prefilter", &preFilterState{passed: true})

	klog.V(2).InfoS("PreFilter completed successfully",
		"pod", klog.KObj(pod),
		"schedulerName", pod.Spec.SchedulerName)
	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (cs *ComputeGardenerScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Filter implements the Filter interface
func (cs *ComputeGardenerScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	// Log start of filter for debugging
	klog.V(3).InfoS("Filter starting",
		"pod", klog.KObj(pod),
		"node", nodeInfo.Node().Name,
		"schedulerName", pod.Spec.SchedulerName)
	startTime := cs.clock.Now()

	// Record initial carbon intensity and electricity rate for savings calculation
	// This is done in Filter because it's called for each pod-node pair during scheduling
	cs.recordInitialMetrics(ctx, pod)

	var returnStatus *framework.Status
	defer func() {
		PodSchedulingLatency.WithLabelValues("filter").Observe(cs.clock.Since(startTime).Seconds())

		// Log the filter result with timing information
		if returnStatus != nil && !returnStatus.IsSuccess() {
			klog.V(2).InfoS("Node filtered out during scheduling",
				"pod", klog.KObj(pod),
				"node", nodeInfo.Node().Name,
				"reason", returnStatus.Message(),
				"code", returnStatus.Code().String(),
				"durationMs", cs.clock.Since(startTime).Milliseconds())
		} else {
			klog.V(3).InfoS("Node passed filter evaluation",
				"pod", klog.KObj(pod),
				"node", nodeInfo.Node().Name,
				"durationMs", cs.clock.Since(startTime).Milliseconds())
		}
	}()

	// Perform a soft check for prefilter state, but continue even if missing
	v, err := state.Read("compute-gardener-passed-prefilter")
	if err != nil || v == nil {
		// Log the issue but continue instead of failing
		klog.V(2).InfoS("Missing prefilter state but continuing with filter evaluation",
			"pod", klog.KObj(pod),
			"error", err,
			"stateFound", v != nil)
	} else {
		// If state exists, check if it's valid
		s, ok := v.(*preFilterState)
		if !ok || !s.passed {
			klog.V(2).InfoS("Invalid prefilter state but continuing with filter evaluation",
				"pod", klog.KObj(pod),
				"correctType", ok,
				"passed", ok && s.passed)
		}
	}

	// Check for opt-out annotation directly since we might not have prefilter state
	if cs.isOptedOut(pod) {
		klog.V(3).InfoS("Pod opted out of scheduling constraints", "pod", klog.KObj(pod))
		return framework.NewStatus(framework.Success, "")
	}

	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	// Check hardware profile energy efficiency if available
	if cs.hardwareProfiler != nil {
		klog.V(3).InfoS("Checking hardware profile for node", "node", node.Name)

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
					klog.V(3).InfoS("Checking max power requirement", "pod", klog.KObj(pod), "maxPower", maxPower)

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
								klog.V(3).InfoS("Adjusting GPU power for workload type",
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
							fmt.Sprintf("node power profile (%.1f W) exceeds pod max power requirement (%.1f W)",
								effectiveMaxPower, maxPower))
					} else {
						klog.V(3).InfoS("Node power within pod requirement",
							"node", node.Name,
							"nodePower", effectiveMaxPower,
							"podMaxPower", maxPower)
					}
				}
			}

			// Check power efficiency ratio if specified
			if val, ok := pod.Annotations[common.AnnotationMinEfficiency]; ok {
				minEfficiency, err := strconv.ParseFloat(val, 64)
				if err == nil {
					klog.V(3).InfoS("Checking minimum efficiency requirement",
						"pod", klog.KObj(pod),
						"minEfficiency", minEfficiency)

					if nodeEfficiency < minEfficiency {
						klog.V(2).InfoS("Filtered node due to efficiency requirements",
							"node", node.Name,
							"nodeEfficiency", nodeEfficiency,
							"minEfficiency", minEfficiency)

						// Record the filtering in metrics
						PowerFilteredNodes.WithLabelValues("efficiency").Inc()

						return framework.NewStatus(framework.Unschedulable,
							"node efficiency below pod's minimum requirement")
					} else {
						klog.V(3).InfoS("Node efficiency meets pod requirement",
							"node", node.Name,
							"nodeEfficiency", nodeEfficiency,
							"minEfficiency", minEfficiency)
					}
				}
			}
		} else {
			klog.V(3).InfoS("Failed to get hardware profile",
				"pod", klog.KObj(pod),
				"node", node.Name,
				"error", err)
		}
	} else {
		klog.V(3).InfoS("Hardware profiler not enabled", "pod", klog.KObj(pod))
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

// applyCarbonIntensityCheck performs carbon intensity threshold checks and returns appropriate status
func (cs *ComputeGardenerScheduler) applyCarbonIntensityCheck(ctx context.Context, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	// Get pod-specific threshold if it exists, otherwise use the global threshold
	threshold := cs.config.Carbon.IntensityThreshold
	if threshStr, ok := pod.Annotations[common.AnnotationCarbonIntensityThreshold]; ok {
		if threshVal, err := strconv.ParseFloat(threshStr, 64); err == nil {
			threshold = threshVal
			klog.V(2).InfoS("Using pod-specific carbon intensity threshold",
				"pod", klog.KObj(pod),
				"threshold", threshold)
		} else {
			klog.ErrorS(err, "Invalid carbon intensity threshold annotation, using global threshold",
				"pod", klog.KObj(pod),
				"annotation", threshStr,
				"globalThreshold", threshold)
		}
	}

	// Check carbon intensity
	currentIntensity, err := cs.carbonImpl.GetCurrentIntensity(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to get carbon intensity in PreFilter, allowing pod",
			"pod", klog.KObj(pod))
		return nil, framework.NewStatus(framework.Success, "")
	} 
	
	// Update metrics regardless of threshold check result
	CarbonIntensityGauge.WithLabelValues(cs.config.Carbon.APIConfig.Region).Set(currentIntensity)
	
	if currentIntensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)",
			currentIntensity, threshold)
		klog.V(2).InfoS("Carbon intensity check failed in PreFilter",
			"pod", klog.KObj(pod),
			"currentIntensity", currentIntensity,
			"threshold", threshold,
			"usingPodSpecificThreshold", threshold != cs.config.Carbon.IntensityThreshold)

		// Only count carbon delay once per pod
		podUID := string(pod.UID)
		if !cs.delayedPods[podUID] {
			CarbonBasedDelays.WithLabelValues(cs.config.Carbon.APIConfig.Region).Inc()
			cs.delayedPods[podUID] = true
			klog.V(2).InfoS("Recorded new carbon-based delay",
				"pod", klog.KObj(pod),
				"podUID", podUID)
		} else {
			klog.V(3).InfoS("Skipping duplicate carbon-based delay count",
				"pod", klog.KObj(pod),
				"podUID", podUID)
		}
		return nil, framework.NewStatus(framework.Unschedulable, msg)
	}
	
	return nil, framework.NewStatus(framework.Success, "")
}

// recordInitialMetrics records the initial carbon intensity and electricity rate
// at the time of scheduling to enable savings calculations later.
// This function will add the annotations only if they don't already exist.
func (cs *ComputeGardenerScheduler) recordInitialMetrics(ctx context.Context, pod *v1.Pod) {
	// Don't modify the pod if it's opted out of compute gardener scheduling
	if cs.isOptedOut(pod) {
		return
	}

	// Skip if the pod already has both annotations - check them together to reduce log spam
	if _, hasCarbon := pod.Annotations[common.AnnotationInitialCarbonIntensity]; hasCarbon {
		if _, hasElectricity := pod.Annotations[common.AnnotationInitialElectricityRate]; hasElectricity {
			// Pod already has both annotations, no need to update
			return
		}
	}

	// Create a client to update the pod
	clientset := cs.handle.ClientSet()

	// Make a copy of the pod to modify
	podCopy := pod.DeepCopy()
	if podCopy.Annotations == nil {
		podCopy.Annotations = make(map[string]string)
	}

	// Flag to track if we need to update the pod
	needsUpdate := false

	// Record current carbon intensity if enabled and not already present
	if _, hasCarbon := podCopy.Annotations[common.AnnotationInitialCarbonIntensity]; !hasCarbon &&
		cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		currentIntensity, err := cs.carbonImpl.GetCurrentIntensity(ctx)
		if err == nil && currentIntensity > 0 {
			podCopy.Annotations[common.AnnotationInitialCarbonIntensity] = strconv.FormatFloat(currentIntensity, 'f', 2, 64)
			needsUpdate = true
		}
	}

	// Record current electricity rate if enabled and not already present
	if _, hasRate := podCopy.Annotations[common.AnnotationInitialElectricityRate]; !hasRate &&
		cs.config.Pricing.Enabled && cs.priceImpl != nil {
		currentRate := cs.priceImpl.GetCurrentRate(time.Now())
		if currentRate > 0 {
			podCopy.Annotations[common.AnnotationInitialElectricityRate] = strconv.FormatFloat(currentRate, 'f', 6, 64)
			needsUpdate = true
		}
	}

	// Only update the pod if we need to add at least one annotation
	if needsUpdate {
		// Attempt to update the pod - if it fails, it will be tried again next cycle
		_, err := clientset.CoreV1().Pods(pod.Namespace).Update(ctx, podCopy, metav1.UpdateOptions{})
		if err == nil {
			klog.V(2).InfoS("Updated pod with initial metrics annotations",
				"pod", klog.KObj(pod),
				"carbonIntensity", podCopy.Annotations[common.AnnotationInitialCarbonIntensity],
				"electricityRate", podCopy.Annotations[common.AnnotationInitialElectricityRate])
		} else {
			klog.V(3).InfoS("Failed to update pod with metrics annotations, will try again next cycle",
				"pod", klog.KObj(pod),
				"error", err)
		}
	}
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
	if cs.config.Pricing.Enabled && cs.priceImpl != nil {
		rate := cs.priceImpl.GetCurrentRate(cs.clock.Now())
		if rate < 0 {
			return fmt.Errorf("pricing health check failed: invalid rate returned")
		}
	}

	return nil
}

// hasGPURequest checks if a pod is requesting GPU resources
func hasGPURequest(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if gpu, ok := container.Resources.Requests["nvidia.com/gpu"]; ok && !gpu.IsZero() {
			return true
		}
	}
	return false
}

// carbonCacheRefreshWorker refreshes the carbon intensity cache at regular intervals
// to ensure that the cache never becomes stale when scheduling decisions are made
func (cs *ComputeGardenerScheduler) carbonCacheRefreshWorker(ctx context.Context) {
	// Calculate refresh interval as 1/3 of the cache TTL to ensure we always have fresh data
	refreshInterval := cs.config.Cache.CacheTTL / 3
	
	// Ensure the interval is not too short or too long
	if refreshInterval < 10*time.Second {
		refreshInterval = 10 * time.Second // Minimum 10 seconds
	} else if refreshInterval > 5*time.Minute {
		refreshInterval = 5 * time.Minute // Maximum 5 minutes
	}
	
	klog.InfoS("Starting carbon intensity cache refresh worker", 
		"refreshInterval", refreshInterval.String(),
		"cacheTTL", cs.config.Cache.CacheTTL.String(),
		"region", cs.config.Carbon.APIConfig.Region)
	
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	
	// Get all regions that need to be refreshed
	regions := []string{cs.config.Carbon.APIConfig.Region}
	
	// Initial refresh on startup
	cs.refreshCarbonCache(ctx, regions)
	
	for {
		select {
		case <-cs.stopCh:
			klog.V(2).InfoS("Stopping carbon intensity cache refresh worker")
			return
		case <-ticker.C:
			cs.refreshCarbonCache(ctx, regions)
		}
	}
}

// refreshCarbonCache refreshes the carbon intensity data for given regions
func (cs *ComputeGardenerScheduler) refreshCarbonCache(ctx context.Context, regions []string) {
	for _, region := range regions {
		// Create a context with timeout for the API request
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		
		// Force refresh by calling GetCurrentIntensity which will update the cache
		intensity, err := cs.carbonImpl.GetCurrentIntensity(requestCtx)
		
		// Always cancel the context after use to prevent leaks
		cancel()
		
		if err != nil {
			klog.ErrorS(err, "Failed to refresh carbon intensity cache", 
				"region", region)
		} else {
			klog.V(2).InfoS("Successfully refreshed carbon intensity cache", 
				"region", region, 
				"intensity", intensity)
			
			// Update metrics
			CarbonIntensityGauge.WithLabelValues(region).Set(intensity)
		}
	}
}
