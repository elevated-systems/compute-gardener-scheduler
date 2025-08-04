package forecast

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/klog/v2"
	_ "github.com/mattn/go-sqlite3"
)

// DataCollector interface for persisting historical carbon intensity and weather data
type DataCollector interface {
	Store(record CarbonIntensityRecord) error
	GetHistoricalData(region string, start, end time.Time) ([]CarbonIntensityRecord, error)
	GetTrainingData(region string, lookbackDays int) ([]CarbonIntensityRecord, error)
	Cleanup(retentionDays int) error
	Close() error
}

// SQLiteDataCollector implements DataCollector using SQLite for local persistence
type SQLiteDataCollector struct {
	db       *sql.DB
	dbPath   string
	mutex    sync.RWMutex
	prepared map[string]*sql.Stmt
}

// FileDataCollector implements DataCollector using JSON files (simpler alternative)
type FileDataCollector struct {
	dataDir string
	mutex   sync.RWMutex
}

// NewSQLiteDataCollector creates a new SQLite-based data collector
func NewSQLiteDataCollector(dbPath string) (*SQLiteDataCollector, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %v", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_sync=NORMAL&_cache=shared")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	collector := &SQLiteDataCollector{
		db:       db,
		dbPath:   dbPath,
		prepared: make(map[string]*sql.Stmt),
	}

	// Initialize database schema
	if err := collector.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database schema: %v", err)
	}

	// Prepare common statements
	if err := collector.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %v", err)
	}

	return collector, nil
}

// NewFileDataCollector creates a new file-based data collector
func NewFileDataCollector(dataDir string) (*FileDataCollector, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	return &FileDataCollector{
		dataDir: dataDir,
	}, nil
}

func (c *SQLiteDataCollector) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS carbon_intensity_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		region TEXT NOT NULL,
		carbon_intensity REAL NOT NULL,
		temperature REAL,
		global_irradiance REAL,
		direct_irradiance REAL,
		diffuse_irradiance REAL,
		cloud_cover REAL,
		wind_speed REAL,
		humidity REAL,
		pressure REAL,
		weather_data TEXT, -- JSON blob for full weather data
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_region_timestamp ON carbon_intensity_records(region, timestamp);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON carbon_intensity_records(timestamp);
	CREATE INDEX IF NOT EXISTS idx_created_at ON carbon_intensity_records(created_at);
	`

	_, err := c.db.Exec(schema)
	return err
}

func (c *SQLiteDataCollector) prepareStatements() error {
	statements := map[string]string{
		"insert": `
			INSERT INTO carbon_intensity_records (
				timestamp, region, carbon_intensity, temperature, global_irradiance,
				direct_irradiance, diffuse_irradiance, cloud_cover, wind_speed,
				humidity, pressure, weather_data
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
		"select_range": `
			SELECT timestamp, region, carbon_intensity, temperature, global_irradiance,
				   direct_irradiance, diffuse_irradiance, cloud_cover, wind_speed,
				   humidity, pressure, weather_data
			FROM carbon_intensity_records
			WHERE region = ? AND timestamp BETWEEN ? AND ?
			ORDER BY timestamp ASC
		`,
		"select_training": `
			SELECT timestamp, region, carbon_intensity, temperature, global_irradiance,
				   direct_irradiance, diffuse_irradiance, cloud_cover, wind_speed,
				   humidity, pressure, weather_data
			FROM carbon_intensity_records
			WHERE region = ? AND timestamp >= ?
			ORDER BY timestamp ASC
		`,
		"cleanup": `
			DELETE FROM carbon_intensity_records
			WHERE created_at < ?
		`,
	}

	for name, query := range statements {
		stmt, err := c.db.Prepare(query)
		if err != nil {
			return fmt.Errorf("failed to prepare statement %s: %v", name, err)
		}
		c.prepared[name] = stmt
	}

	return nil
}

// Store saves a carbon intensity record with weather data
func (c *SQLiteDataCollector) Store(record CarbonIntensityRecord) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Serialize weather data to JSON
	weatherJSON, err := json.Marshal(record.Weather)
	if err != nil {
		return fmt.Errorf("failed to marshal weather data: %v", err)
	}

	stmt := c.prepared["insert"]
	_, err = stmt.Exec(
		record.Timestamp,
		record.Region,
		record.CarbonIntensity,
		record.Weather.Temperature,
		record.Weather.GlobalIrradiance,
		record.Weather.DirectIrradiance,
		record.Weather.DiffuseIrradiance,
		record.Weather.CloudCover,
		record.Weather.WindSpeed,
		record.Weather.Humidity,
		record.Weather.Pressure,
		string(weatherJSON),
	)

	if err != nil {
		klog.V(2).InfoS("Failed to store carbon intensity record", "error", err, "region", record.Region)
		return fmt.Errorf("failed to store record: %v", err)
	}

	klog.V(3).InfoS("Stored carbon intensity record",
		"region", record.Region,
		"timestamp", record.Timestamp,
		"intensity", record.CarbonIntensity)

	return nil
}

// GetHistoricalData retrieves records for a specific time range
func (c *SQLiteDataCollector) GetHistoricalData(region string, start, end time.Time) ([]CarbonIntensityRecord, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	stmt := c.prepared["select_range"]
	rows, err := stmt.Query(region, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query historical data: %v", err)
	}
	defer rows.Close()

	return c.scanRows(rows)
}

