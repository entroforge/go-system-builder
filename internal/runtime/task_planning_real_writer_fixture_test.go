package runtime_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Commit the S3 receipt and its S4 continuation through their actual Writers.
func taskPlanningOriginalS3(t *testing.T, root string, source map[string]any, packet workpacket.ContractPacket, answer workpacket.ContractAnswer, evidencePath string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["runtime_id"], state["revision"] = "1.2.0", packet.Binding.RuntimeID, 0
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": packet.Binding.StageEntry}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	req := packet.Binding.Requirement
	state["bound_req"] = map[string]any{"id": req.ID, "path": req.Path, "version": req.Version, "status": "locked", "approved_by": "user", "approved_at": "2026-06-18T00:00:00Z", "metadata": map[string]any{"ui_impact": "none"}, "workspace": map[string]any{"project_root": root, "dev_branch": "test-development", "release_upstream": "main"}}
	state["documents"] = source["documents"]
	entities := state["entities"].(map[string]any)
	entities["work_packets"], entities["submissions"] = []any{}, []any{}
	state["evidence"] = []any{}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, "docs/control/loop-definition.json", definition)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	draft := packet.Draft
	draft.Answer = answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	files := fileview.Disk{Root: root}
	issued, err := runtime.RecordContractPacket(root, statePath, journalPath, packet, files, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("original S3 packet Writer: %v", err)
	}
	paths := append([]string{*answer.IndexPath}, answer.ContractPaths...)
	accepted, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: issued.Revision, ID: "E-S3-001", Kind: "planning_contract", Path: evidencePath,
		ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: semantic.RuntimeCandidateValidator{},
		Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, Files: files,
			Authorize: func(root string, _ map[string]any) error {
				checked, err := semantic.ContractsCheckBatch(root, files, req, paths)
				if err != nil {
					return err
				}
				if checked.Contracts == 0 || len(checked.Problems) != 0 {
					return fmt.Errorf("original contract check: %+v", checked)
				}
				return nil
			}},
	})
	if err != nil {
		t.Fatalf("original S3 evidence Writer: %v", err)
	}
	receipt, found, err := runtime.FindSubmission(accepted.State, packet.ID)
	if err != nil || !found {
		t.Fatalf("original S3 receipt: found=%t err=%v", found, err)
	}
	advanced, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: files, TransitionID: "PTR-PLAN-02", ExpectedRevision: accepted.Revision, ExpectedRuntimeID: packet.Binding.RuntimeID,
		Actor: "orchestrator", SubmissionFollowupID: receipt.FollowupID,
	})
	if err != nil {
		t.Fatalf("original S3 -> S4 transition: %v", err)
	}
	return advanced.State
}
