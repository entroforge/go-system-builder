package assignment

import (
	"context"
	"fmt"
	"strings"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// issuedRepairResultLifecycleTarget reports whether the current registered
// Agent slot has any historically issued S9 RepairResult packet. It is a
// lifecycle guard only: it grants no permission and deliberately ignores
// mutable source freshness, Agent state, worktree state, and answer files.
func issuedRepairResultLifecycleTarget(state map[string]any, owner string, contexts ...context.Context) (bool, error) {
	if strings.TrimSpace(owner) == "" {
		return false, fmt.Errorf("S9 lifecycle target owner is required")
	}
	ctx := context.Background()
	if len(contexts) != 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	packets, err := loopruntime.RepairResultPacketsForOwnerContext(ctx, state, owner)
	if err != nil {
		return false, fmt.Errorf("read registered S9 RepairResult history: %w", err)
	}
	for _, packet := range packets {
		matches, err := repairResultLifecyclePacketTargetsOwner(state, owner, packet)
		if err != nil {
			return false, err
		}
		if matches {
			// More than one packet can be retained for the same complete slot
			// across attempts. Any original issuance is sufficient to keep the
			// typed lifecycle guard in force.
			return true, nil
		}
	}
	return false, nil
}

// repairResultLifecyclePacketTargetsOwner mechanically compares one packet
// with the current registered Runtime slot. Production calls this only with
// packets returned by runtime.RepairResultPackets, which verifies the packet
// schema and canonical rebuild before this comparison.
func repairResultLifecyclePacketTargetsOwner(state map[string]any, owner string, packet workpacket.RepairResultPacket) (bool, error) {
	b := packet.Binding.Sources
	if b.OwnerAgentID != owner {
		return false, nil
	}
	if err := validateRepairResultLifecycleBinding(b); err != nil {
		return false, err
	}

	runtimeID, ok := state["runtime_id"].(string)
	if !ok || strings.TrimSpace(runtimeID) == "" {
		return false, fmt.Errorf("S9 lifecycle target Runtime identity is missing")
	}
	if b.RuntimeID != runtimeID {
		return false, nil
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, ok := lifecycleRepairPositiveInt(baseline["generation"])
	if !ok {
		return false, fmt.Errorf("S9 lifecycle target baseline generation is missing or invalid")
	}
	review, _ := state["review"].(map[string]any)
	round, ok := lifecycleRepairPositiveInt(review["round"])
	if !ok {
		return false, fmt.Errorf("S9 lifecycle target review round is missing or invalid")
	}
	if b.BaselineGeneration != generation || b.ReviewRound != round {
		return false, nil
	}

	entities, ok := state["entities"].(map[string]any)
	if !ok || entities == nil {
		return false, fmt.Errorf("S9 lifecycle target Runtime entities are missing")
	}
	agent, err := lifecycleRepairUniqueAgent(entities["agents"], owner)
	if err != nil {
		return false, err
	}
	agentTeamID, ok := agent["team_id"].(string)
	if !ok || strings.TrimSpace(agentTeamID) == "" {
		return false, fmt.Errorf("S9 lifecycle target Agent %s has no registered team", owner)
	}
	taskIDs, err := lifecycleRepairTaskIDs(agent["task_ids"])
	if err != nil {
		return false, fmt.Errorf("S9 lifecycle target Agent %s task registration: %w", owner, err)
	}
	promptRef, ok := agent["prompt_ref"].(string)
	if !ok || strings.TrimSpace(promptRef) == "" {
		return false, fmt.Errorf("S9 lifecycle target Agent %s has no registered prompt_ref", owner)
	}

	team, err := lifecycleRepairUniqueTeam(entities["teams"], agentTeamID)
	if err != nil {
		return false, err
	}
	manifestRef, ok := team["manifest_ref"].(string)
	if !ok || strings.TrimSpace(manifestRef) == "" {
		return false, fmt.Errorf("S9 lifecycle target Team %s has no registered manifest_ref", agentTeamID)
	}
	manifestSHA, ok := team["manifest_sha256"].(string)
	if !ok || !lifecycleRepairSHA256(manifestSHA) {
		return false, fmt.Errorf("S9 lifecycle target Team %s has no valid registered manifest_sha256", agentTeamID)
	}
	promptManifest, promptAssignment, ok := lifecycleRepairPromptRef(promptRef)
	if !ok || promptManifest != manifestRef {
		return false, fmt.Errorf("S9 lifecycle target Agent %s prompt_ref does not bind its registered Team manifest", owner)
	}

	// A fully registered current Agent/Team Assignment can prove that the
	// packet belongs to a different slot. This check precedes lookup of the old
	// RepairAssignment in assignment_owners so an ordinary new Assignment does
	// not become malformed merely because that old key is no longer present.
	if agentTeamID != b.TeamID || manifestRef != b.ManifestRef || manifestSHA != b.ManifestSHA256 || promptAssignment != b.ManifestAssignmentID {
		return false, nil
	}
	if !lifecycleRepairContainsTask(taskIDs, b.TaskID) {
		return false, fmt.Errorf("S9 lifecycle target Agent %s no longer registers the frozen TASK %s for the same Assignment", owner, b.TaskID)
	}

	pointer, ok := review["repair"].(map[string]any)
	if !ok || pointer == nil {
		return false, fmt.Errorf("S9 lifecycle target repair pointer is missing")
	}
	current := make(map[string]string, 8)
	for _, key := range []string{
		"session_id", "path", "sha256", "plan_ref", "plan_sha256",
		"contract_id", "contract_ref", "contract_sha256",
	} {
		value, exists := pointer[key].(string)
		if !exists || strings.TrimSpace(value) == "" {
			return false, fmt.Errorf("S9 lifecycle target repair pointer omits %s", key)
		}
		current[key] = value
	}
	for _, key := range []string{"sha256", "plan_sha256", "contract_sha256"} {
		if !lifecycleRepairSHA256(current[key]) {
			return false, fmt.Errorf("S9 lifecycle target repair pointer has invalid %s", key)
		}
	}
	if current["session_id"] != b.SessionID || current["path"] != b.SessionRef || current["sha256"] != b.SessionSHA256 ||
		current["plan_ref"] != b.PlanRef || current["plan_sha256"] != b.PlanSHA256 ||
		current["contract_id"] != b.ContractID || current["contract_ref"] != b.ContractRef || current["contract_sha256"] != b.ContractSHA256 {
		return false, nil
	}

	owners, ok := pointer["assignment_owners"].(map[string]any)
	if !ok || owners == nil {
		return false, fmt.Errorf("S9 lifecycle target repair pointer assignment_owners is missing")
	}
	registeredOwner, exists := owners[b.RepairAssignmentID]
	if !exists {
		return false, fmt.Errorf("S9 lifecycle target repair Assignment %s has no registered owner", b.RepairAssignmentID)
	}
	if registeredOwner != owner {
		return false, fmt.Errorf("S9 lifecycle target repair Assignment %s owner conflicts with Agent %s registration", b.RepairAssignmentID, owner)
	}
	return true, nil
}

func validateRepairResultLifecycleBinding(b workpacket.RepairPlanReportBinding) error {
	for name, value := range map[string]string{
		"runtime_id": b.RuntimeID, "session_id": b.SessionID, "session_ref": b.SessionRef,
		"plan_id": b.PlanID, "plan_ref": b.PlanRef, "contract_id": b.ContractID,
		"contract_ref": b.ContractRef, "repair_assignment_id": b.RepairAssignmentID,
		"manifest_assignment_id": b.ManifestAssignmentID, "team_id": b.TeamID,
		"task_id": b.TaskID, "manifest_ref": b.ManifestRef, "owner_agent_id": b.OwnerAgentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("S9 lifecycle packet binding omits %s", name)
		}
	}
	if b.BaselineGeneration < 1 || b.ReviewRound < 1 {
		return fmt.Errorf("S9 lifecycle packet binding has an invalid generation or round")
	}
	for name, value := range map[string]string{
		"session_sha256": b.SessionSHA256, "plan_sha256": b.PlanSHA256,
		"contract_sha256": b.ContractSHA256, "manifest_sha256": b.ManifestSHA256,
	} {
		if !lifecycleRepairSHA256(value) {
			return fmt.Errorf("S9 lifecycle packet binding has invalid %s", name)
		}
	}
	return nil
}

func lifecycleRepairUniqueAgent(raw any, owner string) (map[string]any, error) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("S9 lifecycle target Agent registry is missing or malformed")
	}
	var found map[string]any
	for i, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("S9 lifecycle target Agent registry row %d is malformed", i)
		}
		id, ok := row["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("S9 lifecycle target Agent registry row %d has no identity", i)
		}
		if id != owner {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("S9 lifecycle target Agent %s is registered more than once", owner)
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("S9 issued RepairResult owner Agent %s is no longer registered", owner)
	}
	return found, nil
}

