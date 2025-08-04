package forecast

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// Engine coordinates carbon intensity forecasting and scheduling decisions
type Engine struct {
	dataCollector DataCollector
	weatherClient WeatherClient
	config        Config
	cache         *PredictionCache
	metrics       ModelMetrics
	mutex         sync.RWMutex
}

// Config holds forecasting engine configuration
type Config struct {
	Enabled             bool          `yaml:"enabled"`
	LookAheadHours      int           `yaml:"lookAheadHours"`
	WeatherProvider     string        `yaml:"weatherProvider"`
	WeatherAPIKey       string        `yaml:"weatherAPIKey"`
	DatabasePath        string        `yaml:"databasePath"`
	ConfidenceThreshold float64       `yaml:"confidenceThreshold"`
	MaxDelayHours       int           `yaml:"maxDelayHours"`
	UpdateInterval      time.Duration `yaml:"updateInterval"`
	RegionLatitude      float64       `yaml:"regionLatitude"`
	RegionLongitude     float64       `yaml:"regionLongitude"`
	TrainingLookbackDays int          `yaml:"trainingLookbackDays"`
	MinimumImprovement  float64       `yaml:"minimumImprovement"` // gCO2eq/kWh improvement threshold
}

// PredictionCache caches forecast predictions to avoid redundant API calls
type PredictionCache struct {
	predictions map[string]*CachedPrediction
	mutex       sync.RWMutex
	ttl         time.Duration
}

// CachedPrediction wraps a forecast response with cache metadata
type CachedPrediction struct {
	Response  *ForecastResponse
	Timestamp time.Time
	Region    string
}

// NewEngine creates a new forecasting engine
func NewEngine(config Config) (*Engine, error) {
	if !config.Enabled {
		return &Engine{config: config}, nil
	}

	// Initialize data collector
	var collector DataCollector
	var err error
	if config.DatabasePath != "" {
		collector, err = NewSQLiteDataCollector(config.DatabasePath)
	} else {
		collector, err = NewFileDataCollector("/tmp/compute-gardener-forecast")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize data collector: %v", err)
	}

	// Initialize weather client
	var weatherClient WeatherClient
	switch config.WeatherProvider {
	case "solcast":
		weatherClient = NewSolcastClient(config.WeatherAPIKey)
	case "openweathermap", "":
		weatherClient = NewOpenWeatherMapClient(config.WeatherAPIKey)
	default:
		return nil, fmt.Errorf("unsupported weather provider: %s", config.WeatherProvider)
	}

	// Initialize prediction cache
	cache := &PredictionCache{
		predictions: make(map[string]*CachedPrediction),
		ttl:         config.UpdateInterval,
	}

	engine := &Engine{
		dataCollector: collector,
		weatherClient: weatherClient,
		config:        config,
		cache:         cache,
		metrics: ModelMetrics{
			LastTraining:   time.Time{},
			MAE:            0.0,
			RMSE:           0.0,
			R2Score:        0.0,
			ValidationLoss: 0.0,
		},
	}

	return engine, nil
}

// GetSchedulingDecision makes an intelligent scheduling decision based on forecasts
func (e *Engine) GetSchedulingDecision(ctx context.Context, request ForecastRequest) (*SchedulingDecision, error) {
	if !e.config.Enabled {
		// Fallback to simple threshold-based decision
		return &SchedulingDecision{
			ShouldSchedule:      true,
			RecommendedDelay:    0,
			Reason:              "Forecasting disabled - proceeding with immediate scheduling",
			ConfidenceLevel:     1.0,
			ExpectedImprovement: 0.0,
		}, nil
	}

	// Get forecast predictions
	forecast, err := e.GetForecast(ctx, request)
	if err != nil {
		klog.V(2).InfoS("Failed to get forecast, falling back to immediate scheduling", "error", err)
		return &SchedulingDecision{
			ShouldSchedule:      true,
			RecommendedDelay:    0,
			Reason:              fmt.Sprintf("Forecast unavailable (%v) - proceeding with immediate scheduling", err),
			ConfidenceLevel:     0.0,
			ExpectedImprovement: 0.0,
		}, nil
	}

	// Analyze predictions and make scheduling decision
	return e.analyzeAndDecide(request, forecast)
}

