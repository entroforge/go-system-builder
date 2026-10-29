package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/checkexec"
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
	baseline, _ := state["baseline"].(map[string]any)
	gen, _ := integerField(baseline, "generation")
	owners, _ := pointer["assignment_owners"].(map[string]any)
	all, err := repairResultPacketsMatchingContext(ctx, state, func(binding workpacket.RepairResultBinding) bool {
		b := binding.Sources
		return (owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && b.BaselineGeneration == gen && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef && owners[b.RepairAssignmentID] == b.OwnerAgentID
	})
	if err != nil {
		return nil, err
	}
	return currentRepairResultPackets(root, state, owner, all, ctx)
}

func currentRepairResultPackets(root string, state map[string]any, owner string, all []workpacket.RepairResultPacket, contexts ...context.Context) ([]workpacket.RepairResultPacket, error) {
	candidates := currentRepairResultCandidates(state, owner, all)
	if len(candidates) == 0 {
		return nil, nil
	}
	journal, err := repairResultResumeJournal(root, state, contexts...)
	if err != nil {
		return nil, err
	}
	return currentRepairResultPacketsWithJournal(root, state, owner, all, candidates, journal)
}

func currentRepairResultCandidates(state map[string]any, owner string, all []workpacket.RepairResultPacket) []workpacket.RepairResultPacket {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	gen, _ := integerField(baseline, "generation")
	owners, _ := pointer["assignment_owners"].(map[string]any)
	workspace, _ := state["workspace"].(map[string]any)
	executions, _ := workspace["executions"].(map[string]any)
	candidates := make([]workpacket.RepairResultPacket, 0, len(all))
	for _, p := range all {
		b := p.Binding.Sources
		if (owner != "" && b.OwnerAgentID != owner) || b.RuntimeID != state["runtime_id"] || b.BaselineGeneration != gen || pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || owners[b.RepairAssignmentID] != b.OwnerAgentID {
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
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return err
	}
	return verifyRepairResultPacketIssue(root, state, p, j, ctx)
}

// VerifyRepairResultPacketIssueAndCurrent verifies the original issuance and
// current source/slot against one state snapshot, packet registry, journal,
// and Resume verifier. It does not cache authority across calls; callers that
// later mutate Runtime must perform their own fresh verification in the CAS.
func VerifyRepairResultPacketIssueAndCurrent(root string, state map[string]any, p workpacket.RepairResultPacket) error {
	return VerifyRepairResultPacketIssueAndCurrentContext(context.Background(), root, state, p)
}
func VerifyRepairResultPacketIssueAndCurrentContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket) error {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	journal, err := repairResultResumeJournal(root, state, ctx)
	if err != nil {
		return err
	}
	packets, err := repairResultPacketsForBindingContext(ctx, state, p.Binding)
	if err != nil {
		return err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, journal, packets)
	if err != nil {
		return err
	}
	if err := verifyRepairResultPacketIssueWithVerifier(root, state, p, verifier); err != nil {
		return err
	}
	return verifyRepairResultPacketCurrentWithVerifier(root, state, p, verifier)
}

func verifyRepairResultPacketIssue(root string, state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, contexts ...context.Context) error {
	packets, err := repairResultPacketsForBindingContext(repairPacketContext(root, contexts...), state, p.Binding)
	if err != nil {
		return err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, j, packets)
	if err != nil {
		return err
	}
	return verifyRepairResultPacketIssueWithVerifier(root, state, p, verifier)
}

func verifyRepairResultPacketIssueWithVerifier(root string, state map[string]any, p workpacket.RepairResultPacket, verifier *repairResultResumeVerifier) error {
	j := verifier.journal
	found, err := verifier.registered(p)
	if err != nil || !found {
		return fmt.Errorf("S9 packet is not exact registered row: %v", err)
	}
	i, ok := j.EventIndex[p.IssuanceEventID]
	if !ok {
		return fmt.Errorf("S9 original issuance event missing")
	}
	e := j.Events[i]
	actor, _ := e["actor"].(map[string]any)
	from, _ := e["from"].(map[string]any)
	to, _ := e["to"].(map[string]any)
	gen, ge := integerField(e, "baseline_generation")
	before, be := integerField(e, "before_revision")
	after, ae := integerField(e, "after_revision")
	rev, re := integerField(state, "revision")
	if e["event"] != "transition_committed" || e["outcome"] != "committed" || e["transition_id"] != "PACKET-ISSUE" || e["idempotency_key"] != "packet:"+p.ID || e["runtime_id"] != p.Binding.Sources.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || ge != nil || gen != p.Binding.Sources.BaselineGeneration || be != nil || ae != nil || after != before+1 || re != nil || after > rev || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !reflect.DeepEqual(from, to) {
		return fmt.Errorf("S9 original issuance tuple differs")
	}
	if err := verifyRepairResultSources(root, state, p, j, after); err != nil {
		return err
	}
	return verifier.verifyResume(p)
}

