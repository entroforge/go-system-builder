package investigation_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type intakeFixture struct {
	hookBinary  string // Optional production-process platform acceptance path.
	root        string
	statePath   string
	journalPath string
	findingIDs  []string
}

func TestIngestRequiresActualFindingOrHumanReport(t *testing.T) {
	fixture := newIntakeFixture(t, nil)
	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "investigate observed defect"})
	if err == nil || !strings.Contains(err.Error(), "Finding or actual human defect") {
		t.Fatalf("missing source error: %v", err)
	}
}

func TestIngestUsesWriterRevisionWhenExpectedRuntimeRevisionIsOmitted(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "the normal S8 path lets the Writer assign the Runtime commit sequence",
	})
	if err != nil {
		t.Fatalf("Ingest() without Runtime revision error = %v", err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("Runtime revision = %d, want Writer-assigned revision 1", snapshot.Revision)
	}
}

func TestIngestCreatesCaseAndCommitsInvestigationPointer(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-2", "finding-1"})

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "same sealed batch is the provisional grouping boundary",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1", snapshot.Revision)
	}

	review := snapshot.State["review"].(map[string]any)
	pointer := review["investigation"].(map[string]any)
	if pointer["status"] != "investigating" {
		t.Fatalf("investigation status = %v, want investigating", pointer["status"])
	}
	casePath, _ := pointer["path"].(string)
	caseData, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(casePath)))
	if err != nil {
		t.Fatalf("read InvestigationCase: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseData); err != nil {
		t.Fatalf("InvestigationCase schema: %v", err)
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseData, &caseDocument); err != nil {
		t.Fatal(err)
	}
	if caseDocument["status"] != "investigating" || caseDocument["grouping_rationale"] != "same sealed batch is the provisional grouping boundary" {
		t.Fatalf("unexpected case document: %#v", caseDocument)
	}
	if got := caseDocument["route"]; got != nil {
		t.Fatalf("route = %v, want null", got)
	}
	if got := caseDocument["primary_root_cause"]; got != nil {
		t.Fatalf("primary_root_cause = %v, want null", got)
	}
	if got := caseDocument["unexplained_finding_ids"].([]any); len(got) != 2 || got[0] != "finding-1" || got[1] != "finding-2" {
		t.Fatalf("unexplained_finding_ids = %#v, want sorted exact set", got)
	}

	event := readLastJournalEvent(t, fixture.journalPath)
	if event["event"] != "transition_committed" || event["transition_id"] != "INVESTIGATION-CASE-INGESTED" {
		t.Fatalf("journal event = %#v", event)
	}
	if !strings.Contains(event["message"].(string), "investigation_case_ingested") {
		t.Fatalf("journal message does not identify domain event: %#v", event)
	}
	if _, ok := snapshot.State["entities"].(map[string]any)["bugs"]; !ok {
		t.Fatal("runtime state must retain BUG entity collection")
	}
}

// G18 §1 reverse assertion: the ingest owns its own event identity, and its
// idempotency key names the Case responsibility and the source intent that
// opened it — never the Runtime write count. The ingest must therefore not
// share one event identity with a later Case revision of the same Case.
func TestIngestEventIdentityNamesTheCaseNotTheRuntimeWriteCount(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision: -1, GroupingRationale: "one sealed Finding enters one bounded Case",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	pointer := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)
	caseID, _ := pointer["case_id"].(string)
	event := readLastJournalEvent(t, fixture.journalPath)
	eventID, _ := event["event_id"].(string)
	key, _ := event["idempotency_key"].(string)
	if eventID != "evt-investigation-case-ingested-"+caseID {
		t.Fatalf("ingest event id = %q, want the ingest identity of Case %s", eventID, caseID)
	}
	if !strings.HasPrefix(key, "runtime:investigation-case-ingested:"+caseID+":") {
		t.Fatalf("ingest idempotency key = %q, want the Case responsibility and its source intent", key)
	}
	if strings.HasSuffix(key, fmt.Sprintf(":%d", snapshot.Revision)) {
		t.Fatalf("ingest idempotency key = %q, want no Runtime write count", key)
	}
}

// setIntakeLifecycle overrides the fixture lifecycle cursor. The RC-18
// phase-gate test uses it to move the cursor back to verification.running.
func setIntakeLifecycle(t *testing.T, fixture *intakeFixture, state, phase string) {
	t.Helper()
	data, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["lifecycle"] = map[string]any{"state": state, "phase": phase, "phase_revision": 0}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIngestRejectsWrongLifecycleCursor covers the RC-18 phase gate: ingest is
// only legal at bug_resolution.investigation (TR-008 or a human defect
// decision), so a batch pointer that survives into another cursor — or a
// replay after route consume left the phase — must fail closed with a
// phase-specific recovery hint, never with the ingest self-loop.
func TestIngestRejectsWrongLifecycleCursor(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	setIntakeLifecycle(t, fixture, "verification", "running")

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "the cursor does not authorize S8 intake yet",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a verification cursor")
	}
	assertErrorMentions(t, err, "bug_resolution.investigation", "verification.running", "actual accepted Finding handoff")
	if strings.Contains(err.Error(), "state.review") {
		t.Errorf("phase-gate error must describe the lifecycle cursor, not the batch pointer: %q", err.Error())
	}
}

