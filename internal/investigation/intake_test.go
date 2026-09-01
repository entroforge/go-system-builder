package investigation_test

import (
	"bufio"
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
)

type intakeFixture struct {
	root        string
	statePath   string
	journalPath string
	batchRel    string
	batchSHA    string
	batchIDs    []string
}

func TestIngestRejectsMissingObservationBatchWithRecoveryCommand(t *testing.T) {
	fixture := newIntakeFixture(t, nil)

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "one sealed batch enters one provisional case",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a missing observation batch")
	}
	assertErrorMentions(t, err, "observation_batch", "s7 status --explain")
	if strings.Contains(err.Error(), "runtime investigation ingest") {
		t.Errorf("no-batch error must not point back at this ingest command (RC-18 F-H1): %q", err.Error())
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

func TestIngestRejectsObservationBatchHashDrift(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	setObservationBatchPointer(t, fixture, "0"+strings.Repeat("0", 63))

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "single finding remains a provisional case",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a drifted observation batch")
	}
	assertErrorMentions(t, err, "sha256", "runtime investigation ingest")
}

func TestIngestRejectsFindingArtifactHashDrift(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	findingPath := filepath.Join(fixture.root, ".claude/evidence/findings/finding-1.json")
	if err := os.WriteFile(findingPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "a Finding must remain bound to its immutable artifact",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a drifted Finding artifact")
	}
	assertErrorMentions(t, err, "Finding finding-1", "sha256", "runtime investigation ingest")
}

func TestIngestRejectsFindingArtifactSchemaDrift(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	findingPath := filepath.Join(fixture.root, ".claude/evidence/findings/finding-1.json")
	invalid := []byte(`{"finding_id":"finding-1"}` + "\n")
	if err := os.WriteFile(findingPath, invalid, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBytes, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["findings"].([]any)[0].(map[string]any)["sha256"] = hash(invalid)
	stateBytes, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "a Finding must satisfy the canonical Finding schema before S8 consumes it",
	})
	if err == nil {
		t.Fatal("Ingest() must reject an invalid Finding artifact")
	}
	assertErrorMentions(t, err, "finding.schema.json", "runtime investigation ingest")
}

func TestIngestRejectsObservationBatchFindingSetMismatch(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1", "finding-2"})
	writeObservationBatch(t, fixture, []string{"finding-1", "finding-3"}, 3)

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "findings are provisionally investigated together",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a non-exact Finding set")
	}
	assertErrorMentions(t, err, "exact", "finding-2", "finding-3", "runtime investigation ingest")
}

func TestIngestRejectsObservationBatchBaselineMismatch(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	writeObservationBatch(t, fixture, []string{"finding-1"}, 2)

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "baseline mismatch must not enter investigation",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a baseline generation mismatch")
	}
	assertErrorMentions(t, err, "baseline_generation", "runtime investigation ingest")
}

func TestIngestCreatesCaseAndCommitsInvestigationPointer(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-2", "finding-1"})

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
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
	if pointer["observation_batch_id"] != "observation-batch-r1" {
		t.Fatalf("observation_batch_id = %v", pointer["observation_batch_id"])
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
		ExpectedRevision:  0,
		GroupingRationale: "the cursor does not authorize S8 intake yet",
	})
	if err == nil {
		t.Fatal("Ingest() must reject a verification cursor")
	}
	assertErrorMentions(t, err, "bug_resolution.investigation", "verification.running", "runtime investigation status")
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
		ExpectedRevision:  0,
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
	if got := document["baseline_digest"]; got != strings.Repeat("a", 64) {
		t.Fatalf("baseline_digest = %v, want the sealed subject digest", got)
	}
}

// TestIngestProjectsBlockedClaimsIntoEvidenceGaps proves blocked Claims and
// unobserved Claims land in evidence_gaps with their preconditions, and that
// blocked evidence refs join failure_boundary_refs.
func TestIngestProjectsBlockedClaimsIntoEvidenceGaps(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	// Re-seal the batch with one blocked claim and one unobserved claim.
	batchPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.batchRel))
	data := mustRead(t, batchPath)
	var batch map[string]any
	if err := json.Unmarshal(data, &batch); err != nil {
		t.Fatal(err)
	}
	batch["claim_coverage_summary"].(map[string]any)["blocked"] = 1
	batch["claim_coverage_summary"].(map[string]any)["blocked_claims"] = []any{
		map[string]any{
			"claim_id":              "claim-intake-1",
			"blocking_finding_ids":  []any{"finding-1"},
			"failed_precondition":   map[string]any{"kind": "build", "detail": "the module does not compile in the verification workspace"},
			"evidence_refs":         []any{"evidence://intake-build-log"},
			"after_repair_required": true,
			"result_id":             "review-result-r1",
		},
	}
	batch["unobserved_claim_ids"] = []any{"claim-intake-9"}
	sealed, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	sealed = append(sealed, '\n')
	if err := os.WriteFile(batchPath, sealed, 0o644); err != nil {
		t.Fatal(err)
	}
	setObservationBatchPointer(t, fixture, hash(sealed))
	setIntakeLifecycle(t, fixture, "bug_resolution", "investigation")

	snapshot, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "blocked claims must surface as investigation evidence gaps",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	caseRel := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)["path"].(string)
	var document map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(caseRel))), &document); err != nil {
		t.Fatal(err)
	}
	gaps := document["evidence_gaps"].([]any)
	if len(gaps) != 2 {
		t.Fatalf("evidence_gaps = %#v, want one blocked + one unobserved entry", gaps)
	}
	blocked, _ := gaps[0].(string)
	if !strings.Contains(blocked, "claim-intake-1") || !strings.Contains(blocked, "finding-1") || !strings.Contains(blocked, "build") {
		t.Fatalf("blocked gap = %q, want claim/finding/precondition projection", blocked)
	}
	unobserved, _ := gaps[1].(string)
	if !strings.Contains(unobserved, "claim-intake-9") {
		t.Fatalf("unobserved gap = %q, want the unobserved claim projection", unobserved)
	}
	boundaries := document["failure_boundary_refs"].([]any)
	found := false
	for _, raw := range boundaries {
		if ref, _ := raw.(string); ref == "evidence://intake-build-log" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failure_boundary_refs = %#v, want the blocked claim's evidence ref", boundaries)
	}
}

