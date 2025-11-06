package cache

import (
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
)

func TestNew(t *testing.T) {
	// Test with provided duration
	c := New(5*time.Minute, 0)
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.expiration != 5*time.Minute {
		t.Errorf("Expected expiration to be 5m, got %v", c.expiration)
	}

	// Test with zero duration (should use default of 30m)
	c = New(0, 0)
	if c.expiration != 30*time.Minute {
		t.Errorf("Expected default expiration to be 30m, got %v", c.expiration)
	}
}

func TestSetGet(t *testing.T) {
	c := New(5*time.Minute, 0)

	// Initial state: cache is empty
	if c.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", c.Size())
	}

	// Test cache miss
	data, found := c.Get("test-region")
	if found {
		t.Error("Get() returned true for non-existent key")
	}
	if data != nil {
		t.Errorf("Get() returned non-nil data for non-existent key: %+v", data)
	}

	// Test cache set and hit
	testData := &api.ElectricityData{
		CarbonIntensity: 200.0,
		Timestamp:       time.Now(),
	}
	c.Set("test-region", testData)

	// Verify size updated
	if c.Size() != 1 {
		t.Errorf("Expected cache size 1 after Set(), got %d", c.Size())
	}

	// Test cache hit
	data, found = c.Get("test-region")
	if !found {
		t.Error("Get() returned false for existing key")
	}
	if data == nil {
		t.Error("Get() returned nil for existing key")
	}
	if data.CarbonIntensity != testData.CarbonIntensity {
		t.Errorf("Expected carbon intensity %f, got %f", testData.CarbonIntensity, data.CarbonIntensity)
	}

	// Test metric counts
	hits, misses := c.GetMetrics()
	if hits != 1 {
		t.Errorf("Expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
}

func TestCacheTTL(t *testing.T) {
	// Use a reasonable expiration duration
	expiration := 5 * time.Minute
	c := New(expiration, 0)

	// 1. Initial cache entry with a specific timestamp
	pastTime := time.Now().Add(-6 * time.Minute) // Timestamp older than expiration

	// Create the entry manually to simulate expired entry
	c.mutex.Lock()
	c.data["test-region"] = &cacheEntry{
		data: &api.ElectricityData{
			CarbonIntensity: 200.0,
		},
		timestamp: pastTime, // Set to past time (already expired)
		hits:      0,
	}
	c.mutex.Unlock()

	// Should be a miss since entry is too old
	_, found := c.Get("test-region")
	if found {
		t.Error("Get() returned true for expired entry")
	}

	// Now add a fresh entry
	currentEntry := &api.ElectricityData{
		CarbonIntensity: 250.0,
		Timestamp:       time.Now(),
	}
	c.Set("test-region-fresh", currentEntry)

	// Should be a hit
	data, found := c.Get("test-region-fresh")
	if !found {
		t.Error("Get() returned false for fresh entry")
	}
	if data.CarbonIntensity != currentEntry.CarbonIntensity {
		t.Errorf("Expected carbon intensity %f, got %f", currentEntry.CarbonIntensity, data.CarbonIntensity)
	}

	// Check metrics
	hits, misses := c.GetMetrics()
	if hits != 1 {
		t.Errorf("Expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
}

func TestClear(t *testing.T) {
	c := New(5*time.Minute, 0)

	// Set some test data
	c.Set("region1", &api.ElectricityData{CarbonIntensity: 100})
	c.Set("region2", &api.ElectricityData{CarbonIntensity: 200})

	if c.Size() != 2 {
		t.Errorf("Expected cache size 2, got %d", c.Size())
	}

	// Test Clear
	c.Clear()
	if c.Size() != 0 {
		t.Errorf("Expected empty cache after Clear(), got size %d", c.Size())
	}

	// Test getting after clear
	_, found := c.Get("region1")
	if found {
		t.Error("Get() found entry after Clear()")
	}
}

func TestGetRegions(t *testing.T) {
	c := New(5*time.Minute, 0)

	// Set some test data
	c.Set("region1", &api.ElectricityData{CarbonIntensity: 100})
	c.Set("region2", &api.ElectricityData{CarbonIntensity: 200})

	// Test GetRegions
	regions := c.GetRegions()
	if len(regions) != 2 {
		t.Errorf("Expected 2 regions, got %d", len(regions))
	}

	// Check region names (order may vary)
	regionMap := make(map[string]bool)
	for _, r := range regions {
		regionMap[r] = true
	}

	if !regionMap["region1"] || !regionMap["region2"] {
		t.Errorf("Regions did not contain expected values, got %v", regions)
	}
}

func TestRemoveExpired(t *testing.T) {
	// Create cache with specific expiration
	expiration := 10 * time.Millisecond
	c := New(expiration, 0)

	// Set entries through the normal API to ensure consistency
	pastEntry := &api.ElectricityData{
		CarbonIntensity: 100.0,
		Timestamp:       time.Now().Add(-time.Hour), // Old timestamp
	}
	c.Set("expired-region", pastEntry)

	// Manually backdating the timestamp of the first entry to simulate an old entry
	// Note: In a real application, entries naturally age over time
	// Cleanup removes entries older than 2x expiration
	now := time.Now()
	c.mutex.Lock()
	if entry, exists := c.data["expired-region"]; exists {
		// Set timestamp to well past 2x expiration
		entry.timestamp = now.Add(-expiration * 3)
	}
	c.mutex.Unlock()

	// Add a second entry that should remain valid
	currentEntry := &api.ElectricityData{
		CarbonIntensity: 200.0,
		Timestamp:       time.Now(),
	}
	c.Set("valid-region", currentEntry)

	// Verify we have 2 entries
	if c.Size() != 2 {
		t.Errorf("Expected 2 entries before cleanup, got %d", c.Size())
	}

	// Manually trigger cleanup
	c.removeExpired()

	// Verify which entries remain
	if c.Size() != 1 {
		t.Errorf("Expected 1 entry after cleanup, got %d", c.Size())
	}

	// Check if the right entry was removed
	_, found := c.Get("expired-region")
	if found {
		t.Error("Expected expired entry to be removed")
	}

	data, found := c.Get("valid-region")
	if !found {
		t.Error("Expected valid entry to remain")
	} else if data.CarbonIntensity != 200.0 {
		t.Errorf("Expected carbon intensity 200.0, got %f", data.CarbonIntensity)
	}

	// Close to stop goroutine
	c.Close()
}

func TestClose(t *testing.T) {
	c := New(5*time.Minute, 0)

	// Just ensure Close() doesn't panic
	c.Close()
}
