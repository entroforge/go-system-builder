package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const repairResultPacketTemplateID = "s9-repair-result-v1"

// RepairResultPackets reads the existing packet registry. It does not
// grant permission to answer, deliver a candidate or register a final Result.
func RepairResultPackets(state map[string]any) ([]workpacket.RepairResultPacket, error) {
	return RepairResultPacketsContext(context.Background(), state)
}
func RepairResultPacketsContext(ctx context.Context, state map[string]any) ([]workpacket.RepairResultPacket, error) {
	return repairResultPacketsMatchingContext(ctx, state, nil)
}

func repairResultPacketByID(packets []workpacket.RepairResultPacket, id string) (workpacket.RepairResultPacket, bool) {
	for _, packet := range packets {
		if packet.ID == id {
			return packet, true
		}
	}
	return workpacket.RepairResultPacket{}, false
}

func FindRepairResultPacket(state map[string]any, id string) (workpacket.RepairResultPacket, bool, error) {
	return FindRepairResultPacketContext(context.Background(), state, id)
}
func FindRepairResultPacketContext(ctx context.Context, state map[string]any, id string) (workpacket.RepairResultPacket, bool, error) {
	if id == "" {
		return workpacket.RepairResultPacket{}, false, fmt.Errorf("S9 packet ID is required")
	}
	rows, err := repairPacketRowsContext(ctx, state, repairResultPacketTemplateID)
	if err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	for _, row := range rows {
		if row["packet_id"] == id {
			packet, err := readRepairResultRowContext(ctx, row)
			return packet, err == nil, err
		}
	}
	return workpacket.RepairResultPacket{}, false, nil
}

// CurrentRepairResultPackets selects current responsibilities, not write grants.
func CurrentRepairResultPackets(root string, state map[string]any, owner string) ([]workpacket.RepairResultPacket, error) {
	return CurrentRepairResultPacketsContext(context.Background(), root, state, owner)
}
func CurrentRepairResultPacketsContext(ctx context.Context, root string, state map[string]any, owner string) ([]workpacket.RepairResultPacket, error) {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation, generationErr := integerField(baseline, "generation")
	all, err := repairResultPacketsMatchingContext(ctx, state, func(binding workpacket.RepairResultBinding) bool {
		b := binding.Sources
		return (generationErr != nil || generation <= 0 || b.BaselineGeneration == generation) &&
			(owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef && owners[b.RepairAssignmentID] == b.OwnerAgentID
	})
	if err != nil {
		return nil, err
	}
	return currentRepairResultPackets(root, state, owner, all, ctx)
}

func currentRepairResultPackets(_ string, state map[string]any, owner string, all []workpacket.RepairResultPacket, _ ...context.Context) ([]workpacket.RepairResultPacket, error) {
	candidates := currentRepairResultCandidates(state, owner, all)
	retired := make(map[string]bool)
	for _, p := range candidates {
		if p.Binding.Resume != nil {
			retired[p.Binding.Resume.PredecessorPacketID] = true
		}
	}
	selected := make([]workpacket.RepairResultPacket, 0, len(candidates))
	seen := make(map[string]bool)
	for _, p := range candidates {
		if retired[p.ID] {
			continue
		}
		slot := p.Binding.Sources.RepairAssignmentID
		if seen[slot] {
			return nil, fmt.Errorf("S9 current Assignment has multiple issued packets")
		}
		seen[slot] = true
		selected = append(selected, p)
	}
	return selected, nil
}

func currentRepairResultCandidates(state map[string]any, owner string, all []workpacket.RepairResultPacket) []workpacket.RepairResultPacket {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	workspace, _ := state["workspace"].(map[string]any)
	executions, _ := workspace["executions"].(map[string]any)
	candidates := make([]workpacket.RepairResultPacket, 0, len(all))
	for _, p := range all {
		b := p.Binding.Sources
		if (owner != "" && b.OwnerAgentID != owner) || b.RuntimeID != state["runtime_id"] || pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || owners[b.RepairAssignmentID] != b.OwnerAgentID {
			continue
		}
		var issued map[string]any
		if json.Unmarshal(p.Inputs.Execution, &issued) != nil || issued == nil {
			continue
		}
		execution, _ := executions[stringValue(issued["assignment_id"])].(map[string]any)
		if execution == nil || execution["runtime_id"] != b.RuntimeID || execution["agent_id"] != b.OwnerAgentID ||
			!hypothesisJSONEqual(execution["baseline_generation"], b.BaselineGeneration) ||
			!hypothesisJSONEqual(execution["execution_generation"], issued["execution_generation"]) ||
			execution["worktree_path"] != issued["worktree_path"] || stringValue(execution["rework_ref"]) != p.Binding.ReworkRef {
			continue
		}
		candidates = append(candidates, p)
	}
	return candidates
}

func currentRepairResultPacketsWithJournal(root string, state map[string]any, owner string, all, candidates []workpacket.RepairResultPacket, journal controlproof.JournalInspection) ([]workpacket.RepairResultPacket, error) {
	verifier, err := newRepairResultResumeVerifier(root, state, journal, all)
	if err != nil {
		return nil, err
	}
	return currentRepairResultPacketsWithVerifier(verifier, owner, candidates)
}

func currentRepairResultPacketsWithVerifier(verifier *repairResultResumeVerifier, owner string, candidates []workpacket.RepairResultPacket) ([]workpacket.RepairResultPacket, error) {
	selected := []workpacket.RepairResultPacket{}
	seen := map[string]bool{}
	for _, p := range candidates {
		b := p.Binding.Sources
		if _, resolved, resolutionErr := verifier.resolution(p, b.OwnerAgentID); resolutionErr != nil {
			return nil, fmt.Errorf("verify S9 resolution for current packet %s: %w", p.ID, resolutionErr)
		} else if resolved {
			// A verified resolution permanently retires its predecessor packet.
			continue
		}
		if err := verifier.verifyResume(p); err != nil {
			return nil, fmt.Errorf("verify S9 Resume lineage for current packet %s: %w", p.ID, err)
		}
		execution, eerr := repairResultExecution(verifier.state, p)
		if eerr != nil || stringValue(execution["rework_ref"]) != p.Binding.ReworkRef {
			continue
		}
		slot := b.RepairAssignmentID
		if seen[slot] {
			return nil, fmt.Errorf("S9 current Assignment has multiple issued packets")
		}
		seen[slot] = true
		selected = append(selected, p)
	}
	return selected, nil
}

func VerifyRepairResultPacketIssue(root string, state map[string]any, p workpacket.RepairResultPacket) error {
	return VerifyRepairResultPacketIssueContext(context.Background(), root, state, p)
}
func VerifyRepairResultPacketIssueContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket) error {
	stored, found, err := FindRepairResultPacketContext(repairPacketContext(root, ctx), state, p.ID)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(stored, p) {
		return fmt.Errorf("S9 original packet not found")
	}
	return nil
}

