package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop(t *testing.T) {
	root, snapshot, plan, _ := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	snapshot = repairVerifierDispatchBuilder(t, root, plan.Assignments[0].AssignmentID, "source-guard-builder")
	division := repairVerifierDispatchPaths(root, snapshot.State)[0]
	repairVerifierDispatchSave(t, root, division, []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true}})
	snapshot = repairDispatchSnapshot(t, root)
	pointer := repairPointerCLI(snapshot.State)
	original := plan.Assignments[0].AssignmentID
	performing := repair.VerificationAssignmentID(original)
	owner := stringMapCLI(pointer["assignment_owners"])[performing]
	shared, err := repair.UsesSharedTargetedAuthoring(root, snapshot.State, original, performing, owner)
	if err != nil || !shared {
		t.Fatalf("original registered shared Targeted contract: %t %v", shared, err)
	}
	units := []repair.RepairUnitResult{}
	for _, id := range plan.Assignments[0].UnitIDs {
		units = append(units, repair.RepairUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-repair"}})
	}
	_, finalRef, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{
		Contract: repair.ContractRef{Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"])},
		Session:  repair.ArtifactRef{Path: stringValue(pointer["path"]), SHA256: stringValue(pointer["sha256"])},
		Plan:     repair.ArtifactRef{Path: stringValue(pointer["plan_ref"]), SHA256: stringValue(pointer["plan_sha256"])},
		ResultID: "repair-result-targeted-source-guard", ProducerAgentID: "source-guard-builder", AssignmentID: original,
		UnitResults: units, Result: "pass", ChangedArtifacts: []repair.ChangedArtifact{},
		BeforeFixChecks: []repair.RepairCheck{{Name: "synthetic pre-fix observation", Command: "test -f internal/api/one.go", Result: "fail", EvidenceRefs: []string{"test://synthetic-source-guard-red"}}},
		Checks:          []repair.RepairCheck{{Name: "synthetic post-fix observation", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-green"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pointer["result_refs"] = []any{map[string]any{"path": finalRef.Path, "sha256": finalRef.SHA256}}
	// Synthetic Impact establishes the current independent observation slots;
	// TASK/manifest source authority above comes from the actual dispatch path.
	impact := repair.ChangeImpactRequest{ImpactID: "impact-targeted-source-guard", RuntimeID: stringValue(snapshot.State["runtime_id"]), ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{stringValue(pointer["case_id"])}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts:          []repair.ArtifactRef{{ID: "changed-one", Path: "internal/api/one.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:                 []repair.ImpactDecision{{SourceID: "changed-one", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-ROOT", Decision: "reverify", Scope: []string{"internal/api/one.go"}, Rationale: "synthetic source observation", RecoveryEvidence: []string{"test://source-guard"}}},
		RequiredReverificationIDs: []string{"reverify-source-guard"}, AnalyzedBy: "Main"}
	_, impactRef, err := repair.CreateChangeImpact(root, impact)
	if err != nil {
		t.Fatal(err)
	}
	pointer["impact_ref"], pointer["impact_sha256"], pointer["status"] = impactRef.Path, impactRef.SHA256, "targeted_reverification"
	pointer["required_reverification_ids"] = []any{"reverify-source-guard"}
	mapFieldCLI(snapshot.State, "lifecycle")["phase"] = "targeted_reverification"
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	selections, err := repairTargetedSelections(root, snapshot.State)
	if err != nil || len(selections) != 1 {
		t.Fatalf("original exact Targeted selection: %+v %v", selections, err)
	}
	path, draft, err := prepareRepairTargetedQuestionnaire(root, snapshot.State, selections[0])
	if err != nil {
		t.Fatal(err)
	}
	draft.Result, draft.FailureClass = "blocked", "blocked"
	for i := range draft.AssertionResults {
		draft.AssertionResults[i].Result = "blocked"
		draft.AssertionResults[i].EvidenceRefs = []string{"test://synthetic-observed-environment-blocker"}
	}
	writeRepairLeadJSON(t, path, draft)
	originalBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(t.TempDir(), "isolated-targeted")
	repairLeadGit(t, root, "worktree", "add", "-b", "targeted-source-guard", worker, "HEAD")
	invoke := func(cwd string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-real-targeted-stop", "session_id": "targeted-parent", "cwd": cwd, "last_assistant_message": path})
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 {
			t.Fatalf("real native Stop: code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
		}
		return diagnostic.String()
	}
	before := repairDispatchRuntimeBytes(t, root)
	nested := filepath.Join(worker, "nested", "actual-stop-cwd")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{worker, nested} {
		if diagnostic := invoke(cwd); !strings.Contains(diagnostic, "no source binding for actual native worktree") || !strings.Contains(diagnostic, "no answer was inspected or received") {
			t.Fatalf("isolated Stop borrowed Main's original (cwd %s): %s", cwd, diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
	if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, originalBytes) {
		t.Fatal("source rejection inspected or rewrote the original answer", err)
	}
	if diagnostic := invoke(root); !strings.Contains(diagnostic, "committed through the original Runtime Writer") {
		t.Fatalf("shared Main Stop did not reach original Targeted consumer: %s", diagnostic)
	}
	accepted := repairDispatchSnapshot(t, root)
	if len(repairArtifactRefs(repairPointerCLI(accepted.State), "targeted_reverification_artifacts", "", "")) != 1 {
		t.Fatal("shared Stop did not retain its original independent observation")
	}
}
