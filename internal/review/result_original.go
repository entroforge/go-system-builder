package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// IssuedResultWorkComplete reads only this original issuance's reception.
// Historical assignment IDs may be reused by a completely different Plan.
func IssuedResultWorkComplete(state map[string]any, packet workpacket.ReviewResultPacket) bool {
	reception := loopruntime.ReviewResultReception(state, packet.ID)
	if complete, ok := reception["work_complete"].(bool); ok && reception["receipt"] != nil {
		return complete
	}
	if !loopruntime.ReviewResultPacketCurrent(state, packet) {
		return false
	}
	row := resultPacketAssignmentRow(state, packet.Binding.AssignmentID)
	receipt, _ := row["result_receipt"].(map[string]any)
	return receipt["packet_id"] == packet.ID && ResultWorkComplete(state, packet.Binding.AssignmentID)
}

// IssuedResultAppliesToCurrent reports whether a previously issued, still
// in-flight responsibility remains unchanged by a subsequent amendment to
// the same Plan. It compares only the original Assignment and its Claims when
// that particular answer arrives. It does not scan earlier plans or files.
func IssuedResultAppliesToCurrent(root string, state map[string]any, packet workpacket.ReviewResultPacket, ctx context.Context) (bool, error) {
	if loopruntime.ReviewResultPacketCurrent(state, packet) {
		return true, nil
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	reviewState, _ := state["review"].(map[string]any)
	rows, _ := reviewState["assignments"].(map[string]any)
	row, _ := rows[packet.Binding.AssignmentID].(map[string]any)
	ptr := PlanPointerFromState(state)
	// Once accepted, the original receipt attached to the current Assignment
	// is the direct established fact, including after the stage advances.
	receipt, _ := row["result_receipt"].(map[string]any)
	if ptr != nil && ptr.PlanID == packet.Binding.ReviewPlanID && packet.ID != "" && receipt["packet_id"] == packet.ID {
		return true, nil
	}
	if lifecycle["state"] != "verification" || ptr == nil || ptr.PlanID != packet.Binding.ReviewPlanID ||
		state["runtime_id"] != packet.Binding.RuntimeID || baselineGeneration(state) != packet.Binding.BaselineGeneration ||
		row == nil || row["agent_id"] != packet.Binding.OwnerAgentID {
		return false, nil
	}
	switch workstate.ReviewAssignmentActivity(row) {
	case "dispatched", "blocked", "consumed":
	default:
		return false, nil
	}
	original, _, err := loadOriginalResultPlan(root, packet, ctx)
	if err != nil {
		return false, err
	}
	current, _, err := LoadPlan(root, state)
	if err != nil {
		return false, err
	}
	oldAssignment := findPlanAssignment(original, packet.Binding.AssignmentID)
	newAssignment := findPlanAssignment(current, packet.Binding.AssignmentID)
	if oldAssignment == nil || newAssignment == nil {
		return false, nil
	}
	oldContent, oldErr := planAssignmentContent(oldAssignment)
	newContent, newErr := planAssignmentContent(newAssignment)
	if oldErr != nil || newErr != nil || oldContent != newContent {
		return false, nil
	}
	oldClaims, newClaims := map[string]Claim{}, map[string]Claim{}
	for _, claim := range original.Claims {
		oldClaims[claim.ClaimID] = claim
	}
	for _, claim := range current.Claims {
		newClaims[claim.ClaimID] = claim
	}
	for _, id := range oldAssignment.ClaimIDs {
		old, foundOld := oldClaims[id]
		next, foundNext := newClaims[id]
		if !foundOld || !foundNext || !sameClaimContent(old, next) {
			return false, nil
		}
	}
	return true, nil
}

func issuedResultRow(state map[string]any, packet workpacket.ReviewResultPacket) map[string]any {
	reception := loopruntime.ReviewResultReception(state, packet.ID)
	if receipt, ok := reception["receipt"].(map[string]any); ok {
		return map[string]any{"result_receipt": receipt, "result_ref": receipt["result_ref"], "agent_id": packet.Binding.OwnerAgentID}
	}
	if loopruntime.ReviewResultPacketCurrent(state, packet) {
		row := resultPacketAssignmentRow(state, packet.Binding.AssignmentID)
		receipt, _ := row["result_receipt"].(map[string]any)
		if receipt == nil || receipt["packet_id"] == packet.ID {
			return row
		}
	}
	return nil
}

// IssuedResultSourceRoot resolves only a saved original execution relationship
// for historical work. Absence is unknown, never a fallback to Main/new owner.
func IssuedResultSourceRoot(root string, state map[string]any, packet workpacket.ReviewResultPacket, indexedReception ...map[string]any) (string, error) {
	if loopruntime.ReviewResultPacketCurrent(state, packet) {
		source, _, err := registeredResultPacketSourceRoot(root, state, packet)
		return source, err
	}
	var reception map[string]any
	if len(indexedReception) > 0 {
		reception = indexedReception[0]
	} else {
		reception = loopruntime.ReviewResultReception(state, packet.ID)
	}
	source, _ := reception["execution_root"].(string)
	if source == "" {
		receipt, _ := reception["receipt"].(map[string]any)
		source, _ = receipt["execution_root"].(string)
	}
	if source == "" {
		// A pre-upgrade issuance may not have cached the execution root. If
		// the exact original responsibility is still current and the original
		// workspace registration explicitly names it, reuse that relationship.
		// Never substitute Main or a merely similar/new responsibility.
		current, _ := IssuedResultAppliesToCurrent(root, state, packet, context.Background())
		if current {
			registry, err := workspace.Decode(state)
			if err == nil && registry != nil {
				execution, found := registry.Executions[packet.Binding.AssignmentID]
				main, mainErr := workspace.Canonical(root)
				if found && mainErr == nil && registry.MainRoot == main && execution.RuntimeID == packet.Binding.RuntimeID &&
					execution.BaselineGeneration == packet.Binding.BaselineGeneration && execution.AssignmentID == packet.Binding.AssignmentID &&
					execution.AgentID == packet.Binding.OwnerAgentID && execution.Status == "ready" && execution.Path != main {
					source = execution.Path
				}
			}
		}
	}
	if source == "" || !filepath.IsAbs(source) || filepath.Clean(source) != source {
		return "", errors.New("S7 original execution source is unknown after Plan replacement; preserve the original issuance and accepted facts for Main; current S7 is unaffected")
	}
	return source, nil
}

func loadOriginalResultPlan(root string, packet workpacket.ReviewResultPacket, ctx context.Context) (*Plan, *PlanPointer, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	b := packet.Binding
	data, err := workpacket.ReadFile(root, b.ReviewPlanRef, resultReceiptReadLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("read original S7 Plan: %w", err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, nil, err
	}
	a := findPlanAssignment(&plan, b.AssignmentID)
	if a == nil || plan.ReviewPlanID != b.ReviewPlanID || plan.BaselineGeneration != b.BaselineGeneration {
		return nil, nil, errors.New("original S7 Plan/Assignment differs from issuance")
	}
	return &plan, &PlanPointer{PlanID: b.ReviewPlanID, Path: b.ReviewPlanRef, Revision: b.ReviewPlanRevision, Status: "running"}, nil
}

func setIssuedResultReception(state map[string]any, packet workpacket.ReviewResultPacket, receipt ResultReceipt, result *Result) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	complete := (result.Verdict == "pass" || result.Verdict == "finding") && !hasOwnedBlocked(result, packet.Binding.ClaimIDs)
	return loopruntime.SetReviewResultReception(state, packet.ID, map[string]any{"receipt": value, "work_complete": complete, "execution_root": receipt.ExecutionRoot})
}

// An old handoff registers its immutable evidence, while active claims,
// assignment status, agent lifecycle and clean/queue projections stay current.
func applyOriginalResultEvidence(state map[string]any, result *Result, resultRel, responsibility string) error {
	return appendEvidence(state, map[string]any{"id": result.ResultID, "kind": "review_result", "path": resultRel, "status": "valid", "baseline_generation": result.BaselineGeneration, "produced_by": []any{result.ProducerAgentID}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": responsibility, "scope_refs": []any{}})
}

func requestWithContext(request SubmitRequest, ctx context.Context) SubmitRequest {
	request.Context = ctx
	return request
}

func replayResultRequest(root string, state map[string]any, request SubmitRequest, incoming *Result, answerContent string) (bool, error) {
	if request.issuedPacket == nil {
		return replayAcceptedResult(root, state, incoming, answerContent, request.Context)
	}
	p := *request.issuedPacket
	row := issuedResultRow(state, p)
	raw := row["result_receipt"]
	if raw == nil {
		return false, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}
	var receipt ResultReceipt
	if err = decodeOriginalResultReceipt(data, &receipt); err != nil {
		return false, err
	}
	if receipt.SubmissionKey != incoming.ResultID {
		if IssuedResultWorkComplete(state, p) {
			return false, errors.New("completed S7 issuance cannot accept a new professional attempt")
		}
		return false, nil
	}
	if err := compareReceivedProfessionalContent(root, receipt, answerContent); err != nil {
		return false, err
	}
	if err := verifyResultReceiptClosure(root, state, row, receipt, incoming, request.Context); err != nil {
		return false, err
	}
	return true, nil
}