// GetForecast generates carbon intensity predictions for the requested time window
func (e *Engine) GetForecast(ctx context.Context, request ForecastRequest) (*ForecastResponse, error) {
	if !e.config.Enabled {
		return nil, fmt.Errorf("forecasting is disabled")
	}

	// Check cache first
	cacheKey := e.buildCacheKey(request)
	if cached := e.cache.Get(cacheKey); cached != nil {
		klog.V(3).InfoS("Using cached forecast prediction", "region", request.Region, "cacheKey", cacheKey)
		return cached.Response, nil
	}

	// Get current weather conditions and forecast
	weather, err := e.weatherClient.GetCurrentWeather(ctx, e.config.RegionLatitude, e.config.RegionLongitude)
	if err != nil {
		klog.V(2).InfoS("Failed to get current weather", "error", err)
		weather = &WeatherData{Timestamp: time.Now()} // Use empty weather data as fallback
	}

	weatherForecast, err := e.weatherClient.GetWeatherForecast(ctx, e.config.RegionLatitude, e.config.RegionLongitude, e.config.LookAheadHours)
	if err != nil {
		klog.V(2).InfoS("Failed to get weather forecast", "error", err)
		weatherForecast = []WeatherData{} // Use empty forecast as fallback
	}

	// Generate predictions using simplified model
	predictions := e.generatePredictions(request, *weather, weatherForecast)

	// Find optimal scheduling time
	optimalTime := e.findOptimalSchedulingTime(request, predictions)

	// Calculate expected savings
	expectedSavings := e.calculateExpectedSavings(request, predictions, optimalTime)

	response := &ForecastResponse{
		Predictions:         predictions,
		OptimalScheduleTime: optimalTime,
		ExpectedSavings:     expectedSavings,
		ModelConfidence:     e.calculateOverallConfidence(predictions),
		ModelMetrics:        e.metrics,
	}

	// Cache the response
	e.cache.Set(cacheKey, &CachedPrediction{
		Response:  response,
		Timestamp: time.Now(),
		Region:    request.Region,
	})

	return response, nil
}

// generatePredictions creates carbon intensity predictions using a simplified model
func (e *Engine) generatePredictions(request ForecastRequest, currentWeather WeatherData, weatherForecast []WeatherData) []ForecastPrediction {
	var predictions []ForecastPrediction
	
	current := request.StartTime
	for current.Before(request.EndTime) {
		// Find corresponding weather data
		var weather WeatherData
		if len(weatherForecast) > 0 {
			weather = e.findClosestWeatherData(current, weatherForecast)
		} else {
			weather = currentWeather // Fallback to current weather
		}

		// Simple prediction model based on time of day and weather
		prediction := e.predictCarbonIntensity(current, weather, request.Region)
		
		predictions = append(predictions, prediction)
		current = current.Add(request.Resolution)
	}

	return predictions
}

// predictCarbonIntensity implements a simplified carbon intensity prediction model
func (e *Engine) predictCarbonIntensity(timestamp time.Time, weather WeatherData, region string) ForecastPrediction {
	// Base intensity varies by time of day (simplified pattern)
	hour := timestamp.Hour()
	var baseIntensity float64
	
	// Typical daily pattern: lower at night (more renewables), higher during peak hours
	switch {
	case hour >= 2 && hour <= 6:   // Early morning - low demand, more renewables
		baseIntensity = 300.0
	case hour >= 10 && hour <= 16: // Midday - solar peak, lower intensity
		baseIntensity = 250.0
	case hour >= 18 && hour <= 22: // Evening peak - higher demand, higher intensity
		baseIntensity = 450.0
	default:                        // Night/transition hours
		baseIntensity = 380.0
	}

	// Adjust based on weather conditions
	weatherAdjustment := 0.0
	
	// Solar irradiance impact (more sun = more solar = lower intensity)
	if weather.GlobalIrradiance > 0 {
		solarFactor := math.Min(weather.GlobalIrradiance/1000.0, 1.0) // Normalize to 0-1
		weatherAdjustment -= solarFactor * 50.0 // Up to 50 gCO2eq/kWh reduction
	}
	
	// Wind speed impact (more wind = more wind power = lower intensity)
	if weather.WindSpeed > 0 {
		windFactor := math.Min(weather.WindSpeed/15.0, 1.0) // Normalize to 0-1 (15 m/s as max)
		weatherAdjustment -= windFactor * 30.0 // Up to 30 gCO2eq/kWh reduction
	}
	
	// Cloud cover impact (reduces solar effectiveness)
	if weather.CloudCover > 0 {
		cloudFactor := weather.CloudCover / 100.0 // Convert percentage to 0-1
		weatherAdjustment += cloudFactor * 25.0 // Up to 25 gCO2eq/kWh increase
	}

	predictedIntensity := baseIntensity + weatherAdjustment

	// Ensure non-negative intensity
	if predictedIntensity < 0 {
		predictedIntensity = 0
	}

	// Calculate confidence based on weather data availability and time horizon
	confidence := e.calculatePredictionConfidence(timestamp, weather)

	// Calculate confidence intervals (simplified ±15% for now)
	margin := predictedIntensity * 0.15
	
	return ForecastPrediction{
		Timestamp:         timestamp,
		PredictedIntensity: predictedIntensity,
		Confidence:        confidence,
		LowerBound:        math.Max(0, predictedIntensity-margin),
		UpperBound:        predictedIntensity + margin,
		OptimalWindow:     predictedIntensity < baseIntensity*0.8, // Consider optimal if 20% below base
	}
}

