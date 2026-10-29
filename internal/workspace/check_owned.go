package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// NativeCheckSource contains the frozen source coordinate for an owner-run
// check. It is populated by resolving the current Record-frozen manifest.
type NativeCheckSource struct {
	Checkout       string
	Branch         string
	TargetBranch   string
	Head           string
	ManifestRef    string
	ManifestSHA256 string
}

// NativeOwnedCheckRequest carries the original Runtime and Record tuple plus
// the selected Record-owned WorkerCheck. It cannot supply a command, profile,
// or independent read root.
type NativeOwnedCheckRequest struct {
	RuntimeID          string
	BaselineGeneration int
	AssignmentID       string
	OwnerAgentID       string
	Record             assignmentrecord.RecordReference
	RecordSHA256       string
	RecordData         assignmentrecord.Record
	ContextGeneration  int
	ContextRef         agentcontext.Reference
	ContextSHA256      string
	ActivationRef      string
	ActivationSHA256   string
	Check              assignmentrecord.WorkerCheck
	Source             NativeCheckSource
}

type NativeOwnedCheckResult struct {
	ReceiptPath    string
	SnapshotSHA256 string
	ToolReceipt    checkexec.ToolReceipt
	Passed         bool
	Reused         bool
	Attempt        *OwnedCheckRunAttempt
}

// CheckSnapshotDigest applies the exact copyCheckTree exclusions, file-mode
// normalization and symlink checks used by the runner, returning the hash of
// the disposable snapshot without invoking a command.
func CheckSnapshotDigest(ctx context.Context, source string) (string, error) {
	root, err := Canonical(source)
	if err != nil || root != source {
		return "", fmt.Errorf("check source must be a canonical checkout root")
	}
	scratch, err := os.MkdirTemp("", "loop-owned-check-snapshot-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	return copyCheckTree(ctx, source, scratch)
}

// ObserveOwnedCheckSource returns both the current runner-compatible copy
// hash and the profile observation computed from that same normalized copy.
// Permission facts are intentionally left to the current Runtime-bound runner
// because they include its independent control/source coordinates.
func ObserveOwnedCheckSource(ctx context.Context, source string, profile checkexec.Profile) (string, checkexec.Observation, error) {
	var empty checkexec.Observation
	root, err := Canonical(source)
	if err != nil || root != source {
		return "", empty, fmt.Errorf("check source must be a canonical checkout root")
	}
	if profile.Location != "worker" {
		return "", empty, fmt.Errorf("owned check profile must use the frozen Worker runner")
	}
	scratch, err := os.MkdirTemp("", "loop-owned-check-observe-")
	if err != nil {
		return "", empty, err
	}
	defer os.RemoveAll(scratch)
	snapshot, err := copyCheckTree(ctx, source, scratch)
	if err != nil {
		return "", empty, err
	}
	observation, err := checkexec.ObserveProfile(checkexec.WithProfile(ctx, profile), scratch, profile)
	if err != nil {
		return "", empty, err
	}
	return snapshot, observation, nil
}

// OwnedCheckRunAttempt keeps the original same-key diagnostic locked until
// the caller has completed the owning Runtime Writer receipt reception.
type OwnedCheckRunAttempt struct {
	attempt  *checkexec.RunAttempt
	finished bool
}

func (a *OwnedCheckRunAttempt) Finish(receipt string, runErr error) error {
	if a == nil || a.attempt == nil || a.finished {
		return nil
	}
	a.finished = true
	err := a.attempt.Finish(receipt, runErr)
	a.attempt.Close()
	return err
}

func (a *OwnedCheckRunAttempt) Close() {
	if a == nil || a.attempt == nil || a.finished {
		return
	}
	_ = a.Finish("", fmt.Errorf("owned-check receipt was not received by its original Writer"))
}

// OwnedCheckInvocation is a fixed command projection of one immutable
// WorkerCheck. Its index is separate from Main RequiredChecks.
func OwnedCheckInvocation(e Execution, index int) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `"'"'"'`) + "'" }
	return quote(filepath.Join(e.Path, ".claude/bin/loop-harness")) + " runtime workspace check --root " + quote(e.Path) + " --assignment " + quote(e.AssignmentID) + " --agent " + quote(e.AgentID) + " --owned-check " + fmt.Sprint(index)
}

