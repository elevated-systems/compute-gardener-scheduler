package computegardener

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/computegardener/cache"
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
	apiClient     *api.Client
	cache         *schedulercache.Cache
	pricingImpl   pricing.Implementation
	clock         clock.Clock
	metricsClient metricsv1beta1.MetricsV1beta1Interface

	// Metric value cache
	powerMetrics sync.Map // map[string]float64 - key format: "nodeName/podName/phase"

	// Shutdown
	stopCh chan struct{}
}

var (
	_ framework.PreFilterPlugin = &ComputeGardenerScheduler{}
	_ framework.PostBindPlugin  = &ComputeGardenerScheduler{}
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
	apiClient := api.NewClient(cfg.API)
	dataCache := schedulercache.New(cfg.API.CacheTTL, cfg.API.MaxCacheAge)

	// Initialize pricing implementation if enabled
	pricingImpl, err := pricing.Factory(cfg.Pricing)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pricing implementation: %v", err)
	}

	// Initialize metrics client
	metricsClient, err := metricsv1beta1.NewForConfig(h.KubeConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics client: %v", err)
	}

	scheduler := &ComputeGardenerScheduler{
		handle:        h,
		config:        cfg,
		apiClient:     apiClient,
		cache:         dataCache,
		pricingImpl:   pricingImpl,
		clock:         clock.RealClock{},
		metricsClient: metricsClient,
		stopCh:        make(chan struct{}),
	}

	// Start health check worker
	go scheduler.healthCheckWorker(ctx)

	// Register pod informer to track completion
	h.SharedInformerFactory().Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldPod := oldObj.(*v1.Pod)
				newPod := newObj.(*v1.Pod)

				// Enhanced logging for debugging
				klog.V(2).InfoS("Pod update received",
					"pod", newPod.Name,
					"namespace", newPod.Namespace,
					"oldPhase", oldPod.Status.Phase,
					"newPhase", newPod.Status.Phase)

				// Log detailed container states
				for i, status := range newPod.Status.ContainerStatuses {
					klog.V(2).InfoS("Container status",
						"pod", newPod.Name,
						"container", status.Name,
						"index", i,
						"ready", status.Ready,
						"started", status.Started,
						"restartCount", status.RestartCount,
						"state", describeContainerState(status.State))
				}

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

	// Start metrics server (insecure) on a separate mux
	go func() {
		metricsPort := fmt.Sprint(":", scheduler.config.Observability.MetricsPort)
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", legacyregistry.Handler())

		metricsServer := &http.Server{
			Addr:    metricsPort,
			Handler: metricsMux,
		}

		klog.InfoS("Starting metrics server", "addr", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil {
			klog.ErrorS(err, "Failed to start metrics server")
		}
	}()

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

	// Check carbon intensity constraints
	if status := cs.checkCarbonIntensityConstraints(ctx, pod); !status.IsSuccess() {
		return nil, status
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

func (cs *ComputeGardenerScheduler) checkCarbonIntensityConstraints(ctx context.Context, pod *v1.Pod) *framework.Status {
	// Get carbon intensity data
	data, err := cs.getCarbonIntensityData(ctx)
	if err != nil {
		SchedulingAttempts.WithLabelValues("error").Inc()
		return framework.NewStatus(framework.Error, fmt.Sprintf("failed to get carbon intensity data: %v", err))
	}

	// Record carbon intensity metric
	CarbonIntensityGauge.WithLabelValues(cs.config.API.ElectricityMapRegion).Set(data.CarbonIntensity)

	// Get threshold from pod annotation or use configured threshold
	threshold := cs.config.Carbon.IntensityThreshold
	if val, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
		} else {
			return framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation")
		}
	}

	if data.CarbonIntensity > threshold {
		SchedulingAttempts.WithLabelValues("intensity_exceeded").Inc()
		// Record scheduling efficiency metrics
		if initialIntensity, ok := pod.Annotations["compute-gardener-scheduler.kubernetes.io/initial-carbon-intensity"]; ok {
			if initial, err := strconv.ParseFloat(initialIntensity, 64); err == nil {
				delta := data.CarbonIntensity - initial
				SchedulingEfficiencyMetrics.WithLabelValues("carbon_intensity_delta", pod.Name).Set(delta)

				// Estimate savings based on delta
				if delta < 0 { // negative delta means improvement
					EstimatedSavings.WithLabelValues("carbon", "grams_co2").Add(-delta)
				}
			}
		} else {
			// First time seeing this pod, initialize annotations if needed
			if pod.Annotations == nil {
				pod.Annotations = make(map[string]string)
			}
			pod.Annotations["compute-gardener-scheduler.kubernetes.io/initial-carbon-intensity"] = fmt.Sprintf("%.2f", data.CarbonIntensity)
		}

		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", data.CarbonIntensity, threshold)

		// Track node CPU usage if pod was previously running
		if pod.Spec.NodeName != "" {
			nodeName := pod.Spec.NodeName
			// Record pre-job metrics
			NodeCPUUsage.WithLabelValues(nodeName, pod.Name, "pre_job").Set(cs.getNodeCPUUsage(nodeName))
			power := cs.estimateNodePower(nodeName)
			NodePowerEstimate.WithLabelValues(nodeName, pod.Name, "pre_job").Set(power)
		}

		return framework.NewStatus(framework.Unschedulable, msg)
	}

	return framework.NewStatus(framework.Success, "")
}

