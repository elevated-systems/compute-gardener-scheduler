package metrics

import (
	"testing"
	"time"
)

// Helper function to create a test store
func newTestStore() *InMemoryStore {
	return NewInMemoryStore(
		time.Hour,           // cleanupPeriod
		time.Hour,           // retentionTime
		100,                 // maxRecordsPerPod
		&LTTBDownsampling{}, // downsamplingStrategy
	)
}

// Helper function to create a test record
func createTestRecord(timestamp time.Time) PodMetricsRecord {
	return PodMetricsRecord{
		Timestamp:     timestamp,
		CPU:           0.5,
		Memory:        1024 * 1024 * 100, // 100 MB
		GPUPowerWatts: 75.0,
		PowerEstimate: 100.0,
	}
}

func TestInMemoryStore_AddRecord(t *testing.T) {
	store := newTestStore()
	defer store.Close()

	podUID := "test-pod-123"
	podName := "test-pod"
	namespace := "default"
	nodeName := "node-1"
	now := time.Now()

	// Add a record
	record := createTestRecord(now)
	store.AddRecord(podUID, podName, namespace, nodeName, record)

	// Verify record was added
	history, exists := store.GetHistory(podUID)
	if !exists {
		t.Fatal("Expected pod history to exist")
	}

	if len(history.Records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(history.Records))
	}

	if history.PodUID != podUID {
		t.Errorf("Expected PodUID %s, got %s", podUID, history.PodUID)
	}

	if history.PodName != podName {
		t.Errorf("Expected PodName %s, got %s", podName, history.PodName)
	}

	if history.Namespace != namespace {
		t.Errorf("Expected Namespace %s, got %s", namespace, history.Namespace)
	}

	if history.NodeName != nodeName {
		t.Errorf("Expected NodeName %s, got %s", nodeName, history.NodeName)
	}

	if !history.StartTime.Equal(now) {
		t.Errorf("Expected StartTime %v, got %v", now, history.StartTime)
	}

	if !history.LastSeen.Equal(now) {
		t.Errorf("Expected LastSeen %v, got %v", now, history.LastSeen)
	}

	// Add another record
	record2 := createTestRecord(now.Add(time.Minute))
	store.AddRecord(podUID, podName, namespace, nodeName, record2)

	// Verify second record was added
	history, _ = store.GetHistory(podUID)
	if len(history.Records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(history.Records))
	}

	if !history.LastSeen.Equal(now.Add(time.Minute)) {
		t.Errorf("Expected LastSeen to be updated")
	}
}

func TestInMemoryStore_MarkCompleted(t *testing.T) {
	store := newTestStore()
	defer store.Close()

	podUID := "test-pod-123"
	podName := "test-pod"
	namespace := "default"
	nodeName := "node-1"
	now := time.Now()

	// Add a record
	record := createTestRecord(now)
	store.AddRecord(podUID, podName, namespace, nodeName, record)

	// Mark pod as completed
	store.MarkCompleted(podUID)

	// Verify pod is marked as completed
	history, exists := store.GetHistory(podUID)
	if !exists {
		t.Fatal("Expected pod history to exist")
	}

	if !history.Completed {
		t.Error("Expected pod to be marked as completed")
	}

	// Try to add another record after marking as completed
	record2 := createTestRecord(now.Add(time.Minute))
	store.AddRecord(podUID, podName, namespace, nodeName, record2)

	// Verify no new record was added
	history, _ = store.GetHistory(podUID)
	if len(history.Records) != 1 {
		t.Errorf("Expected 1 record (new record should be ignored), got %d", len(history.Records))
	}
}

