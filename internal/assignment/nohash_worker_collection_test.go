package assignment

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestCollectOriginalWorkerCheckFactsWithoutDigestOrOID(t *testing.T) {
	root := t.TempDir()
	profile := checkexec.Profile{ID: "worker-check", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 5, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	record := assignmentrecord.Record{RuntimeID: "loop-facts", BaselineGeneration: 1, AssignmentID: "assignment-facts", Revision: 1, OwnerAgentID: "worker-facts", ClosingContract: &assignmentrecord.ClosingContract{WorkerChecks: []assignmentrecord.WorkerCheck{{Index: 0, Command: "true", Profile: profile}}}}
	binding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: record.OwnerAgentID, Responsibility: "owned_check"}
	observation, err := checkexec.ObserveProfile(context.Background(), root, profile)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude/evidence/workspace-checks/check-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "output.log")
	if err := os.WriteFile(log, []byte("actual output"), 0600); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	receipt, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": "true", "status": "pass", "output": log, "check_binding": binding, "profile_observation": observation, "check_index": 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, receipt, 0600); err != nil {
		t.Fatal(err)
	}
	captured := checkexec.ToolReceipt{Path: receiptPath, Checkout: root, Command: "true", Binding: binding}
	row := NativeOwnedCheckReceipt{CheckIndex: 0, Record: record.Reference(), RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, AssignmentID: record.AssignmentID, OwnerAgentID: record.OwnerAgentID, Checkout: root, ToolReceipt: captured}
	state := map[string]any{"entities": map[string]any{"agents": []any{map[string]any{"id": record.OwnerAgentID, "owned_check_receipts": []any{row}}}}}
	facts, _, err := CollectWorkerCheckFacts(context.Background(), root, state, record)
	if err != nil || len(facts) != 1 || facts[0].Status != "pass" || facts[0].OriginalReceipt == nil {
		t.Fatalf("actual receipt missing: %+v, %v", facts, err)
	}
	data, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	var output []map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"assignment_record_sha256", "profile_digest", "source_head", "source_snapshot_sha256"} {
		if _, ok := output[0][key]; ok {
			t.Fatalf("new fact retained %s", key)
		}
	}
	original := output[0]["original_receipt"].(map[string]any)
	for _, key := range []string{"sha256", "log_sha256", "profile_digest"} {
		if _, ok := original[key]; ok {
			t.Fatalf("new receipt retained %s", key)
		}
	}
	row.OwnerAgentID = "other"
	state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["owned_check_receipts"] = []any{row}
	facts, _, err = CollectWorkerCheckFacts(context.Background(), root, state, record)
	if err != nil || facts[0].Status == "pass" {
		t.Fatal("another responsibility was treated as passing")
	}
}
