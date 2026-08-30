package runtime_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestStoreRejectsSemanticInvalidCandidateWithoutValidator(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatalf("create journal: %v", err)
	}

	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatalf("read loop definition: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatalf("write loop definition: %v", err)
	}

	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read initial state: %v", err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read initial journal: %v", err)
	}

	var candidate map[string]any
	if err := json.Unmarshal(stateBefore, &candidate); err != nil {
		t.Fatalf("decode initial state: %v", err)
	}
	candidate["lifecycle"].(map[string]any)["phase"] = "invalid_phase"
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("encode candidate: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", candidateBytes); err != nil {
		t.Fatalf("candidate must remain schema-valid: %v", err)
	}
	if err := semantic.ValidateRuntimeBytes(root, candidateBytes); err == nil {
		t.Fatal("candidate must be rejected by semantic validation")
	}

	store := runtime.NewStore(statePath, journalPath)
	_, err = store.Update(1, runtime.Mutation{
		EventID:        "evt-semantic-invalid",
		TransitionID:   "transition-semantic-invalid",
		Event:          "task_updated",
		Actor:          "agent-test",
		IdempotencyKey: "idem-semantic-invalid",
		Apply: func(state map[string]any) error {
			state["lifecycle"].(map[string]any)["phase"] = "invalid_phase"
			return nil
		},
	})
	if err == nil {
		t.Fatal("schema-valid but semantic-invalid candidate committed without a validator")
	}

	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read final journal: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("state changed after semantic validation failure")
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatal("journal changed after semantic validation failure")
	}
}

func TestStoreSemanticValidationFailureLeavesStateJournalAndRevisionUnchanged(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	validatorErr := errors.New("semantic invariant rejected candidate")
	store := runtime.NewWriter(
		statePath,
		journalPath,
		dir,
		runtimeTestValidator{err: validatorErr},
	)
	_, err = store.Update(1, runtime.Mutation{
		EventID:        "evt-semantic-rejected",
		TransitionID:   "TR-SEMANTIC-REJECTED",
		Event:          "semantic_rejected",
		Actor:          "test",
		IdempotencyKey: "runtime:semantic-rejected:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-08-13T00:00:00Z"
			return nil
		},
	})
	if !errors.Is(err, validatorErr) {
		t.Fatalf("error = %v, want wrapped semantic validator error", err)
	}

	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("semantic validation failure changed state bytes")
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatal("semantic validation failure changed journal bytes")
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("semantic validation failure changed revision: got %d", snapshot.Revision)
	}
}

func testWriter(statePath, journalPath string) *runtime.Store {
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		panic(err)
	}
	definitionDir := filepath.Join(filepath.Dir(statePath), "docs")
	if err := os.MkdirAll(definitionDir, 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(definitionDir, "loop-definition.json"), definition, 0o644); err != nil {
		panic(err)
	}
	return runtime.NewWriter(
		statePath,
		journalPath,
		filepath.Dir(statePath),
		testCandidateValidator(),
	)
}

func testCandidateValidator() runtime.CandidateValidator {
	return runtimeTestValidator{}
}

type runtimeTestValidator struct {
	err error
}

func (v runtimeTestValidator) ValidateCandidate(_ string, state map[string]any) error {
	if state == nil || state["runtime_id"] == nil {
		return errors.New("test validator rejects empty probe")
	}
	if lifecycle, ok := state["lifecycle"].(map[string]any); ok && lifecycle["phase"] == "invalid_semantic_phase" {
		return errors.New("test validator rejects semantic probe")
	}
	return v.err
}

var _ runtime.CandidateValidator = runtimeTestValidator{}
