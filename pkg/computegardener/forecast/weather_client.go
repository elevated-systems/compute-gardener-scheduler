package forecast

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"k8s.io/klog/v2"
)

// WeatherClient interface for fetching weather data from various providers
type WeatherClient interface {
	GetCurrentWeather(ctx context.Context, lat, lon float64) (*WeatherData, error)
	GetWeatherForecast(ctx context.Context, lat, lon float64, hours int) ([]WeatherData, error)
	GetHistoricalWeather(ctx context.Context, lat, lon float64, start, end time.Time) ([]WeatherData, error)
}

// OpenWeatherMapClient implements WeatherClient using OpenWeatherMap API
type OpenWeatherMapClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// SolcastClient implements WeatherClient using Solcast Solar API
type SolcastClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// OpenWeatherMap API response structures
type owmCurrentResponse struct {
	Main struct {
		Temp     float64 `json:"temp"`
		Humidity float64 `json:"humidity"`
		Pressure float64 `json:"pressure"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
	Clouds struct {
		All float64 `json:"all"`
	} `json:"clouds"`
	Timestamp int64 `json:"dt"`
}

type owmForecastResponse struct {
	List []struct {
		Timestamp int64 `json:"dt"`
		Main struct {
			Temp     float64 `json:"temp"`
			Humidity float64 `json:"humidity"`
			Pressure float64 `json:"pressure"`
		} `json:"main"`
		Wind struct {
			Speed float64 `json:"speed"`
		} `json:"wind"`
		Clouds struct {
			All float64 `json:"all"`
		} `json:"clouds"`
	} `json:"list"`
}

type owmSolarResponse struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Data []struct {
		Timestamp int64   `json:"dt"`
		Ghi       float64 `json:"ghi"` // Global Horizontal Irradiance
		Dni       float64 `json:"dni"` // Direct Normal Irradiance
		Dhi       float64 `json:"dhi"` // Diffuse Horizontal Irradiance
	} `json:"data"`
}

// Solcast API response structures
type solcastResponse struct {
	Forecasts []struct {
		PeriodEnd      string  `json:"period_end"`
		Ghi            float64 `json:"ghi"`
		Dni            float64 `json:"dni"`
		Dhi            float64 `json:"dhi"`
		AirTemp        float64 `json:"air_temp"`
		RelativeHumidity float64 `json:"relative_humidity"`
		WindSpeed10m   float64 `json:"wind_speed_10m"`
		CloudOpacity   float64 `json:"cloud_opacity"`
	} `json:"forecasts"`
}

// NewOpenWeatherMapClient creates a new OpenWeatherMap weather client
func NewOpenWeatherMapClient(apiKey string) *OpenWeatherMapClient {
	return &OpenWeatherMapClient{
		apiKey:  apiKey,
		baseURL: "https://api.openweathermap.org/data/2.5",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewSolcastClient creates a new Solcast weather client
func NewSolcastClient(apiKey string) *SolcastClient {
	return &SolcastClient{
		apiKey:  apiKey,
		baseURL: "https://api.solcast.com.au",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetCurrentWeather fetches current weather data from OpenWeatherMap
func (c *OpenWeatherMapClient) GetCurrentWeather(ctx context.Context, lat, lon float64) (*WeatherData, error) {
	// First get basic weather data
	weatherURL := fmt.Sprintf("%s/weather?lat=%f&lon=%f&appid=%s&units=metric", 
		c.baseURL, lat, lon, c.apiKey)
	
	req, err := http.NewRequestWithContext(ctx, "GET", weatherURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create weather request: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weather API returned status %d", resp.StatusCode)
	}

	var weatherResp owmCurrentResponse
	if err := json.NewDecoder(resp.Body).Decode(&weatherResp); err != nil {
		return nil, fmt.Errorf("failed to decode weather response: %v", err)
	}

	// Get solar irradiance data
	solarURL := fmt.Sprintf("%s/solar/current?lat=%f&lon=%f&appid=%s", 
		c.baseURL, lat, lon, c.apiKey)
	
	solarReq, err := http.NewRequestWithContext(ctx, "GET", solarURL, nil)
	if err != nil {
		klog.V(2).InfoS("Failed to create solar request, using zero irradiance", "error", err)
		return c.buildWeatherData(weatherResp, 0, 0, 0), nil
	}

	solarResp, err := c.httpClient.Do(solarReq)
	if err != nil {
		klog.V(2).InfoS("Solar request failed, using zero irradiance", "error", err)
		return c.buildWeatherData(weatherResp, 0, 0, 0), nil
	}
	defer solarResp.Body.Close()

	if solarResp.StatusCode != http.StatusOK {
		klog.V(2).InfoS("Solar API returned error status, using zero irradiance", "status", solarResp.StatusCode)
		return c.buildWeatherData(weatherResp, 0, 0, 0), nil
	}

	var solar owmSolarResponse
	if err := json.NewDecoder(solarResp.Body).Decode(&solar); err != nil {
		klog.V(2).InfoS("Failed to decode solar response, using zero irradiance", "error", err)
		return c.buildWeatherData(weatherResp, 0, 0, 0), nil
	}

	// Use most recent solar data if available
	ghi, dni, dhi := 0.0, 0.0, 0.0
	if len(solar.Data) > 0 {
		ghi = solar.Data[0].Ghi
		dni = solar.Data[0].Dni  
		dhi = solar.Data[0].Dhi
	}

	return c.buildWeatherData(weatherResp, ghi, dni, dhi), nil
}

func (c *OpenWeatherMapClient) buildWeatherData(weather owmCurrentResponse, ghi, dni, dhi float64) *WeatherData {
	return &WeatherData{
		Timestamp:         time.Unix(weather.Timestamp, 0),
		Temperature:       weather.Main.Temp,
		GlobalIrradiance:  ghi,
		DirectIrradiance:  dni,
		DiffuseIrradiance: dhi,
		CloudCover:        weather.Clouds.All,
		WindSpeed:         weather.Wind.Speed,
		Humidity:          weather.Main.Humidity,
		Pressure:          weather.Main.Pressure,
	}
}

// GetWeatherForecast fetches weather forecast data from OpenWeatherMap
func (c *OpenWeatherMapClient) GetWeatherForecast(ctx context.Context, lat, lon float64, hours int) ([]WeatherData, error) {
	forecastURL := fmt.Sprintf("%s/forecast?lat=%f&lon=%f&appid=%s&units=metric&cnt=%d", 
		c.baseURL, lat, lon, c.apiKey, hours/3) // OWM forecast is 3-hourly

	req, err := http.NewRequestWithContext(ctx, "GET", forecastURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create forecast request: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forecast request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forecast API returned status %d", resp.StatusCode)
	}

	var forecastResp owmForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&forecastResp); err != nil {
		return nil, fmt.Errorf("failed to decode forecast response: %v", err)
	}

	var forecasts []WeatherData
	for _, item := range forecastResp.List {
		forecasts = append(forecasts, WeatherData{
			Timestamp:         time.Unix(item.Timestamp, 0),
			Temperature:       item.Main.Temp,
			GlobalIrradiance:  0, // Solar forecast requires separate API call
			DirectIrradiance:  0,
			DiffuseIrradiance: 0,
			CloudCover:        item.Clouds.All,
			WindSpeed:         item.Wind.Speed,
			Humidity:          item.Main.Humidity,
			Pressure:          item.Main.Pressure,
		})
	}

	return forecasts, nil
}

// GetHistoricalWeather fetches historical weather data from OpenWeatherMap
func (c *OpenWeatherMapClient) GetHistoricalWeather(ctx context.Context, lat, lon float64, start, end time.Time) ([]WeatherData, error) {
	// OpenWeatherMap historical API requires separate calls for each day
	var allData []WeatherData
	
	current := start
	for current.Before(end) {
		histURL := fmt.Sprintf("%s/onecall/timemachine?lat=%f&lon=%f&dt=%d&appid=%s&units=metric", 
			c.baseURL, lat, lon, current.Unix(), c.apiKey)
		
		req, err := http.NewRequestWithContext(ctx, "GET", histURL, nil)
		if err != nil {
			klog.V(2).InfoS("Failed to create historical request", "date", current, "error", err)
			current = current.Add(24 * time.Hour)
			continue
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			klog.V(2).InfoS("Historical request failed", "date", current, "error", err)
			current = current.Add(24 * time.Hour)
			continue
		}
		resp.Body.Close() // Close immediately as this is a simplified implementation

		// For now, skip historical data collection to avoid API limits
		// In production, implement proper historical data collection with rate limiting
		current = current.Add(24 * time.Hour)
	}

	return allData, nil
}

// Solcast client implementations
func (c *SolcastClient) GetCurrentWeather(ctx context.Context, lat, lon float64) (*WeatherData, error) {
	latStr := strconv.FormatFloat(lat, 'f', 6, 64)
	lonStr := strconv.FormatFloat(lon, 'f', 6, 64)
	
	reqURL := fmt.Sprintf("%s/world_radiation/estimated_actuals?latitude=%s&longitude=%s&hours=1&format=json", 
		c.baseURL, latStr, lonStr)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create solcast request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solcast request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solcast API returned status %d", resp.StatusCode)
	}

	var solcastResp solcastResponse
	if err := json.NewDecoder(resp.Body).Decode(&solcastResp); err != nil {
		return nil, fmt.Errorf("failed to decode solcast response: %v", err)
	}

	if len(solcastResp.Forecasts) == 0 {
		return nil, fmt.Errorf("no solcast data available")
	}

	forecast := solcastResp.Forecasts[0]
	timestamp, err := time.Parse(time.RFC3339, forecast.PeriodEnd)
	if err != nil {
		timestamp = time.Now()
	}

	return &WeatherData{
		Timestamp:         timestamp,
		Temperature:       forecast.AirTemp,
		GlobalIrradiance:  forecast.Ghi,
		DirectIrradiance:  forecast.Dni,
		DiffuseIrradiance: forecast.Dhi,
		CloudCover:        forecast.CloudOpacity,
		WindSpeed:         forecast.WindSpeed10m,
		Humidity:          forecast.RelativeHumidity,
		Pressure:          1013.25, // Standard atmospheric pressure as fallback
	}, nil
}

func (c *SolcastClient) GetWeatherForecast(ctx context.Context, lat, lon float64, hours int) ([]WeatherData, error) {
	latStr := strconv.FormatFloat(lat, 'f', 6, 64)
	lonStr := strconv.FormatFloat(lon, 'f', 6, 64)
	
	reqURL := fmt.Sprintf("%s/world_radiation/forecasts?latitude=%s&longitude=%s&hours=%d&format=json", 
		c.baseURL, latStr, lonStr, hours)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create solcast forecast request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solcast forecast request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solcast forecast API returned status %d", resp.StatusCode)
	}

	var solcastResp solcastResponse
	if err := json.NewDecoder(resp.Body).Decode(&solcastResp); err != nil {
		return nil, fmt.Errorf("failed to decode solcast forecast response: %v", err)
	}

	var forecasts []WeatherData
	for _, forecast := range solcastResp.Forecasts {
		timestamp, err := time.Parse(time.RFC3339, forecast.PeriodEnd)
		if err != nil {
			continue
		}

		forecasts = append(forecasts, WeatherData{
			Timestamp:         timestamp,
			Temperature:       forecast.AirTemp,
			GlobalIrradiance:  forecast.Ghi,
			DirectIrradiance:  forecast.Dni,
			DiffuseIrradiance: forecast.Dhi,
			CloudCover:        forecast.CloudOpacity,
			WindSpeed:         forecast.WindSpeed10m,
			Humidity:          forecast.RelativeHumidity,
			Pressure:          1013.25, // Standard atmospheric pressure as fallback
		})
	}

	return forecasts, nil
}

func (c *SolcastClient) GetHistoricalWeather(ctx context.Context, lat, lon float64, start, end time.Time) ([]WeatherData, error) {
	// Solcast historical data requires different endpoint and parameters
	// This is a simplified implementation
	return []WeatherData{}, fmt.Errorf("historical weather data not implemented for Solcast")
}