// RunOwnedCheck executes exactly one frozen WorkerCheck in the same
// disposable-copy Bubblewrap/seccomp runner as the original workspace checks.
// Main-required checks remain independent and are never run through this API.
func (b *ExecutionRegistry) RunOwnedCheck(ctx context.Context, e Execution, index int, output io.Writer) (NativeOwnedCheckResult, error) {
	var empty NativeOwnedCheckResult
	if index < 0 || index >= len(e.WorkerChecks) {
		return empty, fmt.Errorf("select a frozen WorkerCheck index")
	}
	if e.Status != "ready" || e.BootstrapSHA256 == "" || e.AssignmentRecord == nil {
		return empty, fmt.Errorf("owned check requires the current bootstrapped canonical Assignment execution")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return empty, err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return empty, err
	}
	if err := b.ValidateInputs(e); err != nil {
		return empty, err
	}
	if err := b.ValidateBootstrap(e); err != nil {
		return empty, err
	}
	check := e.WorkerChecks[index]
	if check.Index != index || check.Command == "" || check.Command != check.Profile.Command || check.Profile.CheckIndex != index || check.Profile.Location != "worker" {
		return empty, fmt.Errorf("selected WorkerCheck differs from its frozen owner profile")
	}
	return runOwnedCheck(ctx, ownedCheckRun{
		MainRoot: b.MainRoot, CommonDir: b.CommonDir, Checkout: e.Path,
		EvidenceRoot: filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"),
		RunsRoot:     filepath.Join(b.MainRoot, ".claude/evidence/check-runs"),
		AssignmentID: e.AssignmentID, Index: index, Check: check,
		Binding: CheckBinding(e), Profile: check.Profile, Output: output,
		ReceiptFacts: map[string]any{
			"assignment_record":        e.AssignmentRecord,
			"assignment_record_sha256": e.AssignmentRecordSHA256,
			"worker_check_index":       index,
			"worker_check_contract":    "s6-closing-v1",
			"worker_check_snapshot":    map[string]any{"runtime_id": e.RuntimeID, "baseline_generation": e.BaselineGeneration, "assignment_id": e.AssignmentID, "owner_agent_id": e.AgentID, "execution_generation": e.Generation, "bootstrap_sha256": e.BootstrapSHA256},
		},
		PermissionObservation: func(ctx context.Context, scratch string) checkexec.PermissionObservation {
			return b.observeCheckPermissions(ctx, e, scratch)
		},
		PriorReceipts: latestOwnedCheckReceipts(e.OwnedCheckReceipts, index),
		ReuseRoot:     filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"),
		UseReuse:      true,
	})
}

