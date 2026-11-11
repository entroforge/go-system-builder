package runtime_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This is an in-memory compatibility fixture, not a Writer/model acceptance.
func taskPlanningFollowupState(t *testing.T) (map[string]any, map[string]any, workpacket.TaskPlanningPacket, string) {
	t.Helper()
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	issued, err := runtime.RecordTaskPlanningPacket(root, statePath, journalPath, packet, files, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("original S4 packet Writer: %v", err)
	}
	indexPath, taskPath := "docs/dev/tasks/index-REQ-001.md", "docs/dev/tasks/TASK-REQ-001-01.md"
	writeTaskPlanningFile(t, root, taskPath, "# TASK-REQ-001-01: Scope\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n> Dispatch policy: waves-v1\n> Primary contract: FE-001\n\n## Delivered Clauses\n\n| Contract | Delivered clauses |\n|---|---|\n| FE-001 | §1 |\n\n## Scope\n\n| Type | Paths |\n|---|---|\n| prospective write paths | `web/ui` |\n")
	writeTaskPlanningFile(t, root, indexPath, "# Dispatch plan\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n- [ ] [TASK](TASK-REQ-001-01.md)\n")
	answer := workpacket.TaskPlanningAnswer{Conclusion: stringPtr("pass"), IndexPath: stringPtr(indexPath), TaskPaths: []string{taskPath}}
	artifacts, err := runtime.TaskPlanningArtifacts(root, issued.State, packet, answer, files)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := workpacket.TaskPlanningEvidence(packet, "E-S4-001", artifacts, answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	const evidencePath = ".claude/evidence/E-S4-001.json"
	writeBytes(t, root, evidencePath, data)
	draft := packet.Draft
	draft.Answer = answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: issued.Revision, ID: "E-S4-001", Kind: "planning_task", Path: evidencePath,
		ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: semantic.RuntimeCandidateValidator{},
		Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, Files: files,
			Authorize: func(root string, _ map[string]any) error {
				checked, err := semantic.TasksCheckWithFiles(root, files, packet.Binding.Requirement.ID)
				if err != nil {
					return err
				}
				if checked.Tasks == 0 || len(checked.Problems) != 0 {
					return fmt.Errorf("original TASK check: %+v", checked)
				}
				return nil
			}},
	})
	if err != nil {
		t.Fatalf("original S4 evidence Writer: %v", err)
	}
	record, found, err := runtime.FindSubmission(accepted.State, packet.ID)
	if err != nil || !found {
		t.Fatalf("original S4 receipt: %t %v", found, err)
	}
	state = accepted.State
	var row map[string]any
	for _, raw := range state["entities"].(map[string]any)["submissions"].([]any) {
		candidate, _ := raw.(map[string]any)
		if candidate["packet_id"] == packet.ID {
			row = candidate
			break
		}
	}
	if row == nil {
		t.Fatal("original S4 receipt was not registered")
	}
	followup := record.FollowupID

	return state, row, packet, followup
}

func TestTaskPlanningOriginalPendingReceiptRetriesOnlyAtOriginalEntry(t *testing.T) {
	for _, change := range []string{"none", "new_entry", "new_generation", "other_runtime", "advanced", "replaced"} {
		t.Run(change, func(t *testing.T) {
			state, row, packet, followup := taskPlanningFollowupState(t)
			var expected error
			switch change {
			case "new_entry":
				state["lifecycle"].(map[string]any)["phase_revision"] = packet.Binding.StageEntry + 4
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "new_generation":
				state["baseline"].(map[string]any)["generation"] = packet.Binding.Generation + 1
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "other_runtime":
				state["runtime_id"] = "other-runtime"
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "advanced", "replaced":
				row["control_status"] = change
				expected = runtime.ErrSubmissionFollowupProcessed
			}
			if _, err := runtime.SubmissionFollowup(state, followup); !errors.Is(err, expected) {
				t.Fatalf("followup after %s: got %v, want %v", change, err, expected)
			}
		})
	}
}
