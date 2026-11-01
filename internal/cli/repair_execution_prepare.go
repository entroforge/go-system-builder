package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type repairExecutionResolution struct {
	owner     string
	id        string
	execution map[string]any
	err       error
}

// The cache belongs to one Runtime snapshot and one request. Never reuse it
// after a Writer updates Runtime.
type repairExecutionResolver struct {
	ctx   context.Context
	root  string
	state map[string]any
	cache map[string]repairExecutionResolution
}

func newRepairExecutionResolver(ctx context.Context, root string, state map[string]any) *repairExecutionResolver {
	return &repairExecutionResolver{ctx: ctx, root: root, state: state, cache: map[string]repairExecutionResolution{}}
}

func (r *repairExecutionResolver) resolve(row repairDispatchChoice) repairExecutionResolution {
	if err := r.ctx.Err(); err != nil {
		return repairExecutionResolution{err: err}
	}
	pointer := repairPointerCLI(r.state)
	owner := row.AgentID
	if owner == "" {
		owner = stringMapCLI(pointer["assignment_owners"])[row.AssignmentID]
	}
	key := row.AssignmentID + "\x00" + owner
	if result, ok := r.cache[key]; ok {
		return result
	}
	id, execution := repairExecutionForChoice(r.state, row)
	result := repairExecutionResolution{owner: owner, id: id, execution: execution}

	r.cache[key] = result
	return result
}

// Read the registered execution projection only; reminders never re-run the
// scheduler, workspace decoder/authority checks, Git, or physical check proofs.
func repairExecutionForChoice(state map[string]any, row repairDispatchChoice) (string, map[string]any) {
	owner := repairDispatchOwner(state, row)
	id := row.AssignmentID
	pointer := repairPointerCLI(state)
	for _, packet := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
		if packet["template_id"] != "s9-plan-report-v1" {
			continue
		}
		binding := mapFieldCLI(packet, "binding")
		if binding["repair_assignment_id"] == row.AssignmentID && binding["owner_agent_id"] == owner && binding["runtime_id"] == state["runtime_id"] && binding["session_id"] == pointer["session_id"] && binding["plan_ref"] == pointer["plan_ref"] {
			id = stringValue(binding["manifest_assignment_id"])
		}
	}
	executions := mapFieldCLI(mapFieldCLI(state, "workspace"), "executions")
	if execution := mapFieldCLI(executions, id); len(execution) > 0 && execution["purpose"] == workspace.NativeDeliveryPurpose && (owner == "" || execution["agent_id"] == owner) {
		return id, execution
	}
	if execution := mapFieldCLI(executions, row.AssignmentID); len(execution) > 0 && execution["purpose"] == workspace.NativeDeliveryPurpose && (owner == "" || execution["agent_id"] == owner) {
		return row.AssignmentID, execution
	}
	return id, nil
}

func repairExecutionProjectionContext(ctx context.Context, root string, state map[string]any, row repairDispatchChoice, resolved repairExecutionResolution) string {
	id, execution := resolved.id, resolved.execution
	if len(execution) == 0 {
		return ""
	}
	if stringValue(execution["status"]) == "complete" {
		return fmt.Sprintf("Assignment %s is complete; Main integration/checks and cleanup are recorded. ", row.AssignmentID)
	}
	if stringValue(execution["delivery_ref"]) != "" {
		return fmt.Sprintf("Assignment %s: original Result received; Main performs actual integration and necessary checks. ", row.AssignmentID)
	}
	owner := stringValue(execution["agent_id"])
	path := stringValue(execution["worktree_path"])
	var b strings.Builder
	fmt.Fprintf(&b, "Original execution %s owner=%s status=%s; actual Worker root: %s; target branch=%s. ", id, owner, stringValue(execution["status"]), path, stringValue(execution["target_branch"]))
	if stringValue(execution["status"]) != "ready" {
		b.WriteString("This execution is not ready; retain it and follow its original preparation/recovery responsibility. ")
	}
	packets, err := s9ListenerQuestionnairesContext(ctx, root, state, owner, true)
	if err != nil {
		fmt.Fprintf(&b, "Current Result path projection is unavailable: %v. ", err)
	} else {
		found := false
		for _, packet := range packets {
			if packet.binding.RepairAssignmentID != row.AssignmentID {
				continue
			}
			found = true
			fmt.Fprintf(&b, "Original RepairResult view: %s; exact professional answer: %s. ", filepath.Join(path, filepath.FromSlash(packet.view)), filepath.Join(path, filepath.FromSlash(packet.answer)))
		}
		if !found {
			b.WriteString("The original RepairResult questionnaire is not prepared yet; Main/system must prepare it during the normal work startup and supply its exact path. No Result answer path was guessed. ")
		}
	}
	return b.String()
}

