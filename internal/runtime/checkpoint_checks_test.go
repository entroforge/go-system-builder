package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestCheckpointToolReceiptCannotInventReceptionOrForeignBinding(t *testing.T) {
	root := t.TempDir()
	if err := validateCheckpointToolReceipts(map[string]any{}, nil); err != nil {
		t.Fatal("legacy checkpoint changed", err)
	}
	p := checkexec.Profile{ID: "CP-OWN", Version: "v1", Command: "true"}
	b := checkexec.Binding{RuntimeID: "loop-checkpoint", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
	ref := checkexec.ToolReceipt{Path: filepath.Join(root, "evidence", "receipt.json"), SHA256: checkexec.Hash([]byte("receipt")), ProfileDigest: p.Digest(), Command: p.Command, Binding: b, Checkout: filepath.Join(root, "worker")}
	// A rebind updates the execution worktree_path, while the receipt retains
	// its canonical captured checkout from the original check run.
	e := toolExecution{RuntimeID: b.RuntimeID, AssignmentID: b.AssignmentID, Owner: b.Owner, Generation: 1, ExecutionGeneration: 1, Path: filepath.Join(root, "worker", "rebound"), Profiles: []checkexec.Profile{p}, Receipts: []checkexec.ToolReceipt{ref}}
	state := map[string]any{"workspace": map[string]any{"executions": map[string]toolExecution{"assignment": e}}}
	if err := validateCheckpointToolReceipts(state, nil); err == nil {
		t.Fatal("receipt file became reception without original journal")
	}
	key := "tool-check:" + ref.SHA256
	valid := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "WORKSPACE-CHECK", IdempotencyKey: key, EventID: key,
		RuntimeID: b.RuntimeID, BaselineGeneration: b.Generation,
		ActorID: b.Owner, ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, valid); err != nil {
		t.Fatalf("legacy workspace receipt event rejected: %v", err)
	}
	e.Receipts[0].Binding.Owner = "another"
	state["workspace"] = map[string]any{"executions": map[string]toolExecution{"assignment": e}}
	if err := validateCheckpointToolReceipts(state, nil); err == nil {
		t.Fatal("foreign owner preserved")
	}
}

func TestCheckpointWorkspaceOwnedReceiptRequiresItsOriginalWriterEvent(t *testing.T) {
	root := t.TempDir()
	profile := checkexec.Profile{ID: "WORKER-0", Version: "1.0.0", CheckIndex: 0, Command: "true", Location: "worker"}
	binding := checkexec.Binding{RuntimeID: "loop-checkpoint", Generation: 2, AssignmentID: "assignment-worker", Owner: "builder-worker", ExecutionGeneration: 3, Responsibility: "owned_check"}
	receipt := checkexec.ToolReceipt{Path: filepath.Join(root, "evidence", "receipt.json"), SHA256: checkexec.Hash([]byte("receipt")), LogSHA256: checkexec.Hash([]byte("log")), Checkout: filepath.Join(root, "worker"), Command: profile.Command, ProfileDigest: profile.Digest(), Binding: binding}
	execution := toolExecution{
		RuntimeID: binding.RuntimeID, AssignmentID: binding.AssignmentID, Owner: binding.Owner,
		Generation: binding.Generation, ExecutionGeneration: binding.ExecutionGeneration, Path: filepath.Join(root, "worker", "rebound"),
		WorkerChecks:  []toolWorkerCheck{{Index: 0, Command: profile.Command, Profile: profile}},
		OwnedReceipts: []toolOwnedCheckReceipt{{WorkerCheckIndex: 0, Receipt: receipt}},
	}
	state := map[string]any{"runtime_id": binding.RuntimeID, "workspace": map[string]any{"executions": map[string]toolExecution{binding.AssignmentID: execution}}}
	key := fmt.Sprintf("owned-check:0:%s", receipt.SHA256)
	if err := validateCheckpointToolReceipts(state, nil); err == nil {
		t.Fatal("workspace owned receipt was accepted without its original Writer event")
	}
	valid := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "WORKSPACE-OWNED-CHECK", IdempotencyKey: key, EventID: key,
		RuntimeID: binding.RuntimeID, BaselineGeneration: binding.Generation,
		ActorID: binding.Owner, ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, valid); err != nil {
		t.Fatalf("rebound workspace execution rejected its original captured receipt event: %v", err)
	}
	wrongEvent := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "WORKSPACE-OWNED-CHECK", IdempotencyKey: key, EventID: key + "-wrong",
		RuntimeID: binding.RuntimeID, BaselineGeneration: binding.Generation,
		ActorID: binding.Owner, ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, wrongEvent); err == nil {
		t.Fatal("rebound workspace execution accepted a journal event with the wrong receipt event id")
	}
	forged := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "WORKSPACE-CHECK", IdempotencyKey: key, EventID: key,
		RuntimeID: binding.RuntimeID, BaselineGeneration: binding.Generation,
		ActorID: binding.Owner, ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, forged); err == nil {
		t.Fatal("legacy workspace check event was accepted for a WorkerCheck receipt")
	}
}

