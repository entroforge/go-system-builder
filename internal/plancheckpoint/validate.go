// Package plancheckpoint validates the existing plan evidence; it owns no state.
package plancheckpoint

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"os"
	"path/filepath"
	"strings"
)

func Validate(root string, snapshot runtime.Snapshot, agentID, ref string) error {
	if ref == "" {
		return fmt.Errorf("plan_ref is required")
	}
	abs := ref
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(ref))
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return fmt.Errorf("resolve plan_ref: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("plan_ref %q is outside the repository", ref)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read plan_ref %q: %w", ref, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return fmt.Errorf("plan_ref schema: %w", err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return fmt.Errorf("decode plan_ref: %w", err)
	}
	if message["message_type"] != "plan_report" || message["agent_id"] != agentID {
		return fmt.Errorf("plan_ref must be a plan_report for Agent %s", agentID)
	}
	if runtimeID, _ := snapshot.State["runtime_id"].(string); runtimeID != "" && message["runtime_id"] != runtimeID {
		return fmt.Errorf("plan_ref runtime_id does not match the current runtime")
	}
	ptr := review.PlanPointerFromState(snapshot.State)
	assignmentID, _ := message["assignment_id"].(string)
	if ptr != nil {
		if revision := intValue(message["assignment_revision"]); revision != ptr.Revision {
			// A non-S7 workgroup may be active while the previous S7 plan is
			// still present in the projection. Only enforce the ReviewPlan
			// revision when the submitted Assignment is actually one of its
			// rows; S8/S9 assignments are bound by their manifest below.
			reviewMap, _ := snapshot.State["review"].(map[string]any)
			assignments, _ := reviewMap["assignments"].(map[string]any)
			if _, exists := assignments[assignmentID]; exists {
				return fmt.Errorf("plan_ref assignment_revision %d does not match ReviewPlan revision %d", revision, ptr.Revision)
			}
		}
		reviewMap, _ := snapshot.State["review"].(map[string]any)
		assignments, _ := reviewMap["assignments"].(map[string]any)
		row, _ := assignments[assignmentID].(map[string]any)
		if row != nil {
			if row["agent_id"] != agentID {
				return fmt.Errorf("plan_ref Assignment %s is not dispatched to Agent %s", assignmentID, agentID)
			}
			if revision := intValue(message["assignment_revision"]); revision != ptr.Revision {
				return fmt.Errorf("plan_ref assignment_revision %d does not match ReviewPlan revision %d", revision, ptr.Revision)
			}
			return nil
		}
	}
	return validateManifestPlanReportCheckpoint(root, snapshot, message, agentID)
}

// validateManifestPlanReportCheckpoint validates the generic L4 checkpoint
// for Builder/Investigator workgroups that are not S7 ReviewPlan rows. Their
// Assignment identity lives in the fingerprinted team manifest; the manifest
// assignment is immutable for the lifetime of that dispatch, so its generic
// checkpoint revision is deliberately 1. S9 keeps a separate domain
// RepairAssignment (repair-assignment-*) and maps it to the platform-safe
// manifest id (assignment-s9-*); this function validates the latter while the
// S9 domain PlanReport validates the former.
func validateManifestPlanReportCheckpoint(root string, snapshot runtime.Snapshot, message map[string]any, agentID string) error {
	assignmentID := strValue(message["assignment_id"])
	teamID := strValue(message["team_id"])
	taskID := strValue(message["task_id"])
	if assignmentID == "" || teamID == "" || taskID == "" {
		return fmt.Errorf("manifest-bound plan_ref requires assignment_id, team_id, and task_id")
	}
	if revision := intValue(message["assignment_revision"]); revision != 1 {
		return fmt.Errorf("manifest-bound Assignment %s uses assignment_revision=1; got %d", assignmentID, revision)
	}

	entities, _ := snapshot.State["entities"].(map[string]any)
	var agent map[string]any
	rawAgents, _ := entities["agents"].([]any)
	for _, raw := range rawAgents {
		candidate, _ := raw.(map[string]any)
		if strValue(candidate["id"]) == agentID {
			agent = candidate
			break
		}
	}
	if agent == nil {
		return fmt.Errorf("manifest-bound plan_ref Agent %s is not registered", agentID)
	}
	if recordedTeam := strValue(agent["team_id"]); recordedTeam != "" && recordedTeam != teamID {
		return fmt.Errorf("plan_ref team_id %s does not match Agent %s team %s", teamID, agentID, recordedTeam)
	}
	if !containsStringValue(stringsValue(agent["task_ids"]), taskID) {
		return fmt.Errorf("plan_ref TASK %s is outside Agent %s assignment", taskID, agentID)
	}

	var teamRow map[string]any
	rawTeams, _ := entities["teams"].([]any)
	for _, raw := range rawTeams {
		candidate, _ := raw.(map[string]any)
		if strValue(candidate["id"]) == teamID {
			teamRow = candidate
			break
		}
	}
	if teamRow == nil {
		return fmt.Errorf("manifest-bound plan_ref team %s is not registered", teamID)
	}
	manifestRef := strValue(teamRow["manifest_ref"])
	if manifestRef == "" {
		return fmt.Errorf("team %s has no manifest_ref for the plan checkpoint", teamID)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root for manifest: %w", err)
	}
	manifestPath := resolveRootPath(root, manifestRef)
	manifestAbs, err := filepath.Abs(manifestPath)
	if err != nil {
		return fmt.Errorf("resolve team manifest %q: %w", manifestRef, err)
	}
	manifestRel, err := filepath.Rel(rootAbs, manifestAbs)
	if err != nil || manifestRel == ".." || strings.HasPrefix(manifestRel, ".."+string(filepath.Separator)) || filepath.IsAbs(manifestRel) {
		return fmt.Errorf("team manifest %q is outside the repository", manifestRef)
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read dispatched team manifest %s: %w", manifestRef, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return fmt.Errorf("dispatched team manifest schema: %w", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		return fmt.Errorf("dispatched team manifest semantics: %w", err)
	}
	var manifest struct {
		RuntimeID   string `json:"runtime_id"`
		WorkgroupID string `json:"workgroup_id"`
		Assignments []struct {
			AssignmentID       string `json:"assignment_id"`
			AgentID            string `json:"agent_id"`
			AgentDefinitionRef string `json:"agent_definition_ref"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode dispatched team manifest: %w", err)
	}
	if manifest.RuntimeID != strValue(snapshot.State["runtime_id"]) {
		return fmt.Errorf("plan_ref manifest runtime_id does not match the current runtime")
	}
	if manifest.WorkgroupID != teamID {
		return fmt.Errorf("plan_ref team_id %s does not match manifest workgroup_id %s", teamID, manifest.WorkgroupID)
	}
	for _, assignment := range manifest.Assignments {
		if assignment.AssignmentID != assignmentID {
			continue
		}
		if assignment.AgentID != agentID {
			return fmt.Errorf("plan_ref Assignment %s is owned by Agent %s, not %s", assignmentID, assignment.AgentID, agentID)
		}
		return nil
	}
	return fmt.Errorf("plan_ref Assignment %s is not declared by manifest %s", assignmentID, manifestRef)
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func strValue(v any) string { s, _ := v.(string); return s }
func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		value, _ := n.Int64()
		return int(value)
	case float64:
		return int(n)
	}
	return 0
}
func stringsValue(v any) []string {
	var out []string
	switch xs := v.(type) {
	case []any:
		for _, x := range xs {
			out = append(out, strValue(x))
		}
	case []string:
		return xs
	}
	return out
}
func resolveRootPath(root, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(root, ref)
}
