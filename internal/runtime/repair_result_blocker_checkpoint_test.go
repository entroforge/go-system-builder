package runtime

import (
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// These tests cover only pure selection/tuple helpers plus a negative
// reader path. They do not claim an authoritative capability-writer chain.
func TestRepairResultBlockerEvidenceIgnoresSiblingResolutionRow(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	p := f.packet
	b := p.Binding.Sources
	artifactDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blockers", p.ID)
	messageDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blocker-messages", p.ID)
	blocker := blockerEvidenceRowFixture(p, path.Join(artifactDir, strings.Repeat("a", 64)+".json"), "repair_result_blocker")
	resolution := map[string]any{
		"id":   path.Join(messageDir, strings.Repeat("b", 64)+".json"),
		"path": path.Join(messageDir, strings.Repeat("b", 64)+".json"),
		"kind": "repair_result_resolution",
		// Resolution lineage legitimately names the predecessor packet.
		"scope_refs": []string{p.ID, b.RepairAssignmentID, blocker["id"].(string)},
	}
	state := map[string]any{"evidence": []any{resolution, blocker}}
	rows, err := repairResultBlockerEvidenceRows(state, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != blocker["id"] {
		t.Fatalf("blocker selector included sibling resolution evidence: %#v", rows)
	}
}

func TestRepairResultBlockerEvidenceDirectoryCatchesWrongKind(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	p := f.packet
	b := p.Binding.Sources
	artifactDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blockers", p.ID)
	row := blockerEvidenceRowFixture(p, path.Join(artifactDir, strings.Repeat("c", 64)+".json"), "repair_result_resolution")
	state := map[string]any{"evidence": []any{row}}
	rows, err := repairResultBlockerEvidenceRows(state, p)
	if err != nil || len(rows) != 1 {
		t.Fatalf("wrong-kind row under this packet's blocker-artifact directory must be surfaced for rejection: rows=%d err=%v", len(rows), err)
	}
	if err := verifyRepairResultBlockerEvidence(rows[0], p, row["id"].(string), row["sha256"].(string)); err == nil {
		t.Fatal("wrong evidence kind under exact blocker artifact directory was accepted")
	}
}

func TestRepairResultBlockerEvidenceRequiresExactUniqueScope(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	p := f.packet
	row := blockerEvidenceRowFixture(p, "artifact.json", "repair_result_blocker")
	for _, tc := range []struct {
		name  string
		refs  []string
		valid bool
	}{
		{"packet then assignment", []string{p.ID, p.Binding.Sources.RepairAssignmentID}, true},
		{"assignment then packet", []string{p.Binding.Sources.RepairAssignmentID, p.ID}, true},
		{"duplicate packet", []string{p.ID, p.ID}, false},
		{"extra scope", []string{p.ID, p.Binding.Sources.RepairAssignmentID, "another-slot"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := make(map[string]any, len(row))
			for k, v := range row {
				copy[k] = v
			}
			copy["scope_refs"] = tc.refs
			err := verifyRepairResultBlockerEvidence(copy, p, copy["id"].(string), copy["sha256"].(string))
			if (err == nil) != tc.valid {
				t.Fatalf("scope refs %v valid=%v, err=%v", tc.refs, tc.valid, err)
			}
		})
	}
}

func TestRepairResultBlockerAllowsOnlyFrozenCanonicalOrManifestAssignmentSpelling(t *testing.T) {
	repairID := "repair-assignment-unit-api"
	manifestID := "assignment-unit-api"
	for _, tc := range []struct {
		name       string
		frozenID   string
		artifactID string
		valid      bool
	}{
		{"canonical exact", repairID, repairID, true},
		{"manifest exact", manifestID, manifestID, true},
		{"cross-spelling is not frozen exact", repairID, manifestID, false},
		{"unrecognized suffix alias", "assignment-unit-api-alias", "assignment-unit-api-alias", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := validBlockerExecutionAssignmentID(tc.frozenID, repairID, manifestID, tc.artifactID)
			if got != tc.valid {
				t.Fatalf("frozen=%q artifact=%q valid=%v got=%v", tc.frozenID, tc.artifactID, tc.valid, got)
			}
		})
	}
}

func TestRepairResultBlockerAbsenceIsNotCandidateConflict(t *testing.T) {
	present, err := repairResultBlockerPresence(0, 0)
	if err != nil || present {
		t.Fatalf("empty blocker history should be absent: present=%v err=%v", present, err)
	}
	// A successful candidate event is only a conflict with a blocker receipt
	// after a complete blocker row/event pair has been found.
	if err := rejectRepairResultBlockerOutcomeConflict(present, true, false); err != nil {
		t.Fatalf("candidate-only packet incorrectly became a blocker conflict: %v", err)
	}
	if _, err := repairResultBlockerPresence(1, 0); err == nil {
		t.Fatal("orphan artifact without lifecycle event should not be treated as absent")
	}
	if _, err := repairResultBlockerPresence(1, 1); err != nil {
		t.Fatalf("complete blocker pair should be present: %v", err)
	}
	if err := rejectRepairResultBlockerOutcomeConflict(true, true, false); err == nil {
		t.Fatal("complete blocker and successful candidate must conflict")
	}
	if err := rejectRepairResultBlockerOutcomeConflict(true, false, true); err == nil {
		t.Fatal("complete blocker and final Result must conflict")
	}
}

func TestVerifiedRepairResultBlockerReceiptReturnsAbsentForCandidateOnlyPacket(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	// Existing mechanical fixture synthesizes a candidate journal event. This
	// negative case verifies query ordering only; it is not an original
	// capability-writer acceptance chain.
	acceptRepairResultCandidateFixture(t, &f)
	got, found, err := VerifiedRepairResultBlockerReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID)
	if err != nil || found {
		t.Fatalf("candidate-only packet should have no blocker receipt: found=%v err=%v receipt=%+v", found, err, got)
	}
}

func blockerEvidenceRowFixture(p workpacket.RepairResultPacket, ref, kind string) map[string]any {
	return map[string]any{
		"id": ref, "path": ref, "kind": kind, "sha256": strings.Repeat("d", 64),
		"responsibility_id":   "BUILD-WORK-PACKAGE",
		"baseline_generation": p.Binding.Sources.BaselineGeneration,
		"review_round":        p.Binding.Sources.ReviewRound,
		"produced_by":         []string{p.Binding.Sources.OwnerAgentID},
		"scope_refs":          []string{p.ID, p.Binding.Sources.RepairAssignmentID},
	}
}
