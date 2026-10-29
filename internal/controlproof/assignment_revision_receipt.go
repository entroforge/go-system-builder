package controlproof

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

// AssignmentRevisionReceipt is the append-only receipt emitted by the
// original register-workgroup Runtime Writer when it replaces one canonical
// Assignment Record. It is valid only alongside the matching immutable Record
// and original journal event.
type AssignmentRevisionReceipt struct {
	IntentKey               string                            `json:"intent_key"`
	RequestDigest           string                            `json:"request_digest"`
	AssignmentRecord        assignmentrecord.RecordReference  `json:"assignment_record"`
	AssignmentRecordSHA256  string                            `json:"assignment_record_sha256"`
	PreviousRecord          assignmentrecord.RecordReference  `json:"previous_record"`
	PreviousRecordSHA256    string                            `json:"previous_record_sha256"`
	RuntimeEventID          string                            `json:"runtime_event_id"`
	RuntimeCommitRevision   int                               `json:"runtime_commit_revision"`
	PreviousSourceSnapshots []assignmentrecord.SourceSnapshot `json:"previous_source_snapshots,omitempty"`
}

// AssignmentRevisionEventID is stable across retries and independent of the
// Runtime's unrelated commit count.
func AssignmentRevisionEventID(ref assignmentrecord.RecordReference, intentKey string) string {
	return "evt-s6-assignment-revision-" + sha256Hex([]byte(assignmentRevisionPartition(ref, intentKey)))[:32]
}

// AssignmentRevisionIdempotencyKey names the stable revision intent. The
// request digest is checked separately so a repeated key cannot change the
// authorized source bundle.
func AssignmentRevisionIdempotencyKey(ref assignmentrecord.RecordReference, intentKey string) string {
	return "runtime:s6-assignment-revision:" + sha256Hex([]byte(assignmentRevisionPartition(ref, intentKey)))
}

func assignmentRevisionPartition(ref assignmentrecord.RecordReference, intentKey string) string {
	return fmt.Sprintf("%s\x00%d\x00%s\x00%s", ref.RuntimeID, ref.BaselineGeneration, ref.AssignmentID, intentKey)
}

// ValidateAssignmentRevisionReceipts checks that every persisted revision
// receipt is exactly tied to its immutable current/history Record and the
// original committed Runtime event. A Record file or state row alone never
// recreates issuance authority.
func ValidateAssignmentRevisionReceipts(state map[string]any, journalBytes []byte) error {
	return ValidateAssignmentRevisionReceiptsContext(context.Background(), state, journalBytes)
}