func VerifyRepairResultPacketIssueAndCurrent(root string, state map[string]any, p workpacket.RepairResultPacket) error {
	return VerifyRepairResultPacketIssueAndCurrentContext(context.Background(), root, state, p)
}
func VerifyRepairResultPacketIssueAndCurrentContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket) error {
	if err := VerifyRepairResultPacketIssueContext(ctx, root, state, p); err != nil {
		return err
	}
	return VerifyRepairResultPacketCurrentContext(ctx, root, state, p)
}

func verifyRepairResultPacketIssue(root string, state map[string]any, p workpacket.RepairResultPacket, _ controlproof.JournalInspection, contexts ...context.Context) error {
	return VerifyRepairResultPacketIssueContext(repairPacketContext(root, contexts...), root, state, p)
}

func verifyRepairResultPacketIssueWithVerifier(root string, state map[string]any, p workpacket.RepairResultPacket, _ *repairResultResumeVerifier) error {
	return VerifyRepairResultPacketIssueContext(context.Background(), root, state, p)
}

func ProjectRepairResultExecution(execution map[string]any) (json.RawMessage, error) {
	projection := map[string]any{}
	for _, key := range []string{
		"assignment_record", "worker_checks", "check_profiles",
		"purpose", "work_packet_id", "stage_entry", "check_location",
		"assignment_id", "runtime_id", "baseline_generation", "execution_generation", "agent_id",
		"worktree_path", "branch", "target_branch", "write_paths", "required_checks",
	} {
		if v, exists := execution[key]; exists && v != nil {
			projection[key] = v
		}
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	return canonicalRuntimeJSON(data)
}

// repairResultExecutionCoordinates are the structural coordinates of one
// attempt: which attempt this is, which stage it belongs to and where its
// work sits. They are reconciled field by field between the issued execution
// definition and the registered execution projection.
//
// A digest, hash, checksum, fingerprint, revision or round is deliberately
// absent. Those machine gates were retired (DEFECT-REMEDIATION-PLAN.md §13.1)
// while these structural facts are kept (§13.2 "原责任坐标"; §13.6
// "baseline_generation 保留"). The execution identity itself stays pinned by
// the binding-identity filter below, so it is not repeated here.
var repairResultExecutionCoordinates = []string{
	"execution_generation", // attempt coordinate: a replaced attempt is not the issued one
	"baseline_generation",  // stage coordinate: the baseline the attempt belongs to
	"runtime_id",           // runtime coordinate
	"agent_id",             // owner coordinate
	"worktree_path",        // work position coordinate
	"branch",               // work position coordinate
	"target_branch",        // integration target coordinate
}

// sameRepairResultExecutionCoordinates reconciles the structural coordinates of
// the issued execution definition with the registered execution. It returns the
// first coordinate that no longer agrees.
//
// A coordinate the issued attempt did not record is unknown rather than a
// fact, so it is not compared: an execution that gained its worktree path or
// branch after issuance has not drifted from the issued attempt.
func sameRepairResultExecutionCoordinates(issued, registered map[string]any) (string, bool) {
	for _, key := range repairResultExecutionCoordinates {
		issuedValue, issuedKnown := repairResultExecutionCoordinate(issued[key])
		registeredValue, registeredKnown := repairResultExecutionCoordinate(registered[key])
		if !issuedKnown {
			continue
		}
		if !registeredKnown || !hypothesisJSONEqual(issuedValue, registeredValue) {
			return key, false
		}
	}
	return "", true
}

// repairResultExecutionCoordinate reads one coordinate. An absent or empty
// value is unknown: the attempt did not freeze that fact.
func repairResultExecutionCoordinate(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		return typed, typed != ""
	case float64:
		return typed, typed != 0
	}
	return value, true
}

