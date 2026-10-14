package runtime_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestOperationReplayReturnsOriginalReceiptWithoutApply(t *testing.T) {
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	store := testWriter(sp, jp)
	m := artifactMutation(".claude/evidence/operation.json")
	m.Operation = &runtime.Operation{ID: "submission-1", InputSHA256: sha256HexForTest([]byte("canonical-input")), RuntimeID: "loop-test", Actor: m.Actor, Kind: m.TransitionID}
	first, err := store.Update(1, m)
	if err != nil || first.Operation == nil {
		t.Fatalf("first commit: %+v %v", first, err)
	}
	journal := mustRead(t, jp)
	artifact := mustRead(t, filepath.Join(root, m.Artifacts[0].Path))
	m.Apply = func(map[string]any) error { t.Fatal("replay ran business mutation twice"); return nil }
	replay, err := store.Update(1, m) // original CAS is deliberately stale
	if err != nil {
		t.Fatal(err)
	}
	firstReceipt, _ := json.Marshal(first.Operation)
	replayedReceipt, _ := json.Marshal(replay.Operation)
	if !bytes.Equal(firstReceipt, replayedReceipt) {
		t.Fatalf("retry replaced original receipt: %s / %s", firstReceipt, replayedReceipt)
	}
	if !bytes.Equal(journal, mustRead(t, jp)) || !bytes.Equal(artifact, mustRead(t, filepath.Join(root, m.Artifacts[0].Path))) {
		t.Fatal("retry rewrote durable evidence")
	}
	if _, err := store.Update(2, runtime.Mutation{EventID: "evt-after", TransitionID: "TEST-AFTER", Event: "after", Actor: "orchestrator", IdempotencyKey: "after"}); err != nil {
		t.Fatal(err)
	}
	later, found, err := store.LookupOperation(*m.Operation)
	if err != nil || !found || later.Revision != 3 || later.Operation.Revision != 2 {
		t.Fatalf("lost original receipt after later commit: %+v %v %v", later, found, err)
	}
	for _, field := range []string{"input", "actor", "kind"} {
		changed := *m.Operation
		switch field {
		case "input":
			changed.InputSHA256 = sha256HexForTest([]byte("different"))
		case "actor":
			changed.Actor = "other"
		case "kind":
			changed.Kind = "OTHER"
		}
		if _, _, err := store.LookupOperation(changed); !errors.Is(err, runtime.ErrOperationConflict) {
			t.Fatalf("changed %s accepted: %v", field, err)
		}
	}
}

func TestOperationReceiptCannotBeInjectedAsOrdinaryAction(t *testing.T) {
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	m := artifactMutation(".claude/evidence/operation.json")
	m.ActionResults = []map[string]any{{"id": "runtime:operation-receipt:v1", "result": "committed", "detail": "{}"}}
	before := mustRead(t, sp)
	if _, err := testWriter(sp, jp).Update(1, m); err == nil {
		t.Fatal("caller manufactured a durable operation receipt")
	}
	if !bytes.Equal(before, mustRead(t, sp)) {
		t.Fatal("rejected spoof changed state")
	}
}
