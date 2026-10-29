package runtime_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func revisionRequest(root string, key string) runtime.DocumentReviewRevisionRequest {
	return runtime.DocumentReviewRevisionRequest{PreviousSubmissionKey: key, Reason: "Recheck the concrete task dependency observation", Files: fileview.Disk{Root: root}, Validator: testCandidateValidator(), Authorize: func(map[string]any, workpacket.Packet, workpacket.Packet) error { return nil }}
}

func TestDocumentReviewRevisionSuspendsAndSupersedesAtomically(t *testing.T) {
	root, statePath, journalPath, before, record := acceptedDocumentFixture(t)
	if !runtime.CurrentDocumentReviewEvidence(before.State, record.EvidenceID) {
		t.Fatal("accepted current S5 evidence was not recognized")
	}
	old, _, err := runtime.FindWorkPacket(before.State, record.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	req := revisionRequest(root, record.Key)
	issued, next, err := runtime.RecordDocumentReviewRevision(root, statePath, journalPath, req)
	if err != nil {
		t.Fatal(err)
	}
	if next.Binding.Attempt != 2 || next.Binding.PreviousSubmissionKey != record.Key || next.Binding.Owner != old.Binding.Owner {
		t.Fatal("revision widened or lost the original binding")
	}
	ev := issued.State["evidence"].([]any)[0].(map[string]any)
	if ev["status"] != "invalid" || ev["invalidation_rule"] != "document_review_revision_pending" {
		t.Fatal("old PASS remained qualified while the revision was pending")
	}
	if runtime.CurrentDocumentReviewEvidence(issued.State, record.EvidenceID) {
		t.Fatal("superseded evidence remained current while a revision was pending")
	}
	if _, err := runtime.SubmissionFollowup(issued.State, record.FollowupID); !errors.Is(err, runtime.ErrSubmissionFollowupProcessed) {
		t.Fatalf("old pending followup still usable: %v", err)
	}
	replay, same, err := runtime.RecordDocumentReviewRevision(root, statePath, journalPath, req)
	if err != nil || same.ID != next.ID || replay.Revision != issued.Revision {
		t.Fatalf("issuance replay: %v", err)
	}
	req.Reason = "A different concrete reason"
	if _, _, err := runtime.RecordDocumentReviewRevision(root, statePath, journalPath, req); !errors.Is(err, runtime.ErrRevisionConflict) {
		t.Fatalf("different reason did not conflict: %v", err)
	}
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, submissionFixture(t, root, old, "review.json", passAnswer())); !errors.Is(err, runtime.ErrSubmissionSuperseded) {
		t.Fatalf("old answer replay was authorized: %v", err)
	}
	replacement := submissionFixture(t, root, next, "replacement.json", passAnswer())
	replacement.ID = "ev-replacement"
	replacementData, err := json.Marshal(workpacket.DocumentReview(next, replacement.ID, passAnswer(), time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "replacement.json"), replacementData, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := runtime.RecordEvidence(root, statePath, journalPath, replacement)
	if err != nil {
		t.Fatal(err)
	}
	previous, _, _ := runtime.FindSubmission(after.State, old.ID)
	received, _, _ := runtime.FindSubmission(after.State, next.ID)
	if previous.SupersededBy != received.Key || previous.RevisionPacketID != next.ID || len(after.State["evidence"].([]any)) != 2 {
		t.Fatal("replacement domain and lineage were not committed together")
	}
	if runtime.CurrentDocumentReviewEvidence(after.State, record.EvidenceID) || !runtime.CurrentDocumentReviewEvidence(after.State, received.EvidenceID) {
		t.Fatal("current evidence projection did not select only the accepted revision tip")
	}
	ev = after.State["evidence"].([]any)[0].(map[string]any)
	if ev["invalidated_by"] != received.EvidenceID || ev["invalidation_rule"] != "document_review_revision_superseded" {
		t.Fatal("replacement did not retain explicit original evidence invalidation")
	}
	unchanged, err := os.ReadFile(filepath.Join(root, "review.json"))
	if err != nil || string(original) != string(unchanged) {
		t.Fatal("historical artifact overwritten")
	}
	current, err := runtime.S5Packets(root, after.State, old.Binding.Owner)
	if err != nil || len(current) != 1 || current[0].ID != next.ID {
		t.Fatalf("current revision projection: %v", err)
	}
	// Projection must not depend on serialized array order.
	packets := after.State["entities"].(map[string]any)["work_packets"].([]any)
	packets[0], packets[1] = packets[1], packets[0]
	current, err = runtime.S5Packets(root, after.State, old.Binding.Owner)
	if err != nil || len(current) != 1 || current[0].ID != next.ID {
		t.Fatal("packet array order selected an old attempt")
	}
}

func TestConcurrentDocumentReviewRevisionHasOneSuccessor(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(map[bool]string{false: "same reason", true: "different reason"}[different], func(t *testing.T) {
			root, statePath, journalPath, _, record := acceptedDocumentFixture(t)
			reqs := []runtime.DocumentReviewRevisionRequest{revisionRequest(root, record.Key), revisionRequest(root, record.Key)}
			if different {
				reqs[1].Reason = "Recheck another concrete contradiction"
			}
			var callbacks atomic.Int32
			for i := range reqs {
				reqs[i].Authorize = func(map[string]any, workpacket.Packet, workpacket.Packet) error { callbacks.Add(1); return nil }
			}
			var group sync.WaitGroup
			errs := make([]error, 2)
			packets := make([]workpacket.Packet, 2)
			for i := range reqs {
				group.Add(1)
				go func(i int) {
					defer group.Done()
					_, packets[i], errs[i] = runtime.RecordDocumentReviewRevision(root, statePath, journalPath, reqs[i])
				}(i)
			}
			group.Wait()
			successes, conflicts := 0, 0
			for _, err := range errs {
				if err == nil {
					successes++
				} else if errors.Is(err, runtime.ErrRevisionConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if (!different && successes != 2) || (different && (successes != 1 || conflicts != 1)) || callbacks.Load() != 1 {
				t.Fatalf("successes=%d conflicts=%d source retirements=%d", successes, conflicts, callbacks.Load())
			}
			if !different && packets[0].ID != packets[1].ID {
				t.Fatal("one previous receipt produced two successor identities")
			}
			snap, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.State["entities"].(map[string]any)["work_packets"].([]any)) != 2 {
				t.Fatal("concurrent issuance branched")
			}
		})
	}
}

func TestDocumentReviewRevisionRejectsDriftAndFailedAuthorization(t *testing.T) {
	for _, bad := range []string{"unreceived", "empty reason", "too long", "input drift", "authorization"} {
		t.Run(bad, func(t *testing.T) {
			root, statePath, journalPath, _, record := acceptedDocumentFixture(t)
			req := revisionRequest(root, record.Key)
			switch bad {
			case "unreceived":
				req.PreviousSubmissionKey = workpacket.Digest([]byte("not received"))
			case "empty reason":
				req.Reason = " \n "
			case "too long":
				req.Reason = string(make([]byte, 1025))
			case "input drift":
				if err := os.WriteFile(filepath.Join(root, "req.md"), []byte("drift"), 0600); err != nil {
					t.Fatal(err)
				}
			case "authorization":
				req.Authorize = func(map[string]any, workpacket.Packet, workpacket.Packet) error {
					return errors.New("original activation/source denied")
				}
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := runtime.RecordDocumentReviewRevision(root, statePath, journalPath, req); err == nil {
				t.Fatal("invalid revision accepted")
			}
			after, err := os.ReadFile(statePath)
			if err != nil || string(before) != string(after) {
				t.Fatal("failed revision changed original runtime/evidence")
			}
		})
	}
}

func TestIssuedDocumentReviewRejectsManualParallelEvidence(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
		t.Fatal(err)
	}
	request := submissionFixture(t, root, p, "manual.json", passAnswer())
	request.Submission = nil
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil {
		t.Fatal("manual evidence bypassed the issued answer identity")
	}
	after, err := os.ReadFile(statePath)
	if err != nil || string(before) != string(after) {
		t.Fatal("rejected parallel evidence changed runtime")
	}
}

// S2-S4 and S5 share the original registry in a real installed chain.
func TestDocumentReviewRevisionGuardSelectsOnlyS5Subtype(t *testing.T) {
	root, _, _, before, _ := acceptedDocumentFixture(t)
	entities := before.State["entities"].(map[string]any)
	original := entities["work_packets"].([]any)
	for _, name := range []string{"s2-design-v1", "s3-contracts-v1", "s9-repair-result-v1"} {
		// Foreign domains are opaque to this guard and must never be decoded as S5.
		entities["work_packets"] = append([]any{map[string]any{"template_id": name, "packet_id": "foreign-" + name}}, original...)
		if err := runtime.ValidateCurrentDocumentReviewRevisions(root, before.State, fileview.Disk{Root: root}); err != nil {
			t.Fatalf("foreign subtype %s blocked valid S5 first attempt: %v", name, err)
		}
	}
	entities["work_packets"] = append([]any{map[string]any{"template_id": "s4-task-planning-v1", "packet_id": "current-s4"}}, original...)
	if err := runtime.ValidateCurrentDocumentReviewRevisions(root, before.State, fileview.Disk{Root: root}); err == nil {
		t.Fatal("typed S4 batch without its original accepted receipt and two current S5 passes was accepted")
	}
	entities["work_packets"] = append([]any{map[string]any{"template_id": "s5-document-review-v1", "packet_id": "broken-s5", "binding": "not-an-object"}}, original...)
	if err := runtime.ValidateCurrentDocumentReviewRevisions(root, before.State, fileview.Disk{Root: root}); err == nil {
		t.Fatal("malformed S5 packet bypassed revision guard")
	}
	entities["work_packets"] = append([]any{"not-an-object"}, original...)
	if err := runtime.ValidateCurrentDocumentReviewRevisions(root, before.State, fileview.Disk{Root: root}); err == nil {
		t.Fatal("malformed registry row bypassed revision guard")
	}
}

func TestCurrentDocumentReviewEvidencePreservesLegacyAndRejectsRetiredBatch(t *testing.T) {
	_, _, _, snapshot, record := acceptedDocumentFixture(t)
	team := snapshot.State["entities"].(map[string]any)["teams"].([]any)[0].(map[string]any)
	if !runtime.CurrentDocumentReviewEvidence(snapshot.State, record.EvidenceID) {
		t.Fatal("current original reception was not recognized")
	}
	life := snapshot.State["lifecycle"].(map[string]any)
	originalEntry := life["phase_revision"]
	life["phase_revision"] = record.OriginEntry + 4
	if runtime.CurrentDocumentReviewEvidence(snapshot.State, record.EvidenceID) {
		t.Fatal("old planned team qualified after a same-generation S5 re-entry")
	}
	life["phase_revision"] = originalEntry
	team["status"] = "retired"
	if runtime.CurrentDocumentReviewEvidence(snapshot.State, record.EvidenceID) {
		t.Fatal("evidence from a retired S5 team remained current")
	}
	entities := snapshot.State["entities"].(map[string]any)
	entities["teams"] = []any{}
	entities["work_packets"] = []any{}
	entities["submissions"] = []any{}
	if !runtime.CurrentDocumentReviewEvidence(snapshot.State, record.EvidenceID) {
		t.Fatal("legacy document-review evidence without typed S5 issuance lost compatibility")
	}
	entities["work_packets"] = []any{map[string]any{"template_id": "s4-task-planning-v1", "packet_id": "current-s4"}}
	if runtime.CurrentDocumentReviewEvidence(snapshot.State, record.EvidenceID) {
		t.Fatal("typed S4 entry without an issued/received S5 review fell back to legacy evidence")
	}
}