func repairResultExecution(state map[string]any, p workpacket.RepairResultPacket) (map[string]any, error) {
	b := p.Binding.Sources
	workspace, _ := state["workspace"].(map[string]any)
	executions, _ := workspace["executions"].(map[string]any)
	var frozen map[string]any
	if err := json.Unmarshal(p.Inputs.Execution, &frozen); err != nil {
		return nil, err
	}
	var selected map[string]any
	for key, raw := range executions {
		e, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("S9 execution registry contains a non-object")
		}
		id := stringValue(e["assignment_id"])
		if id != b.RepairAssignmentID && id != b.ManifestAssignmentID {
			continue
		}
		if key != id || e["agent_id"] != b.OwnerAgentID || e["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(e["baseline_generation"], b.BaselineGeneration) {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("S9 result has ambiguous registered canonical/manifest execution")
		}
		selected = e
	}
	if selected == nil {
		return nil, fmt.Errorf("S9 result has no exact registered execution")
	}
	// The issued attempt froze its own execution definition. A registered
	// execution that no longer agrees on any structural coordinate is a
	// different attempt, so this issued package must not be delivered as the
	// current one. The sibling blocker path reconciles the same coordinates
	// against the same frozen definition (internal/repair/
	// issued_repair_result_blocker.go).
	if key, same := sameRepairResultExecutionCoordinates(frozen, selected); !same {
		return nil, fmt.Errorf("S9 result current execution %s differs from the frozen assignment", key)
	}
	return selected, nil
}

// RepairResultExecution resolves the exact registered execution frozen by an
// issued S9 packet. It is a read-only projection and grants no execution,
// workspace or write permission.
func RepairResultExecution(state map[string]any, p workpacket.RepairResultPacket) (map[string]any, error) {
	return repairResultExecution(state, p)
}

// VerifyRepairResultSources checks the readable work records associated with
// this result. It does not certify completion or replay planning permissions.
func VerifyRepairResultSources(root string, state map[string]any, p workpacket.RepairResultPacket, issuedAfter int) error {
	return verifyRepairResultSources(root, state, p, controlproof.JournalInspection{}, issuedAfter)
}

func verifyRepairResultSources(_ string, state map[string]any, p workpacket.RepairResultPacket, _ controlproof.JournalInspection, _ int) error {
	if state["runtime_id"] != p.Binding.Sources.RuntimeID {
		return fmt.Errorf("S9 packet belongs to another Runtime")
	}
	return nil
}

func VerifyRepairResultPacketCurrent(root string, state map[string]any, p workpacket.RepairResultPacket) error {
	return VerifyRepairResultPacketCurrentContext(context.Background(), root, state, p)
}
func VerifyRepairResultPacketCurrentContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b := p.Binding.Sources
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	if state["runtime_id"] != b.RuntimeID || pointer["session_id"] != b.SessionID || owners[b.RepairAssignmentID] != b.OwnerAgentID {
		return fmt.Errorf("S9 original responsibility is not current")
	}
	return nil
}

func verifyRepairResultPacketCurrentWithVerifier(root string, state map[string]any, p workpacket.RepairResultPacket, _ *repairResultResumeVerifier) error {
	return VerifyRepairResultPacketCurrentContext(context.Background(), root, state, p)
}
