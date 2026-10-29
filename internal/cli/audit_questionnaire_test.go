package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestMainAuditQuestionnairesDoNotRouteToCompletedReviewer(t *testing.T) {
	for _, request := range []policy.Input{
		{Event: "PreToolUse", AgentID: "reviewer"},
		{Event: "SubagentStart"},
	} {
		root := t.TempDir()
		for _, stage := range []string{"acceptance", "awaiting_human_release"} {
			snapshot := runtime.Snapshot{State: map[string]any{"lifecycle": map[string]any{"state": stage}}}
			if got := stageAuditQuestionnaireContext(root, request, snapshot); got != "" {
				t.Fatalf("%s routed Main audit to another responsibility: %s", request.Event, got)
			}
			if got := stageHumanReleaseQuestionnaireContext(root, request, snapshot); got != "" {
				t.Fatalf("%s routed human application to another responsibility: %s", request.Event, got)
			}
		}
		files, err := os.ReadDir(root)
		if err != nil || len(files) != 0 {
			t.Fatalf("routing another responsibility created files: %v %v", files, err)
		}
	}
}

func TestAuditManifestDraftUsesAuthoritativeInventoryAndDerivedMetrics(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-REQ-7",
		"baseline":   map[string]any{"generation": 3},
		"review":     map[string]any{"round": 5},
	}
	authority := acceptance.InventoryAuthority{
		RequirementIDs: []string{"REQ-7/FR-1"},
		ContractIDs:    []string{"CONTRACT-2"},
		TaskIDs:        []string{"TASK-4#closing-contract"},
		ClaimIDs:       []string{"CLAIM-9"},
		ChangedPaths:   []string{"internal/example.go"},
	}
	for _, kind := range []string{acceptance.ManifestAcceptance, acceptance.ManifestReleaseAudit} {
		draft := auditManifestDraft(state, kind, authority)
		if draft.SchemaVersion != acceptance.SchemaVersionAcceptance || draft.ManifestType != kind || draft.RuntimeID != "loop-REQ-7" || draft.BaselineGeneration != 3 || draft.ReviewRound != 5 {
			t.Fatalf("%s draft lost current Runtime binding: %#v", kind, draft)
		}
		if len(draft.CoverageInventory) != 0 || len(draft.Counterevidence) != 0 || len(draft.AuditAreas) != 0 {
			t.Fatalf("%s metadata draft invented authority rows, per-row counterevidence or eight audit tasks: %#v", kind, draft)
		}
		if draft.Outcome != "" || len(draft.Risks) != 0 || len(draft.TechnicalDebt) != 0 || len(draft.BlockingFindings) != 0 {
			t.Fatalf("%s empty draft invented professional conclusions or obligations: %#v", kind, draft)
		}
		if draft.Metrics != acceptance.DeriveMetrics(draft) {
			t.Fatalf("%s display metrics differ from the empty current draft: %#v", kind, draft.Metrics)
		}
	}

}

func TestAuditQuestionnaireStopIsReadOnlyAndKeepsDraft(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	path, err := auditQuestionnairePath(root, state, acceptance.ManifestAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	absPath, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Snapshot{State: state}
	context := stageAuditQuestionnaireContext(root, policy.Input{Event: "Stop"}, snapshot)
	if !strings.Contains(context, absPath) || !strings.Contains(context, "read-only") || !strings.Contains(context, "not prepared") {
		t.Fatalf("Stop context does not report the exact missing draft read-only: %s", context)
	}
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Fatalf("Stop prepared a draft: %v", err)
	}

	if _, _, err := prepareAuditQuestionnaire(root, state, acceptance.ManifestAcceptance); err != nil {
		t.Fatal(err)
	}
	original := []byte("keep this author's draft intact\n")
	if err := os.WriteFile(absPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	context = stageAuditQuestionnaireContext(root, policy.Input{Event: "Idle"}, snapshot)
	if !strings.Contains(context, absPath) {
		t.Fatalf("Idle context omitted existing answer path: %s", context)
	}
	actual, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, original) {
		t.Fatalf("Stop/Idle changed the existing draft: %q", actual)
	}
}

