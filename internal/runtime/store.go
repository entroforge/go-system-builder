package runtime

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var ErrStaleRevision = errors.New("stale runtime revision")

// ErrStaleRuntimeIdentity distinguishes a stale snapshot from a same-runtime
// CAS conflict. It is especially important when a boundary transition resets
// revision to zero: the old runtime identity must not become valid again just
// because its old numeric revision was also zero.
var ErrStaleRuntimeIdentity = errors.New("stale runtime identity")

// ErrPendingRuntimeOperation is returned by read-only APIs when the durable
// runtime pair has an unfinished rollover or commit. Readers must not repair
// a pair implicitly: the caller must use an explicit writer/recovery path.
var ErrPendingRuntimeOperation = errors.New("runtime has a pending durable operation")

// ErrCandidateValidatorRequired is returned when a caller attempts a Runtime
// write without injecting the repository-semantic validator. NewStore is
// intentionally suitable for snapshots and inspection only; mutation writers
// must use NewWriter so the composition root supplies the repository root and
// semantic validation implementation.
var ErrCandidateValidatorRequired = errors.New("runtime candidate validator is required for writes")

// ErrCandidateValidatorInvalid is returned when a supplied validator cannot
// demonstrate that it rejects an obviously invalid candidate. This is a
// fail-closed guard against accidentally wiring a no-op test/function adapter
// into a production writer.
var ErrCandidateValidatorInvalid = errors.New("runtime candidate validator is invalid or no-op")

const staleLockAge = 30 * time.Second

type Mutation struct {
	// Audit is the structured commit envelope. The legacy fields below remain
	// as a compatibility surface for existing callers; Update normalizes both
	// forms into this envelope before applying a mutation.
	Audit          AuditEnvelope
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
	// RequireEmptyJournal is retained for legacy mutation callers that require
	// an empty journal. TR-001 uses BoundaryReset instead: it validates the
	// inactive source pair, archives it, and creates a new empty active pair.
	RequireEmptyJournal bool
	// BoundaryReset makes the mutation install a new runtime identity after
	// applying its domain actions. TR-001 uses this because binding is the
	// boundary between the inactive bootstrap runtime and the new active
	// runtime; the new runtime starts at revision zero with an empty journal.
	BoundaryReset bool
	// RetainLastTransition keeps the existing last_transition snapshot when
	// the mutation is not a legality lifecycle commit (e.g. milestone refresh).
	RetainLastTransition bool
	Apply                func(state map[string]any) error
}

// AuditEnvelope contains the fields that identify and anchor one durable
// Runtime commit. From and To are cursor objects rather than arbitrary JSON;
// Update fills a missing cursor from the current Runtime cursor for entity
// events that do not change the top-level lifecycle.
type AuditEnvelope struct {
	EventID        string
	TransitionID   string
	Event          string
	Actor          string
	IdempotencyKey string
	RuntimeID      string
	From           map[string]any
	To             map[string]any
	EvidenceIDs    []string
}

type Snapshot struct {
	Revision int
	State    map[string]any
}

// ArtifactCleanupRequest describes a staged repository artifact that may be
// removed only when the Runtime is stable and the artifact is not reachable
// from the state being protected. The operation deliberately does not recover
// pending Runtime writes: a pending marker means a prior CAS may still make
// the artifact reachable, so the safe answer is to retain it.
type ArtifactCleanupRequest struct {
	ExpectedRevision int
	ArtifactPath     string
	ArtifactSHA256   string
	ReferencedPaths  []string
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
	SchemaVersion       string           `json:"schema_version"`
	FreshState          map[string]any   `json:"fresh_state"`
	Record              RolloverRecord   `json:"record"`
	Approval            RolloverApproval `json:"approval"`
	BoundaryKind        string           `json:"boundary_kind,omitempty"`
	Disposition         string           `json:"disposition,omitempty"`
	OccurredAt          string           `json:"occurred_at"`
	SourceStateSHA256   string           `json:"source_state_sha256"`
	SourceJournalSHA256 string           `json:"source_journal_sha256"`
	SourceRuntimeID     string           `json:"source_runtime_id"`
	SourceRevision      *int             `json:"source_revision"`
}

type commitPending struct {
	SchemaVersion        string         `json:"schema_version"`
	PreviousStateSHA256  string         `json:"previous_state_sha256"`
	PreviousRevision     int            `json:"previous_revision"`
	StateSHA256          string         `json:"state_sha256"`
	JournalEventSHA256   string         `json:"journal_event_sha256"`
	RequestID            string         `json:"request_id"`
	IdempotencyKey       string         `json:"idempotency_key"`
	RetainLastTransition bool           `json:"retain_last_transition"`
	State                map[string]any `json:"state"`
	JournalEvent         map[string]any `json:"journal_event"`
}

// statePending records a state-only durable write. Fingerprint refreshes do
// not represent a lifecycle event, so they must not invent a journal entry or
// revision bump; they still use the same marker-before-state-write protocol
// and explicit writer recovery as normal commits.
type statePending struct {
	SchemaVersion       string         `json:"schema_version"`
	PreviousStateSHA256 string         `json:"previous_state_sha256"`
	PreviousRevision    int            `json:"previous_revision"`
	StateSHA256         string         `json:"state_sha256"`
	State               map[string]any `json:"state"`
}

// runtimeSemanticDefinition is the small, repository-owned part of the Loop
// Definition that Runtime must understand itself. Keeping this check here is
// deliberate: an injected CandidateValidator may add repository-wide checks,
// but it cannot be the only authority deciding whether a lifecycle state,
// phase, or entity state exists in the active machine.
type runtimeSemanticDefinition struct {
	States map[string]struct {
		PhaseMachine *string `json:"phase_machine"`
	} `json:"states"`
	PhaseMachines map[string]struct {
		Phases map[string]json.RawMessage `json:"phases"`
	} `json:"phase_machines"`
	EntityLifecycles map[string]struct {
		States []string `json:"states"`
	} `json:"entity_lifecycles"`
}

type Store struct {
	statePath          string
	journalPath        string
	root               string
	candidateValidator CandidateValidator
	validatorInitErr   error
	mutationCapable    bool
}

func NewStore(statePath, journalPath string) *Store {
	return &Store{statePath: statePath, journalPath: journalPath}
}

// CandidateValidator validates a post-mutation Runtime candidate in the
// repository context. Runtime deliberately owns only this small interface;
// the semantic implementation is injected by the composition root so the
// runtime package does not depend on transition or semantic packages. A
// private method cannot be used here because it would prevent the semantic
// package from implementing the interface across the package boundary; the
// strongest runtime-local capability check is therefore the semantic-negative
// probe performed by validateCandidate.
type CandidateValidator interface {
	ValidateCandidate(root string, state map[string]any) error
}

// NewWriter constructs a mutation-capable Store. A nil validator is retained
// as an invalid writer and causes every write to fail closed; this keeps the
// constructor convenient for error-returning public writer APIs while making
// omission of semantic validation impossible at the commit boundary. The
// probe also rejects a validator that accepts an empty, schema-invalid state,
// which catches the common no-op adapter mistake before any write occurs.
func NewWriter(statePath, journalPath, root string, validator CandidateValidator) *Store {
	store := &Store{
		statePath:          statePath,
		journalPath:        journalPath,
		root:               root,
		candidateValidator: validator,
		mutationCapable:    true,
	}
	if validator == nil {
		store.validatorInitErr = ErrCandidateValidatorRequired
		return store
	}
	if err := validator.ValidateCandidate("", map[string]any{}); err == nil {
		store.validatorInitErr = ErrCandidateValidatorInvalid
	}
	return store
}

func normalizeMutation(state map[string]any, mutation Mutation) (Mutation, error) {
	stateRuntimeID, _ := state["runtime_id"].(string)
	if strings.TrimSpace(stateRuntimeID) == "" {
		return Mutation{}, errors.New("state runtime_id is required")
	}
	allowsInitialBinding := mutation.TransitionID == "TR-001" && mutation.BoundaryReset && strings.HasPrefix(mutation.RuntimeID, "loop-REQ-")
	if allowsInitialBinding {
		if err := ValidateBindEligibleState(state); err != nil {
			return Mutation{}, fmt.Errorf("requires a fresh inactive runtime (unbound, revision-independent): %w", err)
		}
	}
	if mutation.RuntimeID != "" && mutation.RuntimeID != stateRuntimeID && !allowsInitialBinding {
		return Mutation{}, fmt.Errorf("mutation runtime_id %q does not match state runtime_id %q", mutation.RuntimeID, stateRuntimeID)
	}
	if mutation.Audit.RuntimeID != "" && mutation.Audit.RuntimeID != stateRuntimeID && !allowsInitialBinding {
		return Mutation{}, fmt.Errorf("mutation audit runtime_id %q does not match state runtime_id %q", mutation.Audit.RuntimeID, stateRuntimeID)
	}
	envelope := mutation.Audit
	if envelope.EventID == "" {
		envelope.EventID = mutation.EventID
	}
	if envelope.TransitionID == "" {
		envelope.TransitionID = mutation.TransitionID
	}
	if envelope.Event == "" {
		envelope.Event = mutation.Event
	}
	if envelope.Actor == "" {
		envelope.Actor = mutation.Actor
	}
	if envelope.IdempotencyKey == "" {
		envelope.IdempotencyKey = mutation.IdempotencyKey
	}
	if envelope.RuntimeID == "" {
		envelope.RuntimeID = mutation.RuntimeID
	}
	if envelope.From == nil {
		envelope.From = mutation.From
	}
	if envelope.To == nil {
		envelope.To = mutation.To
	}
	if envelope.EvidenceIDs == nil {
		envelope.EvidenceIDs = mutation.EvidenceIDs
	}

	if strings.TrimSpace(envelope.EventID) == "" {
		return Mutation{}, errors.New("mutation event_id is required")
	}
	if strings.TrimSpace(envelope.Actor) == "" {
		return Mutation{}, errors.New("mutation actor is required")
	}
	if strings.TrimSpace(envelope.IdempotencyKey) == "" {
		return Mutation{}, errors.New("mutation idempotency_key is required")
	}
	if envelope.Event == "" {
		envelope.Event = mutation.JournalEvent
	}
	if envelope.Event == "" {
		return Mutation{}, errors.New("mutation event is required")
	}
	if envelope.TransitionID == "" && mutation.JournalEvent == "" {
		return Mutation{}, errors.New("mutation transition_id is required")
	}

	if envelope.RuntimeID == "" {
		envelope.RuntimeID = stateRuntimeID
	}
	if envelope.RuntimeID != stateRuntimeID && !allowsInitialBinding {
		return Mutation{}, fmt.Errorf("mutation runtime_id %q does not match state runtime_id %q", envelope.RuntimeID, stateRuntimeID)
	}
	if envelope.From == nil || envelope.To == nil {
		cursor, err := currentCursor(state)
		if err != nil {
			return Mutation{}, err
		}
		if envelope.From == nil {
			envelope.From = cursor
		}
		if envelope.To == nil {
			envelope.To = cursor
		}
	}
	if envelope.EvidenceIDs == nil {
		envelope.EvidenceIDs = []string{}
	}

	mutation.Audit = envelope
	mutation.EventID = envelope.EventID
	mutation.TransitionID = envelope.TransitionID
	mutation.Event = envelope.Event
	mutation.Actor = envelope.Actor
	mutation.IdempotencyKey = envelope.IdempotencyKey
	mutation.RuntimeID = envelope.RuntimeID
	mutation.From = envelope.From
	mutation.To = envelope.To
	mutation.EvidenceIDs = envelope.EvidenceIDs
	return mutation, nil
}

