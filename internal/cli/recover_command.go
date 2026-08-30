package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
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
	PlanID                 string                           `json:"plan_id"`
	Status                 string                           `json:"status"`
	CreatedAt              string                           `json:"created_at"`
	CandidateStatePath     string                           `json:"candidate_state_path"`
	CandidateJournalPath   string                           `json:"candidate_journal_path"`
	CandidateStateSHA256   string                           `json:"candidate_state_sha256"`
	CandidateJournalSHA256 string                           `json:"candidate_journal_sha256"`
	DocumentSHA256         string                           `json:"document_sha256"`
	ImportFindings         []recovery.ImportFinding         `json:"import_findings,omitempty"`
	ReplayTrace            []controller.RecoveryReplayTrace `json:"replay_trace,omitempty"`
	ReplayStopReason       controller.ReplayStopReason      `json:"replay_stop_reason,omitempty"`
}

type recoveryActiveRuntime struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Readable  bool   `json:"readable"`
	SHA256    string `json:"sha256,omitempty"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

type recoveryInspectOutput struct {
	SchemaVersion string                    `json:"schema_version"`
	REQ           recovery.REQBinding       `json:"req"`
	Inputs        []recovery.InventoryInput `json:"inputs"`
	ActiveRuntime recoveryActiveRuntime     `json:"active_runtime"`
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
	if err := flags.Parse(args); err != nil {
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
	// Once the durable marker exists it is the authoritative crash-recovery
	// source. Probe/resume it under the Runtime lock before any mutable plan
	// file or repository input is read.
	if result, resumeErr := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:        root,
		StatePath:   filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath: filepath.Join(root, ".claude", "loop-events.jsonl"),
		Approver:    strings.TrimSpace(*approvedBy),
		OccurredAt:  time.Now().UTC(),
		Validator:   semantic.RuntimeCandidateValidator{},
	}); resumeErr == nil {
		return encodeRecoveryApplyResult(stdout, result)
	} else if !errors.Is(resumeErr, runtime.ErrRecoveryNoPending) {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", resumeErr))
		return 1
	}
	// A coherent Store commit/fingerprint/rollover marker contains a more exact
	// source than artifact reconstruction. Complete it first; an invalid marker
	// falls through so the approved recovery plan can quarantine it.
	writer := runtime.NewWriter(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		root,
		semantic.RuntimeCandidateValidator{},
	)
	if completed, pendingErr := writer.RecoverPendingOperations(); pendingErr == nil && completed {
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
			return resumePendingRecovery(root, strings.TrimSpace(*approvedBy), stdout, stderr)
		}
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	if err := validateRecoveryPlanDocument(document); err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	if err := verifyRecoveryStableInputs(root, document); err != nil {
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
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	sourceStateExists := recoveryInputExists(document.Inputs, recovery.InputKindRuntimeState, ".claude/loop-state.json")
	sourceJournalExists := recoveryInputExists(document.Inputs, recovery.InputKindRuntimeJournal, ".claude/loop-events.jsonl")
	sourcePendingSHA256 := recoveryPendingInputHashes(root, document.Inputs)
	result, err := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:                   root,
		StatePath:              statePath,
		JournalPath:            journalPath,
		PlanID:                 document.PlanID,
		PlanSHA:                document.DocumentSHA256,
		CandidateState:         candidateState,
		CandidateJournal:       candidateJournal,
		CandidateStateSHA256:   document.CandidateStateSHA256,
		CandidateJournalSHA256: document.CandidateJournalSHA256,
		SourceStateSHA256:      recoveryInputHash(document.Inputs, recovery.InputKindRuntimeState, ".claude/loop-state.json"),
		SourceJournalSHA256:    recoveryInputHash(document.Inputs, recovery.InputKindRuntimeJournal, ".claude/loop-events.jsonl"),
		SourceStateExists:      &sourceStateExists,
		SourceJournalExists:    &sourceJournalExists,
		SourcePendingSHA256:    sourcePendingSHA256,
		Approver:               *approvedBy,
		OccurredAt:             time.Now().UTC(),
		Validator:              semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	return encodeRecoveryApplyResult(stdout, result)
}

func resumePendingRecovery(root, approvedBy string, stdout, stderr io.Writer) int {
	result, err := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:        root,
		StatePath:   filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath: filepath.Join(root, ".claude", "loop-events.jsonl"),
		Approver:    approvedBy,
		OccurredAt:  time.Now().UTC(),
		Validator:   semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatRecoveryFailure("runtime recover apply", err))
		return 1
	}
	return encodeRecoveryApplyResult(stdout, result)
}

func encodeRecoveryApplyResult(stdout io.Writer, result runtime.RecoveryResult) int {
	response := struct {
		Status        string `json:"status"`
		PlanID        string `json:"plan_id"`
		ManifestPath  string `json:"manifest_path"`
		QuarantineDir string `json:"quarantine_dir"`
	}{
		Status:        "applied",
		PlanID:        result.Manifest.PlanID,
		ManifestPath:  filepath.ToSlash(result.ManifestPath),
		QuarantineDir: filepath.ToSlash(result.QuarantineDir),
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
	if rebuilt.PlanSHA256 != document.PlanSHA256 || rebuilt.BaseMode != document.BaseMode || rebuilt.TargetCursor != document.TargetCursor || rebuilt.Confidence != document.Confidence {
		return invalidRecoveryPlan(fmt.Errorf("recovery base plan fingerprint mismatch"))
	}
	if len(document.PlanSHA256) < 16 || document.PlanID != "rr-"+document.PlanSHA256[:16] {
		return invalidRecoveryPlan(fmt.Errorf("recovery plan identity mismatch"))
	}
	if document.Status != "planned" || document.CandidateStatePath == "" || document.CandidateJournalPath == "" {
		return invalidRecoveryPlan(fmt.Errorf("recovery plan is incomplete"))
	}
	return nil
}

func verifyRecoveryStableInputs(root string, document recoveryPlanDocument) error {
	current, err := recovery.Inspect(root, document.REQ.Path)
	if err != nil {
		return fmt.Errorf("re-inspect recovery inputs: %w", err)
	}
	expectedInputs := stableRecoveryInputHashes(document.Inputs)
	currentInputs := stableRecoveryInputHashes(current.Inputs)
	if len(expectedInputs) != len(currentInputs) {
		return fmt.Errorf("%w: recovery input set changed: expected %d stable inputs, got %d", runtime.ErrRecoveryInputDrift, len(expectedInputs), len(currentInputs))
	}
	for path, expected := range expectedInputs {
		actual, exists := currentInputs[path]
		if !exists {
			return fmt.Errorf("%w: input %s is missing", runtime.ErrRecoveryInputDrift, path)
		}
		if actual != expected {
			return fmt.Errorf("%w: input %s expected %s, got %s", runtime.ErrRecoveryInputDrift, path, expected, actual)
		}
	}
	return nil
}

func stableRecoveryInputHashes(inputs []recovery.InventoryInput) map[string]string {
	result := make(map[string]string, len(inputs))
	for _, input := range inputs {
		if input.Kind == recovery.InputKindRuntimeState || input.Kind == recovery.InputKindRuntimeJournal || input.Kind == recovery.InputKindRuntimePending {
			continue
		}
		result[input.Path] = input.SHA256
	}
	return result
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

func recoveryInputHash(inputs []recovery.InventoryInput, kind, path string) string {
	for _, input := range inputs {
		if input.Kind == kind && input.Path == path {
			return input.SHA256
		}
	}
	return ""
}

func recoveryInputExists(inputs []recovery.InventoryInput, kind, path string) bool {
	return recoveryInputHash(inputs, kind, path) != ""
}

func recoveryPendingInputHashes(root string, inputs []recovery.InventoryInput) map[string]string {
	result := make(map[string]string)
	for _, input := range inputs {
		if input.Kind != recovery.InputKindRuntimePending || input.Path == ".claude/loop-state.json.recovery-pending.json" {
			continue
		}
		result[filepath.Join(root, filepath.FromSlash(input.Path))] = input.SHA256
	}
	return result
}

func runRuntimeRecoverPlan(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime recover plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime recover plan")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "explicit locked REQ path")
	if err := flags.Parse(args); err != nil {
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
	document, planPath, err := persistRecoveryPlan(inventory.Root, inventory)
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
	basePlan, err := recovery.BuildPlan(inventory)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("build conservative recovery plan: %w", err)
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

	createdAt := time.Now().UTC()
	statePath := filepath.Join(temporaryDir, "candidate-state.json")
	journalPath := filepath.Join(temporaryDir, "candidate-events.jsonl")
	state, err := inactiveRuntimeState(root, createdAt)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("build recovery seed runtime: %w", err)
	}
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
	_, err = transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-001",
		ExpectedRevision: 0,
		Actor:            "orchestrator",
		Evidence: map[string]string{
			"req_lock_record":           basePlan.REQ.Path + "@" + basePlan.REQ.SHA256,
			"loop_authorization_record": "recovery-plan:" + basePlan.PlanSHA256[:16],
		},
		REQ: &transition.LockedREQ{
			ID:         basePlan.REQ.ID,
			Path:       basePlan.REQ.Path,
			Version:    basePlan.REQ.Version,
			SHA256:     basePlan.REQ.SHA256,
			ApprovedBy: "recovery-plan",
			ApprovedAt: createdAt.Format(time.RFC3339Nano),
		},
		OccurredAt: createdAt,
	})
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("bind recovery seed runtime: %w", err)
	}
	imported, err := recovery.Import(root, inventory)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("import recovery artifacts: %w", err)
	}
	if err := mergeRecoveryProjection(root, statePath, journalPath, imported, basePlan.PlanSHA256, createdAt); err != nil {
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
			return recoveryPlanDocument{}, "", fmt.Errorf("fingerprint replayed recovery cursor: %w", err)
		}
	}
	if len(basePlan.PlanSHA256) < 16 {
		return recoveryPlanDocument{}, "", fmt.Errorf("recovery plan hash is incomplete")
	}
	planID := "rr-" + basePlan.PlanSHA256[:16]
	planDir := filepath.Join(recoveryRoot, planID)
	planPath := filepath.Join(planDir, "plan.json")
	if existing, readErr := readRecoveryPlan(planPath); readErr == nil {
		return existing, filepath.ToSlash(planPath), nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return recoveryPlanDocument{}, "", fmt.Errorf("read existing recovery plan: %w", readErr)
	}
	candidateState, err := os.ReadFile(statePath)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("read recovery candidate state: %w", err)
	}
	candidateJournal, err := os.ReadFile(journalPath)
	if err != nil {
		return recoveryPlanDocument{}, "", fmt.Errorf("read recovery candidate journal: %w", err)
	}
	document := recoveryPlanDocument{
		Plan:                   basePlan,
		PlanID:                 planID,
		Status:                 "planned",
		CreatedAt:              createdAt.Format(time.RFC3339Nano),
		CandidateStatePath:     filepath.ToSlash(filepath.Join(".claude", "recovery", planID, "candidate-state.json")),
		CandidateJournalPath:   filepath.ToSlash(filepath.Join(".claude", "recovery", planID, "candidate-events.jsonl")),
		CandidateStateSHA256:   hashRecoveryBytes(candidateState),
		CandidateJournalSHA256: hashRecoveryBytes(candidateJournal),
		ImportFindings:         imported.Findings,
		ReplayTrace:            replay.Trace,
		ReplayStopReason:       replay.StopReason,
	}
	document.DocumentSHA256, err = recoveryPlanDocumentHash(document)
	if err != nil {
		return recoveryPlanDocument{}, "", err
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

func mergeRecoveryProjection(root, statePath, journalPath string, imported recovery.ImportResult, planHash string, occurredAt time.Time) error {
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
	eventSuffix := planHash
	if len(eventSuffix) > 16 {
		eventSuffix = eventSuffix[:16]
	}
	if eventSuffix == "" {
		return errors.New("recovery projection plan hash is required")
	}
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	_, err = writer.Update(revision, runtime.Mutation{
		EventID:              "evt-recovery-import-" + eventSuffix,
		Event:                "recovery_projection_imported",
		Actor:                "orchestrator",
		IdempotencyKey:       "recovery-import:" + planHash,
		RuntimeID:            runtimeID,
		From:                 cursor,
		To:                   cursor,
		EvidenceIDs:          evidenceIDs,
		JournalEvent:         "milestone_refreshed",
		JournalOutcome:       "refreshed",
		Message:              "Recovery projection imported from fingerprinted artifacts.",
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
	want, err := recoveryPlanDocumentHash(document)
	if err != nil {
		return recoveryPlanDocument{}, invalidRecoveryPlan(err)
	}
	if document.DocumentSHA256 == "" || document.DocumentSHA256 != want {
		return recoveryPlanDocument{}, invalidRecoveryPlan(fmt.Errorf("recovery plan document hash mismatch"))
	}
	return document, nil
}

func recoveryPlanDocumentHash(document recoveryPlanDocument) (string, error) {
	document.DocumentSHA256 = ""
	data, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode recovery plan fingerprint: %w", err)
	}
	return hashRecoveryBytes(data), nil
}

func hashRecoveryBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func runRuntimeRecoverInspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime recover inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime recover inspect")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "explicit locked REQ path")
	if err := flags.Parse(args); err != nil {
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
		result.SHA256 = input.SHA256
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