func TestIngestRejectsStaleRevisionBeforeCreatingCase(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  7,
		GroupingRationale: "stale caller must not create an artifact",
	})
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("error = %v, want ErrStaleRevision", err)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.root, ".claude/review/investigation/cases")); !os.IsNotExist(statErr) {
		t.Fatalf("stale ingest must not create case directory, stat error = %v", statErr)
	}
}

func TestIngestRejectsSecondIngestAsExplicitIdempotentConflict(t *testing.T) {
	fixture := newIntakeFixture(t, []string{"finding-1"})
	request := investigation.IngestRequest{
		ExpectedRevision:  0,
		GroupingRationale: "single finding remains a provisional case",
	}
	if _, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, request); err != nil {
		t.Fatalf("first Ingest() error = %v", err)
	}

	_, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  1,
		GroupingRationale: request.GroupingRationale,
	})
	if err == nil {
		t.Fatal("second Ingest() must return an explicit idempotent conflict")
	}
	assertErrorMentions(t, err, "already", "idempotent", "runtime investigation status")
}

func newIntakeFixture(t *testing.T, pointerIDs []string) *intakeFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Join("..", "..")
	for _, name := range []string{"loop-definition.json", "hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot, "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "docs", name), data, 0o644); err != nil {
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
		"round":             1,
		"clean_round":       nil,
		"observation_batch": nil,
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
			rows = append(rows, map[string]any{"finding_id": findingID, "path": findingRel, "sha256": hash(findingBytes), "claim_id": findingBody["claim_id"], "assignment_id": "assignment-intake", "lens": "qa", "severity": "P1", "observation_mode": "code_inspection", "original_finder": "agent-intake", "review_round": 1, "created_at": "2026-08-25T00:00:00Z"})
		}
		entities["findings"] = rows
	}
	definitionSHA := fileSHA(t, filepath.Join(root, "docs/loop-definition.json"))
	hookSHA := fileSHA(t, filepath.Join(root, "docs/hook-policy.json"))
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
	fixture := &intakeFixture{root: root, statePath: statePath, journalPath: journalPath, batchIDs: pointerIDs}
	if pointerIDs != nil {
		writeObservationBatch(t, fixture, pointerIDs, 3)
	}
	return fixture
}

func writeObservationBatch(t *testing.T, fixture *intakeFixture, findingIDs []string, baselineGeneration int) {
	t.Helper()
	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": "observation-batch-r1", "conclusion": "sealed",
		"evidence_id": "observation-batch-r1", "kind": "observation_batch", "runtime_id": "loop-REQ-INTAKE",
		"producer_agent_id": "round-consumer", "producer_responsibility": "Orchestrator", "review_plan_id": "review-plan-r1",
		"review_round": 1, "baseline_generation": baselineGeneration, "subject_digest": strings.Repeat("a", 64),
		"finding_ids": findingIDs, "drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{"total_required": 1, "pass": 0, "finding": len(findingIDs), "not_applicable": 0, "blocked": 0, "blocked_claims": []any{}, "plan_revision": 1},
		"unobserved_claim_ids":   []any{}, "original_finder_routes": []any{}, "investigation_readiness": readiness(findingIDs),
		"sealed_at": "2026-08-25T00:00:00Z", "sealed_by": "round-consumer", "revision": 1,
	}
	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fixture.batchRel = ".claude/evidence/observation-batch-r1.json"
	abs := filepath.Join(fixture.root, filepath.FromSlash(fixture.batchRel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.batchSHA = hash(data)
	setObservationBatchPointer(t, fixture, fixture.batchSHA)
}

func setObservationBatchPointer(t *testing.T, fixture *intakeFixture, sha string) {
	t.Helper()
	stateData, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	ids := make([]any, 0, len(fixture.batchIDs))
	for _, id := range fixture.batchIDs {
		ids = append(ids, id)
	}
	state["review"].(map[string]any)["observation_batch"] = map[string]any{
		"batch_id": "observation-batch-r1", "path": fixture.batchRel, "sha256": sha, "finding_ids": ids,
		"drain_policy": "complete_required_claims", "sealed_at": "2026-08-25T00:00:00Z",
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readiness(ids []string) []any {
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{"finding_id": id, "status": "ready"})
	}
	return items
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
