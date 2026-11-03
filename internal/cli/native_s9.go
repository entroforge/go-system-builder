package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// These are professional input/output references, not platform execution slots.
func nativeS9BriefPath(p workpacket.RepairPlanReportPacket) string {
	if p.BriefPath != "" {
		return p.BriefPath
	}
	packetPath := repair.NativeS9PacketPath(p.ID)
	if p.ViewPath == "" {
		return filepath.ToSlash(filepath.Join(".claude", "s9-briefs", packetPath, "brief.md"))
	}
	dir := filepath.ToSlash(filepath.Join(filepath.Dir(p.ViewPath), packetPath, "native"))
	return dir + "/brief.md"
}

// nativeS9AuthorPaths provides the current author's original Plan and Result paths.
func nativeS9AuthorPaths(_ string, p workpacket.RepairPlanReportPacket) (string, string, error) {
	return repair.NativeS9PrivatePaths(p.ID)
}

// A registered packet is an observation candidate, not proof that a Worker
// owns it or that its professional artifacts have been received.
func nativeS9RegisteredPlanPackets(root string, state map[string]any) []workpacket.RepairPlanReportPacket {
	if stringValue(mapFieldCLI(state, "lifecycle")["state"]) != "bug_resolution" || !s9ProfessionalPhase(stringValue(mapFieldCLI(state, "lifecycle")["phase"])) {
		return nil
	}
	packets, err := runtime.CurrentRepairPlanReportPackets(root, state, "")
	if err != nil {
		return nil
	}
	pointer := repairPointerCLI(state)
	var current []workpacket.RepairPlanReportPacket
	for _, p := range packets {
		b := p.Binding
		if b.RuntimeID != state["runtime_id"] || b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			continue
		}
		current = append(current, p)
	}
	return current
}

func nativeS9PlanPackets(root string, state map[string]any) []workpacket.RepairPlanReportPacket {
	var current []workpacket.RepairPlanReportPacket
	for _, p := range nativeS9RegisteredPlanPackets(root, state) {
		if p.BriefPath == "" {
			continue
		}
		brief := nativeS9BriefPath(p)
		if _, err := os.Stat(filepath.Join(root, brief)); err == nil {
			current = append(current, p)
		}
	}
	return current
}

func prepareNativeS9Brief(ctx context.Context, root string, p workpacket.RepairPlanReportPacket) (string, error) {
	return prepareNativeS9BriefMode(ctx, root, &p)
}

// restoreNativeS9Brief recreates missing guidance from the issued packet.
func restoreNativeS9Brief(ctx context.Context, root string, p workpacket.RepairPlanReportPacket) (string, error) {
	if p.BriefPath == "" {
		return "", fmt.Errorf("original packet has no issued brief reference")
	}
	return prepareNativeS9BriefMode(ctx, root, &p)
}

func prepareNativeS9BriefForIssue(ctx context.Context, root string, p *workpacket.RepairPlanReportPacket) error {
	_, err := prepareNativeS9BriefMode(ctx, root, p)
	return err
}

