package hookctx

import (
	"encoding/json"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// projectNativeOwnedAssignments gives PreToolUse an exact, trusted projection
// of currently frozen native WorkerChecks. Invalid context/activation/source
// bindings simply produce no native allow entry; the CLI still revalidates all
// sources and execution coordinates before invoking the runner.
func projectNativeOwnedAssignments(root string, snapshot runtime.Snapshot, assignments []AssignmentContext, registry *workspace.ExecutionRegistry) []policy.NativeOwnedAssignment {
	var projected []policy.NativeOwnedAssignment
	for _, assignment := range assignments {
		if assignment.AssignmentRecord == nil || assignment.AssignmentID == "" || assignment.OwnerAgentID == "" {
			continue
		}
		record, found, err := assignmentrecord.ForOwner(snapshot.State, assignment.OwnerAgentID)
		if err != nil || !found || record.Reference() != *assignment.AssignmentRecord || record.AssignmentID != assignment.AssignmentID || record.DispatchMode != "plan_checkpoint" || record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) == 0 {
			continue
		}
		if err := assignmentrecord.ValidateRecord(record); err != nil {
			continue
		}
		if registry != nil {
			independent := false
			for _, execution := range registry.Executions {
				if execution.AgentID == record.OwnerAgentID || execution.AssignmentID == record.AssignmentID {
					independent = true
					break
				}
			}
			if independent {
				continue
			}
		}
		contextBound := nativeContextBound(root, snapshot.State, record)
		activated := false
		if ref, err := plancheckpoint.ActivatedRef(root, snapshot, record.OwnerAgentID); err == nil && ref != "" {
			activated = true
		}
		checks := make([]policy.NativeOwnedCheck, 0, len(record.ClosingContract.WorkerChecks))
		for _, check := range record.ClosingContract.WorkerChecks {
			checks = append(checks, policy.NativeOwnedCheck{Index: check.Index, Command: check.Command})
		}
		value, err := policy.NativeOwnedCheckProjection(record.AssignmentID, record.OwnerAgentID, assignment.WorktreePath, nativeOwnerState(snapshot.State, record.OwnerAgentID), contextBound, activated, checks)
		if err == nil {
			projected = append(projected, value)
		}
	}
	return projected
}

func nativeOwnerState(state map[string]any, owner string) string {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var result string
	found := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] != owner || found {
			continue
		}
		found = true
		version, _ := state["schema_version"].(string)
		if version == "" {
			version = "1.2.0"
		}
		projection, err := workstate.AgentProjection(row, version)
		if err == nil {
			result = projection.Activity
		}
	}
	return result
}

func nativeContextBound(root string, state map[string]any, record assignmentrecord.Record) bool {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var binding map[string]any
	found := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] != record.OwnerAgentID {
			continue
		}
		if found {
			return false
		}
		found = true
		binding, _ = row["context_binding"].(map[string]any)
	}
	if !found || binding == nil || number(binding["version"]) != 1 || number(binding["generation"]) < 1 {
		return false
	}
	ref, _ := binding["context_ref"].(map[string]any)
	path, _ := ref["path"].(string)
	if path == "" {
		return false
	}
	data, err := workpacket.ReadFile(root, path, 2<<20)
	if err != nil || schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data) != nil {
		return false
	}
	var context struct {
		RuntimeID  string            `json:"runtime_id"`
		Generation int               `json:"baseline_generation"`
		SourceRoot string            `json:"source_root"`
		Inputs     map[string]string `json:"frozen_inputs"`
		Selection  struct {
			ProfileVersion string `json:"profile_version"`
			Mode           string `json:"mode"`
		} `json:"context_selection"`
		Dispatch struct {
			AssignmentID       string                            `json:"assignment_id"`
			Owner              string                            `json:"owner"`
			AssignmentRevision int                               `json:"assignment_revision"`
			AssignmentRecord   *assignmentrecord.RecordReference `json:"assignment_record"`
			ClosingContract    *assignmentrecord.ClosingContract `json:"closing_contract"`
		} `json:"dispatch"`
	}
	if json.Unmarshal(data, &context) != nil {
		return false
	}
	canonicalRoot, err := workspace.Canonical(root)
	if err != nil || context.SourceRoot != canonicalRoot || context.RuntimeID != record.RuntimeID || context.Generation != record.BaselineGeneration {
		return false
	}
	selection := binding["profile_version"] == context.Selection.ProfileVersion && binding["mode"] == context.Selection.Mode
	if !selection {
		return false
	}
	return context.Dispatch.AssignmentID == record.AssignmentID && context.Dispatch.Owner == record.OwnerAgentID &&
		context.Dispatch.AssignmentRevision == record.Revision && context.Dispatch.AssignmentRecord != nil &&
		*context.Dispatch.AssignmentRecord == record.Reference() &&
		reflect.DeepEqual(context.Dispatch.ClosingContract, record.ClosingContract)
}

func number(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}
