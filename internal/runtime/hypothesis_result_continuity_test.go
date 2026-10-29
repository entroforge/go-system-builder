package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func hypothesisContinuityHistoryRow(revision int, ref, sha string) map[string]any {
	return map[string]any{
		"revision": revision, "path": ref, "sha256": sha,
		"operation": "hypothesis_sibling_updated", "occurred_at": "2026-10-14T00:03:00Z",
	}
}

func writeHypothesisContinuityRevision(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, revision int) (string, []byte) {
	t.Helper()
	ref := fmt.Sprintf(".claude/review/investigation/cases/%s-r%d.json", f.packet.Binding.CaseID, revision)
	data := s8CheckpointWriteJSON(t, f.root, ref, document)
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	pointer["path"], pointer["sha256"], pointer["revision"] = ref, workpacket.Digest(data), revision
	return ref, data
}

func TestVerifyHypothesisResultContinuityAllowsCompatibleSiblingRevision(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	original, _ := readHypothesisCheckpointCase(t, f)
	current := s8CheckpointCloneMap(t, original)
	current["revision"] = 2
	current["revision_history"] = []any{hypothesisContinuityHistoryRow(1, f.packet.Binding.CaseRef, f.packet.Binding.CaseSHA256)}
	current["hypotheses"] = append(objectRows(current["hypotheses"]), map[string]any{
		"hypothesis_id": "hypothesis-sibling-002", "assignment_id": "assignment-sibling-002",
		"statement":          "A later sibling question tracks a different discriminator.",
		"invariant":          "The sibling remains independent of the issued question.",
		"discriminator":      "Compare the sibling read boundary.",
		"expected_outcomes":  map[string]any{"support": "The sibling path is stale.", "refute": "The sibling path is current."},
		"source_finding_ids": []string{"finding-002"}, "evidence_refs": []string{"EVID-S8-FINDING-002"}, "status": "open",
	})
	writeHypothesisContinuityRevision(t, &f, current, 2)
	if err := VerifyHypothesisResultContinuity(f.root, f.state, f.packet); err != nil {
		t.Fatalf("compatible sibling Case revision invalidated the issued question: %v", err)
	}
}

func TestVerifyHypothesisResultContinuityRejectsChangedThenRestoredOwnedProjection(t *testing.T) {
	for _, test := range []struct {
		name         string
		mutate       func(map[string]any)
		wantContains string
	}{
		{
			name:         "own question",
			wantContains: "own hypothesis changed after issuance",
			mutate: func(intermediate map[string]any) {
				objectRows(intermediate["hypotheses"])[0]["statement"] = "A changed question that is later restored."
			},
		},
		{
			name:         "Case route",
			wantContains: "own Case source or route changed",
			mutate: func(intermediate map[string]any) {
				intermediate["route"] = "investigate_more"
				intermediate["route_reason"] = "The Case temporarily changed route before being restored."
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			original, _ := readHypothesisCheckpointCase(t, f)
			intermediate := s8CheckpointCloneMap(t, original)
			intermediate["revision"] = 2
			test.mutate(intermediate)
			intermediateRef := fmt.Sprintf(".claude/review/investigation/cases/%s-r2.json", f.packet.Binding.CaseID)
			intermediateBytes := s8CheckpointWriteJSON(t, f.root, intermediateRef, intermediate)

			current := s8CheckpointCloneMap(t, original)
			current["revision"] = 3
			current["revision_history"] = []any{
				hypothesisContinuityHistoryRow(1, f.packet.Binding.CaseRef, f.packet.Binding.CaseSHA256),
				hypothesisContinuityHistoryRow(2, intermediateRef, workpacket.Digest(intermediateBytes)),
			}
			writeHypothesisContinuityRevision(t, &f, current, 3)
			if err := VerifyHypothesisResultContinuity(f.root, f.state, f.packet); err == nil || !strings.Contains(err.Error(), test.wantContains) {
				t.Fatalf("restoring the current projection erased intermediate drift: %v", err)
			}
		})
	}
}

