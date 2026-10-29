package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeCheckAuthority struct {
	Assignment    hookctx.AssignmentContext
	Record        assignmentrecord.Record
	RecordSHA     string
	Binding       assignmentContextBinding
	Context       nativeAssignmentContext
	Source        assignment.NativeCheckSource
	ActivationRef string
	ActivationSHA string
}

// runNativeAssignmentCheck selects only a check index from the current Record.
// The command, profile, checkout and receipt come from frozen sources and the
// original isolated runner.
func runNativeAssignmentCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "runtime assignment-context check --root <Main> --agent-id <registered owner> --check-index <frozen WorkerCheck index>")
		return 0
	}
	fs := flag.NewFlagSet("runtime assignment-context check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "original Main authority root")
	owner := fs.String("agent-id", "", "actual registered Assignment owner")
	index := fs.Int("check-index", -1, "zero-based frozen WorkerCheck index")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if strings.TrimSpace(*owner) == "" || *index < 0 {
		return fail(fmt.Errorf("registered owner and nonnegative --check-index are required"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	canonicalRoot, err := workspace.Canonical(*root)
	if err != nil {
		return fail(err)
	}
	store := nativeCheckRuntimeWriter(canonicalRoot)
	snapshot, err := store.Snapshot()
	if err != nil {
		return fail(err)
	}
	if !workstate.SupportsAssignments(snapshot.State["schema_version"]) {
		return fail(fmt.Errorf("native WorkerCheck requires original Runtime 1.2; legacy Runtime is not upgraded"))
	}
	authority, err := resolveNativeCheckAuthority(canonicalRoot, snapshot, *owner, *index)
	if err != nil {
		return fail(err)
	}

	// The Main integration lock serializes receipt acceptance with inspection,
	// merge, checks and cleanup. Reacquiring the current snapshot after waiting
	// prevents a queued invocation from using stale authorization.
	ctx, release, err := integration.LockWorkspace(context.Background(), canonicalRoot)
	if err != nil {
		return fail(err)
	}
	defer release()
	snapshot, err = store.Snapshot()
	if err != nil {
		return fail(err)
	}
	authority, err = resolveNativeCheckAuthority(canonicalRoot, snapshot, *owner, *index)
	if err != nil {
		return fail(err)
	}
	check := authority.Record.ClosingContract.WorkerChecks[*index]
	source := workspace.NativeCheckSource{
		Checkout: authority.Source.Checkout, Branch: authority.Source.Branch,
		TargetBranch: authority.Source.TargetBranch, Head: authority.Source.Head,
		ManifestRef: authority.Source.ManifestRef, ManifestSHA256: authority.Source.ManifestSHA256,
	}
	request := workspace.NativeOwnedCheckRequest{
		RuntimeID: authority.Record.RuntimeID, BaselineGeneration: authority.Record.BaselineGeneration,
		AssignmentID: authority.Record.AssignmentID, OwnerAgentID: authority.Record.OwnerAgentID,
		Record: authority.Record.Reference(), RecordSHA256: authority.RecordSHA, RecordData: authority.Record,
		ContextGeneration: authority.Binding.Generation, ContextRef: authority.Binding.Context,
		ContextSHA256: authority.Binding.Context.SHA256,
		ActivationRef: authority.ActivationRef, ActivationSHA256: authority.ActivationSHA,
		Check: check, Source: source,
	}
	run, runErr := workspace.CoordinateNativeRun(ctx, canonicalRoot, request, stdout)
	if run.ToolReceipt.Path == "" {
		if run.Attempt != nil {
			finishErr := run.Attempt.Finish(run.ReceiptPath, runErr)
			if runErr == nil {
				runErr = finishErr
			}
		}
		if runErr != nil {
			return fail(runErr)
		}
		return fail(fmt.Errorf("native WorkerCheck returned no process-captured receipt"))
	}
	wrapper := assignment.NativeOwnedCheckReceipt{
		CheckIndex: check.Index, Command: check.Command, ProfileDigest: check.Profile.Digest(),
		Record: authority.Record.Reference(), RecordSHA256: authority.RecordSHA,
		RuntimeID: authority.Record.RuntimeID, BaselineGeneration: authority.Record.BaselineGeneration,
		AssignmentID: authority.Record.AssignmentID, OwnerAgentID: authority.Record.OwnerAgentID,
		ExecutionGeneration: 0, NativeContextGeneration: authority.Binding.Generation,
		NativeContextRef: authority.Binding.Context.Path, NativeContextSHA256: authority.Binding.Context.SHA256,
		ActivationRef: authority.ActivationRef, ActivationSHA256: authority.ActivationSHA,
		Checkout: authority.Source.Checkout, SourceBranch: authority.Source.Branch,
		TargetBranch: authority.Source.TargetBranch, SourceHead: authority.Source.Head,
		SourceSnapshotSHA256: run.SnapshotSHA256, ToolReceipt: run.ToolReceipt,
	}
	var acceptErr error
	if run.Reused {
		acceptErr = fmt.Errorf("native WorkerCheck runner must execute fresh and cannot reuse a prior receipt")
	} else if run.ReceiptPath != wrapper.ToolReceipt.Path {
		acceptErr = fmt.Errorf("native runner path differs from its process-captured ToolReceipt")
	} else if err := validateCapturedNativeToolReceipt(canonicalRoot, authority, wrapper, run.Passed); err != nil {
		acceptErr = err
	} else if err := acceptNativeOwnedCheckReceipt(canonicalRoot, *owner, wrapper); err != nil {
		acceptErr = err
	}
	finishErr := errors.Join(acceptErr, runErr)
	if run.Attempt != nil {
		if err := run.Attempt.Finish(run.ReceiptPath, finishErr); err != nil {
			finishErr = errors.Join(finishErr, err)
		}
	}
	if acceptErr != nil {
		return fail(errors.Join(acceptErr, finishErr))
	}
	fmt.Fprintf(stderr, "Native WorkerCheck receipt recorded: %s\n", wrapper.ToolReceipt.Path)
	if finishErr != nil {
		return fail(finishErr)
	}
	if !run.Passed {
		return fail(fmt.Errorf("native WorkerCheck %d failed; actual failure receipt was recorded", check.Index))
	}
	return 0
}

func nativeCheckRuntimeWriter(root string) *runtime.Store {
	return runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
}

func resolveNativeCheckAuthority(root string, snapshot runtime.Snapshot, owner string, index int) (nativeCheckAuthority, error) {
	var out nativeCheckAuthority
	state := snapshot.State
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return out, fmt.Errorf("native WorkerCheck requires original Runtime 1.2")
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return out, err
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle == nil || lifecycle["state"] != "building" {
		return out, fmt.Errorf("native WorkerCheck requires the original building lifecycle")
	}
	activated, err := plancheckpoint.ActivatedRef(root, snapshot, owner)
	if err != nil {
		return out, err
	}
	if activated == "" {
		return out, fmt.Errorf("native WorkerCheck requires the current original PLAN and activation")
	}
	agent := contextAgent(state, owner)
	if agent == nil || (workstate.AgentActivity(agent) != "working" && workstate.AgentActivity(agent) != "reported") {
		return out, fmt.Errorf("native WorkerCheck owner is not in an active original working/reporting state")
	}
	binding, bound, err := decodeContextBinding(agent)
	if err != nil {
		return out, err
	}
	if !bound {
		return out, fmt.Errorf("native WorkerCheck requires a persisted fixed context binding; prepare the original context first")
	}
	loaded, err := hookctx.FromSnapshot(root, owner, snapshot)
	if err != nil {
		return out, err
	}
	var selected *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		candidate := &loaded.Assignments[i]
		if candidate.OwnerAgentID != owner {
			continue
		}
		if selected != nil {
			return out, fmt.Errorf("native WorkerCheck owner has ambiguous current Assignments")
		}
		selected = candidate
	}
	if selected == nil {
		return out, fmt.Errorf("native WorkerCheck owner has no current original Assignment")
	}
	record, found, err := hookctx.ResolveAssignmentRecordProjection(root, state, *selected)
	if err != nil {
		return out, err
	}
	if !found || record.ClosingContract == nil || index >= len(record.ClosingContract.WorkerChecks) || record.ClosingContract.WorkerChecks[index].Index != index {
		return out, fmt.Errorf("requested check index is not in the current canonical ClosingContract")
	}
	if record.DispatchMode != "plan_checkpoint" {
		return out, fmt.Errorf("native WorkerCheck requires the original plan_checkpoint dispatch; other approval modes retain their original gate")
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) { return assignmentinputs.Read(root, ref) }); err != nil {
		return out, fmt.Errorf("current Assignment Record sources are not verifiable: %w", err)
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return out, err
	}
	context, err := verifyFixedContext(root, state, *selected, binding)
	if err != nil {
		return out, fmt.Errorf("native WorkerCheck fixed context: %w", err)
	}
	if context.Dispatch.AssignmentRecord == nil || *context.Dispatch.AssignmentRecord != record.Reference() || context.Dispatch.AssignmentRecordSHA256 != recordSHA {
		return out, fmt.Errorf("native context does not project the current Assignment Record")
	}
	activationRef, activationSHA, err := validatedNativeActivation(root, snapshot, owner, agent, activated)
	if err != nil {
		return out, err
	}
	source, err := assignment.ResolveFrozenNativeCheckSource(root, state, record)
	if err != nil {
		return out, err
	}
	out = nativeCheckAuthority{Assignment: *selected, Record: record, RecordSHA: recordSHA, Binding: binding, Context: context, Source: source, ActivationRef: activationRef, ActivationSHA: activationSHA}
	return out, nil
}

func validatedNativeActivation(root string, snapshot runtime.Snapshot, owner string, agent map[string]any, activatedPlanRef string) (string, string, error) {
	activationRef, _ := agent["activation_ref"].(string)
	if activationRef == "" {
		return "", "", fmt.Errorf("native WorkerCheck has no current original activation artifact")
	}
	before, err := workpacket.ReadFile(root, activationRef, 1<<20)
	if err != nil {
		return "", "", fmt.Errorf("read current original activation artifact: %w", err)
	}
	sha := workpacket.Digest(before)
	planRef, err := plancheckpoint.ActivatedRef(root, snapshot, owner)
	if err != nil || planRef == "" || planRef != activatedPlanRef {
		return "", "", fmt.Errorf("native WorkerCheck PLAN/activation changed during source validation")
	}
	after, err := workpacket.ReadFile(root, activationRef, 1<<20)
	if err != nil || workpacket.Digest(after) != sha {
		return "", "", fmt.Errorf("native WorkerCheck activation bytes changed during source validation")
	}
	return activationRef, sha, nil
}

func validateCapturedNativeToolReceipt(root string, authority nativeCheckAuthority, wrapper assignment.NativeOwnedCheckReceipt, expectedPassed bool) error {
	if err := assignment.ValidateNativeOwnedCheckReceipt(authority.Record, authority.Binding.Generation, authority.Binding.Context.Path, authority.Binding.Context.SHA256, authority.ActivationRef, authority.ActivationSHA, authority.Source, wrapper); err != nil {
		return err
	}
	receiptRoot := filepath.Join(root, ".claude/evidence")
	actual, err := filepath.EvalSymlinks(wrapper.ToolReceipt.Path)
	if err != nil {
		return err
	}
	base, err := filepath.EvalSymlinks(receiptRoot)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, actual)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("native check receipt is outside original Main evidence")
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Dir(actual), wrapper.ToolReceipt)
	if err != nil {
		return fmt.Errorf("verify process-captured native Worker receipt: %w", err)
	}
	if verified.Passed != expectedPassed {
		return fmt.Errorf("native runner result differs from its sealed captured receipt status")
	}
	return nil
}