func TestCheckpointNativeOwnedReceiptRequiresItsOriginalWriterEvent(t *testing.T) {
	root := t.TempDir()
	const runtimeID, assignmentID, owner = "loop-checkpoint", "assignment-native", "builder-native"
	const baseline, recordRevision = 2, 4
	profile := checkexec.Profile{ID: "WORKER-0", Version: "1.0.0", CheckIndex: 0, Command: "true", Location: "worker"}
	binding := checkexec.Binding{RuntimeID: runtimeID, Generation: baseline, AssignmentID: assignmentID, Owner: owner, ExecutionGeneration: 0, Responsibility: "owned_check"}
	receipt := checkexec.ToolReceipt{Path: filepath.Join(root, "evidence", "native-receipt.json"), SHA256: checkexec.Hash([]byte("native-receipt")), LogSHA256: checkexec.Hash([]byte("native-log")), Checkout: filepath.Join(root, "native-worker"), Command: profile.Command, ProfileDigest: profile.Digest(), Binding: binding}
	sha := func(value byte) string { return string(make([]byte, 0)) + fmt.Sprintf("%064x", value) }
	row := map[string]any{
		"check_index": 0, "command": profile.Command, "profile_digest": profile.Digest(),
		"record":        map[string]any{"runtime_id": runtimeID, "baseline_generation": baseline, "assignment_id": assignmentID, "assignment_revision": recordRevision},
		"record_sha256": sha('a'), "runtime_id": runtimeID, "baseline_generation": baseline,
		"assignment_id": assignmentID, "owner_agent_id": owner, "execution_generation": 0,
		"native_context_generation": 5, "native_context_ref": ".claude/assignment-context.json", "native_context_sha256": sha('b'),
		"activation_ref": ".claude/evidence/activation.json", "activation_sha256": sha('c'),
		"checkout": receipt.Checkout, "source_branch": "worker/native", "target_branch": "main",
		"source_head": "abcdef0123456789abcdef0123456789abcdef01", "source_snapshot_sha256": sha('d'),
		"tool_receipt": receipt,
	}
	state := map[string]any{"runtime_id": runtimeID, "entities": map[string]any{"agents": []any{map[string]any{"id": owner, "owned_check_receipts": []any{row}}}}}
	key := "native-worker-check:" + receipt.SHA256
	if err := validateCheckpointToolReceipts(state, nil); err == nil {
		t.Fatal("native owned receipt was accepted without its original Writer event")
	}
	valid := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "NATIVE-WORKER-CHECK", IdempotencyKey: key, EventID: key,
		RuntimeID: runtimeID, BaselineGeneration: baseline, ActorID: owner,
		ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, valid); err != nil {
		t.Fatalf("original native receipt event rejected: %v", err)
	}
	forged := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "WORKSPACE-OWNED-CHECK", IdempotencyKey: key, EventID: key,
		RuntimeID: runtimeID, BaselineGeneration: baseline, ActorID: owner,
		ActorType: "agent", Responsibility: "owned_check",
	})
	if err := validateCheckpointToolReceipts(state, forged); err == nil {
		t.Fatal("workspace receipt event was accepted for a native receipt index")
	}
}

