package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UnbindApproval authenticates a human-initiated unbind: the authorization
// for one bound REQ is revoked, the runtime is archived mid-flight with
// disposition "unbound", and a fresh inactive runtime takes its place.
type UnbindApproval struct {
	ApprovedBy string
	EvidenceID string
	Reason     string
	// Forced records that the human overrode the in-flight soft gate; the
	// abandoned entities are listed in InFlight so the archive carries the
	// visible abandonment, not just the label.
	Forced   bool
	InFlight []string
}

// Unbind revokes the current binding from any non-terminal lifecycle state.
// It mirrors Rollover's crash-safe archive-and-restart mechanics but inverts
// the precondition: rollover closes a finished period, unbind abandons an
// open one. The archived runtime keeps disposition "unbound", which the
// bindable computation treats as "returned to the pool" (unlike terminal
// archives, which close the REQ's lifecycle).
func (s *Store) Unbind(freshState map[string]any, archiveRoot string, approval UnbindApproval, occurredAt time.Time) (RolloverRecord, error) {
	if err := s.requireCandidateValidator(); err != nil {
		return RolloverRecord{}, err
	}
	if strings.TrimSpace(approval.ApprovedBy) == "" {
		return RolloverRecord{}, errors.New("unbind approval is required")
	}
	if strings.TrimSpace(approval.EvidenceID) == "" {
		return RolloverRecord{}, errors.New("unbind approval evidence is required")
	}
	if strings.TrimSpace(approval.Reason) == "" {
		return RolloverRecord{}, errors.New("unbind reason is required")
	}
	if len(freshState) == 0 {
		return RolloverRecord{}, errors.New("fresh runtime state is required")
	}
	if err := ValidateFreshInactiveState(freshState); err != nil {
		return RolloverRecord{}, fmt.Errorf("validate fresh runtime: %w", err)
	}
	if err := s.validateCandidate(freshState); err != nil {
		return RolloverRecord{}, fmt.Errorf("validate fresh runtime semantics: %w", err)
	}
	if strings.TrimSpace(archiveRoot) == "" {
		return RolloverRecord{}, errors.New("runtime archive path is required")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return RolloverRecord{}, err
	}
	defer release()
	if err := s.recoverPendingWritesLocked(); err != nil {
		return RolloverRecord{}, err
	}

	stateData, err := os.ReadFile(s.statePath)
	if err != nil {
		return RolloverRecord{}, fmt.Errorf("read runtime for unbind: %w", err)
	}
	journalData, err := os.ReadFile(s.journalPath)
	if err != nil {
		return RolloverRecord{}, fmt.Errorf("read runtime journal for unbind: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		return RolloverRecord{}, fmt.Errorf("decode runtime for unbind: %w", err)
	}
	if err := s.validateCandidate(state); err != nil {
		return RolloverRecord{}, fmt.Errorf("validate current runtime for unbind: %w", err)
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	if lifecycleState == "" {
		return RolloverRecord{}, errors.New("lifecycle state is required for unbind")
	}
	if isRolloverTerminalState(lifecycleState) {
		return RolloverRecord{}, fmt.Errorf("runtime unbind is for open lifecycles, current state is terminal %q — use rollover instead", lifecycleState)
	}
	if lifecycleState == "inactive" {
		return RolloverRecord{}, errors.New("runtime unbind requires a bound REQ, current state is inactive")
	}
	bound, _ := state["bound_req"].(map[string]any)
	boundID, _ := bound["id"].(string)
	if boundID == "" {
		return RolloverRecord{}, errors.New("runtime unbind requires a bound REQ")
	}
	runtimeID, _ := state["runtime_id"].(string)
	if runtimeID == "" {
		return RolloverRecord{}, errors.New("runtime id is required for unbind")
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return RolloverRecord{}, err
	}
	if err := validateLifecycleApproval(state, approval.ApprovedBy, approval.EvidenceID, runtimeID, revision, "runtime_unbind"); err != nil {
		return RolloverRecord{}, fmt.Errorf("unbind approval: %w", err)
	}

	extras := map[string]any{"disposition": "unbound", "reason": approval.Reason, "unbound_req": boundID}
	if approval.Forced {
		extras["forced"] = true
	}
	if len(approval.InFlight) > 0 {
		extras["in_flight_entities"] = approval.InFlight
	}
	return s.archiveAndReset(stateData, journalData, runtimeID, revision, freshState, archiveRoot,
		extras, "unbound", RolloverApproval{ApprovedBy: approval.ApprovedBy, EvidenceID: approval.EvidenceID}, occurredAt)
}

// archiveAndReset performs the shared crash-safe tail of Rollover and
// Unbind: seal the current state+journal into a timestamped archive dir
// with a manifest, record the pending marker (carrying the disposition the
// recovery-side approval check needs), then run the recovery that writes
// the fresh replacement runtime.
func (s *Store) archiveAndReset(stateData, journalData []byte, runtimeID string, revision int, freshState map[string]any, archiveRoot string, manifestExtras map[string]any, disposition string, approval RolloverApproval, occurredAt time.Time) (RolloverRecord, error) {
	return s.archiveAndResetWithBoundary(stateData, journalData, runtimeID, revision, freshState, archiveRoot, manifestExtras, disposition, "", approval, occurredAt)
}

func (s *Store) archiveAndResetWithBoundary(stateData, journalData []byte, runtimeID string, revision int, freshState map[string]any, archiveRoot string, manifestExtras map[string]any, disposition, boundaryKind string, approval RolloverApproval, occurredAt time.Time) (RolloverRecord, error) {
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return RolloverRecord{}, fmt.Errorf("create runtime archive root: %w", err)
	}
	if err := syncDir(filepath.Dir(archiveRoot)); err != nil {
		return RolloverRecord{}, fmt.Errorf("sync runtime archive parent: %w", err)
	}
	stamp := occurredAt.UTC().Format("20060102T150405.000000000Z")
	archiveDir := filepath.Join(archiveRoot, fmt.Sprintf("%s-r%d-%s", runtimeID, revision, stamp))
	if err := os.Mkdir(archiveDir, 0o755); err != nil {
		return RolloverRecord{}, fmt.Errorf("create runtime archive: %w", err)
	}
	if err := syncDir(archiveRoot); err != nil {
		return RolloverRecord{}, fmt.Errorf("sync runtime archive root: %w", err)
	}
	if err := writeDurableFile(filepath.Join(archiveDir, "loop-state.json"), stateData); err != nil {
		return RolloverRecord{}, fmt.Errorf("archive runtime state: %w", err)
	}
	if err := writeDurableFile(filepath.Join(archiveDir, "loop-events.jsonl"), journalData); err != nil {
		return RolloverRecord{}, fmt.Errorf("archive runtime journal: %w", err)
	}
	stateHash := sha256Hex(stateData)
	journalHash := sha256Hex(journalData)
	manifest := map[string]any{
		"runtime_id":           runtimeID,
		"revision":             revision,
		"approved_by":          approval.ApprovedBy,
		"approval_evidence_id": approval.EvidenceID,
		"state_sha256":         stateHash,
		"journal_sha256":       journalHash,
		"occurred_at":          occurredAt.UTC().Format(time.RFC3339Nano),
	}
	for key, value := range manifestExtras {
		manifest[key] = value
	}
	if err := atomicWriteJSON(filepath.Join(archiveDir, "rollover.json"), manifest); err != nil {
		return RolloverRecord{}, fmt.Errorf("write runtime archive manifest: %w", err)
	}
	pending := rolloverPending{
		SchemaVersion: "1.0.0",
		FreshState:    freshState,
		Record: RolloverRecord{
			ArchiveDir: archiveDir, RuntimeID: runtimeID, Revision: revision,
			ArchiveStateSHA: stateHash, ArchiveJournalSHA: journalHash,
		},
		Approval:            approval,
		BoundaryKind:        boundaryKind,
		Disposition:         disposition,
		OccurredAt:          occurredAt.UTC().Format(time.RFC3339Nano),
		SourceStateSHA256:   stateHash,
		SourceJournalSHA256: journalHash,
		SourceRuntimeID:     runtimeID,
		SourceRevision:      intPointer(revision),
	}
	if err := atomicWriteJSON(s.rolloverMarkerPath(), pending); err != nil {
		return RolloverRecord{}, fmt.Errorf("record pending runtime rollover: %w", err)
	}
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return RolloverRecord{}, err
	}
	return pending.Record, nil
}
