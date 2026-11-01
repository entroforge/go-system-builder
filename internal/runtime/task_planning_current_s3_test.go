package runtime_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Append a real BuildContracts packet and ContractEvidence receipt, retaining
// the prior packet, submission and evidence as formal rework does.
func appendTaskPlanningS3(t *testing.T, root string, state map[string]any, _ int) {
	t.Helper()
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	files := fileview.Disk{Root: root}
	reworked, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: files, TransitionID: "PTR-PLAN-06", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: current.State["runtime_id"].(string), Actor: "orchestrator",
		ProducerResponsibility: "Main", RequestID: "s3-rework-fixture", Params: map[string]any{"reason": "Update the current contracts."},
	})
	if err != nil {
		t.Fatalf("original S3 rework transition: %v", err)
	}
	binding, _, err := runtime.CurrentContractBinding(reworked.State)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := runtime.ResolveContractInputs(root, reworked.State, files)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildContracts(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := runtime.RecordContractPacket(root, statePath, journalPath, packet, files, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("original S3 packet Writer: %v", err)
	}
	evidenceData, err := os.ReadFile(filepath.Join(root, ".claude/evidence/E-S3-001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var previous struct {
		Answer workpacket.ContractAnswer `json:"contract_answer"`
	}
	if err := json.Unmarshal(evidenceData, &previous); err != nil {
		t.Fatal(err)
	}
	artifacts := []workpacket.Reference{
		{ID: "CONTRACTS-001", Path: *previous.Answer.IndexPath, Version: "v1.0.0"},
		{ID: "FE-001", Path: previous.Answer.ContractPaths[0], Version: "v1.0.0"},
	}
	evidence, err := workpacket.ContractEvidence(packet, "E-S3-CURRENT", artifacts, previous.Answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	const evidencePath = ".claude/evidence/E-S3-CURRENT.json"
	writeBytes(t, root, evidencePath, data)
	draft := packet.Draft
	draft.Answer = previous.Answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	paths := append([]string{*previous.Answer.IndexPath}, previous.Answer.ContractPaths...)
	accepted, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: issued.Revision, ID: "E-S3-CURRENT", Kind: "planning_contract", Path: evidencePath,
		ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: semantic.RuntimeCandidateValidator{},
		Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, Files: files,
			Authorize: func(root string, _ map[string]any) error {
				checked, err := semantic.ContractsCheckBatch(root, files, binding.Requirement, paths)
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
		t.Fatalf("original S3 rework evidence Writer: %v", err)
	}
	receipt, found, err := runtime.FindSubmission(accepted.State, packet.ID)
	if err != nil || !found {
		t.Fatalf("original rework receipt: %t %v", found, err)
	}
	advanced, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: files, TransitionID: "PTR-PLAN-02", ExpectedRevision: accepted.Revision, ExpectedRuntimeID: binding.RuntimeID,
		Actor: "orchestrator", SubmissionFollowupID: receipt.FollowupID,
	})
	if err != nil {
		t.Fatalf("original reworked S3 -> S4 transition: %v", err)
	}
	for key := range state {
		delete(state, key)
	}
	for key, value := range advanced.State {
		state[key] = value
	}
}