// CoordinateNativeRun is the same isolated check engine used by workspace
// execution. The CLI supplies only its authenticated Runtime root and the
// currently resolved Record context; this function re-reads and verifies the
// current Runtime, Record, context bytes, source checkout, and WorkerCheck.
func CoordinateNativeRun(ctx context.Context, controlRoot string, req NativeOwnedCheckRequest, output io.Writer) (NativeOwnedCheckResult, error) {
	var empty NativeOwnedCheckResult
	root, err := Canonical(controlRoot)
	if err != nil {
		return empty, fmt.Errorf("canonical Runtime root: %w", err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	snapshot, err := loopruntime.NewStore(statePath, filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return empty, err
	}
	state := snapshot.State
	if RuntimeID(state) != req.RuntimeID || Generation(state) != req.BaselineGeneration || req.RuntimeID == "" || req.BaselineGeneration < 1 {
		return empty, fmt.Errorf("native owned check differs from current Runtime identity/generation")
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "building" {
		return empty, fmt.Errorf("native owned check requires the current Builder lifecycle")
	}
	if !currentOwnerActive(state, req.OwnerAgentID) {
		return empty, fmt.Errorf("native owned check owner is no longer active")
	}
	if _, err := plancheckpoint.ActivatedRef(root, snapshot, req.OwnerAgentID); err != nil {
		return empty, fmt.Errorf("native owned check requires the current accepted PLAN and activation chain: %w", err)
	}
	activationRef := currentAgentActivationRef(state, req.OwnerAgentID)
	if activationRef == "" || activationRef != req.ActivationRef || req.ActivationSHA256 == "" {
		return empty, fmt.Errorf("native owned check activation ref differs from the current Agent row")
	}
	activationBytes, err := workpacket.ReadFile(root, activationRef, 1<<20)
	if err != nil || workpacket.Digest(activationBytes) != req.ActivationSHA256 {
		return empty, fmt.Errorf("native owned check activation bytes changed")
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", activationBytes); err != nil {
		return empty, fmt.Errorf("native owned check activation envelope is invalid: %w", err)
	}
	bound, _ := state["bound_req"].(map[string]any)
	workspaceAuthority, _ := bound["workspace"].(map[string]any)
	projectRoot, _ := workspaceAuthority["project_root"].(string)
	devBranch, _ := workspaceAuthority["dev_branch"].(string)
	projectRoot, err = Canonical(projectRoot)
	if err != nil || projectRoot != root {
		return empty, fmt.Errorf("native owned check root differs from the original REQ-bound Main root")
	}
	devBranch = strings.TrimPrefix(devBranch, "refs/heads/")
	if devBranch == "" {
		return empty, fmt.Errorf("native owned check has no REQ-bound development branch")
	}

	if err := assignmentrecord.ValidateRecord(req.RecordData); err != nil {
		return empty, fmt.Errorf("validate native Assignment Record: %w", err)
	}
	current, found, err := assignmentrecord.ForOwner(state, req.OwnerAgentID)
	if err != nil || !found {
		return empty, fmt.Errorf("native owned check has no current canonical Assignment Record: %w", err)
	}
	currentSHA, err := current.Digest()
	if err != nil {
		return empty, err
	}
	requestedSHA, err := req.RecordData.Digest()
	if err != nil || current.Reference() != req.Record || requestedSHA != req.RecordSHA256 || currentSHA != req.RecordSHA256 || req.RecordData.Reference() != req.Record {
		return empty, fmt.Errorf("native owned check Record tuple/digest is not current")
	}
	if req.RuntimeID != req.Record.RuntimeID || req.BaselineGeneration != req.Record.BaselineGeneration || req.AssignmentID != req.Record.AssignmentID || req.OwnerAgentID != req.RecordData.OwnerAgentID {
		return empty, fmt.Errorf("native owned check identity differs from its canonical Assignment Record")
	}
	if req.RecordData.ClosingContract == nil || req.ContextGeneration < 0 || req.ContextRef.Path == "" || req.ContextSHA256 == "" || req.ContextSHA256 != req.ContextRef.SHA256 {
		return empty, fmt.Errorf("native owned check requires a frozen context and explicit closing contract")
	}
	if err := assignmentrecord.VerifySources(req.RecordData, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(root, ref)
	}); err != nil {
		return empty, fmt.Errorf("native Assignment source closure changed: %w", err)
	}
	contextBytes, err := workpacket.ReadFile(root, req.ContextRef.Path, 2<<20)
	if err != nil || workpacket.Digest(contextBytes) != req.ContextSHA256 {
		return empty, fmt.Errorf("native fixed context bytes changed")
	}
	if req.RecordData.Manifest.Ref != req.Source.ManifestRef || req.RecordData.Manifest.SHA256 != req.Source.ManifestSHA256 {
		return empty, fmt.Errorf("native check source is not bound to the Record-frozen manifest")
	}
	if !sameWorkerCheck(req.RecordData.ClosingContract.WorkerChecks, req.Check) {
		return empty, fmt.Errorf("selected native WorkerCheck differs from the canonical Record")
	}
	if err := validateNativeCheckSource(root, devBranch, req); err != nil {
		return empty, err
	}
	if req.Check.Index < 0 || req.Check.Index >= len(req.RecordData.ClosingContract.WorkerChecks) || req.Check.Profile.Location != "worker" {
		return empty, fmt.Errorf("native WorkerCheck index/profile is invalid")
	}
	check := req.RecordData.ClosingContract.WorkerChecks[req.Check.Index]
	common, err := Git(ctx, req.Source.Checkout, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return empty, err
	}
	common, err = Canonical(common)
	if err != nil {
		return empty, err
	}

	nativeContext := map[string]any{
		"generation": req.ContextGeneration,
		"ref":        req.ContextRef.Path,
		"sha256":     req.ContextSHA256,
	}
	activation := map[string]any{"ref": req.ActivationRef, "sha256": req.ActivationSHA256}
	return runOwnedCheck(ctx, ownedCheckRun{
		MainRoot: root, CommonDir: common, Checkout: req.Source.Checkout,
		EvidenceRoot: filepath.Join(root, ".claude/evidence/workspace-checks"),
		RunsRoot:     filepath.Join(root, ".claude/evidence/check-runs"),
		AssignmentID: req.AssignmentID, Index: check.Index, Check: check,
		Binding: checkexec.Binding{RuntimeID: req.RuntimeID, Generation: req.BaselineGeneration, AssignmentID: req.AssignmentID, Owner: req.OwnerAgentID, ExecutionGeneration: 0, Responsibility: "owned_check"},
		Profile: check.Profile, Output: output, UseReuse: false,
		ReceiptFacts: map[string]any{
			"record":         req.Record,
			"record_sha256":  req.RecordSHA256,
			"activation":     activation,
			"source_branch":  req.Source.Branch,
			"target_branch":  req.Source.TargetBranch,
			"source_head":    req.Source.Head,
			"native_context": nativeContext,
		},
		PermissionObservation: func(ctx context.Context, scratch string) checkexec.PermissionObservation {
			return observeNativeCheckPermissions(ctx, root, common, req.Source.Checkout, scratch, req)
		},
	})
}

type ownedCheckRun struct {
	MainRoot, CommonDir, Checkout, EvidenceRoot, RunsRoot string
	AssignmentID                                          string
	Index                                                 int
	Check                                                 assignmentrecord.WorkerCheck
	Binding                                               checkexec.Binding
	Profile                                               checkexec.Profile
	Output                                                io.Writer
	ReceiptFacts                                          map[string]any
	PermissionObservation                                 func(context.Context, string) checkexec.PermissionObservation
	PriorReceipts                                         []checkexec.ToolReceipt
	ReuseRoot                                             string
	UseReuse                                              bool
}

func runOwnedCheck(ctx context.Context, spec ownedCheckRun) (NativeOwnedCheckResult, error) {
	var result NativeOwnedCheckResult
	if spec.Output == nil {
		spec.Output = io.Discard
	}
	if spec.Check.Index != spec.Index || spec.Check.Command != spec.Profile.Command || spec.Profile.CheckIndex != spec.Index || spec.Profile.Location != "worker" || spec.Binding.Responsibility != "owned_check" {
		return result, fmt.Errorf("owned check invocation differs from its frozen WorkerCheck")
	}
	ctx = checkexec.WithProfile(ctx, spec.Profile)
	ctx = checkexec.WithBinding(ctx, spec.Binding)
	ctx, cancel := checkexec.CheckTimeout(ctx)
	defer cancel()
	if _, err := spec.Profile.ActualEnvironment(); err != nil {
		return result, checkexec.New("capability_missing", "environment owner", spec.Profile.Command, err)
	}
	scratch, err := os.MkdirTemp("", "loop-owned-check-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(scratch)
	snapshot, err := copyCheckTree(ctx, spec.Checkout, scratch)
	if err != nil {
		return result, err
	}
	result.SnapshotSHA256 = snapshot
	observation, err := checkexec.ObserveProfile(ctx, scratch, spec.Profile)
	if err != nil {
		return result, err
	}
	permissions := spec.PermissionObservation(ctx, scratch)
	observation.Permissions = &permissions
	observation.PermissionDigest = permissions.Digest()
	var reuse *checkexec.ReuseDecision
	if spec.UseReuse && spec.Profile.ReusePolicy != "never" {
		guard := checkexec.CurrentReuseGuard(ctx)
		decision := checkexec.DecideReuse(spec.ReuseRoot, checkexec.ReuseRequest{Profile: spec.Profile, Observation: observation, Binding: spec.Binding, Checkout: spec.Checkout, Fresh: guard.Fresh, NewHypothesis: guard.NewHypothesis}, spec.PriorReceipts)
		reuse = &decision
		if decision.Action == "reuse" {
			for i := len(spec.PriorReceipts) - 1; i >= 0; i-- {
				prior := spec.PriorReceipts[i]
				if prior.Path != decision.ReceiptRef {
					continue
				}
				verified, verifyErr := checkexec.VerifyToolReceipt(spec.ReuseRoot, prior)
				if verifyErr != nil || !verified.Passed {
					return result, fmt.Errorf("reuse receipt changed after the current reuse decision")
				}
				result.ReceiptPath, result.ToolReceipt, result.Passed, result.Reused = prior.Path, prior, true, true
				fmt.Fprintln(spec.Output, "Worker check reused latest original accepted receipt:", prior.Path)
				return result, nil
			}
			return result, fmt.Errorf("reuse decision refers to an unindexed original receipt")
		}
	}
	keyBytes, _ := json.Marshal(struct {
		Kind     string
		Binding  checkexec.Binding
		Checkout string
		Index    int
		Command  string
		Profile  string
		Snapshot string
		Facts    map[string]any
	}{"worker_owned", spec.Binding, spec.Checkout, spec.Index, spec.Profile.Command, spec.Profile.Digest(), snapshot, spec.ReceiptFacts})
	attempt, err := checkexec.BeginRun(ctx, spec.RunsRoot, checkexec.Hash(keyBytes), spec.Binding)
	if err != nil {
		return result, err
	}
	pending := &OwnedCheckRunAttempt{attempt: attempt}
	if err := attempt.Start(); err != nil {
		_ = pending.Finish("", err)
		return result, err
	}
	finish := func(path string, runErr error) {
		_ = pending.Finish(path, runErr)
	}
	if err := os.MkdirAll(spec.EvidenceRoot, 0700); err != nil {
		finish("", err)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, err)
	}
	dir, err := os.MkdirTemp(spec.EvidenceRoot, spec.AssignmentID+"-owned-")
	if err != nil {
		finish("", err)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, err)
	}
	logPath := filepath.Join(dir, "output.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		finish("", err)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, err)
	}
	started := time.Now().UTC()
	runErr := runIsolatedCheck(ctx, spec.MainRoot, spec.CommonDir, spec.Checkout, scratch, spec.Profile.Command, io.MultiWriter(spec.Output, log))
	closeErr := log.Close()
	if runErr == nil && spec.Profile.Location == "worker" {
		after, afterErr := checkexec.ObserveProfile(ctx, scratch, spec.Profile)
		afterPermissions := spec.PermissionObservation(ctx, scratch)
		after.Permissions = &afterPermissions
		after.PermissionDigest = afterPermissions.Digest()
		if afterErr != nil || !sameOwnedObservationBeforeAfter(observation, after) {
			runErr = checkexec.New("input_drift", "environment owner", spec.Profile.Command, fmt.Errorf("profile inputs/environment/runner changed during check"))
		}
	}
	if runErr == nil {
		runErr = closeErr
	}
	if closeErr != nil && runErr != nil {
		runErr = checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, closeErr)
	}
	status := "pass"
	message := ""
	if runErr != nil {
		status, message = "fail", runErr.Error()
	}
	failure := checkexec.Classify(ctx, "environment owner", spec.Profile.Command, runErr)
	if closeErr != nil {
		failure = checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, closeErr)
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	if failure != nil {
		failure.ReceiptRef = receiptPath
		failure.LogRef = logPath
	}
	binding, _ := checkexec.CurrentBinding(ctx)
	receipt := map[string]any{
		"reuse_decision": reuse, "check_binding": binding,
		"profile_observation": observation, "failure": failure,
		"version": 2, "runtime_id": binding.RuntimeID,
		"baseline_generation": binding.Generation, "assignment_id": binding.AssignmentID,
		"execution_generation": binding.ExecutionGeneration,
		"check_index":          spec.Index, "command": spec.Profile.Command,
		"snapshot_sha256": snapshot, "started_at": started,
		"elapsed_ms": time.Since(started).Milliseconds(), "status": status,
		"error": message, "output": logPath, "disposable_copy": true,
	}
	for key, value := range spec.ReceiptFacts {
		receipt[key] = value
	}
	sealed, sealErr := checkexec.SealReceipt(receipt, logPath)
	if sealErr != nil {
		finish(receiptPath, sealErr)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, sealErr)
	}
	if err := publishBootstrapFile(receiptPath, sealed, 0600); err != nil {
		finish(receiptPath, err)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, err)
	}
	var captured *checkexec.ToolReceipt
	captureCtx := checkexec.WithReceiptCapture(ctx, func(ref checkexec.ToolReceipt) { captured = &ref })
	if err := checkexec.CaptureReceipt(captureCtx, receiptPath, spec.Checkout, sealed); err != nil {
		finish(receiptPath, err)
		return result, checkexec.New("diagnostic_io", "environment owner", spec.Profile.Command, err)
	}
	if captured == nil {
		finish(receiptPath, fmt.Errorf("actual owned runner did not capture its sealed receipt"))
		return result, fmt.Errorf("actual owned runner did not capture its sealed receipt")
	}
	result.ReceiptPath, result.ToolReceipt, result.Passed, result.Attempt = receiptPath, *captured, runErr == nil, pending
	if runErr != nil {
		return result, failure
	}
	return result, nil
}