func prepareNativeS9BriefMode(ctx context.Context, root string, packet *workpacket.RepairPlanReportPacket) (string, error) {
	p := *packet
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var assignment repair.RepairAssignment
	if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
		return "", fmt.Errorf("read original native S9 RepairAssignment: %w", err)
	}
	assertionIDs, err := json.Marshal(assignment.AssertionIDs)
	if err != nil {
		return "", err
	}
	brief := nativeS9BriefPath(p)
	question := filepath.Join(root, p.ViewPath)
	if p.ViewPath == "" {
		question = "embedded below in this brief (the optional separate view was unavailable)"
	}
	planPath, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf(`# S9 repair — %s

Read the original professional question %s and its actual source materials.
The question and this brief are read-only materials in the Main control root, not
paths within your private worktree. Use their supplied absolute paths for reading.
Follow the actual user constraints Main passed in this task, including any explicit
restriction against product development or commits; the general repair instructions
below apply only within that task and the original approved scope.
Use the question for professional duties, assigned units, assertions and source materials.
The private outputs below use top-level professional fields, without an answer envelope;
this brief defines the current output locations and format.
Use Claude Code native worktree isolation from the committed Main checkout.
Respect the actual dependency order. Before implementation,
perform actual prechecks and personally save the professional PlanReport at %s relative
to your actual native worktree. Write only plan, red_checks and proposed_paths in this
private JSON; the original Main-root question is read-only input, not a Worker write
location. red_checks records actual attempted prechecks with honest pass, fail or
blocked results; passing checks and unavailable reproductions do not require invented
RED. The selected prechecks may cover a subset; they do not define or reduce the
Result assertion set below, including when Main selects a RED experiment subset.
This workflow requires isolated Workers to keep edits in their actual
native worktree; Main owns changes to the shared Main checkout.
Continue your repair and real self-checks without waiting for a save receipt or activation.
Save your professional Result at %s relative to your actual native worktree:

`+"```json"+`
{"verdict":"","before_fix_results":[],"unit_results":[],"detection_gap_artifacts":[],"evidence_refs":[],"scope_deviations":[],"residual_risks":[],"migration_ref":"","rollback_ref":""}
`+"```"+`

Use verdict completed, blocked, stale or contract_conflict truthfully. before_fix_results
items have assertion_id, observed_red, evidence_refs. Use the applicable assertion IDs
from this original RepairAssignment; report unfinished observations honestly:
%s
These IDs come directly from the issued packet's original RepairAssignment; consult
assignment %s in the original RepairPlan %s for that responsibility and the original
Contract in the question for assertion text. Include assertions already satisfied
before repair, as well as failed, unreproduced or blocked observations. observed_red
is a historical field name: write the truthful pre-repair observation and evidence,
without manufacturing a failure or omitting an already satisfied assertion.
unit_results items have unit_id,
status, evidence_refs, implementation_summary, restored_invariant_refs. Use the exact
assigned units/assertions from the question and actual evidence, never invented PASS.
unit_results.status is pass, fail or blocked; implementation_summary and observed_red
are strings. Every evidence_refs and restored_invariant_refs is an array of strings.
detection_gap_artifacts, top-level evidence_refs, scope_deviations and residual_risks
are arrays of strings, never artifact objects. migration_ref and rollback_ref are strings;
use "" if not applicable. Use [] for an empty array. Do not add
machine metadata or extra nested fields. Record commands/observations in the professional
text fields and reference actual files as strings.
Local files in Result evidence_refs (top-level and per-observation) are relative
to your registered Worker root (or absolute within it); prefer path:logs/check.txt,
without authoring a hash.
Use path:check.log or ./check.log for a local bare filename; bare tokens without
path: or a slash remain opaque IDs. Referenced local check outputs such as
logs/check.txt need no extra evidence commit; URI references remain unchanged.
Ordinary PreToolUse events and successful saves observed at PostToolUse automatically
attempt reception of these exact professional originals; SubagentStop is a fallback.
A complete Plan can be received while implementation continues and Result is unfinished.
Keep working without a submit command, done marker, or extra path-return action.
Saving a private Plan/Result does not itself establish reception.
Only the original consumer receipt confirms acceptance; Main can query
loop-harness runtime repair status --root <Main-root> for missing_reports and
missing_results. Keep the original responsibility and available artifacts while
reception is pending. Actual unavailable material is reported to Main.
The system selects the original responsibility from this exact brief %s in the native
task and the actual Worker source. Use the supplied Main-root absolute path for reading;
a private-worktree relative path does not locate these Main materials.
Original actor, source and professional reception checks still apply. Reception feedback preserves the original responsibility,
exact artifact paths and consumer diagnostics; only its accepted receipt proves reception.
Main owns integration and necessary checks. No workspace
prepare/launch, session reservation, Team, generic readback, begin or activation.
`, p.Binding.RepairAssignmentID, question, planPath, result, assertionIDs, assignment.AssignmentID, filepath.Join(root, p.Binding.PlanRef), filepath.Join(root, brief))
	if p.ViewPath == "" {
		original, err := json.MarshalIndent(p.Draft, "", "  ")
		if err != nil {
			return "", err
		}
		text += "\nOriginal professional question:\n```json\n" + string(original) + "\n```\n"
	}
	if p.BriefPath != "" {
		// The packet's original Writer has already selected this location.
		// Recreate only if it is missing; an existing registered file is reused.
		if _, err := workpacket.PublishNewBounded(root, brief, []byte(text), 1<<20); err != nil {
			return "", err
		}
	} else {
		defaultBrief := brief
		brief, err = workpacket.PublishS9QuestionnairePrepared(ctx, root, defaultBrief, func(candidate string) []byte {
			return []byte(strings.ReplaceAll(text, filepath.Join(root, defaultBrief), filepath.Join(root, candidate)))
		}, 1<<20)
		if err != nil {
			return "", err
		}
		packet.BriefPath = brief
	}
	return fmt.Sprintf("Main: use Claude Code native Agent with worktree isolation and carry this exact S9 brief: `%s`. Pass the actual user task constraints to the Worker, including any no-development or no-commit restriction. The brief and read-only question are Main control-root materials; preserve these absolute readable paths rather than resolving them in the private worktree. Read-only professional question: %s; private PlanReport output relative to the actual native worktree: %s. Builder owns its professional plan, repair and Result; natural PreToolUse and successful save PostToolUse events receive exact artifacts automatically, with SubagentStop fallback. No submit, done marker or additional path-return action is required. Never ask the isolated Worker to write Main. No workspace prepare/launch or platform ID is required. ", filepath.Join(root, brief), question, planPath), nil
}

