package repair_test

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionReplacementPreservesUnsubmittedWorkWithoutDigest(t *testing.T) {
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
			workBefore, _ := os.ReadFile(filepath.Join(root, "internal/api/payload.go"))
			oldSession, _ := os.ReadFile(filepath.Join(root, sessionRef.Path))
			snapshot, replacement, _, err := repair.OpenRepairSession(root, statePath, journal, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, SessionID: "repair-session-replacement", CreatedBy: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if replacement.SessionID != "repair-session-replacement" {
				t.Fatalf("wrong replacement: %q", replacement.SessionID)
			}
			pointer := snapshot.State["review"].(map[string]any)["repair"].(map[string]any)
			if pointer["session_id"] != replacement.SessionID || pointer["result_ref"] != nil {
				t.Fatalf("unexpected current responsibility or fabricated result: %#v", pointer)
			}
			workAfter, _ := os.ReadFile(filepath.Join(root, "internal/api/payload.go"))
			if string(workBefore) != string(workAfter) {
				t.Fatal("replacement modified unsubmitted work")
			}
			afterSession, _ := os.ReadFile(filepath.Join(root, sessionRef.Path))
			if string(oldSession) != string(afterSession) {
				t.Fatal("original session changed")
			}
		})
	}
}

func TestSessionRetryIgnoresHistoricalMachineMetadata(t *testing.T) {
	for _, value := range []any{nil, "not-a-digest", float64(123), map[string]any{"legacy": true}} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			root := req039fixtures.FreshRoot(t)
			state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
			ref, sha := writeRuntimeContract(t, root)
			state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": ref.Path, "repair_contract_sha256": sha}
			req039fixtures.WriteState(t, root, state)
			statePath, journal := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			if err := os.WriteFile(journal, nil, 0644); err != nil {
				t.Fatal(err)
			}
			request := repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, SessionID: "repair-session-original", CreatedBy: "main"}
			first, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journal, request)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, sessionRef.Path)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(original, &doc); err != nil {
				t.Fatal(err)
			}
			doc["contract_sha256"], doc["baseline_digest"] = value, value
			doc["baseline_artifacts"] = []any{map[string]any{"path": "internal/api/payload.go", "sha256": value}}
			original, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			replayed, session, gotRef, err := repair.OpenRepairSession(root, statePath, journal, request)
			if err != nil {
				t.Fatal(err)
			}
			if replayed.Revision != first.Revision || session.SessionID != request.SessionID || gotRef.Path != sessionRef.Path {
				t.Fatalf("retry changed original responsibility: %#v %#v", session, gotRef)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(original) {
				t.Fatalf("retry rewrote original session: %v", err)
			}
		})
	}
}

func TestRepairPlanReadsHistoricalContractMetadataWithoutChangingOriginal(t *testing.T) {
	root := t.TempDir()
	contract, _ := writeRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contract, SessionID: "repair-session-legacy-plan", RuntimeID: "loop-legacy-plan",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contract, Session: sessionRef, PlanID: "repair-plan-legacy", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, planRef.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{nil, "not-a-digest", float64(12), map[string]any{"old": true}} {
		doc["contract_sha256"] = value
		original, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, original, 0644); err != nil {
			t.Fatal(err)
		}
		plan, err := repair.ValidateRepairPlan(root, planRef)
		if err != nil || plan.PlanID != "repair-plan-legacy" || len(plan.Assignments) == 0 {
			t.Fatalf("old machine metadata changed Plan responsibility: %#v %v", plan, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(original) {
			t.Fatalf("validation rewrote original Plan: %v", err)
		}
	}
}
