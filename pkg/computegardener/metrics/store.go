package metrics

import (
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// InMemoryStore implements PodMetricsStorage interface with in-memory storage
type InMemoryStore struct {
	data            map[string]*PodMetricsHistory // key: PodUID
	mutex           sync.RWMutex
	cleanupPeriod   time.Duration
	retentionTime   time.Duration  // How long to keep completed pod metrics
	maxRecordsPerPod int           // Maximum records per pod to prevent unbounded memory growth
	stopCh          chan struct{}
	downsamplingStrategy DownsamplingStrategy
}

// NewInMemoryStore creates a new metrics store with in-memory implementation
func NewInMemoryStore(
	cleanupPeriod time.Duration,
	retentionTime time.Duration,
	maxRecordsPerPod int,
	downsamplingStrategy DownsamplingStrategy,
) *InMemoryStore {
	store := &InMemoryStore{
		data:            make(map[string]*PodMetricsHistory),
		cleanupPeriod:   cleanupPeriod,
		retentionTime:   retentionTime,
		maxRecordsPerPod: maxRecordsPerPod,
		stopCh:          make(chan struct{}),
		downsamplingStrategy: downsamplingStrategy,
	}

	// Start cleanup goroutine
	go store.cleanupWorker()

	return store
}

// AddRecord adds a new metrics record for a pod
func (s *InMemoryStore) AddRecord(podUID, podName, namespace, nodeName string, record PodMetricsRecord) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	history, exists := s.data[podUID]
	if !exists {
		history = &PodMetricsHistory{
			PodUID:     podUID,
			PodName:    podName,
			Namespace:  namespace,
			NodeName:   nodeName,
			Records:    make([]PodMetricsRecord, 0, 10),
			StartTime:  record.Timestamp,
			MaxRecords: s.maxRecordsPerPod,
		}
		s.data[podUID] = history
	}
	
	// If pod is marked as completed, don't add more records
	if history.Completed {
		return
	}
	
	// Add the new record
	history.Records = append(history.Records, record)
	history.LastSeen = record.Timestamp
	
	// If we've exceeded max records, use downsampling
	if len(history.Records) > history.MaxRecords {
		if s.downsamplingStrategy != nil {
			// Use provided downsampling strategy
			targetCount := int(float64(history.MaxRecords) * 0.8) // Target 80% of max to avoid constant downsampling
			history.Records = s.downsamplingStrategy.Downsample(history.Records, targetCount)
		} else {
			// Simple strategy: drop oldest record
			history.Records = history.Records[1:]
		}
	}
}

// MarkCompleted marks a pod as completed to prevent further metrics collection
func (s *InMemoryStore) MarkCompleted(podUID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if history, exists := s.data[podUID]; exists {
		history.Completed = true
		klog.V(2).InfoS("Marked pod metrics as completed", 
			"podUID", podUID, 
			"podName", history.PodName,
			"recordCount", len(history.Records))
	}
}

// GetHistory retrieves the full metrics history for a pod
func (s *InMemoryStore) GetHistory(podUID string) (*PodMetricsHistory, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	history, exists := s.data[podUID]
	return history, exists
}

// Cleanup removes old completed pod data
func (s *InMemoryStore) Cleanup() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	now := time.Now()
	removedCount := 0
	
	for podUID, history := range s.data {
		if history.Completed && now.Sub(history.LastSeen) > s.retentionTime {
			delete(s.data, podUID)
			removedCount++
		}
	}
	
	if removedCount > 0 {
		klog.V(2).InfoS("Removed expired pod metrics", "count", removedCount)
	}
}

// cleanupWorker runs periodic cleanup
func (s *InMemoryStore) cleanupWorker() {
	ticker := time.NewTicker(s.cleanupPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.Cleanup()
		}
	}
}

// Close releases resources
func (s *InMemoryStore) Close() {
	close(s.stopCh)
}

// Size returns the number of pods being tracked
func (s *InMemoryStore) Size() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.data)
}