func TestCheckpointResultSubmissionAliasRequiresItsOriginalWriterEvent(t *testing.T) {
	const runtimeID, assignmentID = "loop-checkpoint", "assignment-alias"
	const baseline, revision = 2, 4
	submissionKey := "message-alias-1"
	record := checkpointRecordReference{RuntimeID: runtimeID, BaselineGeneration: baseline, AssignmentID: assignmentID, Revision: revision}
	partition, err := json.Marshal(struct {
		Record checkpointRecordReference
		Key    string
	}{Record: record, Key: submissionKey})
	if err != nil {
		t.Fatal(err)
	}
	keyHash := sha256Hex(partition)
	binding := map[string]any{
		"record": record, "record_sha256": fmt.Sprintf("%064x", 'a'), "submission_key": submissionKey,
		"answer_digest": fmt.Sprintf("%064x", 'b'), "tool_closure_digest": fmt.Sprintf("%064x", 'c'),
		"result_ref": ".claude/evidence/accepted-result.json", "result_sha256": fmt.Sprintf("%064x", 'd'), "result_revision": 3,
	}
	state := map[string]any{"runtime_id": runtimeID, "entities": map[string]any{"agents": []any{map[string]any{"id": "builder-alias", "result_submission_bindings": []any{binding}}}}}
	proof := checkpointReceiptProof{
		TransitionID:   "BUILDER-RESULT-KEY",
		IdempotencyKey: fmt.Sprintf("runtime:s6-result-key:%s:g%d:a%d:%s", assignmentID, baseline, revision, keyHash),
		EventID:        "evt-result-alias-" + keyHash, RuntimeID: runtimeID, BaselineGeneration: baseline,
		ActorID: "orchestrator", ActorType: "orchestrator", Responsibility: "orchestrator",
	}
	if err := validateCheckpointToolReceipts(state, nil); err == nil {
		t.Fatal("Result submission alias was accepted without its original Writer event")
	}
	valid := checkpointTestJournal(t, proof)
	if err := validateCheckpointToolReceipts(state, valid); err != nil {
		t.Fatalf("original Result alias binding event rejected: %v", err)
	}
	forged := checkpointTestJournal(t, checkpointReceiptProof{
		TransitionID: "BUILDER-RESULT", IdempotencyKey: proof.IdempotencyKey, EventID: proof.EventID,
		RuntimeID: runtimeID, BaselineGeneration: baseline, ActorID: "orchestrator",
		ActorType: "orchestrator", Responsibility: "orchestrator",
	})
	if err := validateCheckpointToolReceipts(state, forged); err == nil {
		t.Fatal("Result acceptance event was accepted for an alias key binding")
	}
}

func checkpointTestJournal(t *testing.T, proof checkpointReceiptProof) []byte {
	t.Helper()
	event := buildJournalEvent(Mutation{
		EventID: proof.EventID, TransitionID: proof.TransitionID, Actor: proof.ActorID,
		IdempotencyKey: proof.IdempotencyKey, RuntimeID: proof.RuntimeID,
		BaselineGeneration: proof.BaselineGeneration, ProducerResponsibility: proof.Responsibility,
		From: map[string]any{"state": "building", "phase": "build"},
		To:   map[string]any{"state": "building", "phase": "build"}, Message: "checkpoint proof test event",
	}, proof.RuntimeID, 0, 1, 1, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateJournalEvent(event); err != nil {
		t.Fatalf("build test journal event: %v", err)
	}
	return append(data, '\n')
}