// ProjectRepairResultExecution retains the registered execution definition.
// It excludes progress, delivery and attempt fields; attempt is bound separately.
// This formatter establishes no source or runner authority.
func ProjectRepairResultExecution(execution map[string]any) (json.RawMessage, error) {
	projection := map[string]any{}
	for _, key := range []string{
		"assignment_record", "assignment_record_sha256", "worker_checks", "check_profiles",
		"purpose", "work_packet_id", "stage_entry", "check_location",
		"assignment_id", "runtime_id", "baseline_generation", "execution_generation", "agent_id",
		"worktree_path", "branch", "target_branch", "base_commit", "write_paths", "required_checks",
		"inputs", "bootstrap_sha256",
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
		projection, err := ProjectRepairResultExecution(e)
		if err != nil {
			return nil, err
		}
		canonical, err := canonicalRuntimeJSON(p.Inputs.Execution)
		if err != nil || string(projection) != string(canonical) || workpacket.Digest(projection) != p.Binding.ExecutionSHA256 {
			return nil, fmt.Errorf("S9 registered execution definition differs from issued result")
		}
		selected = e
	}
	if selected == nil {
		return nil, fmt.Errorf("S9 result has no exact registered execution")
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

func verifyRepairResultSources(root string, state map[string]any, p workpacket.RepairResultPacket, _ controlproof.JournalInspection, _ int) error {
	b := p.Binding.Sources
	data, err := workpacket.ReadFile(root, p.Binding.PlanReportRef, 20<<20)
	if err != nil {
		return err
	}
	var report map[string]any
	if json.Unmarshal(data, &report) != nil || report == nil || report["assignment_id"] != b.RepairAssignmentID || report["session_id"] != b.SessionID || report["plan_id"] != b.PlanID || report["agent_id"] != b.OwnerAgentID {
		return fmt.Errorf("S9 result PlanReport does not describe this responsibility")
	}
	var task map[string]any
	if json.Unmarshal(p.Inputs.Sources.Task, &task) != nil || task == nil || task["task_id"] != b.TaskID {
		return fmt.Errorf("S9 result task does not describe this responsibility")
	}
	var execution map[string]any
	if json.Unmarshal(p.Inputs.Execution, &execution) != nil || execution == nil {
		return fmt.Errorf("S9 result execution input invalid")
	}
	if (execution["assignment_id"] != b.RepairAssignmentID && execution["assignment_id"] != b.ManifestAssignmentID) || execution["runtime_id"] != b.RuntimeID || execution["agent_id"] != b.OwnerAgentID || !hypothesisJSONEqual(execution["baseline_generation"], b.BaselineGeneration) {
		return fmt.Errorf("S9 result execution describes another responsibility")
	}
	commands, valid := checkpointStringSlice(execution["required_checks"])
	if !valid && execution["required_checks"] != nil {
		return fmt.Errorf("S9 execution required_checks must be commands")
	}
	var manifestAssignment map[string]any
	if json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment) != nil || manifestAssignment == nil || manifestAssignment["assignment_id"] != b.ManifestAssignmentID {
		return fmt.Errorf("S9 result manifest assignment differs")
	}
	if declared, exists := manifestAssignment["required_checks"]; exists && !hypothesisJSONEqual(declared, commands) {
		return fmt.Errorf("S9 execution checks differ from the configured manifest checks")
	}
	var profiles []checkexec.Profile
	if raw := execution["check_profiles"]; raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(encoded, &profiles); err != nil {
			return err
		}
	}
	if len(profiles) > 0 {
		if err := checkexec.ValidateProfiles(profiles, commands, stringValue(execution["check_location"])); err != nil {
			return err
		}
	}
	// Missing commands/profiles remain an actual Main configuration or execution
	// gap. Recording the professional result here supplies no passing checks.
	return nil
}

// VerifyRepairResultPacketCurrent checks fresh original source and definition.
// It is not a host/session, PLAN, reported-completion or runner permission.
func VerifyRepairResultPacketCurrent(root string, state map[string]any, p workpacket.RepairResultPacket) error {
	return VerifyRepairResultPacketCurrentContext(context.Background(), root, state, p)
}
func VerifyRepairResultPacketCurrentContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket) error {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	journal, err := repairResultResumeJournal(root, state, ctx)
	if err != nil {
		return err
	}
	packets, err := repairResultPacketsForBindingContext(ctx, state, p.Binding)
	if err != nil {
		return err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, journal, packets)
	if err != nil {
		return err
	}
	return verifyRepairResultPacketCurrentWithVerifier(root, state, p, verifier)
}

