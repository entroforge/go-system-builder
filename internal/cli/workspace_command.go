package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Workspace creation is an explicit, journaled operation. The intent is saved
// before Git side effects, so interrupted preparation can resume by identity.
func runRuntimeWorkspace(args []string, stdout, stderr io.Writer) int {
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
		fmt.Fprintln(stdout, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|check|commit|deliver|integrate|pending> --root <main-root> [--assignment <id> --agent <id>]; prepare installs a building or authorized S9 Worker; check runs a declared check in an isolated copy; see docs/workspace-integration.md")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime workspace <bind|status|prepare|adopt|replace|rebind|rework|launch|begin|report|check|commit|deliver|integrate|pending> --root <main-root> [--assignment <id> --agent <id>]")
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
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
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
		next, e := store.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: now, Message: event, Apply: func(state map[string]any) error { state["workspace"] = workspace.Encode(binding); return nil }})
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
	if lifecycle["state"] != "building" && lifecycle["state"] != "bug_resolution" {
		return fail(fmt.Errorf("workspace prepare requires building or an executing S9 assignment"))
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
	inputs := []string{"docs/loop-definition.json", "docs/hook-policy.json"}
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
		assignment.RequiredChecks = authority.Checks
		inputs = append(inputs, authority.Inputs...)
	}
	e, err := binding.Plan(ctx, snap.State, *id, *agent, assignment.WritePaths, assignment.RequiredChecks, inputs)
	if err != nil {
		return fail(err)
	}
	if e.Status == "ready" {
		if err = binding.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err = binding.ValidateBootstrap(e); err != nil {
			return fail(err)
		}
		if err = writePreparedExecution(stdout, e); err != nil {
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
	if err = writePreparedExecution(stdout, e); err != nil {
		return fail(err)
	}
	return 0
}

func writePreparedExecution(out io.Writer, e workspace.Execution) error {
	commands := []string{}
	for i, check := range e.Checks {
		if !strings.HasPrefix(strings.TrimSpace(check), "locked:") {
			commands = append(commands, workspace.CheckInvocation(e, i))
		}
	}
	return json.NewEncoder(out).Encode(struct {
		workspace.Execution
		CheckCommands   []string `json:"check_commands"`
		CommitCommand   string   `json:"commit_command"`
		BeginCommand    string   `json:"begin_command"`
		ReportCommand   string   `json:"report_command"`
		DeliveryCommand string   `json:"s9_delivery_command"`
	}{e, commands, workspace.WorkerInvocation(e, "commit"), workspace.WorkerInvocation(e, "begin"), workspace.WorkerInvocation(e, "report"), workspace.WorkerInvocation(e, "deliver")})
}
