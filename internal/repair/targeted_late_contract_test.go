package repair_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestLateOriginalTargetedPreservesNewStage(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "running", 0)
	state["review"].(map[string]any)["repair"] = map[string]any{
		"session_id": "repair-session-late", "case_id": "investigation-case-1", "contract_id": "repair-contract-1",
		"contract_ref": ".claude/review/investigation/contracts/repair-contract-1.json", "contract_sha256": strings.Repeat("a", 64),
		"path": ".claude/review/repair/sessions/repair-session-late.json", "sha256": strings.Repeat("b", 64), "revision": 1,
		"status": "closed", "targeted_reverification_refs": []string{}, "targeted_reverification_artifacts": []any{},
		"assignment_owners": map[string]any{"assignment-s9-qa-verifier": "qa"},
		"updated_at":        "2026-08-26T00:00:00Z", "next_action": "continue new S7",
	}
	fixtures.WriteState(t, root, state)
	value := repair.TargetedReverification{RuntimeID: "loop-req039-ct", BaselineGeneration: 1, PerformingAssignmentID: "assignment-s9-qa-verifier", Result: "fail"}
	ref := repair.ArtifactRef{Path: ".claude/review/repair/reverification/reverify-late.json"}
	snapshot, err := repair.CommitTargetedReverification(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.CommitTargetedRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "qa"}, Reverification: ref, Record: &value,
	})
	if err != nil {
		t.Fatal(err)
	}
	life := snapshot.State["lifecycle"].(map[string]any)
	pointer := snapshot.State["review"].(map[string]any)["repair"].(map[string]any)
	if life["state"] != "verification" || life["phase"] != "running" || pointer["status"] != "closed" || pointer["next_action"] != "continue new S7" {
		t.Fatalf("late result rewound current responsibility: lifecycle=%v repair=%v", life, pointer)
	}
	rows := pointer["targeted_reverification_artifacts"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["result"] != "fail" {
		t.Fatalf("late original conclusion not retained: %v", rows)
	}
}
