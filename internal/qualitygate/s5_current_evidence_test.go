package qualitygate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type s5EvidenceFiles map[string][]byte

func (files s5EvidenceFiles) ReadFile(path string) ([]byte, error) {
	data, ok := files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func TestS5EvidenceSelectorsIgnorePriorEntryReviews(t *testing.T) {
	t.Run("qualified evidence", func(t *testing.T) {
		input, documents := priorEntryS5EvidenceInput(t, "DV-SPEC-CONSISTENCY", "pass", "")
		requirement := EvidenceRequirement{
			Kind: "document_review_record", Responsibilities: []string{"DV-SPEC-CONSISTENCY"},
			Conclusions: []string{"pass"}, CurrentReviewRound: true,
		}
		ids, conflicts := qualifiedEvidence(input.Snapshot.State, input.Files, "loop-test", 1, 2, requirement, documents)
		if len(ids) != 0 || len(conflicts) != 0 {
			t.Fatalf("prior S5 evidence qualified for the new entry: ids=%v conflicts=%v", ids, conflicts)
		}

		setS5PacketEnabled(input.Snapshot.State, false)
		ids, conflicts = qualifiedEvidence(input.Snapshot.State, input.Files, "loop-test", 1, 2, requirement, documents)
		if len(ids) != 1 || ids[0] != "ev-old" || len(conflicts) != 0 {
			t.Fatalf("packet-off legacy evidence behavior changed: ids=%v conflicts=%v", ids, conflicts)
		}
	})

	t.Run("requested event routing", func(t *testing.T) {
		input, documents := priorEntryS5EvidenceInput(t, "DV-SPEC-CONSISTENCY", "fix_required", "document_fix_required")
		registry := &Registry{gates: map[string]GateSpec{
			"GATE-DOCUMENT-FIX-REQUIRED": {
				ID: "GATE-DOCUMENT-FIX-REQUIRED", TransitionEvent: "document_fix_required",
				CursorState: "document_verification", EvidenceRequirements: []EvidenceRequirement{{
					Kind: "document_review_record", Responsibilities: []string{"DV-SPEC-CONSISTENCY"},
					Conclusions: []string{"fix_required"}, CurrentReviewRound: true,
				}},
			},
			"GATE-DOCUMENT-PASS": {
				ID: "GATE-DOCUMENT-PASS", TransitionEvent: "document_pass",
				CursorState: "document_verification",
			},
		}}
		engine := &Engine{registry: registry}
		if got := engine.qualifiedRequestedEvents(input, documents); len(got) != 0 {
			t.Fatalf("prior S5 verdict routed a new transition: %v", got)
		}

		setS5PacketEnabled(input.Snapshot.State, false)
		if got := engine.qualifiedRequestedEvents(input, documents); len(got) != 1 || got[0] != "document_fix_required" {
			t.Fatalf("packet-off legacy event routing changed: %v", got)
		}
	})

	t.Run("unauthorized producer conflict", func(t *testing.T) {
		input, documents := priorEntryS5EvidenceInput(t, "FOREIGN-RESPONSIBILITY", "pass", "")
		spec := GateSpec{EvidenceRequirements: []EvidenceRequirement{{
			Kind: "document_review_record", Responsibilities: []string{"DV-SPEC-CONSISTENCY"},
		}}}
		if got := unauthorizedProducerConflicts(input, spec, documents, 1, 2); len(got) != 0 {
			t.Fatalf("prior S5 evidence produced a current producer conflict: %v", got)
		}

		setS5PacketEnabled(input.Snapshot.State, false)
		if got := unauthorizedProducerConflicts(input, spec, documents, 1, 2); len(got) != 1 || got[0] != "evidence:ev-old:producer" {
			t.Fatalf("packet-off legacy producer conflict behavior changed: %v", got)
		}
	})
}

func priorEntryS5EvidenceInput(t *testing.T, responsibility, conclusion, requestedEvent string) (Input, []documentFact) {
	t.Helper()
	const documentPath = "docs/dev/tasks/TASK-001.md"
	const evidencePath = ".claude/evidence/ev-old.json"
	documentBytes := []byte("# TASK-001\n")
	documentSHA := sha256Hex(documentBytes)
	files := s5EvidenceFiles{documentPath: documentBytes}
	documents := []documentFact{{ID: "TASK-001", Path: documentPath, Version: "v1", SHA256: documentSHA}}

	packet, err := workpacket.BuildS5(workpacket.Binding{
		RuntimeID: "loop-test", Generation: 1, StageEntry: 4, Owner: "reviewer-spec",
		AssignmentID: "assignment-review-old", AssignmentSHA: strings.Repeat("a", 64),
		Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1,
	}, []workpacket.Reference{{ID: "TASK-001", Path: documentPath, Version: "v1", SHA256: documentSHA}})
	if err != nil {
		t.Fatalf("build prior S5 packet: %v", err)
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow map[string]any
	if err := json.Unmarshal(packetBytes, &packetRow); err != nil {
		t.Fatal(err)
	}

	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-old", "kind": "document_review",
		"runtime_id": "loop-test", "baseline_generation": 1, "review_round": 2,
		"producer_agent_id": "reviewer-spec", "producer_responsibility": responsibility,
		"subject_refs": []any{map[string]any{"path": documentPath, "version": "v1", "sha256": documentSHA}},
		"conclusion":   conclusion, "requested_event": requestedEvent,
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	files[evidencePath] = envelopeBytes
	evidenceRow := map[string]any{
		"id": "ev-old", "kind": "document_review", "path": evidencePath,
		"sha256": sha256Hex(envelopeBytes), "status": "valid", "baseline_generation": 1,
		"review_round": 2, "produced_by": []any{"reviewer-spec"}, "invalidated_by": nil,
		"responsibility_id": responsibility, "scope_refs": []any{documentPath},
	}

	state := map[string]any{
		"schema_version": "1.3.0", "runtime_id": "loop-test",
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"lifecycle":     map[string]any{"state": "document_verification", "phase_revision": 8},
		"baseline":      map[string]any{"generation": 1}, "review": map[string]any{"round": 2},
		"documents": []any{map[string]any{
			"id": "TASK-001", "kind": "task", "path": documentPath, "version": "v1",
			"sha256": documentSHA, "status": "locked", "generation": 1,
		}},
		"entities": map[string]any{
			"work_packets": []any{packetRow},
			"submissions": []any{map[string]any{
				"submission_key": workpacket.Digest([]byte("submission:" + packet.ID)),
				"packet_id":      packet.ID, "owner": "reviewer-spec", "evidence_id": "ev-old",
				"control_status": "pending", "origin_state": "document_verification",
				"origin_phase": "", "origin_entry": 4,
			}},
			"teams": []any{
				map[string]any{
					"id": "workgroup-review-old", "kind": "document_verifier", "status": "active",
					"packet_generation": 1, "packet_stage_entry": 4,
					"agent_ids": []any{"reviewer-spec"}, "responsibility_ids": []any{"DV-SPEC-CONSISTENCY"},
				},
				map[string]any{
					"id": "workgroup-review-current", "kind": "document_verifier", "status": "planned",
					"packet_generation": 1, "packet_stage_entry": 8,
				},
			},
		},
		"evidence": []any{evidenceRow},
	}
	if responsibility == "DV-SPEC-CONSISTENCY" {
		// Prove the packet and accepted-submission fixture is valid for its
		// original entry before moving the runtime cursor to the newer entry.
		lifecycle := state["lifecycle"].(map[string]any)
		entities := state["entities"].(map[string]any)
		teams := entities["teams"].([]any)
		lifecycle["phase_revision"] = 4
		entities["teams"] = teams[:1]
		if !runtimepkg.CurrentDocumentReviewEvidence(state, "ev-old") {
			t.Fatal("canonical evidence fixture is not current at its accepted S5 entry")
		}
		lifecycle["phase_revision"] = 8
		entities["teams"] = teams
	}
	if runtimepkg.CurrentDocumentReviewEvidence(state, "ev-old") {
		t.Fatal("prior-entry evidence fixture was unexpectedly classified as current")
	}
	return Input{Snapshot: runtimepkg.Snapshot{State: state}, Files: files}, documents
}

func setS5PacketEnabled(state map[string]any, enabled bool) {
	configuration := state["configuration"].(map[string]any)
	optimization := configuration["optimization"].(map[string]any)
	optimization["packet"] = enabled
}
