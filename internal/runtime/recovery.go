package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

var (
	// ErrRecoveryCandidateInvalid means that the proposed state or journal
	// cannot be accepted as a valid Runtime pair.
	ErrRecoveryCandidateInvalid = errors.New("recovery candidate is invalid")
	// ErrRecoveryInputDrift means that a plan input changed after it was
	// fingerprinted, or that the supplied candidate does not match its hash.
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
	// hash promised by the durable recovery marker after replacement.
	ErrRecoveryOutputMismatch = errors.New("recovery output hash mismatch")
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

// RecoveryRequest contains the complete, fingerprinted input to one recovery
// apply. CandidateState or CandidateStateMap must be supplied; when both are
// supplied they must describe the same JSON value.
type RecoveryRequest struct {
	Root        string
	StatePath   string
	JournalPath string

	PlanID  string
	PlanSHA string

	CandidateState    []byte
	CandidateStateMap map[string]any
	CandidateJournal  []byte

	// CandidateStateSHA256 and CandidateJournalSHA256 are optional expected
	// output hashes. Supplying them makes candidate drift fail before any
	// active file is touched.
	CandidateStateSHA256   string
	CandidateJournalSHA256 string
	// SourceStateSHA256 and SourceJournalSHA256 are optional hashes captured by
	// inspect/plan for the active pair. They prevent applying a stale plan.
	SourceStateSHA256   string
	SourceJournalSHA256 string
	// SourceStateExists and SourceJournalExists distinguish "expected missing"
	// from an omitted hash assertion. When supplied, existence is checked under
	// the Runtime lock before quarantine or replacement.
	SourceStateExists   *bool
	SourceJournalExists *bool
	// SourcePendingSHA256 fingerprints pre-existing commit/fingerprint/rollover markers.
	// Recovery quarantines and retires only these known Runtime marker paths.
	SourcePendingSHA256 map[string]string

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
	SHA256         string `json:"sha256,omitempty"`
	Size           int64  `json:"size,omitempty"`
	QuarantinePath string `json:"quarantine_path,omitempty"`
}

// RecoveryManifest is the durable audit record for an applied recovery.
type RecoveryManifest struct {
	SchemaVersion string `json:"schema_version"`
	RecoveryID    string `json:"recovery_id"`
	Status        string `json:"status"`
	PlanID        string `json:"plan_id"`
	PlanSHA       string `json:"plan_sha256"`
	StatePath     string `json:"state_path"`
	JournalPath   string `json:"journal_path"`
	QuarantineDir string `json:"quarantine_dir"`
	Approver      string `json:"approved_by"`
	OccurredAt    string `json:"occurred_at"`
	AppliedAt     string `json:"applied_at,omitempty"`

	SourceState   RecoveryArtifact   `json:"source_state"`
	SourceJournal RecoveryArtifact   `json:"source_journal"`
	SourcePending []RecoveryArtifact `json:"source_pending,omitempty"`

	CandidateStateSHA256   string `json:"candidate_state_sha256"`
	CandidateJournalSHA256 string `json:"candidate_journal_sha256"`
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

// RecoveryInputDriftError identifies one changed or mismatched fingerprint.
type RecoveryInputDriftError struct {
	Artifact string
	Expected string
	Actual   string
}

func (e *RecoveryInputDriftError) Error() string {
	return fmt.Sprintf("recovery input %s hash %q does not match expected %q", e.Artifact, e.Actual, e.Expected)
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
	return fmt.Sprintf("recovery output %s hash %q does not match expected %q", e.Artifact, e.Actual, e.Expected)
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
	PlanID  string
	PlanSHA string
	Reason  string
}

func (e *RecoveryConflictError) Error() string {
	return fmt.Sprintf("recovery plan %s/%s conflicts: %s", e.PlanID, e.PlanSHA, e.Reason)
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
	request       RecoveryRequest
	state         map[string]any
	stateBytes    []byte
	journalBytes  []byte
	stateSHA256   string
	journalSHA256 string
}

type recoveryPendingMarker struct {
	SchemaVersion string `json:"schema_version"`
	PlanID        string `json:"plan_id"`
	PlanSHA       string `json:"plan_sha256"`
	StatePath     string `json:"state_path"`
	JournalPath   string `json:"journal_path"`
	QuarantineDir string `json:"quarantine_dir"`

	CandidateStateBase64   string `json:"candidate_state_base64"`
	CandidateJournalBase64 string `json:"candidate_journal_base64"`
	CandidateStateSHA256   string `json:"candidate_state_sha256"`
	CandidateJournalSHA256 string `json:"candidate_journal_sha256"`

	Manifest RecoveryManifest `json:"manifest"`
}

type quarantineManifest struct {
	SchemaVersion          string             `json:"schema_version"`
	PlanID                 string             `json:"plan_id"`
	PlanSHA                string             `json:"plan_sha256"`
	CreatedAt              string             `json:"created_at"`
	CandidateStateSHA256   string             `json:"candidate_state_sha256"`
	CandidateJournalSHA256 string             `json:"candidate_journal_sha256"`
	State                  RecoveryArtifact   `json:"state"`
	Journal                RecoveryArtifact   `json:"journal"`
	Pending                []RecoveryArtifact `json:"pending,omitempty"`
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
		if normalizedRequest.PlanID == "" && normalizedRequest.PlanSHA == "" {
			normalizedRequest.PlanID = marker.PlanID
			normalizedRequest.PlanSHA = marker.PlanSHA
		}
		if marker.PlanID != normalizedRequest.PlanID || marker.PlanSHA != normalizedRequest.PlanSHA {
			return RecoveryResult{}, &RecoveryConflictError{PlanID: marker.PlanID, PlanSHA: marker.PlanSHA, Reason: "another plan is pending"}
		}
		prepared, err := prepareRecoveryFromMarker(normalizedRequest, marker)
		if err != nil {
			return RecoveryResult{}, err
		}
		return completePendingRecovery(prepared, marker, true)
	}
	if len(normalizedRequest.CandidateState) == 0 && normalizedRequest.CandidateStateMap == nil && len(normalizedRequest.CandidateJournal) == 0 {
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
	if strings.TrimSpace(request.PlanID) == "" || strings.TrimSpace(request.PlanSHA) == "" {
		return preparedRecovery{}, fmt.Errorf("%w: plan id and sha are required", ErrRecoveryCandidateInvalid)
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

	stateSHA := sha256Hex(stateBytes)
	journalSHA := sha256Hex(journalBytes)
	if err := verifyDeclaredHash("candidate_state", request.CandidateStateSHA256, stateSHA); err != nil {
		return preparedRecovery{}, err
	}
	if err := verifyDeclaredHash("candidate_journal", request.CandidateJournalSHA256, journalSHA); err != nil {
		return preparedRecovery{}, err
	}
	request.CandidateStateSHA256 = stateSHA
	request.CandidateJournalSHA256 = journalSHA
	return preparedRecovery{
		request:       request,
		state:         state,
		stateBytes:    stateBytes,
		journalBytes:  journalBytes,
		stateSHA256:   stateSHA,
		journalSHA256: journalSHA,
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
	if marker.CandidateStateSHA256 == "" || marker.CandidateJournalSHA256 == "" {
		return preparedRecovery{}, fmt.Errorf("%w: pending candidate hashes are required", ErrRecoveryCandidateInvalid)
	}
	if marker.Manifest.PlanID != marker.PlanID || marker.Manifest.PlanSHA != marker.PlanSHA ||
		marker.Manifest.CandidateStateSHA256 != marker.CandidateStateSHA256 ||
		marker.Manifest.CandidateJournalSHA256 != marker.CandidateJournalSHA256 {
		return preparedRecovery{}, fmt.Errorf("%w: pending marker manifest does not match candidate identity", ErrRecoveryCandidateInvalid)
	}
	request.CandidateState = stateBytes
	request.CandidateStateMap = nil
	request.CandidateJournal = journalBytes
	request.CandidateStateSHA256 = marker.CandidateStateSHA256
	request.CandidateJournalSHA256 = marker.CandidateJournalSHA256
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
	if len(request.SourcePendingSHA256) > 0 {
		normalizedPending := make(map[string]string, len(request.SourcePendingSHA256))
		for path, hash := range request.SourcePendingSHA256 {
			normalizedPath, pathErr := recoveryPathWithinRoot(request.Root, path)
			if pathErr != nil {
				return RecoveryRequest{}, fmt.Errorf("%w: source pending path: %v", ErrRecoveryCandidateInvalid, pathErr)
			}
			if _, duplicate := normalizedPending[normalizedPath]; duplicate {
				return RecoveryRequest{}, fmt.Errorf("%w: duplicate source pending path", ErrRecoveryCandidateInvalid)
			}
			normalizedPending[normalizedPath] = hash
		}
		request.SourcePendingSHA256 = normalizedPending
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
	if len(request.CandidateState) > 0 {
		if err := json.Unmarshal(request.CandidateState, &state); err != nil {
			return nil, nil, fmt.Errorf("decode candidate state: %w", err)
		}
		if state == nil {
			return nil, nil, errors.New("candidate state must be an object")
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", request.CandidateState); err != nil {
			return nil, nil, fmt.Errorf("validate candidate state bytes: %w", err)
		}
		if request.CandidateStateMap != nil {
			left, err := canonicalJSON(request.CandidateState)
			if err != nil {
				return nil, nil, fmt.Errorf("normalize candidate state bytes: %w", err)
			}
			right, err := json.Marshal(request.CandidateStateMap)
			if err != nil {
				return nil, nil, fmt.Errorf("encode candidate state map: %w", err)
			}
			right, err = canonicalJSON(right)
			if err != nil {
				return nil, nil, fmt.Errorf("normalize candidate state map: %w", err)
			}
			if !bytes.Equal(left, right) {
				return nil, nil, errors.New("candidate state bytes and map differ")
			}
		}
	} else if request.CandidateStateMap != nil {
		state = request.CandidateStateMap
	} else {
		return nil, nil, errors.New("candidate state bytes or map are required")
	}

	if len(request.CandidateState) > 0 {
		return append([]byte(nil), request.CandidateState...), state, nil
	}
	encoded, err := jsonDocumentBytes(state)
	if err != nil {
		return nil, nil, fmt.Errorf("encode candidate state: %w", err)
	}
	return encoded, state, nil
}

func canonicalJSON(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
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

func verifyDeclaredHash(artifact, expected, actual string) error {
	if expected == "" || expected == actual {
		return nil
	}
	return &RecoveryInputDriftError{Artifact: artifact, Expected: expected, Actual: actual}
}

func applyRecoveryLocked(prepared preparedRecovery) (RecoveryResult, error) {
	request := prepared.request
	markerPath := request.StatePath + ".recovery-pending.json"
	marker, exists, err := readRecoveryMarker(markerPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	if exists {
		if marker.PlanID != request.PlanID || marker.PlanSHA != request.PlanSHA {
			return RecoveryResult{}, &RecoveryConflictError{PlanID: marker.PlanID, PlanSHA: marker.PlanSHA, Reason: "another plan is pending"}
		}
		if marker.CandidateStateSHA256 != prepared.stateSHA256 || marker.CandidateJournalSHA256 != prepared.journalSHA256 {
			return RecoveryResult{}, &RecoveryInputDriftError{Artifact: "pending_candidate", Expected: marker.CandidateStateSHA256 + "/" + marker.CandidateJournalSHA256, Actual: prepared.stateSHA256 + "/" + prepared.journalSHA256}
		}
		return completePendingRecovery(prepared, marker, true)
	}

	applied, err := findAppliedRecovery(request)
	if err != nil {
		return RecoveryResult{}, err
	}
	if applied != nil {
		if applied.PlanID != request.PlanID || applied.PlanSHA != request.PlanSHA {
			return RecoveryResult{}, &RecoveryConflictError{PlanID: applied.PlanID, PlanSHA: applied.PlanSHA, Reason: "a different plan was already applied"}
		}
		if applied.CandidateStateSHA256 != prepared.stateSHA256 || applied.CandidateJournalSHA256 != prepared.journalSHA256 {
			return RecoveryResult{}, &RecoveryConflictError{PlanID: applied.PlanID, PlanSHA: applied.PlanSHA, Reason: "applied candidate hashes differ"}
		}
		stateData, stateExists, err := readOptionalFile(request.StatePath)
		if err != nil {
			return RecoveryResult{}, err
		}
		journalData, journalExists, err := readOptionalFile(request.JournalPath)
		if err != nil {
			return RecoveryResult{}, err
		}
		if stateExists && journalExists && sha256Hex(stateData) == prepared.stateSHA256 && sha256Hex(journalData) == prepared.journalSHA256 {
			return recoveryResult(request, *applied, true, false), nil
		}
		return RecoveryResult{}, &RecoveryConflictError{PlanID: request.PlanID, PlanSHA: request.PlanSHA, Reason: "active pair differs from applied manifest"}
	}

	stateData, stateExists, err := readOptionalFile(request.StatePath)
	if err != nil {
		return RecoveryResult{}, err
	}
	journalData, journalExists, err := readOptionalFile(request.JournalPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := verifyRecoverySourceExists("source_state", request.SourceStateExists, stateExists); err != nil {
		return RecoveryResult{}, err
	}
	if err := verifyRecoverySourceExists("source_journal", request.SourceJournalExists, journalExists); err != nil {
		return RecoveryResult{}, err
	}
	if err := verifyDeclaredHash("source_state", request.SourceStateSHA256, optionalHash(stateData, stateExists)); err != nil {
		return RecoveryResult{}, err
	}
	if err := verifyDeclaredHash("source_journal", request.SourceJournalSHA256, optionalHash(journalData, journalExists)); err != nil {
		return RecoveryResult{}, err
	}
	pendingSources, err := inspectRecoveryPendingSources(request)
	if err != nil {
		return RecoveryResult{}, err
	}

	if err := injectRecoveryFailure(request, RecoveryBeforeQuarantine); err != nil {
		return RecoveryResult{}, err
	}
	quarantineDir, sourceState, sourceJournal, sourcePending, err := ensureRecoveryQuarantine(request, stateData, stateExists, journalData, journalExists, pendingSources)
	if err != nil {
		return RecoveryResult{}, err
	}
	manifest := RecoveryManifest{
		SchemaVersion:          "1.0.0",
		RecoveryID:             recoveryID(request),
		Status:                 "applying",
		PlanID:                 request.PlanID,
		PlanSHA:                request.PlanSHA,
		StatePath:              recoveryPath(request.Root, request.StatePath),
		JournalPath:            recoveryPath(request.Root, request.JournalPath),
		QuarantineDir:          quarantineDir,
		Approver:               request.Approver,
		OccurredAt:             request.OccurredAt.UTC().Format(time.RFC3339Nano),
		SourceState:            sourceState,
		SourceJournal:          sourceJournal,
		SourcePending:          sourcePending,
		CandidateStateSHA256:   prepared.stateSHA256,
		CandidateJournalSHA256: prepared.journalSHA256,
	}
	marker = recoveryPendingMarker{
		SchemaVersion:          "1.0.0",
		PlanID:                 request.PlanID,
		PlanSHA:                request.PlanSHA,
		StatePath:              recoveryPath(request.Root, request.StatePath),
		JournalPath:            recoveryPath(request.Root, request.JournalPath),
		QuarantineDir:          quarantineDir,
		CandidateStateBase64:   base64.StdEncoding.EncodeToString(prepared.stateBytes),
		CandidateJournalBase64: base64.StdEncoding.EncodeToString(prepared.journalBytes),
		CandidateStateSHA256:   prepared.stateSHA256,
		CandidateJournalSHA256: prepared.journalSHA256,
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

func verifyRecoverySourceExists(artifact string, expected *bool, actual bool) error {
	if expected == nil || *expected == actual {
		return nil
	}
	want := "missing"
	if *expected {
		want = "present"
	}
	got := "missing"
	if actual {
		got = "present"
	}
	return &RecoveryInputDriftError{Artifact: artifact + "_existence", Expected: want, Actual: got}
}

func inspectRecoveryPendingSources(request RecoveryRequest) ([]recoveryPendingSource, error) {
	known := knownRecoverySourcePendingPaths(request.StatePath)
	knownSet := make(map[string]struct{}, len(known))
	for _, path := range known {
		knownSet[filepath.Clean(path)] = struct{}{}
	}
	for path := range request.SourcePendingSHA256 {
		if _, ok := knownSet[filepath.Clean(path)]; !ok {
			return nil, fmt.Errorf("%w: unsupported source pending path %s", ErrRecoveryCandidateInvalid, path)
		}
	}

	sources := make([]recoveryPendingSource, 0, len(known))
	for _, path := range known {
		data, exists, err := readOptionalFile(path)
		if err != nil {
			return nil, err
		}
		expected, planned := request.SourcePendingSHA256[path]
		if exists != planned {
			want := "missing"
			if planned {
				want = "present"
			}
			got := "missing"
			if exists {
				got = "present"
			}
			return nil, &RecoveryInputDriftError{Artifact: recoveryPath(request.Root, path) + "_existence", Expected: want, Actual: got}
		}
		if !exists {
			continue
		}
		actual := sha256Hex(data)
		if actual != expected {
			return nil, &RecoveryInputDriftError{Artifact: recoveryPath(request.Root, path), Expected: expected, Actual: actual}
		}
		sources = append(sources, recoveryPendingSource{path: path, data: data})
	}
	return sources, nil
}

// KnownRecoverySourcePendingPaths returns the canonical list of Runtime
// pending marker paths that a recovery plan can declare in
// SourcePendingSHA256. Exported for tests and operator tooling.
func KnownRecoverySourcePendingPaths(statePath string) []string {
	return knownRecoverySourcePendingPaths(statePath)
}

func knownRecoverySourcePendingPaths(statePath string) []string {
	return []string{
		statePath + ".commit-pending.json",
		statePath + ".fingerprint-pending.json",
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
	if sha256Hex(stateBytes) != marker.CandidateStateSHA256 || sha256Hex(journalBytes) != marker.CandidateJournalSHA256 {
		return RecoveryResult{}, fmt.Errorf("%w: pending candidate hash mismatch", ErrRecoveryPending)
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
	if !stateExists || sha256Hex(stateData) != marker.CandidateStateSHA256 {
		if err := injectRecoveryFailure(request, RecoveryBeforeStateReplace); err != nil {
			return RecoveryResult{}, err
		}
		if err := atomicWriteBytes(request.StatePath, stateBytes, ".loop-recovery-state-*.tmp"); err != nil {
			return RecoveryResult{}, fmt.Errorf("replace recovery state: %w", err)
		}
		if err := injectRecoveryFailure(request, RecoveryAfterStateReplace); err != nil {
			return RecoveryResult{}, err
		}
		stateData, stateExists, err = readOptionalFile(request.StatePath)
		if err != nil {
			return RecoveryResult{}, err
		}
		if !stateExists || sha256Hex(stateData) != marker.CandidateStateSHA256 {
			return RecoveryResult{}, &RecoveryOutputMismatchError{Artifact: "active_state", Expected: marker.CandidateStateSHA256, Actual: optionalHash(stateData, stateExists)}
		}
	}

	journalData, journalExists, err := readOptionalFile(request.JournalPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	if !journalExists || sha256Hex(journalData) != marker.CandidateJournalSHA256 {
		if err := injectRecoveryFailure(request, RecoveryBeforeJournalReplace); err != nil {
			return RecoveryResult{}, err
		}
		if err := atomicWriteBytes(request.JournalPath, journalBytes, ".loop-recovery-journal-*.tmp"); err != nil {
			return RecoveryResult{}, fmt.Errorf("replace recovery journal: %w", err)
		}
		if err := injectRecoveryFailure(request, RecoveryAfterJournalReplace); err != nil {
			return RecoveryResult{}, err
		}
		journalData, journalExists, err = readOptionalFile(request.JournalPath)
		if err != nil {
			return RecoveryResult{}, err
		}
		if !journalExists || sha256Hex(journalData) != marker.CandidateJournalSHA256 {
			return RecoveryResult{}, &RecoveryOutputMismatchError{Artifact: "active_journal", Expected: marker.CandidateJournalSHA256, Actual: optionalHash(journalData, journalExists)}
		}
	}
	if err := retireRecoverySourcePending(request, marker.Manifest.SourcePending); err != nil {
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

	if err := injectRecoveryFailure(request, RecoveryBeforeManifest); err != nil {
		return RecoveryResult{}, err
	}
	manifestPath := recoveryManifestPath(request)
	manifest, err := appliedManifestFor(marker.Manifest, manifestPath)
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := writeImmutableJSON(manifestPath, manifest); err != nil {
		return RecoveryResult{}, fmt.Errorf("write recovery manifest: %w", err)
	}
	if err := os.Remove(request.StatePath + ".recovery-pending.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return RecoveryResult{}, fmt.Errorf("clear recovery pending marker: %w", err)
	}
	if err := syncDir(filepath.Dir(request.StatePath)); err != nil {
		return RecoveryResult{}, fmt.Errorf("sync recovery active directory: %w", err)
	}
	return recoveryResult(request, manifest, false, retried), nil
}

func verifyPostApplyPendingRetired(request RecoveryRequest) error {
	for _, path := range knownRecoverySourcePendingPaths(request.StatePath) {
		data, exists, err := readOptionalFile(path)
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
			PlanID:  request.PlanID,
			PlanSHA: request.PlanSHA,
			Reason:  fmt.Sprintf("Runtime pending marker %s survived atomic replace; run `runtime reconcile` to retire it (sha256=%s)", relative, sha256Hex(data)),
		}
	}
	return nil
}

func retireRecoverySourcePending(request RecoveryRequest, artifacts []RecoveryArtifact) error {
	byPath := make(map[string]RecoveryArtifact, len(artifacts))
	for _, artifact := range artifacts {
		byPath[artifact.Path] = artifact
	}
	removed := false
	for _, path := range knownRecoverySourcePendingPaths(request.StatePath) {
		relative := recoveryPath(request.Root, path)
		artifact, planned := byPath[relative]
		data, exists, err := readOptionalFile(path)
		if err != nil {
			return err
		}
		if !planned {
			if exists {
				return &RecoveryConflictError{PlanID: request.PlanID, PlanSHA: request.PlanSHA, Reason: "unexpected Runtime pending marker appeared during recovery"}
			}
			continue
		}
		if !artifact.Exists || artifact.SHA256 == "" {
			return fmt.Errorf("%w: pending source artifact is incomplete", ErrRecoveryPending)
		}
		if !exists {
			// A retry may observe a marker already retired after both candidate
			// files were replaced but before the applied manifest was written.
			continue
		}
		if sha256Hex(data) != artifact.SHA256 {
			return &RecoveryInputDriftError{Artifact: relative, Expected: artifact.SHA256, Actual: sha256Hex(data)}
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

func appliedManifestFor(pending RecoveryManifest, path string) (RecoveryManifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		pending.Status = "applied"
		pending.AppliedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return pending, nil
	}
	if err != nil {
		return RecoveryManifest{}, fmt.Errorf("read existing recovery manifest: %w", err)
	}
	var existing RecoveryManifest
	if err := json.Unmarshal(data, &existing); err != nil {
		return RecoveryManifest{}, fmt.Errorf("decode existing recovery manifest: %w", err)
	}
	if existing.Status != "applied" || existing.PlanID != pending.PlanID || existing.PlanSHA != pending.PlanSHA || existing.CandidateStateSHA256 != pending.CandidateStateSHA256 || existing.CandidateJournalSHA256 != pending.CandidateJournalSHA256 {
		return RecoveryManifest{}, &RecoveryConflictError{PlanID: pending.PlanID, PlanSHA: pending.PlanSHA, Reason: "existing recovery manifest differs"}
	}
	return existing, nil
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
	if marker.SchemaVersion != "1.0.0" || marker.PlanID == "" || marker.PlanSHA == "" {
		return recoveryPendingMarker{}, false, fmt.Errorf("%w: recovery pending marker is incomplete", ErrRecoveryPending)
	}
	return marker, true, nil
}

func findAppliedRecovery(request RecoveryRequest) (*RecoveryManifest, error) {
	dir := filepath.Join(request.Root, ".claude", "recovery", "manifests")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read recovery manifests: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read recovery manifest %s: %w", entry.Name(), err)
		}
		var manifest RecoveryManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode recovery manifest %s: %w", entry.Name(), err)
		}
		if manifest.Status != "applied" {
			continue
		}
		if manifest.PlanID != request.PlanID || manifest.PlanSHA != request.PlanSHA {
			// Applied manifests describe completed recovery epochs. They are
			// historical lineage, not a global mutex. Only a pending marker
			// represents an in-flight recovery that can conflict.
			continue
		}
		return &manifest, nil
	}
	return nil, nil
}

func ensureRecoveryQuarantine(request RecoveryRequest, stateData []byte, stateExists bool, journalData []byte, journalExists bool, pendingSources []recoveryPendingSource) (string, RecoveryArtifact, RecoveryArtifact, []RecoveryArtifact, error) {
	base := filepath.Join(request.Root, ".claude", "recovery", "quarantine")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("create recovery quarantine root: %w", err)
	}
	dir := filepath.Join(base, safeRecoveryComponent(request.PlanID)+"-"+safeRecoveryComponent(request.PlanSHA))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("create recovery quarantine: %w", err)
	}

	stateArtifact := recoveryArtifact(request.Root, request.StatePath, stateData, stateExists, filepath.Join(dir, "loop-state.json"))
	journalArtifact := recoveryArtifact(request.Root, request.JournalPath, journalData, journalExists, filepath.Join(dir, "loop-events.jsonl"))
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
		name := "commit-pending.json"
		if strings.HasSuffix(source.path, ".fingerprint-pending.json") {
			name = "fingerprint-pending.json"
		} else if strings.HasSuffix(source.path, ".rollover-pending.json") {
			name = "rollover-pending.json"
		}
		artifact := recoveryArtifact(request.Root, source.path, source.data, true, filepath.Join(dir, name))
		if err := writeImmutableBytes(artifact.QuarantinePath, source.data); err != nil {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("quarantine pending marker: %w", err)
		}
		pendingArtifacts = append(pendingArtifacts, artifact)
	}
	manifest := quarantineManifest{
		SchemaVersion:          "1.0.0",
		PlanID:                 request.PlanID,
		PlanSHA:                request.PlanSHA,
		CreatedAt:              request.OccurredAt.UTC().Format(time.RFC3339Nano),
		CandidateStateSHA256:   request.CandidateStateSHA256,
		CandidateJournalSHA256: request.CandidateJournalSHA256,
		State:                  stateArtifact,
		Journal:                journalArtifact,
		Pending:                pendingArtifacts,
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if data, readErr := os.ReadFile(manifestPath); readErr == nil {
		var existing quarantineManifest
		if err := json.Unmarshal(data, &existing); err != nil {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("decode existing quarantine manifest: %w", err)
		}
		if existing.SchemaVersion != manifest.SchemaVersion || existing.PlanID != manifest.PlanID || existing.PlanSHA != manifest.PlanSHA || existing.CandidateStateSHA256 != manifest.CandidateStateSHA256 || existing.CandidateJournalSHA256 != manifest.CandidateJournalSHA256 || existing.State != manifest.State || existing.Journal != manifest.Journal || !equalRecoveryArtifacts(existing.Pending, manifest.Pending) {
			return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("%w: existing quarantine manifest differs", ErrRecoveryConflict)
		}
		// The first durable quarantine owns its creation timestamp. A retry is
		// the same recovery operation even when the caller's wall clock changed.
		manifest = existing
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("read existing quarantine manifest: %w", readErr)
	}
	if err := writeImmutableJSON(manifestPath, manifest); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("write quarantine manifest: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return "", RecoveryArtifact{}, RecoveryArtifact{}, nil, fmt.Errorf("sync recovery quarantine: %w", err)
	}
	return dir, stateArtifact, journalArtifact, pendingArtifacts, nil
}

func equalRecoveryArtifacts(left, right []RecoveryArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func recoveryArtifact(root, path string, data []byte, exists bool, quarantinePath string) RecoveryArtifact {
	artifact := RecoveryArtifact{Path: recoveryPath(root, path), Exists: exists}
	if exists {
		artifact.SHA256 = sha256Hex(data)
		artifact.Size = int64(len(data))
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
		SchemaVersion:          manifest.SchemaVersion,
		PlanID:                 manifest.PlanID,
		PlanSHA:                manifest.PlanSHA,
		CreatedAt:              manifest.OccurredAt,
		CandidateStateSHA256:   manifest.CandidateStateSHA256,
		CandidateJournalSHA256: manifest.CandidateJournalSHA256,
		State:                  manifest.SourceState,
		Journal:                manifest.SourceJournal,
		Pending:                manifest.SourcePending,
	}
	if quarantine.SchemaVersion != expected.SchemaVersion || quarantine.PlanID != expected.PlanID || quarantine.PlanSHA != expected.PlanSHA || quarantine.CandidateStateSHA256 != expected.CandidateStateSHA256 || quarantine.CandidateJournalSHA256 != expected.CandidateJournalSHA256 {
		return errors.New("quarantine manifest identity does not match recovery manifest")
	}
	if !reflect.DeepEqual(quarantine.State, expected.State) || !reflect.DeepEqual(quarantine.Journal, expected.Journal) {
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
		data, err := os.ReadFile(artifact.QuarantinePath)
		if err != nil {
			return err
		}
		if sha256Hex(data) != artifact.SHA256 {
			return fmt.Errorf("quarantine artifact %s hash mismatch", artifact.Path)
		}
	}
	return nil
}

func sameRecoveryArtifacts(left, right []RecoveryArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !reflect.DeepEqual(left[index], right[index]) {
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

func optionalHash(data []byte, exists bool) string {
	if !exists {
		return ""
	}
	return sha256Hex(data)
}

func writeImmutableBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create immutable artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read existing immutable artifact: %w", readErr)
		}
		if !bytes.Equal(existing, data) {
			return errors.New("immutable artifact already exists with different bytes")
		}
		return nil
	}
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

func recoveryManifestPath(request RecoveryRequest) string {
	return filepath.Join(request.Root, ".claude", "recovery", "manifests", safeRecoveryComponent(request.PlanID)+"-"+safeRecoveryComponent(request.PlanSHA)+".json")
}

func recoveryID(request RecoveryRequest) string {
	return safeRecoveryComponent(request.PlanID) + "-" + safeRecoveryComponent(request.PlanSHA)
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