// Used only to route an actual Worker event; the consumer still establishes
// the actor, source and issued responsibility from the original references.
func nativeS9WorkerAvailable(root string, state map[string]any) bool {
	return len(nativeS9RegisteredPlanPackets(root, state)) > 0 || nativeS9Available(root, state)
}

func nativeS9Available(root string, state map[string]any) bool {
	if len(nativeS9PlanPackets(root, state)) > 0 {
		return true
	}
	pointer := repairPointerCLI(state)
	status := stringValue(pointer["status"])
	return stringValue(mapFieldCLI(state, "lifecycle")["state"]) == "bug_resolution" && s9ProfessionalPhase(stringValue(mapFieldCLI(state, "lifecycle")["phase"])) && (status == "targeted_reverification" || status == "blocked" && pointer["failure_route"] == "blocked" && len(repairArtifactRefs(pointer, "targeted_reverification_artifacts", "")) > 0)
}

func nativeS9Hook(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	snap, err := nativeRoutingSnapshot(root, in)
	workerObservation := in.AgentID != "" && in.AgentID != "Main" && (in.Event == "PreToolUse" || in.Event == "PostToolUse" || in.Event == "SubagentStop")
	if err != nil || !runtime.PacketEnabled(snap.State) {
		return false, 0
	}
	available := nativeS9RoutingAvailable(contextForHook(in), root, snap.State, false)
	candidateOnly := !available && workerObservation && nativeS9RoutingAvailable(contextForHook(in), root, snap.State, true)
	if !available && !candidateOnly {
		return false, 0
	}
	natural := in.Event == "PreToolUse" || in.Event == "PostToolUse"
	// Ordinary child events identify the actor but point transcript_path at the
	// parent session. Resolve only that actor's actual native child transcript.
	if in.AgentID == "" || in.AgentID == "Main" {
		// An old S9 route can outlive its handoff. Only the still-current
		// repair stage may prepare its independent-verification successor;
		// late Worker deliveries are selected separately by their own task.
		if (natural || in.Event == "Stop") && lifecycleState(snap.State) == "bug_resolution" && stringValue(repairPointerCLI(snap.State)["status"]) != "closed" {
			continueRegisteredS9Verification(root, snap.State, diagnostic)
		}
		if in.Event == "PostToolUse" && (in.ToolName == "Agent" || in.ToolName == "Task") {
			return true, 0
		}
		return false, 0
	}
	if natural {
		child, ok := nativeChildTranscript(in)
		if !ok {
			// No task was selected. An existing S9 route cannot consume an
			// unrelated Worker's event or prevent its own receiver from seeing it.
			return false, 0
		}
		in.AgentTranscriptPath = child
		feedback := &nativeS9WorkerFeedback{}
		in.Context = context.WithValue(contextForHook(in), nativeS9WorkerFeedbackKey{}, feedback)
		defer func() {
			if len(feedback.messages) > 0 {
				if err := json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": strings.Join(feedback.messages, "\n")}}); err == nil && feedback.commitGuidance != nil {
					feedback.commitGuidance()
				}
			}
		}()
	}
	if natural && (in.Error != "" || in.ToolResponse["is_error"] == true) {
		return false, 0
	}
	if in.Event != "SubagentStart" && in.Event != "SubagentStop" && !natural {
		return false, 0
	}
	if engine, err := policy.Load(filepath.Join(root, "docs/control/hook-policy.json")); err == nil {
		observed := policy.Decision{Decision: "audit", RuleID: "native_s9_lifecycle_observed", Reason: "native S9 lifecycle observed", Retry: "not_applicable"}
		auditStarted := time.Now()
		defer func() {
			observed.ElapsedMS = time.Since(auditStarted).Milliseconds()
			if err := appendHookAudit(contextForHook(in), root, engine.Envelope(in, observed, time.Now())); err != nil {
				fmt.Fprintln(diagnostic, err)
			}
		}()
	}
	if in.Event == "SubagentStart" {
		// Start has no complete task prompt. Do not infer a repair or review
		// assignment from a historical route or the platform's role string.
		context := "Follow the actual task and original materials supplied by Main. Save your professional work at its assigned location; ordinary tools remain available. The platform manages your execution and return."
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": context}})
		return true, 0
	}
	routes, routeErr := nativeS9Routes(contextForHook(in), snap.State)
	var refs, targetedRefs []string
	targetedOwners := map[string]string{}
	for _, route := range routes {
		if route.BriefPath == "" {
			continue
		}
		refs = append(refs, filepath.Join(root, nativeS9RouteBrief(route)))
	}
	status := stringValue(repairPointerCLI(snap.State)["status"])
	if status == "targeted_reverification" || status == "blocked" || status == "ready_for_full_review" || status == "closed" {
		if selections, err := repairTargetedSelections(root, snap.State); err == nil {
			for _, selection := range selections {
				if ref, err := repairTargetedQuestionnairePath(root, snap.State, selection.performingAssignmentID); err == nil {
					targetedRefs = append(targetedRefs, ref)
					targetedOwners[ref] = selection.verifierOwner
				}
			}
		}
	}
	briefRefs := append([]string(nil), refs...)
	refs = append(refs, targetedRefs...)
	var sources nativeTaskSources
	if prior, ok := contextForHook(in).Value(nativeRecoveredS9SourcesKey{}).(nativeRecoveredS9Sources); ok {
		sources = prior.sources
	} else {
		sources = nativeS9TaskSources(in, root, refs, briefRefs...)
	}
	text := sources.s9ResponsibilityText(root, refs, briefRefs...)
	var selectedRoute *runtime.RepairPlanReportRoute
	for _, route := range routes {
		brief := nativeS9RouteBrief(route)
		if brief == "" {
			continue
		}
		if nativeS6ExactReference(text, filepath.Join(root, brief)) {
			if selectedRoute != nil {
				fmt.Fprintln(diagnostic, "S9 return has multiple exact briefs; Main must resolve it")
				return true, 0
			}
			route := route
			selectedRoute = &route
		}
	}
	// A verifier may quote an earlier Builder's brief as background material.
	// Multiple exact responsibilities are ambiguous even across S9 branches;
	// never consume the Builder merely because it was checked first.
	if selectedRoute != nil {
		for _, ref := range targetedRefs {
			if nativeS6ExactReference(text, ref) {
				fmt.Fprintln(diagnostic, "S9 return has both Builder and Targeted responsibility references; Main must resolve the handoff")
				return true, 0
			}
		}
	}

	var selected *workpacket.RepairPlanReportPacket
	if selectedRoute != nil {
		packet, readErr := nativeS9RoutePacket(contextForHook(in), root, snap.State, *selectedRoute)
		if readErr != nil {
			message := fmt.Sprintf("S9 original responsibility at control root %s, brief %s remains unconfirmed: %v. Preserve original artifacts; Main and framework maintainers own this system source fault.", root, filepath.Join(root, nativeS9RouteBrief(*selectedRoute)), readErr)
			fmt.Fprintln(diagnostic, message)
			if natural {
				nativeS9WorkerMessage(contextForHook(in), message)
			}
			return true, 0
		}
		selected = &packet
	}
	// Independent Targeted keeps its own source contract even if the Builder
	// registry is unavailable. Never use a failed Builder read as its gate.
	if routeErr != nil {
		targeted := false
		for _, ref := range targetedRefs {
			if nativeS6ExactReference(text, ref) {
				targeted = true
			}
		}
		if !targeted {
			message := fmt.Sprintf("S9 responsibility is unknown at control root %s: %v. Main and framework maintainers must preserve and repair the original source; reception is unconfirmed.", root, routeErr)
			fmt.Fprintln(diagnostic, message)
			// The damaged S9 index cannot identify this event as S9 work.
			// Preserve its diagnostic without swallowing an unrelated receiver.
			return false, 0
		}
	}

	key, owner, paths := "", "", []string{}
	var capture *nativeS9AttemptCapture
	if selected != nil {
		brief := nativeS9BriefPath(*selected)
		key, owner = selected.ID, selected.Binding.OwnerAgentID
		paths = []string{filepath.Join(root, brief)}
		capture = newNativeS9AttemptCapture(contextForHook(in), root, snap, key, owner, "Builder")
		capture.SetResponsibilityRef(filepath.Join(root, brief))
		if natural {
			err = receiveNativeS9BuilderEvent(root, snap.State, in, *selected, sources.text(), diagnostic, capture)
		} else {
			err = receiveNativeS9Builder(root, snap.State, in, *selected, sources.text(), diagnostic, capture)
		}
	} else {
		for _, ref := range targetedRefs {
			if nativeS6ExactReference(text, ref) {
				key, owner, paths = ref, targetedOwners[ref], []string{ref}
				break
			}
		}
		if key != "" {
			capture = newNativeS9AttemptCapture(contextForHook(in), root, snap, key, owner, "Targeted")
			capture.SetResponsibilityRef(key)
		}
		if key == "" {
			return false, 0
		}

		if err == nil {
			err = receiveNativeS9Targeted(contextForHook(in), root, snap, text, diagnostic, capture)
		}
	}
	if err == nativeS9ProgressPending {
		return true, 0
	}
	if capture != nil {
		if publishErr := capture.Publish(contextForHook(in), err); publishErr != nil {
			fmt.Fprintln(diagnostic, "S9 reception diagnostic could not be delivered:", publishErr)
		}
	}
	if err != nil {
		// The original read already captured these coordinates. Preserve them
		// in the Stop channel too, without resolving the source a second time.
		if capture != nil {
			known := map[string]bool{}
			for _, path := range paths {
				known[path] = true
			}
			var captured []string
			for path := range capture.value.Files {
				captured = append(captured, path)
			}
			captured = append(captured, capture.value.Attempt.InputRef, capture.value.Attempt.TypedInputRef)
			sort.Strings(captured)
			for _, path := range captured {
				if path != "" && !known[path] {
					paths = append(paths, path)
					known[path] = true
				}
			}
		}
		if natural {
			var completion *repairExitFailure
			if !errors.As(err, &completion) {
				message := nativeS9UnconfirmedReceptionContext(root, owner, strings.Join(paths, "; "), err)
				if message == "" {
					message = fmt.Sprintf("S9 reception for %s is incomplete. Original artifacts: %s. Original consumer diagnostic: %s. Preserve the originals; correct any identified professional fields within your assigned responsibility. System continuation remains with Main.", owner, strings.Join(paths, "; "), nativeFailureDetail(err))
				}
				nativeS9WorkerMessage(contextForHook(in), message)
			}
		}
		return true, nativeS9HandoffFailure(owner, strings.Join(paths, "; "), err, diagnostic, root)
	}
	return true, 0
}

