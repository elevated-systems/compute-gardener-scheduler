package forecast

import (
	"time"
)

// WeatherData represents weather conditions that affect renewable energy generation
type WeatherData struct {
	Timestamp         time.Time `json:"timestamp"`
	Temperature       float64   `json:"temperature"`        // Celsius
	GlobalIrradiance  float64   `json:"globalIrradiance"`   // W/m²
	DirectIrradiance  float64   `json:"directIrradiance"`   // W/m²
	DiffuseIrradiance float64   `json:"diffuseIrradiance"`  // W/m²
	CloudCover        float64   `json:"cloudCover"`         // Percentage (0-100)
	WindSpeed         float64   `json:"windSpeed"`          // m/s
	Humidity          float64   `json:"humidity"`           // Percentage (0-100)
	Pressure          float64   `json:"pressure"`           // hPa
}

// CarbonIntensityRecord represents historical carbon intensity with weather context
type CarbonIntensityRecord struct {
	Timestamp       time.Time   `json:"timestamp"`
	Region          string      `json:"region"`
	CarbonIntensity float64     `json:"carbonIntensity"` // gCO2eq/kWh
	Weather         WeatherData `json:"weather"`
}

// ForecastPrediction represents ML model output for carbon intensity forecasting
type ForecastPrediction struct {
	Timestamp         time.Time `json:"timestamp"`
	PredictedIntensity float64  `json:"predictedIntensity"` // gCO2eq/kWh
	Confidence        float64   `json:"confidence"`         // 0.0-1.0
	LowerBound        float64   `json:"lowerBound"`         // 95% confidence interval
	UpperBound        float64   `json:"upperBound"`         // 95% confidence interval
	OptimalWindow     bool      `json:"optimalWindow"`      // True if predicted to be optimal time
}

// ModelMetrics tracks ML model performance and accuracy
type ModelMetrics struct {
	LastTraining    time.Time `json:"lastTraining"`
	TrainingRecords int       `json:"trainingRecords"`
	MAE             float64   `json:"mae"`  // Mean Absolute Error
	RMSE            float64   `json:"rmse"` // Root Mean Square Error
	R2Score         float64   `json:"r2Score"`
	ValidationLoss  float64   `json:"validationLoss"`
}

// ForecastRequest represents a request for carbon intensity predictions
type ForecastRequest struct {
	Region        string        `json:"region"`
	StartTime     time.Time     `json:"startTime"`
	EndTime       time.Time     `json:"endTime"`
	Resolution    time.Duration `json:"resolution"` // Prediction interval (e.g., 1h, 30m)
	Threshold     float64       `json:"threshold"`  // Current carbon intensity threshold
	MaxDelay      time.Duration `json:"maxDelay"`   // Maximum acceptable delay
}

// ForecastResponse contains model predictions and scheduling recommendations
type ForecastResponse struct {
	Predictions         []ForecastPrediction `json:"predictions"`
	OptimalScheduleTime *time.Time           `json:"optimalScheduleTime"` // Recommended scheduling time
	ExpectedSavings     float64              `json:"expectedSavings"`     // gCO2eq saved vs immediate scheduling
	ModelConfidence     float64              `json:"modelConfidence"`     // Overall confidence in recommendations
	ModelMetrics        ModelMetrics         `json:"modelMetrics"`
}

// SchedulingDecision represents the enhanced decision logic output
type SchedulingDecision struct {
	ShouldSchedule      bool      `json:"shouldSchedule"`
	RecommendedDelay    time.Duration `json:"recommendedDelay"`
	Reason              string    `json:"reason"`
	CurrentIntensity    float64   `json:"currentIntensity"`
	PredictedIntensity  float64   `json:"predictedIntensity"`
	ConfidenceLevel     float64   `json:"confidenceLevel"`
	ExpectedImprovement float64   `json:"expectedImprovement"` // gCO2eq/kWh improvement
}