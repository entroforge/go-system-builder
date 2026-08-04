package runtime

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var ErrStaleRevision = errors.New("stale runtime revision")

const staleLockAge = 30 * time.Second

type Mutation struct {
	EventID        string
	TransitionID   string
	Event          string
	Actor          string
	IdempotencyKey string
	RuntimeID      string
	From           map[string]any
	To             map[string]any
	EvidenceIDs    []string
	GuardResults   []map[string]any
	ActionResults  []map[string]any
	Message        string
	OccurredAt     time.Time
	// Journal audit fields (SYNC-039 §7 / BE-039 §7.3).
	RequestID              string
	BaselineGeneration     int
	GateID                 string
	GateFingerprint        string
	ProducerResponsibility string
	// JournalEvent overrides the persisted journal `event` field. Defaults to
	// transition_committed when empty.
	JournalEvent string
	// JournalOutcome overrides the persisted journal `outcome` field. Defaults
	// to committed when empty.
	JournalOutcome string
	// RequireEmptyJournal is used by TR-001. Binding may only start from the
	// canonical fresh runtime pair, not from a hand-edited state file that
	// still points at a previous runtime's journal.
	RequireEmptyJournal bool
	// RetainLastTransition keeps the existing last_transition snapshot when
	// the mutation is not a legality lifecycle commit (e.g. milestone refresh).
	RetainLastTransition bool
	Apply                func(state map[string]any) error
}

type Snapshot struct {
	Revision int
	State    map[string]any
}

// RolloverRecord describes the human-authorized archive created before a
// terminal runtime is replaced by a fresh inactive runtime.
type RolloverRecord struct {
	ArchiveDir        string `json:"archive_dir"`
	RuntimeID         string `json:"runtime_id"`
	Revision          int    `json:"revision"`
	ArchiveStateSHA   string `json:"archive_state_sha256"`
	ArchiveJournalSHA string `json:"archive_journal_sha256"`
}

// RolloverApproval identifies the existing human-decision evidence that
// authorizes a terminal runtime to be archived and replaced. The harness
// verifies the evidence linkage; authenticating the human identity itself is
// intentionally outside this local-file runtime's trust boundary.
type RolloverApproval struct {
	ApprovedBy string `json:"approved_by"`
	EvidenceID string `json:"evidence_id"`
}

type rolloverPending struct {
	SchemaVersion string           `json:"schema_version"`
	FreshState    map[string]any   `json:"fresh_state"`
	Record        RolloverRecord   `json:"record"`
	Approval      RolloverApproval `json:"approval"`
	OccurredAt    string           `json:"occurred_at"`
}

type Store struct {
	statePath   string
	journalPath string
	// PreCommitValidator, when set, is invoked on the post-mutation state
	// immediately before the atomic write. It must return an error if the
	// state is invalid; the store then refuses to commit. Defaults to nil.
	PreCommitValidator func(state map[string]any) error
}

func NewStore(statePath, journalPath string) *Store {
	return &Store{statePath: statePath, journalPath: journalPath}
}

// Snapshot reads the current runtime revision while holding the runtime lock.
// Callers still pass the returned revision into Update, which retains the CAS
// check if another writer commits after this snapshot is released.
func (s *Store) Snapshot() (Snapshot, error) {
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return Snapshot{}, err
	}

	state, err := s.read()
	if err != nil {
		return Snapshot{}, err
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: revision, State: state}, nil
}