func currentCursor(state map[string]any) (map[string]any, error) {
	lifecycle, err := objectField(state, "lifecycle")
	if err != nil {
		return nil, fmt.Errorf("runtime lifecycle is required for audit cursor: %w", err)
	}
	stateName, ok := lifecycle["state"].(string)
	if !ok || strings.TrimSpace(stateName) == "" {
		return nil, errors.New("runtime lifecycle state is required for audit cursor")
	}
	return map[string]any{"state": stateName, "phase": lifecycle["phase"]}, nil
}

func (s *Store) validateCandidate(state map[string]any) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode candidate runtime: %w", err)
	}
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateBytes("loop-state.schema.json", encoded); err != nil {
		return fmt.Errorf("candidate runtime schema: %w", err)
	}
	if err := s.requireCandidateValidator(); err != nil {
		return err
	}
	if err := validateRuntimeSemanticCore(s.root, state); err != nil {
		return fmt.Errorf("candidate runtime built-in semantic validation: %w", err)
	}
	probe, err := semanticInvalidProbe(state)
	if err != nil {
		return err
	}
	if err := s.candidateValidator.ValidateCandidate(s.root, probe); err == nil {
		return ErrCandidateValidatorInvalid
	}
	if err := s.candidateValidator.ValidateCandidate(s.root, state); err != nil {
		return fmt.Errorf("candidate runtime semantic validation: %w", err)
	}
	return nil
}

func validateRuntimeSemanticCore(root string, state map[string]any) error {
	definitionPath := filepath.Join(root, "docs", "loop-definition.json")
	definitionData, err := os.ReadFile(definitionPath)
	if err != nil {
		// The runtime package has small unit fixtures that intentionally inject
		// a validator without materializing the repository definition. A real
		// production writer still fails closed through the injected semantic
		// validator (semantic.RuntimeCandidateValidator), which reads the same
		// definition. Do not turn the runtime-local helper into a second fixture
		// contract or require every low-level test to duplicate the repository.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read Loop Definition %q: %w", definitionPath, err)
	}
	var definition runtimeSemanticDefinition
	if err := json.Unmarshal(definitionData, &definition); err != nil {
		return fmt.Errorf("decode Loop Definition: %w", err)
	}

	lifecycle, err := objectField(state, "lifecycle")
	if err != nil {
		return fmt.Errorf("lifecycle: %w", err)
	}
	lifecycleState, ok := lifecycle["state"].(string)
	if !ok || strings.TrimSpace(lifecycleState) == "" {
		return errors.New("lifecycle state is required")
	}
	stateSpec, ok := definition.States[lifecycleState]
	if !ok {
		return fmt.Errorf("unknown lifecycle state %q", lifecycleState)
	}
	phase := lifecycle["phase"]
	if stateSpec.PhaseMachine == nil {
		if phase != nil {
			return fmt.Errorf("lifecycle state %q does not allow phase %v", lifecycleState, phase)
		}
	} else {
		phaseName, ok := phase.(string)
		if !ok || strings.TrimSpace(phaseName) == "" {
			return fmt.Errorf("lifecycle state %q requires a phase", lifecycleState)
		}
		machine, ok := definition.PhaseMachines[*stateSpec.PhaseMachine]
		if !ok {
			return fmt.Errorf("missing phase machine %q", *stateSpec.PhaseMachine)
		}
		if _, ok := machine.Phases[phaseName]; !ok {
			return fmt.Errorf("unknown phase %q for lifecycle state %q", phaseName, lifecycleState)
		}
	}

	entities, err := objectField(state, "entities")
	if err != nil {
		return fmt.Errorf("entities: %w", err)
	}
	for field, lifecycleName := range map[string]string{
		"agents": "agent",
		"tasks":  "task",
		"bugs":   "bug",
	} {
		items, ok := entities[field].([]any)
		if !ok {
			return fmt.Errorf("entities.%s must be an array", field)
		}
		allowed, ok := definition.EntityLifecycles[lifecycleName]
		if !ok {
			return fmt.Errorf("missing entity lifecycle %q", lifecycleName)
		}
		for index, raw := range items {
			entity, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("entities.%s[%d] must be an object", field, index)
			}
			entityState, ok := entity["state"].(string)
			if !ok || strings.TrimSpace(entityState) == "" {
				return fmt.Errorf("entities.%s[%d] state is required", field, index)
			}
			if !containsRuntimeState(allowed.States, entityState) {
				return fmt.Errorf("unknown %s state %q", lifecycleName, entityState)
			}
		}
	}
	return nil
}

func containsRuntimeState(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func semanticInvalidProbe(state map[string]any) (map[string]any, error) {
	probe, err := cloneState(state)
	if err != nil {
		return nil, fmt.Errorf("create validator capability probe: %w", err)
	}
	lifecycle, err := objectField(probe, "lifecycle")
	if err != nil {
		return nil, fmt.Errorf("create validator capability probe: %w", err)
	}
	// This value matches the Runtime schema's phase pattern but is not present
	// in any Loop Definition phase machine. A real semantic validator must
	// reject it; accepting it identifies a no-op or schema-only validator.
	lifecycle["phase"] = "invalid_semantic_phase"
	return probe, nil
}

func (s *Store) requireCandidateValidator() error {
	if s.validatorInitErr != nil {
		return s.validatorInitErr
	}
	if s.candidateValidator == nil {
		return ErrCandidateValidatorRequired
	}
	if strings.TrimSpace(s.root) == "" {
		return errors.New("runtime writer root is required")
	}
	return nil
}

func validateJournalEvent(event map[string]any) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode journal event: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-event.schema.json", encoded); err != nil {
		return fmt.Errorf("journal event schema: %w", err)
	}
	return nil
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
	if s.mutationCapable {
		if err := s.recoverPendingWritesLocked(); err != nil {
			return Snapshot{}, err
		}
	} else if err := s.reportPendingOperationLocked(); err != nil {
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

// RemoveUnreferencedArtifact performs a lock-protected, fail-closed cleanup
// for artifacts staged before a Runtime CAS. It returns false when cleanup is
// unsafe or unnecessary. In particular, it never removes anything while a
// commit/fingerprint/rollover marker exists because the pending operation may
// still publish a state that references the artifact.
func (s *Store) RemoveUnreferencedArtifact(request ArtifactCleanupRequest) (bool, error) {
	if s == nil {
		return false, errors.New("runtime store is required for artifact cleanup")
	}
	if strings.TrimSpace(s.root) == "" {
		return false, errors.New("runtime root is required for artifact cleanup")
	}
	if request.ExpectedRevision < 0 {
		return false, errors.New("artifact cleanup expected revision must not be negative")
	}
	if len(request.ArtifactSHA256) != 64 {
		return false, errors.New("artifact cleanup requires a 64-character sha256")
	}
	cleanArtifact, err := safeEvidencePath(s.root, request.ArtifactPath)
	if err != nil {
		return false, fmt.Errorf("validate artifact cleanup path: %w", err)
	}

	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer release()
	if err := s.reportPendingOperationLocked(); err != nil {
		if errors.Is(err, ErrPendingRuntimeOperation) {
			return false, nil
		}
		return false, fmt.Errorf("inspect pending runtime operation before artifact cleanup: %w", err)
	}
	state, err := s.read()
	if err != nil {
		return false, fmt.Errorf("read runtime before artifact cleanup: %w", err)
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return false, fmt.Errorf("read runtime revision before artifact cleanup: %w", err)
	}
	if revision != request.ExpectedRevision {
		return false, nil
	}
	for _, referenced := range request.ReferencedPaths {
		cleanReferenced, cleanErr := cleanArtifactReference(referenced)
		if cleanErr != nil {
			return false, fmt.Errorf("validate referenced artifact path %q: %w", referenced, cleanErr)
		}
		if cleanReferenced == cleanArtifact {
			return false, nil
		}
	}

	artifactPath := filepath.Join(s.root, filepath.FromSlash(cleanArtifact))
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read artifact before cleanup: %w", err)
	}
	if sha256Hex(data) != request.ArtifactSHA256 {
		return false, nil
	}
	if err := os.Remove(artifactPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("remove unreferenced artifact: %w", err)
	}
	if err := syncDir(filepath.Dir(artifactPath)); err != nil {
		return false, fmt.Errorf("sync artifact cleanup: %w", err)
	}
	return true, nil
}

func cleanArtifactReference(path string) (string, error) {
	clean := filepath.Clean(path)
	if strings.TrimSpace(path) == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact reference must be repository-relative")
	}
	return filepath.ToSlash(clean), nil
}

