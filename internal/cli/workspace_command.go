package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Workspace creation is an explicit, journaled operation. The intent is saved
// before Git side effects, so interrupted preparation can resume by identity.
func runRuntimeWorkspace(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "integration-status" {
		return runIntegrationStatus(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "answer" || args[0] == "receipt" || args[0] == "review-check") {
		return runWorkspaceAnswer(args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "observe" {
		return runWorkspaceObserve(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "rework" {
		return runWorkspaceRework(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "rebind" {
		return runWorkspaceRebind(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "adopt" || args[0] == "replace") {
		return runWorkspaceRecovery(args, stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "begin" || args[0] == "report") {
		return runWorkspaceLifecycle(args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "launch" {
		return runWorkspaceLaunch(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "integrate" {
		return runWorkspaceDelivery(args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "commit" {
		return runWorkspaceCommit(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "pending" {
		return runWorkspacePending(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "check" {
		return runWorkspaceCheck(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|observe|integration-status|check|commit|integrate|pending|answer|receipt|review-check> --root <main-root> [--assignment <id> --agent <id>]; prepare installs a building or enabled S5 document-review Worker; check runs a declared check in an isolated copy; integration-status observes the original Main lock without writing; see docs/workspace-integration.md")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|observe|check|commit|integrate|pending> --root <main-root> [--assignment <id> --agent <id>]")
		return 2
	}
	verb := args[0]
	if verb != "bind" && verb != "status" && verb != "prepare" {
		fmt.Fprintln(stderr, "unknown workspace action")
		return 2
	}
	flags := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "main control workspace root")
	id := flags.String("assignment", "", "registered assignment identity")
	agent := flags.String("agent", "", "registered owner identity")
	contextProfile := flags.String("context-profile", "", "prepare: shadow (default) or backend-core for a new 1.2 S6 backend Builder")
	checkLocation := flags.String("check-location", "", "prepare: worker (isolated) or main (integration checks)")
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *contextProfile != "" && (*contextProfile != "shadow" && *contextProfile != "backend-core" || verb != "prepare") {
		return fail(fmt.Errorf("context-profile is a prepare-only shadow/backend-core choice"))
	}
	mainRoot, err := workspace.Canonical(*root)
	if err != nil {
		return fail(err)
	}
	if err := workspace.RequireMain(mainRoot); err != nil {
		return fail(err)
	}
	ctx, release, err := integration.LockWorkspace(ctx, mainRoot)
	if err != nil {
		return fail(err)
	}
	defer release()
	store := runtime.NewWriter(filepath.Join(mainRoot, ".claude/loop-state.json"), filepath.Join(mainRoot, ".claude/loop-events.jsonl"), mainRoot, semantic.RuntimeCandidateValidator{})
	snap, err := store.Snapshot()
	if err != nil {
		return fail(err)
	}
	binding, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if verb == "status" {
		if binding != nil {
			if err = binding.Validate(ctx, mainRoot); err != nil {
				return fail(err)
			}
		}
		json.NewEncoder(stdout).Encode(binding)
		return 0
	}
	persist := func(event string) error {
		now := time.Now().UTC()
		key := fmt.Sprintf("workspace-%s-r%d", event, snap.Revision+1)
		next, e := store.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), BaselineGeneration: workspace.Generation(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: now, Message: event, Apply: func(state map[string]any) error {
			if event == "preparing" || event == "ready" {
				for _, execution := range binding.Executions {
					if execution.AssignmentID != *id {
						continue
					}
					if !workspace.HasDispatchContext(execution) {
						continue
					}
					captured, err := binding.ReadDispatchContext(execution)
					if err != nil {
						return err
					}
					if err := verifyConventionView(mainRoot, state, captured.Conventions, captured.FrozenInputs); err != nil {
						return err
					}
				}
			}
			state["workspace"] = workspace.Encode(binding)
			return nil
		}})
		if e == nil {
			snap = next
		}
		return e
	}
	if verb == "bind" {
		if binding == nil {
			binding, err = workspace.New(ctx, mainRoot)
			if err != nil {
				return fail(err)
			}
			if err = persist("bound"); err != nil {
				return fail(err)
			}
		} else if err = binding.Validate(ctx, mainRoot); err != nil {
			return fail(err)
		}
		json.NewEncoder(stdout).Encode(binding)
		return 0
	}
	if binding == nil {
		return fail(fmt.Errorf("bind the main workspace first; the target branch is never inferred from develop"))
	}
	lifecycle, _ := snap.State["lifecycle"].(map[string]any)
	documentReview := runtime.PacketEnabled(snap.State) && lifecycle["state"] == "document_verification"
	if lifecycle["state"] != "building" && !documentReview {
		return fail(fmt.Errorf("workspace prepare requires building or an enabled current S5 review"))
	}
	loaded, err := hookctx.LoadFull(mainRoot, *agent)
	if err != nil {
		return fail(err)
	}
	var assignment *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		a := &loaded.Assignments[i]
		if a.AssignmentID == *id && a.OwnerAgentID == *agent {
			assignment = a
			break
		}
	}
	if assignment == nil {
		return fail(fmt.Errorf("assignment and owner must match the current registered runtime"))
	}
	inputs := []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"}
	for _, ref := range []string{assignment.ManifestRef, assignment.AgentDefinitionRef} {
		path, _, _ := strings.Cut(ref, "#")
		if path != "" {
			inputs = append(inputs, path)
		}
	}

	if *contextProfile == "backend-core" && (!workstate.SupportsAssignments(snap.State["schema_version"]) || lifecycle["state"] != "building" || assignment.RoleFamily != "backend-builder") {
		return fail(fmt.Errorf("backend-core requires a new original 1.2 S6 backend Builder"))
	}
	_, priorExecution := binding.Executions[*id]

	newContext := workstate.SupportsAssignments(snap.State["schema_version"]) && !priorExecution
	if priorExecution && *contextProfile != "" {
		prior := binding.Executions[*id]
		if !workspace.HasDispatchContext(prior) {
			return fail(fmt.Errorf("legacy execution cannot be retrofitted with a context profile"))
		}
		context, err := binding.ReadDispatchContext(prior)
		if err != nil {
			return fail(err)
		}
		expected := "shadow"
		if context.Selection.Mode == "enabled" {
			expected = "backend-core"
		}
		if *contextProfile != expected {
			return fail(fmt.Errorf("existing execution context profile is immutable; keep its original release/profile"))
		}
	}
	if *contextProfile != "" && !priorExecution && !newContext {
		return fail(fmt.Errorf("context-profile requires original 1.2; legacy Runtime is not upgraded"))
	}
	var canonicalDispatch *workspace.Dispatch
	var declaredReadView *fileview.View
	if lifecycle["state"] == "building" && assignment.AssignmentRecord != nil {
		dispatch, dispatchErr := workspaceDispatch(snap.State, *assignment)
		if dispatchErr != nil {
			return fail(dispatchErr)
		}
		canonicalDispatch = &dispatch
		if dispatch.AssignmentRecord != nil && len(dispatch.ReadPaths) > 0 {
			var declaredPaths []string
			declaredReadView, declaredPaths, _, err = freezeAssignmentReadClosure(ctx, mainRoot, snap.State, dispatch.ReadPaths, "")
			if err != nil {
				return fail(err)
			}
			inputs = append(inputs, declaredPaths...)
		}
	}

	if newContext {
		contextInputs, err := workspaceContextInputs(mainRoot, snap.State, assignment.TaskID)
		if err != nil {
			return fail(err)
		}
		inputs = append(inputs, contextInputs...)
	}
	var e workspace.Execution
	var issuedReview *workpacket.Packet
	if documentReview {
		s, issueErr := submission.New(mainRoot)
		if issueErr != nil {
			return fail(issueErr)
		}
		packets, issueErr := s.IssueContext(ctx, *agent)
		if issueErr != nil {
			return fail(issueErr)
		}
		var p *workpacket.Packet
		for i := range packets {
			if packets[i].Binding.AssignmentID == *id {
				p = &packets[i]
			}
		}
		if p == nil {
			return fail(fmt.Errorf("S5 preparation requires this owner's exact issued Assignment"))
		}
		snap, err = store.Snapshot()
		if err != nil {
			return fail(err)
		}
		binding, err = workspace.Decode(snap.State)
		if err != nil || binding == nil {
			return fail(fmt.Errorf("workspace binding disappeared during S5 issuance: %v", err))
		}
		issuedReview = p
		e, err = binding.PlanDocumentReview(ctx, snap.State, *p, inputs)
	} else {
		e, err = binding.Plan(ctx, snap.State, *id, *agent, assignment.WritePaths, assignment.RequiredChecks, inputs)
	}
	if err != nil {
		return fail(err)
	}
	if declaredReadView != nil {
		if err := declaredReadView.Verify(); err != nil {
			return fail(err)
		}
	}
	if *checkLocation != "" && e.CheckLocation != "" && *checkLocation != e.CheckLocation {
		return fail(fmt.Errorf("requested check-location %q differs from the Assignment's frozen integration check mode %q", *checkLocation, e.CheckLocation))
	}
	if e.CheckLocation == "" && *checkLocation != "" {
		e.CheckLocation = *checkLocation
	}
	if !priorExecution && len(assignment.CheckProfiles) > 0 {
		if !workstate.SupportsAssignments(snap.State["schema_version"]) {
			return fail(fmt.Errorf("legacy execution cannot add CheckProfile authority"))
		}
		e, err = binding.FreezeCheckProfiles(e, assignment.CheckProfiles)
		if err != nil {
			return fail(err)
		}
	}
	if newContext {
		var d workspace.Dispatch
		if canonicalDispatch != nil {
			d = *canonicalDispatch
		} else {
			d, err = workspaceDispatch(snap.State, *assignment)
			if err != nil {
				return fail(err)
			}
		}
		conventions, projectionErr := contextConventions(mainRoot, snap.State, *assignment, issuedReview)
		if projectionErr != nil {
			return fail(projectionErr)
		}
		if err := mergeConventionInputs(mainRoot, conventions, e.Inputs); err != nil {
			return fail(err)
		}
		if err := verifyConventionView(mainRoot, snap.State, conventions, e.Inputs); err != nil {
			return fail(err)
		}
		e, err = binding.FreezeDispatchContextWithConventions(e, d, issuedReview, conventions, *contextProfile)
		if err != nil {
			return fail(err)
		}
	}
	if *checkLocation != "" && *checkLocation != "worker" && *checkLocation != "main" {
		return fail(fmt.Errorf("check-location must be worker or main"))
	}
	if prior, exists := binding.Executions[*id]; exists {
		if *checkLocation != "" && *checkLocation != prior.CheckLocation && !(prior.CheckLocation == "" && *checkLocation == "worker") {
			return fail(fmt.Errorf("existing execution check location is immutable; replace before delivery to choose another runner"))
		}
	} else {
		if e.CheckLocation == "" {
			e.CheckLocation = *checkLocation
		}
	}
	writePrepared := func(execution workspace.Execution) error {
		return writePreparedExecution(stdout, execution, binding, lifecycle["state"] == "building" && (assignment.RoleFamily == "backend-builder" || assignment.RoleFamily == "frontend-builder" || assignment.RoleFamily == "test-builder"))
	}
	source := mainRoot
	if e.Status == "ready" {
		source = e.Path
	}
	if err = binding.PreflightChecks(ctx, e, source); err != nil {
		return fail(err)
	}
	if e.Status == "ready" {
		if err = binding.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err = writePrepared(e); err != nil {
			return fail(err)
		}
		return 0
	}
	if e.Status != "preparing" {
		return fail(fmt.Errorf("execution %s cannot be prepared again", e.Status))
	}
	binding.Executions[*id] = e
	if err = persist("preparing"); err != nil {
		return fail(err)
	}
	if err = binding.Materialize(ctx, e); err != nil {
		return fail(err)
	}
	reserved, err := binding.ReserveWorkerSubmissionPaths(e)
	if err != nil {
		return fail(err)
	}
	if workspace.DispatchContextSource(reserved) != workspace.DispatchContextSource(e) {
		e = reserved
		binding.Executions[*id] = e
		if err = persist("preparing"); err != nil {
			return fail(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err = binding.Bootstrap(e, executable); err != nil {
		return fail(err)
	}
	e.Status = "ready"
	binding.Executions[*id] = e
	if err = persist("ready"); err != nil {
		return fail(err)
	}
	if err = writePrepared(e); err != nil {
		return fail(err)
	}
	return 0
}

func writePreparedExecution(out io.Writer, e workspace.Execution, binding *workspace.ExecutionRegistry, builderReceipt bool) error {
	contextMode, contextRef := "legacy", ""
	submissionPaths, err := binding.WorkerSubmissionPaths(e)
	if err != nil {
		return err
	}
	if workspace.HasDispatchContext(e) {
		context, err := binding.ReadDispatchContext(e)
		if err != nil {
			return err
		}
		contextMode, contextRef = context.Selection.Mode, workspace.DispatchContextPath
	}
	if e.Purpose == workspace.DocumentReviewPurpose {
		prepared := map[string]any{"context_mode": contextMode, "assignment_context_ref": contextRef, "execution": e, "work_packet_ref": workpacket.WorkerViewPath, "answer_path": submissionPaths.Answer, "submission_paths": map[string]string{"answer": submissionPaths.Answer}, "frozen_input_root": ".claude/inputs", "receipt_command": workspace.WorkerInvocation(e, "receipt"), "review_check_command": workspace.WorkerInvocation(e, "review-check"), "observation_commands": workspace.ObservationCommands(e), "done_when": "the domain receipt confirms work_complete=true; process exit alone is not completion"}
		if submissionPaths.Plan != "" {
			prepared["plan_draft_path"] = submissionPaths.Plan
			prepared["begin_command"] = workspace.WorkerInvocation(e, "begin")
			prepared["answer_command"] = workspace.WorkerInvocation(e, "answer")
			prepared["submission_paths"].(map[string]string)["plan"] = submissionPaths.Plan
		}
		return json.NewEncoder(out).Encode(prepared)
	}
	commands := []string{}
	ownedCommands := []string{}
	receiptCommand := ""
	if builderReceipt {
		receiptCommand = workspace.ReceiptInvocation(e)
	}
	for i, check := range e.Checks {
		if e.CheckLocation != "main" && !strings.HasPrefix(strings.TrimSpace(check), "locked:") {
			commands = append(commands, workspace.CheckInvocation(e, i))
		}
	}
	for i := range e.WorkerChecks {
		ownedCommands = append(ownedCommands, workspace.OwnedCheckInvocation(e, i))
	}
	return json.NewEncoder(out).Encode(struct {
		workspace.Execution
		ContextMode         string                          `json:"context_mode"`
		ContextRef          string                          `json:"assignment_context_ref,omitempty"`
		ObservationCommands map[string]string               `json:"observation_commands"`
		CheckCommands       []string                        `json:"check_commands"`
		OwnedCheckCommands  []string                        `json:"owned_check_commands"`
		CommitCommand       string                          `json:"commit_command"`
		BeginCommand        string                          `json:"begin_command"`
		ReportCommand       string                          `json:"report_command"`
		ReceiptCommand      string                          `json:"builder_receipt_command,omitempty"`
		DeliveryCommand     string                          `json:"s9_delivery_command"`
		SubmissionPaths     workspace.WorkerSubmissionPaths `json:"submission_paths"`
	}{e, contextMode, contextRef, workspace.ObservationCommands(e), commands, ownedCommands, workspace.WorkerInvocation(e, "commit"), workspace.WorkerInvocation(e, "begin"), workspace.WorkerInvocation(e, "report"), receiptCommand, workspace.WorkerInvocation(e, "deliver"), submissionPaths})
}