// Rollover archives a terminal runtime and its journal, then replaces the
// active state and journal with a supplied fresh runtime. A durable pending
// marker makes the two-file replacement recoverable after a process crash. It is
// deliberately not a Loop Definition transition: terminal Loop states have no
// automated exit, so a human authorization is required to start another REQ.
func (s *Store) Rollover(freshState map[string]any, archiveRoot string, approval RolloverApproval, occurredAt time.Time) (RolloverRecord, error) {
	if strings.TrimSpace(approval.ApprovedBy) == "" {
		return RolloverRecord{}, errors.New("rollover approval is required")
	}
	if strings.TrimSpace(approval.EvidenceID) == "" {
		return RolloverRecord{}, errors.New("rollover approval evidence is required")
	}
	if len(freshState) == 0 {
		return RolloverRecord{}, errors.New("fresh runtime state is required")
	}
	if err := ValidateFreshInactiveState(freshState); err != nil {
		return RolloverRecord{}, fmt.Errorf("validate fresh runtime: %w", err)
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
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return RolloverRecord{}, err
	}

	stateData, err := os.ReadFile(s.statePath)
	if err != nil {
		return RolloverRecord{}, fmt.Errorf("read runtime for rollover: %w", err)
	}
	journalData, err := os.ReadFile(s.journalPath)
	if err != nil {
		return RolloverRecord{}, fmt.Errorf("read runtime journal for rollover: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		return RolloverRecord{}, fmt.Errorf("decode runtime for rollover: %w", err)
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	if lifecycleState != "awaiting_human_release" && lifecycleState != "aborted" {
		return RolloverRecord{}, fmt.Errorf("runtime rollover requires terminal state, current state is %q", lifecycleState)
	}
	runtimeID, _ := state["runtime_id"].(string)
	if runtimeID == "" {
		return RolloverRecord{}, errors.New("runtime id is required for rollover")
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return RolloverRecord{}, err
	}
	if err := validateRolloverApproval(state, approval, runtimeID, revision); err != nil {
		return RolloverRecord{}, err
	}

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
		Approval:   approval,
		OccurredAt: occurredAt.UTC().Format(time.RFC3339Nano),
	}
	if err := atomicWriteJSON(s.rolloverMarkerPath(), pending); err != nil {
		return RolloverRecord{}, fmt.Errorf("record pending runtime rollover: %w", err)
	}
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return RolloverRecord{}, err
	}
	return pending.Record, nil
}

