package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestImpactOriginalAcceptsPathWithoutSHAAndPreservesNotes(t *testing.T) {
	var request ChangeImpactRequest
	input := `{"impact_id":"impact-original","runtime_id":"loop-REQ-001","req_id":"REQ-001","baseline_generation":1,"source_case_ids":["case-1"],"change_types":[],"changed_artifacts":["internal/example.go",{"path":"docs/example.md"}],"decisions":[],"integration_notes":"Merged the contribution; checks are pending.","analyzed_by":"Main"}`
	if err := DecodeOriginalChangeImpact([]byte(input), &request); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	record, ref, err := CreateChangeImpact(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.ChangedArtifacts) != 2 || record.ChangedArtifacts[0].Path != "internal/example.go" || record.IntegrationNotes != "Merged the contribution; checks are pending." {
		t.Fatalf("unexpected impact content: %+v", record)
	}
	data, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	var stored ChangeImpact
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.ChangedArtifacts) != 2 || stored.ChangedArtifacts[1].Path != "docs/example.md" || stored.IntegrationNotes != request.IntegrationNotes {
		t.Fatalf("stored impact lost original expression: %+v", stored)
	}
}

func TestImpactOriginalDecodeKeepsProfessionalFormatsAndJSONComposition(t *testing.T) {
	input := []byte(`{"changed_artifacts":["src/a.go",{"path":"src/b.go"}],"decisions":[],"integration_notes":"keep"}`)
	original := append([]byte(nil), input...)
	var request ChangeImpactRequest
	if err := DecodeOriginalChangeImpact(input, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.ChangedArtifacts) != 2 || request.ChangedArtifacts[0].Path != "src/a.go" || request.ChangedArtifacts[1].Path != "src/b.go" || request.IntegrationNotes != "keep" {
		t.Fatalf("original professional input lost: %+v", request)
	}
	if string(input) != string(original) {
		t.Fatal("original answer was rewritten")
	}
	var outer struct {
		ChangeImpactRequest
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"extra":"kept"}`), &outer); err != nil || outer.Extra != "kept" {
		t.Fatalf("embedded DTO swallowed the outer field: %+v, %v", outer, err)
	}
}
