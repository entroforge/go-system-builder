package submission

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type PlanningReworkResult struct {
	Committed         bool   `json:"committed"`
	Target            string `json:"target"`
	Reason            string `json:"reason"`
	StageEntry        any    `json:"stage_entry"`
	PreparationStatus string `json:"preparation_status"`
	Packet            any    `json:"packet,omitempty"`
	PreparationError  string `json:"preparation_error,omitempty"`
	NextAction        string `json:"next_action,omitempty"`
}

// ReworkPlanning commits through the catalog's explicit phase route, then
// publishes through the existing issuer. Preparation failure never rolls back
// or disguises the committed entry, and issue retries the original publication.
func (s *Service) ReworkPlanning(ctx context.Context, target, reason string) (PlanningReworkResult, error) {
	s = s.WithContext(ctx)
	r := PlanningReworkResult{Target: target, Reason: strings.TrimSpace(reason)}
	if (target != "design" && target != "contracts" && target != "tasks") || r.Reason == "" {
		return r, fmt.Errorf("rework requires --target design|contracts|tasks and a concrete --reason")
	}
	if err := workspace.RequireMain(s.Root); err != nil {
		return r, err
	}
	snap, err := s.snapshot()
	if err != nil {
		return r, err
	}
	requestID := "planning-rework:" + workpacket.Digest([]byte(target+":"+r.Reason))
	replay, err := runtime.PlanningReworkReplay(snap.State, s.JournalPath, requestID, target)
	if err != nil {
		return r, err
	}
	if !replay {
		life, _ := snap.State["lifecycle"].(map[string]any)
		routes := map[string]map[string]string{"contracts": {"design": "PTR-PLAN-03", "contracts": "PTR-PLAN-04"}, "tasks": {"design": "PTR-PLAN-05", "contracts": "PTR-PLAN-06", "tasks": "PTR-PLAN-07"}}
		id := routes[text(life["phase"])][target]
		if life["state"] != "planning" || id == "" {
			if target == "tasks" {
				return r, fmt.Errorf("tasks rework requires the current S4 Main responsibility; S3 and S5 retain their original professional boundaries")
			}
			return r, fmt.Errorf("rework requires the current S3 or S4 Main responsibility")
		}
		snap, err = transition.Apply(s.Root, s.StatePath, s.JournalPath, transition.Request{Context: ctx, Files: s.Files, TransitionID: id, ExpectedRevision: snap.Revision, ExpectedRuntimeID: text(snap.State["runtime_id"]), Actor: "orchestrator", ProducerResponsibility: "Main", RequestID: requestID, Params: map[string]any{"reason": r.Reason}})
		if err != nil {
			return r, err
		}
	}
	r.Committed = true
	r.StageEntry = snap.State["lifecycle"].(map[string]any)["phase_revision"]
	if target == "design" {
		r.Packet, err = s.IssueDesign(ctx)
	} else if target == "tasks" {
		r.Packet, err = s.IssueTaskPlanning(ctx)
	} else {
		r.Packet, err = s.IssueContracts(ctx)
	}
	if err != nil {
		r.Packet = nil
		r.PreparationStatus = "failed"
		r.PreparationError = err.Error()
		r.NextAction = "Repair the named preparation gap, then run " + PlanningRecoveryCommand(s.Root, "issue")
		RecordPlanningDiagnostic(s.Root, snap.State, "preparation", err.Error())
		return r, nil
	}
	r.PreparationStatus = "ready"
	RecordPlanningDiagnostic(s.Root, snap.State, "preparation", "")
	switch p := r.Packet.(type) {
	case workpacket.TaskPlanningPacket:
		r.NextAction = "Read the S4 task-planning questionnaire at " + filepath.Join(s.Root, p.ViewPath) + "; revise the TASK batch and dispatch index within the frozen REQ/S2/S3 inputs, then edit and save the professional answer at " + filepath.Join(s.Root, p.AnswerPath) + "." + TaskPlanningDeliveryGuidance(s.Root, p, s.Files, snap.State)
	case workpacket.ContractPacket:
		r.NextAction = "Read the S3 questionnaire at " + filepath.Join(s.Root, p.ViewPath) + "; edit and save the professional answer at " + filepath.Join(s.Root, p.AnswerPath) + "." + ContractDeliveryGuidance(s.Root, p, s.Files, snap.State)
	case workpacket.DesignPacket:
		r.NextAction = "Read the S2 design questionnaire at " + filepath.Join(s.Root, p.ViewPath) + "; revise the actual architecture and design materials within the locked REQ, perform the professional self-review, then edit and save the answer at " + filepath.Join(s.Root, p.AnswerPath) + ". The original consumer records the answer and continues the original stage outlet."
	}
	return r, nil
}