func lifecycleRepairUniqueTeam(raw any, teamID string) (map[string]any, error) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("S9 lifecycle target Team registry is missing or malformed")
	}
	var found map[string]any
	for i, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("S9 lifecycle target Team registry row %d is malformed", i)
		}
		id, ok := row["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("S9 lifecycle target Team registry row %d has no identity", i)
		}
		if id != teamID {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("S9 lifecycle target Team %s is registered more than once", teamID)
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("S9 lifecycle target Agent Team %s is not registered", teamID)
	}
	return found, nil
}

func lifecycleRepairTaskIDs(raw any) ([]string, error) {
	var rows []any
	switch value := raw.(type) {
	case []any:
		rows = value
	case []string:
		rows = make([]any, len(value))
		for i := range value {
			rows[i] = value[i]
		}
	default:
		return nil, fmt.Errorf("task_ids is missing or malformed")
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("task_ids is empty")
	}
	seen := make(map[string]bool, len(rows))
	ids := make([]string, 0, len(rows))
	for _, rawID := range rows {
		id, ok := rawID.(string)
		if !ok || strings.TrimSpace(id) == "" || seen[id] {
			return nil, fmt.Errorf("task_ids has an empty, invalid, or duplicate entry")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func lifecycleRepairPromptRef(ref string) (manifestRef, assignmentID string, ok bool) {
	if strings.Count(ref, "#") != 1 {
		return "", "", false
	}
	parts := strings.SplitN(ref, "#", 2)
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func lifecycleRepairContainsTask(ids []string, taskID string) bool {
	for _, id := range ids {
		if id == taskID {
			return true
		}
	}
	return false
}

func lifecycleRepairSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func lifecycleRepairPositiveInt(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, number > 0
	case int32:
		return int(number), number > 0
	case int64:
		return int(number), number > 0
	case uint:
		return int(number), number > 0
	case uint64:
		return int(number), number > 0
	case float64:
		if number > 0 && number == float64(int(number)) {
			return int(number), true
		}
	}
	return 0, false
}
