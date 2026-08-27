package dryrun

import (
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

// Filter mode constants
const (
	// FilterModeSchedulerName filters pods by schedulerName matching compute-gardener-scheduler.
	// Pods that match are evaluated and have their schedulerName mutated back to default-scheduler.
	FilterModeSchedulerName = "schedulerName"

	// FilterModeNamespace filters pods by explicit namespace list.
	// Empty list = watch nothing. Each namespace must be explicitly specified.
	FilterModeNamespace = "namespace"
)

// Config holds configuration for the dry-run system
type Config struct {
	Mode            string        // "metrics" or "annotate"
	FilterMode      string        // "schedulerName" (default) or "namespace"
	WatchNamespaces []string      // Namespaces to evaluate (only used in namespace filter mode)
	PendingTTL      time.Duration // How long an admitted pod may take to reach the informer before its claim expires
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

// DefaultPendingTTL bounds how long a pod admitted by the webhook may take to
// reach the completion controller's informer before its claim is discarded.
const DefaultPendingTTL = 2 * time.Minute

// PendingStore hands off "this pod opted in via schedulerName" from the webhook
// to the completion controller.
//
// The webhook cannot key on pod UID: the apiserver assigns metadata.uid after
// mutating admission runs, so at admission time there is no identifier the
// controller will later see. Pods are instead matched on the identity they do
// carry at admission (namespace, name or generateName, controlling owner), with
// admission times queued per key so N pods from one owner claim N times.
type PendingStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	pending map[string][]time.Time
}

// NewPendingStore creates a pending store. A ttl of zero uses DefaultPendingTTL.
func NewPendingStore(ttl time.Duration) *PendingStore {
	if ttl <= 0 {
		ttl = DefaultPendingTTL
	}
	return &PendingStore{
		ttl:     ttl,
		pending: make(map[string][]time.Time),
	}
}

// pendingKey builds the correlation key from the fields a pod carries both at
// admission and once persisted. Pods sharing a key come from the same owner and
// evaluate identically, so claiming any one of them yields the same result.
//
// generateName is preferred over name: a pod created from a template has no name
// until the apiserver generates one after admission, but keeps its generateName
// once persisted. Only explicitly named pods fall back to name.
func pendingKey(pod *corev1.Pod) string {
	name := pod.GenerateName
	if name == "" {
		name = pod.Name
	}

	var ownerUID string
	for _, ref := range pod.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			ownerUID = string(ref.UID)
			break
		}
	}

	return fmt.Sprintf("%s/%s/%s", pod.Namespace, name, ownerUID)
}

// Record marks a pod as admitted and awaiting arrival in the informer.
func (s *PendingStore) Record(pod *corev1.Pod, now time.Time) {
	key := pendingKey(pod)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[key] = append(s.pending[key], now)
}

// Claim consumes one admission for the pod's key, reporting whether the pod was
// admitted through the webhook. Expired admissions are dropped rather than
// claimed.
func (s *PendingStore) Claim(pod *corev1.Pod, now time.Time) bool {
	key := pendingKey(pod)

	s.mu.Lock()
	defer s.mu.Unlock()

	queue := s.pending[key]
	for i, recordedAt := range queue {
		if now.Sub(recordedAt) > s.ttl {
			continue
		}
		s.setQueue(key, queue[i+1:])
		return true
	}

	// Every admission under this key had expired
	if len(queue) > 0 {
		s.setQueue(key, nil)
	}
	return false
}

// Sweep discards admissions that no pod ever claimed, which happens when a pod
// is rejected after mutating admission by quota or a validating webhook.
func (s *PendingStore) Sweep(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, queue := range s.pending {
		kept := queue[:0]
		for _, recordedAt := range queue {
			if now.Sub(recordedAt) <= s.ttl {
				kept = append(kept, recordedAt)
			}
		}
		s.setQueue(key, kept)
	}
}

// Count returns the number of admissions awaiting a claim.
func (s *PendingStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := 0
	for _, queue := range s.pending {
		total += len(queue)
	}
	return total
}

// setQueue stores a key's remaining admissions, removing the key when empty so
// keys from short-lived owners do not accumulate. Callers must hold s.mu.
func (s *PendingStore) setQueue(key string, queue []time.Time) {
	if len(queue) == 0 {
		delete(s.pending, key)
		return
	}
	s.pending[key] = queue
}
