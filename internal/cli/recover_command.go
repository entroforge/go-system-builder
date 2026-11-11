package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const (
	recoveryMalformedCode      = "LOOP_RUNTIME_MALFORMED"
	recoveryREQInvalidCode     = "LOOP_RECOVERY_REQ_INVALID"
	recoveryInputDriftCode     = "LOOP_RECOVERY_INPUT_DRIFT"
	recoverySourceConflictCode = "LOOP_RECOVERY_SOURCE_CONFLICT"
	recoveryGateUnknownCode    = "LOOP_RECOVERY_GATE_UNKNOWN"
	recoveryPlanInvalidCode    = "LOOP_RECOVERY_PLAN_INVALID"
	recoveryApplyPendingCode   = "LOOP_RECOVERY_APPLY_PENDING"
	recoveryAlreadyApplied     = "LOOP_RECOVERY_ALREADY_APPLIED"
	recoveryImportedBugID      = "recovery-import"
)

var (
	// These sentinels classify CLI-only recovery conditions that are not
	// currently exposed by the lower-level packages as typed errors.
	errRecoveryPlanInvalid    = errors.New("recovery plan is invalid")
	errRecoveryGateUnknown    = errors.New("recovery replay encountered an unknown gate")
	errRecoverySourceConflict = errors.New("recovery sources conflict")
)

// recoveryErrorCode translates the wrapped error chain into the stable CLI
// contract. Classification deliberately uses errors.Is/As; callers must not
// parse error strings because lower layers add diagnostic context over time.
func recoveryErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errRecoveryPlanInvalid) {
		return recoveryPlanInvalidCode
	}
	if errors.Is(err, errRecoveryGateUnknown) {
		return recoveryGateUnknownCode
	}
	if errors.Is(err, errRecoverySourceConflict) {
		return recoverySourceConflictCode
	}

	var validation *recovery.ValidationError
	if errors.As(err, &validation) {
		switch {
		case errors.Is(validation, recovery.ErrInvalidREQ),
			errors.Is(validation, recovery.ErrREQFilename),
			errors.Is(validation, recovery.ErrREQStatusMissing),
			errors.Is(validation, recovery.ErrREQNotLocked),
			errors.Is(validation, recovery.ErrREQVersionMissing),
			errors.Is(validation, recovery.ErrPathOutsideRepository):
			return recoveryREQInvalidCode
		case errors.Is(validation, recovery.ErrInvalidInventory):
			return recoveryPlanInvalidCode
		}
	}

	switch {
	case errors.Is(err, runtime.ErrRecoveryInputDrift):
		return recoveryInputDriftCode
	case errors.Is(err, runtime.ErrRecoveryConflict):
		return recoverySourceConflictCode
	case errors.Is(err, runtime.ErrRecoveryPending):
		return recoveryApplyPendingCode
	case errors.Is(err, recovery.ErrInvalidREQ),
		errors.Is(err, recovery.ErrREQFilename),
		errors.Is(err, recovery.ErrREQStatusMissing),
		errors.Is(err, recovery.ErrREQNotLocked),
		errors.Is(err, recovery.ErrREQVersionMissing),
		errors.Is(err, recovery.ErrPathOutsideRepository):
		return recoveryREQInvalidCode
	case errors.Is(err, recovery.ErrInvalidInventory), errors.Is(err, runtime.ErrRecoveryCandidateInvalid):
		return recoveryPlanInvalidCode
	default:
		return ""
	}
}

func formatRecoveryFailure(cmd string, err error) string {
	if code := recoveryErrorCode(err); code != "" {
		return fmt.Sprintf("%s: %s: %v", cmd, code, err)
	}
	return formatFailure(cmd, err)
}

func invalidRecoveryPlan(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", errRecoveryPlanInvalid, err)
}

func recoveryReplayStopError(result controller.RecoveryReplayResult) error {
	switch result.StopReason {
	case controller.ReplayStopUnknown:
		return fmt.Errorf("%w: replay stopped at an unknown gate", errRecoveryGateUnknown)
	case controller.ReplayStopConflict:
		return fmt.Errorf("%w: replay stopped on conflicting trusted sources", errRecoverySourceConflict)
	default:
		return nil
	}
}