type nativeS9PlanStopKey struct{}

type nativeS9WorkerFeedbackKey struct{}
type nativeS9WorkerFeedback struct {
	messages       []string
	commitGuidance func()
}

func nativeS9WorkerMessage(ctx context.Context, message string) {
	if feedback, ok := ctx.Value(nativeS9WorkerFeedbackKey{}).(*nativeS9WorkerFeedback); ok {
		feedback.messages = append(feedback.messages, message)
	}
}

var nativeS9ProgressPending = fmt.Errorf("native S9 professional work remains in progress")

// Natural tool events observe the actual checkout before the next tool runs.
// They never use a quoted Result in another worktree as source authority.
func receiveNativeS9BuilderEvent(root string, state map[string]any, in policy.Input, p workpacket.RepairPlanReportPacket, text string, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	return receiveNativeS9OriginalBuilder(contextForHook(in), root, state, p, in.CWD, diagnostic, capture)
}

// A flat private author file carries only the author's own professional fields.
// The canonical consumer stays lenient about unrelated extensions, so this
// entry point keeps the strictness the original flat entry owned: an unknown
// key is the author's own file error, while a type mismatch belongs to the
// original consumer's precise field pointer and is handed back unchanged.
func nativeS9FlatUnknownAuthorField(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(value)
	if err == nil {
		return nil
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		return nil
	}
	return err
}