// This context reads the existing division choice and reports normal startup
// commands. Neither observation nor saving prepares a workspace.
func stageRepairExecutionPrepareContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) || stringValue(mapFieldCLI(snapshot.State, "lifecycle")["state"]) != "bug_resolution" || stringValue(repairPointerCLI(snapshot.State)["status"]) != "repairing" {
		return ""
	}
	paths := repairDispatchPaths(root, snapshot.State)
	if len(paths) != 1 {
		return "S9 execution preparation cannot resolve the original Builder division answer from the current Case/Session/Plan binding."
	}
	if err := contextForHook(request).Err(); err != nil {
		return "S9 current execution observation is unavailable: " + err.Error() + ". " + s9KnownMainHandoffsContext(contextForHook(request), root, snapshot)
	}
	plan, err := repairDispatchObservedPlan(root, snapshot.State)
	if err != nil {
		return fmt.Sprintf("S9 execution preparation at %s cannot read the original Plan: %v. %s", paths[0], err, s9KnownMainHandoffsContext(contextForHook(request), root, snapshot))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "S9 execution preparation uses the same Builder division answer: %s. ", paths[0])
	if questionnaireMissing(paths[0]) {
		b.WriteString("The same division answer is not prepared yet; Main/system must supply its rows from the original Plan and registered dispatch/execution facts during normal responsibility preparation. This event does not invent another report. ")
	}
	pointer := repairPointerCLI(snapshot.State)
	resolver := newRepairExecutionResolver(contextForHook(request), root, snapshot.State)
	choices := map[string]repairDispatchChoice{}
	if data, err := os.ReadFile(paths[0]); err == nil {
		var rows []repairDispatchChoice
		if json.Unmarshal(data, &rows) == nil {
			for _, row := range rows {
				choices[row.AssignmentID] = row
			}
		}
	}
	priority := func(id string) int {
		execution := resolver.resolve(repairDispatchChoice{AssignmentID: id}).execution
		if stringValue(execution["status"]) == "complete" {
			return 2
		}
		if stringValue(execution["delivery_ref"]) != "" {
			return 0
		}
		return 1
	}
	sort.SliceStable(plan.Assignments, func(i, j int) bool {
		return priority(plan.Assignments[i].AssignmentID) < priority(plan.Assignments[j].AssignmentID)
	})
	for _, assignment := range plan.Assignments {
		row := choices[assignment.AssignmentID]
		row.AssignmentID = assignment.AssignmentID
		if len(assignment.DependsOn) > 0 {
			fmt.Fprintf(&b, "Assignment %s depends_on=%v; Main must arrange the named predecessor's actual Result before starting dependent work. ", assignment.AssignmentID, assignment.DependsOn)
		}
		if len(assignment.ResourceLocks) > 0 {
			fmt.Fprintf(&b, "Assignment %s resource_locks=%v; Main must arrange work around these shared resources. ", assignment.AssignmentID, assignment.ResourceLocks)
		}
		owner := stringMapCLI(pointer["assignment_owners"])[assignment.AssignmentID]

		if owner == "" {
			fmt.Fprintf(&b, "Assignment %s awaits Builder selection at %s. ", assignment.AssignmentID, paths[0])
			continue
		}
		b.WriteString(prepareRepairExecutionFromChoiceResolved(contextForHook(request), root, snapshot.State, row, owner, resolver))
	}
	b.WriteString("Main owns Claude Code native spawn/resume and later integration; this reminder only reads current facts. ")
	return b.String()
}

// Keep the existing save adapter as pure feedback; Main runs normal startup.
func prepareRepairExecutionFromChoice(root string, state map[string]any, row repairDispatchChoice, owner string) string {
	return prepareRepairExecutionFromChoiceContext(context.Background(), root, state, row, owner)
}
func prepareRepairExecutionFromChoiceContext(ctx context.Context, root string, state map[string]any, row repairDispatchChoice, owner string) string {
	return prepareRepairExecutionFromChoiceResolved(ctx, root, state, row, owner, newRepairExecutionResolver(ctx, root, state))
}

func prepareRepairExecutionFromChoiceResolved(ctx context.Context, root string, state map[string]any, row repairDispatchChoice, owner string, resolver *repairExecutionResolver) string {
	resolved := resolver.resolve(row)
	if resolved.owner != "" {
		owner = resolved.owner
	}
	current := resolved.execution
	indexed, _ := s9ListenerQuestionnaires(state, owner, false)
	for _, question := range indexed {
		packet := workpacket.RepairPlanReportPacket{ID: question.id, Binding: question.binding, ViewPath: question.view, AnswerPath: question.answer, BriefPath: question.brief}
		if packet.Binding.OwnerAgentID != owner || packet.Binding.RepairAssignmentID != row.AssignmentID {
			continue
		}
		handoff := s9HandoffForBinding(ctx, root, runtime.Snapshot{State: state}, packet.Binding)
		brief := nativeS9BriefPath(packet)
		briefMissing := false
		if ctx.Err() == nil {
			briefMissing = !s8QuestionnaireFileExists(resolveRootPath(root, brief))
			handoff.Native = true
		}
		if !handoff.Native {
			continue
		}
		displayGap := ""
		if briefMissing {
			displayGap = "The original native brief display is missing at " + filepath.Join(root, brief) + "; preserve its original source packet and accepted work. "
		}
		if stringValue(current["status"]) == "complete" {
			return fmt.Sprintf("S9 Assignment %s is already integrated and cleaned through the original Main consumer; continue remaining professional responsibilities. ", row.AssignmentID)
		}
		if len(current) > 0 || handoff.PlanAccepted || handoff.Diagnostic != "" {
			return displayGap + handoff.summary(root) + " " + s9ObservedHandoffAction(ctx, root, runtime.Snapshot{State: state}, handoff, current) + " "
		}
		return displayGap + fmt.Sprintf("Main uses Claude Code native Agent for initial execution with this exact S9 brief; continue an existing actor through the supported same-actor continuation interface: %s. Builder continues its own plan, repair, self-checks and Result in its native worktree. No workspace prepare/launch is required. ", filepath.Join(root, brief))
	}
	if stringValue(current["status"]) == "complete" || stringValue(current["delivery_ref"]) != "" {
		return repairExecutionProjectionContext(ctx, root, state, row, resolved)
	}
	return fmt.Sprintf("S9 Assignment %s owner=%s has no current prepared PlanReport/brief. Main/system continues original preparation at %s. ", row.AssignmentID, owner, strings.Join(repairDispatchPaths(root, state), ", "))
}
