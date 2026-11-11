package assignment

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// CollectWorkerCheckFacts reads only original registered machine receipts.
// It leaves artifact paths unset: the original CompleteTask Apply publishes
// these exact bytes and fills the immutable per-Result paths under its lock.
func CollectWorkerCheckFacts(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record) ([]assignmentresult.WorkerCheckFact, map[string][]byte, error) {
	artifacts := map[string][]byte{}
	if record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) == 0 {
		return nil, artifacts, nil
	}
	facts := make([]assignmentresult.WorkerCheckFact, len(record.ClosingContract.WorkerChecks))
	for i, check := range record.ClosingContract.WorkerChecks {
		facts[i] = assignmentresult.WorkerCheckFact{Index: i, Command: check.Command, Status: "unavailable", ExecutionKind: "native", Record: record.Reference(), RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID}
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return facts, artifacts, nil
	}
	if registry != nil {
		if execution, found := registry.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration); found && execution.AgentID == record.OwnerAgentID && execution.AssignmentRecord != nil && *execution.AssignmentRecord == record.Reference() {
			for i, check := range record.ClosingContract.WorkerChecks {
				facts[i].ExecutionKind, facts[i].ExecutionGeneration = "workspace", execution.Generation
				facts[i].Status = "missing"
				facts[i].Checkout = execution.Path
				for j := len(execution.OwnedCheckReceipts) - 1; j >= 0; j-- {
					row := execution.OwnedCheckReceipts[j]
					if row.WorkerCheckIndex == i {
						collectWorkerReceipt(ctx, root, check, row.Receipt, &facts[i], artifacts)
						break
					}
				}
			}
			clearUnprovedWorkerCoordinates(facts)
			return facts, artifacts, nil
		}
	}
	agent, found := findCompletionAgent(state, record.OwnerAgentID)
	if !found {
		return facts, artifacts, nil
	}
	if binding, ok := agent["context_binding"].(map[string]any); ok {
		if contextRef, ok := binding["context_ref"].(map[string]any); ok {
			if path, _ := contextRef["path"].(string); path != "" {
				for i := range facts {
					facts[i].Status = "missing"
				}
			}
		}
	}
	data, err := json.Marshal(agent["owned_check_receipts"])
	if err != nil {
		return facts, artifacts, nil
	}
	var rows []NativeOwnedCheckReceipt
	if err := json.Unmarshal(data, &rows); err != nil {
		return facts, artifacts, nil
	}
	for i, check := range record.ClosingContract.WorkerChecks {
		for j := len(rows) - 1; j >= 0; j-- {
			row := rows[j]
			if row.CheckIndex != i {
				continue
			}
			if row.Record != record.Reference() || row.RuntimeID != record.RuntimeID || row.BaselineGeneration != record.BaselineGeneration || row.AssignmentID != record.AssignmentID || row.OwnerAgentID != record.OwnerAgentID {
				facts[i].Status = "unavailable"
				break
			}
			facts[i].Checkout = row.Checkout
			collectWorkerReceipt(ctx, root, check, row.ToolReceipt, &facts[i], artifacts)
			break
		}
	}
	clearUnprovedWorkerCoordinates(facts)
	return facts, artifacts, nil
}

func clearUnprovedWorkerCoordinates(facts []assignmentresult.WorkerCheckFact) {
	for i := range facts {
		if facts[i].OriginalReceipt == nil {
			facts[i].Checkout = ""
		}
	}
}

func collectWorkerReceipt(ctx context.Context, root string, check assignmentrecord.WorkerCheck, ref checkexec.ToolReceipt, fact *assignmentresult.WorkerCheckFact, artifacts map[string][]byte) {
	fact.Status = "unavailable"
	want := checkexec.Binding{RuntimeID: fact.RuntimeID, Generation: fact.BaselineGeneration, AssignmentID: fact.Record.AssignmentID, Owner: fact.OwnerAgentID, ExecutionGeneration: fact.ExecutionGeneration, Responsibility: "owned_check"}
	if ref.Binding != want || ref.Checkout != fact.Checkout || ref.Command != check.Command {
		return
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Join(root, ".claude/evidence/workspace-checks"), ref)
	if err != nil || verified.Observation.ProfileID != check.Profile.ID || verified.Observation.ProfileVersion != check.Profile.Version {
		return
	}
	rel, err := filepath.Rel(root, ref.Path)
	if err != nil || !filepath.IsLocal(rel) {
		return
	}
	data, err := workpacket.ReadFile(root, filepath.ToSlash(rel), 4<<20)
	if err != nil {
		return
	}
	var raw struct {
		Index  int    `json:"check_index"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || raw.Index != check.Index || (raw.Status != "pass" && raw.Status != "fail") {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	observation := verified.Observation
	if observation.Permissions != nil {
		permissions := *observation.Permissions
		observation.Permissions = &permissions
	}
	fact.Status, fact.Observation, fact.OriginalReceipt = raw.Status, &observation, &ref
}