func sameOwnedObservationBeforeAfter(a, b checkexec.Observation) bool {
	return a.ProfileID == b.ProfileID && a.ProfileVersion == b.ProfileVersion &&
		a.ProfileDigest == b.ProfileDigest && a.SubjectDigest == b.SubjectDigest &&
		a.RunnerDigest == b.RunnerDigest && a.EnvironmentDigest == b.EnvironmentDigest &&
		a.DependencyDigest == b.DependencyDigest && a.FixtureDigest == b.FixtureDigest &&
		a.PermissionDigest == b.PermissionDigest && a.VerificationScope == b.VerificationScope &&
		a.ReuseEligible == b.ReuseEligible && reflect.DeepEqual(a.Environment, b.Environment) &&
		a.Permissions != nil && b.Permissions != nil && a.Permissions.Digest() == b.Permissions.Digest()
}

func latestOwnedCheckReceipts(rows []OwnedCheckReceipt, index int) []checkexec.ToolReceipt {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].WorkerCheckIndex == index {
			return []checkexec.ToolReceipt{rows[i].Receipt}
		}
	}
	return nil
}

func sameWorkerCheck(rows []assignmentrecord.WorkerCheck, check assignmentrecord.WorkerCheck) bool {
	if check.Index < 0 || check.Index >= len(rows) {
		return false
	}
	row := rows[check.Index]
	return row.Index == check.Index && row.Command == check.Command && row.Profile.Digest() == check.Profile.Digest()
}