// GetTrainingData retrieves recent data for model training
func (c *SQLiteDataCollector) GetTrainingData(region string, lookbackDays int) ([]CarbonIntensityRecord, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	startTime := time.Now().AddDate(0, 0, -lookbackDays)
	stmt := c.prepared["select_training"]
	rows, err := stmt.Query(region, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query training data: %v", err)
	}
	defer rows.Close()

	return c.scanRows(rows)
}

func (c *SQLiteDataCollector) scanRows(rows *sql.Rows) ([]CarbonIntensityRecord, error) {
	var records []CarbonIntensityRecord

	for rows.Next() {
		var record CarbonIntensityRecord
		var weatherJSON string

		err := rows.Scan(
			&record.Timestamp,
			&record.Region,
			&record.CarbonIntensity,
			&record.Weather.Temperature,
			&record.Weather.GlobalIrradiance,
			&record.Weather.DirectIrradiance,
			&record.Weather.DiffuseIrradiance,
			&record.Weather.CloudCover,
			&record.Weather.WindSpeed,
			&record.Weather.Humidity,
			&record.Weather.Pressure,
			&weatherJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}

		// Parse full weather data JSON if needed
		if weatherJSON != "" {
			if err := json.Unmarshal([]byte(weatherJSON), &record.Weather); err != nil {
				klog.V(2).InfoS("Failed to unmarshal weather JSON, using individual fields", "error", err)
			}
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}

	return records, nil
}

// Cleanup removes old records beyond retention period
func (c *SQLiteDataCollector) Cleanup(retentionDays int) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	stmt := c.prepared["cleanup"]
	result, err := stmt.Exec(cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup old records: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	klog.V(2).InfoS("Cleaned up old carbon intensity records",
		"cutoff", cutoffTime,
		"rowsDeleted", rowsAffected)

	return nil
}

// Close closes the database connection
func (c *SQLiteDataCollector) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close prepared statements
	for _, stmt := range c.prepared {
		stmt.Close()
	}

	return c.db.Close()
}

// File-based collector implementation (simpler alternative)

// Store saves a record to a daily JSON file
func (c *FileDataCollector) Store(record CarbonIntensityRecord) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Create filename based on date and region
	dateStr := record.Timestamp.Format("2006-01-02")
	filename := fmt.Sprintf("%s_%s.json", record.Region, dateStr)
	filepath := filepath.Join(c.dataDir, filename)

	// Read existing records for the day
	var records []CarbonIntensityRecord
	if data, err := os.ReadFile(filepath); err == nil {
		if err := json.Unmarshal(data, &records); err != nil {
			klog.V(2).InfoS("Failed to unmarshal existing records", "file", filepath, "error", err)
		}
	}

	// Append new record
	records = append(records, record)

	// Write back to file
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal records: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write records file: %v", err)
	}

	klog.V(3).InfoS("Stored carbon intensity record to file",
		"file", filepath,
		"region", record.Region,
		"intensity", record.CarbonIntensity)

	return nil
}

// GetHistoricalData reads records from multiple daily files
func (c *FileDataCollector) GetHistoricalData(region string, start, end time.Time) ([]CarbonIntensityRecord, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var allRecords []CarbonIntensityRecord

	// Iterate through each day in the range
	current := start
	for current.Before(end) || current.Equal(end) {
		dateStr := current.Format("2006-01-02")
		filename := fmt.Sprintf("%s_%s.json", region, dateStr)
		filepath := filepath.Join(c.dataDir, filename)

		if data, err := os.ReadFile(filepath); err == nil {
			var dayRecords []CarbonIntensityRecord
			if err := json.Unmarshal(data, &dayRecords); err == nil {
				// Filter records within time range
				for _, record := range dayRecords {
					if (record.Timestamp.After(start) || record.Timestamp.Equal(start)) &&
						(record.Timestamp.Before(end) || record.Timestamp.Equal(end)) {
						allRecords = append(allRecords, record)
					}
				}
			}
		}

		current = current.AddDate(0, 0, 1)
	}

	return allRecords, nil
}

// GetTrainingData retrieves recent data for model training
func (c *FileDataCollector) GetTrainingData(region string, lookbackDays int) ([]CarbonIntensityRecord, error) {
	startTime := time.Now().AddDate(0, 0, -lookbackDays)
	endTime := time.Now()
	return c.GetHistoricalData(region, startTime, endTime)
}

// Cleanup removes old daily files
func (c *FileDataCollector) Cleanup(retentionDays int) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	
	files, err := os.ReadDir(c.dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %v", err)
	}

	removedCount := 0
	for _, file := range files {
		if !file.Type().IsRegular() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		// Extract date from filename (assuming format: region_2006-01-02.json)
		name := file.Name()
		if len(name) < 10 {
			continue
		}
		
		dateStr := name[len(name)-15 : len(name)-5] // Extract YYYY-MM-DD part
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		if fileDate.Before(cutoffTime) {
			filepath := filepath.Join(c.dataDir, name)
			if err := os.Remove(filepath); err != nil {
				klog.V(2).InfoS("Failed to remove old file", "file", filepath, "error", err)
			} else {
				removedCount++
			}
		}
	}

	klog.V(2).InfoS("Cleaned up old carbon intensity files",
		"cutoff", cutoffTime,
		"filesDeleted", removedCount)

	return nil
}

// Close is a no-op for file collector
func (c *FileDataCollector) Close() error {
	return nil
}