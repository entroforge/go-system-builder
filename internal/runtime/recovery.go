package runtime

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

var (
	// ErrRecoveryCandidateInvalid means that the proposed state or journal
	// cannot be accepted as a valid Runtime pair.
	ErrRecoveryCandidateInvalid = errors.New("recovery candidate is invalid")
	// ErrRecoveryInputDrift means that a plan input changed after it was
	// observed, or that the supplied candidate cannot be applied.
	ErrRecoveryInputDrift = errors.New("recovery input drift")
	// ErrRecoveryConflict means that another recovery plan owns the Runtime
	// recovery slot or that an applied result no longer matches the active pair.
	ErrRecoveryConflict = errors.New("recovery plan conflicts with runtime")
	// ErrRecoveryPending means that an interrupted recovery must be completed
	// before another recovery operation can proceed.
	ErrRecoveryPending = errors.New("recovery apply is pending")
	// ErrRecoveryNoPending means a marker-only resume was requested while no
	// durable recovery apply exists.
	ErrRecoveryNoPending = errors.New("no pending recovery apply")
	// ErrRecoveryOutputMismatch means that an active output did not have the
	// output described by the durable recovery marker after replacement.
	ErrRecoveryOutputMismatch = errors.New("recovery output mismatch")
	// ErrRecoveryLockUnavailable means the shared Runtime lock could not be
	// acquired within the requested timeout.
	ErrRecoveryLockUnavailable = errors.New("recovery runtime lock unavailable")
	// ErrRecoveryInjectedFailure identifies a failure deliberately injected at
	// a recovery boundary by a test or an integration harness.
	ErrRecoveryInjectedFailure = errors.New("recovery failure injected")
)

// RecoveryFailureStep identifies a crash/failure boundary in recovery apply.
type RecoveryFailureStep string

const (
	// RecoveryBeforeQuarantine runs before the original pair is copied.
	RecoveryBeforeQuarantine RecoveryFailureStep = "before_quarantine"
	// RecoveryBeforePendingMarker runs before the durable pending marker write.
	RecoveryBeforePendingMarker RecoveryFailureStep = "before_pending_marker"
	// RecoveryBeforeStateReplace runs before replacing the active state.
	RecoveryBeforeStateReplace RecoveryFailureStep = "before_state_replace"
	// RecoveryAfterStateReplace runs after the active state is replaced.
	RecoveryAfterStateReplace RecoveryFailureStep = "after_state_replace"
	// RecoveryBeforeJournalReplace runs before replacing the active journal.
	RecoveryBeforeJournalReplace RecoveryFailureStep = "before_journal_replace"
	// RecoveryAfterJournalReplace runs after the active journal is replaced.
	RecoveryAfterJournalReplace RecoveryFailureStep = "after_journal_replace"
	// RecoveryBeforeManifest runs before the applied manifest is persisted.
	RecoveryBeforeManifest RecoveryFailureStep = "before_manifest"
)

// RecoveryFailureInjector injects deterministic failures at apply boundaries.
// Production callers should leave it nil. Tests can use it to model crashes,
// partial replacement, and post-write corruption without changing Store.
type RecoveryFailureInjector interface {
	Inject(step RecoveryFailureStep) error
}

// RecoveryRequest contains the input to one recovery
// apply. CandidateState is the original approved Runtime state.
type RecoveryRequest struct {
	Root        string
	StatePath   string
	JournalPath string

	PlanID string

	CandidateState   []byte
	CandidateJournal []byte

	// ValidateRestore is an application capability recomposed by the original
	// Main recovery entrypoint, including marker-only retries. It runs under
	// the Runtime lock before the first active replacement.
	ValidateRestore func(state map[string]any, journal []byte) error

	Approver   string
	OccurredAt time.Time
	Validator  CandidateValidator

	FailureInjector RecoveryFailureInjector
	LockTimeout     time.Duration
}

// RecoveryArtifact describes one original Runtime artifact and its immutable
// quarantine copy. A missing source is represented with Exists=false.
type RecoveryArtifact struct {
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	QuarantinePath string `json:"quarantine_path,omitempty"`
}

// RecoveryManifest is the durable audit record for an applied recovery.
type RecoveryManifest struct {
	SchemaVersion string `json:"schema_version"`
	RecoveryID    string `json:"recovery_id"`
	Status        string `json:"status"`
	PlanID        string `json:"plan_id"`
	StatePath     string `json:"state_path"`
	JournalPath   string `json:"journal_path"`
	QuarantineDir string `json:"quarantine_dir"`
	Approver      string `json:"approved_by"`
	OccurredAt    string `json:"occurred_at"`
	AppliedAt     string `json:"applied_at,omitempty"`

	SourceState      RecoveryArtifact   `json:"source_state"`
	SourceJournal    RecoveryArtifact   `json:"source_journal"`
	SourcePending    []RecoveryArtifact `json:"source_pending,omitempty"`
	UncertainSources []RecoveryArtifact `json:"preserved_unknown_sources,omitempty"`
}

// RecoveryResult reports the applied manifest and whether the call completed
// an interrupted operation or returned an already-applied plan.
type RecoveryResult struct {
	ManifestPath  string
	QuarantineDir string
	Manifest      RecoveryManifest
	Applied       bool
	Retried       bool
	Idempotent    bool
}

// RecoveryInputDriftError identifies a changed original recovery input.
type RecoveryInputDriftError struct {
	Artifact string
	Expected string
	Actual   string
}

func (e *RecoveryInputDriftError) Error() string {
	return fmt.Sprintf("recovery input %s differs: %s (expected %s)", e.Artifact, e.Actual, e.Expected)
}

func (e *RecoveryInputDriftError) Is(target error) bool { return target == ErrRecoveryInputDrift }

// RecoveryOutputMismatchError identifies an active file whose post-replace
// bytes do not match the pending marker.
type RecoveryOutputMismatchError struct {
	Artifact string
	Expected string
	Actual   string
}