func (cs *ComputeGardenerScheduler) getCarbonIntensityData(ctx context.Context) (*api.ElectricityData, error) {
	// Check cache first
	if data, found := cs.cache.Get(cs.config.API.ElectricityMapRegion); found {
		return data, nil
	}

	// Fetch from API
	data, err := cs.apiClient.GetCarbonIntensity(ctx, cs.config.API.ElectricityMapRegion)
	if err != nil {
		return nil, err
	}

	// Update cache
	cs.cache.Set(cs.config.API.ElectricityMapRegion, data)
	return data, nil
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
	_, err := cs.getCarbonIntensityData(ctx)
	return err
}

// PostBind implements the PostBind interface
func (cs *ComputeGardenerScheduler) PostBind(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	// Record baseline CPU/power when pod is bound but hasn't started
	baselineCPU := cs.getNodeCPUUsage(nodeName)
	baselinePower := cs.estimateNodePower(nodeName)

	// Store in cache and set metric
	key := fmt.Sprintf("%s/%s/baseline", nodeName, pod.Name)
	cs.powerMetrics.Store(key, baselinePower)

	NodeCPUUsage.WithLabelValues(nodeName, pod.Name, "baseline").Set(baselineCPU)
	NodePowerEstimate.WithLabelValues(nodeName, pod.Name, "baseline").Set(baselinePower)
}

// handlePodCompletion records metrics when a pod completes
func (cs *ComputeGardenerScheduler) handlePodCompletion(pod *v1.Pod) {
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return
	}

	// Record final CPU/power at completion (better represents average utilization)
	finalCPU := cs.getNodeCPUUsage(nodeName)
	finalPower := cs.estimateNodePower(nodeName)

	// Store in cache and set metric
	key := fmt.Sprintf("%s/%s/final", nodeName, pod.Name)
	cs.powerMetrics.Store(key, finalPower)

	NodeCPUUsage.WithLabelValues(nodeName, pod.Name, "final").Set(finalCPU)
	NodePowerEstimate.WithLabelValues(nodeName, pod.Name, "final").Set(finalPower)

	// Calculate energy usage and carbon emissions based on baseline and final measurements
	if baselinePower, ok := cs.getPowerMetric(nodeName, pod.Name, "baseline"); ok {
		duration := cs.clock.Since(pod.Status.StartTime.Time)
		// Use final power as better representation of average
		energyKWh := (finalPower * duration.Hours()) / 1000 // Convert W*h to kWh

		JobEnergyUsage.WithLabelValues(pod.Name, pod.Namespace).Observe(energyKWh)

		// Get current carbon intensity
		data, err := cs.getCarbonIntensityData(context.Background())
		if err == nil {
			// Calculate carbon emissions (gCO2eq) = energy (kWh) * intensity (gCO2eq/kWh)
			carbonEmissions := energyKWh * data.CarbonIntensity
			JobCarbonEmissions.WithLabelValues(pod.Name, pod.Namespace).Observe(carbonEmissions)
		}

		// Calculate additional energy from job (above baseline)
		additionalPower := finalPower - baselinePower
		if additionalPower > 0 {
			additionalEnergyKWh := (additionalPower * duration.Hours()) / 1000
			EstimatedSavings.WithLabelValues("energy", "kwh").Add(additionalEnergyKWh)

			// Calculate additional carbon emissions if we have intensity data
			if err == nil {
				additionalEmissions := additionalEnergyKWh * data.CarbonIntensity
				EstimatedSavings.WithLabelValues("carbon", "grams_co2").Add(additionalEmissions)
			}
		}
	}
}