func TestTaskPlanningCurrentS3IgnoresInvalidReworkHistory(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	appendTaskPlanningS3(t, root, state, 7)
	old := state["evidence"].([]any)[0].(map[string]any)
	old["status"], old["invalidated_by"], old["invalidation_rule"], old["invalidation_reason"] = "invalid", "rework-1", "planning_rework", "correct contracts"
	// The selector must not read historical evidence bodies at all. The Store
	// remains responsible for validating persisted historical structure.
	if err := os.Remove(filepath.Join(root, old["path"].(string))); err != nil {
		t.Fatal(err)
	}
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatalf("valid current S3 was blocked by history: %v", err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workpacket.BuildTaskPlanning(binding, inputs); err != nil {
		t.Fatalf("prepare S4: %v", err)
	}
}

func TestTaskPlanningBadCurrentS3NeverFallsBack(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*testing.T, string, map[string]any)
	}{
		{"invalid evidence", "accepted evidence", func(t *testing.T, root string, s map[string]any) {
			s["evidence"].([]any)[1].(map[string]any)["status"] = "invalid"
		}},
		{"missing evidence", "accepted evidence", func(t *testing.T, root string, s map[string]any) { s["evidence"] = s["evidence"].([]any)[:1] }},
		{"malformed evidence", "original S3 answer is unavailable", func(t *testing.T, root string, s map[string]any) {
			writeBytes(t, root, ".claude/evidence/E-S3-CURRENT.json", []byte("{}"))
		}},
		{"missing bytes", "read S4 original S3 evidence", func(t *testing.T, root string, s map[string]any) {
			if err := os.Remove(filepath.Join(root, ".claude/evidence/E-S3-CURRENT.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing submission", "reception/transition association", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["submissions"] = s["entities"].(map[string]any)["submissions"].([]any)[:1]
		}},
		{"missing packet", "lacks its matching source packet", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"] = s["entities"].(map[string]any)["work_packets"].([]any)[:1]
		}},
		{"missing transition", "reception/transition association", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["submissions"].([]any)[1].(map[string]any)["transition_event_id"] = ""
		}},
		{"wrong origin", "reception/transition association", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["submissions"].([]any)[1].(map[string]any)["origin_entry"] = 2
		}},
		{"unadvanced", "reception/transition association", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["submissions"].([]any)[1].(map[string]any)["control_status"] = "pending"
		}},
		{"duplicate responsibility", "responsibility is duplicated", func(t *testing.T, root string, s map[string]any) {
			e := s["entities"].(map[string]any)
			data, _ := json.Marshal(e["work_packets"].([]any)[1])
			var row map[string]any
			if err := json.Unmarshal(data, &row); err != nil {
				t.Fatal(err)
			}
			row["packet_id"] = "conflicting-original-packet"
			e["work_packets"] = append(e["work_packets"].([]any), row)
		}},
		{"wrong packet owner", "reception/transition association", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"].([]any)[1].(map[string]any)["binding"].(map[string]any)["owner"] = "Other"
		}},
		{"wrong packet requirement ID", "lacks its matching source packet", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"].([]any)[1].(map[string]any)["binding"].(map[string]any)["requirement"].(map[string]any)["id"] = "REQ-OTHER"
		}},
		{"wrong packet requirement path", "lacks its matching source packet", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"].([]any)[1].(map[string]any)["binding"].(map[string]any)["requirement"].(map[string]any)["path"] = "docs/requirements/REQ-OTHER.md"
		}},
		{"wrong packet requirement version", "lacks its matching source packet", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"].([]any)[1].(map[string]any)["binding"].(map[string]any)["requirement"].(map[string]any)["version"] = "v999.0.0"
		}},
		{"duplicate submission", "reception identity is duplicated", func(t *testing.T, root string, s map[string]any) {
			e := s["entities"].(map[string]any)
			e["submissions"] = append(e["submissions"].([]any), e["submissions"].([]any)[1])
		}},
		{"duplicate evidence", "evidence identity is duplicated", func(t *testing.T, root string, s map[string]any) {
			s["evidence"] = append(s["evidence"].([]any), s["evidence"].([]any)[1])
		}},
		{"different contract closure", "contract/source closure differs", func(t *testing.T, root string, s map[string]any) {
			rewriteTaskPlanningCurrentEvidence(t, root, s, "contract")
		}},
		{"different design closure", "design sources differ", func(t *testing.T, root string, s map[string]any) {
			rewriteTaskPlanningCurrentEvidence(t, root, s, "design")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, state, files := taskPlanningRuntimeFixture(t)
			appendTaskPlanningS3(t, root, state, 7)
			tt.mutate(t, root, state)
			_, err := runtime.ResolveTaskPlanningInputs(root, state, files)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("bad current source fell back or lost diagnostic: %v; want %q", err, tt.want)
			}
		})
	}
}

// Keep packet/envelope identity valid while changing the current source closure;
// this distinguishes selection from choosing whichever older receipt matches.
func rewriteTaskPlanningCurrentEvidence(t *testing.T, root string, state map[string]any, kind string) {
	t.Helper()
	entities := state["entities"].(map[string]any)
	row := entities["work_packets"].([]any)[1]
	data, _ := json.Marshal(row)
	var p workpacket.ContractPacket
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/evidence/E-S3-CURRENT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Answer workpacket.ContractAnswer `json:"contract_answer"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	artifacts := []workpacket.Reference{}
	for _, raw := range state["documents"].([]any) {
		r := raw.(map[string]any)
		if r["kind"] == "contract" {
			artifacts = append(artifacts, workpacket.Reference{ID: r["id"].(string), Path: r["path"].(string), Version: r["version"].(string)})
		}
	}
	if kind == "contract" {
		artifacts[0].Version = "different-version"
	} else {
		p.Inputs.Designs[0].Version = "different-version"
		p, err = workpacket.BuildContracts(p.Binding, p.Inputs)
		if err != nil {
			t.Fatal(err)
		}
	}
	e, err := workpacket.ContractEvidence(p, "E-S3-CURRENT", artifacts, envelope.Answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, ".claude/evidence/E-S3-CURRENT.json", data)
	packetData, _ := json.Marshal(p)
	var packetRow map[string]any
	if err := json.Unmarshal(packetData, &packetRow); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"].([]any)[1] = packetRow
}

func TestTaskPlanningTasksOnlyReworkKeepsOriginalS3(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(p)
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	e := state["entities"].(map[string]any)
	e["work_packets"] = append(e["work_packets"].([]any), row)
	state["lifecycle"].(map[string]any)["phase_revision"] = binding.StageEntry + 1
	if err := runtime.ApplyPlanningRework(root, state, "tasks", "correct task split", "rework-tasks", files); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ResolveTaskPlanningInputs(root, state, files); err != nil {
		t.Fatalf("tasks-only rework lost original S3: %v", err)
	}
	if state["evidence"].([]any)[0].(map[string]any)["status"] != "valid" {
		t.Fatal("tasks-only rework invalidated S3")
	}
}

func TestTaskPlanningIssuedIdentityCannotRebindToNewClosure(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	appendTaskPlanningS3(t, root, state, 7)
	if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err == nil {
		t.Fatal("old issued S4 silently rebound to new responsibility")
	}
}
