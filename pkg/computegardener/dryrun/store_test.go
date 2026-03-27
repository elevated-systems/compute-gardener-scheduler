package dryrun

import (
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
)

func TestPodEvaluationStore_RecordAndGetStart(t *testing.T) {
	store := NewPodEvaluationStore()

	now := time.Now()
	startTime := now.Add(-5 * time.Hour)

	startData := &eval.PodStartData{
		UID:              "test-uid",
		Namespace:        "default",
		StartTime:        startTime,
		EstimatedPowerW:  100.0,
		EstimatedRuntimeH: 4.0,
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.5,
		CarbonThreshold:  1.0,
		InitialPrice:     0.06,
		PriceThreshold:   0.05,
	}

	store.RecordStart("test-uid", startData)

	// Test successful retrieval
	got, found := store.GetStart("test-uid")
	if !found {
		t.Fatal("Expected to find start data")
	}

	if got.UID != "test-uid" {
		t.Errorf("Expected UID 'test-uid', got %s", got.UID)
	}
	if got.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got %s", got.Namespace)
	}
	if got.EstimatedPowerW != 100.0 {
		t.Errorf("Expected power 100.0, got %f", got.EstimatedPowerW)
	}
	if got.DelayType != "carbon" {
		t.Errorf("Expected delay type 'carbon', got %s", got.DelayType)
	}

	// Test non-existing
	_, found = store.GetStart("non-existing")
	if found {
		t.Error("Expected not to find start data for non-existing pod")
	}
}

func TestPodEvaluationStore_Remove(t *testing.T) {
	store := NewPodEvaluationStore()

	now := time.Now()
	startTime := now.Add(-5 * time.Hour)

	startData := &eval.PodStartData{
		UID:              "test-uid",
		Namespace:        "default",
		StartTime:        startTime,
		EstimatedPowerW:  100.0,
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.5,
		CarbonThreshold:  1.0,
		InitialPrice:     0.06,
		PriceThreshold:   0.05,
	}
	store.RecordStart("test-uid", startData)

	// Remove
	store.Remove("test-uid")

	// Verify deletion
	_, found := store.GetStart("test-uid")
	if found {
		t.Error("Expected pod to be deleted")
	}
}

func TestPodEvaluationStore_Count(t *testing.T) {
	store := NewPodEvaluationStore()

	if store.Count() != 0 {
		t.Errorf("Expected 0 entries, got %d", store.Count())
	}

	now := time.Now()

	// Add multiple pods
	store.RecordStart("pod-1", &eval.PodStartData{
		UID:              "pod-1",
		Namespace:        "default",
		StartTime:        now,
		WouldHaveDelayed: true,
		DelayType:        "carbon",
	})

	store.RecordStart("pod-2", &eval.PodStartData{
		UID:              "pod-2",
		Namespace:        "staging",
		StartTime:        now.Add(-3 * time.Hour),
		WouldHaveDelayed: false,
	})

	if store.Count() != 2 {
		t.Errorf("Expected 2 entries, got %d", store.Count())
	}

	// Remove one
	store.Remove("pod-1")

	if store.Count() != 1 {
		t.Errorf("Expected 1 entry after removal, got %d", store.Count())
	}
}

func TestPodEvaluationStore_Overwrite(t *testing.T) {
	store := NewPodEvaluationStore()

	now := time.Now()

	// Store initial data
	store.RecordStart("test-uid", &eval.PodStartData{
		UID:              "test-uid",
		Namespace:        "default",
		StartTime:        now.Add(-5 * time.Hour),
		WouldHaveDelayed: true,
		DelayType:        "carbon",
		InitialCarbon:    1.5,
	})

	// Overwrite with updated data
	updatedTime := now.Add(-2 * time.Hour)
	store.RecordStart("test-uid", &eval.PodStartData{
		UID:              "test-uid",
		Namespace:        "default",
		StartTime:        updatedTime,
		WouldHaveDelayed: true,
		DelayType:        "price",
		InitialCarbon:    0.8,
	})

	got, found := store.GetStart("test-uid")
	if !found {
		t.Fatal("Expected to find start data")
	}

	if got.StartTime != updatedTime {
		t.Errorf("Expected updated start time, got %v", got.StartTime)
	}
	if got.DelayType != "price" {
		t.Errorf("Expected delay type 'price', got %s", got.DelayType)
	}

	// Count should still be 1
	if store.Count() != 1 {
		t.Errorf("Expected 1 entry, got %d", store.Count())
	}
}

func TestPodEvaluationStore_RemoveNonExistent(t *testing.T) {
	store := NewPodEvaluationStore()

	// Removing a non-existent key should not panic
	store.Remove("does-not-exist")

	if store.Count() != 0 {
		t.Errorf("Expected 0 entries, got %d", store.Count())
	}
}