// TestIngestAllowsBugResolutionInvestigationCursor proves the phase gate admits
// exactly the cursor TR-008 opens and that the ingested Case projects the
// sealed batch's claim-coverage facts into the boundary views (RC-18 S8-M1).
func TestIngestAllowsBugResolutionInvestigationCursorAndProjectsBatchViews(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-2", "finding-1"})
	setIntakeLifecycle(t, fixture, "bug_resolution", "investigation")

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "TR-008 opened S8 with an exact Finding set",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	caseRel := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)["path"].(string)
	caseData := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(caseRel)))
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseData); err != nil {
		t.Fatalf("InvestigationCase schema: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(caseData, &document); err != nil {
		t.Fatal(err)
	}
	// failure_boundary_refs: one finding:<id> ref per source Finding.
	boundaries := document["failure_boundary_refs"].([]any)
	if len(boundaries) != 2 || boundaries[0] != "finding:finding-1" || boundaries[1] != "finding:finding-2" {
		t.Fatalf("failure_boundary_refs = %#v, want sorted finding refs", boundaries)
	}
	// evidence_gaps: an array (never JSON null) even when nothing is blocked.
	if _, isArray := document["evidence_gaps"].([]any); !isArray {
		t.Fatalf("evidence_gaps = %#v, want an array", document["evidence_gaps"])
	}
}

// G18 §3.1/§4.5 reverse assertion: the Runtime write count is not a domain
// gate on the professional completion. A caller that omits the expectation
// lets the Writer resolve the current value, so an intake whose Runtime moved
// for an unrelated reason still completes. The G18 §2 received-row clause
// still holds: the same Case is never received twice, and the retried intent
// writes nothing.
func TestIngestCompletesWhenTheRuntimeMovedAndTheCallerOmittedTheWriteCount(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	moveIntakeRuntimeForAnUnrelatedReason(t, fixture)

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "an unrelated Runtime move must not refuse the S8 intake",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	pointer := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)
	caseID, _ := pointer["case_id"].(string)
	eventID, _ := readLastJournalEvent(t, fixture.journalPath)["event_id"].(string)
	if eventID != "evt-investigation-case-ingested-"+caseID {
		t.Fatalf("ingest event id = %q, want the ingest identity of Case %s", eventID, caseID)
	}

	stateBefore, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "the same Case must not be received twice",
	})
	if !errors.Is(err, investigation.ErrAlreadyIngested) {
		t.Fatalf("second Ingest() error = %v, want ErrAlreadyIngested", err)
	}
	stateAfter, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("a refused second ingest wrote the Runtime or the journal")
	}
}

// G18 §3.3 reverse assertion: the explicit caller binding stays a mechanism.
// A caller that names a revision it no longer holds is refused by the Runtime
// CAS, and the refused intake leaves no Case behind — no Case artifact, no
// Case directory and no Runtime write.
func TestIngestRefusesAnExplicitStaleRevisionWithoutLeavingACaseBehind(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  7,
		GroupingRationale: "a caller that names a revision it no longer holds is refused",
	})
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("error = %v, want ErrStaleRevision", err)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.root, ".claude/review/investigation/cases")); !os.IsNotExist(statErr) {
		t.Fatalf("refused ingest must not create case directory, stat error = %v", statErr)
	}
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 {
		t.Fatalf("Runtime revision = %d, want the refused ingest to write no Runtime revision", snapshot.Revision)
	}
	if review, _ := snapshot.State["review"].(map[string]any); review["investigation"] != nil {
		t.Fatalf("refused ingest must not point the Runtime at a Case: %#v", review["investigation"])
	}
}

// moveIntakeRuntimeForAnUnrelatedReason commits a Runtime mutation that has
// nothing to do with the S8 intake, so the Runtime write count moves on.
func moveIntakeRuntimeForAnUnrelatedReason(t *testing.T, fixture *intakeFixture) {
	t.Helper()
	_, err := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{}).UpdateCurrent(runtime.Mutation{
		EventID: "evt-intake-unrelated-runtime-move", TransitionID: "MILESTONE-REFRESH", Event: "milestone_refreshed",
		Actor: "Main", IdempotencyKey: "intake-unrelated-runtime-move", RetainLastTransition: true,
	})
	if err != nil {
		t.Fatalf("unrelated Runtime move: %v", err)
	}
}

