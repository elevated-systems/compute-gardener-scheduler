package computegardener

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/computegardener/cache"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/carbon"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/pricing"
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
	carbonImpl  carbon.Implementation
	clock       clock.Clock

	// Metric value cache
	powerMetrics sync.Map // map[string]float64 - key format: "nodeName/podName/phase"

	// Shutdown
	stopCh chan struct{}
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

	scheduler := &ComputeGardenerScheduler{
		handle:      h,
		config:      cfg,
		apiClient:   apiClient,
		cache:       dataCache,
		pricingImpl: pricingImpl,
		carbonImpl:  carbonImpl,
		clock:       clock.RealClock{},
		stopCh:      make(chan struct{}),
	}

	// Start health check worker
	go scheduler.healthCheckWorker(ctx)

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
					klog.V(2).InfoS("Pod succeeded",
						"pod", newPod.Name,
						"namespace", newPod.Namespace)
				case newPod.Status.Phase == v1.PodFailed:
					isCompleted = true
					klog.V(2).InfoS("Pod failed",
						"pod", newPod.Name,
						"namespace", newPod.Namespace,
						"reason", newPod.Status.Reason,
						"message", newPod.Status.Message)
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
						klog.V(2).InfoS("All containers terminated",
							"pod", newPod.Name,
							"namespace", newPod.Namespace)
					}
				}

				if isCompleted {
					klog.V(2).InfoS("Handling pod completion",
						"pod", newPod.Name,
						"namespace", newPod.Namespace,
						"node", newPod.Spec.NodeName)
					scheduler.handlePodCompletion(newPod)
				}
			},
		},
	)

	// Register shutdown handler
	h.SharedInformerFactory().Core().V1().Nodes().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
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
	if cs.config.Pricing.Enabled {
		if status := cs.checkPricingConstraints(ctx, pod); !status.IsSuccess() {
			return nil, status
		}
	}

	// Check carbon intensity constraints if enabled
	if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
		if status := cs.carbonImpl.CheckIntensityConstraints(ctx, pod); !status.IsSuccess() {
			return nil, status
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
	return nil
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

func (cs *ComputeGardenerScheduler) checkPricingConstraints(ctx context.Context, pod *v1.Pod) *framework.Status {
	if cs.pricingImpl == nil {
		return framework.NewStatus(framework.Success, "")
	}

	rate := cs.pricingImpl.GetCurrentRate(cs.clock.Now())

	// Get threshold from pod annotation, env var, or use off-peak rate as threshold
	var threshold float64
	if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/price-threshold"]; ok {
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
		} else {
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	} else if len(cs.config.Pricing.Schedules) > 0 {
		// Use off-peak rate as default threshold
		threshold = cs.config.Pricing.Schedules[0].OffPeakRate
	} else {
		return framework.NewStatus(framework.Error, "no pricing schedules configured")
	}

	// Record current electricity rate
	period := "peak"
	if rate <= threshold {
		period = "off-peak"
	}
	ElectricityRateGauge.WithLabelValues("tou", period).Set(rate)

	if rate > threshold {
		PriceBasedDelays.WithLabelValues(period).Inc()
		savings := rate - threshold
		EstimatedSavings.WithLabelValues("cost", "dollars").Add(savings)

		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current electricity rate ($%.3f/kWh) exceeds threshold ($%.3f/kWh)",
				rate,
				threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
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
	// Check cache health - there should be at least one region with fresh data
	regions := cs.cache.GetRegions()
	if len(regions) == 0 {
		return fmt.Errorf("cache health check failed: no regions cached")
	}

	// Verify at least one region has fresh data
	hasFreshData := false
	for _, region := range regions {
		if _, fresh := cs.cache.Get(region); fresh {
			hasFreshData = true
			break
		}
	}
	if !hasFreshData {
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

// handlePodCompletion records metrics when a pod completes
func (cs *ComputeGardenerScheduler) handlePodCompletion(pod *v1.Pod) {
	// Get pod CPU usage from container statuses
	var totalCPUUsage float64
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			// TODO: Get pod CPU usage from container metrics... yes, pod coming into prefilter should have this, right? but we may need to do before termination... hmm
			// For now, we'll just use a placeholder value
			totalCPUUsage += 0.5 // Placeholder - need to implement actual pod CPU metrics
		}
	}

	// Calculate power consumption based on CPU usage
	nodeName := pod.Spec.NodeName
	var idlePower, maxPower float64
	if nodePower, ok := cs.config.Power.NodePowerConfig[nodeName]; ok {
		idlePower = nodePower.IdlePower
		maxPower = nodePower.MaxPower
	} else {
		idlePower = cs.config.Power.DefaultIdlePower
		maxPower = cs.config.Power.DefaultMaxPower
	}

	// Linear interpolation between idle and max power based on CPU usage
	estimatedPower := idlePower + (maxPower-idlePower)*totalCPUUsage

	// Calculate energy usage
	if pod.Status.StartTime != nil {
		duration := cs.clock.Since(pod.Status.StartTime.Time)
		energyKWh := (estimatedPower * duration.Hours()) / 1000 // Convert W*h to kWh
		JobEnergyUsage.WithLabelValues(pod.Name, pod.Namespace).Observe(energyKWh)

		// Get current carbon intensity if enabled
		if cs.config.Carbon.Enabled && cs.carbonImpl != nil {
			if intensity, err := cs.carbonImpl.GetCurrentIntensity(context.Background()); err == nil {
				// Calculate carbon emissions (gCO2eq) = energy (kWh) * intensity (gCO2eq/kWh)
				carbonEmissions := energyKWh * intensity
				JobCarbonEmissions.WithLabelValues(pod.Name, pod.Namespace).Observe(carbonEmissions)
			}
		}
	}
}