// getPowerMetric retrieves a previously recorded power metric from cache
func (cs *ComputeGardenerScheduler) getPowerMetric(nodeName, podName, phase string) (float64, bool) {
	key := fmt.Sprintf("%s/%s/%s", nodeName, podName, phase)
	if value, ok := cs.powerMetrics.Load(key); ok {
		return value.(float64), true
	}
	return 0, false
}

// getNodeCPUUsage returns the current CPU usage (0-1) for a node
func (cs *ComputeGardenerScheduler) getNodeCPUUsage(nodeName string) float64 {
	// Get node metrics from metrics server
	metrics, err := cs.metricsClient.NodeMetricses().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get node metrics", "node", nodeName, "error_type", "metrics_api")
		SchedulingAttempts.WithLabelValues("metrics_api_error").Inc()
		return 0
	}

	// Validate metrics data
	if metrics.Usage.Cpu().IsZero() {
		klog.V(2).InfoS("Node CPU metrics reported as zero - possible metrics-server issue",
			"node", nodeName)
		SchedulingAttempts.WithLabelValues("zero_metrics").Inc()
		return 0
	}

	node, err := cs.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get node", "node", nodeName)
		return 0
	}

	// Calculate CPU usage percentage
	cpuQuantity := metrics.Usage.Cpu()
	capacityQuantity := node.Status.Capacity.Cpu()

	cpuUsage := float64(cpuQuantity.MilliValue()) / float64(capacityQuantity.MilliValue())
	return cpuUsage
}

// describeContainerState returns a string description of a container state
func describeContainerState(state v1.ContainerState) string {
	switch {
	case state.Running != nil:
		return fmt.Sprintf("running since %v", state.Running.StartedAt)
	case state.Waiting != nil:
		return fmt.Sprintf("waiting: %s - %s", state.Waiting.Reason, state.Waiting.Message)
	case state.Terminated != nil:
		return fmt.Sprintf("terminated: %s (exit code: %d) - %s",
			state.Terminated.Reason,
			state.Terminated.ExitCode,
			state.Terminated.Message)
	default:
		return "unknown"
	}
}

// estimateNodePower estimates power consumption based on CPU usage
func (cs *ComputeGardenerScheduler) estimateNodePower(nodeName string) float64 {
	cpuUsage := cs.getNodeCPUUsage(nodeName)

	// Get node-specific power config if available, otherwise use defaults
	var idlePower, maxPower float64
	if nodePower, ok := cs.config.Power.NodePowerConfig[nodeName]; ok {
		idlePower = nodePower.IdlePower
		maxPower = nodePower.MaxPower
	} else {
		idlePower = cs.config.Power.DefaultIdlePower
		maxPower = cs.config.Power.DefaultMaxPower
	}

	// Linear interpolation between idle and max power based on CPU usage
	estimatedPower := idlePower + (maxPower-idlePower)*cpuUsage
	return estimatedPower
}
