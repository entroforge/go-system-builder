package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The pre-existing closed Session, Plan, original verifier responsibility and
// current S7 state are fixture inputs. The answer is received through the
// public natural Hook and original Targeted consumer, rather than inserting a
// receipt or fabricating a second lifecycle.
func TestNativeS9LateTargetedPublicHookPreservesCurrentS7(t *testing.T) {
	root := repairLeadFreshRoot(t)
	caseID := "investigation-case-late-hook"
	contract := writeRepairLeadTestContract(t, root, caseID)
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contract, SessionID: "repair-session-late-hook", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contract, Session: sessionRef, PlanID: "repair-plan-late-hook", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-late-hook", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{caseID}, ChangeTypes: []string{}, Decisions: []repair.ImpactDecision{}, AnalyzedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment := plan.Assignments[0].AssignmentID
	verifier := repair.VerificationAssignmentID(assignment)
	state := repairLeadBaseState(t, root, "verification", "running", 0)
	state["schema_version"] = "1.2.0"
	mapFieldCLI(mapFieldCLI(state, "configuration"), "optimization")["packet"] = true
	mapFieldCLI(state, "review")["repair"] = map[string]any{
		"status": "closed", "case_id": caseID, "contract_id": "repair-contract-investigation-case-late-hook",
		"contract_ref": contract.Path,
		"session_id":   session.SessionID, "path": sessionRef.Path,
		"revision": 1, "updated_at": "2026-10-30T00:00:00Z", "next_action": "continue new S7",
		"plan_ref":                     planRef.Path,
		"impact_ref":                   impactRef.Path,
		"questionnaire_root":           repair.QuestionnaireRoot(stringValue(state["runtime_id"]), session.SessionID),
		"assignment_owners":            map[string]any{assignment: "builder-late-hook", verifier: "verifier-late-hook"},
		"targeted_reverification_refs": []any{}, "targeted_reverification_artifacts": []any{},
	}
	registerLegacyTargetedTestSources(t, root, state, assignment, verifier, "verifier-late-hook")
	// The legacy selector helper supplies only its lookup fields. This test
	// reaches the real Runtime Writer, so complete the existing v1.2 entity
	// shapes without changing the original task/owner/manifest associations.
	writeRepairLeadFile(t, root, "agents/test-builder.md", "# Original verifier role\n")
	entities := mapFieldCLI(state, "entities")
	agent := entities["agents"].([]any)[0].(map[string]any)
	agent["state"], agent["definition_ref"] = "working", "agents/test-builder.md"
	agent["readback_ref"], agent["activation_ref"], agent["activation_revision"] = nil, nil, nil
	agent["updated_at"] = "2026-10-30T00:00:00Z"
	task := entities["tasks"].([]any)[0].(map[string]any)
	task["state"] = "in_progress"
	team := entities["teams"].([]any)[0].(map[string]any)
	team["platform_team_id"], team["kind"], team["status"] = "team-test-verifier", "builder", "active"
	team["responsibility_ids"] = []any{repair.ManifestVerificationAssignmentID(assignment)}
	team["agent_ids"], team["review_round"] = []any{"verifier-late-hook"}, 1
	selections, err := repairTargetedSelections(root, state)
	if err != nil || len(selections) != 1 {
		t.Fatalf("original verifier selection: %v %+v", err, selections)
	}
	path, _, err := prepareRepairTargetedQuestionnaire(root, state, selections[0])
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, path, map[string]any{"impact_id": impact.ImpactID, "result": "fail"})
	repairLeadWriteState(t, root, state)
	invoke := func(reference string) (string, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "cwd": root, "agent_id": "native-verifier", "last_assistant_message": reference})
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("public Stop: %d %s %s", code, &out, &diagnostic)
		}
		return out.String(), diagnostic.String()
	}
	before, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	invoke(path + ".backup")
	afterUnrelated, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || afterUnrelated.Revision != before.Revision {
		t.Fatalf("unrelated reference changed the original responsibility: %v", err)
	}
	out, diagnostic := invoke(path)
	after, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pointer := repairPointerCLI(after.State)
	rows := repairArtifactRefs(pointer, "targeted_reverification_artifacts", "")
	if len(rows) != 1 || lifecycleState(after.State) != "verification" || mapFieldCLI(after.State, "lifecycle")["phase"] != "running" || pointer["status"] != "closed" || pointer["next_action"] != "continue new S7" {
		t.Fatalf("late Targeted not received or rewound S7: rows=%+v lifecycle=%+v repair=%+v output=%s diagnostic=%s", rows, after.State["lifecycle"], pointer, out, diagnostic)
	}
	if !strings.Contains(diagnostic, "committed through the original Runtime Writer") {
		t.Fatalf("original consumer outcome unavailable: %s", diagnostic)
	}
}
