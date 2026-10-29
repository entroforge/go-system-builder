package runtime_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func acceptedDocumentFixture(t *testing.T) (string, string, string, runtime.Snapshot, runtime.SubmissionRecord) {
	t.Helper()
	root, statePath, journalPath, packet := packetFixture(t)
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, packet, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.RecordEvidence(root, statePath, journalPath, submissionFixture(t, root, packet, "review.json", passAnswer()))
	if err != nil {
		t.Fatal(err)
	}
	record, ok, err := runtime.FindSubmission(snapshot.State, packet.ID)
	if err != nil || !ok {
		t.Fatalf("missing domain reception: %v", err)
	}
	return root, statePath, journalPath, snapshot, record
}

func TestSubmissionEvaluationIsDurableAndIdempotent(t *testing.T) {
	root, statePath, journalPath, before, record := acceptedDocumentFixture(t)
	if _, err := runtime.SubmissionFollowup(before.State, record.FollowupID); err != nil {
		t.Fatal(err)
	}
	first, err := runtime.RecordSubmissionEvaluation(root, statePath, journalPath, record.FollowupID, testCandidateValidator())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.RecordSubmissionEvaluation(root, statePath, journalPath, record.FollowupID, testCandidateValidator())
	if err != nil || replay.Revision != first.Revision {
		t.Fatalf("replayed evaluation committed again: %v", err)
	}
	result, err := runtime.SubmissionFollowup(replay.State, record.FollowupID)
	if !errors.Is(err, runtime.ErrSubmissionFollowupProcessed) || result.ControlStatus != "no_transition" {
		t.Fatalf("lost evaluated result: %+v %v", result, err)
	}
	for _, field := range []string{"lifecycle", "last_transition"} {
		old, err := json.Marshal(before.State[field])
		if err != nil {
			t.Fatal(err)
		}
		current, err := json.Marshal(replay.State[field])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(old, current) {
			t.Fatalf("non-transitioning evaluation changed %s", field)
		}
	}
}

func TestSubmissionFollowupRejectsNewStageEntry(t *testing.T) {
	root, statePath, journalPath, before, record := acceptedDocumentFixture(t)
	writer := runtime.NewWriter(statePath, journalPath, root, testCandidateValidator())
	_, err := writer.UpdateCurrent(runtime.Mutation{EventID: "evt-origin-changed", TransitionID: "REENTRY", Event: "stage_reentered", Actor: "orchestrator", IdempotencyKey: "reentry", OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		state["lifecycle"].(map[string]any)["phase_revision"] = record.OriginEntry + 1
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := runtime.RecordSubmissionEvaluation(root, statePath, journalPath, record.FollowupID, testCandidateValidator())
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.SubmissionFollowup(after.State, record.FollowupID)
	if !errors.Is(err, runtime.ErrSubmissionFollowupProcessed) || result.ControlStatus != "replaced" {
		t.Fatalf("old origin was not retired: %+v %v", result, err)
	}
	if after.State["lifecycle"].(map[string]any)["state"] != before.State["lifecycle"].(map[string]any)["state"] {
		t.Fatal("old submission advanced a new entry")
	}
}

func TestSubmissionFollowupCannotUseUnregisteredEvent(t *testing.T) {
	root, statePath, journalPath, before, _ := acceptedDocumentFixture(t)
	if _, err := runtime.RecordSubmissionEvaluation(root, statePath, journalPath, "unregistered-event", testCandidateValidator()); err == nil {
		t.Fatal("unregistered event gained a control checkpoint")
	}
	after, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("unknown event changed Runtime: %v", err)
	}
}
