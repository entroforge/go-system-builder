package repair_test

import (
	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionReplacementCannotSwallowUnsubmittedRepair(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "unreported-code"}[changed], func(t *testing.T) {
			root := req039fixtures.FreshRoot(t)
			writeFile(t, root, "internal/api/payload.go", "package api\n")
			state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
			ref, sha := writeRuntimeContract(t, root)
			state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": ref.Path, "repair_contract_sha256": sha}
			req039fixtures.WriteState(t, root, state)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			journal := filepath.Join(root, ".claude/loop-events.jsonl")
			os.WriteFile(journal, nil, 0644)
			_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journal, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, SessionID: "repair-session-original", CreatedBy: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				writeFile(t, root, "internal/api/payload.go", "package api\n// repaired\n")
			}
			before, _ := os.ReadFile(statePath)
			oldSession, _ := os.ReadFile(filepath.Join(root, sessionRef.Path))
			_, _, _, err = repair.OpenRepairSession(root, statePath, journal, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, SessionID: "repair-session-replacement", CreatedBy: "main"})
			if changed {
				if err == nil || !strings.Contains(err.Error(), "implementation changes") {
					t.Fatalf("expected protected diff: %v", err)
				}
				after, _ := os.ReadFile(statePath)
				if string(after) != string(before) {
					t.Fatal("blocked replacement mutated state")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			afterSession, _ := os.ReadFile(filepath.Join(root, sessionRef.Path))
			if string(oldSession) != string(afterSession) {
				t.Fatal("original session changed")
			}
		})
	}
}
