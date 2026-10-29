package runtime_test

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

// Append a real BuildContracts packet and ContractEvidence receipt, retaining
// the prior packet, submission and evidence as formal rework does.
func appendTaskPlanningS3(t *testing.T, root string, state map[string]any, entry int) {
	t.Helper()
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	data, _ := json.Marshal(rows[0])
	var old workpacket.ContractPacket
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	old.Binding.StageEntry = entry
	p, err := workpacket.BuildContracts(old.Binding, old.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	evidenceData, err := os.ReadFile(filepath.Join(root, ".claude/evidence/E-S3-001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Answer   workpacket.ContractAnswer `json:"contract_answer"`
		Subjects []workpacket.Subject      `json:"subject_refs"`
	}
	if err := json.Unmarshal(evidenceData, &envelope); err != nil {
		t.Fatal(err)
	}
	artifacts := []workpacket.Reference{}
	for _, raw := range state["documents"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "contract" {
			artifacts = append(artifacts, workpacket.Reference{ID: row["id"].(string), Path: row["path"].(string), Version: row["version"].(string), SHA256: row["sha256"].(string)})
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
	var row map[string]any
	if err := json.Unmarshal(packetData, &row); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"] = append(rows, row)
	key := workpacket.Digest([]byte("submission:" + p.ID))
	entities["submissions"] = append(entities["submissions"].([]any), map[string]any{
		"submission_key": key, "packet_id": p.ID, "owner": "Main", "content_sha256": mustContractAnswerDigest(t, envelope.Answer), "evidence_id": "E-S3-CURRENT", "followup_event_id": workpacket.Digest([]byte(key + ":after-domain")), "control_status": "advanced", "origin_state": "planning", "origin_phase": "contracts", "origin_entry": entry, "transition_event_id": "evt-current-PTR-PLAN-02",
	})
	state["evidence"] = append(state["evidence"].([]any), map[string]any{
		"id": "E-S3-CURRENT", "kind": "planning_contract", "baseline_generation": 1, "responsibility_id": "Orchestrator", "produced_by": []any{"Main"}, "status": "valid", "path": ".claude/evidence/E-S3-CURRENT.json", "sha256": workpacket.Digest(data), "review_round": nil, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
	})
	state["lifecycle"].(map[string]any)["phase_revision"] = entry + 1
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
		{"changed bytes", "unavailable or changed", func(t *testing.T, root string, s map[string]any) {
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
		{"wrong packet requirement", "lacks its matching source packet", func(t *testing.T, root string, s map[string]any) {
			s["entities"].(map[string]any)["work_packets"].([]any)[1].(map[string]any)["binding"].(map[string]any)["requirement"].(map[string]any)["sha256"] = workpacket.Digest([]byte("other requirement"))
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
			artifacts = append(artifacts, workpacket.Reference{ID: r["id"].(string), Path: r["path"].(string), Version: r["version"].(string), SHA256: r["sha256"].(string)})
		}
	}
	if kind == "contract" {
		artifacts[0].SHA256 = workpacket.Digest([]byte("different contract bytes"))
	} else {
		p.Inputs.Designs[0].SHA256 = workpacket.Digest([]byte("different design bytes"))
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
	state["evidence"].([]any)[1].(map[string]any)["sha256"] = workpacket.Digest(data)
	oldID := row.(map[string]any)["packet_id"].(string)
	packetData, _ := json.Marshal(p)
	var packetRow map[string]any
	if err := json.Unmarshal(packetData, &packetRow); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"].([]any)[1] = packetRow
	record := entities["submissions"].([]any)[1].(map[string]any)
	if oldID != p.ID {
		key := workpacket.Digest([]byte("submission:" + p.ID))
		record["packet_id"], record["submission_key"], record["followup_event_id"] = p.ID, key, workpacket.Digest([]byte(key+":after-domain"))
	}
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