func receiveNativeS9ReadyPlan(ctx context.Context, root, source string, p workpacket.RepairPlanReportPacket, service *submission.Service, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
	if err != nil {
		return err
	}
	if !receipt.Accepted {
		raw, readErr := workpacket.ReadFile(source, plan, 20<<20)
		if capture != nil {
			if readErr != nil {
				capture.ObserveInputError(filepath.Join(source, plan), readErr)
			} else {
				capture.ObserveInput(filepath.Join(source, plan), raw)
			}
		}
		if os.IsNotExist(readErr) || readErr == nil && !json.Valid(raw) {
			return nativeS9ProgressPending
		}
		if readErr != nil {
			return readErr
		}
		if capture != nil {
			capture.EnterConsumer("plan_report_consumer")
		}
		var planAnswer workpacket.RepairPlanReportProfessionalAnswer
		if err := nativeS9FlatUnknownAuthorField(raw, &planAnswer); err != nil {
			return nativeS9ArtifactAuthorError("PlanReport", filepath.Join(source, plan), err)
		}
		receipt, err = service.SaveRepairPlanReportBytes(ctx, p.Binding.OwnerAgentID, p.ID, source, raw)
		if err != nil {
			return nativeS9FlatArtifactError("PlanReport", filepath.Join(source, plan), err)
		}
		if !receipt.Accepted {
			return fmt.Errorf("original PlanReport was not accepted: %s", receipt.Error)
		}
		if capture != nil {
			capture.ConsumerSucceeded()
			capture.SetStage("plan_received")
		}
		fmt.Fprintf(diagnostic, "Original S9 PlanReport received for %s; continue implementation and self-checks without waiting for Result or a receipt.\n", p.Binding.OwnerAgentID)
		nativeS9WorkerMessage(ctx, fmt.Sprintf("S9 PlanReport for %s was accepted by the original consumer from %s. Continue your assigned repair and self-checks; save Result at %s when ready.", p.Binding.OwnerAgentID, filepath.Join(source, plan), filepath.Join(source, result)))
	}
	return nil
}