func (e *RecoveryOutputMismatchError) Error() string {
	return fmt.Sprintf("recovery output %s differs: %s (expected %s)", e.Artifact, e.Actual, e.Expected)
}

func (e *RecoveryOutputMismatchError) Is(target error) bool {
	return target == ErrRecoveryOutputMismatch
}

type recoveryLockError struct{ Cause error }

func (e *recoveryLockError) Error() string {
	return fmt.Sprintf("recovery runtime lock unavailable: %v", e.Cause)
}

func (e *recoveryLockError) Unwrap() error { return e.Cause }

func (e *recoveryLockError) Is(target error) bool { return target == ErrRecoveryLockUnavailable }

// RecoveryConflictError identifies the recovery plan that owns a conflicting
// pending or applied recovery slot.
type RecoveryConflictError struct {
	PlanID string
	Reason string
}

func (e *RecoveryConflictError) Error() string {
	return fmt.Sprintf("recovery plan %s conflicts: %s", e.PlanID, e.Reason)
}

func (e *RecoveryConflictError) Is(target error) bool { return target == ErrRecoveryConflict }

type recoveryInjectedError struct {
	Step  RecoveryFailureStep
	Cause error
}

func (e *recoveryInjectedError) Error() string {
	return fmt.Sprintf("recovery failed at %s: %v", e.Step, e.Cause)
}

func (e *recoveryInjectedError) Unwrap() error { return e.Cause }

func (e *recoveryInjectedError) Is(target error) bool { return target == ErrRecoveryInjectedFailure }

type preparedRecovery struct {
	request      RecoveryRequest
	state        map[string]any
	stateBytes   []byte
	journalBytes []byte
}

type recoveryPendingMarker struct {
	SchemaVersion string `json:"schema_version"`
	PlanID        string `json:"plan_id"`
	StatePath     string `json:"state_path"`
	JournalPath   string `json:"journal_path"`
	QuarantineDir string `json:"quarantine_dir"`
	ManifestPath  string `json:"manifest_path"`

	CandidateStateBase64   string `json:"candidate_state_base64"`
	CandidateJournalBase64 string `json:"candidate_journal_base64"`

	Manifest RecoveryManifest `json:"manifest"`
}

type quarantineManifest struct {
	SchemaVersion string             `json:"schema_version"`
	PlanID        string             `json:"plan_id"`
	CreatedAt     string             `json:"created_at"`
	State         RecoveryArtifact   `json:"state"`
	Journal       RecoveryArtifact   `json:"journal"`
	Pending       []RecoveryArtifact `json:"pending,omitempty"`
}

type recoveryPendingSource struct {
	path string
	data []byte
}

// ApplyRecovery validates a candidate pair, quarantines the original bytes,
// and atomically applies a new Runtime pair under the shared Runtime lock.
// The active state is never parsed before quarantine, so malformed or BOM-
// prefixed input remains recoverable. An interrupted apply is resumed from
// its durable marker and repeated application of one plan is idempotent.
func ApplyRecovery(request RecoveryRequest) (RecoveryResult, error) {
	normalizedRequest, err := normalizeRecoveryRequest(request)
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := projectlayout.Check(normalizedRequest.Root); err != nil {
		return RecoveryResult{}, err
	}
	if data, err := os.ReadFile(normalizedRequest.StatePath); err == nil {
		if err := projectlayout.CheckRuntime(data); err != nil {
			return RecoveryResult{}, err
		}
	} else if !os.IsNotExist(err) {
		return RecoveryResult{}, err
	}
	for _, path := range []string{normalizedRequest.StatePath, normalizedRequest.JournalPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return RecoveryResult{}, fmt.Errorf("prepare recovery artifact directory: %w", err)
		}
	}
	timeout := normalizedRequest.LockTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	release, err := acquireLock(normalizedRequest.StatePath+".lock", timeout)
	if err != nil {
		return RecoveryResult{}, &recoveryLockError{Cause: err}
	}
	defer release()

	markerPath := normalizedRequest.StatePath + ".recovery-pending.json"
	marker, exists, err := readRecoveryMarker(markerPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	if exists {
		if normalizedRequest.PlanID == "" {
			normalizedRequest.PlanID = marker.PlanID
		}
		if marker.PlanID != normalizedRequest.PlanID {
			return RecoveryResult{}, &RecoveryConflictError{PlanID: marker.PlanID, Reason: "another plan is pending"}
		}
		prepared, err := prepareRecoveryFromMarker(normalizedRequest, marker)
		if err != nil {
			return RecoveryResult{}, err
		}
		if normalizedRequest.ValidateRestore != nil {
			if err := normalizedRequest.ValidateRestore(prepared.state, prepared.journalBytes); err != nil {
				return RecoveryResult{}, err
			}
		}
		return completePendingRecovery(prepared, marker, true)
	}
	if normalizedRequest.PlanID != "" {
		if applied, err := findAppliedRecovery(normalizedRequest); err != nil {
			return RecoveryResult{}, err
		} else if applied != nil {
			return *applied, nil
		}
	}
	if len(normalizedRequest.CandidateState) == 0 && len(normalizedRequest.CandidateJournal) == 0 {
		return RecoveryResult{}, ErrRecoveryNoPending
	}

	prepared, err := prepareRecovery(normalizedRequest)
	if err != nil {
		return RecoveryResult{}, err
	}
	return applyRecoveryLocked(prepared)
}

