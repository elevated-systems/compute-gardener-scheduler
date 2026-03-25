package dryrun

import (
	"testing"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

func TestCreateAnnotationPatches(t *testing.T) {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated:  "true",
		common.AnnotationDryRunWouldDelay: "true",
		common.AnnotationDryRunDelayType:  "carbon",
	}

	patches, err := createAnnotationPatches(annotations)
	if err != nil {
		t.Fatalf("Failed to create patches: %v", err)
	}

	if len(patches) != 3 {
		t.Errorf("Expected 3 patch operations, got %d", len(patches))
	}

	// Verify each operation has required fields
	for i, op := range patches {
		if op["op"] != "add" {
			t.Errorf("Operation %d: expected op 'add', got %v", i, op["op"])
		}
		if op["path"] == nil {
			t.Errorf("Operation %d: missing path", i)
		}
		if op["value"] == nil {
			t.Errorf("Operation %d: missing value", i)
		}
	}
}

func TestCreateAnnotationPatches_EmptyAnnotations(t *testing.T) {
	annotations := map[string]string{}

	patches, err := createAnnotationPatches(annotations)
	if err != nil {
		t.Fatalf("Failed to create patches: %v", err)
	}

	if len(patches) != 0 {
		t.Errorf("Expected 0 patch operations for empty annotations, got %d", len(patches))
	}
}

func TestCreateAnnotationPatches_SpecialCharacters(t *testing.T) {
	annotations := map[string]string{
		"compute-gardener-scheduler.kubernetes.io/dry-run-evaluated": "true",
	}

	patches, err := createAnnotationPatches(annotations)
	if err != nil {
		t.Fatalf("Failed to create patches: %v", err)
	}

	if len(patches) != 1 {
		t.Fatalf("Expected 1 patch operation, got %d", len(patches))
	}

	// The / in the annotation key should be escaped as ~1
	path := patches[0]["path"].(string)
	expectedPath := "/metadata/annotations/compute-gardener-scheduler.kubernetes.io~1dry-run-evaluated"
	if path != expectedPath {
		t.Errorf("Expected path %q, got %q", expectedPath, path)
	}
}

func TestCreateAnnotationPatches_NoDelay(t *testing.T) {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated:  "true",
		common.AnnotationDryRunWouldDelay: "false",
	}

	patches, err := createAnnotationPatches(annotations)
	if err != nil {
		t.Fatalf("Failed to create patches: %v", err)
	}

	if len(patches) != 2 {
		t.Errorf("Expected 2 patch operations, got %d", len(patches))
	}
}