func nativeS9TargetedStartContext(root string, state map[string]any, in policy.Input) string {
	status := stringValue(repairPointerCLI(state)["status"])
	if status != "targeted_reverification" && status != "blocked" && status != "ready_for_full_review" && status != "closed" {
		return ""
	}
	selections, err := repairTargetedSelections(root, state)
	if err != nil {
		return ""
	}
	var paths []string
	for _, selection := range selections {
		if path, err := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID); err == nil {
			paths = append(paths, path)
		}
	}
	text := nativeS9TaskSources(in, root, paths).s9ResponsibilityText(root, paths)
	for _, path := range paths {
		if nativeS6ExactReference(text, path) {
			return repairStopConditionsContext(root, state)
		}
	}
	return ""
}

// Resolve only the actual returned Result reference or platform CWD.
func nativeS9BuilderSource(root string, in policy.Input, text, result string) (string, error) {
	selected := ""
	for _, token := range strings.Fields(text) {
		token = strings.Trim(token, "`\"'(),;")
		suffix := string(filepath.Separator) + filepath.FromSlash(result)
		if filepath.IsAbs(token) && strings.HasSuffix(token, suffix) {
			candidate := strings.TrimSuffix(token, suffix)
			if selected != "" && !samePath(selected, candidate) {
				return "", fmt.Errorf("multiple actual Result locations; Main must resolve the original responsibility")
			}
			selected = candidate
		}
	}
	if selected != "" {
		return workspace.CanonicalProspective(selected)
	}
	if in.CWD == "" {
		return "", fmt.Errorf("S9 actual authoring location is missing; Main must locate the original responsibility")
	}
	return workspace.CanonicalProspective(in.CWD)
}

