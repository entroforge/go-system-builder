package workspace

import (
	"encoding/json"
	"testing"
)

func TestExecutionEmbeddingAndDecode(t *testing.T) {
	var envelope struct {
		Execution
		Mode string `json:"context_mode"`
	}
	if err := json.Unmarshal([]byte(`{"assignment_id":"a","context_mode":"enabled"}`), &envelope); err != nil || envelope.AssignmentID != "a" || envelope.Mode != "enabled" {
		t.Fatalf("embedded public execution changed JSON decoding: %+v %v", envelope, err)
	}
	e, err := DecodeExecution([]byte(`{"assignment_id":"a","inputs":{"doc.md":"doc.md"}}`))
	if err != nil || e.AssignmentID != "a" || e.Inputs["doc.md"] != "doc.md" {
		t.Fatalf("execution decoding: %+v %v", e, err)
	}
}

func TestRegistryEmbeddingAndDecode(t *testing.T) {
	var envelope struct {
		ExecutionRegistry
		Mode string `json:"context_mode"`
	}
	if err := json.Unmarshal([]byte(`{"main_root":"/main","context_mode":"enabled"}`), &envelope); err != nil || envelope.MainRoot != "/main" || envelope.Mode != "enabled" {
		t.Fatalf("embedded public registry changed JSON decoding: %+v %v", envelope, err)
	}
	r, err := DecodeRegistry([]byte(`{"main_root":"/main","executions":{"a":{"assignment_id":"a"}}}`))
	if err != nil || r.MainRoot != "/main" || r.Executions["a"].AssignmentID != "a" {
		t.Fatalf("registry decoding: %+v %v", r, err)
	}
}
