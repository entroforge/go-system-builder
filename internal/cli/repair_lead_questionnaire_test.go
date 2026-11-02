package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS9ImpactPrepareRejectsWrongResponsibilityWithoutMutation(t *testing.T) {
	root := repairLeadFreshRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), "requires the current S9 impact_reconciliation") {
		t.Fatalf("wrong-stage preparation was not refused: code=%d out=%s diagnostic=%s", code, &out, &diagnostic)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("wrong-stage preparation mutated Runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "submissions", "s9")); !os.IsNotExist(err) {
		t.Fatalf("wrong-stage preparation created S9 draft tree: %v", err)
	}
}

func TestS9RepairLeadStopAndIdleDoNotGuessPathWithoutAuthority(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadBaseState(t, root, "bug_resolution", "fixing", 0)
	mapFieldCLI(state, "review")["repair"] = map[string]any{"status": "impact_reconciliation"}
	snapshot := runtime.Snapshot{State: state}
	for _, event := range []string{"Stop", "Idle"} {
		context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, snapshot)
		if !strings.Contains(context, "the original S9 questionnaire path was not retained") || !strings.Contains(context, "Main/system") {
			t.Fatalf("%s guessed a ChangeImpact path without current source authority: %s", event, context)
		}
		if paths := repairLeadQuestionnairePaths(root, state); len(paths) != 0 {
			t.Fatalf("%s path selector invented a questionnaire path without current authority: %#v", event, paths)
		}
	}

	// S9 Handoff has no Main-authored professional fields. A ready Stop/Idle
	// reports its read-only behavior without creating a handoff or new form.
	ready := repairLeadBaseState(t, root, "bug_resolution", "ready_for_full_review", 0)
	mapFieldCLI(ready, "review")["repair"] = map[string]any{"status": "ready_for_full_review", "required_reverification_ids": []any{}}
	repairLeadWriteState(t, root, ready)
	beforeReady, _ := json.Marshal(ready)
	beforeRuntime := repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"Stop", "Idle"} {
		context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, runtime.Snapshot{State: ready})
		if !strings.Contains(context, repairExitRecovery(root)) || !strings.Contains(context, "Preserve accepted Targeted observations") || !strings.Contains(context, "only completes the original Handoff and S7 preparation") {
			t.Fatalf("%s did not report read-only automatic handoff: %s", event, context)
		}
	}
	afterReady, _ := json.Marshal(ready)
	if !bytes.Equal(beforeReady, afterReady) {
		t.Fatal("ready reminder changed its input facts")
	}
	repairDispatchAssertUnchanged(t, root, beforeRuntime)
	if got := repairLeadQuestionnairePaths(root, ready); len(got) != 0 {
		t.Fatalf("ready handoff has no professional answer path, got %#v", got)
	}
}

func TestS9RepairLeadPathSelectorUsesCurrentPhaseWithoutPreparing(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadBaseState(t, root, "bug_resolution", "fixing", 0)
	mapFieldCLI(state, "review")["repair"] = map[string]any{"status": "impact_reconciliation"}
	if paths := repairLeadQuestionnairePaths(root, state); len(paths) != 0 {
		t.Fatalf("path selector guessed without current Case/Session/Plan source: %#v", paths)
	}
	ready := repairLeadBaseState(t, root, "bug_resolution", "ready_for_full_review", 0)
	mapFieldCLI(ready, "review")["repair"] = map[string]any{"status": "ready_for_full_review", "required_reverification_ids": []any{}}
	if got := repairLeadQuestionnairePaths(root, ready); len(got) != 0 {
		t.Fatalf("ready handoff should not manufacture an answer path: %#v", got)
	}

	state = repairLeadBaseState(t, root, "bug_resolution", "targeted_reverification", 0)
	if got := repairLeadQuestionnairePaths(root, state); len(got) != 0 {
		t.Fatalf("targeted path selector guessed without a current plan/impact/dispatch: %#v", got)
	}
}

