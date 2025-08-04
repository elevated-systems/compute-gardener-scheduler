package carbon

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/forecast"
)

// Implementation defines the interface for carbon-aware scheduling
type Implementation interface {
	// GetCurrentIntensity returns the current carbon intensity for the configured region
	GetCurrentIntensity(ctx context.Context) (float64, error)

	// CheckIntensityConstraints checks if current carbon intensity exceeds threshold
	CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status
	
	// GetIntelligentSchedulingDecision uses forecasting for intelligent scheduling decisions
	GetIntelligentSchedulingDecision(ctx context.Context, threshold float64, maxDelay time.Duration) (*forecast.SchedulingDecision, error)
}

type carbonImpl struct {
	config         *config.CarbonConfig
	apiClient      *api.Client
	forecastEngine *forecast.Engine
}

// New creates a new carbon implementation
func New(cfg *config.CarbonConfig, apiClient *api.Client) Implementation {
	impl := &carbonImpl{
		config:    cfg,
		apiClient: apiClient,
	}
	
	// Initialize forecast engine if forecasting is enabled
	if cfg.Forecast.Enabled {
		// Convert config types to forecast engine config
		engineConfig := forecast.Config{
			Enabled:              cfg.Forecast.Enabled,
			LookAheadHours:       cfg.Forecast.LookAheadHours,
			WeatherProvider:      cfg.Forecast.WeatherProvider,
			WeatherAPIKey:        cfg.Forecast.WeatherAPIKey,
			DatabasePath:         cfg.Forecast.DatabasePath,
			ConfidenceThreshold:  cfg.Forecast.ConfidenceThreshold,
			MaxDelayHours:        cfg.Forecast.MaxDelayHours,
			UpdateInterval:       cfg.Forecast.UpdateInterval,
			RegionLatitude:       cfg.Forecast.RegionLatitude,
			RegionLongitude:      cfg.Forecast.RegionLongitude,
			TrainingLookbackDays: cfg.Forecast.TrainingLookbackDays,
			MinimumImprovement:   cfg.Forecast.MinimumImprovement,
		}
		
		engine, err := forecast.NewEngine(engineConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to initialize forecast engine, forecasting disabled")
		} else {
			impl.forecastEngine = engine
			klog.V(2).InfoS("Forecast engine initialized successfully")
		}
	}
	
	return impl
}

func (c *carbonImpl) GetCurrentIntensity(ctx context.Context) (float64, error) {
	// Log region used for debugging
	klog.V(3).InfoS("Fetching carbon intensity data",
		"region", c.config.APIConfig.Region,
		"apiKey", c.config.APIConfig.APIKey != "")

	// The API client will check cache first and only make a request if needed
	data, err := c.apiClient.GetCarbonIntensity(ctx, c.config.APIConfig.Region)
	if err != nil {
		klog.V(2).InfoS("Failed to get carbon intensity data", "error", err)
		return 0, fmt.Errorf("failed to get carbon intensity data: %v", err)
	}

	return data.CarbonIntensity, nil
}

func (c *carbonImpl) CheckIntensityConstraints(ctx context.Context, threshold float64) *framework.Status {
	klog.V(2).InfoS("Checking carbon intensity constraints",
		"threshold", threshold,
		"region", c.config.APIConfig.Region,
		"forecastEnabled", c.forecastEngine != nil)

	// Use intelligent forecasting if enabled
	if c.forecastEngine != nil {
		maxDelay := time.Duration(c.config.Forecast.MaxDelayHours) * time.Hour
		decision, err := c.GetIntelligentSchedulingDecision(ctx, threshold, maxDelay)
		if err != nil {
			klog.V(2).InfoS("Forecast decision failed, falling back to simple threshold check", "error", err)
		} else {
			// Store historical data for model training
			go func() {
				if err := c.forecastEngine.StoreHistoricalData(ctx, c.config.APIConfig.Region, decision.CurrentIntensity); err != nil {
					klog.V(3).InfoS("Failed to store historical data", "error", err)
				}
			}()
			
			if decision.ShouldSchedule {
				klog.V(2).InfoS("Forecast recommends immediate scheduling",
					"reason", decision.Reason,
					"currentIntensity", decision.CurrentIntensity,
					"confidence", decision.ConfidenceLevel)
				return framework.NewStatus(framework.Success, "")
			} else {
				msg := fmt.Sprintf("Forecast recommends delay: %s (expected improvement: %.1f gCO2eq/kWh)", 
					decision.Reason, decision.ExpectedImprovement)
				klog.V(2).InfoS("Forecast recommends delaying scheduling",
					"reason", decision.Reason,
					"recommendedDelay", decision.RecommendedDelay,
					"expectedImprovement", decision.ExpectedImprovement,
					"confidence", decision.ConfidenceLevel)
				return framework.NewStatus(framework.Unschedulable, msg)
			}
		}
	}

	// Fallback to simple threshold-based check
	intensity, err := c.GetCurrentIntensity(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to get carbon intensity data",
			"region", c.config.APIConfig.Region)
		return framework.NewStatus(framework.Error, err.Error())
	}

	klog.V(2).InfoS("Carbon intensity check",
		"intensity", intensity,
		"threshold", threshold,
		"region", c.config.APIConfig.Region,
		"exceeds", intensity > threshold)

	if intensity > threshold {
		msg := fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)", intensity, threshold)
		klog.V(2).InfoS("Carbon intensity exceeds threshold - delaying scheduling",
			"intensity", intensity,
			"threshold", threshold,
			"region", c.config.APIConfig.Region)
		return framework.NewStatus(framework.Unschedulable, msg)
	}

	klog.V(2).InfoS("Carbon intensity within acceptable limits",
		"intensity", intensity,
		"threshold", threshold,
		"region", c.config.APIConfig.Region)
	return framework.NewStatus(framework.Success, "")
}

// GetIntelligentSchedulingDecision uses forecasting for intelligent scheduling decisions
func (c *carbonImpl) GetIntelligentSchedulingDecision(ctx context.Context, threshold float64, maxDelay time.Duration) (*forecast.SchedulingDecision, error) {
	if c.forecastEngine == nil {
		return nil, fmt.Errorf("forecast engine not initialized")
	}

	// Create forecast request
	now := time.Now()
	request := forecast.ForecastRequest{
		Region:     c.config.APIConfig.Region,
		StartTime:  now,
		EndTime:    now.Add(time.Duration(c.config.Forecast.LookAheadHours) * time.Hour),
		Resolution: time.Hour, // 1-hour resolution
		Threshold:  threshold,
		MaxDelay:   maxDelay,
	}

	// Get intelligent scheduling decision
	decision, err := c.forecastEngine.GetSchedulingDecision(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduling decision: %v", err)
	}

	return decision, nil
}