// DocumentMetadataVersion extracts the authoritative version string from a
// JSON document referenced by the runtime state.
//
// The document's own `version` field wins over `schema_version` (BUG-039-12
// repair). `version` is the artifact's semantic version — the thing an audit
// trail cares about, e.g. `docs/hook-policy.json` carries
// `version: "v2.0.0"` alongside `schema_version: "1.2.0"` (the wire-format
// version of the policy schema). The previous heuristic preferred
// `schema_version`, which wrote the schema format version into the policy
// version slot of `hook_control.policy_ref` and made the runtime record a
// version the policy file never declared.
//
// `schema_version` remains the fallback for documents that declare no
// `version` at all — `docs/loop-definition.json` is exactly this shape, so
// `definition.version` continues to track its `schema_version`.
//
// An empty return means the document declares neither field (or is not JSON);
// callers must treat that as "no version information" and leave state alone.
func DocumentMetadataVersion(data []byte) string {
	var metadata struct {
		SchemaVersion string `json:"schema_version"`
		Version       string `json:"version"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ""
	}
	if metadata.Version != "" {
		return metadata.Version
	}
	return metadata.SchemaVersion
}

// FingerprintResult summarises a RefreshFingerprints pass. Updated lists the
// document paths whose stored SHA256 changed; Unchanged lists paths that
// already matched. Missing lists paths whose on-disk file does not exist.
type FingerprintResult struct {
	Updated   []string
	Unchanged []string
	Missing   []string
}

// RefreshFingerprints recomputes the SHA256 of every document referenced by
// the runtime state and writes the updated state back atomically. It is the
// single source of truth for refreshing document fingerprints (BUG-004
// repair). It does NOT bump the runtime revision and does NOT append a
// journal entry — fingerprint refresh is a non-semantic housekeeping
// operation that should not trigger transition events.
func (s *Store) RefreshFingerprints(root string) (FingerprintResult, error) {
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return FingerprintResult{}, err
	}
	defer release()
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return FingerprintResult{}, err
	}

	state, err := s.read()
	if err != nil {
		return FingerprintResult{}, err
	}

	var result FingerprintResult
	refresh := func(entry map[string]any) {
		path, _ := entry["path"].(string)
		if path == "" {
			return
		}
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			result.Missing = append(result.Missing, path)
			return
		}
		sum := sha256Hex(data)
		current, _ := entry["sha256"].(string)
		if current == sum {
			result.Unchanged = append(result.Unchanged, path)
			return
		}
		entry["sha256"] = sum
		result.Updated = append(result.Updated, path)
	}
	refreshMetadataVersion := func(entry map[string]any) {
		path, _ := entry["path"].(string)
		if path == "" {
			return
		}
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return
		}
		version := DocumentMetadataVersion(data)
		if version == "" || entry["version"] == version {
			return
		}
		entry["version"] = version
		for index, unchangedPath := range result.Unchanged {
			if unchangedPath == path {
				result.Unchanged = append(result.Unchanged[:index], result.Unchanged[index+1:]...)
				break
			}
		}
		for _, updatedPath := range result.Updated {
			if updatedPath == path {
				return
			}
		}
		result.Updated = append(result.Updated, path)
	}

	if docs, ok := state["documents"].([]any); ok {
		for _, raw := range docs {
			if doc, ok := raw.(map[string]any); ok {
				refresh(doc)
			}
		}
	}
	if evidence, ok := state["evidence"].([]any); ok {
		for _, raw := range evidence {
			if entry, ok := raw.(map[string]any); ok {
				refresh(entry)
			}
		}
	}
	if boundReq, ok := state["bound_req"].(map[string]any); ok {
		refresh(boundReq)
	}
	// definition (loop-definition.json) and hook policy fingerprints must also
	// track the on-disk artifact — REQ-003 TASK-003-C upgrades the definition
	// file and validateRuntimeReferences compares state.Definition.SHA256
	// against the new on-disk hash. Without this refresh, validate fails closed
	// until a future REQ bind rewrites the whole runtime.
	if definition, ok := state["definition"].(map[string]any); ok {
		refresh(definition)
		refreshMetadataVersion(definition)
	}
	if hookControl, ok := state["hook_control"].(map[string]any); ok {
		if policyRef, ok := hookControl["policy_ref"].(map[string]any); ok {
			refresh(policyRef)
			refreshMetadataVersion(policyRef)
		}
	}
	if entities, ok := state["entities"].(map[string]any); ok {
		if tasks, ok := entities["tasks"].([]any); ok {
			for _, raw := range tasks {
				if task, ok := raw.(map[string]any); ok {
					refresh(task)
				}
			}
		}
	}

	if len(result.Updated) > 0 {
		if s.PreCommitValidator != nil {
			if err := s.PreCommitValidator(state); err != nil {
				return FingerprintResult{}, fmt.Errorf("post-refresh snapshot invalid: %w", err)
			}
		}
		if err := atomicWriteJSON(s.statePath, state); err != nil {
			return FingerprintResult{}, err
		}
	}
	return result, nil
}

// PolicyRefDrift reports whether the Hook policy reference recorded in
// `hook_control.policy_ref` still matches the policy document on disk.
//
// The runtime snapshots the Hook policy path/version/sha256 at bind time
// (REQ-039 §11, SYNC-039 §6-7): the recorded reference is what the audit
// trail claims the enforced safety boundary was. When the policy document is
// rewritten in place — e.g. the REQ-039 reduction to the minimal boundary
// bumped `docs/hook-policy.json` to `v2.0.0` — the snapshot silently goes
// stale and the runtime attributes decisions to a policy version that is no
// longer on disk. Detecting that divergence is the point of this type; the
// fix path is RefreshFingerprints, which rewrites both fields in place
// without bumping the revision.
type PolicyRefDrift struct {
	// Path is the policy document path recorded in state (repo-relative).
	Path string
	// Missing is true when state records no policy_ref at all.
	Missing bool
	// FileMissing is true when the recorded path does not exist on disk.
	FileMissing bool
	// RecordedVersion / OnDiskVersion hold the policy `version` values.
	RecordedVersion string
	OnDiskVersion   string
	// RecordedSHA256 / OnDiskSHA256 hold the policy document digests.
	RecordedSHA256 string
	OnDiskSHA256   string
}

// VersionDrifted reports a divergence between the recorded and on-disk policy
// version. An empty on-disk version is not drift: the document simply does not
// declare one, so there is nothing authoritative to compare against.
func (d PolicyRefDrift) VersionDrifted() bool {
	return d.OnDiskVersion != "" && d.RecordedVersion != d.OnDiskVersion
}

// SHADrifted reports a divergence between the recorded and on-disk digest.
func (d PolicyRefDrift) SHADrifted() bool {
	return d.OnDiskSHA256 != "" && d.RecordedSHA256 != d.OnDiskSHA256
}

// Drifted reports whether any actionable inconsistency was observed.
func (d PolicyRefDrift) Drifted() bool {
	return d.Missing || d.FileMissing || d.VersionDrifted() || d.SHADrifted()
}

// InspectPolicyRef loads the runtime state and compares `hook_control.policy_ref`
// against the on-disk policy document, resolving relative paths under root.
// It takes no lock and never writes: it is a read-only diagnostic used by
// `loop-harness doctor` and by `runtime reconcile-policy-ref` to decide whether
// a refresh is warranted.
func (s *Store) InspectPolicyRef(root string) (PolicyRefDrift, error) {
	state, err := s.read()
	if err != nil {
		return PolicyRefDrift{}, err
	}
	hookControl, ok := state["hook_control"].(map[string]any)
	if !ok {
		return PolicyRefDrift{Missing: true}, nil
	}
	policyRef, ok := hookControl["policy_ref"].(map[string]any)
	if !ok {
		return PolicyRefDrift{Missing: true}, nil
	}
	drift := PolicyRefDrift{}
	drift.Path, _ = policyRef["path"].(string)
	drift.RecordedVersion, _ = policyRef["version"].(string)
	drift.RecordedSHA256, _ = policyRef["sha256"].(string)
	if drift.Path == "" {
		drift.Missing = true
		return drift, nil
	}
	full := drift.Path
	if !filepath.IsAbs(full) {
		full = filepath.Join(root, full)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		drift.FileMissing = true
		return drift, nil
	}
	drift.OnDiskSHA256 = sha256Hex(data)
	drift.OnDiskVersion = DocumentMetadataVersion(data)
	return drift, nil
}

func (s *Store) Update(expectedRevision int, mutation Mutation) (Snapshot, error) {
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return Snapshot{}, err
	}
	return s.applyMutation(expectedRevision, mutation)
}

func (s *Store) Reconcile() (bool, error) {
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer release()
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return false, err
	}

	state, err := s.read()
	if err != nil {
		return false, err
	}
	transition, ok := state["last_transition"].(map[string]any)
	if !ok || transition == nil {
		return false, nil
	}
	eventID, _ := transition["event_id"].(string)
	if eventID == "" {
		return false, errors.New("last transition has no event_id")
	}
	found, err := journalContains(s.journalPath, eventID)
	if err != nil {
		return false, err
	}
	if found {
		return false, nil
	}
	runtimeID, _ := state["runtime_id"].(string)
	actor, _ := transition["actor"].(string)
	beforeRevision, _ := transition["expected_revision"].(float64)
	afterRevision, _ := transition["committed_revision"].(float64)
	event := buildJournalEvent(Mutation{
		EventID:            eventID,
		TransitionID:       stringValue(transition["transition_id"]),
		Actor:              nonEmpty(actor, "runtime-reconciler"),
		RuntimeID:          runtimeID,
		From:               mapValue(transition["from"]),
		To:                 mapValue(transition["to"]),
		EvidenceIDs:        stringSlice(transition["evidence_ids"]),
		RequestID:          "journal-reconcile",
		BaselineGeneration: baselineGeneration(state),
		JournalEvent:       "journal_reconciled",
		JournalOutcome:     "reconciled",
		Message:            "Reconciled a committed transition missing from the journal.",
		OccurredAt:         time.Now().UTC(),
	}, runtimeID, int(beforeRevision), int(afterRevision), intValue(transition["sequence"]), time.Now().UTC())
	if err := appendJSONLine(s.journalPath, event); err != nil {
		return false, err
	}
	return true, nil
}

// MigrateLegacyPlanning maps a legacy planning runtime phase to the formal
// design|contracts|tasks phase machine based on artifact presence. It is
// explicit-only (CLI migrate-planning); StageFor never scans artifacts.
func (s *Store) MigrateLegacyPlanning(root string) (bool, error) {
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer release()
	if err := s.recoverPendingRolloverLocked(); err != nil {
		return false, err
	}

	state, err := s.read()
	if err != nil {
		return false, err
	}
	lifecycle, err := objectField(state, "lifecycle")
	if err != nil {
		return false, err
	}
	currentState, _ := lifecycle["state"].(string)
	if currentState != "planning" {
		return false, nil
	}
	currentPhase := phaseString(lifecycle["phase"])
	targetPhase, err := ReconcileLegacyPlanningPhase(root)
	if err != nil {
		return false, err
	}
	if isFormalPlanningPhase(currentPhase) && currentPhase == targetPhase {
		return false, nil
	}

	expectedRevision, err := integerField(state, "revision")
	if err != nil {
		return false, err
	}
	runtimeID, _ := state["runtime_id"].(string)
	from := planningCursor(currentPhase)
	to := planningCursor(targetPhase)
	eventID := fmt.Sprintf("evt-migrate-planning-r%d", expectedRevision+1)

	mutation := Mutation{
		EventID:            eventID,
		Actor:              "runtime-migrator",
		IdempotencyKey:     fmt.Sprintf("runtime:migrate-planning:%d", expectedRevision),
		RuntimeID:          runtimeID,
		From:               from,
		To:                 to,
		RequestID:          "migrate-planning",
		BaselineGeneration: baselineGeneration(state),
		JournalEvent:       "planning_phase_migrated",
		JournalOutcome:     "migrated",
		Message:            fmt.Sprintf("Legacy planning phase migrated to %s.", targetPhase),
		OccurredAt:         time.Now().UTC(),
		Apply: func(state map[string]any) error {
			lifecycle, err := objectField(state, "lifecycle")
			if err != nil {
				return err
			}
			lifecycle["phase"] = targetPhase
			phaseRevision, err := integerField(lifecycle, "phase_revision")
			if err != nil {
				return err
			}
			lifecycle["phase_revision"] = phaseRevision + 1
			state["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			return nil
		},
	}
	if _, err := s.applyMutation(expectedRevision, mutation); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) applyMutation(expectedRevision int, mutation Mutation) (Snapshot, error) {
	state, err := s.read()
	if err != nil {
		return Snapshot{}, err
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return Snapshot{}, err
	}
	if revision != expectedRevision {
		return Snapshot{}, ErrStaleRevision
	}
	if mutation.RequireEmptyJournal {
		empty, err := journalEmpty(s.journalPath)
		if err != nil {
			return Snapshot{}, err
		}
		if !empty {
			return Snapshot{}, errors.New("requires a fresh inactive runtime with an empty journal")
		}
	}
	if mutation.Apply != nil {
		if err := mutation.Apply(state); err != nil {
			return Snapshot{}, err
		}
	}

	nextRevision := expectedRevision + 1
	state["revision"] = nextRevision
	journal, err := objectField(state, "journal")
	if err != nil {
		return Snapshot{}, err
	}
	lastSequence, err := integerField(journal, "last_sequence")
	if err != nil {
		return Snapshot{}, err
	}
	sequence := lastSequence + 1
	journal["last_sequence"] = sequence
	journal["last_event_id"] = mutation.EventID

	occurredAt := mutation.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if !mutation.RetainLastTransition {
		transition := map[string]any{
			"event_id":           mutation.EventID,
			"sequence":           sequence,
			"transition_id":      mutation.TransitionID,
			"event":              mutation.Event,
			"actor":              mutation.Actor,
			"from":               mutation.From,
			"to":                 mutation.To,
			"expected_revision":  expectedRevision,
			"committed_revision": nextRevision,
			"idempotency_key":    mutation.IdempotencyKey,
			"evidence_ids":       mutation.EvidenceIDs,
			"occurred_at":        occurredAt.UTC().Format(time.RFC3339Nano),
		}
		state["last_transition"] = transition
	}

	if s.PreCommitValidator != nil {
		if err := s.PreCommitValidator(state); err != nil {
			return Snapshot{}, fmt.Errorf("post-mutation snapshot invalid: %w", err)
		}
	}

	if err := atomicWriteJSON(s.statePath, state); err != nil {
		return Snapshot{}, err
	}
	runtimeID := mutation.RuntimeID
	if runtimeID == "" {
		runtimeID, _ = state["runtime_id"].(string)
	}
	journalEvent := buildJournalEvent(mutation, runtimeID, expectedRevision, nextRevision, sequence, occurredAt)
	if err := appendJSONLine(s.journalPath, journalEvent); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: nextRevision, State: state}, nil
}

func buildJournalEvent(mutation Mutation, runtimeID string, beforeRevision, afterRevision, sequence int, occurredAt time.Time) map[string]any {
	eventKind := mutation.JournalEvent
	if eventKind == "" {
		eventKind = "transition_committed"
	}
	outcome := mutation.JournalOutcome
	if outcome == "" {
		outcome = "committed"
	}
	requestID := mutation.RequestID
	if requestID == "" {
		requestID = "runtime-store"
	}
	baselineGen := mutation.BaselineGeneration
	if baselineGen < 1 {
		baselineGen = 1
	}
	event := map[string]any{
		"schema_version":      "1.0.0",
		"runtime_id":          runtimeID,
		"event_id":            mutation.EventID,
		"sequence":            sequence,
		"event":               eventKind,
		"outcome":             outcome,
		"actor":               map[string]any{"type": actorType(mutation.Actor), "id": mutation.Actor},
		"request_id":          requestID,
		"baseline_generation": baselineGen,
		"before_revision":     beforeRevision,
		"after_revision":      afterRevision,
		"from":                mutation.From,
		"to":                  mutation.To,
		"evidence_ids":        nonNilStrings(mutation.EvidenceIDs),
		"message":             nonEmpty(mutation.Message, "Transition committed."),
		"occurred_at":         occurredAt.UTC().Format(time.RFC3339Nano),
	}
	switch eventKind {
	case "transition_committed":
		event["transition_id"] = mutation.TransitionID
		event["gate_id"] = nonEmpty(mutation.GateID, "MANUAL")
		event["gate_fingerprint"] = nonEmpty(mutation.GateFingerprint, "sha256:manual")
		event["producer_responsibility"] = nonEmpty(mutation.ProducerResponsibility, mutation.Actor)
		event["guard_results"] = nonNilResults(mutation.GuardResults)
		event["action_results"] = nonNilResults(mutation.ActionResults)
	case "journal_reconciled":
		event["transition_id"] = mutation.TransitionID
		event["guard_results"] = nonNilResults(mutation.GuardResults)
		event["action_results"] = nonNilResults(mutation.ActionResults)
	default:
		event["transition_id"] = nil
		event["guard_results"] = []map[string]any{}
		event["action_results"] = []map[string]any{}
	}
	return event
}

func baselineGeneration(state map[string]any) int {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return 1
	}
	gen, err := integerField(baseline, "generation")
	if err != nil || gen < 1 {
		return 1
	}
	return gen
}

func isFormalPlanningPhase(phase string) bool {
	switch phase {
	case "design", "contracts", "tasks":
		return true
	default:
		return false
	}
}

func phaseString(value any) string {
	if value == nil {
		return ""
	}
	phase, _ := value.(string)
	return phase
}

func planningCursor(phase string) map[string]any {
	cursor := map[string]any{"state": "planning"}
	if phase == "" {
		cursor["phase"] = nil
	} else {
		cursor["phase"] = phase
	}
	return cursor
}

func mapValue(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func intValue(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	default:
		return 0
	}
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func actorType(actor string) string {
	switch actor {
	case "user", "orchestrator", "hook", "system":
		return actor
	default:
		return "agent"
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilResults(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}

func nonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (s *Store) read() (map[string]any, error) {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return nil, fmt.Errorf("read runtime: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime: %w", err)
	}
	return state, nil
}

func (s *Store) rolloverMarkerPath() string {
	return s.statePath + ".rollover-pending.json"
}

// recoverPendingRolloverLocked completes a rollover recorded before either
// member of the state/journal pair was replaced. Callers must already hold the
// state lock. The journal is reset first: a crash before the state write leaves
// the marker in place, so the next Store operation deterministically finishes
// the state write instead of accepting a mixed runtime pair.
func (s *Store) recoverPendingRolloverLocked() error {
	data, err := os.ReadFile(s.rolloverMarkerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending runtime rollover: %w", err)
	}
	var pending rolloverPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending runtime rollover: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending runtime rollover schema %q", pending.SchemaVersion)
	}
	if err := ValidateFreshInactiveState(pending.FreshState); err != nil {
		return fmt.Errorf("pending runtime rollover is invalid: %w", err)
	}
	if err := verifyRolloverArchive(pending.Record); err != nil {
		return fmt.Errorf("pending runtime rollover archive is invalid: %w", err)
	}
	if err := atomicWriteBytes(s.journalPath, nil, ".loop-journal-*.tmp"); err != nil {
		return fmt.Errorf("reset runtime journal during rollover recovery: %w", err)
	}
	if err := atomicWriteJSON(s.statePath, pending.FreshState); err != nil {
		return fmt.Errorf("seed fresh runtime during rollover recovery: %w", err)
	}
	if err := os.Remove(s.rolloverMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending runtime rollover: %w", err)
	}
	if err := syncDir(filepath.Dir(s.rolloverMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending runtime rollover: %w", err)
	}
	return nil
}

// ValidateFreshInactiveState verifies the canonical state half of a runtime
// pair that may begin TR-001. Store.Update additionally verifies that the
// corresponding journal file is empty before committing the bind.
func ValidateFreshInactiveState(state map[string]any) error {
	if state["runtime_id"] != "loop-inactive" {
		return errors.New("runtime_id must be loop-inactive")
	}
	if revision, err := integerField(state, "revision"); err != nil || revision != 0 {
		return errors.New("revision must be zero")
	}
	lifecycle, err := objectField(state, "lifecycle")
	if err != nil || lifecycle["state"] != "inactive" || lifecycle["phase"] != nil {
		return errors.New("lifecycle must be inactive without a phase")
	}
	if phaseRevision, err := integerField(lifecycle, "phase_revision"); err != nil || phaseRevision != 0 {
		return errors.New("lifecycle phase_revision must be zero")
	}
	journal, err := objectField(state, "journal")
	if err != nil {
		return errors.New("journal must be an object")
	}
	if path, _ := journal["path"].(string); path != ".claude/loop-events.jsonl" {
		return errors.New("journal path must be .claude/loop-events.jsonl")
	}
	if sequence, err := integerField(journal, "last_sequence"); err != nil || sequence != 0 || journal["last_event_id"] != nil {
		return errors.New("journal cursor must be empty")
	}
	if state["bound_req"] != nil || state["pause"] != nil || state["last_transition"] != nil || state["change"] != nil {
		return errors.New("runtime contains prior lifecycle state")
	}
	authorization, err := objectField(state, "authorization")
	if err != nil || authorization["mode"] != "none" {
		return errors.New("authorization must be none")
	}
	baseline, err := objectField(state, "baseline")
	if err != nil || baseline["captured_at"] != nil {
		return errors.New("baseline must be uncaptured")
	}
	if generation, err := integerField(baseline, "generation"); err != nil || generation != 0 {
		return errors.New("baseline generation must be zero")
	}
	review, err := objectField(state, "review")
	if err != nil || review["clean_round"] != nil {
		return errors.New("review must be empty")
	}
	if round, err := integerField(review, "round"); err != nil || round != 0 {
		return errors.New("review round must be zero")
	}
	for _, field := range []string{"documents", "evidence", "blockers"} {
		if !emptyArray(state[field]) {
			return fmt.Errorf("%s must be empty", field)
		}
	}
	entities, err := objectField(state, "entities")
	if err != nil {
		return errors.New("entities must be an object")
	}
	for _, field := range []string{"agents", "tasks", "bugs", "teams"} {
		if !emptyArray(entities[field]) {
			return fmt.Errorf("entities.%s must be empty", field)
		}
	}
	return nil
}

func validateRolloverApproval(state map[string]any, approval RolloverApproval, runtimeID string, revision int) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return errors.New("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["id"] != approval.EvidenceID || item["kind"] != "human_decision" || item["status"] != "valid" {
			continue
		}
		if containsString(item["produced_by"], approval.ApprovedBy) && containsString(item["scope_refs"], fmt.Sprintf("runtime_rollover:%s@%d", runtimeID, revision)) {
			return nil
		}
	}
	return fmt.Errorf("rollover approval evidence %q must be valid human_decision evidence produced by %q and scoped to runtime_rollover:%s@%d", approval.EvidenceID, approval.ApprovedBy, runtimeID, revision)
}

func emptyArray(value any) bool {
	switch values := value.(type) {
	case []any:
		return len(values) == 0
	case []string:
		return len(values) == 0
	default:
		return false
	}
}

func containsString(value any, want string) bool {
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	}
	return false
}

func journalEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, errors.New("fresh runtime journal is missing")
	}
	if err != nil {
		return false, fmt.Errorf("read runtime journal: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("runtime journal is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read runtime journal: %w", err)
	}
	return len(strings.TrimSpace(string(data))) == 0, nil
}

func verifyRolloverArchive(record RolloverRecord) error {
	if strings.TrimSpace(record.ArchiveDir) == "" || record.ArchiveStateSHA == "" || record.ArchiveJournalSHA == "" {
		return errors.New("archive record is incomplete")
	}
	stateData, err := os.ReadFile(filepath.Join(record.ArchiveDir, "loop-state.json"))
	if err != nil {
		return fmt.Errorf("read archived runtime state: %w", err)
	}
	if sha256Hex(stateData) != record.ArchiveStateSHA {
		return errors.New("archived runtime state fingerprint mismatch")
	}
	journalData, err := os.ReadFile(filepath.Join(record.ArchiveDir, "loop-events.jsonl"))
	if err != nil {
		return fmt.Errorf("read archived runtime journal: %w", err)
	}
	if sha256Hex(journalData) != record.ArchiveJournalSHA {
		return errors.New("archived runtime journal fingerprint mismatch")
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime: %w", err)
	}
	return atomicWriteBytes(path, append(data, '\n'), ".loop-state-*.tmp")
}

func atomicWriteBytes(path string, data []byte, pattern string) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create runtime temp: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write runtime temp: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync runtime: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close runtime: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace runtime: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync runtime directory: %w", err)
	}
	return nil
}

func writeDurableFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func appendJSONLine(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return fmt.Errorf("encode journal: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync journal: %w", err)
	}
	return nil
}

func journalContains(path, eventID string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return false, fmt.Errorf("decode journal event: %w", err)
		}
		if event["event_id"] == eventID {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan journal: %w", err)
	}
	return false, nil
}

func acquireLock(path string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	owner := strconv.Itoa(os.Getpid()) + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := file.WriteString(owner); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, fmt.Errorf("write runtime lock owner: %w", writeErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("close runtime lock: %w", closeErr)
			}
			return func() {
				currentOwner, readErr := os.ReadFile(path)
				if readErr == nil && string(currentOwner) == owner {
					_ = os.Remove(path)
				}
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire runtime lock: %w", err)
		}
		recovered, recoverErr := removeExpiredLock(path, staleLockAge)
		if recoverErr != nil {
			return nil, recoverErr
		}
		if recovered {
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("runtime lock timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func removeExpiredLock(path string, maxAge time.Duration) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect runtime lock: %w", err)
	}
	if time.Since(info.ModTime()) <= maxAge {
		return false, nil
	}

	latest, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reinspect runtime lock: %w", err)
	}
	if !os.SameFile(info, latest) || time.Since(latest.ModTime()) <= maxAge {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove expired runtime lock: %w", err)
	}
	return true, nil
}

func integerField(object map[string]any, key string) (int, error) {
	switch value := object[key].(type) {
	case float64:
		return int(value), nil
	case int:
		return value, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func objectField(object map[string]any, key string) (map[string]any, error) {
	value, ok := object[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return value, nil
}
