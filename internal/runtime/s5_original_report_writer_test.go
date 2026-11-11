package runtime_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The system report belongs to the same transaction as its submission. Neither
// an occupied root nor an identical unregistered file is a reception winner.
func TestS5OriginalReportWriterChoosesAndReplaysRegisteredWinner(t *testing.T) {
	for _, obstacle := range []string{"root file", "root symlink", "identical orphan"} {
		t.Run(obstacle, func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			req := submissionFixture(t, root, p, "prepared.json", passAnswer())
			report, err := os.ReadFile(filepath.Join(root, "prepared.json"))
			if err != nil {
				t.Fatal(err)
			}
			req.Submission.EvidenceJSON = report
			req.Path = ".claude/evidence/submissions/original/document-review.json"
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			occupied := filepath.Join(root, ".claude/evidence")
			preserved := []byte("unknown root\n")
			switch obstacle {
			case "root file":
				if err := os.WriteFile(occupied, preserved, 0600); err != nil {
					t.Fatal(err)
				}
			case "root symlink":
				target := t.TempDir()
				if err := os.WriteFile(filepath.Join(target, "unknown"), preserved, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, occupied); err != nil {
					t.Skip(err)
				}
			case "identical orphan":
				occupied = filepath.Join(root, req.Path)
				if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
					t.Fatal(err)
				}
				preserved = report
				if err := os.WriteFile(occupied, preserved, 0600); err != nil {
					t.Fatal(err)
				}
			}
			first, err := runtime.RecordEvidence(root, statePath, journalPath, req)
			if err != nil {
				t.Fatal(err)
			}
			actual, found := runtime.RecordedDocumentReviewReportPath(first.State, req.ID)
			if !found || actual == req.Path {
				t.Fatalf("registered winner = %q, found=%v", actual, found)
			}
			got, err := runtime.ReadRecordedEvidenceReport(root, actual)
			if err != nil || !bytes.Equal(got, report) {
				t.Fatalf("winner differs: %v", err)
			}
			if obstacle == "root symlink" {
				entries, err := os.ReadDir(occupied)
				if err != nil || len(entries) != 1 || entries[0].Name() != "unknown" {
					t.Fatalf("symlink target changed: %v %v", entries, err)
				}
			} else {
				got, err := os.ReadFile(occupied)
				if err != nil || !bytes.Equal(got, preserved) {
					t.Fatalf("unknown object changed: %v", err)
				}
			}
			// Replay must use the complete accepted winner rather than a candidate.
			req.Path = "missing-new-candidate.json"
			replay, err := runtime.RecordEvidence(root, statePath, journalPath, req)
			if err != nil || replay.Revision != first.Revision {
				t.Fatalf("replay: revision=%d err=%v", replay.Revision, err)
			}
			if err := os.WriteFile(filepath.Join(root, actual), []byte("broken winner"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = runtime.RecordEvidence(root, statePath, journalPath, req)
			if err == nil || errors.Is(err, runtime.ErrSubmissionConflict) {
				t.Fatalf("broken winner must be a material error: %v", err)
			}
		})
	}
}

func TestS5PreparedReportConcurrentReceptionUsesCommittedWinner(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(map[bool]string{false: "same answer", true: "different answer"}[different], func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			requests := make([]runtime.EvidenceRequest, 2)
			for i := range requests {
				answer := passAnswer()
				if different && i == 1 {
					answer = fixAnswer(p)
				}
				requests[i] = submissionFixture(t, root, p, "prepared.json", answer)
				data, err := os.ReadFile(filepath.Join(root, "prepared.json"))
				if err != nil {
					t.Fatal(err)
				}
				requests[i].Submission.EvidenceJSON = data
				requests[i].Path = ".claude/evidence/submissions/packet/document-review.json"
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, req := range requests {
				go func(req runtime.EvidenceRequest) {
					<-start
					_, err := runtime.RecordEvidence(root, statePath, journalPath, req)
					results <- err
				}(req)
			}
			close(start)
			successes, conflicts := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					successes++
				} else if errors.Is(err, runtime.ErrSubmissionConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if (!different && successes != 2) || (different && (successes != 1 || conflicts != 1)) {
				t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
			}
			snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if rows := evidenceReplayRows(snapshot.State, requests[0].ID); rows != 1 {
				t.Fatalf("evidence rows = %d", rows)
			}
			path, found := runtime.RecordedDocumentReviewReportPath(snapshot.State, requests[0].ID)
			if !found {
				t.Fatal("missing registered report")
			}
			// There is one published report: a late contender never publishes an orphan
			// before the lock determines whether it is a replay or a conflict.
			entries, err := os.ReadDir(filepath.Dir(filepath.Join(root, path)))
			if err != nil || len(entries) != 1 {
				t.Fatalf("report directory: %v, %v", entries, err)
			}
		})
	}
}
