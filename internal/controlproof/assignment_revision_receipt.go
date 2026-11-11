package controlproof

import (
	"context"
	"encoding/base64"
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
	AssignmentRecord        assignmentrecord.RecordReference  `json:"assignment_record"`
	PreviousRecord          assignmentrecord.RecordReference  `json:"previous_record"`
	RuntimeEventID          string                            `json:"runtime_event_id"`
	RuntimeCommitRevision   int                               `json:"runtime_commit_revision"`
	PreviousSourceSnapshots []assignmentrecord.SourceSnapshot `json:"previous_source_snapshots,omitempty"`
}

// AssignmentRevisionEventID is stable across retries and independent of the
// Runtime's unrelated commit count.
func AssignmentRevisionEventID(ref assignmentrecord.RecordReference, intentKey string) string {
	return "evt-s6-assignment-revision-" + base64.RawURLEncoding.EncodeToString([]byte(assignmentRevisionPartition(ref, intentKey)))
}

// AssignmentRevisionIdempotencyKey names the stable revision intent. The
// original previous Record and intent identify the original issued responsibility.
func AssignmentRevisionIdempotencyKey(ref assignmentrecord.RecordReference, intentKey string) string {
	return "runtime:s6-assignment-revision:" + base64.RawURLEncoding.EncodeToString([]byte(assignmentRevisionPartition(ref, intentKey)))
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
		if strings.TrimSpace(receipt.IntentKey) == "" || len(receipt.IntentKey) > 1024 || receipt.RuntimeEventID == "" {
			return fmt.Errorf("Assignment revision receipt %d has incomplete identity fields", i)
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
		_, found, err = assignmentrecord.FindRecordRevision(records, oldRef)
		if err != nil || !found {
			return fmt.Errorf("Assignment revision receipt %d has no exact previous Record: %v", i, err)
		}
		issuance := newRecord.RevisionIssuance
		if issuance == nil || issuance.IntentKey != receipt.IntentKey || issuance.PreviousRecord != oldRef {
			return fmt.Errorf("Assignment revision receipt %d differs from the original Record issuance", i)
		}
		// The issued Record is bound to this receipt by its intent key, its
		// previous Record and the exact committed Writer event below. The
		// Runtime's commit counter is a write counter, not an identity: it is
		// recorded on the receipt but never compared and never qualifies it.
		coveredRevisions[newRef] = true
		eventID := receipt.RuntimeEventID // Old event IDs remain opaque recorded identities.
		index, ok := journal.EventIndex[eventID]
		if !ok {
			return fmt.Errorf("Assignment revision receipt %d has no original journal event", i)
		}
		event := journal.Events[index]
		actor, _ := event["actor"].(map[string]any)
		generation, generationErr := integerField(event, "baseline_generation")
		if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "ASSIGNMENT-REVISION" || event["runtime_id"] != oldRef.RuntimeID || generationErr != nil || generation != oldRef.BaselineGeneration || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" {
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