func TestVerifyHypothesisResultContinuityRejectsMissingOrChangedHistoricalSource(t *testing.T) {
	for _, test := range []struct {
		name         string
		includeR2    bool
		mutateSHA    bool
		wantContains string
	}{
		{name: "missing intermediate history", includeR2: false, wantContains: "missing revision 2"},
		{name: "changed intermediate source sha", includeR2: true, mutateSHA: true, wantContains: "bytes/SHA differ"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			original, _ := readHypothesisCheckpointCase(t, f)
			current := s8CheckpointCloneMap(t, original)
			current["revision"] = 3
			history := []any{hypothesisContinuityHistoryRow(1, f.packet.Binding.CaseRef, f.packet.Binding.CaseSHA256)}
			if test.includeR2 {
				intermediate := s8CheckpointCloneMap(t, original)
				intermediate["revision"] = 2
				ref, data := writeHypothesisContinuityRevision(t, &f, intermediate, 2)
				sha := workpacket.Digest(data)
				if test.mutateSHA {
					sha = strings.Repeat("e", 64)
				}
				history = append(history, hypothesisContinuityHistoryRow(2, ref, sha))
			}
			current["revision_history"] = history
			writeHypothesisContinuityRevision(t, &f, current, 3)
			if err := VerifyHypothesisResultContinuity(f.root, f.state, f.packet); err == nil || !strings.Contains(err.Error(), test.wantContains) {
				t.Fatalf("unproven Case history was accepted: %v", err)
			}
		})
	}
}

func TestVerifyHypothesisResultContinuityRejectsConsumedRouteEvenAfterPointerReset(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	const afterRevision = 4
	event := buildJournalEvent(Mutation{
		EventID:      fmt.Sprintf("evt-s8-route-consume-%s-r%d", f.packet.Binding.CaseID, afterRevision),
		TransitionID: "S8-ROUTE-CONSUME", Event: "investigation_route_consumed", Actor: "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:s8:route-consume:%s:%d", f.packet.Binding.CaseID, afterRevision-1),
		RuntimeID:      f.packet.Binding.RuntimeID, BaselineGeneration: f.packet.Binding.BaselineGeneration,
		From:        map[string]any{"state": "bug_resolution", "phase": "investigation"},
		To:          map[string]any{"state": "bug_resolution", "phase": "investigation"},
		EvidenceIDs: []string{f.packet.Binding.CaseRef},
	}, f.packet.Binding.RuntimeID, 3, afterRevision, 3, time.Date(2026, 10, 14, 0, 4, 0, 0, time.UTC))
	f.journal = s8CheckpointJournalBytes(t, f.registerEvent, f.issueEvent, event)
	if err := os.WriteFile(filepath.Join(f.root, ".claude", "loop-events.jsonl"), f.journal, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := controlproof.InspectJournalData(f.journal)
	if err != nil {
		t.Fatalf("actual route-consumption event is not schema-valid: %v", err)
	}
	f.state["revision"] = afterRevision
	f.state["journal"] = map[string]any{"last_sequence": 3, "last_event_id": event["event_id"]}
	if err := controlproof.ValidateStateJournalPair(f.state, inspection); err != nil {
		t.Fatalf("route event/state pair is invalid: %v", err)
	}

	// Simulate a later pointer reset: the Case and current tuple look eligible,
	// while the original journal still proves that this route was consumed.
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	pointer["status"], pointer["route"], pointer["route_reason"] = "investigating", nil, nil
	pointer["route_consumed_at"], pointer["route_consumer"] = nil, nil
	if err := VerifyHypothesisResultContinuity(f.root, f.state, f.packet); err == nil || !strings.Contains(err.Error(), "route was consumed after issuance") {
		t.Fatalf("original route consumption was erased by resetting the current pointer: %v", err)
	}
}