func TestSavedAuditQuestionnaireRecordsEvidenceForMainWithoutAgentIDAndReplayIsNoop(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	path, draft, err := prepareAuditQuestionnaire(root, state, acceptance.ManifestAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	for i := range draft.CoverageInventory {
		draft.CoverageInventory[i].Expected = "matches the bound requirement and review claim"
		draft.CoverageInventory[i].Oracle = "current registered source and test evidence"
		draft.CoverageInventory[i].Owner = "Acceptance Reviewer"
		draft.CoverageInventory[i].EvidenceRefs = []string{"support"}
		draft.CoverageInventory[i].Disposition = "pass"
	}
	for i := range draft.Counterevidence {
		draft.Counterevidence[i].Question = "What evidence would disprove this conclusion?"
		draft.Counterevidence[i].EvidenceRefs = []string{"support"}
		draft.Counterevidence[i].Outcome = "pass"
	}
	draft.Outcome = "pass"
	// Simulate an author who edits professional rows without copying derived
	// metrics. The save path must refresh them with the validator's algorithm.
	draft.Metrics = acceptance.Metrics{}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	absPath, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	request := policy.Input{
		Event: "PostToolUse", ToolName: "Write", CWD: root,
		ToolInput: map[string]any{"file_path": absPath},
	}
	var feedback bytes.Buffer
	if handled, code := runSavedAuditQuestionnaire(root, request, &feedback, &bytes.Buffer{}); !handled || code != 0 {
		t.Fatalf("saved answer was not handled: handled=%v code=%d feedback=%s", handled, code, feedback.String())
	}
	if !strings.Contains(feedback.String(), absPath) {
		t.Fatalf("save feedback omitted its absolute answer path: %s", feedback.String())
	}

	store := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"))
	afterSave, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var registered map[string]any
	for _, raw := range stateEvidence(afterSave.State) {
		entry, _ := raw.(map[string]any)
		if entry != nil && stringValue(entry["kind"]) == acceptance.ManifestAcceptance {
			registered = entry
		}
	}
	if registered == nil {
		t.Fatalf("successful questionnaire save did not call the existing RecordEvidence path: %s", feedback.String())
	}
	producers, _ := registered["produced_by"].([]any)
	if len(producers) != 1 || producers[0] != "Main" {
		t.Fatalf("missing platform agent_id must record the normal Main producer, got %#v", registered["produced_by"])
	}
	updatedManifest, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved acceptance.Manifest
	if err := json.Unmarshal(updatedManifest, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Metrics != acceptance.DeriveMetrics(saved) {
		t.Fatalf("save did not refresh machine-derived metrics: %#v", saved.Metrics)
	}
	if stringValue(registered["path"]) == "" {
		t.Fatal("recorded evidence has no existing envelope path")
	}

	feedback.Reset()
	if handled, code := runSavedAuditQuestionnaire(root, request, &feedback, &bytes.Buffer{}); !handled || code != 0 {
		t.Fatalf("replay was not handled: handled=%v code=%d feedback=%s", handled, code, feedback.String())
	}
	afterReplay, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if afterReplay.Revision != afterSave.Revision || len(stateEvidence(afterReplay.State)) != len(stateEvidence(afterSave.State)) {
		t.Fatalf("replaying the registered exact answer mutated Runtime: revision %d->%d, evidence %d->%d", afterSave.Revision, afterReplay.Revision, len(stateEvidence(afterSave.State)), len(stateEvidence(afterReplay.State)))
	}
	if !strings.Contains(feedback.String(), absPath) || !strings.Contains(feedback.String(), stringValue(registered["id"])) || strings.Contains(feedback.String(), "not registered") {
		t.Fatalf("replay feedback lost the original accepted path/receipt: %s", feedback.String())
	}
}

func TestSavedAuditQuestionnaireRequiresProfessionalOutcome(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	path, draft, err := prepareAuditQuestionnaire(root, state, acceptance.ManifestAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	for i := range draft.CoverageInventory {
		draft.CoverageInventory[i].Expected = "professional assessment"
		draft.CoverageInventory[i].Oracle = "source and evidence"
		draft.CoverageInventory[i].Owner = "Acceptance Reviewer"
		draft.CoverageInventory[i].EvidenceRefs = []string{"support"}
		draft.CoverageInventory[i].Disposition = "pass"
	}
	for i := range draft.Counterevidence {
		draft.Counterevidence[i].Question = "What would disprove this?"
		draft.Counterevidence[i].EvidenceRefs = []string{"support"}
		draft.Counterevidence[i].Outcome = "pass"
	}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	absPath, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	request := policy.Input{Event: "PostToolUse", ToolName: "Write", CWD: root, ToolInput: map[string]any{"file_path": absPath}}
	var feedback bytes.Buffer
	if handled, _ := runSavedAuditQuestionnaire(root, request, &feedback, &bytes.Buffer{}); !handled {
		t.Fatal("missing-outcome save was not recognized")
	}
	if !strings.Contains(feedback.String(), absPath) || !strings.Contains(feedback.String(), "draft") || !strings.Contains(feedback.String(), "outcome") {
		t.Fatalf("missing outcome feedback must identify the original unfinished draft: %s", feedback.String())
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != integerValue(state["revision"]) {
		t.Fatal("an unanswered outcome wrote Runtime")
	}
	for _, raw := range stateEvidence(snapshot.State) {
		entry, _ := raw.(map[string]any)
		if entry != nil && stringValue(entry["kind"]) == acceptance.ManifestAcceptance {
			t.Fatal("answer without a professional outcome was registered")
		}
	}
}

func TestHumanReleaseQuestionnaireNamesUnverifiablePackage(t *testing.T) {
	root := t.TempDir()
	snapshot := runtime.Snapshot{State: map[string]any{
		"runtime_id": "loop-REQ-7",
		"lifecycle":  map[string]any{"state": "awaiting_human_release"},
		"baseline":   map[string]any{"generation": 3},
		"review":     map[string]any{"round": 5},
		"evidence":   []any{},
	}}
	context := stageHumanReleaseQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	for _, phrase := range []string{"current release audit record is missing", "Main must resolve", "do not reconstruct metadata"} {
		if !strings.Contains(context, phrase) {
			t.Fatalf("S11 missing-package context %q does not contain %q", context, phrase)
		}
	}
}

// auditQuestionnaireRuntime uses a real init Runtime, locked REQ, pinned S7
// ReviewPlan, and fingerprinted completion evidence so S10 uses its normal
// changed-path and inventory authorities in the save/RecordEvidence path.
func auditQuestionnaireRuntime(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := initFormatRoot(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code != 0 {
		t.Fatalf("init fixture: code=%d stderr=%s", code, stderr.String())
	}
	state := initMap(t, root)
	state["runtime_id"] = "loop-audit-questionnaire"
	state["lifecycle"] = map[string]any{"state": "acceptance", "phase": nil, "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-10-15T00:00:00Z"}
	req := []byte("# REQ-901\nProfessional S10 review fixture.\n")
	reqPath := "docs/requirements/REQ-901.md"
	writeQuestionnaireFixture(t, root, reqPath, req)
	boundReq, _ := state["bound_req"].(map[string]any)
	if boundReq == nil {
		boundReq = map[string]any{}
		state["bound_req"] = boundReq
	}
	boundReq["id"], boundReq["path"], boundReq["sha256"] = "REQ-901", reqPath, questionnaireSHA(req)
	boundReq["status"], boundReq["approved_by"], boundReq["approved_at"] = "locked", "review-owner", "2026-10-15T00:00:00Z"
	boundReq["version"] = "v1.0.0"
	boundReq["metadata"] = map[string]any{"ui_impact": "none"}
	contract := []byte("# CONTRACT-QUESTIONNAIRE\n")
	contractPath := "docs/dev/contracts/CONTRACT-QUESTIONNAIRE.md"
	writeQuestionnaireFixture(t, root, contractPath, contract)
	state["documents"] = []any{map[string]any{
		"id": "CONTRACT-QUESTIONNAIRE", "kind": "contract", "path": contractPath,
		"version": "v1.0.0", "sha256": questionnaireSHA(contract), "status": "locked", "generation": 1,
	}}
	plan := []byte(`{"review_plan_id":"review-plan-questionnaire","review_round":1,"baseline_generation":1,"claims":[{"claim_id":"claim-qa-questionnaire"}]}`)
	planPath := ".claude/review/plans/review-plan-questionnaire.json"
	writeQuestionnaireFixture(t, root, planPath, plan)
	review, _ := state["review"].(map[string]any)
	review["round"] = 1
	review["clean_round"] = nil
	review["plan"] = map[string]any{
		"plan_id": "review-plan-questionnaire", "path": planPath, "sha256": questionnaireSHA(plan),
		"revision": 1, "review_round": 1, "status": "running",
		"e2e_coverage_state": "not_applicable", "submitted_at": "2026-10-15T00:00:00Z",
	}
	completion := []byte(`{"changed_paths":["internal/example.go"]}`)
	completionPath := "evidence/questionnaire-completion.json"
	writeQuestionnaireFixture(t, root, completionPath, completion)
	writeQuestionnaireFixture(t, root, "internal/example.go", []byte("package example\n"))
	state["evidence"] = []any{map[string]any{
		"id": "support", "kind": "completion_report", "path": completionPath,
		"sha256": questionnaireSHA(completion), "status": "valid", "baseline_generation": 1,
		"review_round": 1, "produced_by": []any{"Main"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "S7",
		"scope_refs": []any{},
	}}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	return root, state
}

func writeQuestionnaireFixture(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func questionnaireSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func TestAuditQuestionnaireNamesPrefilledBaselineAndTopicSlots(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	for _, tc := range []struct {
		lifecycle string
		want      []string
	}{
		{"acceptance", []string{"system-prefilled", "preserve that baseline"}},
		{"release_audit", []string{"audit_areas[].id", "not_applicable", "risks/technical_debt/blocking_findings"}},
	} {
		state["lifecycle"] = map[string]any{"state": tc.lifecycle, "phase": nil, "phase_revision": 0}
		if _, _, err := prepareAuditQuestionnaire(root, state, tc.lifecycle); err != nil {
			t.Fatal(err)
		}
		context := stageAuditQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
		for _, want := range tc.want {
			if !strings.Contains(context, want) {
				t.Fatalf("%s context omits %q: %s", tc.lifecycle, want, context)
			}
		}
	}
}
