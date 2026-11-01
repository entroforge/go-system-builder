package semantic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskReadingReferenceIgnoresHistoricalSHAValue(t *testing.T) {
	for _, old := range []string{"null", `"old"`, "42", `{"legacy":true}`} {
		var values []TaskContextSummary
		data := `[{"task_id":"T","reading":[{"source":"contract","path":"docs/dev/contracts/BE-001.md","mode":"required","condition_unknown":false,"status":"readable","bytes":8,"sha256":` + old + `}]}]`
		if err := json.Unmarshal([]byte(data), &values); err != nil {
			t.Fatalf("%s: %v", old, err)
		}
		value := values[0].Reading[0]
		if value.Path != "docs/dev/contracts/BE-001.md" || value.Mode != "required" || value.Status != "readable" {
			t.Fatalf("%s: actual reading not preserved: %+v", old, value)
		}
	}
	var embedded struct {
		TaskReadingReference
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"extra":"kept"}`), &embedded); err != nil || embedded.Extra != "kept" {
		t.Fatalf("outer field lost: %+v, %v", embedded, err)
	}
}

func TestOriginalBehaviorProjectionKeepsProfessionalSameNamedInput(t *testing.T) {
	data := []byte(`[{"task_id":"T","example":{"inputs":{"sha256":{"professional":true}},"sources":[{"path":"docs/source","sha256":{"legacy":true}}]}}]`)
	var rows []TaskBehaviorProjection
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Example == nil || rows[0].Example.Sources[0].Path != "docs/source" {
		t.Fatalf("original source lost: %+v", rows)
	}
	if _, ok := rows[0].Example.Inputs["sha256"].(map[string]any); !ok {
		t.Fatalf("professional same-named input lost: %+v", rows[0].Example.Inputs)
	}
}

func TestContractRequirementReadsOriginalIdentityWithUnrelatedFields(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"lifecycle":{"state":"planning"},"bound_req":{"id":"REQ-001","path":"docs/requirements/REQ-001.md","version":"v1","sha256":{"historical":true},"note":"preserved"}}`)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := contractRequirement(root, "REQ-001")
	if err != nil || ref.ID != "REQ-001" || ref.Path != "docs/requirements/REQ-001.md" || ref.Version != "v1" {
		t.Fatalf("original REQ identity was not preserved: %+v %v", ref, err)
	}
}