func receiveNativeS9Builder(root string, state map[string]any, in policy.Input, p workpacket.RepairPlanReportPacket, text string, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	_, result, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		return pathErr
	}
	// A cleaned accepted source no longer appears in git worktree list. Its
	// exact returned reference may still select the original receipt.
	if registry, err := workspace.Decode(state); err == nil && registry != nil {
		if execution, ok := registry.Executions[p.Binding.ManifestAssignmentID]; ok && execution.Purpose == workspace.NativeDeliveryPurpose && execution.Status == "complete" && nativeS6ExactReference(text, filepath.Join(execution.Path, result)) {
			return receiveNativeS9BuilderFrom(contextForHook(in), root, state, p, execution.Path, false, diagnostic, nativeS9Capture(captures))
		}
	}
	source, err := nativeS9BuilderSource(root, in, text, result)
	if err != nil {
		return err
	}
	ctx := contextForHook(in)
	if in.Event == "SubagentStop" {
		ctx = context.WithValue(ctx, nativeS9PlanStopKey{}, true)
	}
	return receiveNativeS9BuilderFrom(ctx, root, state, p, source, false, diagnostic, nativeS9Capture(captures))
}

// Stop and Main recovery use the same original-answer consumer.
func receiveNativeS9BuilderFrom(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source string, recovery bool, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	return receiveNativeS9OriginalBuilder(ctx, root, state, p, source, diagnostic, capture)
}

