package dryrun

import (
	"sync"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// Config holds configuration for the dry-run system
type Config struct {
	Mode            string   // "metrics" or "annotate"
	WatchNamespaces []string // Whitelist of namespaces to evaluate
	Carbon          CarbonConfig
	Pricing         PricingConfig
}

// CarbonConfig holds carbon-aware evaluation settings
type CarbonConfig struct {
	Enabled   bool
	Region    string
	Threshold float64
	APIKey    string
}

// PricingConfig holds price-aware evaluation settings
type PricingConfig struct {
	Enabled bool
	// TOU schedules would be loaded from ConfigMap or similar
}

// PodEvaluationStore stores pod start data for completion tracking
type PodEvaluationStore struct {
	mu   sync.RWMutex
	data map[string]*eval.PodStartData // keyed by pod UID
}

// NewPodEvaluationStore creates a new pod evaluation store
func NewPodEvaluationStore() *PodEvaluationStore {
	return &PodEvaluationStore{
		data: make(map[string]*eval.PodStartData),
	}
}

// RecordStart stores pod start data
func (s *PodEvaluationStore) RecordStart(uid string, data *eval.PodStartData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[uid] = data
}

// GetStart retrieves pod start data
func (s *PodEvaluationStore) GetStart(uid string) (*eval.PodStartData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, found := s.data[uid]
	return data, found
}

// Remove removes pod start data
func (s *PodEvaluationStore) Remove(uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, uid)
}

// Count returns the number of tracked pods
func (s *PodEvaluationStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}