func prepareRecovery(request RecoveryRequest) (preparedRecovery, error) {
	var err error
	request, err = normalizeRecoveryRequest(request)
	if err != nil {
		return preparedRecovery{}, err
	}
	if strings.TrimSpace(request.PlanID) == "" {
		return preparedRecovery{}, fmt.Errorf("%w: plan id is required", ErrRecoveryCandidateInvalid)
	}
	if strings.TrimSpace(request.Approver) == "" || request.OccurredAt.IsZero() {
		return preparedRecovery{}, fmt.Errorf("%w: approver and occurred_at are required", ErrRecoveryCandidateInvalid)
	}
	if request.Validator == nil {
		return preparedRecovery{}, fmt.Errorf("%w: candidate validator is required", ErrRecoveryCandidateInvalid)
	}

	stateBytes, state, err := candidateStateBytes(request)
	if err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: %w", ErrRecoveryCandidateInvalid, err)
	}
	journalBytes := append([]byte(nil), request.CandidateJournal...)
	if _, err := inspectJournalData(journalBytes); err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: candidate journal: %w", ErrRecoveryCandidateInvalid, err)
	}

	store := NewWriter(request.StatePath, request.JournalPath, request.Root, request.Validator)
	if err := store.validateCandidate(state); err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: candidate state: %w", ErrRecoveryCandidateInvalid, err)
	}
	if err := validateRecoveryPair(state, journalBytes); err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: candidate pair: %w", ErrRecoveryCandidateInvalid, err)
	}
	if hasIssuedIndices(state) {
		if err := ValidateCheckpointIndices(request.Root, state, journalBytes); err != nil {
			return preparedRecovery{}, fmt.Errorf("%w: indices: %w", ErrRecoveryCandidateInvalid, err)
		}
	}

	return preparedRecovery{
		request: request, state: state, stateBytes: stateBytes, journalBytes: journalBytes,
	}, nil
}

func prepareRecoveryFromMarker(request RecoveryRequest, marker recoveryPendingMarker) (preparedRecovery, error) {
	stateBytes, err := base64.StdEncoding.DecodeString(marker.CandidateStateBase64)
	if err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: decode pending candidate state: %w", ErrRecoveryCandidateInvalid, err)
	}
	journalBytes, err := base64.StdEncoding.DecodeString(marker.CandidateJournalBase64)
	if err != nil {
		return preparedRecovery{}, fmt.Errorf("%w: decode pending candidate journal: %w", ErrRecoveryCandidateInvalid, err)
	}
	if marker.Manifest.PlanID != marker.PlanID {
		return preparedRecovery{}, fmt.Errorf("%w: pending marker plan identity differs", ErrRecoveryCandidateInvalid)
	}
	request.CandidateState = stateBytes
	request.CandidateJournal = journalBytes
	prepared, err := prepareRecovery(request)
	if err != nil {
		return preparedRecovery{}, err
	}
	return prepared, nil
}

func normalizeRecoveryRequest(request RecoveryRequest) (RecoveryRequest, error) {
	if strings.TrimSpace(request.Root) == "" || strings.TrimSpace(request.Root) == "." {
		return RecoveryRequest{}, fmt.Errorf("%w: root is required", ErrRecoveryCandidateInvalid)
	}
	absRoot, err := filepath.Abs(request.Root)
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: resolve root: %v", ErrRecoveryCandidateInvalid, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: resolve root symlinks: %v", ErrRecoveryCandidateInvalid, err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return RecoveryRequest{}, fmt.Errorf("%w: root must be an existing directory", ErrRecoveryCandidateInvalid)
	}
	request.Root = filepath.Clean(resolvedRoot)
	request.StatePath, err = recoveryPathWithinRoot(request.Root, request.StatePath)
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: state path: %v", ErrRecoveryCandidateInvalid, err)
	}
	request.JournalPath, err = recoveryPathWithinRoot(request.Root, request.JournalPath)
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: journal path: %v", ErrRecoveryCandidateInvalid, err)
	}
	if request.StatePath == request.JournalPath {
		return RecoveryRequest{}, fmt.Errorf("%w: distinct state and journal paths are required", ErrRecoveryCandidateInvalid)
	}

	return request, nil
}

func recoveryPathWithinRoot(root, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" || strings.TrimSpace(requested) == "." {
		return "", errors.New("path is required")
	}
	target := requested
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	probe := target
	for {
		_, statErr := os.Lstat(probe)
		if statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(probe)
			if resolveErr != nil {
				return "", resolveErr
			}
			suffix, relativeErr := filepath.Rel(probe, target)
			if relativeErr != nil || suffix == ".." || strings.HasPrefix(suffix, ".."+string(filepath.Separator)) || filepath.IsAbs(suffix) {
				return "", errors.New("path cannot be resolved from existing ancestor")
			}
			resolvedTarget := filepath.Clean(filepath.Join(resolved, suffix))
			if !pathContainedBy(root, resolvedTarget) {
				return "", errors.New("resolved path escapes repository root")
			}
			return resolvedTarget, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("no existing repository ancestor")
		}
		probe = parent
	}
}