func TestInMemoryStore_Cleanup(t *testing.T) {
	// Create store with short retention time for testing
	store := NewInMemoryStore(
		time.Millisecond*10, // cleanupPeriod - very short for testing
		time.Millisecond*50, // retentionTime - very short for testing
		100,                 // maxRecordsPerPod
		nil,                 // no downsampling for this test
	)
	defer store.Close()

	podUID1 := "test-pod-1"
	podUID2 := "test-pod-2"
	now := time.Now()

	// Add records for two pods
	store.AddRecord(podUID1, "pod1", "default", "node1", createTestRecord(now))
	store.AddRecord(podUID2, "pod2", "default", "node1", createTestRecord(now))

	// Mark first pod as completed
	store.MarkCompleted(podUID1)

	// Wait for retention time to pass
	time.Sleep(time.Millisecond * 100)

	// Manually trigger cleanup
	store.Cleanup()

	// Verify completed pod was removed, but active pod remains
	_, exists1 := store.GetHistory(podUID1)
	_, exists2 := store.GetHistory(podUID2)

	if exists1 {
		t.Error("Expected completed pod to be removed after cleanup")
	}

	if !exists2 {
		t.Error("Expected active pod to remain after cleanup")
	}
}

func TestInMemoryStore_Downsampling(t *testing.T) {
	// Create store with small maxRecordsPerPod to test downsampling
	store := NewInMemoryStore(
		time.Hour,           // cleanupPeriod
		time.Hour,           // retentionTime
		5,                   // maxRecordsPerPod - very small to trigger downsampling
		&LTTBDownsampling{}, // use LTTB downsampling
	)
	defer store.Close()

	podUID := "test-pod-123"
	podName := "test-pod"
	namespace := "default"
	nodeName := "node-1"
	now := time.Now()

	// Add records up to maxRecordsPerPod
	for i := 0; i < 5; i++ {
		record := createTestRecord(now.Add(time.Duration(i) * time.Minute))
		record.CPU = float64(i) * 0.2 // Make CPU values different
		store.AddRecord(podUID, podName, namespace, nodeName, record)
	}

	// Verify we have exactly maxRecordsPerPod records
	history, _ := store.GetHistory(podUID)
	if len(history.Records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(history.Records))
	}

	// Add one more record to trigger downsampling
	extraRecord := createTestRecord(now.Add(6 * time.Minute))
	extraRecord.CPU = 1.5
	store.AddRecord(podUID, podName, namespace, nodeName, extraRecord)

	// Verify downsampling occurred (should have fewer than 6 records)
	history, _ = store.GetHistory(podUID)
	if len(history.Records) > 5 {
		t.Errorf("Expected downsampling to keep records under limit, got %d records", len(history.Records))
	}

	// Verify the most recent record was kept after downsampling
	lastRecord := history.Records[len(history.Records)-1]
	if !lastRecord.Timestamp.Equal(now.Add(6 * time.Minute)) {
		t.Error("Expected most recent record to be kept after downsampling")
	}
}

func TestInMemoryStore_Size(t *testing.T) {
	store := newTestStore()
	defer store.Close()

	// Initially empty
	if size := store.Size(); size != 0 {
		t.Errorf("Expected initial size 0, got %d", size)
	}

	// Add records for multiple pods
	store.AddRecord("pod1", "pod1", "default", "node1", createTestRecord(time.Now()))
	store.AddRecord("pod2", "pod2", "default", "node1", createTestRecord(time.Now()))
	store.AddRecord("pod3", "pod3", "default", "node1", createTestRecord(time.Now()))

	// Verify size is updated
	if size := store.Size(); size != 3 {
		t.Errorf("Expected size 3, got %d", size)
	}

	// Mark one pod as completed and run cleanup
	store.MarkCompleted("pod1")
	// Manually force retention time to pass for the test
	store.data["pod1"].LastSeen = time.Now().Add(-2 * store.retentionTime)
	store.Cleanup()

	// Verify size is updated after cleanup
	if size := store.Size(); size != 2 {
		t.Errorf("Expected size 2 after cleanup, got %d", size)
	}
}

