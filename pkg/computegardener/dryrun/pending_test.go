package dryrun

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func ownedPod(name, generateName, namespace, ownerUID string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: generateName,
			Namespace:    namespace,
		},
	}
	if ownerUID != "" {
		controller := true
		pod.OwnerReferences = []metav1.OwnerReference{{
			Kind:       "ReplicaSet",
			Name:       "owner",
			UID:        types.UID(ownerUID),
			Controller: &controller,
		}}
	}
	return pod
}

func TestPendingStore_ClaimMatchesAcrossAdmission(t *testing.T) {
	store := NewPendingStore(0)
	now := time.Now()

	// At admission the pod has a generateName and no UID
	admitted := ownedPod("", "worker-", "default", "owner-uid")
	store.Record(admitted, now)

	// Once persisted it has a generated name and a UID
	persisted := ownedPod("worker-abc12", "worker-", "default", "owner-uid")
	persisted.UID = "assigned-uid"

	if !store.Claim(persisted, now) {
		t.Fatal("Expected persisted pod to claim its admission")
	}
	if store.Count() != 0 {
		t.Errorf("Expected claim to consume the admission, %d left", store.Count())
	}
}

// TestPendingStore_ClaimIsOncePerAdmission covers N pods created by one owner:
// each admission is claimable exactly once.
func TestPendingStore_ClaimIsOncePerAdmission(t *testing.T) {
	store := NewPendingStore(0)
	now := time.Now()

	admitted := ownedPod("", "worker-", "default", "owner-uid")
	store.Record(admitted, now)
	store.Record(admitted, now)

	for i := 0; i < 2; i++ {
		pod := ownedPod("worker-abc12", "worker-", "default", "owner-uid")
		if !store.Claim(pod, now) {
			t.Fatalf("Expected claim %d to succeed", i+1)
		}
	}

	third := ownedPod("worker-abc12", "worker-", "default", "owner-uid")
	if store.Claim(third, now) {
		t.Error("Expected a third pod to find nothing left to claim")
	}
}

func TestPendingStore_UnrelatedPodDoesNotClaim(t *testing.T) {
	store := NewPendingStore(0)
	now := time.Now()

	store.Record(ownedPod("", "worker-", "default", "owner-uid"), now)

	cases := map[string]*corev1.Pod{
		"different namespace": ownedPod("worker-abc12", "worker-", "production", "owner-uid"),
		"different owner":     ownedPod("worker-abc12", "worker-", "default", "other-owner"),
		"different name":      ownedPod("unrelated-xyz", "unrelated-", "default", "owner-uid"),
		"no owner":            ownedPod("worker-abc12", "worker-", "default", ""),
	}

	for name, pod := range cases {
		if store.Claim(pod, now) {
			t.Errorf("Expected pod with %s not to claim the admission", name)
		}
	}

	if store.Count() != 1 {
		t.Errorf("Expected the admission to still be pending, got %d", store.Count())
	}
}

func TestPendingStore_ExpiredAdmissionIsNotClaimed(t *testing.T) {
	store := NewPendingStore(time.Minute)
	admittedAt := time.Now()

	store.Record(ownedPod("", "worker-", "default", "owner-uid"), admittedAt)

	pod := ownedPod("worker-abc12", "worker-", "default", "owner-uid")
	if store.Claim(pod, admittedAt.Add(2*time.Minute)) {
		t.Error("Expected an expired admission not to be claimed")
	}
	if store.Count() != 0 {
		t.Error("Expected the expired admission to be discarded on claim")
	}
}

// TestPendingStore_SweepDiscardsUnclaimed covers pods rejected after mutating
// admission, which never reach the informer.
func TestPendingStore_SweepDiscardsUnclaimed(t *testing.T) {
	store := NewPendingStore(time.Minute)
	admittedAt := time.Now()

	store.Record(ownedPod("", "rejected-", "default", "owner-uid"), admittedAt)
	store.Record(ownedPod("", "kept-", "default", "owner-uid"), admittedAt.Add(2*time.Minute))

	store.Sweep(admittedAt.Add(90 * time.Second))

	if store.Count() != 1 {
		t.Fatalf("Expected 1 admission to survive the sweep, got %d", store.Count())
	}
	if store.Claim(ownedPod("kept-abc12", "kept-", "default", "owner-uid"), admittedAt.Add(2*time.Minute)) != true {
		t.Error("Expected the unexpired admission to survive")
	}
}
