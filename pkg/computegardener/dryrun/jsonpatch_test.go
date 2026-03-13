package dryrun

import (
	"encoding/json"
	"testing"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

func TestCreateJSONPatch(t *testing.T) {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated:  "true",
		common.AnnotationDryRunWouldDelay: "true",
		common.AnnotationDryRunDelayType:  "carbon",
	}

	patch, err := createJSONPatch(annotations)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	if patch == nil {
		t.Fatal("Expected patch to be non-nil")
	}

	// Verify patch is valid JSON array
	var rawPatch []map[string]interface{}
	if err := json.Unmarshal(patch, &rawPatch); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	if len(rawPatch) != 3 {
		t.Errorf("Expected 3 patch operations, got %d", len(rawPatch))
	}

	// Verify each operation has required fields
	for i, op := range rawPatch {
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

func TestCreateJSONPatch_EmptyAnnotations(t *testing.T) {
	annotations := map[string]string{}

	patch, err := createJSONPatch(annotations)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	// Empty annotations should produce null/empty JSON array
	var rawPatch []map[string]interface{}
	if err := json.Unmarshal(patch, &rawPatch); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	if len(rawPatch) != 0 {
		t.Errorf("Expected 0 patch operations for empty annotations, got %d", len(rawPatch))
	}
}

func TestCreateJSONPatch_SpecialCharacters(t *testing.T) {
	annotations := map[string]string{
		"compute-gardener-scheduler.kubernetes.io/dry-run-evaluated": "true",
	}

	patch, err := createJSONPatch(annotations)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	var rawPatch []map[string]interface{}
	if err := json.Unmarshal(patch, &rawPatch); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	if len(rawPatch) != 1 {
		t.Fatalf("Expected 1 patch operation, got %d", len(rawPatch))
	}

	// The / in the annotation key should be escaped as ~1
	path := rawPatch[0]["path"].(string)
	expectedPath := "/metadata/annotations/compute-gardener-scheduler.kubernetes.io~1dry-run-evaluated"
	if path != expectedPath {
		t.Errorf("Expected path %q, got %q", expectedPath, path)
	}
}

func TestCreateJSONPatch_NoDelay(t *testing.T) {
	annotations := map[string]string{
		common.AnnotationDryRunEvaluated:  "true",
		common.AnnotationDryRunWouldDelay: "false",
	}

	patch, err := createJSONPatch(annotations)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	if patch == nil {
		t.Fatal("Expected patch to be non-nil even when no delay")
	}

	var rawPatch []map[string]interface{}
	if err := json.Unmarshal(patch, &rawPatch); err != nil {
		t.Fatalf("Failed to unmarshal patch: %v", err)
	}

	if len(rawPatch) != 2 {
		t.Errorf("Expected 2 patch operations, got %d", len(rawPatch))
	}
}