func pathContainedBy(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func candidateStateBytes(request RecoveryRequest) ([]byte, map[string]any, error) {
	var state map[string]any
	if err := json.Unmarshal(request.CandidateState, &state); err != nil {
		return nil, nil, fmt.Errorf("decode candidate state: %w", err)
	}
	if state == nil {
		return nil, nil, errors.New("candidate state must be an object")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", request.CandidateState); err != nil {
		return nil, nil, fmt.Errorf("validate candidate state bytes: %w", err)
	}
	return append([]byte(nil), request.CandidateState...), state, nil
}

func validateRecoveryPair(state map[string]any, journalBytes []byte) error {
	inspection, err := inspectJournalData(journalBytes)
	if err != nil {
		return err
	}
	runtimeID, _ := state["runtime_id"].(string)
	if inspection.RuntimeID != "" && inspection.RuntimeID != runtimeID {
		return fmt.Errorf("candidate journal runtime_id %q does not match state runtime_id %q", inspection.RuntimeID, runtimeID)
	}
	journaling, err := objectField(state, "journal")
	if err != nil {
		return err
	}
	lastSequence, err := integerField(journaling, "last_sequence")
	if err != nil {
		return err
	}
	if lastSequence != inspection.TailSequence {
		return fmt.Errorf("candidate journal tail sequence %d does not match state sequence %d", inspection.TailSequence, lastSequence)
	}
	lastEvent, _ := journaling["last_event_id"].(string)
	if inspection.TailSequence == 0 {
		if journaling["last_event_id"] != nil && lastEvent != "" {
			return errors.New("empty candidate journal has a state last_event_id")
		}
		return nil
	}
	tail := inspection.Events[len(inspection.Events)-1]
	tailID, _ := tail["event_id"].(string)
	if lastEvent != tailID {
		return fmt.Errorf("candidate journal tail event %q does not match state event %q", tailID, lastEvent)
	}
	return nil
}

func applyRecoveryLocked(prepared preparedRecovery) (RecoveryResult, error) {
	request := prepared.request
	markerPath := request.StatePath + ".recovery-pending.json"
	var marker recoveryPendingMarker

	stateData, stateExists, err := readOptionalFile(request.StatePath)
	if err != nil {
		return RecoveryResult{}, err
	}
	journalData, journalExists, err := readOptionalFile(request.JournalPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	pendingSources, err := inspectRecoveryPendingSources(request)
	if err != nil {
		return RecoveryResult{}, err
	}

	if request.ValidateRestore != nil {
		if err := request.ValidateRestore(prepared.state, prepared.journalBytes); err != nil {
			return RecoveryResult{}, err
		}
	}

	if err := injectRecoveryFailure(request, RecoveryBeforeQuarantine); err != nil {
		return RecoveryResult{}, err
	}
	quarantineDir, sourceState, sourceJournal, sourcePending, err := ensureRecoveryQuarantine(request, stateData, stateExists, journalData, journalExists, pendingSources)
	if err != nil {
		return RecoveryResult{}, err
	}
	manifest := RecoveryManifest{
		SchemaVersion: "1.0.0",
		RecoveryID:    recoveryID(request),
		Status:        "applying",
		PlanID:        request.PlanID,
		StatePath:     recoveryPath(request.Root, request.StatePath),
		JournalPath:   recoveryPath(request.Root, request.JournalPath),
		QuarantineDir: quarantineDir,
		Approver:      request.Approver,
		OccurredAt:    request.OccurredAt.UTC().Format(time.RFC3339Nano),
		SourceState:   sourceState,
		SourceJournal: sourceJournal,
		SourcePending: sourcePending,
	}
	manifest.Status = "applied"
	manifest.AppliedAt = time.Now().UTC().Format(time.RFC3339Nano)
	manifestPath, err := publishRecoveryManifest(request, manifest)
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := appendRecoveryCompletion(&prepared, recoveryPath(request.Root, manifestPath)); err != nil {
		return RecoveryResult{}, err
	}
	marker = recoveryPendingMarker{
		SchemaVersion:          "1.0.0",
		PlanID:                 request.PlanID,
		StatePath:              recoveryPath(request.Root, request.StatePath),
		JournalPath:            recoveryPath(request.Root, request.JournalPath),
		QuarantineDir:          quarantineDir,
		ManifestPath:           recoveryPath(request.Root, manifestPath),
		CandidateStateBase64:   base64.StdEncoding.EncodeToString(prepared.stateBytes),
		CandidateJournalBase64: base64.StdEncoding.EncodeToString(prepared.journalBytes),
		Manifest:               manifest,
	}
	if err := injectRecoveryFailure(request, RecoveryBeforePendingMarker); err != nil {
		return RecoveryResult{}, err
	}
	if err := atomicWriteJSON(markerPath, marker); err != nil {
		return RecoveryResult{}, fmt.Errorf("write recovery pending marker: %w", err)
	}
	return completePendingRecovery(prepared, marker, false)
}

func inspectRecoveryPendingSources(request RecoveryRequest) ([]recoveryPendingSource, error) {
	known, err := recoverySourcePaths(request, nil)
	if err != nil {
		return nil, err
	}

	sources := make([]recoveryPendingSource, 0, len(known))
	for _, path := range known {
		data, exists, err := readOptionalFile(path)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		sources = append(sources, recoveryPendingSource{path: path, data: data})
	}
	return sources, nil
}

// KnownRecoverySourcePendingPaths returns the canonical list of Runtime
// pending marker paths that a recovery plan can declare in
func KnownRecoverySourcePendingPaths(statePath string) []string {
	return knownRecoverySourcePendingPaths(statePath)
}

func knownRecoverySourcePendingPaths(statePath string) []string {
	return []string{
		statePath + ".commit-pending.json",
		statePath + ".rollover-pending.json",
		statePath + ".journal-rotation-pending.json",
	}
}

func completePendingRecovery(prepared preparedRecovery, marker recoveryPendingMarker, retried bool) (RecoveryResult, error) {
	request := prepared.request
	stateBytes, err := base64.StdEncoding.DecodeString(marker.CandidateStateBase64)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("%w: decode pending candidate state: %w", ErrRecoveryPending, err)
	}
	journalBytes, err := base64.StdEncoding.DecodeString(marker.CandidateJournalBase64)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("%w: decode pending candidate journal: %w", ErrRecoveryPending, err)
	}
	if marker.QuarantineDir == "" {
		return RecoveryResult{}, fmt.Errorf("%w: quarantine directory is missing", ErrRecoveryPending)
	}
	if marker.StatePath != recoveryPath(request.Root, request.StatePath) || marker.JournalPath != recoveryPath(request.Root, request.JournalPath) {
		return RecoveryResult{}, fmt.Errorf("%w: pending marker paths do not match request", ErrRecoveryPending)
	}
	if err := verifyQuarantineManifest(marker.QuarantineDir, marker.Manifest); err != nil {
		return RecoveryResult{}, fmt.Errorf("%w: verify quarantine: %w", ErrRecoveryPending, err)
	}

	stateData, stateExists, err := readOptionalFile(request.StatePath)
	if err != nil {
		return RecoveryResult{}, err
	}
	stateDone, stateNewer := recoveryStateProgress(stateData, stateBytes)
	if stateNewer {
		var current, target map[string]any
		_ = json.Unmarshal(stateData, &current)
		_ = json.Unmarshal(stateBytes, &target)
		if stringValue(current["runtime_id"]) == stringValue(target["runtime_id"]) {
			if _, err := controlproof.ReadJournalForStateAt(request.JournalPath, current); err != nil {
				stateNewer = false // unverified state is preserved as unknown below
			}
		}
	}
	if stateNewer {
		return RecoveryResult{}, &RecoveryConflictError{PlanID: marker.PlanID, Reason: "current Runtime has later or different committed progress; original retained"}
	}
	if !stateDone {
		if retried && stateExists {
			if err := preserveUnknownRecoverySource(request, &marker, request.StatePath, stateData); err != nil {
				return RecoveryResult{}, err
			}
		}
		if err := injectRecoveryFailure(request, RecoveryBeforeStateReplace); err != nil {
			return RecoveryResult{}, err
		}
		if err := atomicWriteBytes(request.StatePath, stateBytes, ".loop-recovery-state-*.tmp"); err != nil {
			return RecoveryResult{}, fmt.Errorf("replace recovery state: %w", err)
		}
		if err := injectRecoveryFailure(request, RecoveryAfterStateReplace); err != nil {
			return RecoveryResult{}, err
		}
		written, err := os.ReadFile(request.StatePath)
		if err != nil {
			return RecoveryResult{}, err
		}
		if done, _ := recoveryStateProgress(written, stateBytes); !done {
			return RecoveryResult{}, &RecoveryOutputMismatchError{Artifact: "active_state", Expected: "pending Writer revision", Actual: "unknown state"}
		}
	}
	journalData, journalExists, err := readOptionalFile(request.JournalPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	journalDone, journalNewer := recoveryJournalProgress(journalData, journalBytes)
	if journalNewer {
		return RecoveryResult{}, &RecoveryConflictError{PlanID: marker.PlanID, Reason: "current Runtime journal has later or different committed progress; original retained"}
	}
	if !journalDone {
		if retried && journalExists {
			if err := preserveUnknownRecoverySource(request, &marker, request.JournalPath, journalData); err != nil {
				return RecoveryResult{}, err
			}
		}
		if err := injectRecoveryFailure(request, RecoveryBeforeJournalReplace); err != nil {
			return RecoveryResult{}, err
		}
		if err := atomicWriteBytes(request.JournalPath, journalBytes, ".loop-recovery-journal-*.tmp"); err != nil {
			return RecoveryResult{}, fmt.Errorf("replace recovery journal: %w", err)
		}
		if err := injectRecoveryFailure(request, RecoveryAfterJournalReplace); err != nil {
			return RecoveryResult{}, err
		}
		written, err := os.ReadFile(request.JournalPath)
		if err != nil {
			return RecoveryResult{}, err
		}
		if done, _ := recoveryJournalProgress(written, journalBytes); !done {
			return RecoveryResult{}, &RecoveryOutputMismatchError{Artifact: "active_journal", Expected: "pending Writer event", Actual: "unknown journal"}
		}
	}
	if err := retireRecoverySourcePending(request, marker.Manifest.SourcePending, retried); err != nil {
		return RecoveryResult{}, err
	}
	// RC-13 contract: a rotation marker that survived the atomic apply means
	// it was created concurrently (between inspect and retire) or its
	// retirement failed silently. Either way the state/journal pair has
	// diverged from the operator's declared plan; refuse to claim the
	// recovery applied and surface a `reconcile` hint so the marker is not
	// orphaned and the next writer does not write through a stale pair.
	if err := verifyPostApplyPendingRetired(request); err != nil {
		return RecoveryResult{}, err
	}
	// A sealed recovery can outlive the mutable checkpoint directory. Restore
	// the bounded backup before releasing its original pending marker; failure
	// remains a retry of this pair, never a second domain reception.
	if hasIssuedIndices(prepared.state) {
		writer := NewWriter(request.StatePath, request.JournalPath, request.Root, request.Validator)
		if err := writer.publishCheckpointLocked(); err != nil {
			return RecoveryResult{}, fmt.Errorf("checkpoint restored Runtime (recovery pending retained): %w", err)
		}
	}

	if err := injectRecoveryFailure(request, RecoveryBeforeManifest); err != nil {
		return RecoveryResult{}, err
	}
	manifestPath := filepath.Join(request.Root, filepath.FromSlash(marker.ManifestPath))
	if err := atomicWriteJSON(manifestPath, marker.Manifest); err != nil {
		return RecoveryResult{}, fmt.Errorf("record preserved recovery sources: %w", err)
	}
	if err := os.Remove(request.StatePath + ".recovery-pending.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return RecoveryResult{}, fmt.Errorf("clear recovery pending marker: %w", err)
	}
	if err := syncDir(filepath.Dir(request.StatePath)); err != nil {
		return RecoveryResult{}, fmt.Errorf("sync recovery active directory: %w", err)
	}
	result := recoveryResult(request, marker.Manifest, false, retried)
	result.ManifestPath = manifestPath
	return result, nil
}

// Progress is decided by the original Writer's Runtime revision and journal
// event, never by comparing a complete user document.
func recoveryStateProgress(current, target []byte) (done, later bool) {
	var have, want map[string]any
	if json.Unmarshal(current, &have) != nil || json.Unmarshal(target, &want) != nil {
		return false, false
	}
	haveRev, e1 := integerField(have, "revision")
	wantRev, e2 := integerField(want, "revision")
	haveJournal, ok1 := have["journal"].(map[string]any)
	wantJournal, ok2 := want["journal"].(map[string]any)
	if e1 != nil || e2 != nil || !ok1 || !ok2 {
		return false, false
	}
	if stringValue(have["runtime_id"]) != stringValue(want["runtime_id"]) {
		return false, true
	}
	if haveRev > wantRev {
		return false, true
	}
	if haveRev == wantRev {
		haveSequence, haveErr := integerField(haveJournal, "last_sequence")
		wantSequence, wantErr := integerField(wantJournal, "last_sequence")
		if haveErr == nil && wantErr == nil && stringValue(haveJournal["last_event_id"]) == stringValue(wantJournal["last_event_id"]) && haveSequence == wantSequence {
			return true, false
		}
		return false, true
	}
	return false, false
}

func recoveryJournalProgress(current, target []byte) (done, later bool) {
	have, e1 := inspectJournalData(current)
	want, e2 := inspectJournalData(target)
	if e1 != nil || e2 != nil || have.TailSequence == 0 || want.TailSequence == 0 {
		return false, false
	}
	if have.RuntimeID != want.RuntimeID || have.TailSequence > want.TailSequence {
		return false, true
	}
	if have.TailSequence == want.TailSequence {
		if have.Events[len(have.Events)-1]["event_id"] == want.Events[len(want.Events)-1]["event_id"] {
			return true, false
		}
		return false, true
	}
	return false, false
}

func preserveUnknownRecoverySource(request RecoveryRequest, marker *recoveryPendingMarker, source string, data []byte) error {
	dir, err := os.MkdirTemp(marker.QuarantineDir, "pending-source-")
	if err != nil {
		return err
	}
	saved := filepath.Join(dir, filepath.Base(source))
	if err := writeImmutableBytes(saved, data); err != nil {
		return err
	}
	marker.Manifest.UncertainSources = append(marker.Manifest.UncertainSources, recoveryArtifact(request.Root, source, true, saved))
	if err := atomicWriteJSON(request.StatePath+".recovery-pending.json", marker); err != nil {
		return fmt.Errorf("record preserved unknown source: %w", err)
	}
	return nil
}

func verifyPostApplyPendingRetired(request RecoveryRequest) error {
	paths, err := recoverySourcePaths(request, nil)
	if err != nil {
		return err
	}
	for _, path := range paths {
		_, exists, err := readOptionalFile(path)
		if err != nil {
			return fmt.Errorf("post-apply pending marker check: %w", err)
		}
		if !exists {
			continue
		}
		// A pending marker that survived the atomic state/journal replace
		// must be the rotation marker (or any other Runtime pending marker)
		// racing with recovery. The plan-driven recovery claimed the
		// candidate was applied, but the runtime still has a marker that
		// could rewrite state/journal. Surface the conflict and require
		// the operator to `runtime reconcile` so the marker is retired
		// before any subsequent writer observes the active pair.
		relative := recoveryPath(request.Root, path)
		return &RecoveryConflictError{
			PlanID: request.PlanID,
			Reason: fmt.Sprintf("Runtime pending marker %s survived atomic replace; preserve it for reconciliation", relative),
		}
	}
	return nil
}

func retireRecoverySourcePending(request RecoveryRequest, artifacts []RecoveryArtifact, retried bool) error {
	byPath := make(map[string]RecoveryArtifact, len(artifacts))
	for _, artifact := range artifacts {
		byPath[artifact.Path] = artifact
	}
	removed := false
	paths, err := recoverySourcePaths(request, artifacts)
	if err != nil {
		return err
	}
	for _, path := range paths {
		relative := recoveryPath(request.Root, path)
		artifact, planned := byPath[relative]
		data, exists, err := readOptionalFile(path)
		if err != nil {
			return err
		}
		if !planned {
			if exists {
				return &RecoveryConflictError{PlanID: request.PlanID, Reason: "unexpected Runtime pending marker appeared during recovery"}
			}
			continue
		}
		if !artifact.Exists || artifact.QuarantinePath == "" {
			return fmt.Errorf("%w: pending source artifact is incomplete", ErrRecoveryPending)
		}
		if !exists {
			// A retry may observe a marker already retired after both candidate
			// files were replaced but before the applied manifest was written.
			continue
		}
		if retried {
			original, err := os.ReadFile(artifact.QuarantinePath)
			if err != nil {
				return err
			}
			if !sameRecoveryPendingIdentity(path, original, data) {
				return &RecoveryConflictError{PlanID: request.PlanID, Reason: "Runtime pending marker identity is unknown; original file retained"}
			}
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("retire recovered Runtime pending marker %s: %w", relative, err)
		}
		removed = true
	}
	if removed {
		if err := syncDir(filepath.Dir(request.StatePath)); err != nil {
			return fmt.Errorf("sync retired Runtime pending markers: %w", err)
		}
	}
	return nil
}

func sameRecoveryPendingIdentity(path string, original, current []byte) bool {
	var before, after map[string]any
	if json.Unmarshal(original, &before) != nil || json.Unmarshal(current, &after) != nil {
		return false
	}
	keys := []string{}
	switch {
	case strings.HasSuffix(path, ".commit-pending.json"):
		oldEvent, ok1 := before["journal_event"].(map[string]any)
		newEvent, ok2 := after["journal_event"].(map[string]any)
		oldID, oldOK := oldEvent["event_id"].(string)
		newID, newOK := newEvent["event_id"].(string)
		if !ok1 || !ok2 || !oldOK || !newOK || oldID == "" || oldID != newID {
			return false
		}
		keys = []string{"previous_revision", "idempotency_key"}
	case strings.HasSuffix(path, ".rollover-pending.json"):
		keys = []string{"source_runtime_id", "source_revision"}
	case strings.HasSuffix(path, ".journal-rotation-pending.json"):
		keys = []string{"tail_event_id", "tail_sequence", "archived_file"}
	default:
		return false
	}
	for _, key := range keys {
		switch value := before[key].(type) {
		case string:
			other, ok := after[key].(string)
			if !ok || value == "" || value != other {
				return false
			}
		case float64:
			other, ok := after[key].(float64)
			if !ok || value != other {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func recoveryResult(request RecoveryRequest, manifest RecoveryManifest, idempotent, retried bool) RecoveryResult {
	return RecoveryResult{
		ManifestPath:  recoveryManifestPath(request),
		QuarantineDir: manifest.QuarantineDir,
		Manifest:      manifest,
		Applied:       true,
		Retried:       retried,
		Idempotent:    idempotent,
	}
}

func injectRecoveryFailure(request RecoveryRequest, step RecoveryFailureStep) error {
	if request.FailureInjector == nil {
		return nil
	}
	if err := request.FailureInjector.Inject(step); err != nil {
		return &recoveryInjectedError{Step: step, Cause: err}
	}
	return nil
}

func readRecoveryMarker(path string) (recoveryPendingMarker, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return recoveryPendingMarker{}, false, nil
	}
	if err != nil {
		return recoveryPendingMarker{}, false, fmt.Errorf("read recovery pending marker: %w", err)
	}
	var marker recoveryPendingMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return recoveryPendingMarker{}, false, fmt.Errorf("%w: decode recovery pending marker: %w", ErrRecoveryPending, err)
	}
	if marker.SchemaVersion != "1.0.0" || marker.PlanID == "" {
		return recoveryPendingMarker{}, false, fmt.Errorf("%w: recovery pending marker is incomplete", ErrRecoveryPending)
	}
	return marker, true, nil
}

// A recovery is complete only when the original Writer's committed journal
// records its plan and the actual manifest it created. Unrelated files are not
// inspected as recovery receipts.
func findAppliedRecovery(request RecoveryRequest) (*RecoveryResult, error) {
	raw, err := os.ReadFile(request.StatePath)
	if err != nil {
		return nil, nil
	}
	var state map[string]any
	if json.Unmarshal(raw, &state) != nil {
		return nil, nil
	}
	journal, err := controlproof.ReadJournalForStateAt(request.JournalPath, state)
	if err != nil {
		return nil, nil
	}
	inspection, err := inspectJournalData(journal)
	if err != nil {
		return nil, nil
	}
	for _, event := range inspection.Events {
		if event["transition_id"] != "RECOVERY-APPLIED" || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["idempotency_key"] != "recovery:"+request.PlanID || event["runtime_id"] != state["runtime_id"] {
			continue
		}
		refs, _ := event["evidence_ids"].([]any)
		if len(refs) != 1 {
			continue
		}
		ref, ok := refs[0].(string)
		if !ok || ref == "" {
			continue
		}
		manifest := RecoveryManifest{PlanID: request.PlanID, Status: "applied"}
		if data, err := workpacket.ReadFile(request.Root, ref, 4<<20); err == nil {
			_ = json.Unmarshal(data, &manifest)
		}
		result := recoveryResult(request, manifest, true, false)
		result.Manifest.PlanID = request.PlanID
		result.ManifestPath = filepath.Join(request.Root, filepath.FromSlash(ref))
		return &result, nil
	}
	return nil, nil
}

func publishRecoveryManifest(request RecoveryRequest, manifest RecoveryManifest) (string, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	original := recoveryManifestPath(request)
	for attempt := 0; ; attempt++ {
		path := original
		if attempt > 0 {
			path = filepath.Join(append([]string{request.Root, ".claude", fmt.Sprintf("recovery-manifests-alt-%d", attempt)}, append(recoveryPlanParts(request.PlanID), "manifest.json")...)...)
		}
		relative := recoveryPath(request.Root, path)
		created, err := workpacket.PublishNewBounded(request.Root, relative, data, 4<<20)
		if err != nil {
			return "", fmt.Errorf("publish recovery manifest: %w", err)
		}
		if created {
			return path, nil
		}
	}
}

func appendRecoveryCompletion(prepared *preparedRecovery, manifestRef string) error {
	state := prepared.state
	before, err := integerField(state, "revision")
	if err != nil {
		return err
	}
	journal, err := objectField(state, "journal")
	if err != nil {
		return err
	}
	sequence, err := integerField(journal, "last_sequence")
	if err != nil {
		return err
	}
	id := "recovery-" + recoveryID(prepared.request)
	mutation := Mutation{EventID: id, TransitionID: "RECOVERY-APPLIED", Actor: "orchestrator", IdempotencyKey: "recovery:" + prepared.request.PlanID, EvidenceIDs: []string{manifestRef}, BaselineGeneration: baselineGeneration(state), Message: "Applied the approved Runtime recovery plan."}
	event := buildJournalEvent(mutation, fmt.Sprint(state["runtime_id"]), before, before+1, sequence+1, prepared.request.OccurredAt)
	if err := validateJournalEvent(event); err != nil {
		return err
	}
	state["revision"] = before + 1
	journal["last_sequence"] = sequence + 1
	journal["last_event_id"] = id
	encodedState, err := json.Marshal(state)
	if err != nil {
		return err
	}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(prepared.journalBytes) > 0 && prepared.journalBytes[len(prepared.journalBytes)-1] != '\n' {
		prepared.journalBytes = append(prepared.journalBytes, '\n')
	}
	prepared.journalBytes = append(prepared.journalBytes, encodedEvent...)
	prepared.journalBytes = append(prepared.journalBytes, '\n')
	prepared.stateBytes = encodedState
	if err := NewWriter(prepared.request.StatePath, prepared.request.JournalPath, prepared.request.Root, prepared.request.Validator).validateCandidate(state); err != nil {
		return err
	}
	return validateRecoveryPair(state, prepared.journalBytes)
}

func ensureRecoveryQuarantine(request RecoveryRequest, stateData []byte, stateExists bool, journalData []byte, journalExists bool, pendingSources []recoveryPendingSource) (string, RecoveryArtifact, RecoveryArtifact, []RecoveryArtifact, error) {
	base := filepath.Join(request.Root, ".claude", "recovery", "quarantine")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("create recovery quarantine root: %w", err)
	}
	dir, err := os.MkdirTemp(base, "recovery-")
	if err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("create recovery quarantine: %w", err)
	}

	stateArtifact := recoveryArtifact(request.Root, request.StatePath, stateExists, filepath.Join(dir, "loop-state.json"))
	journalArtifact := recoveryArtifact(request.Root, request.JournalPath, journalExists, filepath.Join(dir, "loop-events.jsonl"))
	if stateExists {
		if err := writeImmutableBytes(stateArtifact.QuarantinePath, stateData); err != nil {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("quarantine state: %w", err)
		}
	}
	if journalExists {
		if err := writeImmutableBytes(journalArtifact.QuarantinePath, journalData); err != nil {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("quarantine journal: %w", err)
		}
	}
	pendingArtifacts := make([]RecoveryArtifact, 0, len(pendingSources))
	for _, source := range pendingSources {
		name := filepath.Base(source.path)
		artifact := recoveryArtifact(request.Root, source.path, true, filepath.Join(dir, name))
		if err := writeImmutableBytes(artifact.QuarantinePath, source.data); err != nil {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("quarantine pending marker: %w", err)
		}
		pendingArtifacts = append(pendingArtifacts, artifact)
	}
	manifest := quarantineManifest{
		SchemaVersion: "1.0.0",
		PlanID:        request.PlanID,
		CreatedAt:     request.OccurredAt.UTC().Format(time.RFC3339Nano),
		State:         stateArtifact,
		Journal:       journalArtifact,
		Pending:       pendingArtifacts,
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := writeImmutableJSON(manifestPath, manifest); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("write quarantine manifest: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("sync recovery quarantine: %w", err)
	}
	return dir, stateArtifact, journalArtifact, pendingArtifacts, nil
}

func recoveryArtifact(root, path string, exists bool, quarantinePath string) RecoveryArtifact {
	artifact := RecoveryArtifact{Path: recoveryPath(root, path), Exists: exists}
	if exists {
		artifact.QuarantinePath = quarantinePath
	}
	return artifact
}

func verifyQuarantineManifest(dir string, manifest RecoveryManifest) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var quarantine quarantineManifest
	if err := json.Unmarshal(data, &quarantine); err != nil {
		return err
	}
	if manifest.QuarantineDir != dir {
		return errors.New("recovery manifest quarantine directory mismatch")
	}
	expected := quarantineManifest{
		SchemaVersion: manifest.SchemaVersion,
		PlanID:        manifest.PlanID,
		CreatedAt:     manifest.OccurredAt,
		State:         manifest.SourceState,
		Journal:       manifest.SourceJournal,
		Pending:       manifest.SourcePending,
	}
	if quarantine.SchemaVersion != expected.SchemaVersion || quarantine.PlanID != expected.PlanID {
		return errors.New("quarantine manifest identity does not match recovery manifest")
	}
	if !sameRecoveryArtifact(quarantine.State, expected.State) || !sameRecoveryArtifact(quarantine.Journal, expected.Journal) {
		return errors.New("quarantine manifest state or journal does not match recovery manifest")
	}
	if !sameRecoveryArtifacts(quarantine.Pending, expected.Pending) {
		return errors.New("quarantine manifest source_pending does not match recovery manifest")
	}
	artifacts := []RecoveryArtifact{quarantine.State, quarantine.Journal}
	artifacts = append(artifacts, quarantine.Pending...)
	for _, artifact := range artifacts {
		if !artifact.Exists {
			continue
		}
		if _, err := os.Stat(artifact.QuarantinePath); err != nil {
			return err
		}
	}
	return nil
}

func sameRecoveryArtifact(left, right RecoveryArtifact) bool {
	return left.Path == right.Path && left.Exists == right.Exists && left.QuarantinePath == right.QuarantinePath
}

func sameRecoveryArtifacts(left, right []RecoveryArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameRecoveryArtifact(left[index], right[index]) {
			return false
		}
	}
	return true
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read recovery artifact %s: %w", path, err)
	}
	return data, true, nil
}

func writeImmutableBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create immutable artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create immutable artifact: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write immutable artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync immutable artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close immutable artifact: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync immutable artifact directory: %w", err)
	}
	return nil
}

func writeImmutableJSON(path string, value any) error {
	data, err := jsonDocumentBytes(value)
	if err != nil {
		return fmt.Errorf("encode immutable JSON: %w", err)
	}
	return writeImmutableBytes(path, data)
}

func recoveryPlanParts(id string) []string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(id))
	parts := []string{"plan"}
	for len(encoded) > 120 {
		parts = append(parts, encoded[:120])
		encoded = encoded[120:]
	}
	return append(parts, encoded)
}

func recoveryManifestPath(request RecoveryRequest) string {
	return filepath.Join(append([]string{request.Root, ".claude", "recovery", "manifests"}, append(recoveryPlanParts(request.PlanID), "manifest.json")...)...)
}

func recoveryID(request RecoveryRequest) string {
	return base64.RawURLEncoding.EncodeToString([]byte(request.PlanID))
}

func safeRecoveryComponent(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "unnamed"
	}
	return builder.String()
}

func recoveryPath(root, path string) string {
	rootAbs, rootErr := filepath.Abs(root)
	pathAbs, pathErr := filepath.Abs(path)
	if rootErr == nil && pathErr == nil {
		rel, err := filepath.Rel(rootAbs, pathAbs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// Rotation archives are original pair members. A complete replacement journal
// must quarantine and retire them with the same marker, including retries
// after some segments were already retired. Arbitrary paths remain forbidden.
func recoverySourcePaths(request RecoveryRequest, artifacts []RecoveryArtifact) ([]string, error) {
	paths := knownRecoverySourcePendingPaths(request.StatePath)
	segments, err := journalSegmentPaths(request.JournalPath)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range paths {
		seen[p] = true
	}
	add := func(p string) error {
		if seen[p] {
			return nil
		}
		if filepath.Dir(p) != filepath.Dir(request.JournalPath) || !strings.HasPrefix(p, request.JournalPath+".archive.") {
			return fmt.Errorf("%w: unsupported recovery pair member %s", ErrRecoveryCandidateInvalid, p)
		}
		if _, ok := parseArchiveSeq(p); !ok {
			return fmt.Errorf("%w: invalid journal archive", ErrRecoveryCandidateInvalid)
		}
		seen[p] = true
		segments = append(segments, p)
		return nil
	}
	// Deduplicate initial discovery before adding frozen paths.
	discovered := append([]string(nil), segments...)
	segments = nil
	for _, p := range discovered {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, a := range artifacts {
		p := filepath.Join(request.Root, filepath.FromSlash(a.Path))
		if err := add(p); err != nil {
			return nil, err
		}
	}
	sort.Strings(segments)
	return append(paths, segments...), nil
}
