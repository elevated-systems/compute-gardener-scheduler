package forecast

import (
	"context"
	"testing"
	"time"
)

func TestNewEngine(t *testing.T) {
	// Test creating engine with forecast disabled
	config := Config{
		Enabled: false,
	}
	
	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("Failed to create disabled engine: %v", err)
	}
	
	if engine.config.Enabled {
		t.Error("Engine should be disabled")
	}
	
	if engine.dataCollector != nil {
		t.Error("Data collector should be nil when disabled")
	}
}

func TestNewEngineEnabled(t *testing.T) {
	// Test creating engine with forecast enabled
	config := Config{
		Enabled:              true,
		LookAheadHours:       24,
		WeatherProvider:      "openweathermap",
		WeatherAPIKey:        "test-key",
		DatabasePath:         "",  // Will use file collector
		ConfidenceThreshold:  0.7,
		MaxDelayHours:        6,
		UpdateInterval:       time.Hour,
		RegionLatitude:       37.7749,
		RegionLongitude:      -122.4194,
		TrainingLookbackDays: 30,
		MinimumImprovement:   10.0,
	}
	
	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("Failed to create enabled engine: %v", err)
	}
	
	if !engine.config.Enabled {
		t.Error("Engine should be enabled")
	}
	
	if engine.dataCollector == nil {
		t.Error("Data collector should not be nil")
	}
	
	if engine.weatherClient == nil {
		t.Error("Weather client should not be nil")
	}
}

func TestPredictCarbonIntensity(t *testing.T) {
	config := Config{
		Enabled:              true,
		LookAheadHours:       24,
		WeatherProvider:      "openweathermap",
		WeatherAPIKey:        "test-key",
		ConfidenceThreshold:  0.7,
		MaxDelayHours:        6,
		UpdateInterval:       time.Hour,
		RegionLatitude:       37.7749,
		RegionLongitude:      -122.4194,
		TrainingLookbackDays: 30,
		MinimumImprovement:   10.0,
	}
	
	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	
	// Test prediction with typical daytime conditions
	timestamp := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC) // 2 PM
	weather := WeatherData{
		Timestamp:         timestamp,
		Temperature:       20.0,
		GlobalIrradiance:  800.0, // Good solar conditions
		WindSpeed:         10.0,  // Good wind conditions
		CloudCover:        20.0,  // Low cloud cover
		Humidity:          50.0,
		Pressure:          1013.25,
	}
	
	prediction := engine.predictCarbonIntensity(timestamp, weather, "test-region")
	
	// Check that prediction is reasonable
	if prediction.PredictedIntensity < 0 {
		t.Error("Predicted intensity should not be negative")
	}
	
	if prediction.Confidence < 0 || prediction.Confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", prediction.Confidence)
	}
	
	if prediction.LowerBound > prediction.PredictedIntensity {
		t.Error("Lower bound should not exceed predicted intensity")
	}
	
	if prediction.UpperBound < prediction.PredictedIntensity {
		t.Error("Upper bound should not be less than predicted intensity")
	}
	
	// With good solar and wind conditions, this should be considered optimal
	if !prediction.OptimalWindow {
		t.Error("Good renewable conditions should result in optimal window")
	}
}

func TestGetSchedulingDecisionDisabled(t *testing.T) {
	// Test with forecasting disabled
	config := Config{
		Enabled: false,
	}
	
	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	
	request := ForecastRequest{
		Region:     "test-region",
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(24 * time.Hour),
		Resolution: time.Hour,
		Threshold:  300.0,
		MaxDelay:   6 * time.Hour,
	}
	
	decision, err := engine.GetSchedulingDecision(context.Background(), request)
	if err != nil {
		t.Fatalf("GetSchedulingDecision should not fail when disabled: %v", err)
	}
	
	if !decision.ShouldSchedule {
		t.Error("Should schedule immediately when forecasting is disabled")
	}
	
	if decision.RecommendedDelay != 0 {
		t.Error("Should not recommend delay when forecasting is disabled")
	}
}

func TestCalculatePredictionConfidence(t *testing.T) {
	config := Config{Enabled: true}
	engine, _ := NewEngine(config)
	
	// Test confidence calculation for different time horizons
	baseTime := time.Now()
	
	// Near-term prediction (2 hours ahead)
	nearTerm := baseTime.Add(2 * time.Hour)
	weather := WeatherData{
		GlobalIrradiance: 500.0,
		WindSpeed:        8.0,
	}
	
	confidence := engine.calculatePredictionConfidence(nearTerm, weather)
	if confidence < 0.8 { // Should have high confidence for near-term with good weather data
		t.Errorf("Near-term prediction with good weather data should have high confidence, got %f", confidence)
	}
	
	// Long-term prediction (48 hours ahead)
	longTerm := baseTime.Add(48 * time.Hour)
	longTermConfidence := engine.calculatePredictionConfidence(longTerm, weather)
	if longTermConfidence >= confidence {
		t.Error("Long-term predictions should have lower confidence than near-term")
	}
}

func TestFindOptimalSchedulingTime(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxDelayHours:       6,
		ConfidenceThreshold: 0.7,
	}
	engine, _ := NewEngine(config)
	
	now := time.Now()
	predictions := []ForecastPrediction{
		{
			Timestamp:          now,
			PredictedIntensity: 400.0,
			Confidence:         0.8,
		},
		{
			Timestamp:          now.Add(2 * time.Hour),
			PredictedIntensity: 250.0, // Better option
			Confidence:         0.8,
		},
		{
			Timestamp:          now.Add(4 * time.Hour),
			PredictedIntensity: 200.0, // Best option
			Confidence:         0.8,
		},
		{
			Timestamp:          now.Add(8 * time.Hour), // Too far ahead
			PredictedIntensity: 150.0,
			Confidence:         0.8,
		},
	}
	
	request := ForecastRequest{MaxDelay: 6 * time.Hour}
	optimal := engine.findOptimalSchedulingTime(request, predictions)
	
	if optimal == nil {
		t.Fatal("Should find optimal scheduling time")
	}
	
	expected := now.Add(4 * time.Hour)
	if !optimal.Equal(expected) {
		t.Errorf("Expected optimal time %v, got %v", expected, *optimal)
	}
}

func TestPredictionCache(t *testing.T) {
	cache := &PredictionCache{
		predictions: make(map[string]*CachedPrediction),
		ttl:         time.Minute,
	}
	
	// Test cache miss
	result := cache.Get("test-key")
	if result != nil {
		t.Error("Should return nil for cache miss")
	}
	
	// Test cache set and hit
	prediction := &CachedPrediction{
		Response:  &ForecastResponse{},
		Timestamp: time.Now(),
		Region:    "test-region",
	}
	
	cache.Set("test-key", prediction)
	result = cache.Get("test-key")
	if result == nil {
		t.Error("Should return cached prediction")
	}
	
	// Test cache expiry
	expiredPrediction := &CachedPrediction{
		Response:  &ForecastResponse{},
		Timestamp: time.Now().Add(-2 * time.Minute), // Expired
		Region:    "test-region",
	}
	
	cache.Set("expired-key", expiredPrediction)
	result = cache.Get("expired-key")
	if result != nil {
		t.Error("Should return nil for expired cache entry")
	}
}