func TestInMemoryStore_CleanupWorker(t *testing.T) {
	// This test verifies the cleanup worker goroutine functions correctly
	store := NewInMemoryStore(
		time.Millisecond*50, // cleanupPeriod - very short for testing
		time.Millisecond*50, // retentionTime - very short for testing
		100,                 // maxRecordsPerPod
		nil,                 // no downsampling for this test
	)

	podUID := "test-pod-cleanup"
	store.AddRecord(podUID, "pod", "default", "node1", createTestRecord(time.Now()))
	store.MarkCompleted(podUID)

	// Force last seen time to be in the past
	store.mutex.Lock()
	store.data[podUID].LastSeen = time.Now().Add(-time.Second)
	store.mutex.Unlock()

	// Wait for cleanup worker to run at least once
	time.Sleep(time.Millisecond * 200)

	// Verify pod was cleaned up automatically
	_, exists := store.GetHistory(podUID)
	if exists {
		t.Error("Expected pod to be automatically cleaned up by worker")
	}

	// Clean up resources
	store.Close()
}

func TestCalculateTotalEnergy(t *testing.T) {
	now := time.Now()
	
	testCases := []struct {
		name           string
		records        []PodMetricsRecord
		expectedEnergy float64
	}{
		{
			name:           "EmptyRecords",
			records:        []PodMetricsRecord{},
			expectedEnergy: 0,
		},
		{
			name: "SingleRecord",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100},
			},
			expectedEnergy: 0, // Can't calculate energy with just one point
		},
		{
			name: "ConstantPower",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100},
				{Timestamp: now.Add(time.Hour), PowerEstimate: 100},
			},
			expectedEnergy: 0.1, // 100W for 1 hour = 0.1 kWh
		},
		{
			name: "VariablePower",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100},
				{Timestamp: now.Add(30 * time.Minute), PowerEstimate: 200},
				{Timestamp: now.Add(time.Hour), PowerEstimate: 300},
			},
			expectedEnergy: 0.2, // Trapezoidal integration: (100+200)/2 * 0.5h + (200+300)/2 * 0.5h = 0.2 kWh
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			energy := CalculateTotalEnergy(tc.records)
			
			// Use small epsilon for floating point comparison
			epsilon := 0.0001
			if abs(energy - tc.expectedEnergy) > epsilon {
				t.Errorf("Expected energy: %f kWh, got: %f kWh", tc.expectedEnergy, energy)
			}
		})
	}
}

func TestCalculateTotalCarbonEmissions(t *testing.T) {
	now := time.Now()
	
	testCases := []struct {
		name                string
		records             []PodMetricsRecord
		expectedEmissions   float64
	}{
		{
			name:                "EmptyRecords",
			records:             []PodMetricsRecord{},
			expectedEmissions:   0,
		},
		{
			name: "SingleRecord",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100, CarbonIntensity: 500},
			},
			expectedEmissions: 0, // Can't calculate with just one point
		},
		{
			name: "ConstantPowerAndIntensity",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100, CarbonIntensity: 500},
				{Timestamp: now.Add(time.Hour), PowerEstimate: 100, CarbonIntensity: 500},
			},
			expectedEmissions: 50, // 0.1 kWh * 500 gCO2eq/kWh = 50 gCO2eq
		},
		{
			name: "VariablePowerAndIntensity",
			records: []PodMetricsRecord{
				{Timestamp: now, PowerEstimate: 100, CarbonIntensity: 400},
				{Timestamp: now.Add(30 * time.Minute), PowerEstimate: 200, CarbonIntensity: 500},
				{Timestamp: now.Add(time.Hour), PowerEstimate: 300, CarbonIntensity: 600},
			},
			// First interval: (100+200)/2 * 0.5h = 0.075 kWh, avg intensity 450
			// Second interval: (200+300)/2 * 0.5h = 0.125 kWh, avg intensity 550
			// Total emissions: 0.075 * 450 + 0.125 * 550 = 102.5 gCO2eq
			expectedEmissions: 102.5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			emissions := CalculateTotalCarbonEmissions(tc.records)
			
			// Use small epsilon for floating point comparison
			epsilon := 0.0001
			if abs(emissions - tc.expectedEmissions) > epsilon {
				t.Errorf("Expected emissions: %f gCO2eq, got: %f gCO2eq", tc.expectedEmissions, emissions)
			}
		})
	}
}

// Helper function for floating point comparison
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}