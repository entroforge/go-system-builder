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
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
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
	if len(args) > 0 && (args[0] == "deliver" || args[0] == "integrate") {
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
		fmt.Fprintln(stdout, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|observe|integration-status|check|commit|deliver|integrate|pending|answer|receipt|review-check> --root <main-root> [--assignment <id> --agent <id>]; prepare installs a building, authorized S9, or enabled S5 document-review Worker; check runs a declared check in an isolated copy; integration-status observes the original Main lock without writing; see docs/workspace-integration.md")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|observe|check|commit|deliver|integrate|pending> --root <main-root> [--assignment <id> --agent <id>]")
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
	if verb == "prepare" {
		for _, packet := range nativeS9PlanPackets(mainRoot, snap.State) {
			if packet.Binding.OwnerAgentID == *agent && (packet.Binding.ManifestAssignmentID == *id || packet.Binding.RepairAssignmentID == *id) {
				return fail(fmt.Errorf("native S9 responsibility uses Claude Code Agent/worktree; pass its exact brief, then receive the returned source at SubagentStop; workspace prepare is for frozen legacy executions"))
			}
		}
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
	var s9ProfileReportRef repair.ArtifactRef
	var s9ProfileCommands []string
	s9ProfileLocation := ""
	s9GeneratedProfiles := false
	persist := func(event string) error {
		now := time.Now().UTC()
		key := fmt.Sprintf("workspace-%s-r%d", event, snap.Revision+1)
		next, e := store.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), BaselineGeneration: workspace.Generation(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: now, Message: event, Apply: func(state map[string]any) error {
			if event == "preparing" || event == "ready" {
				if lifecycleState, _ := state["lifecycle"].(map[string]any); lifecycleState["state"] == "bug_resolution" && s9GeneratedProfiles {
					authority, err := repair.ResolveWorkspaceAuthority(mainRoot, state, *id, *agent)
					if err != nil {
						return fmt.Errorf("revalidate S9 accepted PlanReport before freezing check profiles: %w", err)
					}
					if authority.ReportRef != s9ProfileReportRef {
						return fmt.Errorf("S9 accepted PlanReport changed before profile freeze")
					}
					execution, exists := binding.Executions[*id]
					if !exists || execution.CheckLocation != s9ProfileLocation {
						return fmt.Errorf("S9 original check location changed before profile freeze")
					}
					profiles, err := runtime.RepairResultCheckProfiles(authority.ReportRef.SHA256, s9ProfileCommands, s9ProfileLocation)
					if err != nil || !reflect.DeepEqual(profiles, execution.CheckProfiles) {
						return fmt.Errorf("S9 deterministic profile projection changed before freeze: %v", err)
					}
				}
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
	if lifecycle["state"] != "building" && lifecycle["state"] != "bug_resolution" && !documentReview {
		return fail(fmt.Errorf("workspace prepare requires building, an executing S9 assignment, or an enabled current S5 review"))
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
	if lifecycle["state"] == "bug_resolution" {
		authority, err := repair.ResolveWorkspaceAuthority(mainRoot, snap.State, *id, *agent)
		if err != nil {
			return fail(err)
		}
		assignment.WritePaths = authority.Assignment.Scope
		if len(assignment.RequiredChecks) == 0 {
			fmt.Fprintln(stderr, "S9 Main checks remain pending: Main must select and execute necessary checks during integration; preparing this Worker does not mark checks passed")
		}
		inputs = append(inputs, authority.Inputs...)
		s9ProfileReportRef = authority.ReportRef
		s9ProfileCommands = append([]string(nil), assignment.RequiredChecks...)
	}

	if *contextProfile == "backend-core" && (!workstate.SupportsAssignments(snap.State["schema_version"]) || lifecycle["state"] != "building" || assignment.RoleFamily != "backend-builder") {
		return fail(fmt.Errorf("backend-core requires a new original 1.2 S6 backend Builder"))
	}
	_, priorExecution := binding.Executions[*id]
	if lifecycle["state"] == "bug_resolution" && runtime.PacketEnabled(snap.State) && !priorExecution && len(assignment.RequiredChecks) > 0 && len(assignment.CheckProfiles) == 0 {
		if *checkLocation != "main" && *checkLocation != "worker" {
			return fail(fmt.Errorf("new packet-backed S9 Worker prepare must explicitly choose --check-location main or worker"))
		}
		profiles, profileErr := runtime.RepairResultCheckProfiles(s9ProfileReportRef.SHA256, s9ProfileCommands, *checkLocation)
		if profileErr != nil {
			return fail(profileErr)
		}
		assignment.CheckProfiles = profiles
		s9ProfileLocation = *checkLocation
		s9GeneratedProfiles = true
	}

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
	var declaredReadDigests map[string]string
	if lifecycle["state"] == "building" && assignment.AssignmentRecord != nil {
		dispatch, dispatchErr := workspaceDispatch(snap.State, *assignment)
		if dispatchErr != nil {
			return fail(dispatchErr)
		}
		canonicalDispatch = &dispatch
		if dispatch.AssignmentRecord != nil && len(dispatch.ReadPaths) > 0 {
			originalCommit := ""
			if priorExecution {
				originalCommit = binding.Executions[*id].BaseCommit
			}
			var declaredPaths []string
			declaredReadView, declaredPaths, declaredReadDigests, err = freezeAssignmentReadClosure(ctx, mainRoot, snap.State, dispatch.ReadPaths, originalCommit)
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
		if e.BaseCommit != declaredReadView.Commit {
			return fail(fmt.Errorf("prepared execution base differs from the pinned Assignment read sources"))
		}
		for path, expected := range declaredReadDigests {
			if e.Inputs[path] != expected {
				return fail(fmt.Errorf("prepared execution did not freeze the pinned Assignment read source: %s", path))
			}
		}
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
	// Real workspace readiness supplies the next professional question once.
	// Ordinary tool observation never enters this preparation path.
	resultViewPath, resultAnswerPath := "", ""
	publishS9Result := func(execution workspace.Execution) error {
		if lifecycle["state"] != "bug_resolution" || !runtime.PacketEnabled(snap.State) {
			return nil
		}
		packets, err := s9ListenerQuestionnairesContext(context.Background(), *root, snap.State, *agent, true)
		if err != nil {
			return err
		}
		supplied := false
		for _, packet := range packets {
			if packet.binding.ManifestAssignmentID != *id {
				continue
			}
			resultViewPath, resultAnswerPath = resolveRootPath(execution.Path, packet.view), resolveRootPath(execution.Path, packet.answer)
			fmt.Fprintf(stderr, "S9 RepairResult view: %s; answer: %s; fill and save this exact professional answer.\n", resultViewPath, resultAnswerPath)
			supplied = true
		}
		if supplied {
			return nil
		}
		service, err := submission.New(mainRoot)
		if err != nil {
			return err
		}
		issued, err := service.IssueRepairResults(ctx, *agent)
		if err != nil {
			return fmt.Errorf("Worker is ready; Main/system must finish RepairResult preparation: %w", err)
		}
		for _, packet := range issued {
			if packet.Binding.Sources.ManifestAssignmentID != *id {
				continue
			}
			resultViewPath, resultAnswerPath = resolveRootPath(execution.Path, packet.ViewPath), resolveRootPath(execution.Path, packet.AnswerPath)
			fmt.Fprintf(stderr, "S9 RepairResult view: %s; answer: %s; fill and save this exact professional answer.\n", resultViewPath, resultAnswerPath)
			supplied = true
		}
		if !supplied {
			return fmt.Errorf("Worker is ready but its RepairResult questionnaire is not prepared for Assignment %s; Main/system owns this preparation gap", *id)
		}
		return nil
	}
	writePrepared := func(execution workspace.Execution) error {
		if lifecycle["state"] == "bug_resolution" {
			contextRef := ""
			if workspace.HasDispatchContext(execution) {
				contextRef = resolveRootPath(execution.Path, workspace.DispatchContextPath)
			}
			return json.NewEncoder(stdout).Encode(map[string]any{
				"execution":              execution,
				"assignment_context_ref": contextRef,
				"frozen_input_root":      resolveRootPath(execution.Path, ".claude/inputs"),
				"view_path":              resultViewPath,
				"answer_path":            resultAnswerPath,
				"next_action":            "Implement or test the assigned repair in this actual work location and perform real self-checks. For actual source changes, use ordinary git add and git commit in this Worker; no-change tests require no new commit. Then fill and save the supplied RepairResult answer. No extra commit form or generic completion report is required. Main owns real integration and necessary checks.",
			})
		}
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
		if err = binding.ValidateBootstrap(e); err != nil {
			return fail(err)
		}
		if err = publishS9Result(e); err != nil {
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
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err = binding.Bootstrap(e, executable); err != nil {
		return fail(err)
	}
	e.BootstrapSHA256, err = workspace.BootstrapDigest(e)
	if err != nil {
		return fail(err)
	}
	e.Status = "ready"
	binding.Executions[*id] = e
	if err = persist("ready"); err != nil {
		return fail(err)
	}
	if err = publishS9Result(e); err != nil {
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