func receiveNativeS9Targeted(ctx context.Context, root string, snap runtime.Snapshot, text string, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	status := stringValue(repairPointerCLI(snap.State)["status"])
	if status != "targeted_reverification" && status != "blocked" && status != "ready_for_full_review" && status != "closed" {
		return nil
	}
	selections, err := repairTargetedSelections(root, snap.State)
	if err != nil {
		return err
	}
	var chosen *repairTargetedSelection
	var path string
	for _, selection := range selections {
		ref, err := repairTargetedQuestionnairePath(root, snap.State, selection.performingAssignmentID)
		if err != nil {
			return err
		}
		if nativeS6ExactReference(text, ref) {
			if chosen != nil {
				return fmt.Errorf("multiple Targeted responsibility references; Main must resolve it")
			}
			selection := selection
			chosen, path = &selection, ref
		}
	}
	if chosen == nil {
		return nil
	}
	// An accepted responsibility projects its stored result; its private
	// answer may have changed or disappeared after reception.
	receipt := lookupTargetedForSelection(root, repairPointerCLI(snap.State), *chosen, "pass")
	if receipt.found {
		if status == "ready_for_full_review" {
			_, err := completeRepairExit(root)
			return err
		}
		return nil
	}
	if receipt.unknown || receipt.previous.Path != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		if available, _ := s9OriginalInputAvailable(ctx, path); !available {
			nativeS9WorkerMessage(ctx, targetedHistoricalUnknownMessage(receipt))
			return nil
		}
	}
	answer, err := readRepairTargetedAnswerContext(ctx, path, nativeS9Capture(captures))
	if err != nil {
		if receipt.unknown || receipt.previous.Path != "" {
			nativeS9WorkerMessage(ctx, targetedHistoricalUnknownMessage(receipt))
			return nil
		}
		return err
	}
	if receipt.previousReport != nil && sameTargetedProfessionalAnswer(answer, receipt.previousReport) {
		nativeS9WorkerMessage(ctx, targetedHistoricalUnknownMessage(receipt))
		return nil
	}
	if receipt.unknown {
		ctx = context.WithValue(ctx, targetedAssociationUnknownKey{}, true)
	}
	if strings.TrimSpace(answer.Result) == "" {
		return nativeS9ProgressPending
	}
	var feedback bytes.Buffer
	if capture := nativeS9Capture(captures); capture != nil {
		capture.EnterConsumer("targeted_consumer")
	}
	_, code, exitFailure := saveRepairTargetedAnswerContext(ctx, root, snap, path, *chosen, answer, &feedback, nativeS9Capture(captures))
	var message struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	_ = json.Unmarshal(feedback.Bytes(), &message)
	fmt.Fprintln(diagnostic, message.Output.Context)
	if code != 0 {
		return fmt.Errorf("original Targeted consumer: %s", feedback.String())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(ctx).Snapshot()
	if err != nil {
		return err
	}
	pointer := repairPointerCLI(latest.State)
	priorRefs := map[string]bool{}
	for _, ref := range repairArtifactRefs(repairPointerCLI(snap.State), "targeted_reverification_artifacts", "") {
		priorRefs[ref.Path] = true
	}
	facts, factErr := repair.LoadTargetedReceiptFacts(ctx, root, pointer, chosen.performingAssignmentID, false)
	if factErr != nil {
		return factErr
	}
	for i := len(facts.Rows) - 1; i >= 0; i-- {
		fact := facts.Rows[i]
		if !priorRefs[fact.Ref.Path] && fact.Result == answer.Result {
			if capture := nativeS9Capture(captures); capture != nil {
				capture.SetStage("targeted_received")
			}
			message := "Your original independent Targeted observations are received at " + fact.Ref.Path + ". Main continues the actual next responsibility."
			if receipt.unknown || answer.ImpactID == "" {
				message += " Earlier accepted records are preserved; their association with the current Impact is unknown."
			}
			nativeS9WorkerMessage(ctx, message)
			return exitFailure
		}
	}
	if accepted, ok := registeredTargetedForSelection(root, pointer, *chosen); ok {
		if capture := nativeS9Capture(captures); capture != nil {
			capture.SetStage("targeted_received")
		}
		nativeS9WorkerMessage(ctx, "Your original independent Targeted observations are received at "+accepted.Path+". Main continues any remaining Handoff and fresh S7 preparation.")
		return exitFailure
	}
	if stringValue(pointer["status"]) == "blocked" {
		return exitFailure
	}
	return fmt.Errorf("Targeted answer was not recorded: %s", message.Output.Context)
}