func verifyRepairResultPacketCurrentWithVerifier(root string, state map[string]any, p workpacket.RepairResultPacket, verifier *repairResultResumeVerifier) error {
	b := p.Binding.Sources
	journal, packets := verifier.journal, verifier.packets
	if err := verifier.verifyResume(p); err != nil {
		return fmt.Errorf("verify S9 Resume lineage: %w", err)
	}
	if _, resolved, err := verifier.resolution(p, b.OwnerAgentID); err != nil {
		return fmt.Errorf("verify S9 resolution history: %w", err)
	} else if resolved {
		return fmt.Errorf("S9 RepairResult packet was retired by an accepted blocker_resolved event")
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	investigation, _ := review["investigation"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	life, _ := state["lifecycle"].(map[string]any)
	if !PacketEnabled(state) || state["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(baseline["generation"], b.BaselineGeneration) || !hypothesisJSONEqual(review["round"], b.ReviewRound) || life["state"] != "bug_resolution" || life["phase"] != "fixing" || pointer["status"] != "repairing" || pointer["session_id"] != b.SessionID || pointer["path"] != b.SessionRef || pointer["sha256"] != b.SessionSHA256 || pointer["plan_ref"] != b.PlanRef || pointer["plan_sha256"] != b.PlanSHA256 || pointer["contract_ref"] != b.ContractRef || pointer["contract_sha256"] != b.ContractSHA256 || pointer["case_id"] != b.CaseID || investigation["case_id"] != b.CaseID || investigation["path"] != b.CaseRef || investigation["sha256"] != b.CaseSHA256 || investigation["status"] != "contract_approved" {
		return fmt.Errorf("S9 result source/phase authority changed; recover by named repair dispatch")
	}
	refs, err := repairPlanReportIndexedRefs(state)
	if err != nil {
		return err
	}
	reportFound := false
	for _, ref := range refs {
		if ref["path"] == p.Binding.PlanReportRef && ref["sha256"] == p.Binding.PlanReportSHA256 {
			reportFound = true
		}
	}
	if !reportFound {
		return fmt.Errorf("S9 current result has no exact original PlanReport index")
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	if owners[b.RepairAssignmentID] != b.OwnerAgentID {
		return fmt.Errorf("S9 result original owner differs")
	}
	execution, err := repairResultExecution(state, p)
	if err != nil {
		return err
	}
	// Native delivery is an observed original worktree, not a legacy launched
	// execution. Its purpose is part of the frozen execution projection checked
	// above. All other executions retain the original bootstrap requirement.
	if execution["status"] != "ready" || stringValue(execution["rework_ref"]) != p.Binding.ReworkRef ||
		(execution["purpose"] != "native_delivery" && stringValue(execution["bootstrap_sha256"]) == "") {
		return fmt.Errorf("S9 result execution/attempt is not current or lacks its required bootstrap")
	}
	path := stringValue(execution["worktree_path"])
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("S9 result registered Worker root is not canonical")
	}
	entities, _ := state["entities"].(map[string]any)
	var manifestAssignment map[string]any
	_ = json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment)
	count := 0
	for _, agent := range objectRows(entities["agents"]) {
		if agent["id"] != b.OwnerAgentID {
			continue
		}
		tasks, _ := checkpointStringSlice(agent["task_ids"])
		if agent["role"] == manifestAssignment["role_family"] && agent["team_id"] == b.TeamID && agent["prompt_ref"] == b.ManifestRef+"#"+b.ManifestAssignmentID && hypothesisStringContains(tasks, b.TaskID) {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("S9 result original agent registration differs")
	}
	if i, exists := journal.EventIndex[p.IssuanceEventID]; exists {
		for _, event := range journal.Events[i+1:] {
			if event["transition_id"] == "S9-SESSION-OPEN" || event["transition_id"] == "PTR-BUG-09" {
				return fmt.Errorf("S9 Session/Plan changed after result issuance")
			}
			if event["transition_id"] == "WORKSPACE-REWORK" {
				refs, _ := checkpointStringSlice(event["evidence_ids"])
				if !hypothesisStringContains(refs, b.RepairAssignmentID) && !hypothesisStringContains(refs, b.ManifestAssignmentID) {
					continue
				}
				for _, ref := range refs {
					if ref != p.Binding.ReworkRef && strings.Contains(ref, "/rework/") {
						return fmt.Errorf("S9 old result attempt was retired by original rework")
					}
				}
			}
		}
	}
	registered, err := verifier.registered(p)
	if err != nil {
		return err
	}
	if registered {
		// A current check must prove that this exact issued packet is the
		// unique active packet for its RepairAssignment. Reuse the registry and
		// Resume verifier already built for this snapshot so predecessor,
		// resolution, issue and lineage facts are not reread.
		candidates := currentRepairResultCandidates(state, b.OwnerAgentID, packets)
		sameSlot := make([]workpacket.RepairResultPacket, 0, 1)
		for _, candidate := range candidates {
			if candidate.Binding.Sources.RepairAssignmentID == b.RepairAssignmentID {
				sameSlot = append(sameSlot, candidate)
			}
		}
		current, err := currentRepairResultPacketsWithVerifier(verifier, b.OwnerAgentID, sameSlot)
		if err != nil {
			return fmt.Errorf("verify unique current S9 RepairAssignment packet: %w", err)
		}
		if len(current) != 1 || current[0].ID != p.ID {
			return fmt.Errorf("S9 packet is not the unique current issued RepairAssignment packet")
		}
	}
	return nil
}