// Rollover archives a terminal runtime and its journal, then replaces the
// active state and journal with a supplied fresh runtime. A durable pending
// marker makes the two-file replacement recoverable after a process crash. It is
// deliberately not a Loop Definition transition: terminal Loop states have no
// automated exit, so a human authorization is required to start another REQ.
func (s *Store) Rollover(freshState map[string]any, archiveRoot string, approval RolloverApproval, occurredAt time.Time) (RolloverRecord, error) {
	if err := s.requireCandidateValidator(); err != nil {
		return RolloverRecord{}, err
	}
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
	if err := s.validateCandidate(state); err != nil {
		return RolloverRecord{}, fmt.Errorf("validate current runtime for rollover: %w", err)
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	if !isRolloverTerminalState(lifecycleState) {
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
	if err := validateLifecycleApproval(state, approval.ApprovedBy, approval.EvidenceID, runtimeID, revision, "runtime_rollover"); err != nil {
		return RolloverRecord{}, err
	}

	return s.archiveAndReset(stateData, journalData, runtimeID, revision, freshState, archiveRoot, nil, "", approval, occurredAt)
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
// Triple is the RC-10 Step B convergence projection of the eight sha256
// families onto (state_hash, evidence_hash, baseline_hash); it is derived
// from the post-refresh state and never replaces per-family validation.
// RC-17 anchor: this result is the canonical doctor/reconcile diagnostic
// surface — `loop-harness doctor` and `runtime reconcile` report Triple
// drift from here instead of re-deriving any family hash (see
// fingerprint.go for the two-layer decision; new diagnostics must consume
// the triple, never mint a ninth family).
type FingerprintResult struct {
	Updated   []string
	Unchanged []string
	Missing   []string
	Triple    FingerprintTriple `json:"triple"`
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
	if err := s.recoverPendingWritesLocked(); err != nil {
		return FingerprintResult{}, err
	}

	state, err := s.read()
	if err != nil {
		return FingerprintResult{}, err
	}
	inspection, err := inspectJournal(s.journalPath)
	if err != nil {
		return FingerprintResult{}, fmt.Errorf("inspect runtime journal before fingerprint refresh: %w", err)
	}
	if err := validateStateJournalPair(state, inspection); err != nil {
		return FingerprintResult{}, fmt.Errorf("validate state/journal pair before fingerprint refresh: %w", err)
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
		baseline, _ := state["baseline"].(map[string]any)
		currentGeneration := 0
		if baseline != nil {
			if gen, err := integerField(baseline, "generation"); err == nil {
				currentGeneration = gen
			}
		}
		for _, raw := range docs {
			doc, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			// Superseded generations of every kind are immutable history:
			// a fingerprint refresh must not rewrite them to match
			// amended files (L3-S3 v4.0.1: the req-only exemption is
			// promoted now that contracts and tasks also register).
			if gen, err := integerField(doc, "generation"); err == nil && gen < currentGeneration {
				continue
			}
			refresh(doc)
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
		if err := s.validateCandidate(state); err != nil {
			return FingerprintResult{}, fmt.Errorf("post-refresh snapshot invalid: %w", err)
		}
		inspection, err = inspectJournal(s.journalPath)
		if err != nil {
			return FingerprintResult{}, fmt.Errorf("inspect runtime journal after fingerprint refresh: %w", err)
		}
		if err := validateStateJournalPair(state, inspection); err != nil {
			return FingerprintResult{}, fmt.Errorf("validate state/journal pair after fingerprint refresh: %w", err)
		}
		previousState, err := s.read()
		if err != nil {
			return FingerprintResult{}, err
		}
		previousRevision, err := integerField(previousState, "revision")
		if err != nil {
			return FingerprintResult{}, fmt.Errorf("read runtime revision for fingerprint refresh: %w", err)
		}
		previousStateSHA256, err := hashState(previousState)
		if err != nil {
			return FingerprintResult{}, err
		}
		stateSHA256, err := hashState(state)
		if err != nil {
			return FingerprintResult{}, err
		}
		pending := statePending{
			SchemaVersion:       "1.0.0",
			PreviousStateSHA256: previousStateSHA256,
			PreviousRevision:    previousRevision,
			StateSHA256:         stateSHA256,
			State:               state,
		}
		if err := atomicWriteJSON(s.fingerprintMarkerPath(), pending); err != nil {
			return FingerprintResult{}, fmt.Errorf("record pending fingerprint refresh: %w", err)
		}
		if err := atomicWriteJSON(s.statePath, state); err != nil {
			return FingerprintResult{}, fmt.Errorf("write refreshed runtime state: %w", err)
		}
		writtenState, err := s.read()
		if err != nil {
			return FingerprintResult{}, fmt.Errorf("read refreshed runtime state: %w", err)
		}
		writtenInspection, err := inspectJournal(s.journalPath)
		if err != nil {
			return FingerprintResult{}, fmt.Errorf("inspect runtime journal after refreshed state write: %w", err)
		}
		if err := validateStateJournalPair(writtenState, writtenInspection); err != nil {
			return FingerprintResult{}, fmt.Errorf("validate state/journal pair after refreshed state write: %w", err)
		}
		if err := s.clearFingerprintMarkerLocked(); err != nil {
			return FingerprintResult{}, err
		}
	}
	// RC-17 anchor: derive the observation triple from the post-refresh state
	// so doctor/reconcile consumers verify one number triple instead of
	// re-deriving family rules. The families written above stay the
	// enforcement layer (see fingerprint.go two-layer decision).
	result.Triple = ComputeTriple(state)
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
	if err := s.recoverPendingWritesLocked(); err != nil {
		return Snapshot{}, err
	}
	return s.applyMutation(expectedRevision, mutation)
}

func (s *Store) Reconcile() (bool, error) {
	if !s.mutationCapable {
		return false, fmt.Errorf("%w: Reconcile requires a mutation-capable writer", ErrCandidateValidatorRequired)
	}
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer release()
	if err := s.recoverPendingWritesLocked(); err != nil {
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
	inspection, err := inspectJournal(s.journalPath)
	if err != nil {
		return false, fmt.Errorf("inspect runtime journal for reconcile: %w", err)
	}
	targetSequence, err := integerField(transition, "sequence")
	if err != nil {
		return false, fmt.Errorf("last transition sequence is invalid: %w", err)
	}
	if targetSequence < 1 {
		return false, errors.New("last transition sequence is invalid: must be positive")
	}
	if eventIndex, found := inspection.EventIndex[eventID]; found {
		if eventIndex != len(inspection.Events)-1 {
			return false, fmt.Errorf("reconcile target event %q is not the journal tail", eventID)
		}
		return false, nil
	}
	if inspection.TailSequence != targetSequence-1 {
		return false, fmt.Errorf("reconcile journal tail sequence %d does not precede target sequence %d", inspection.TailSequence, targetSequence)
	}
	stateJournal, err := objectField(state, "journal")
	if err != nil {
		return false, err
	}
	stateSequence, err := integerField(stateJournal, "last_sequence")
	if err != nil || stateSequence != targetSequence {
		return false, fmt.Errorf("reconcile state journal last_sequence %d does not match target sequence %d", stateSequence, targetSequence)
	}
	if inspection.RuntimeID != "" {
		runtimeID, _ := state["runtime_id"].(string)
		if inspection.RuntimeID != runtimeID {
			return false, fmt.Errorf("reconcile journal runtime_id %q does not match state runtime_id %q", inspection.RuntimeID, runtimeID)
		}
	}
	if err := s.validateCandidate(state); err != nil {
		return false, fmt.Errorf("reconcile requires a valid runtime writer: %w", err)
	}
	runtimeID, _ := state["runtime_id"].(string)
	actor, _ := transition["actor"].(string)
	beforeRevision, _ := transition["expected_revision"].(float64)
	afterRevision, _ := transition["committed_revision"].(float64)
	idempotencyKey := stringValue(transition["idempotency_key"])
	if idempotencyKey == "" {
		idempotencyKey = "runtime:reconcile:" + eventID
	}
	event := buildJournalEvent(Mutation{
		EventID:            eventID,
		TransitionID:       stringValue(transition["transition_id"]),
		Actor:              nonEmpty(actor, "runtime-reconciler"),
		IdempotencyKey:     idempotencyKey,
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
	}, runtimeID, int(beforeRevision), int(afterRevision), targetSequence, time.Now().UTC())
	if err := validateJournalEvent(event); err != nil {
		return false, fmt.Errorf("reconcile journal event invalid: %w", err)
	}
	if err := s.commitJournalOnlyLocked(state, event); err != nil {
		return false, err
	}
	return true, nil
}

// RecoverPendingOperations completes a validated commit, fingerprint refresh,
// or rollover marker without performing ordinary reconcile work. Recovery
// callers use it as the highest-trust source before rebuilding from artifacts.
func (s *Store) RecoverPendingOperations() (bool, error) {
	if !s.mutationCapable {
		return false, fmt.Errorf("%w: RecoverPendingOperations requires a mutation-capable writer", ErrCandidateValidatorRequired)
	}
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer release()
	pending := false
	for _, path := range []string{s.commitMarkerPath(), s.fingerprintMarkerPath(), s.rolloverMarkerPath(), s.journalRotationMarkerPath()} {
		if _, statErr := os.Stat(path); statErr == nil {
			pending = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf("inspect pending Runtime operation %s: %w", path, statErr)
		}
	}
	if !pending {
		return false, nil
	}
	if err := s.recoverPendingWritesLocked(); err != nil {
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
	if err := s.recoverPendingWritesLocked(); err != nil {
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
		TransitionID:       "PLANNING-MIGRATION",
		Actor:              "runtime-migrator",
		Event:              "planning_phase_migrated",
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
	previousState, err := cloneState(state)
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
	mutation, err = normalizeMutation(state, mutation)
	if err != nil {
		return Snapshot{}, err
	}
	existingJournal, err := inspectJournal(s.journalPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect existing runtime journal before mutation: %w", err)
	}
	if err := validateStateJournalPair(state, existingJournal); err != nil {
		return Snapshot{}, fmt.Errorf("inspect runtime journal cursor before mutation (state journal.last_sequence must match the journal tail; if this followed a crash run `runtime reconcile` to replay the pending transition, otherwise the journal was truncated or the state hand-edited and needs manual realignment): %w", err)
	}
	if _, exists := existingJournal.EventIndex[mutation.EventID]; exists {
		return Snapshot{}, fmt.Errorf("mutation event_id %q already exists in runtime journal", mutation.EventID)
	}
	if mutation.RequireEmptyJournal && !mutation.BoundaryReset {
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
	if err := validateMutationApplyBoundary(previousState, state, mutation.RetainLastTransition); err != nil {
		return Snapshot{}, fmt.Errorf("mutation apply coherence: %w", err)
	}
	if mutation.BoundaryReset {
		return s.applyBoundaryResetLocked(previousState, state, mutation, expectedRevision)
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

	runtimeID := mutation.RuntimeID
	if runtimeID == "" {
		runtimeID, _ = state["runtime_id"].(string)
	}
	journalEvent := buildJournalEvent(mutation, runtimeID, expectedRevision, nextRevision, sequence, occurredAt)
	if err := s.validateCandidate(state); err != nil {
		return Snapshot{}, fmt.Errorf("post-mutation snapshot invalid: %w", err)
	}
	if err := validateJournalEvent(journalEvent); err != nil {
		return Snapshot{}, fmt.Errorf("post-mutation journal invalid: %w", err)
	}
	previousStateSHA256, err := hashState(previousState)
	if err != nil {
		return Snapshot{}, err
	}
	stateSHA256, err := hashState(state)
	if err != nil {
		return Snapshot{}, err
	}
	journalEventBytes, err := jsonLineBytes(journalEvent)
	if err != nil {
		return Snapshot{}, err
	}
	pending := commitPending{
		SchemaVersion:        "1.0.0",
		PreviousStateSHA256:  previousStateSHA256,
		PreviousRevision:     expectedRevision,
		StateSHA256:          stateSHA256,
		JournalEventSHA256:   sha256Hex(journalEventBytes),
		RequestID:            stringValue(journalEvent["request_id"]),
		IdempotencyKey:       mutation.IdempotencyKey,
		RetainLastTransition: mutation.RetainLastTransition,
		State:                state,
		JournalEvent:         journalEvent,
	}
	if err := validatePendingCommitCoherence(pending); err != nil {
		return Snapshot{}, fmt.Errorf("pending runtime commit coherence: %w", err)
	}
	if err := atomicWriteJSON(s.commitMarkerPath(), pending); err != nil {
		return Snapshot{}, fmt.Errorf("record pending runtime commit: %w", err)
	}
	if err := atomicWriteJSON(s.statePath, state); err != nil {
		return Snapshot{}, fmt.Errorf("write committed runtime state: %w", err)
	}
	if err := appendJSONLine(s.journalPath, journalEvent); err != nil {
		return Snapshot{}, fmt.Errorf("append committed runtime journal: %w", err)
	}
	if err := s.maybeRotateJournalLocked(); err != nil {
		return Snapshot{}, fmt.Errorf("rotate journal: %w", err)
	}
	if err := s.clearCommitMarkerLocked(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: nextRevision, State: state}, nil
}

// applyBoundaryResetLocked closes the bootstrap runtime and installs the
// domain state produced by a boundary transition as a new runtime pair. The
// source pair is archived before the replacement is published, so a bind
// cannot lose Hook checkpoints merely because the active revision restarts at
// zero. The caller already holds the state lock.
func (s *Store) applyBoundaryResetLocked(previousState, targetState map[string]any, mutation Mutation, sourceRevision int) (Snapshot, error) {
	if strings.TrimSpace(s.root) == "" {
		return Snapshot{}, errors.New("runtime root is required for boundary reset")
	}
	if sourceRevision < 0 {
		return Snapshot{}, errors.New("boundary source revision must be non-negative")
	}
	stateData, err := os.ReadFile(s.statePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read source runtime for boundary reset: %w", err)
	}
	journalData, err := os.ReadFile(s.journalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, errors.New("fresh runtime journal is missing")
		}
		return Snapshot{}, fmt.Errorf("read source journal for boundary reset: %w", err)
	}
	sourceRuntimeID, _ := previousState["runtime_id"].(string)
	targetRuntimeID, _ := targetState["runtime_id"].(string)
	if sourceRuntimeID == "" || targetRuntimeID == "" || sourceRuntimeID == targetRuntimeID {
		return Snapshot{}, fmt.Errorf("boundary reset requires a runtime identity change, source=%q target=%q", sourceRuntimeID, targetRuntimeID)
	}
	occurredAt := mutation.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	targetState["binding_receipt"] = map[string]any{
		"event_id":              mutation.EventID,
		"transition_id":         mutation.TransitionID,
		"event":                 "req_bound",
		"approved_by":           mutation.Actor,
		"occurred_at":           occurredAt.UTC().Format(time.RFC3339Nano),
		"source_runtime_id":     sourceRuntimeID,
		"source_revision":       sourceRevision,
		"source_state_sha256":   sha256Hex(stateData),
		"source_journal_sha256": sha256Hex(journalData),
		"target_runtime_id":     targetRuntimeID,
		"target_revision":       0,
	}
	targetState["revision"] = 0
	targetState["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	targetState["last_transition"] = nil
	if err := validateBoundaryTarget(targetState); err != nil {
		return Snapshot{}, fmt.Errorf("boundary target invalid: %w", err)
	}
	if err := s.validateCandidate(targetState); err != nil {
		return Snapshot{}, fmt.Errorf("boundary target semantic validation: %w", err)
	}
	archiveRoot := filepath.Join(filepath.Dir(s.statePath), "runtime-archive")
	_, err = s.archiveAndResetWithBoundary(
		stateData,
		journalData,
		sourceRuntimeID,
		sourceRevision,
		targetState,
		archiveRoot,
		map[string]any{
			"boundary_kind":     "bind",
			"binding_event_id":  mutation.EventID,
			"target_runtime_id": targetRuntimeID,
		},
		"bound",
		"bind",
		RolloverApproval{ApprovedBy: mutation.Actor, EvidenceID: mutation.EventID},
		occurredAt,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("archive source runtime for bind: %w", err)
	}
	return Snapshot{Revision: 0, State: targetState}, nil
}

func validateBoundaryTarget(state map[string]any) error {
	revision, err := integerField(state, "revision")
	if err != nil || revision != 0 {
		return errors.New("boundary target revision must be zero")
	}
	runtimeID, _ := state["runtime_id"].(string)
	if !strings.HasPrefix(runtimeID, "loop-REQ-") {
		return fmt.Errorf("boundary target runtime_id must identify the bound REQ, got %q", runtimeID)
	}
	if state["bound_req"] == nil {
		return errors.New("boundary target bound_req is required")
	}
	journal, err := objectField(state, "journal")
	if err != nil {
		return errors.New("boundary target journal must be an object")
	}
	sequence, err := integerField(journal, "last_sequence")
	if err != nil || sequence != 0 || journal["last_event_id"] != nil {
		return errors.New("boundary target journal cursor must be empty")
	}
	return nil
}

func validateMutationApplyBoundary(previousState, candidateState map[string]any, retainLastTransition bool) error {
	previousJournal, err := objectField(previousState, "journal")
	if err != nil {
		return fmt.Errorf("previous state journal: %w", err)
	}
	candidateJournal, err := objectField(candidateState, "journal")
	if err != nil {
		return fmt.Errorf("candidate state journal: %w", err)
	}
	previousSequence, err := integerField(previousJournal, "last_sequence")
	if err != nil {
		return fmt.Errorf("previous journal last_sequence: %w", err)
	}
	candidateSequence, err := integerField(candidateJournal, "last_sequence")
	if err != nil {
		return fmt.Errorf("candidate journal last_sequence: %w", err)
	}
	if candidateSequence != previousSequence {
		return fmt.Errorf("Mutation.Apply changed journal last_sequence from %d to %d", previousSequence, candidateSequence)
	}
	if !reflect.DeepEqual(previousJournal["last_event_id"], candidateJournal["last_event_id"]) {
		return errors.New("Mutation.Apply changed journal last_event_id")
	}
	if retainLastTransition && !reflect.DeepEqual(previousState["last_transition"], candidateState["last_transition"]) {
		return errors.New("Mutation.Apply changed retained last_transition")
	}
	return nil
}

// commitJournalOnlyLocked records a journal repair through the same durable
// marker protocol as a normal mutation. State is intentionally unchanged: the
// marker carries the current state as the already-committed target, so recovery
// only needs to append the missing event and clear the marker.
func (s *Store) commitJournalOnlyLocked(state map[string]any, event map[string]any) error {
	if err := validateJournalEvent(event); err != nil {
		return fmt.Errorf("journal-only commit event invalid: %w", err)
	}
	stateHash, err := hashState(state)
	if err != nil {
		return err
	}
	eventBytes, err := jsonLineBytes(event)
	if err != nil {
		return err
	}
	eventAfter, err := integerField(event, "after_revision")
	if err != nil {
		return fmt.Errorf("journal-only commit after_revision: %w", err)
	}
	pending := commitPending{
		SchemaVersion:        "1.0.0",
		PreviousStateSHA256:  stateHash,
		PreviousRevision:     eventAfter - 1,
		StateSHA256:          stateHash,
		JournalEventSHA256:   sha256Hex(eventBytes),
		RequestID:            stringValue(event["request_id"]),
		IdempotencyKey:       stringValue(event["idempotency_key"]),
		RetainLastTransition: true,
		State:                state,
		JournalEvent:         event,
	}
	if err := validatePendingCommitCoherence(pending); err != nil {
		return fmt.Errorf("journal-only commit coherence: %w", err)
	}
	if err := atomicWriteJSON(s.commitMarkerPath(), pending); err != nil {
		return fmt.Errorf("record pending journal-only commit: %w", err)
	}
	if err := appendJSONLine(s.journalPath, event); err != nil {
		return fmt.Errorf("append reconciled runtime journal: %w", err)
	}
	if err := s.maybeRotateJournalLocked(); err != nil {
		return fmt.Errorf("rotate journal: %w", err)
	}
	return s.clearCommitMarkerLocked()
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
		"idempotency_key":     mutation.IdempotencyKey,
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

func intPointer(value int) *int {
	return &value
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

func (s *Store) commitMarkerPath() string {
	return s.statePath + ".commit-pending.json"
}

func (s *Store) fingerprintMarkerPath() string {
	return s.statePath + ".fingerprint-pending.json"
}

// reportPendingOperationLocked is the read-only counterpart of recovery. It
// deliberately performs only metadata reads and returns a diagnostic error;
// Snapshot must never rewrite either half of the Runtime pair.
func (s *Store) reportPendingOperationLocked() error {
	for _, path := range []string{s.commitMarkerPath(), s.fingerprintMarkerPath(), s.rolloverMarkerPath(), s.journalRotationMarkerPath()} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: %s", ErrPendingRuntimeOperation, filepath.Base(path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect pending runtime operation %s: %w", path, err)
		}
	}
	return nil
}

// recoverPendingWritesLocked is called only by explicit mutation-capable
// writers. Each recovery candidate is validated before the first replacement
// of state or journal, so a pending marker cannot become a semantic bypass.
func (s *Store) recoverPendingWritesLocked() error {
	if _, err := os.Stat(s.commitMarkerPath()); err == nil {
		if err := s.recoverPendingCommitLocked(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect pending runtime commit: %w", err)
	}
	if _, err := os.Stat(s.fingerprintMarkerPath()); err == nil {
		if err := s.recoverPendingFingerprintLocked(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect pending fingerprint refresh: %w", err)
	}
	if _, err := os.Stat(s.journalRotationMarkerPath()); err == nil {
		if err := s.recoverPendingJournalRotationLocked(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect pending journal rotation: %w", err)
	}
	if _, err := os.Stat(s.rolloverMarkerPath()); err == nil {
		return s.recoverPendingRolloverLocked()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect pending runtime rollover: %w", err)
	}
	return nil
}

func (s *Store) recoverPendingFingerprintLocked() error {
	if err := s.requireCandidateValidator(); err != nil {
		return fmt.Errorf("recover pending fingerprint refresh: %w", err)
	}
	data, err := os.ReadFile(s.fingerprintMarkerPath())
	if err != nil {
		return fmt.Errorf("read pending fingerprint refresh: %w", err)
	}
	var pending statePending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending fingerprint refresh: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending fingerprint refresh schema %q", pending.SchemaVersion)
	}
	if err := s.validateCandidate(pending.State); err != nil {
		return fmt.Errorf("pending fingerprint refresh state is invalid: %w", err)
	}
	pendingJournal, err := inspectJournal(s.journalPath)
	if err != nil {
		return fmt.Errorf("inspect runtime journal for fingerprint recovery: %w", err)
	}
	if err := validateStateJournalPair(pending.State, pendingJournal); err != nil {
		return fmt.Errorf("pending fingerprint refresh state/journal pair is invalid: %w", err)
	}
	stateHash, err := hashState(pending.State)
	if err != nil {
		return err
	}
	if stateHash != pending.StateSHA256 {
		return errors.New("pending fingerprint refresh state fingerprint mismatch")
	}
	currentState, err := s.read()
	if err != nil {
		return fmt.Errorf("read runtime for fingerprint recovery: %w", err)
	}
	if err := validateStateJournalPair(currentState, pendingJournal); err != nil {
		return fmt.Errorf("current fingerprint refresh state/journal pair is invalid: %w", err)
	}
	currentHash, err := hashState(currentState)
	if err != nil {
		return err
	}
	if currentHash == pending.StateSHA256 {
		return s.clearFingerprintMarkerLocked()
	}
	if currentHash != pending.PreviousStateSHA256 {
		return errors.New("pending fingerprint refresh found an unknown state fingerprint; refusing mixed-state recovery")
	}
	currentRevision, err := integerField(currentState, "revision")
	if err != nil {
		return fmt.Errorf("read current runtime revision for fingerprint recovery: %w", err)
	}
	if currentRevision != pending.PreviousRevision {
		return errors.New("pending fingerprint refresh previous state revision does not match marker")
	}
	if err := atomicWriteJSON(s.statePath, pending.State); err != nil {
		return fmt.Errorf("complete pending fingerprint refresh: %w", err)
	}
	recoveredState, err := s.read()
	if err != nil {
		return fmt.Errorf("read recovered fingerprint refresh state: %w", err)
	}
	recoveredJournal, err := inspectJournal(s.journalPath)
	if err != nil {
		return fmt.Errorf("inspect runtime journal after fingerprint recovery: %w", err)
	}
	if err := validateStateJournalPair(recoveredState, recoveredJournal); err != nil {
		return fmt.Errorf("recovered fingerprint refresh state/journal pair is invalid: %w", err)
	}
	return s.clearFingerprintMarkerLocked()
}

func (s *Store) clearFingerprintMarkerLocked() error {
	if err := os.Remove(s.fingerprintMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending fingerprint refresh: %w", err)
	}
	if err := syncDir(filepath.Dir(s.fingerprintMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending fingerprint refresh: %w", err)
	}
	return nil
}

func (s *Store) recoverPendingCommitLocked() error {
	if err := s.requireCandidateValidator(); err != nil {
		return fmt.Errorf("recover pending runtime commit: %w", err)
	}
	data, err := os.ReadFile(s.commitMarkerPath())
	if err != nil {
		return fmt.Errorf("read pending runtime commit: %w", err)
	}
	var pending commitPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending runtime commit: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending runtime commit schema %q", pending.SchemaVersion)
	}
	if err := s.validateCandidate(pending.State); err != nil {
		return fmt.Errorf("pending runtime commit state is invalid: %w", err)
	}
	if err := validateJournalEvent(pending.JournalEvent); err != nil {
		return fmt.Errorf("pending runtime commit journal is invalid: %w", err)
	}
	if err := validatePendingCommitCoherence(pending); err != nil {
		return fmt.Errorf("pending runtime commit coherence: %w", err)
	}
	journalEventBytes, err := jsonLineBytes(pending.JournalEvent)
	if err != nil {
		return err
	}
	if sha256Hex(journalEventBytes) != pending.JournalEventSHA256 {
		return errors.New("pending runtime commit journal fingerprint mismatch")
	}
	state, err := s.read()
	if err != nil {
		return fmt.Errorf("read runtime for commit recovery: %w", err)
	}
	currentHash, err := hashState(state)
	if err != nil {
		return err
	}
	desiredHash, err := hashState(pending.State)
	if err != nil {
		return err
	}
	if desiredHash != pending.StateSHA256 {
		return errors.New("pending runtime commit state fingerprint mismatch")
	}
	currentRevision, err := integerField(state, "revision")
	if err != nil {
		return fmt.Errorf("read current runtime revision for commit recovery: %w", err)
	}
	eventID, _ := pending.JournalEvent["event_id"].(string)
	if eventID == "" {
		return errors.New("pending runtime commit has no event_id")
	}
	journal, err := inspectJournal(s.journalPath)
	if err != nil {
		return fmt.Errorf("inspect runtime journal during commit recovery: %w", err)
	}
	pendingRuntimeID, _ := pending.State["runtime_id"].(string)
	eventRuntimeID, _ := pending.JournalEvent["runtime_id"].(string)
	if journal.RuntimeID != "" && (journal.RuntimeID != pendingRuntimeID || journal.RuntimeID != eventRuntimeID) {
		return fmt.Errorf("existing runtime journal runtime_id %q does not match pending state/event runtime_id %q", journal.RuntimeID, pendingRuntimeID)
	}
	eventIndex, found := journal.EventIndex[eventID]
	if found {
		if eventIndex != len(journal.Events)-1 {
			return fmt.Errorf("pending runtime commit event %q is not the journal tail", eventID)
		}
		journalEvent := journal.Events[eventIndex]
		journalSequence, sequenceErr := integerField(journalEvent, "sequence")
		pendingSequence, pendingSequenceErr := integerField(pending.JournalEvent, "sequence")
		if sequenceErr != nil || pendingSequenceErr != nil || journalSequence != pendingSequence {
			return fmt.Errorf("pending runtime commit event %q has a journal sequence mismatch", eventID)
		}
		if !reflect.DeepEqual(journalEvent, pending.JournalEvent) {
			return fmt.Errorf("pending runtime commit event %q conflicts with journal", eventID)
		}
	} else {
		pendingSequence, sequenceErr := integerField(pending.JournalEvent, "sequence")
		if sequenceErr != nil {
			return fmt.Errorf("pending runtime commit sequence: %w", sequenceErr)
		}
		if journal.TailSequence != pendingSequence-1 {
			return fmt.Errorf("pending runtime commit journal tail sequence %d does not precede pending sequence %d", journal.TailSequence, pendingSequence)
		}
	}

	switch currentHash {
	case desiredHash:
		if currentRevision != pending.PreviousRevision+1 {
			return errors.New("pending runtime commit desired state revision is not previous revision plus one")
		}
		if found {
			// Both halves already contain the pending commit. The event must be the
			// validated journal tail; no append is needed.
		} else {
			// State reached the target before the process stopped. Only the journal
			// append (and marker cleanup) remain.
		}
		// State reached the target before the process stopped. Only the journal
		// append (if needed) and marker cleanup remain.
	case pending.PreviousStateSHA256:
		if currentRevision != pending.PreviousRevision {
			return errors.New("pending runtime commit previous state revision does not match marker")
		}
		if found {
			return errors.New("pending runtime commit journal event exists while state is still previous")
		}
		if err := atomicWriteJSON(s.statePath, pending.State); err != nil {
			return fmt.Errorf("restore pending runtime state: %w", err)
		}
	default:
		return errors.New("pending runtime commit found an unknown state fingerprint; refusing mixed-pair recovery")
	}
	if !found {
		if err := appendJSONLine(s.journalPath, pending.JournalEvent); err != nil {
			return fmt.Errorf("complete pending runtime journal append: %w", err)
		}
		if err := s.maybeRotateJournalLocked(); err != nil {
			return fmt.Errorf("rotate journal: %w", err)
		}
	}
	return s.clearCommitMarkerLocked()
}

func validatePendingCommitCoherence(pending commitPending) error {
	stateRevision, err := integerField(pending.State, "revision")
	if err != nil {
		return fmt.Errorf("state revision: %w", err)
	}
	eventAfter, err := integerField(pending.JournalEvent, "after_revision")
	if err != nil {
		return fmt.Errorf("journal after_revision: %w", err)
	}
	eventBefore, err := integerField(pending.JournalEvent, "before_revision")
	if err != nil {
		return fmt.Errorf("journal before_revision: %w", err)
	}
	sequence, err := integerField(pending.JournalEvent, "sequence")
	if err != nil {
		return fmt.Errorf("journal sequence: %w", err)
	}
	eventID, _ := pending.JournalEvent["event_id"].(string)
	if eventID == "" {
		return errors.New("journal event_id is required")
	}
	if stateRevision != eventAfter || eventBefore+1 != eventAfter {
		return fmt.Errorf("state revision %d and journal revisions %d->%d are incoherent", stateRevision, eventBefore, eventAfter)
	}
	stateJournal, err := objectField(pending.State, "journal")
	if err != nil {
		return fmt.Errorf("state journal: %w", err)
	}
	stateSequence, err := integerField(stateJournal, "last_sequence")
	if err != nil || stateSequence != sequence {
		return fmt.Errorf("state journal last_sequence %d does not match event sequence %d", stateSequence, sequence)
	}
	stateLastEventID, _ := stateJournal["last_event_id"].(string)
	if stateLastEventID != eventID {
		return fmt.Errorf("state journal last_event_id %q does not match event_id %q", stateLastEventID, eventID)
	}
	stateRuntimeID, _ := pending.State["runtime_id"].(string)
	eventRuntimeID, _ := pending.JournalEvent["runtime_id"].(string)
	if stateRuntimeID == "" || stateRuntimeID != eventRuntimeID {
		return fmt.Errorf("state runtime_id %q does not match journal runtime_id %q", stateRuntimeID, eventRuntimeID)
	}
	requestID, _ := pending.JournalEvent["request_id"].(string)
	if pending.RequestID == "" || pending.RequestID != requestID {
		return fmt.Errorf("marker request_id %q does not match journal request_id %q", pending.RequestID, requestID)
	}
	if pending.IdempotencyKey == "" {
		return errors.New("marker idempotency_key is required")
	}
	eventIdempotencyKey, _ := pending.JournalEvent["idempotency_key"].(string)
	if eventIdempotencyKey == "" {
		return errors.New("journal idempotency_key is required")
	}
	if eventIdempotencyKey != pending.IdempotencyKey {
		return fmt.Errorf("marker idempotency_key %q does not match journal idempotency_key %q", pending.IdempotencyKey, eventIdempotencyKey)
	}
	if !pending.RetainLastTransition {
		transition, err := objectField(pending.State, "last_transition")
		if err != nil {
			return fmt.Errorf("state last_transition: %w", err)
		}
		transitionEventID, _ := transition["event_id"].(string)
		transitionSequence, _ := integerField(transition, "sequence")
		transitionRevision, _ := integerField(transition, "committed_revision")
		transitionIdempotency, _ := transition["idempotency_key"].(string)
		if transitionEventID != eventID || transitionSequence != sequence || transitionRevision != eventAfter || transitionIdempotency != pending.IdempotencyKey {
			return errors.New("state last_transition is not linked to pending journal event")
		}
	}
	return nil
}

func (s *Store) clearCommitMarkerLocked() error {
	if err := os.Remove(s.commitMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending runtime commit: %w", err)
	}
	if err := syncDir(filepath.Dir(s.commitMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending runtime commit: %w", err)
	}
	return nil
}

// recoverPendingRolloverLocked completes a rollover recorded before either
// member of the state/journal pair was replaced. Callers must already hold the
// state lock. The implementation writes the fresh state before resetting the
// journal and therefore explicitly accepts that one durable crash state:
// target state + source journal. The marker makes the retry deterministic.
func (s *Store) recoverPendingRolloverLocked() error {
	if err := s.requireCandidateValidator(); err != nil {
		return fmt.Errorf("recover pending runtime rollover: %w", err)
	}
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
	if pending.SourceStateSHA256 == "" || pending.SourceJournalSHA256 == "" || pending.SourceRuntimeID == "" || pending.SourceRevision == nil || *pending.SourceRevision < 0 {
		return errors.New("pending runtime rollover source binding is incomplete")
	}
	if pending.Record.RuntimeID == "" || pending.Record.Revision < 0 {
		return errors.New("pending runtime rollover archive record is incomplete")
	}
	if pending.Record.RuntimeID != pending.SourceRuntimeID || pending.Record.Revision != *pending.SourceRevision || pending.Record.ArchiveStateSHA != pending.SourceStateSHA256 || pending.Record.ArchiveJournalSHA != pending.SourceJournalSHA256 {
		return errors.New("pending runtime rollover marker cross-binding is incoherent")
	}
	if strings.TrimSpace(pending.Approval.ApprovedBy) == "" || strings.TrimSpace(pending.Approval.EvidenceID) == "" {
		return errors.New("pending runtime rollover approval is incomplete")
	}
	if pending.BoundaryKind == "bind" {
		if err := validateBoundaryTarget(pending.FreshState); err != nil {
			return fmt.Errorf("pending bind boundary target is invalid: %w", err)
		}
	} else if err := ValidateFreshInactiveState(pending.FreshState); err != nil {
		return fmt.Errorf("pending runtime rollover is invalid: %w", err)
	}
	if err := s.validateCandidate(pending.FreshState); err != nil {
		return fmt.Errorf("pending runtime rollover semantic validation: %w", err)
	}
	if err := verifyRolloverArchive(pending.Record); err != nil {
		return fmt.Errorf("pending runtime rollover archive is invalid: %w", err)
	}
	if err := s.validateRolloverArchivePair(pending.Record); err != nil {
		return fmt.Errorf("pending runtime rollover archive journal is invalid: %w", err)
	}
	archivedState, err := readRolloverArchiveState(pending.Record)
	if err != nil {
		return err
	}
	if pending.BoundaryKind != "bind" {
		scopePrefix := "runtime_rollover"
		if pending.Disposition == "unbound" {
			scopePrefix = "runtime_unbind"
		}
		if err := validateLifecycleApproval(archivedState, pending.Approval.ApprovedBy, pending.Approval.EvidenceID, pending.Record.RuntimeID, pending.Record.Revision, scopePrefix); err != nil {
			return fmt.Errorf("pending runtime rollover approval does not match archived runtime: %w", err)
		}
	}

	currentStateData, err := os.ReadFile(s.statePath)
	if err != nil {
		return fmt.Errorf("read current runtime for rollover recovery: %w", err)
	}
	currentJournalData, err := os.ReadFile(s.journalPath)
	if err != nil {
		return fmt.Errorf("read current journal for rollover recovery: %w", err)
	}
	var currentState map[string]any
	if err := json.Unmarshal(currentStateData, &currentState); err != nil {
		return fmt.Errorf("decode current runtime for rollover recovery: %w", err)
	}
	currentStateHash, err := hashState(currentState)
	if err != nil {
		return err
	}
	targetStateHash, err := hashState(pending.FreshState)
	if err != nil {
		return err
	}
	currentJournalHash := sha256Hex(currentJournalData)
	sourcePair := sha256Hex(currentStateData) == pending.SourceStateSHA256 && currentJournalHash == pending.SourceJournalSHA256
	targetPair := currentStateHash == targetStateHash && len(strings.TrimSpace(string(currentJournalData))) == 0
	targetStateSourceJournal := currentStateHash == targetStateHash && currentJournalHash == pending.SourceJournalSHA256
	if !sourcePair && !targetPair && !targetStateSourceJournal {
		return errors.New("pending runtime rollover current state/journal match neither source pair nor completed fresh pair")
	}
	if sourcePair {
		currentRuntimeID, _ := currentState["runtime_id"].(string)
		currentRevision, revisionErr := integerField(currentState, "revision")
		if currentRuntimeID != pending.SourceRuntimeID || revisionErr != nil || currentRevision != *pending.SourceRevision {
			return errors.New("pending runtime rollover source binding does not match current runtime")
		}
		if _, err := inspectJournalData(currentJournalData); err != nil {
			return fmt.Errorf("validate current journal before rollover recovery: %w", err)
		}
		// State and journal are replaced only after every marker, archive,
		// approval, and source-pair check has passed. A subsequent writer either
		// observes the completed fresh pair or fails closed on a mixed pair.
		if err := atomicWriteJSON(s.statePath, pending.FreshState); err != nil {
			return fmt.Errorf("seed fresh runtime during rollover recovery: %w", err)
		}
		if err := atomicWriteBytes(s.journalPath, nil, ".loop-journal-*.tmp"); err != nil {
			return fmt.Errorf("reset runtime journal during rollover recovery: %w", err)
		}
	} else if targetStateSourceJournal {
		if _, err := inspectJournalData(currentJournalData); err != nil {
			return fmt.Errorf("validate source journal in interrupted rollover recovery: %w", err)
		}
		if err := atomicWriteBytes(s.journalPath, nil, ".loop-journal-*.tmp"); err != nil {
			return fmt.Errorf("complete interrupted rollover journal reset: %w", err)
		}
	}
	return s.clearRolloverMarkerLocked()
}

func (s *Store) validateRolloverArchivePair(record RolloverRecord) error {
	archivedState, err := readRolloverArchiveState(record)
	if err != nil {
		return err
	}
	if err := s.validateCandidate(archivedState); err != nil {
		return fmt.Errorf("archived runtime state validation: %w", err)
	}
	runtimeID, _ := archivedState["runtime_id"].(string)
	if runtimeID == "" || runtimeID != record.RuntimeID {
		return fmt.Errorf("archived runtime runtime_id %q does not match archive record %q", runtimeID, record.RuntimeID)
	}
	archivedRevision, err := integerField(archivedState, "revision")
	if err != nil || archivedRevision != record.Revision {
		return fmt.Errorf("archived runtime revision %d does not match archive record %d", archivedRevision, record.Revision)
	}
	journalState, err := objectField(archivedState, "journal")
	if err != nil {
		return fmt.Errorf("archived runtime journal cursor: %w", err)
	}
	lastSequence, err := integerField(journalState, "last_sequence")
	if err != nil {
		return fmt.Errorf("archived runtime journal last_sequence: %w", err)
	}
	lastEventID, _ := journalState["last_event_id"].(string)

	journalData, err := os.ReadFile(filepath.Join(record.ArchiveDir, "loop-events.jsonl"))
	if err != nil {
		return fmt.Errorf("read archived runtime journal: %w", err)
	}
	inspection, err := inspectJournalData(journalData)
	if err != nil {
		return fmt.Errorf("archived journal validation: %w", err)
	}
	if lastSequence != record.Revision {
		return fmt.Errorf("archived state journal last_sequence %d does not match archived revision %d", lastSequence, record.Revision)
	}
	if len(inspection.Events) == 0 {
		if record.Revision != 0 || lastEventID != "" {
			return fmt.Errorf("empty archived journal requires revision 0 and empty event_id, got revision=%d event_id=%q", record.Revision, lastEventID)
		}
		return nil
	}
	if inspection.RuntimeID != runtimeID {
		return fmt.Errorf("archived journal runtime_id %q does not match archived state %q", inspection.RuntimeID, runtimeID)
	}
	for _, event := range inspection.Events {
		sequence, _ := integerField(event, "sequence")
		beforeRevision, err := integerField(event, "before_revision")
		if err != nil || beforeRevision != sequence-1 {
			return fmt.Errorf("archived journal event sequence %d has before_revision %d", sequence, beforeRevision)
		}
		afterRevision, err := integerField(event, "after_revision")
		if err != nil || afterRevision != sequence {
			return fmt.Errorf("archived journal event sequence %d has after_revision %d", sequence, afterRevision)
		}
	}
	finalEvent := inspection.Events[len(inspection.Events)-1]
	finalSequence, _ := integerField(finalEvent, "sequence")
	if finalSequence != lastSequence {
		return fmt.Errorf("archived journal final sequence %d does not match state last_sequence %d", finalSequence, lastSequence)
	}
	finalAfterRevision, _ := integerField(finalEvent, "after_revision")
	if finalAfterRevision != record.Revision {
		return fmt.Errorf("archived journal final after_revision %d does not match archived revision %d", finalAfterRevision, record.Revision)
	}
	finalEventID, _ := finalEvent["event_id"].(string)
	if finalEventID != lastEventID {
		return fmt.Errorf("archived journal final event_id %q does not match state last_event_id %q", finalEventID, lastEventID)
	}
	return nil
}

func readRolloverArchiveState(record RolloverRecord) (map[string]any, error) {
	stateData, err := os.ReadFile(filepath.Join(record.ArchiveDir, "loop-state.json"))
	if err != nil {
		return nil, fmt.Errorf("read archived runtime state: %w", err)
	}
	var archivedState map[string]any
	if err := json.Unmarshal(stateData, &archivedState); err != nil {
		return nil, fmt.Errorf("decode archived runtime state: %w", err)
	}
	return archivedState, nil
}

func (s *Store) clearRolloverMarkerLocked() error {
	if err := os.Remove(s.rolloverMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending runtime rollover: %w", err)
	}
	if err := syncDir(filepath.Dir(s.rolloverMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending runtime rollover: %w", err)
	}
	return nil
}

// ValidateBindEligibleState verifies that the inactive runtime has not made
// business progress and may therefore be replaced by TR-001. The revision is
// deliberately not part of this predicate: controller checkpoints may have
// advanced the CAS cursor before the human binds a REQ.
func ValidateBindEligibleState(state map[string]any) error {
	if state["runtime_id"] != "loop-inactive" {
		return errors.New("runtime_id must be loop-inactive")
	}
	if revision, err := integerField(state, "revision"); err != nil || revision < 0 {
		return errors.New("revision must be a non-negative integer")
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
	if sequence, err := integerField(journal, "last_sequence"); err != nil || sequence < 0 {
		return errors.New("journal cursor sequence must be non-negative")
	}
	if eventID := journal["last_event_id"]; eventID != nil {
		if value, ok := eventID.(string); !ok || strings.TrimSpace(value) == "" {
			return errors.New("journal last_event_id must be null or a non-empty string")
		}
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

// ValidateFreshInactiveState verifies the canonical revision-zero state used
// by init, rollover, and unbind. TR-001 uses ValidateBindEligibleState
// instead because bind is the operation that creates the next revision-zero
// runtime boundary.
func ValidateFreshInactiveState(state map[string]any) error {
	if err := ValidateBindEligibleState(state); err != nil {
		return err
	}
	if revision, err := integerField(state, "revision"); err != nil || revision != 0 {
		return errors.New("revision must be zero")
	}
	journal, err := objectField(state, "journal")
	if err != nil {
		return errors.New("journal must be an object")
	}
	if sequence, err := integerField(journal, "last_sequence"); err != nil || sequence != 0 || journal["last_event_id"] != nil {
		return errors.New("journal cursor must be empty")
	}
	return nil
}

// validateLifecycleApproval verifies that the human-decision evidence for a
// lifecycle verb (rollover, unbind) is current, valid, produced by the named
// approver, and scoped to exactly this runtime and revision. The scope
// prefix distinguishes the verb so an unbind receipt cannot authorize a
// rollover and vice versa.
func validateLifecycleApproval(state map[string]any, approvedBy, evidenceID, runtimeID string, revision int, scopePrefix string) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return errors.New("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["id"] != evidenceID || item["kind"] != "human_decision" || item["status"] != "valid" {
			continue
		}
		if containsString(item["produced_by"], approvedBy) && containsString(item["scope_refs"], fmt.Sprintf("%s:%s@%d", scopePrefix, runtimeID, revision)) {
			return nil
		}
	}
	return fmt.Errorf("%s approval evidence %q must be valid human_decision evidence produced by %q and scoped to %s:%s@%d", scopePrefix, evidenceID, approvedBy, scopePrefix, runtimeID, revision)
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

func (s *Store) journalRotationMarkerPath() string {
	return s.statePath + ".journal-rotation-pending.json"
}

func (s *Store) recoverPendingJournalRotationLocked() error {
	data, err := os.ReadFile(s.journalRotationMarkerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending journal rotation: %w", err)
	}
	var pending journalRotationPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending journal rotation: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending journal rotation schema %q", pending.SchemaVersion)
	}
	if pending.ArchivedFile == "" || pending.TailSequence <= 0 || pending.ArchivedCount <= 0 {
		return errors.New("pending journal rotation record is incomplete")
	}
	// Validate archived segment exists and hashes correctly.
	archData, err := os.ReadFile(pending.ArchivedFile)
	if err != nil {
		return fmt.Errorf("read archived journal segment: %w", err)
	}
	if sha256Hex(archData) != pending.ArchivedSHA256 {
		return errors.New("pending journal rotation archive hash mismatch")
	}
	// RC-13: marker-driven truncation. The active journal may still hold the
	// archived prefix because the rotate was interrupted after writing the
	// archive but before truncating the active segment. We must NOT rely on
	// full-journal contiguous-sequence check, which rejects the duplicate
	// archived prefix still in active and cannot parse an already-truncated
	// tail that starts above sequence one. Instead, inspect the active segment,
	// slice it at pending.ArchivedCount when the archived prefix is present,
	// or validate the tail-only suffix when the atomic truncation already won.
	activeData, err := os.ReadFile(s.journalPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read active journal for rotation recovery: %w", err)
	}
	if err == nil && len(activeData) > 0 {
		activeInspection, aerr := inspectJournalSegmentData(activeData)
		if aerr != nil {
			return fmt.Errorf("pending journal rotation active journal invalid: %w", aerr)
		}
		if activeInspection.TailSequence != pending.TailSequence {
			return fmt.Errorf("pending journal rotation active tail sequence %d does not match marker tail sequence %d", activeInspection.TailSequence, pending.TailSequence)
		}
		// Truncation is complete when the active journal is the suffix that
		// starts at the marker's tail sequence. This is the crash window after
		// the atomic active-file rewrite but before the marker was cleared; the
		// segment parser accepts its non-one starting sequence and the merged
		// pair is validated below.
		firstSequence, _ := integerField(activeInspection.Events[0], "sequence")
		if len(activeInspection.Events) < pending.ArchivedCount {
			if firstSequence != pending.TailSequence || len(activeInspection.Events) != 1 {
				return fmt.Errorf("pending journal rotation active event count %d is less than marker archived count %d and is not the expected tail-only suffix; refusing partial-truncate recovery", len(activeInspection.Events), pending.ArchivedCount)
			}
			archiveInspection, archiveErr := inspectJournalSegmentData(archData)
			if archiveErr != nil {
				return fmt.Errorf("pending journal rotation archived segment invalid: %w", archiveErr)
			}
			if archiveInspection.TailSequence != pending.TailSequence-1 {
				return fmt.Errorf("pending journal rotation archive tail sequence %d does not precede marker tail sequence %d", archiveInspection.TailSequence, pending.TailSequence)
			}
		} else if len(activeInspection.Events) > pending.ArchivedCount {
			tailEvents := activeInspection.Events[pending.ArchivedCount:]
			var buf []byte
			for _, ev := range tailEvents {
				line, err := jsonLineBytes(ev)
				if err != nil {
					return err
				}
				buf = append(buf, line...)
			}
			if err := atomicWriteBytes(s.journalPath, buf, ".loop-journal-*.tmp"); err != nil {
				return fmt.Errorf("complete pending journal rotation truncate: %w", err)
			}
		} else {
			return fmt.Errorf("pending journal rotation active event count %d equals marker archived count %d but does not contain a tail-only suffix; refusing ambiguous recovery", len(activeInspection.Events), pending.ArchivedCount)
		}
	}
	// Validate the recovered journal pair if state exists — fail-closed on
	// mismatch. RC-13 contract: retain the marker and return the error so
	// the operator can run `runtime reconcile` to align state and journal.
	if _, err := os.Stat(s.statePath); err == nil {
		state, err := s.read()
		if err == nil {
			inspection, err := inspectJournal(s.journalPath)
			if err != nil {
				return fmt.Errorf("pending journal rotation journal invalid after recovery: %w", err)
			}
			if err := validateStateJournalPair(state, inspection); err != nil {
				// Fail-closed: keep the marker and surface the mismatch so
				// callers (Snapshot/Update) refuse to write through a
				// diverged state/journal pair. The marker remains on disk
				// for the operator to inspect and reconcile.
				return fmt.Errorf("rotation recovery: state/journal mismatch: %w", err)
			}
		}
	}
	if err := os.Remove(s.journalRotationMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending journal rotation: %w", err)
	}
	if err := syncDir(filepath.Dir(s.journalRotationMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending journal rotation: %w", err)
	}
	return nil
}

// maybeRotateJournalLocked checks whether the active journal segment exceeds the
// threshold and, if so, archives it to a durable segment file. The caller must
// hold the state lock; the operation is crash-safe via a marker transaction.
func (s *Store) maybeRotateJournalLocked() error {
	count, err := journalActiveLineCount(s.journalPath)
	if err != nil {
		return err
	}
	if count <= journalRotationThreshold {
		return nil
	}
	activeData, err := os.ReadFile(s.journalPath)
	if err != nil {
		return fmt.Errorf("read active journal for rotation: %w", err)
	}
	if len(activeData) == 0 {
		return nil
	}
	inspection, err := inspectJournalSegmentData(activeData)
	if err != nil {
		return fmt.Errorf("inspect active journal for rotation: %w", err)
	}
	if inspection.TailSequence == 0 {
		return nil
	}
	// Avoid truncating to empty: keep at least one event in active.
	if len(inspection.Events) <= 1 {
		return nil
	}
	archiveCount := len(inspection.Events) - 1
	archivedEvents := inspection.Events[:archiveCount]
	tailEvents := inspection.Events[archiveCount:]
	var archBuf []byte
	for _, ev := range archivedEvents {
		line, err := jsonLineBytes(ev)
		if err != nil {
			return err
		}
		archBuf = append(archBuf, line...)
	}
	lastSeq, _ := integerField(archivedEvents[len(archivedEvents)-1], "sequence")
	archFile := s.journalPath + ".archive." + strconv.Itoa(lastSeq) + ".jsonl"
	// Avoid overwriting an existing archive (idempotent retry uses same file).
	if _, err := os.Stat(archFile); err == nil {
		// Already rotated — truncate if needed and clear marker.
		data, _ := os.ReadFile(archFile)
		if data != nil && sha256Hex(data) == sha256Hex(archBuf) {
			var tailBuf []byte
			for _, ev := range tailEvents {
				line, _ := jsonLineBytes(ev)
				tailBuf = append(tailBuf, line...)
			}
			if err := atomicWriteBytes(s.journalPath, tailBuf, ".loop-journal-*.tmp"); err != nil {
				return fmt.Errorf("rotate journal truncate (already archived): %w", err)
			}
			_ = os.Remove(s.journalRotationMarkerPath())
			return nil
		}
	}
	pending := journalRotationPending{
		SchemaVersion:  "1.0.0",
		ArchivedFile:   archFile,
		ArchivedSHA256: sha256Hex(archBuf),
		ArchivedCount:  archiveCount,
		TailSequence:   inspection.TailSequence,
		TailEventID:    inspection.Events[len(inspection.Events)-1]["event_id"].(string),
		StartedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := atomicWriteJSON(s.journalRotationMarkerPath(), pending); err != nil {
		return fmt.Errorf("record pending journal rotation: %w", err)
	}
	if err := writeDurableFile(archFile, archBuf); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("write journal archive: %w", err)
		}
	}
	var tailBuf []byte
	for _, ev := range tailEvents {
		line, err := jsonLineBytes(ev)
		if err != nil {
			return err
		}
		tailBuf = append(tailBuf, line...)
	}
	if err := atomicWriteBytes(s.journalPath, tailBuf, ".loop-journal-*.tmp"); err != nil {
		return fmt.Errorf("rotate journal truncate: %w", err)
	}
	if err := os.Remove(s.journalRotationMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear pending journal rotation: %w", err)
	}
	if err := syncDir(filepath.Dir(s.journalRotationMarkerPath())); err != nil {
		return fmt.Errorf("sync cleared pending journal rotation: %w", err)
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := jsonDocumentBytes(value)
	if err != nil {
		return err
	}
	return atomicWriteBytes(path, data, ".loop-state-*.tmp")
}

func jsonDocumentBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode runtime: %w", err)
	}
	return append(data, '\n'), nil
}

func jsonLineBytes(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode journal: %w", err)
	}
	return append(data, '\n'), nil
}

func cloneState(state map[string]any) (map[string]any, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("clone runtime state: %w", err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("decode cloned runtime state: %w", err)
	}
	return clone, nil
}

func hashState(state map[string]any) (string, error) {
	data, err := jsonDocumentBytes(state)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
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

type journalRotationPending struct {
	SchemaVersion  string `json:"schema_version"`
	ArchivedFile   string `json:"archived_file"`
	ArchivedSHA256 string `json:"archived_sha256"`
	ArchivedCount  int    `json:"archived_count"`
	TailSequence   int    `json:"tail_sequence"`
	TailEventID    string `json:"tail_event_id"`
	StartedAt      string `json:"started_at"`
}

// journalRotationThreshold is the RC-10 Step B rotation trigger. When the
// active journal segment exceeds this line count, the next mutation-capable
// writer archives it to a durable segment file
// (loop-events.jsonl.archive.<tailSequence>.jsonl) via a marker transaction.
const journalRotationThreshold = 10000

// JournalRotationThreshold exposes the rotation threshold for diagnostics
// and tests; exceeding it triggers a segment archive on the next writer.
const JournalRotationThreshold = journalRotationThreshold

// journalDiagnosticEnv gates the append-time rotation diagnostic.
const journalDiagnosticEnv = "LOOP_HARNESS_JOURNAL_DIAGNOSTIC"

// JournalNeedsRotation reports whether the durable journal (including archived
// segments) has grown past journalRotationThreshold lines.
func JournalNeedsRotation(path string) (bool, int, error) {
	count, err := journalLineCount(path)
	if err != nil {
		return false, 0, err
	}
	return count > journalRotationThreshold, count, nil
}

// journalLineCount counts JSONL events across all journal segments (archived
// segments plus the active file). A missing journal is an empty tail.
func journalLineCount(path string) (int, error) {
	segments, err := journalSegmentPaths(path)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, seg := range segments {
		n, err := countJournalLines(seg)
		if err != nil {
			return total, err
		}
		total += n
	}
	n, err := countJournalLines(path)
	if err != nil {
		return total, err
	}
	return total + n, nil
}

func countJournalLines(path string) (int, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open journal for line count: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan journal for line count: %w", err)
	}
	return count, nil
}

func journalActiveLineCount(path string) (int, error) {
	return countJournalLines(path)
}

// JournalSegmentPaths returns the sorted (by rotation sequence) list of
// archive segment paths for the given active journal. Non-numeric archive
// names are filtered out so they cannot participate in the contiguous
// merge performed by inspectJournal. Exported for tests and tooling.
func JournalSegmentPaths(path string) ([]string, error) {
	return journalSegmentPaths(path)
}

// ExtractArchiveSeq returns the rotation sequence encoded in an archive
// file name of the form `<journal>.archive.<sequence>.jsonl`. A
// non-numeric or missing sequence is reported as math.MaxInt so the
// file sorts to the end (and is filtered out by JournalSegmentPaths).
func ExtractArchiveSeq(p string) int {
	return extractArchiveSeq(p)
}

func journalSegmentPaths(path string) ([]string, error) {
	pattern := path + ".archive.*.jsonl"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob journal archives: %w", err)
	}
	// Filter out files whose archive sequence cannot be parsed. The
	// marker-driven rotation guarantees `.archive.<sequence>.jsonl`
	// naming; any non-numeric or malformed segment is a stray file that
	// must not participate in the contiguous merge (otherwise inspectJournal
	// would fail on its malformed JSONL).
	filtered := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := parseArchiveSeq(m); !ok {
			continue
		}
		filtered = append(filtered, m)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, _ := parseArchiveSeq(filtered[i])
		right, _ := parseArchiveSeq(filtered[j])
		return left < right
	})
	return filtered, nil
}

// parseArchiveSeq extracts the rotation sequence from an archive path of
// the form `<journal>.archive.<sequence>.jsonl`. The second return value
// is false when the segment name is non-numeric or otherwise malformed.
func parseArchiveSeq(p string) (int, bool) {
	idx := strings.LastIndex(p, ".archive.")
	if idx < 0 {
		return 0, false
	}
	rest := p[idx+len(".archive."):]
	rest = strings.TrimSuffix(rest, ".jsonl")
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

func extractArchiveSeq(p string) int {
	n, ok := parseArchiveSeq(p)
	if !ok {
		return math.MaxInt
	}
	return n
}

// emitJournalRotationDiagnostic reports the journal size when the diagnostic
// env is set.
func emitJournalRotationDiagnostic(path string) {
	if os.Getenv(journalDiagnosticEnv) == "" {
		return
	}
	needs, count, err := JournalNeedsRotation(path)
	if err != nil {
		return
	}
	if needs {
		fmt.Fprintf(os.Stderr, "loop-harness: journal %s has %d events (rotation threshold %d)\n", path, count, journalRotationThreshold)
	}
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
	emitJournalRotationDiagnostic(path)
	return nil
}

type journalInspection struct {
	Events       []map[string]any
	EventIndex   map[string]int
	RuntimeID    string
	TailSequence int
}

func validateStateJournalPair(state map[string]any, inspection journalInspection) error {
	runtimeID, _ := state["runtime_id"].(string)
	if strings.TrimSpace(runtimeID) == "" {
		return errors.New("state runtime_id is required")
	}
	if inspection.RuntimeID != "" && inspection.RuntimeID != runtimeID {
		return fmt.Errorf("journal runtime_id %q does not match state runtime_id %q", inspection.RuntimeID, runtimeID)
	}
	stateJournal, err := objectField(state, "journal")
	if err != nil {
		return fmt.Errorf("state journal: %w", err)
	}
	return validateStateJournalCursor(stateJournal, inspection)
}

func validateStateJournalCursor(stateJournal map[string]any, inspection journalInspection) error {
	stateSequence, err := integerField(stateJournal, "last_sequence")
	if err != nil {
		return fmt.Errorf("state last_sequence: %w", err)
	}
	if inspection.TailSequence != stateSequence {
		return fmt.Errorf("existing runtime journal tail sequence %d does not match state last_sequence %d", inspection.TailSequence, stateSequence)
	}
	stateEventID := stateJournal["last_event_id"]
	if inspection.TailSequence == 0 {
		if stateEventID == nil {
			return nil
		}
		if eventID, ok := stateEventID.(string); ok && strings.TrimSpace(eventID) == "" {
			return errors.New("state journal last_event_id must be null when the journal is empty")
		}
		return fmt.Errorf("state journal last_event_id %v must be null when the journal is empty", stateEventID)
	}
	if len(inspection.Events) == 0 {
		return errors.New("non-empty state journal cursor has no journal tail")
	}
	tailEventID, _ := inspection.Events[len(inspection.Events)-1]["event_id"].(string)
	if tailEventID == "" {
		return errors.New("journal tail event_id is required")
	}
	stateEventIDString, ok := stateEventID.(string)
	if !ok || stateEventIDString != tailEventID {
		return fmt.Errorf("state journal last_event_id %q does not match journal tail event_id %q", stateEventIDString, tailEventID)
	}
	return nil
}

// inspectJournal validates the complete journal (archived segments + active
// file) before commit recovery makes a decision. A missing journal is an empty
// tail.
func inspectJournal(path string) (journalInspection, error) {
	segments, err := journalSegmentPaths(path)
	if err != nil {
		return journalInspection{}, err
	}
	var combined []byte
	for _, seg := range segments {
		data, err := os.ReadFile(seg)
		if err != nil {
			return journalInspection{}, fmt.Errorf("read journal archive %s: %w", seg, err)
		}
		if len(data) > 0 {
			combined = append(combined, data...)
			if data[len(data)-1] != '\n' {
				combined = append(combined, '\n')
			}
		}
	}
	activeData, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return journalInspection{}, fmt.Errorf("read journal: %w", err)
		}
	} else if len(activeData) > 0 {
		combined = append(combined, activeData...)
	}
	if len(combined) == 0 {
		return journalInspection{Events: []map[string]any{}, EventIndex: map[string]int{}}, nil
	}
	return inspectJournalData(combined)
}

func inspectJournalData(data []byte) (journalInspection, error) {
	return inspectJournalSegmentDataFrom(data, 1)
}

// inspectJournalSegmentData validates one active/archive segment while
// allowing its first sequence to be greater than one. Active journal files
// are retained as the tail after rotation, so their first event may be the
// sequence immediately following an archived segment.
func inspectJournalSegmentData(data []byte) (journalInspection, error) {
	return inspectJournalSegmentDataFrom(data, 0)
}

func inspectJournalSegmentDataFrom(data []byte, firstSequence int) (journalInspection, error) {
	inspection := journalInspection{
		Events:     make([]map[string]any, 0),
		EventIndex: make(map[string]int),
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	expectedSequence := firstSequence
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return journalInspection{}, fmt.Errorf("decode journal event: %w", err)
		}
		if err := validateJournalEvent(event); err != nil {
			return journalInspection{}, err
		}
		sequence, err := integerField(event, "sequence")
		if expectedSequence == 0 {
			expectedSequence = sequence
		}
		if err != nil || sequence != expectedSequence {
			return journalInspection{}, fmt.Errorf("journal sequence %d is not contiguous; expected %d", sequence, expectedSequence)
		}
		runtimeID, _ := event["runtime_id"].(string)
		if runtimeID == "" {
			return journalInspection{}, errors.New("journal runtime_id is required")
		}
		if inspection.RuntimeID == "" {
			inspection.RuntimeID = runtimeID
		} else if inspection.RuntimeID != runtimeID {
			return journalInspection{}, fmt.Errorf("journal runtime_id %q does not match journal runtime_id %q", runtimeID, inspection.RuntimeID)
		}
		eventID, _ := event["event_id"].(string)
		if eventID == "" {
			return journalInspection{}, errors.New("journal event_id is required")
		}
		if _, exists := inspection.EventIndex[eventID]; exists {
			return journalInspection{}, fmt.Errorf("journal event_id %q is duplicated", eventID)
		}
		inspection.EventIndex[eventID] = len(inspection.Events)
		inspection.Events = append(inspection.Events, event)
		inspection.TailSequence = sequence
		expectedSequence++
	}
	if err := scanner.Err(); err != nil {
		return journalInspection{}, fmt.Errorf("scan journal: %w", err)
	}
	return inspection, nil
}

func journalContains(path, eventID string) (bool, error) {
	segments, err := journalSegmentPaths(path)
	if err != nil {
		return false, err
	}
	for _, seg := range segments {
		found, err := journalFileContains(seg, eventID)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return journalFileContains(path, eventID)
}

func journalFileContains(path, eventID string) (bool, error) {
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