// calculatePredictionConfidence determines confidence level for a prediction
func (e *Engine) calculatePredictionConfidence(timestamp time.Time, weather WeatherData) float64 {
	confidence := 1.0
	
	// Decrease confidence based on time horizon
	hoursAhead := time.Until(timestamp).Hours()
	if hoursAhead > 24 {
		confidence *= 0.7 // Lower confidence for predictions beyond 24 hours
	} else if hoursAhead > 12 {
		confidence *= 0.85
	}
	
	// Increase confidence if we have good weather data
	if weather.GlobalIrradiance > 0 && weather.WindSpeed > 0 {
		confidence = math.Min(confidence*1.1, 1.0)
	}
	
	// Ensure confidence is between 0 and 1
	return math.Max(0.0, math.Min(1.0, confidence))
}

// findClosestWeatherData finds the weather data point closest to the target time
func (e *Engine) findClosestWeatherData(target time.Time, forecast []WeatherData) WeatherData {
	if len(forecast) == 0 {
		return WeatherData{Timestamp: target}
	}
	
	closest := forecast[0]
	minDiff := math.Abs(target.Sub(closest.Timestamp).Seconds())
	
	for _, weather := range forecast[1:] {
		diff := math.Abs(target.Sub(weather.Timestamp).Seconds())
		if diff < minDiff {
			minDiff = diff
			closest = weather
		}
	}
	
	return closest
}

// analyzeAndDecide makes a scheduling decision based on forecast predictions
func (e *Engine) analyzeAndDecide(request ForecastRequest, forecast *ForecastResponse) (*SchedulingDecision, error) {
	if len(forecast.Predictions) == 0 {
		return &SchedulingDecision{
			ShouldSchedule:      true,
			RecommendedDelay:    0,
			Reason:              "No predictions available - proceeding with immediate scheduling",
			ConfidenceLevel:     0.0,
			ExpectedImprovement: 0.0,
		}, nil
	}

	// Get current predicted intensity (first prediction)
	currentPrediction := forecast.Predictions[0]
	
	// Check if we should delay based on optimal scheduling time
	if forecast.OptimalScheduleTime != nil && 
		   forecast.OptimalScheduleTime.After(time.Now()) &&
		   time.Until(*forecast.OptimalScheduleTime) <= time.Duration(e.config.MaxDelayHours)*time.Hour {
		
		delay := time.Until(*forecast.OptimalScheduleTime)
		expectedImprovement := forecast.ExpectedSavings
		
		// Only recommend delay if improvement meets minimum threshold and we have sufficient confidence
		if expectedImprovement >= e.config.MinimumImprovement && 
		   forecast.ModelConfidence >= e.config.ConfidenceThreshold {
			return &SchedulingDecision{
				ShouldSchedule:      false,
				RecommendedDelay:    delay,
				Reason:              fmt.Sprintf("Delaying for optimal carbon window - expected %0.1f gCO2eq/kWh improvement", expectedImprovement),
				CurrentIntensity:    currentPrediction.PredictedIntensity,
				PredictedIntensity:  e.getIntensityAtTime(*forecast.OptimalScheduleTime, forecast.Predictions),
				ConfidenceLevel:     forecast.ModelConfidence,
				ExpectedImprovement: expectedImprovement,
			}, nil
		}
	}

	// Check if current time is already in an optimal window
	if currentPrediction.OptimalWindow && currentPrediction.Confidence >= e.config.ConfidenceThreshold {
		return &SchedulingDecision{
			ShouldSchedule:      true,
			RecommendedDelay:    0,
			Reason:              "Current time is in optimal carbon intensity window",
			CurrentIntensity:    currentPrediction.PredictedIntensity,
			PredictedIntensity:  currentPrediction.PredictedIntensity,
			ConfidenceLevel:     currentPrediction.Confidence,
			ExpectedImprovement: 0.0,
		}, nil
	}

	// Default to immediate scheduling if no clear benefit from delay
	return &SchedulingDecision{
		ShouldSchedule:      true,
		RecommendedDelay:    0,
		Reason:              "No significant improvement expected from delay - proceeding immediately",
		CurrentIntensity:    currentPrediction.PredictedIntensity,
		PredictedIntensity:  currentPrediction.PredictedIntensity,
		ConfidenceLevel:     currentPrediction.Confidence,
		ExpectedImprovement: 0.0,
	}, nil
}