func ValidateAssignmentRevisionReceiptsContext(ctx context.Context, state map[string]any, journalBytes []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return err
	}
	entities, _ := state["entities"].(map[string]any)
	rawReceipts, hasReceipts := entities["assignment_revision_receipts"]
	if !hasReceipts {
		rawReceipts = []any{}
	}
	data, err := json.Marshal(rawReceipts)
	if err != nil {
		return fmt.Errorf("encode Assignment revision receipt index: %w", err)
	}
	var receipts []AssignmentRevisionReceipt
	if err := json.Unmarshal(data, &receipts); err != nil || receipts == nil || len(receipts) > 1024 {
		return fmt.Errorf("Assignment revision receipt index is invalid or exceeds its budget")
	}
	if len(receipts) == 0 {
		for _, record := range records {
			if record.Revision > 1 {
				return fmt.Errorf("Assignment revision %s r%d has no original issuance receipt", record.AssignmentID, record.Revision)
			}
		}
		return nil
	}
	journal, err := InspectJournalSegmentDataFromContext(ctx, journalBytes, 1)
	if err != nil {
		return fmt.Errorf("inspect Assignment revision journal: %w", err)
	}
	seenIntents := map[string]bool{}
	seenRecords := map[assignmentrecord.RecordReference]bool{}
	coveredRevisions := map[assignmentrecord.RecordReference]bool{}
	for i, receipt := range receipts {
		if strings.TrimSpace(receipt.IntentKey) == "" || len(receipt.IntentKey) > 1024 || !validSHA256(receipt.RequestDigest) || !validSHA256(receipt.AssignmentRecordSHA256) || !validSHA256(receipt.PreviousRecordSHA256) || receipt.RuntimeEventID == "" || receipt.RuntimeCommitRevision < 1 {
			return fmt.Errorf("Assignment revision receipt %d has incomplete identity or digest fields", i)
		}
		newRef, oldRef := receipt.AssignmentRecord, receipt.PreviousRecord
		if newRef.RuntimeID == "" || newRef.BaselineGeneration < 1 || newRef.AssignmentID == "" || newRef.Revision < 2 || oldRef.RuntimeID != newRef.RuntimeID || oldRef.BaselineGeneration != newRef.BaselineGeneration || oldRef.AssignmentID != newRef.AssignmentID || oldRef.Revision != newRef.Revision-1 {
			return fmt.Errorf("Assignment revision receipt %d has an invalid previous/current Record pair", i)
		}
		partition := assignmentRevisionPartition(oldRef, receipt.IntentKey)
		if seenIntents[partition] || seenRecords[newRef] {
			return fmt.Errorf("Assignment revision receipt index duplicates an intent or current Record")
		}
		seenIntents[partition], seenRecords[newRef] = true, true
		newRecord, found, err := assignmentrecord.FindRecordRevision(records, newRef)
		if err != nil || !found {
			return fmt.Errorf("Assignment revision receipt %d has no exact issued Record: %v", i, err)
		}
		oldRecord, found, err := assignmentrecord.FindRecordRevision(records, oldRef)
		if err != nil || !found {
			return fmt.Errorf("Assignment revision receipt %d has no exact previous Record: %v", i, err)
		}
		newSHA, err := newRecord.Digest()
		if err != nil || newSHA != receipt.AssignmentRecordSHA256 {
			return fmt.Errorf("Assignment revision receipt %d current Record digest differs", i)
		}
		oldSHA, err := oldRecord.Digest()
		if err != nil || oldSHA != receipt.PreviousRecordSHA256 {
			return fmt.Errorf("Assignment revision receipt %d previous Record digest differs", i)
		}
		issuance := newRecord.RevisionIssuance
		if issuance == nil || issuance.IntentKey != receipt.IntentKey || issuance.RequestDigest != receipt.RequestDigest || issuance.PreviousRecord != oldRef || issuance.PreviousRecordSHA256 != receipt.PreviousRecordSHA256 {
			return fmt.Errorf("Assignment revision receipt %d differs from the immutable Record issuance binding", i)
		}
		if len(receipt.PreviousSourceSnapshots) == 0 {
			return fmt.Errorf("Assignment revision receipt %d omits the previous Record source audit", i)
		}
		oldWithSnapshots := oldRecord
		oldWithSnapshots.SourceSnapshots = append([]assignmentrecord.SourceSnapshot(nil), receipt.PreviousSourceSnapshots...)
		if err := assignmentrecord.ValidateRecord(oldWithSnapshots); err != nil {
			return fmt.Errorf("Assignment revision receipt %d previous source audit is incomplete: %w", i, err)
		}
		if len(oldRecord.SourceSnapshots) > 0 && !equalAssignmentSourceSnapshots(oldRecord.SourceSnapshots, receipt.PreviousSourceSnapshots) {
			return fmt.Errorf("Assignment revision receipt %d changed the previous Record source audit", i)
		}
		if newRecord.RegistrationRevision != receipt.RuntimeCommitRevision {
			return fmt.Errorf("Assignment revision receipt %d commit revision differs from the issued Record", i)
		}
		coveredRevisions[newRef] = true
		eventID := AssignmentRevisionEventID(oldRef, receipt.IntentKey)
		idempotencyKey := AssignmentRevisionIdempotencyKey(oldRef, receipt.IntentKey)
		if receipt.RuntimeEventID != eventID {
			return fmt.Errorf("Assignment revision receipt %d event ID is not derived from its original intent", i)
		}
		index, ok := journal.EventIndex[eventID]
		if !ok {
			return fmt.Errorf("Assignment revision receipt %d has no original journal event", i)
		}
		event := journal.Events[index]
		actor, _ := event["actor"].(map[string]any)
		generation, generationErr := integerField(event, "baseline_generation")
		afterRevision, afterErr := integerField(event, "after_revision")
		if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "ASSIGNMENT-REVISION" || event["idempotency_key"] != idempotencyKey || event["runtime_id"] != oldRef.RuntimeID || generationErr != nil || generation != oldRef.BaselineGeneration || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || afterErr != nil || afterRevision != receipt.RuntimeCommitRevision {
			return fmt.Errorf("Assignment revision receipt %d does not match its original committed Writer event", i)
		}
	}
	for _, record := range records {
		if record.Revision < 2 {
			continue
		}
		if record.RevisionIssuance == nil || !coveredRevisions[record.Reference()] {
			return fmt.Errorf("Assignment revision %s r%d has no original issuance receipt", record.AssignmentID, record.Revision)
		}
	}
	return nil
}

func equalAssignmentSourceSnapshots(a, b []assignmentrecord.SourceSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
