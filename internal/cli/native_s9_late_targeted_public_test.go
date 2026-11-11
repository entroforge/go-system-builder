package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	// The shared legacy helper still mints the historical `TASK-9<base64>`
	// identifier. L3-S4 requires the canonical
	// `^TASK-[0-9]{3,}(?:-[0-9]{2,})?$` TASK shape and the v1.2 Runtime
	// schema rejects the historical form, so this fixture re-issues its own
	// TASK with the canonical identifier before the Runtime Writer sees it.
	reissueCanonicalTaskID(t, root, state, nativeS9LateCanonicalTaskID)
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
	// The historical `TASK-9<base64>` identifier is a removed compatibility
	// form: the committed Runtime state must keep the original responsibility
	// only under the canonical TASK id.
	committedTasks, _ := mapFieldCLI(after.State, "entities")["tasks"].([]any)
	kept := false
	for _, raw := range committedTasks {
		row, _ := raw.(map[string]any)
		id := stringValue(row["id"])
		if !nativeS9LateCanonicalTaskPattern.MatchString(id) {
			t.Fatalf("committed Runtime state kept the removed historical TASK identifier %q", id)
		}
		if id == nativeS9LateCanonicalTaskID {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("committed Runtime state lost the canonical original TASK %q", nativeS9LateCanonicalTaskID)
	}
}

// The canonical TASK identifier shape shared by the S4 planning check and the
// Runtime schema (internal/schema/assets/loop-state-v1.2.schema.json).
var nativeS9LateCanonicalTaskPattern = regexp.MustCompile(`^TASK-[0-9]{3,}(?:-[0-9]{2,})?$`)

// The canonical TASK id this fixture re-issues for its original verifier.
const nativeS9LateCanonicalTaskID = "TASK-901"

// The path under which the shared legacy helper registers its TASK source.
const nativeS9LateLegacyTaskPath = ".claude/test-targeted/task.json"

// reissueCanonicalTaskID replaces the legacy fixture's historical
// `TASK-9<base64>` identifier with the canonical TASK id and keeps every
// recorded reference consistent: the TASK source file and its digest, the
// TASK entity and the original owner's issued TASK list.
func reissueCanonicalTaskID(t *testing.T, root string, state map[string]any, canonicalID string) {
	t.Helper()
	if !nativeS9LateCanonicalTaskPattern.MatchString(canonicalID) {
		t.Fatalf("canonical TASK id %q does not match the Runtime schema shape", canonicalID)
	}
	entities := mapFieldCLI(state, "entities")
	tasks, _ := entities["tasks"].([]any)
	var taskRow map[string]any
	legacyID := ""
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if row == nil || stringValue(row["path"]) != nativeS9LateLegacyTaskPath {
			continue
		}
		taskRow, legacyID = row, stringValue(row["id"])
	}
	if taskRow == nil || legacyID == "" {
		t.Fatal("legacy Targeted fixture did not register its TASK source")
	}
	if legacyID == canonicalID {
		t.Fatalf("legacy Targeted fixture already uses the canonical TASK id %q", canonicalID)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(nativeS9LateLegacyTaskPath)))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	payload["task_id"] = canonicalID
	updated, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadFile(t, root, nativeS9LateLegacyTaskPath, string(updated))
	taskRow["id"] = canonicalID
	taskRow["sha256"] = repairLeadDigest(updated)
	agents, _ := entities["agents"].([]any)
	reissued := 0
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		ids, _ := row["task_ids"].([]any)
		for index, id := range ids {
			if stringValue(id) == legacyID {
				ids[index] = canonicalID
				reissued++
			}
		}
	}
	if reissued != 1 {
		t.Fatalf("canonical TASK id was not re-issued to exactly one original owner: %d", reissued)
	}
}