// findOptimalSchedulingTime identifies the best time to schedule within the forecast window
func (e *Engine) findOptimalSchedulingTime(request ForecastRequest, predictions []ForecastPrediction) *time.Time {
	if len(predictions) == 0 {
		return nil
	}

	var optimal *ForecastPrediction
	for i := range predictions {
		pred := &predictions[i]
		
		// Skip if prediction is too far in the future
		if time.Until(pred.Timestamp) > time.Duration(e.config.MaxDelayHours)*time.Hour {
			continue
		}
		
		// Skip if confidence is too low
		if pred.Confidence < e.config.ConfidenceThreshold {
			continue
		}
		
		// Select if this is the first viable option or if it's better
		if optimal == nil || pred.PredictedIntensity < optimal.PredictedIntensity {
			optimal = pred
		}
	}

	if optimal != nil {
		return &optimal.Timestamp
	}
	return nil
}

// calculateExpectedSavings estimates carbon savings from optimal scheduling
func (e *Engine) calculateExpectedSavings(request ForecastRequest, predictions []ForecastPrediction, optimalTime *time.Time) float64 {
	if len(predictions) == 0 || optimalTime == nil {
		return 0.0
	}

	currentIntensity := predictions[0].PredictedIntensity
	optimalIntensity := e.getIntensityAtTime(*optimalTime, predictions)
	
	return math.Max(0.0, currentIntensity-optimalIntensity)
}

// getIntensityAtTime finds the predicted intensity at a specific time
func (e *Engine) getIntensityAtTime(target time.Time, predictions []ForecastPrediction) float64 {
	if len(predictions) == 0 {
		return 0.0
	}

	// Find closest prediction
	closest := predictions[0]
	minDiff := math.Abs(target.Sub(closest.Timestamp).Seconds())

	for _, pred := range predictions[1:] {
		diff := math.Abs(target.Sub(pred.Timestamp).Seconds())
		if diff < minDiff {
			minDiff = diff
			closest = pred
		}
	}

	return closest.PredictedIntensity
}

// calculateOverallConfidence computes overall model confidence from predictions
func (e *Engine) calculateOverallConfidence(predictions []ForecastPrediction) float64 {
	if len(predictions) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, pred := range predictions {
		sum += pred.Confidence
	}
	
	return sum / float64(len(predictions))
}

// StoreHistoricalData records carbon intensity with weather context for model training
func (e *Engine) StoreHistoricalData(ctx context.Context, region string, carbonIntensity float64) error {
	if !e.config.Enabled || e.dataCollector == nil {
		return nil // Skip if forecasting is disabled
	}

	// Get current weather conditions
	weather, err := e.weatherClient.GetCurrentWeather(ctx, e.config.RegionLatitude, e.config.RegionLongitude)
	if err != nil {
		klog.V(2).InfoS("Failed to get weather for historical record", "error", err)
		weather = &WeatherData{Timestamp: time.Now()} // Use empty weather data
	}

	record := CarbonIntensityRecord{
		Timestamp:       time.Now(),
		Region:          region,
		CarbonIntensity: carbonIntensity,
		Weather:        *weather,
	}

	return e.dataCollector.Store(record)
}

// Close shuts down the forecasting engine
func (e *Engine) Close() error {
	if e.dataCollector != nil {
		return e.dataCollector.Close()
	}
	return nil
}

// Cache methods
func (c *PredictionCache) Get(key string) *CachedPrediction {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	cached, exists := c.predictions[key]
	if !exists {
		return nil
	}
	
	// Check if cache entry has expired
	if time.Since(cached.Timestamp) > c.ttl {
		delete(c.predictions, key)
		return nil
	}
	
	return cached
}

func (c *PredictionCache) Set(key string, prediction *CachedPrediction) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	c.predictions[key] = prediction
}

func (e *Engine) buildCacheKey(request ForecastRequest) string {
	return fmt.Sprintf("%s_%d_%d_%s", 
		request.Region, 
		request.StartTime.Unix(), 
		request.EndTime.Unix(),
		request.Resolution.String())
}