func TestIngestRejectsSecondIngestAsExplicitIdempotentConflict(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	request := investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "single finding remains a provisional case",
	}
	if _, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, request); err != nil {
		t.Fatalf("first Ingest() error = %v", err)
	}

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: request.GroupingRationale,
	})
	if err == nil {
		t.Fatal("second Ingest() must return an explicit idempotent conflict")
	}
	if !errors.Is(err, investigation.ErrAlreadyIngested) {
		t.Fatalf("second Ingest() error = %v, want ErrAlreadyIngested", err)
	}
	assertErrorMentions(t, err, "already", "Case")
}

func newIntakeFixture(t *testing.T, pointerIDs []string) *intakeFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Join("..", "..")
	for _, name := range []string{"loop-definition.json", "hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot, "docs", "control", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "docs", "control", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-INTAKE"
	state["revision"] = 0
	// The default fixture cursor is the one TR-008 opens: bug_resolution.
	// investigation. Tests that need a different cursor override it through
	// setIntakeLifecycle (e.g. the RC-18 phase-gate rejection test below).
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "investigation", "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 3, "captured_at": "2026-08-25T00:00:00Z"}
	state["review"] = map[string]any{
		"round_entry": map[string]any{"transition_id": "TR-006", "plan_id": "review-plan-r1", "baseline_generation": 3, "change_impact_ref": nil},
	}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["last_transition"] = nil
	state["updated_at"] = "2026-08-25T00:00:00Z"
	if pointerIDs != nil {
		entities := state["entities"].(map[string]any)
		rows := make([]any, 0, len(pointerIDs))
		for index, findingID := range pointerIDs {
			findingBody := map[string]any{
				"schema_version": "1.0.0", "finding_id": findingID, "claim_id": fmt.Sprintf("claim-intake-%d", index+1),
				"lens": "qa", "severity": "P1", "expected": "the expected behavior holds", "authority_refs": []string{"REQ-INTAKE"},
				"observed": "the observed behavior deviates", "observation_mode": "code_inspection",
				"encounter":       map[string]any{"journey_summary": "inspect -> trace -> observe deviation", "inspection_entry": "internal/example", "symbol_trail": "entry -> boundary", "wall_action": "inspect the boundary", "first_bad_checkpoint": "invariant is false", "terminal_state": "review stopped"},
				"reproducibility": "always", "evidence_refs": []string{"evidence://intake"},
			}
			findingBytes, err := json.MarshalIndent(findingBody, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			findingBytes = append(findingBytes, '\n')
			findingRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "findings", findingID+".json"))
			findingPath := filepath.Join(root, filepath.FromSlash(findingRel))
			if err := os.MkdirAll(filepath.Dir(findingPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(findingPath, findingBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, map[string]any{"finding_id": findingID, "path": findingRel, "review_plan_id": "review-plan-r1", "claim_id": findingBody["claim_id"], "assignment_id": "assignment-intake", "lens": "qa", "severity": "P1", "observation_mode": "code_inspection", "original_finder": "agent-intake", "created_at": "2026-08-25T00:00:00Z"})
		}
		entities["findings"] = rows
	}
	definitionSHA := fileSHA(t, filepath.Join(root, "docs/control/loop-definition.json"))
	hookSHA := fileSHA(t, filepath.Join(root, "docs/control/hook-policy.json"))
	state["definition"].(map[string]any)["sha256"] = definitionSHA
	state["hook_control"].(map[string]any)["policy_ref"].(map[string]any)["sha256"] = hookSHA
	statePath := filepath.Join(root, ".claude/loop-state.json")
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return &intakeFixture{root: root, statePath: statePath, journalPath: journalPath, findingIDs: pointerIDs}
}

// setFixtureAcceptedFindings associates the existing original Findings with the current Plan.
func setFixtureAcceptedFindings(t *testing.T, fixture *intakeFixture, findingIDs []string, baselineGeneration int) {
	t.Helper()
	data, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	review := state["review"].(map[string]any)
	review["round_entry"] = map[string]any{"transition_id": "TR-006", "plan_id": "review-plan-r1", "baseline_generation": baselineGeneration, "change_impact_ref": nil}
	for _, raw := range state["entities"].(map[string]any)["findings"].([]any) {
		raw.(map[string]any)["review_plan_id"] = "review-plan-r1"
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(encoded, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	fixture.findingIDs = findingIDs
}

func assertErrorMentions(t *testing.T, err error, terms ...string) {
	t.Helper()
	message := err.Error()
	for _, term := range terms {
		if !strings.Contains(message, term) {
			t.Errorf("error %q does not contain %q", message, term)
		}
	}
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash(data)
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readLastJournalEvent(t *testing.T, path string) map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var event map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("journal is empty")
	}
	return event
}
