package review

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// ---------------------------------------------------------------------------
// FindingSupplement (L3-S7 §3.6, L3-S7 §14.1, L3-S8 §2.1-2.2)
//
// These tests build the fixture state directly (without invoking RegisterPlan
// or SubmitResult) so that they exercise only the supplement path and stay
// independent of sibling-review wiring churn. SubmitSupplement reads entities
// for finding rows and review.round; the helper seeds both.
// ---------------------------------------------------------------------------

// supplementFixture mutates baseVerificationState into a state where
// finding-qa-1 (author agent-qa-1, round 1) is already pre-registered under
// entities.findings. The review.assignments projection is intentionally
// empty: SubmitSupplement does not read it. Returns (statePath, journalPath)
// and the seeded snapshot whose Revision is the baseline SubmitSupplement
// will use as ExpectedRevision.
func supplementFixture(t *testing.T, root string) (string, string, loopruntime.Snapshot) {
	t.Helper()
	statePath, journalPath := writeState(t, root, baseVerificationState())

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	entities["findings"] = []any{
		map[string]any{
			"finding_id":       "finding-qa-1",
			"path":             ".claude/evidence/loop-REQ-TEST/g1/findings/finding-qa-1.json",
			"sha256":           strings.Repeat("f", 64),
			"claim_id":         "claim-qa-1",
			"assignment_id":    "assignment-qa-1",
			"lens":             "qa",
			"severity":         "P1",
			"observation_mode": "code_inspection",
			"original_finder":  "agent-qa-1",
			"review_round":     1,
			"created_at":       "2026-08-19T00:00:00Z",
		},
	}
	// Persist the matching finding artifact so the filesystem shape matches a
	// real round and any future reader can fingerprint it (the supplement's
	// own sha256 chain does not depend on this hash).
	artifactDir := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "findings")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(artifactDir, "finding-qa-1.json")
	if err := os.WriteFile(artifactPath, []byte(`{"finding_id":"finding-qa-1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return statePath, journalPath, loopruntime.Snapshot{Revision: 1, State: state}
}

// supplementBody returns a valid FindingSupplement document (without hash)
// carrying the complete S8 discriminator binding. Pass `overrides` to make
// a specific field empty/partial for the gate tests.
func supplementBody(findingID, supplementID, author string, overrides ...func(map[string]any)) map[string]any {
	body := map[string]any{
		"schema_version":         "1.0.0",
		"supplement_id":          supplementID,
		"supplements_finding_id": findingID,
		"author":                 author,
		"new_observation":        "discriminator probe: the deviation only appears when the caller passes a nil store",
		"evidence_refs":          []string{"ev/probe-1.md"},
		"correlation_refs":       []string{"trace/abc123"},
		"discriminator":          "nil vs empty store distinguishes H1 from H2",
		"hypothesis_id":          "hyp-1",
		"expected_outcomes":      []string{"nil store panics", "empty store returns ErrEmpty"},
		"created_at":             "2026-08-20T00:00:00Z",
	}
	for _, mutate := range overrides {
		mutate(body)
	}
	return body
}

// writeSupplementFile computes the content hash (compact sorted-keys JSON
// without the hash field) and writes the document to disk.
func writeSupplementFile(t *testing.T, root string, body map[string]any) string {
	t.Helper()
	hashBody, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(hashBody)
	body["hash"] = fmt.Sprintf("%x", sum[:])
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, body["supplement_id"].(string)+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func findingArtifactPath(root, findingID string) string {
	return filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "findings", findingID+".json")
}

// TestSubmitSupplementHappyPath: full discriminator-bound path verifies the
// index row hangs under the Finding, the artifact lands next to its
// evidence file, and the main-state CAS transaction bumps the revision
// while the immutable Finding byte-for-byte identity stays intact.
func TestSubmitSupplementHappyPath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, snap := supplementFixture(t, root)
	prevRevision := snap.Revision

	findingBytes, err := os.ReadFile(findingArtifactPath(root, "finding-qa-1"))
	if err != nil {
		t.Fatal(err)
	}

	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1"))
	receipt, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err != nil {
		t.Fatalf("SubmitSupplement: %v", err)
	}
	if receipt.SupplementID != "supplement-qa-1" || receipt.FindingID != "finding-qa-1" {
		t.Fatalf("receipt identity wrong: %+v", receipt)
	}
	if receipt.Revision != prevRevision+1 {
		t.Fatalf("revision = %d, want %d (one CAS transaction for the supplement append)", receipt.Revision, prevRevision+1)
	}

	// Index row appended and readable.
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SupplementID != "supplement-qa-1" || rows[0].Author != "agent-qa-1" {
		t.Fatalf("supplement rows wrong: %+v", rows)
	}
	if rows[0].HypothesisID != "hyp-1" || rows[0].Discriminator == "" {
		t.Fatalf("supplement row coordinates wrong: %+v", rows[0])
	}
	if rows[0].BaselineGeneration != 1 {
		t.Fatalf("supplement row baseline_generation = %d, want 1 (mirrors the runtime baseline.generation)", rows[0].BaselineGeneration)
	}

	// Artifact persists without registering a machine digest.
	artifactBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt.Path)))
	if err != nil {
		t.Fatalf("supplement artifact missing: %v", err)
	}
	if len(artifactBytes) == 0 {
		t.Fatal("missing original artifact")
	}

	// Immutable Finding artifact byte-for-byte identity holds after supplement.
	if after, _ := os.ReadFile(findingArtifactPath(root, "finding-qa-1")); string(after) != string(findingBytes) {
		t.Fatal("Finding artifact bytes changed — Findings are immutable")
	}

	// The supplement index lives in entities.finding_supplements (CAS-managed),
	// and the runtime CAS also appended a finding_supplement evidence entry.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if entities, _ := state["entities"].(map[string]any); entities == nil {
		t.Fatal("entities missing")
	}
	evidence, _ := state["evidence"].([]any)
	foundEntry := false
	for _, raw := range evidence {
		entry, _ := raw.(map[string]any)
		if entry != nil && entry["kind"] == "finding_supplement" && entry["id"] == "supplement-qa-1" {
			foundEntry = true
		}
	}
	if !foundEntry {
		t.Fatalf("finding_supplement evidence entry missing from runtime evidence[]: %+v", evidence)
	}
}

// TestSubmitSupplementRejectsUnknownFinding covers both halves of the
// unknown-finding failure mode (supplements_finding_id mismatch + missing
// finding row).
func TestSubmitSupplementRejectsUnknownFinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1"))
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-missing", FilePath: supplementPath,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected supplements_finding_id/--finding mismatch, got %v", err)
	}

	body := supplementBody("finding-missing", "supplement-qa-2", "agent-qa-1")
	supplementPath = writeSupplementFile(t, root, body)
	_, err = SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-missing", FilePath: supplementPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unknown finding rejection, got %v", err)
	}
}

// The original Finding remains usable without numeric review-round gating.
func TestSubmitSupplementIgnoresLegacyFindingRound(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	// Move the runtime to round 2 (repair loop starts a new round); the
	// round-1 finding is now out of scope for new supplements.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["round"] = 2
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1"))
	_, err = SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err != nil {
		t.Fatalf("legacy round value blocked the original Finding: %v", err)
	}
}

// The original expression replays; the same ID cannot overwrite a distinct observation.
func TestSubmitSupplementReplaysOriginalAndRejectsConflictingID(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1"))
	if _, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	}); err != nil {
		t.Fatalf("first SubmitSupplement: %v", err)
	}
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err != nil {
		t.Fatalf("same original observation should replay: %v", err)
	}
	changed := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1")
	changed["new_observation"] = "a different professional expression"
	supplementPath = writeSupplementFile(t, root, changed)
	_, err = SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: supplementPath})
	if err == nil || !strings.Contains(err.Error(), "different original expression") {
		t.Fatalf("same ID with different content must not overwrite the original: %v", err)
	}
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("duplicate append leaked into the index: %+v", rows)
	}
}

// TestSubmitSupplementAuthorBinding: non-original-finder authors are
// rejected unless a scheduler-appointed replacement is recorded.
func TestSubmitSupplementAuthorBinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	// A different author without scheduler authorization is rejected.
	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-dv-1"))
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not the original finder") {
		t.Fatalf("expected authorship rejection, got %v", err)
	}

	// A scheduler-authorized replacement finder (L3-S8 §2.2) is accepted and
	// the authorization is recorded on the row.
	supplementPath = writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-dv-1"))
	receipt, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath, AuthorizedBy: "scheduler-1",
	})
	if err != nil {
		t.Fatalf("authorized replacement SubmitSupplement: %v", err)
	}
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Author != "agent-dv-1" || rows[0].AuthorizedBy != "scheduler-1" {
		t.Fatalf("authorized replacement row wrong: %+v (receipt %+v)", rows, receipt)
	}
}

// A historical incorrect hash is ignored rather than becoming a qualification.
func TestSubmitSupplementIgnoresHistoricalHashMismatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1")
	body["hash"] = strings.Repeat("0", 64)
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "supplement-bad-hash.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: path,
	})
	if err != nil {
		t.Fatalf("historical hash blocked original observation: %v", err)
	}
}

// Extra professional context in the author’s observation must not be a gate.
func TestSubmitSupplementAcceptsAdditionalProfessionalContext(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1")
	body["root_cause"] = "the store interface is wrong"
	body["suggested_fix"] = "nil-check the caller"
	supplementPath := writeSupplementFile(t, root, body)
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err != nil {
		t.Fatalf("additional professional context blocked the observation: %v", err)
	}
}

// TestSubmitSupplementGateMissingHypothesis exercises the discriminator gate
// (L3-S7 §14.1, L3-S8 §2.2): without --in-round-note, a supplement missing
// hypothesis_id is rejected as a request to re-run an already confirmed
// symptom, with the actionable next-step error message.
func TestSubmitSupplementGateMissingHypothesis(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1",
		func(b map[string]any) { delete(b, "hypothesis_id") },
	)
	supplementPath := writeSupplementFile(t, root, body)
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err == nil {
		t.Fatal("expected gate rejection, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "hypothesis_id") || !strings.Contains(msg, "discriminator") ||
		!strings.Contains(msg, "expected_outcomes") || !strings.Contains(msg, "--in-round-note") {
		t.Fatalf("rejection must list the discriminator binding and the exemption flag, got %v", err)
	}
}

// TestSubmitSupplementGateMissingDiscriminator rejects when the only field
// missing is discriminator.
func TestSubmitSupplementGateMissingDiscriminator(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1",
		func(b map[string]any) { delete(b, "discriminator") },
	)
	supplementPath := writeSupplementFile(t, root, body)
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err == nil || !strings.Contains(err.Error(), "discriminator") {
		t.Fatalf("expected discriminator gate rejection, got %v", err)
	}
}

// TestSubmitSupplementGateMissingOutcomes rejects when expected_outcomes is
// omitted; a follow-up observation without distinguishing outcomes cannot
// tell hypothesis A from hypothesis B.
func TestSubmitSupplementGateMissingOutcomes(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1",
		func(b map[string]any) { delete(b, "expected_outcomes") },
	)
	supplementPath := writeSupplementFile(t, root, body)
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID: "finding-qa-1", FilePath: supplementPath,
	})
	if err == nil || !strings.Contains(err.Error(), "expected_outcomes") {
		t.Fatalf("expected expected_outcomes gate rejection, got %v", err)
	}
}

// TestSubmitSupplementInRoundNoteExempt: an S7 in-round observation from
// the original finder that does not bind a discriminator is accepted when
// declared via --in-round-note, and the row records no hypothesis/discriminator.
func TestSubmitSupplementInRoundNoteExempt(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	body := supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1",
		func(b map[string]any) {
			delete(b, "hypothesis_id")
			delete(b, "discriminator")
			delete(b, "expected_outcomes")
			b["new_observation"] = "extra QA observation while round is still open"
		},
	)
	supplementPath := writeSupplementFile(t, root, body)
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID:   "finding-qa-1",
		FilePath:    supplementPath,
		InRoundNote: true,
	})
	if err != nil {
		t.Fatalf("in-round note SubmitSupplement: %v", err)
	}
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one in-round row, got %+v", rows)
	}
	if rows[0].HypothesisID != "" || rows[0].Discriminator != "" {
		t.Fatalf("in-round row must not carry discriminator binding, got %+v", rows[0])
	}
}

// TestSubmitSupplementInRoundNoteRejectsHypothesis guards the in-round
// exemption: the flag is a declaration of "not a discriminator answer" and
// must not coexist with hypothesis_id (L3-S8 §2.2).
func TestSubmitSupplementInRoundNoteRejectsHypothesis(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	// hypothesis_id + --in-round-note is contradictory: submit says "I am not
	// answering a discriminator" while carrying a hypothesis_id.
	supplementPath := writeSupplementFile(t, root, supplementBody("finding-qa-1", "supplement-qa-1", "agent-qa-1"))
	_, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{
		FindingID:   "finding-qa-1",
		FilePath:    supplementPath,
		InRoundNote: true,
	})
	if err == nil || !strings.Contains(err.Error(), "hypothesis_id") {
		t.Fatalf("expected in-round-note + hypothesis_id rejection, got %v", err)
	}
}

func TestSupplementUsesRuntimeAndLeavesUnrelatedOldFileAlone(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)
	oldPath := filepath.Join(root, ".claude", "review", "finding-supplements.json")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0700); err != nil {
		t.Fatal(err)
	}
	const unrelated = "{unrelated original bytes"
	if err := os.WriteFile(oldPath, []byte(unrelated), 0600); err != nil {
		t.Fatal(err)
	}
	body := supplementBody("finding-qa-1", "supplement-original", "agent-qa-1", func(b map[string]any) {
		delete(b, "hypothesis_id")
		delete(b, "discriminator")
		delete(b, "expected_outcomes")
		b["new_observation"] = "the original observed symptom continues"
	})
	path := writeSupplementFile(t, root, body)
	receipt, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: path, InRoundNote: true})
	if err != nil {
		t.Fatalf("unrelated old file blocked current observation: %v", err)
	}
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil || len(rows) != 1 || rows[0].SupplementID != receipt.SupplementID {
		t.Fatalf("Runtime observation missing: %+v %v", rows, err)
	}
	if got, err := os.ReadFile(oldPath); err != nil || string(got) != unrelated {
		t.Fatalf("unrelated file changed: %q %v", got, err)
	}
}

// TestSubmitSupplementManualEvidenceKindRejected documents the loop-state
// schema admitting `finding_supplement` while the runtime evidence pipeline
// rejects manual registration: an operator cannot keep the evidence kind
// in sync with entities.finding_supplements without going through
// `runtime finding-supplement`.
func TestSubmitSupplementManualEvidenceKindRejected(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)

	// Persist a placeholder artifact on disk; the failure must come from the
	// pipeline-owned gate, not from disk I/O.
	artifactPath := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "finding-supplements", "supplement-x.json")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loopruntime.RecordEvidence(root, statePath, journalPath, loopruntime.EvidenceRequest{
		ExpectedRevision: 5,
		ID:               "supplement-manual",
		Kind:             "finding_supplement",
		Path:             filepath.ToSlash(artifactPath),
		ProducedBy:       []string{"orchestrator"},
	})
	if err == nil || !strings.Contains(err.Error(), "pipeline-owned") {
		t.Fatalf("manual evidence record must be rejected, got %v", err)
	}
}
