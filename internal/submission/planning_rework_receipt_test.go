package submission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestContractHistoricalReceiptAfterPlanningRework(t *testing.T) {
	for _, status := range []string{"pending", "no_transition", "advanced"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			hash := strings.Repeat("a", 64)
			p, err := workpacket.BuildContracts(workpacket.PlanningBinding{RuntimeID: "loop-test", Generation: 1, StageEntry: 2, Stage: "S3", Owner: "Main", Requirement: workpacket.Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: hash}}, workpacket.ContractInputs{Designs: []workpacket.Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1.0.0", SHA256: hash}}, Sources: []workpacket.Subject{}, Directories: map[string][]string{}})
			if err != nil {
				t.Fatal(err)
			}
			pass, index := "pass", "docs/dev/contracts/CONTRACTS-001.md"
			answer := workpacket.ContractAnswer{Conclusion: &pass, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-001.md"}, Applicability: map[string]workpacket.ContractApplicability{"FE": {Disposition: "not_applicable", Rationale: "No UI"}, "BE": {Disposition: "required", Rationale: "Service"}, "SYNC": {Disposition: "not_applicable", Rationale: "Local boundary"}}}
			key := workpacket.Digest([]byte("submission:" + p.ID))
			id := "ev-submission-" + key
			envelope, err := workpacket.ContractEvidence(p, id, []workpacket.Reference{{ID: "CONTRACTS-001", Path: index, Version: "v1.0.0", SHA256: hash}, {ID: "BE-001", Path: answer.ContractPaths[0], Version: "v1.0.0", SHA256: hash}}, answer, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			path := ".claude/evidence/original.json"
			if err := os.MkdirAll(filepath.Join(root, ".claude/evidence"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
				t.Fatal(err)
			}
			digest, _ := workpacket.ContractAnswerDigest(answer)
			asRow := func(v any) map[string]any {
				b, _ := json.Marshal(v)
				var row map[string]any
				if err := json.Unmarshal(b, &row); err != nil {
					t.Fatal(err)
				}
				return row
			}
			record := runtime.SubmissionRecord{Key: key, PacketID: p.ID, Owner: "Main", ContentHash: digest, EvidenceID: id, FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: status, OriginState: "planning", OriginPhase: "contracts", OriginEntry: p.Binding.StageEntry}
			ev := map[string]any{"id": id, "kind": "planning_contract", "path": path, "sha256": workpacket.Digest(data), "status": "valid"}
			state := map[string]any{"runtime_id": p.Binding.RuntimeID, "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 2}, "evidence": []any{ev}, "entities": map[string]any{"work_packets": []any{asRow(p)}, "submissions": []any{asRow(record)}}}
			s := &Service{Root: root}
			before, err := s.contractReceipt(runtime.Snapshot{State: state}, p)
			if err != nil || !before.Accepted || !before.WorkComplete {
				t.Fatalf("original receipt: %+v %v", before, err)
			}
			state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 4}
			// Entry change alone replaces a pending outlet. Advanced history
			// becomes superseded when rework invalidates its original evidence.
			if status == "advanced" {
				ev["status"] = "invalid"
			}
			after, err := s.contractReceipt(runtime.Snapshot{State: state}, p)
			if err != nil || !after.Accepted || after.WorkComplete || after.ResponsibilityStatus != "replaced" || len(after.WaitingOn) != 0 || after.ConsumerPending || strings.Contains(after.NextAction, "stage-answer recover") {
				t.Fatalf("historical receipt after rework: %+v %v", after, err)
			}
			if after.SubmissionKey != before.SubmissionKey || len(after.DomainRefs) != 1 || after.DomainRefs[0] != id {
				t.Fatalf("lost historical acceptance: %+v", after)
			}
			got, _ := os.ReadFile(filepath.Join(root, path))
			if string(got) != string(data) {
				t.Fatal("receipt changed original evidence bytes")
			}
		})
	}
}

func TestTaskPlanningInvalidatedReceiptKeepsAcceptanceHistory(t *testing.T) {
	p := workpacket.TaskPlanningPacket{ID: "packet-s4", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 3}}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 5}, "evidence": []any{map[string]any{"id": "evidence", "status": "invalid"}}, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "key", "evidence_id": "evidence", "control_status": "advanced"}}}}
	s := &Service{Root: t.TempDir()}
	r, err := s.taskPlanningReceipt(runtime.Snapshot{State: state}, p)
	if err != nil || !r.Accepted || r.WorkComplete || r.ResponsibilityStatus != "replaced" || r.ConsumerPending || len(r.WaitingOn) != 0 {
		t.Fatalf("invalidated historical S4 receipt: %+v %v", r, err)
	}
}

func TestDesignInvalidatedReceiptKeepsAcceptanceHistory(t *testing.T) {
	p := workpacket.DesignPacket{ID: "packet-s2", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 1}}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "design", "phase_revision": 4}, "evidence": []any{map[string]any{"id": "evidence", "status": "invalid"}}, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "key", "evidence_id": "evidence", "control_status": "advanced"}}}}
	s := &Service{Root: t.TempDir()}
	r, err := s.designReceipt(runtime.Snapshot{State: state}, p)
	if err != nil || !r.Accepted || r.WorkComplete || r.ResponsibilityStatus != "replaced" || r.ConsumerPending || len(r.WaitingOn) != 0 || strings.Contains(r.NextAction, "unavailable") {
		t.Fatalf("invalidated historical S2 receipt: %+v %v", r, err)
	}
}