type recoveryPlanDocument struct {
	recovery.Plan
	PlanID                string                           `json:"plan_id"`
	Status                string                           `json:"status"`
	CreatedAt             string                           `json:"created_at"`
	CandidateStatePath    string                           `json:"candidate_state_path"`
	CandidateJournalPath  string                           `json:"candidate_journal_path"`
	ImportFindings        []recovery.ImportFinding         `json:"import_findings,omitempty"`
	ReplayTrace           []controller.RecoveryReplayTrace `json:"replay_trace,omitempty"`
	CheckpointPath        string                           `json:"checkpoint_path,omitempty"`
	CandidatePacketBodies map[string]string                `json:"candidate_packet_bodies,omitempty"`
	ReplayStopReason      controller.ReplayStopReason      `json:"replay_stop_reason,omitempty"`
}

type recoveryActiveRuntime struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Readable  bool   `json:"readable"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

type recoveryInspectOutput struct {
	SchemaVersion    string                    `json:"schema_version"`
	REQ              recovery.REQBinding       `json:"req"`
	Inputs           []recovery.InventoryInput `json:"inputs"`
	ActiveRuntime    recoveryActiveRuntime     `json:"active_runtime"`
	LatestCheckpoint string                    `json:"latest_checkpoint,omitempty"`
}

func runRuntimeRecover(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime recover requires <inspect|plan|apply>")
		return 2
	}
	switch args[0] {
	case "inspect":
		return runRuntimeRecoverInspect(args[1:], stdout, stderr)
	case "plan":
		return runRuntimeRecoverPlan(args[1:], stdout, stderr)
	case "apply":
		return runRuntimeRecoverApply(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown runtime recover command %q\n", args[0])
		return 2
	}
}

func runRuntimeRecoverApply(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime recover apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime recover apply")
	rootFlag := flags.String("root", ".", "repository root")
	planFlag := flags.String("plan", "", "approved recovery plan path")
	approvedBy := flags.String("approved-by", "", "human recovery approver identity")
	if wantsHelp(args) {
		flags.SetOutput(stdout)
		flags.Usage()
		return 0
	}
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	if strings.TrimSpace(*planFlag) == "" || strings.TrimSpace(*approvedBy) == "" {
		fmt.Fprintln(stderr, "runtime recover apply requires --plan and --approved-by")
		return 2
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime recover apply", fmt.Errorf("resolve root: %w", err)))
		return 1
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime recover apply", fmt.Errorf("resolve root symlinks: %w", err)))
		return 1
	}
	locked, release, err := integration.LockWorkspace(context.Background(), root)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	defer release()
	var checkpointSource string
	guard, releaseSources := checkpointRecoveryGuard(locked, root, &checkpointSource)
	defer releaseSources()
	// Once the durable marker exists it is the authoritative crash-recovery
	// source. Probe/resume it under the Runtime lock before any mutable plan
	// file or repository input is read.
	if result, resumeErr := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:            root,
		StatePath:       filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath:     filepath.Join(root, ".claude", "loop-events.jsonl"),
		Approver:        strings.TrimSpace(*approvedBy),
		OccurredAt:      time.Now().UTC(),
		Validator:       semantic.RuntimeCandidateValidator{},
		ValidateRestore: guard,
	}); resumeErr == nil {
		return encodeRecoveryApplyResult(stdout, result)
	} else if !errors.Is(resumeErr, runtime.ErrRecoveryNoPending) {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", resumeErr))
		return 1
	}
	// A coherent Store commit/fingerprint/rollover marker contains a more exact
	// source than artifact reconstruction. Complete it first; an unresolved
	// marker must remain in place rather than guessing or retiring it.
	writer := runtime.NewWriter(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		root,
		semantic.RuntimeCandidateValidator{},
	)
	completed, pendingErr := writer.RecoverPendingOperations()
	if pendingErr != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", fmt.Errorf("original pending Runtime operation is unresolved; preserve the existing files: %w", pendingErr)))
		return 1
	}
	if completed {
		return encodeJSON(stdout, struct {
			Status string `json:"status"`
		}{Status: "runtime_pending_completed"})
	}
	planPath, err := resolveRecoveryPathAllowMissing(root, *planFlag)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(err)))
		return 1
	}
	document, err := readRecoveryPlan(planPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return resumePendingRecovery(root, strings.TrimSpace(*approvedBy), guard, stdout, stderr)
		}
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	if err := validateRecoveryPlanDocument(document); err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	// A completed original Writer event is authoritative even after later
	// Runtime progress has advanced the latest checkpoint.
	if result, replayErr := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root: root, StatePath: filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath: filepath.Join(root, ".claude", "loop-events.jsonl"),
		PlanID:      document.PlanID, Approver: strings.TrimSpace(*approvedBy),
		OccurredAt: time.Now().UTC(), Validator: semantic.RuntimeCandidateValidator{},
	}); replayErr == nil {
		return encodeRecoveryApplyResult(stdout, result)
	} else if !errors.Is(replayErr, runtime.ErrRecoveryNoPending) {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", replayErr))
		return 1
	}
	// The reviewed plan is only valid for the material that actually exists.
	// A plan whose inputs were removed, changed or extended after the review
	// supersedes the plan instead of restoring a stale decision.
	if err := verifyRecoveryPlanInputs(root, document); err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	checkpointSource = document.CheckpointPath
	if err := verifyCheckpointPlanSource(root, document); err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	candidateStatePath, err := resolveRecoveryPathAllowMissing(root, document.CandidateStatePath)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(err)))
		return 1
	}
	candidateJournalPath, err := resolveRecoveryPathAllowMissing(root, document.CandidateJournalPath)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(err)))
		return 1
	}
	candidateState, err := os.ReadFile(candidateStatePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(fmt.Errorf("read candidate state: %w", err))))
		return 1
	}
	candidateJournal, err := os.ReadFile(candidateJournalPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(fmt.Errorf("read candidate journal: %w", err))))
		return 1
	}
	if document.CheckpointPath != "" && len(candidateState) > 0 {
		candidateState, err = restoreRecoveryCandidatePacketBodies(locked, root, document, candidateState)
		if err != nil {
			fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", invalidRecoveryPlan(err)))
			return 1
		}
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	result, err := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root: root, StatePath: statePath, JournalPath: journalPath,
		PlanID: document.PlanID, CandidateState: candidateState, CandidateJournal: candidateJournal,
		ValidateRestore: guard, Approver: *approvedBy, OccurredAt: time.Now().UTC(),
		Validator: semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	return encodeRecoveryApplyResult(stdout, result)
}

// verifyRecoveryPlanInputs compares the plan's recorded inputs with the
// repository's actual saved material. A plan is stale when a planned input is
// no longer saved, when a new input appeared after the plan, or when a planned
// input changed; the REQ binding is re-checked as well. No revision, digest or
// fingerprint is compared: the plan records what was actually saved, not a
// hash of it.
func verifyRecoveryPlanInputs(root string, document recoveryPlanDocument) error {
	inventory, err := recovery.Inspect(root, document.REQ.Path)
	if err != nil {
		return fmt.Errorf("%w: recovery plan inputs cannot be inspected: %v", runtime.ErrRecoveryInputDrift, err)
	}
	if document.REQ.ID != inventory.REQ.ID || document.REQ.Path != inventory.REQ.Path || document.REQ.Status != inventory.REQ.Status || document.REQ.Version != inventory.REQ.Version {
		return fmt.Errorf("%w: recovery plan REQ binding differs from the selected locked source", runtime.ErrRecoveryInputDrift)
	}
	current := make(map[string]recovery.InventoryInput, len(inventory.Inputs))
	for _, input := range inventory.Inputs {
		current[input.Path] = input
	}
	planned := make(map[string]recovery.InventoryInput, len(document.Inputs))
	for _, input := range document.Inputs {
		if input.Kind == recovery.InputKindCheckpoint {
			// Checkpoint directories are pruned by the Runtime; they are not
			// part of the repository's saved material.
			continue
		}
		planned[input.Path] = input
	}
	for path, input := range planned {
		actual, ok := current[path]
		if !ok {
			return fmt.Errorf("%w: planned recovery input %q is no longer saved", runtime.ErrRecoveryInputDrift, path)
		}
		if actual.Kind != input.Kind || actual.Size != input.Size {
			return fmt.Errorf("%w: planned recovery input %q changed", runtime.ErrRecoveryInputDrift, path)
		}
	}
	for path := range current {
		if _, ok := planned[path]; !ok {
			return fmt.Errorf("%w: new recovery input %q appeared after the plan", runtime.ErrRecoveryInputDrift, path)
		}
	}
	return nil
}

func resumePendingRecovery(root, approvedBy string, guard func(map[string]any, []byte) error, stdout, stderr io.Writer) int {
	result, err := runtime.ApplyRecovery(runtime.RecoveryRequest{
		ValidateRestore: guard,
		Root:            root,
		StatePath:       filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath:     filepath.Join(root, ".claude", "loop-events.jsonl"),
		Approver:        approvedBy,
		OccurredAt:      time.Now().UTC(),
		Validator:       semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	return encodeRecoveryApplyResult(stdout, result)
}

func encodeRecoveryApplyResult(stdout io.Writer, result runtime.RecoveryResult) int {
	response := struct {
		Status                  string                     `json:"status"`
		PlanID                  string                     `json:"plan_id"`
		ManifestPath            string                     `json:"manifest_path"`
		QuarantineDir           string                     `json:"quarantine_dir"`
		PreservedUnknownSources []runtime.RecoveryArtifact `json:"preserved_unknown_sources,omitempty"`
	}{
		Status:                  "applied",
		PlanID:                  result.Manifest.PlanID,
		ManifestPath:            filepath.ToSlash(result.ManifestPath),
		QuarantineDir:           filepath.ToSlash(result.QuarantineDir),
		PreservedUnknownSources: result.Manifest.UncertainSources,
	}
	if result.Idempotent {
		response.Status = recoveryAlreadyApplied
	}
	return encodeJSON(stdout, response)
}

func validateRecoveryPlanDocument(document recoveryPlanDocument) error {
	inventory := recovery.Inventory{
		SchemaVersion: document.SchemaVersion,
		REQ:           document.REQ,
		Inputs:        document.Inputs,
	}
	rebuilt, err := recovery.BuildPlanForCursor(inventory, document.TargetCursor, document.Confidence)
	if err != nil {
		return invalidRecoveryPlan(fmt.Errorf("validate recovery plan inventory: %w", err))
	}
	if rebuilt.BaseMode != document.BaseMode || rebuilt.TargetCursor != document.TargetCursor || rebuilt.Confidence != document.Confidence {
		return invalidRecoveryPlan(fmt.Errorf("recovery base plan does not match its original responsibilities"))
	}
	if strings.TrimSpace(document.PlanID) == "" {
		return invalidRecoveryPlan(fmt.Errorf("recovery plan identity is missing"))
	}
	if document.Status != "planned" || document.CandidateStatePath == "" || document.CandidateJournalPath == "" {
		return invalidRecoveryPlan(fmt.Errorf("recovery plan is incomplete"))
	}
	return nil
}

func resolveRecoveryPath(root, path string) (string, error) {
	resolved, err := resolveRecoveryPathAllowMissing(root, path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(resolved); err != nil {
		return "", fmt.Errorf("resolve recovery path %q: %w", path, err)
	}
	return resolved, nil
}

func resolveRecoveryPathAllowMissing(root, path string) (string, error) {
	evaluatedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, filepath.FromSlash(path))
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve recovery path %q: %w", path, err)
	}
	probe := resolved
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect recovery path %q: %w", path, statErr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("resolve recovery path %q: no existing ancestor", path)
		}
		probe = parent
	}
	evaluatedProbe, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", fmt.Errorf("resolve recovery path symlinks %q: %w", path, err)
	}
	suffix, err := filepath.Rel(probe, resolved)
	if err != nil || suffix == ".." || strings.HasPrefix(suffix, ".."+string(filepath.Separator)) || filepath.IsAbs(suffix) {
		return "", fmt.Errorf("recovery path %q cannot be resolved from its existing ancestor", path)
	}
	evaluated := filepath.Clean(filepath.Join(evaluatedProbe, suffix))
	relative, err := filepath.Rel(evaluatedRoot, evaluated)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("recovery path %q escapes repository", path)
	}
	return evaluated, nil
}

func recoveryInputExists(inputs []recovery.InventoryInput, kind, path string) bool {
	for _, input := range inputs {
		if input.Kind == kind && input.Path == path {
			return true
		}
	}
	return false
}

func newRecoveryPlanID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "rr-" + hex.EncodeToString(value[:]), nil
}

type recoverySourceCopy struct {
	path string
	data []byte
}

func captureRecoverySources(root string, inputs []recovery.InventoryInput) ([]recoverySourceCopy, error) {
	var sources []recoverySourceCopy
	for _, input := range inputs {
		if input.Kind != recovery.InputKindRuntimeState && input.Kind != recovery.InputKindRuntimeJournal && input.Kind != recovery.InputKindRuntimePending && input.Kind != recovery.InputKindRuntimeSegment {
			continue
		}
		if input.Path == ".claude/loop-state.json.recovery-pending.json" {
			continue
		}
		path, err := resolveRecoveryPath(root, input.Path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, recoverySourceCopy{path: input.Path, data: data})
	}
	return sources, nil
}

func runRuntimeRecoverPlan(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime recover plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime recover plan")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "explicit locked REQ path")
	dev := flags.String("dev-branch", "", "explicit REQ development branch")
	upstream := flags.String("release-upstream", "", "explicit release destination")
	checkpoint := flags.String("checkpoint", "", "explicit latest complete Runtime checkpoint manifest")
	if wantsHelp(args) {
		flags.SetOutput(stdout)
		flags.Usage()
		return 0
	}
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reqPath) == "" {
		fmt.Fprintln(stderr, "runtime recover plan requires --req")
		return 2
	}
	inventory, err := recovery.Inspect(*root, *reqPath)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover plan", err))
		return 1
	}
	binding, err := workspace.Bind(*root, *dev, *upstream)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	inventory.REQ.Workspace = &binding

	var document recoveryPlanDocument
	var planPath string
	if strings.TrimSpace(*checkpoint) != "" {
		document, planPath, err = persistCheckpointRecoveryPlan(inventory.Root, inventory, *checkpoint)
	} else {
		document, planPath, err = persistRecoveryPlan(inventory.Root, inventory)
	}
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover plan", err))
		return 1
	}
	response := struct {
		RecoveryPlan string               `json:"recovery_plan"`
		Plan         recoveryPlanDocument `json:"plan"`
	}{RecoveryPlan: planPath, Plan: document}
	return encodeJSON(stdout, response)
}

func persistRecoveryPlan(root string, inventory recovery.Inventory) (recoveryPlanDocument, string, error) {
	planID, err := newRecoveryPlanID()
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	sources, err := captureRecoverySources(root, inventory.Inputs)
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	basePlan, err := recovery.BuildPlan(inventory)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("build conservative recovery plan: %w", err)
	}
	createdAt := time.Now().UTC()
	state, err := recoverySeedRuntime(root, inventory, createdAt, sources)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("build recovery format: %w", err)
	}
	recoveryRoot := filepath.Join(root, ".claude", "recovery")
	if err := os.MkdirAll(recoveryRoot, 0o755); err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("create recovery root: %w", err)
	}
	temporaryDir, err := os.MkdirTemp(recoveryRoot, ".building-recovery-")
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("create recovery staging directory: %w", err)
	}
	defer os.RemoveAll(temporaryDir)

	statePath := filepath.Join(temporaryDir, "candidate-state.json")
	journalPath := filepath.Join(temporaryDir, "candidate-events.jsonl")
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("encode recovery seed runtime: %w", err)
	}
	if err := os.WriteFile(statePath, append(stateData, '\n'), 0o600); err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("write recovery seed runtime: %w", err)
	}
	if err := os.WriteFile(journalPath, nil, 0o600); err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("write recovery seed journal: %w", err)
	}
	if inventory.REQ.Workspace == nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("explicit recovery workspace binding required")
	}
	files, err := fileview.New(root, "refs/heads/"+inventory.REQ.Workspace.DevBranch, []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}

	_, err = transition.Apply(root, statePath, journalPath, transition.Request{
		Files:        files,
		TransitionID: "TR-001",
		// This is an explicit concrete assertion, not an omission: the
		// recovery seed runtime was just built by this planner and is still at
		// revision 0, and TR-001 is its first commit. A non-negative value is a
		// specific assertion (L4-revision-usage.md:78), so it is compared as
		// such. The number is produced by the system that owns the fresh seed
		// — no author supplies or guesses it, and an unrelated commit is not
		// possible inside this private staging directory.
		ExpectedRevision: 0,
		Actor:            "orchestrator",
		Evidence: map[string]string{
			"req_lock_record":           basePlan.REQ.Path,
			"loop_authorization_record": "recovery-plan:" + planID,
		},
		REQ: &transition.LockedREQ{
			Workspace:  inventory.REQ.Workspace,
			ID:         basePlan.REQ.ID,
			Path:       basePlan.REQ.Path,
			Version:    basePlan.REQ.Version,
			ApprovedBy: "recovery-plan",
			ApprovedAt: createdAt.Format(time.RFC3339Nano),
		},
		OccurredAt:     createdAt,
		RecoveryWriter: runtime.NewOfflineRecoveryCapability(),
	})
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("bind recovery seed runtime: %w", err)
	}
	imported, err := recovery.Import(root, inventory)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("import recovery artifacts: %w", err)
	}
	if err := mergeRecoveryProjection(root, statePath, journalPath, imported, planID, createdAt); err != nil {
		return recoveryPlanDocument{}, "", err
	}
	replay, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
		MaxSteps:    controller.DefaultRecoveryReplayMaxSteps,
	})
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("replay recovery staging runtime: %w", err)
	}
	if err := recoveryReplayStopError(replay); err != nil {
		return recoveryPlanDocument{}, "", err
	}
	if replay.FinalCursor != "" && replay.FinalCursor != recovery.PlanSeedCursor {
		basePlan, err = recovery.BuildPlanForCursor(inventory, replay.FinalCursor, recovery.PlanConfidenceFormalReplay)
		if err != nil {
			return recoveryPlanDocument{}, "", fmt.Errorf("prepare replayed recovery cursor: %w", err)
		}
	}
	planDir := filepath.Join(recoveryRoot, planID)
	planPath := filepath.Join(planDir, "plan.json")
	if existing, readErr := readRecoveryPlan(planPath); readErr == nil {
		return existing, filepath.ToSlash(planPath), nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return recoveryPlanDocument{}, "", fmt.Errorf("read existing recovery plan: %w", readErr)
	}
	document := recoveryPlanDocument{
		Plan:                 basePlan,
		PlanID:               planID,
		Status:               "planned",
		CreatedAt:            createdAt.Format(time.RFC3339Nano),
		CandidateStatePath:   filepath.ToSlash(filepath.Join(".claude", "recovery", planID, "candidate-state.json")),
		CandidateJournalPath: filepath.ToSlash(filepath.Join(".claude", "recovery", planID, "candidate-events.jsonl")),
		ImportFindings:       imported.Findings,
		ReplayTrace:          replay.Trace,
		ReplayStopReason:     replay.StopReason,
	}
	planData, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("encode recovery plan: %w", err)
	}
	if err := os.WriteFile(filepath.Join(temporaryDir, "plan.json"), append(planData, '\n'), 0o600); err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("write recovery plan: %w", err)
	}
	if err := os.Rename(temporaryDir, planDir); err != nil {
		if existing, readErr := readRecoveryPlan(planPath); readErr == nil {
			return existing, filepath.ToSlash(planPath), nil
		}
		return recoveryPlanDocument{}, "", fmt.Errorf("publish recovery plan: %w", err)
	}
	return document, filepath.ToSlash(planPath), nil
}

func mergeRecoveryProjection(root, statePath, journalPath string, imported recovery.ImportResult, planID string, occurredAt time.Time) error {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("read recovery staging state for import: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode recovery staging state for import: %w", err)
	}
	revision, err := recoveryInteger(state["revision"])
	if err != nil {
		return fmt.Errorf("read recovery staging revision: %w", err)
	}
	runtimeID, _ := state["runtime_id"].(string)
	lifecycle, _ := state["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	evidenceIDs := make([]string, 0, len(imported.Evidence))
	for _, item := range imported.Evidence {
		if id, _ := item["id"].(string); id != "" {
			evidenceIDs = append(evidenceIDs, id)
		}
	}
	eventSuffix := planID
	if eventSuffix == "" {
		return errors.New("recovery projection plan identity is required")
	}
	writer := runtime.NewOfflineRecoveryWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}, runtime.NewOfflineRecoveryCapability())
	_, err = writer.Update(revision, runtime.Mutation{
		EventID:              "evt-recovery-import-" + eventSuffix,
		Event:                "recovery_projection_imported",
		Actor:                "orchestrator",
		IdempotencyKey:       "recovery-import:" + planID,
		RuntimeID:            runtimeID,
		From:                 cursor,
		To:                   cursor,
		EvidenceIDs:          evidenceIDs,
		JournalEvent:         "milestone_refreshed",
		JournalOutcome:       "refreshed",
		Message:              "Recovery projection imported from original available artifacts.",
		RequestID:            "runtime-recovery:" + eventSuffix,
		BaselineGeneration:   recoveryBaselineGeneration(state),
		RetainLastTransition: true,
		OccurredAt:           occurredAt,
		Apply: func(candidate map[string]any) error {
			mergeRecoveryProjectionState(candidate, root, imported)
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("commit imported recovery staging projection: %w", err)
	}
	return nil
}

func mergeRecoveryProjectionState(state map[string]any, root string, imported recovery.ImportResult) {
	state["root"] = root
	state["documents"] = recoveryMapsAsAny(imported.Documents)
	state["evidence"] = recoveryMapsAsAny(imported.Evidence)
	// Durable task and BUG records can be reconstructed from trusted imported
	// artifacts without reviving any transient lease or activation state.
	// Agents and teams intentionally start empty because their live ownership,
	// activation, and lease state cannot be reconstructed from documents alone.
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  recoveryMapsAsAny(imported.Entities["tasks"]),
		"bugs":   recoveryBugEntities(imported.Entities["bugs"]),
		"teams":  []any{},
	}
}

func recoveryInteger(value any) (int, error) {
	switch typed := value.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("value %T is not an integer", value)
	}
}

func recoveryBaselineGeneration(state map[string]any) int {
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := recoveryInteger(baseline["generation"])
	if err != nil || generation < 1 {
		return 1
	}
	return generation
}

func recoveryMapsAsAny(values []map[string]any) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func recoveryBugEntities(values []map[string]any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		copyValue := make(map[string]any, len(value)+1)
		for key, item := range value {
			copyValue[key] = item
		}
		// Import can prove that a BUG document is durable without proving the
		// identity of its original live finder. Keep the entity and mark the
		// provenance as recovery-import so the schema remains valid, without
		// creating an Agent or reviving a lease/activation.
		finders, ok := copyValue["original_finder_agent_ids"].([]any)
		if !ok || len(finders) == 0 {
			copyValue["original_finder_agent_ids"] = []any{recoveryImportedBugID}
		}
		result = append(result, copyValue)
	}
	return result
}

func readRecoveryPlan(path string) (recoveryPlanDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return recoveryPlanDocument{}, invalidRecoveryPlan(fmt.Errorf("read recovery plan: %w", err))
	}
	var document recoveryPlanDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return recoveryPlanDocument{}, invalidRecoveryPlan(fmt.Errorf("decode recovery plan: %w", err))
	}
	return document, nil
}

func runRuntimeRecoverInspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime recover inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime recover inspect")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "explicit locked REQ path")
	if wantsHelp(args) {
		flags.SetOutput(stdout)
		flags.Usage()
		return 0
	}
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reqPath) == "" {
		fmt.Fprintln(stderr, "runtime recover inspect requires --req")
		return 2
	}
	inventory, err := recovery.Inspect(*root, *reqPath)
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover inspect", err))
		return 1
	}
	output := recoveryInspectOutput{
		SchemaVersion: inventory.SchemaVersion,
		REQ:           inventory.REQ,
		Inputs:        inventory.Inputs,
		ActiveRuntime: inspectActiveRuntime(inventory),
	}
	// Location only: plan still verifies the complete source and current facts.
	output.LatestCheckpoint, _ = runtime.LatestRuntimeCheckpoint(inventory.Root)
	if err := encodeJSON(stdout, output); err != 0 {
		return err
	}
	return 0
}

func inspectActiveRuntime(inventory recovery.Inventory) recoveryActiveRuntime {
	result := recoveryActiveRuntime{
		Path:     ".claude/loop-state.json",
		Status:   "missing",
		Readable: false,
	}
	for _, input := range inventory.Inputs {
		if input.Kind != recovery.InputKindRuntimeState || input.Path != result.Path {
			continue
		}
		result.Exists = true
		result.Status = "readable"
		data, err := os.ReadFile(filepath.Join(inventory.Root, filepath.FromSlash(input.Path)))
		if err == nil && !bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) && json.Valid(data) {
			result.Readable = true
			return result
		}
		result.Status = "malformed"
		result.ErrorCode = recoveryMalformedCode
		return result
	}
	return result
}