func TestS9RepairLeadTargetedPathUsesRegisteredIndependentAssignment(t *testing.T) {
	root := repairLeadFreshRoot(t)
	contractRef := writeRepairLeadTestContract(t, root, "investigation-case-lead-path")
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-lead-path", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-lead-path", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-lead-path", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{"investigation-case-lead-path"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts:          []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go"}},
		Decisions:                 []repair.ImpactDecision{{SourceID: "finding-1", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", Scope: []string{"internal/api/payload.go"}, Rationale: "the changed authority must be checked", RecoveryEvidence: []string{"test://lead-path"}}},
		RequiredReverificationIDs: []string{"reverify-lead-path"}, AnalyzedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if impact.ImpactID == "" {
		t.Fatal("CreateChangeImpact returned no current Impact")
	}
	assignmentID := plan.Assignments[0].AssignmentID
	verifierID := repair.VerificationAssignmentID(assignmentID)
	state := repairLeadBaseState(t, root, "bug_resolution", "targeted_reverification", 0)
	state["runtime_id"] = "loop-req039-ct"
	review := mapFieldCLI(state, "review")
	review["repair"] = map[string]any{
		"status":  "targeted_reverification",
		"case_id": "investigation-case-lead-path", "contract_id": "repair-contract-investigation-case-lead-path",
		"contract_ref": contractRef.Path, "session_id": session.SessionID,
		"path":                        sessionRef.Path,
		"plan_ref":                    planRef.Path,
		"impact_ref":                  impactRef.Path,
		"required_reverification_ids": []any{"reverify-lead-path"},
		"assignment_owners":           map[string]any{assignmentID: "builder-lead-path", verifierID: "verifier-lead-path"},
		// The Repair session writer retains the original questionnaire
		// location; the answer path is not re-derived from any content digest.
		"questionnaire_root": repair.QuestionnaireRoot("loop-req039-ct", session.SessionID),
	}
	registerLegacyTargetedTestSources(t, root, state, assignmentID, verifierID, "verifier-lead-path")
	want, err := repairTargetedQuestionnairePath(root, state, verifierID)
	if err != nil {
		t.Fatal(err)
	}
	got := repairLeadQuestionnairePaths(root, state)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("targeted paths = %#v, want registered verifier path %s", got, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("read-only path selector created verifier questionnaire: %v", err)
	}
	selections, err := repairTargetedSelections(root, state)
	if err != nil || len(selections) != 1 {
		t.Fatalf("legacy single obligation source did not resolve one selection: selections=%#v err=%v", selections, err)
	}
	_, legacyDraft, err := prepareRepairTargetedQuestionnaire(root, state, selections[0])
	if err != nil || legacyDraft.ReverificationID == "" || legacyDraft.OriginalAssignmentID != assignmentID || legacyDraft.PerformingAssignmentID != verifierID || legacyDraft.ImpactID != impact.ImpactID {
		t.Fatalf("original Targeted DTO lost its current source responsibility: %+v err=%v", legacyDraft, err)
	}
	if legacyDraft.Result != "" {
		t.Fatal("independent verdict was prefilled")
	}
	start := policy.Input{Event: "SubagentStart", AgentID: "opaque-verifier", LastAssistantMessage: want}
	context := nativeS9TargetedStartContext(root, state, start)
	if !strings.Contains(context, "the approved scope expands") || !strings.Contains(context, "scope_changed") {
		t.Fatalf("native Targeted start lost original stop conditions: %s", context)
	}
	start.LastAssistantMessage = "unrelated native work"
	if got := nativeS9TargetedStartContext(root, state, start); got != "" {
		t.Fatalf("unrelated actor acquired guessed responsibility: %s", got)
	}
	repeatedPath, repeated, err := prepareRepairTargetedQuestionnaire(root, state, selections[0])
	if err != nil || repeatedPath != want || repeated.ReverificationID != legacyDraft.ReverificationID {
		t.Fatalf("same actual Targeted responsibility changed identity on retry: %+v %v", repeated, err)
	}
}

func repairLeadSaveInput(root, path, agentID string) policy.Input {
	return policy.Input{Event: "PostToolUse", AgentID: agentID, CWD: root, ToolName: "Write", ToolInput: map[string]any{"file_path": path}}
}

func readRepairLeadJSON(t *testing.T, path string, destination any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decodeErr error
	if impact, ok := destination.(*repair.ChangeImpactRequest); ok {
		decodeErr = repair.DecodeOriginalChangeImpact(data, impact)
	} else {
		decodeErr = json.Unmarshal(data, destination)
	}
	if err := decodeErr; err != nil {
		t.Fatal(err)
	}
}

func writeRepairLeadJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRepairLeadFile(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repairLeadDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func repairLeadFreshRoot(t *testing.T) string {
	t.Helper()
	root := initFormatRoot(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("initialize local S9 fixture: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	return root
}

func repairLeadBaseState(t *testing.T, root, lifecycleState, phase string, revision int) map[string]any {
	t.Helper()
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	reqBytes := []byte("# REQ-039\n> Status: locked\n> Version: v2.0.0\n")
	writeRepairLeadFile(t, root, reqPath, string(reqBytes))
	state, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatalf("build local S9 Runtime from current initialization facts: %v", err)
	}
	state["runtime_id"] = "loop-req039-ct"
	state["revision"] = float64(revision)
	state["lifecycle"] = map[string]any{
		"state": lifecycleState, "phase": repairLeadPhaseValue(phase), "phase_revision": 0,
	}
	milestone := mapFieldCLI(state, "milestone")
	milestone["stage"] = "S8"
	milestone["lifecycle_state"] = lifecycleState
	milestone["lifecycle_phase"] = repairLeadPhaseValue(phase)
	milestone["source_revision"] = float64(revision)
	state["authorization"] = map[string]any{
		"mode": "binding", "command": "loop-harness req bind", "actor": "user",
		"occurred_at": "2026-10-01T00:00:00Z",
	}
	state["bound_req"] = map[string]any{
		"id": "REQ-039", "path": reqPath, "version": "v2.0.0", "sha256": repairLeadDigest(reqBytes),
		"status": "locked", "approved_by": "user", "approved_at": "2026-10-01T00:00:00Z",
		"metadata": map[string]any{"ui_impact": "none"},
	}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-10-01T00:00:00Z"}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	return state
}

func repairLeadPhaseValue(phase string) any {
	if phase == "" {
		return nil
	}
	return phase
}

func repairLeadWriteState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil {
		t.Fatal("local S9 fixture is missing its locked REQ binding")
	}
	repairLeadCommitFixture(t, root)
	bound["workspace"] = map[string]any{
		"project_root": root, "dev_branch": "test-development",
		"release_upstream": "origin/release",
	}
	putInitMap(t, filepath.Join(root, ".claude", "loop-state.json"), state)
}

func repairLeadCommitFixture(t *testing.T, root string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		repairLeadGit(t, root, "init", "-q", "-b", "test-development")
	} else if err != nil {
		t.Fatalf("inspect local S9 fixture Git repository: %v", err)
	}
	repairLeadGit(t, root, "config", "--local", "user.name", "S9 Fixture")
	repairLeadGit(t, root, "config", "--local", "user.email", "s9-fixture@example.invalid")
	repairLeadGit(t, root, "config", "--local", "commit.gpgsign", "false")
	info := filepath.Join(root, ".git", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "exclude"), []byte(".claude/\n.worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repairLeadGit(t, root, "add", "-A")
	repairLeadGit(t, root, "commit", "--allow-empty", "-qm", "S9 test fixture")
	repairLeadGit(t, root, "branch", "-f", "origin/release", "HEAD")
	return repairLeadGit(t, root, "rev-parse", "--verify", "refs/heads/test-development^{commit}")
}

func repairLeadGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestS9RepairReadbackUsesOriginalSessionAndPlanWriters(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadApprovedCase(t, root, "investigation-case-lead-readback", []map[string]any{{
		"id": "unit-lead-questionnaire", "description": "restore the source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})

	before, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	reminder := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	after, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || before.Revision != after.Revision || !strings.Contains(reminder, "Main/system must restore") || strings.Contains(reminder, "runtime repair session open") {
		t.Fatalf("ordinary reminder changed Runtime or omitted startup: %s (%v)", reminder, err)
	}
	runtimeID, generation, contract, err := repairReadbackContractSource(root, state)
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := repairReadbackIDSuffix(runtimeID, generation, contract.CaseID, contract.ContractID, contract.Ref)
	if err != nil {
		t.Fatal(err)
	}
	var out, commandErr bytes.Buffer
	if code := runRuntimeRepairSessionOpen([]string{"--root", root, "--session-id", "repair-session-" + suffix, "--created-by", "Main", "--actor", "Main"}, &out, &commandErr); code != 0 {
		t.Fatalf("normal session startup: %s", commandErr.String())
	}
	if code := runRuntimeRepairPlanCompile([]string{"--root", root, "--plan-id", "repair-plan-" + suffix, "--created-by", "Main", "--actor", "Main"}, &out, &commandErr); code != 0 {
		t.Fatalf("normal plan compile: %s", commandErr.String())
	}
	compiled, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	state = compiled.State
	for _, name := range []string{"builder-dispatch.json", "independent-verifier-dispatch.json"} {
		path, err := repairLeadQuestionnairePath(root, state, name)
		if err != nil || questionnaireMissing(path) {
			t.Fatalf("normal startup omitted division %s: %v", path, err)
		}
	}
	context := stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	prepared, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pointer := repairPointerCLI(prepared.State)
	for _, key := range []string{"contract_ref", "plan_ref"} {
		refPath := stringValue(pointer[key])
		if refPath == "" {
			t.Fatalf("original writer did not register %s: %#v", key, pointer)
		}
		absPath, err := absoluteQuestionnairePath(root, refPath)
		if err != nil || !strings.Contains(context, absPath) {
			t.Fatalf("readback context omitted exact %s path %s: %s (err=%v)", key, absPath, context, err)
		}
	}
	if stringValue(pointer["status"]) != "planning" || !strings.Contains(context, "original RepairPlan") {
		t.Fatalf("readback did not use the original Session/Plan APIs: pointer=%#v context=%s", pointer, context)
	}
	_, err = prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), prepared.State)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != prepared.Revision {
		t.Fatalf("repeated readback preparation wrote Runtime again: revision %d -> %d", prepared.Revision, replayed.Revision)
	}
}

func TestS9RepairReadbackIDsIncludeCurrentCaseAndContract(t *testing.T) {
	root := repairLeadFreshRoot(t)
	caseA, caseB := "investigation-case-lead-readback-a", "investigation-case-lead-readback-b"
	stateA := repairLeadApprovedCase(t, root, caseA, []map[string]any{{
		"id": "unit-lead-questionnaire-a", "description": "restore Case A source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
	first, err := prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), stateA)
	if err != nil {
		t.Fatal(err)
	}
	firstPointer := repairPointerCLI(first)
	firstPlan, err := repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: stringValue(firstPointer["plan_ref"])})
	if err != nil {
		t.Fatal(err)
	}
	firstQuestionnairePath, err := repairLeadQuestionnairePath(root, first, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}

	// Seed the same Runtime and generation from a different approved Case while
	// leaving Case A's immutable Session/Plan artifacts in the shared root.
	stateB := repairLeadApprovedCase(t, root, caseB, []map[string]any{{
		"id": "unit-lead-questionnaire-b", "description": "restore Case B source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
	second, err := prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), stateB)
	if err != nil {
		t.Fatal(err)
	}
	secondPointer := repairPointerCLI(second)
	secondPlan, err := repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: stringValue(secondPointer["plan_ref"])})
	if err != nil {
		t.Fatal(err)
	}
	secondQuestionnairePath, err := repairLeadQuestionnairePath(root, second, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	if firstPointer["session_id"] == secondPointer["session_id"] || firstPlan.PlanID == secondPlan.PlanID {
		t.Fatalf("different Cases reused immutable S9 artifacts at identical Runtime/generation: session %q/%q plan %q/%q", firstPointer["session_id"], secondPointer["session_id"], firstPlan.PlanID, secondPlan.PlanID)
	}
	if firstPointer["contract_ref"] == secondPointer["contract_ref"] {
		t.Fatalf("different Cases reused the same approved Contract: %q", firstPointer["contract_ref"])
	}
	if firstQuestionnairePath == secondQuestionnairePath {
		t.Fatalf("different Case/Plan sources reused the same same-round questionnaire path %s", firstQuestionnairePath)
	}
	if firstPointer["case_id"] != caseA || secondPointer["case_id"] != caseB {
		t.Fatalf("readback did not follow current approved Case: first=%q second=%q", firstPointer["case_id"], secondPointer["case_id"])
	}
}

// repairLeadApprovedCase creates the S9 source through the same S7->S8 and
// Case/Contract writers used by the product. The accepted Finding is an
// actual fixture fact; the InvestigationCase is never
// hand-pinned into Runtime.
func repairLeadApprovedCase(t *testing.T, root, caseID string, units []map[string]any, assertionCopies ...int) map[string]any {
	t.Helper()
	state := repairLeadBaseState(t, root, "bug_resolution", "investigation", 0)
	token := dispatchSlug(caseID)
	findingID := "finding-" + token
	occurredAt := "2026-10-01T00:00:00Z"
	finding := map[string]any{
		"schema_version": "1.0.0", "finding_id": findingID, "claim_id": "claim-" + token,
		"lens": "qa", "severity": "P1", "expected": "the expected behavior holds",
		"authority_refs": []string{"REQ-039"}, "observed": "the observed behavior deviates",
		"observation_mode": "code_inspection",
		"encounter": map[string]any{
			"journey_summary":  "inspect the bound implementation and observe the deviation",
			"inspection_entry": "internal/api", "symbol_trail": "entry -> boundary",
			"wall_action": "inspect the boundary", "first_bad_checkpoint": "the invariant is false",
			"terminal_state": "review stopped",
		},
		"reproducibility": "always", "evidence_refs": []string{"test://repair-lead-finding"},
	}
	findingBytes, err := json.MarshalIndent(finding, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	findingBytes = append(findingBytes, '\n')
	findingRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "findings", findingID+".json"))
	writeRepairLeadFile(t, root, findingRel, string(findingBytes))
	mapFieldCLI(state, "entities")["findings"] = []any{map[string]any{
		"finding_id": findingID, "path": findingRel, "review_plan_id": "review-plan-repair-lead-fixture",
		"claim_id": finding["claim_id"], "assignment_id": "assignment-s8-fixture", "lens": "qa",
		"severity": "P1", "observation_mode": "code_inspection", "original_finder": "reviewer-fixture",
		"created_at": occurredAt,
	}}
	mapFieldCLI(state, "review")["round_entry"] = map[string]any{"plan_id": "review-plan-repair-lead-fixture", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	repairLeadWriteState(t, root, state)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ingested, err := investigation.Ingest(root, statePath, journalPath, investigation.IngestRequest{
		ExpectedRevision: 0, CaseID: caseID, GroupingRationale: "the accepted Finding is a bounded repair Case",
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original InvestigationCase Ingest writer: %v", err)
	}
	caseRevision := func(state map[string]any) int {
		pointer := mapFieldCLI(mapFieldCLI(state, "review"), "investigation")
		switch value := pointer["revision"].(type) {
		case float64:
			return int(value)
		case int:
			return value
		default:
			t.Fatalf("active InvestigationCase has no numeric revision: %#v", pointer["revision"])
			return 0
		}
	}
	assignmentID := "assignment-hypothesis-" + token
	hypothesisID := "hypothesis-" + token
	registered, err := investigation.RegisterHypothesis(root, statePath, journalPath, investigation.HypothesisRequest{
		ExpectedRevision: ingested.Revision, ExpectedCaseRevision: caseRevision(ingested.State), CaseID: caseID, HypothesisID: hypothesisID, AssignmentID: assignmentID,
		Statement:        "the source authority is duplicated at the reported boundary",
		Invariant:        "the boundary has one authoritative source",
		Discriminator:    "trace the observed value across the source boundary",
		ExpectedOutcomes: map[string]any{"support": "the source differs at the boundary", "refute": "the source is consistent at the boundary"},
		SourceFindingIDs: []string{findingID}, EvidenceRefs: []string{"test://repair-lead-finding"},
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 2, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original hypothesis registration writer: %v", err)
	}
	result, err := investigation.SubmitHypothesisResult(root, statePath, journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision: registered.Revision, ExpectedCaseRevision: caseRevision(registered.State), CaseID: caseID, HypothesisID: hypothesisID, AssignmentID: assignmentID,
		Method: "read-only inspection of the cited source boundary", EvidenceRefs: []string{"test://repair-lead-finding"},
		SourceBoundaryRefs: []string{"finding-source-boundary"},
		Observed:           "the observed value crosses two conflicting source definitions",
		Counterfactual:     "one authoritative source would preserve the same value across the boundary",
		Result:             "supported",
		ExplainsFindingIDs: []string{findingID},
		DoesNotExplain:     []string{},
		OccurredAt:         time.Date(2026, 10, 1, 0, 0, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original HypothesisResult writer: %v", err)
	}
	routed, err := investigation.UpdateCaseRoute(root, statePath, journalPath, investigation.RouteRequest{
		ExpectedRevision: result.Revision, ExpectedCaseRevision: caseRevision(result.State), CaseID: caseID, Route: "s9_repair", RouteReason: "the fixture requires a bounded implementation repair",
		PrimaryRootCause: "the source authority is duplicated",
		CausalModel: map[string]any{
			"trigger": "payload crosses the boundary", "violated_invariant": "one owner",
			"faulty_mechanism": "duplicate authority", "propagation": "the boundary diverges",
			"symptoms": []string{"the Finding is reproduced"},
		},
		NoCompetingHypothesis: "the sealed observation supports no credible competing mechanism",
		BlastRadius:           map[string]any{"paths": []string{"internal/api"}},
		DetectionGap:          map[string]any{"gap_type": "contract", "evidence_refs": []string{"test://repair-lead-detection-gap"}},
		OccurredAt:            time.Date(2026, 10, 1, 0, 0, 4, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original InvestigationCase route writer: %v", err)
	}
	approvedCaseRevision := caseRevision(routed.State)
	contractID := "repair-contract-" + token
	draftRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "contracts", "drafts", contractID+".json"))
	draft := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": contractID, "case_id": caseID,
		"revision": approvedCaseRevision, "status": "draft", "source_finding_ids": []string{findingID},
		"root_cause_statement": "the source authority is duplicated", "violated_invariant": "one owner",
		"causal_model_ref": "case://" + caseID + "/causal-model", "architecture_intent": "restore one source authority",
		"repair_units": units, "prospective_scope": []string{"internal/api"},
		"forbidden_scope":            []string{"docs/requirements"},
		"symptom_assertions":         []string{"the observed behavior is corrected"},
		"root_invariant_assertions":  []string{"one authority remains"},
		"detection_gap_assertions":   []string{"the boundary test detects drift"},
		"stop_escalation_conditions": []string{"the approved scope expands"},
	}
	if len(assertionCopies) > 0 && assertionCopies[0] > 1 {
		for _, field := range []string{"symptom_assertions", "root_invariant_assertions", "detection_gap_assertions"} {
			values := draft[field].([]string)
			for copy := 1; copy < assertionCopies[0]; copy++ {
				values = append(values, fmt.Sprintf("%s variation %d", values[0], copy))
			}
			draft[field] = values
		}
	}
	draftBytes, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	draftBytes = append(draftBytes, '\n')
	writeRepairLeadFile(t, root, draftRel, string(draftBytes))
	// Test-double decision records the actual presented Contract; it is not a real human authorization.
	decisionID := "ev-contract-approval-" + token
	decisionRel := filepath.ToSlash(filepath.Join(".claude", "decisions", decisionID+".json"))
	decisionBytes, err := json.MarshalIndent(map[string]any{
		"decision": "approve_contract", "decision_id": decisionID, "runtime_id": "loop-req039-ct",
		"case_id": caseID, "contract_id": contractID, "approved_by": "main-session", "contract_draft": draft,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	decisionBytes = append(decisionBytes, '\n')
	writeRepairLeadFile(t, root, decisionRel, string(decisionBytes))
	evidenceSnapshot, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: routed.Revision, ID: decisionID, Kind: "human_decision", Path: decisionRel,
		ProducedBy: []string{"main-session"}, ScopeRefs: []string{"s8_contract_approval:loop-req039-ct"},
		Validator: semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		t.Fatalf("original human approval evidence writer: %v", err)
	}
	approved, err := investigation.ApproveContract(root, statePath, journalPath, investigation.ContractRequest{
		ExpectedRevision: evidenceSnapshot.Revision, CaseID: caseID, ContractPath: draftRel,
		ApprovedBy: "main-session", ContractDraft: json.RawMessage(draftBytes), ApprovalEvidenceID: decisionID,
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original RepairContract approval writer: %v", err)
	}
	return approved.State
}

func writeRepairLeadTestContract(t *testing.T, root, caseID string) repair.ContractRef {
	return writeRepairLeadContractWithUnits(t, root, caseID, []map[string]any{{
		"id": "unit-lead-questionnaire", "description": "restore the source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
}

func writeRepairLeadContractWithUnits(t *testing.T, root, caseID string, units []map[string]any) repair.ContractRef {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "contracts", "repair-contract-"+dispatchSlug(caseID)+".json"))
	value := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-" + dispatchSlug(caseID),
		"case_id": caseID, "revision": 1, "status": "approved",
		"source_finding_ids": []string{"finding-lead-readback"}, "root_cause_statement": "two authorities drift",
		"violated_invariant": "one authority", "causal_model_ref": "case://lead-readback/model",
		"architecture_intent": "restore one authority",
		"repair_units":        units,
		"prospective_scope":   []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions":         []string{"the reported behavior is corrected"},
		"root_invariant_assertions":  []string{"one authority remains"},
		"detection_gap_assertions":   []string{"the existing test catches drift"},
		"stop_escalation_conditions": []string{"the approved scope expands"},
		"approved_by":                "human", "approved_at": "2026-10-01T00:00:00Z",
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return repair.ContractRef{Path: rel}
}