func acceptNativeOwnedCheckReceipt(root, owner string, receipt assignment.NativeOwnedCheckReceipt) error {
	writer := nativeCheckRuntimeWriter(root)
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err := writer.Snapshot()
		if err != nil {
			return err
		}
		current, err := resolveNativeCheckAuthority(root, snapshot, owner, receipt.CheckIndex)
		if err != nil {
			return fmt.Errorf("revalidate native WorkerCheck before receipt acceptance: %w", err)
		}
		if err := assignment.ValidateNativeOwnedCheckReceipt(current.Record, current.Binding.Generation, current.Binding.Context.Path, current.Binding.Context.SHA256, current.ActivationRef, current.ActivationSHA, current.Source, receipt); err != nil {
			return err
		}
		previous, err := decodeNativeOwnedCheckReceipts(contextAgent(snapshot.State, owner))
		if err != nil {
			return err
		}
		registered := false
		for _, item := range previous {
			if item.ToolReceipt.SHA256 == receipt.ToolReceipt.SHA256 {
				if !reflect.DeepEqual(item, receipt) {
					return fmt.Errorf("native check receipt replay differs from its original Runtime binding")
				}
				registered = true
			}
		}
		if registered {
			return nil
		}
		if len(previous) >= 1024 {
			return fmt.Errorf("native owned_check_receipts reached its non-evicting 1024-entry limit")
		}
		key := "native-worker-check:" + receipt.ToolReceipt.SHA256
		_, err = writer.Update(snapshot.Revision, runtime.Mutation{
			EventID: key, TransitionID: "NATIVE-WORKER-CHECK", Event: "native_worker_check_received",
			Actor: owner, RuntimeID: receipt.RuntimeID, BaselineGeneration: receipt.BaselineGeneration,
			IdempotencyKey: key, RetainLastTransition: true, ProducerResponsibility: "owned_check",
			OccurredAt: time.Now().UTC(), Message: "record process-captured native WorkerCheck evidence; no professional acceptance or PASS",
			Apply: func(state map[string]any) error {
				latest, err := resolveNativeCheckAuthority(root, runtime.Snapshot{State: state}, owner, receipt.CheckIndex)
				if err != nil {
					return fmt.Errorf("native WorkerCheck authority changed during receipt write: %w", err)
				}
				if err := assignment.ValidateNativeOwnedCheckReceipt(latest.Record, latest.Binding.Generation, latest.Binding.Context.Path, latest.Binding.Context.SHA256, latest.ActivationRef, latest.ActivationSHA, latest.Source, receipt); err != nil {
					return err
				}
				row := contextAgent(state, owner)
				stored, err := decodeNativeOwnedCheckReceipts(row)
				if err != nil {
					return err
				}
				for _, item := range stored {
					if item.ToolReceipt.SHA256 == receipt.ToolReceipt.SHA256 {
						if reflect.DeepEqual(item, receipt) {
							return nil
						}
						return fmt.Errorf("native check receipt digest is already bound to different facts")
					}
				}
				if len(stored) >= 1024 {
					return fmt.Errorf("native owned_check_receipts reached its non-evicting 1024-entry limit")
				}
				data, err := json.Marshal(append(stored, receipt))
				if err != nil {
					return err
				}
				var rows []any
				if err := json.Unmarshal(data, &rows); err != nil {
					return err
				}
				row["owned_check_receipts"] = rows
				return nil
			},
		})
		if errors.Is(err, runtime.ErrStaleRevision) {
			continue
		}
		return err
	}
	return runtime.ErrStaleRevision
}

func decodeNativeOwnedCheckReceipts(agent map[string]any) ([]assignment.NativeOwnedCheckReceipt, error) {
	if agent == nil {
		return nil, fmt.Errorf("native WorkerCheck owner disappeared")
	}
	raw, exists := agent["owned_check_receipts"]
	if !exists || raw == nil {
		return []assignment.NativeOwnedCheckReceipt{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var receipts []assignment.NativeOwnedCheckReceipt
	if err := json.Unmarshal(data, &receipts); err != nil {
		return nil, fmt.Errorf("decode original native WorkerCheck receipt index: %w", err)
	}
	if receipts == nil || len(receipts) > 1024 {
		return nil, fmt.Errorf("original native WorkerCheck receipt index is invalid or over its bound")
	}
	return receipts, nil
}