func currentOwnerActive(state map[string]any, owner string) bool {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	found := false
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		if found || (workstate.AgentActivity(row) != "working" && workstate.AgentActivity(row) != "reported") {
			return false
		}
		found = true
	}
	return found
}

func currentAgentActivationRef(state map[string]any, owner string) string {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	var ref string
	found := false
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		if found {
			return ""
		}
		found = true
		ref, _ = row["activation_ref"].(string)
	}
	return ref
}

func validateNativeCheckSource(root, devBranch string, req NativeOwnedCheckRequest) error {
	source := req.Source
	if source.Checkout == "" || source.Branch == "" || source.TargetBranch == "" || source.Head == "" || source.ManifestRef != req.RecordData.Manifest.Ref || source.ManifestSHA256 != req.RecordData.Manifest.SHA256 {
		return fmt.Errorf("native check source omits a Record-frozen checkout coordinate")
	}
	checkout, err := Canonical(source.Checkout)
	if err != nil || checkout != source.Checkout || checkout == root {
		return fmt.Errorf("native check checkout is not a distinct canonical Worker root")
	}
	if err := ValidateCheckout(root, checkout); err != nil {
		return fmt.Errorf("native Worker checkout differs from the bound repository: %w", err)
	}
	branch, err := Git(context.Background(), checkout, "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) != source.Branch {
		return fmt.Errorf("native checkout branch differs from frozen manifest coordinates")
	}
	head, err := Git(context.Background(), checkout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != source.Head {
		return fmt.Errorf("native checkout HEAD differs from the frozen source head")
	}
	manifestBytes, err := assignmentinputs.Read(root, source.ManifestRef)
	if err != nil || workpacket.Digest(manifestBytes) != source.ManifestSHA256 {
		return fmt.Errorf("native source manifest bytes changed")
	}
	var manifest struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		WorkgroupID        string `json:"workgroup_id"`
		Assignments        []struct {
			AssignmentID string `json:"assignment_id"`
			AgentID      string `json:"agent_id"`
			WorktreePath string `json:"worktree_path"`
			Branch       string `json:"branch"`
			TargetBranch string `json:"target_branch"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return err
	}
	if manifest.RuntimeID != req.RuntimeID || manifest.BaselineGeneration != req.BaselineGeneration || manifest.WorkgroupID != req.RecordData.TeamID {
		return fmt.Errorf("native frozen manifest differs from current Record tuple")
	}
	var matched bool
	for _, row := range manifest.Assignments {
		if row.AssignmentID != req.AssignmentID {
			continue
		}
		if matched || row.AgentID != req.OwnerAgentID || row.Branch != source.Branch || strings.TrimPrefix(row.TargetBranch, "refs/heads/") != source.TargetBranch {
			return fmt.Errorf("native frozen manifest contains duplicate or changed Worker coordinates")
		}
		path := row.WorktreePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		path, err = Canonical(path)
		if err != nil || path != checkout {
			return fmt.Errorf("native checkout path differs from frozen manifest coordinates")
		}
		matched = true
	}
	if !matched {
		return fmt.Errorf("native frozen manifest omits the selected Assignment")
	}
	if strings.TrimPrefix(source.TargetBranch, "refs/heads/") != devBranch {
		return fmt.Errorf("native target branch differs from the original REQ development branch")
	}
	return nil
}

// VerifyOwnedCheckReceipt checks the runner seal and the domain tuple captured
// by the original Workspace writer before a ref is registered.
func VerifyOwnedCheckReceipt(evidenceRoot string, e Execution, index int, ref checkexec.ToolReceipt) (checkexec.VerifiedReceipt, error) {
	var out checkexec.VerifiedReceipt
	if index < 0 || index >= len(e.WorkerChecks) {
		return out, fmt.Errorf("owned receipt index is outside frozen WorkerChecks")
	}
	check := e.WorkerChecks[index]
	if ref.Binding != CheckBinding(e) || ref.Checkout != e.Path || ref.Command != check.Command || ref.ProfileDigest != check.Profile.Digest() {
		return out, fmt.Errorf("owned tool receipt differs from the original WorkerCheck")
	}
	out, err := checkexec.VerifyToolReceipt(evidenceRoot, ref)
	if err != nil {
		return out, err
	}
	data, err := workpacket.ReadFile(filepath.Dir(ref.Path), filepath.Base(ref.Path), 4<<20)
	if err != nil {
		return out, err
	}
	var raw struct {
		CheckIndex     int                               `json:"check_index"`
		Assignment     *assignmentrecord.RecordReference `json:"assignment_record"`
		AssignmentSHA  string                            `json:"assignment_record_sha256"`
		WorkerIndex    int                               `json:"worker_check_index"`
		WorkerSnapshot map[string]any                    `json:"worker_check_snapshot"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, err
	}
	if raw.CheckIndex != index || raw.WorkerIndex != index || e.AssignmentRecord == nil || raw.Assignment == nil || *raw.Assignment != *e.AssignmentRecord || raw.AssignmentSHA != e.AssignmentRecordSHA256 || raw.WorkerSnapshot["runtime_id"] != e.RuntimeID || raw.WorkerSnapshot["baseline_generation"] != float64(e.BaselineGeneration) || raw.WorkerSnapshot["assignment_id"] != e.AssignmentID || raw.WorkerSnapshot["owner_agent_id"] != e.AgentID || raw.WorkerSnapshot["execution_generation"] != float64(e.Generation) || raw.WorkerSnapshot["bootstrap_sha256"] != e.BootstrapSHA256 {
		return out, fmt.Errorf("owned receipt differs from the frozen workspace execution tuple")
	}
	return out, nil
}

// AcceptOwnedCheckReceipt appends a current receipt without evicting history.
// The fixed index namespace belongs to WorkerChecks and never Main checks.
func (b *ExecutionRegistry) AcceptOwnedCheckReceipt(e *Execution, index int, ref checkexec.ToolReceipt) error {
	if e == nil || index < 0 || index >= len(e.WorkerChecks) {
		return fmt.Errorf("select a frozen WorkerCheck receipt index")
	}
	if _, err := VerifyOwnedCheckReceipt(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), *e, index, ref); err != nil {
		return err
	}
	for _, previous := range e.OwnedCheckReceipts {
		if previous.Receipt.SHA256 == ref.SHA256 {
			if previous.WorkerCheckIndex != index || previous.Receipt != ref {
				return fmt.Errorf("owned receipt digest is already registered under a different identity")
			}
			return nil
		}
	}
	if len(e.OwnedCheckReceipts) >= 1024 {
		return fmt.Errorf("owned-check receipt index is full; preserve its history before another run")
	}
	e.OwnedCheckReceipts = append(e.OwnedCheckReceipts, OwnedCheckReceipt{WorkerCheckIndex: index, Receipt: ref})
	return nil
}

// ObserveCopiedOwnedCheck uses the same copy/profile/permission observation as
// the actual runner; cache decisions remain diagnostics, not acceptance.
func (b *ExecutionRegistry) observeCopiedOwnedCheck(ctx context.Context, e Execution, index int) (checkexec.Observation, error) {
	if index < 0 || index >= len(e.WorkerChecks) {
		return checkexec.Observation{}, fmt.Errorf("select a frozen WorkerCheck index")
	}
	check := e.WorkerChecks[index]
	scratch, err := os.MkdirTemp("", "loop-owned-check-observe-")
	if err != nil {
		return checkexec.Observation{}, err
	}
	defer os.RemoveAll(scratch)
	if _, err := copyCheckTree(ctx, e.Path, scratch); err != nil {
		return checkexec.Observation{}, err
	}
	ctx = checkexec.WithProfile(ctx, check.Profile)
	observation, err := checkexec.ObserveProfile(ctx, scratch, check.Profile)
	if err != nil {
		return observation, err
	}
	permissions := b.observeCheckPermissions(ctx, e, scratch)
	observation.Permissions = &permissions
	observation.PermissionDigest = permissions.Digest()
	return observation, nil
}
