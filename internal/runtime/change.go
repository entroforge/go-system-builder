package runtime

import (
	"fmt"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/change"
)

// ChangeRequest creates the one active Change Record carried by a Runtime.
// The record is deliberately stored through the same Writer/journal path as
// all other Runtime facts. A negative ExpectedRevision selects the normal
// single-writer path; non-negative values remain an explicit advanced CAS
// assertion.
type ChangeRequest struct {
	ExpectedRevision int
	Record           change.Record
	OccurredAt       time.Time
	Validator        CandidateValidator
}

func CreateChange(root, statePath, journalPath string, request ChangeRequest) (Snapshot, error) {
	if err := change.Validate(request.Record); err != nil {
		return Snapshot{}, err
	}
	store := NewWriter(statePath, journalPath, root, request.Validator)
	snapshot, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	current := snapshot.State
	if existing, ok := current["change"]; ok && existing != nil {
		return Snapshot{}, fmt.Errorf("runtime already has an active Change Record")
	}
	if err := assertBoundREQConsistency(current, request.Record); err != nil {
		return Snapshot{}, err
	}
	if err := assertRequiredChecksInvariant(request.Record); err != nil {
		return Snapshot{}, err
	}
	changeMap, err := change.Encode(request.Record)
	if err != nil {
		return Snapshot{}, err
	}
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	commitRevision := snapshot.Revision
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return store.Update(request.ExpectedRevision, Mutation{
		EventID:        fmt.Sprintf("evt-change-%s-r%d", request.Record.ID, commitRevision+1),
		TransitionID:   "CHANGE-RECORD-CREATE",
		Event:          "change_record_created",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:change:%s:%d", request.Record.ID, commitRevision),
		RuntimeID:      runtimeID,
		From:           map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]},
		To:             map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]},
		EvidenceIDs:    []string{},
		Message:        "Created the active Runtime Change Record.",
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
			if existing, ok := state["change"]; ok && existing != nil {
				return fmt.Errorf("runtime already has an active Change Record")
			}
			if err := assertBoundREQConsistency(state, request.Record); err != nil {
				return err
			}
			if err := assertRequiredChecksInvariant(request.Record); err != nil {
				return err
			}
			state["change"] = changeMap
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}

// assertBoundREQConsistency enforces REQ-001 FR-001 at the Change Record
// boundary: the record's req_ref and req_sha256 must match the Runtime's
// currently bound REQ. Without this, a Change Record could claim lineage to a
// REQ the Runtime is not bound to, creating a second state authority.
func assertBoundREQConsistency(state map[string]any, record change.Record) error {
	bound, ok := state["bound_req"].(map[string]any)
	if !ok || bound == nil {
		return fmt.Errorf("runtime has no bound REQ; cannot create Change Record")
	}
	boundID, _ := bound["id"].(string)
	boundSHA, _ := bound["sha256"].(string)
	if boundID == "" || boundSHA == "" {
		return fmt.Errorf("bound REQ is missing id or sha256; cannot create Change Record")
	}
	if record.REQRef != boundID {
		return fmt.Errorf("change req_ref %q does not match bound REQ %q", record.REQRef, boundID)
	}
	if record.REQSHA != boundSHA {
		return fmt.Errorf("change req_sha256 does not match bound REQ fingerprint")
	}
	return nil
}

// assertRequiredChecksInvariant enforces the deterministic-checks rule from
// REQ-002 FR-005: the record's RequiredChecks must equal DefaultChecks for its
// class/risk/scope. This blocks silent reduction at the CAS boundary, where
// REQ-001's no-partial-state guarantee lives.
func assertRequiredChecksInvariant(record change.Record) error {
	expected := change.DefaultChecks(record.Class, record.Risk, record.Scope)
	if !reflect.DeepEqual(expected, record.RequiredChecks) {
		return fmt.Errorf("change required_checks must equal the deterministic default for class=%q risk=%q (expected %d checks, got %d)", record.Class, record.Risk, len(expected), len(record.RequiredChecks))
	}
	return nil
}
