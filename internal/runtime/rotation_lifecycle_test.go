package runtime_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Storage-level lifecycle fixtures use the same schema-valid journal bytes
// as the existing recovery tests. They do not claim a real human approval or
// product-stage execution; splitting models the original on-disk rotation.
func TestRotatedJournalLifecycleArchivesCompleteOriginalHistory(t *testing.T) {
	for _, operation := range []string{"rollover", "unbind"} {
		t.Run(operation, func(t *testing.T) {
			root, statePath, journalPath, complete := rotatedLifecycleSource(t, operation)
			writer := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{})
			var archive runtime.RolloverRecord
			var err error
			if operation == "rollover" {
				archive, err = writer.Rollover(freshInactiveState(t), filepath.Join(root, "archive"), runtime.RolloverApproval{ApprovedBy: "release-owner", EvidenceID: "ev-approval"}, time.Now().UTC())
			} else {
				archive, err = writer.Unbind(freshInactiveState(t), filepath.Join(root, "archive"), runtime.UnbindApproval{ApprovedBy: "release-owner", EvidenceID: "ev-approval", Reason: "Synthetic explicit revocation for the storage test."}, time.Now().UTC())
			}
			if err != nil {
				t.Fatalf("original %s could not archive its valid rotated history: %v", operation, err)
			}
			if got := mustRead(t, filepath.Join(archive.ArchiveDir, "loop-events.jsonl")); !bytes.Equal(got, complete) {
				t.Fatal("lifecycle archive omitted or rewrote original journal bytes")
			}
			segments, err := runtime.JournalSegmentPaths(journalPath)
			if err != nil || len(segments) != 0 || len(mustRead(t, journalPath)) != 0 {
				t.Fatalf("new Runtime inherited old journal segments: %v err=%v", segments, err)
			}
			fresh, err := writer.Snapshot()
			if err != nil || fresh.Revision != 0 || fresh.State["runtime_id"] != "loop-inactive" {
				t.Fatalf("fresh Runtime is not intact: %+v err=%v", fresh, err)
			}
			first, err := writer.UpdateCurrent(runtime.Mutation{
				EventID: "evt-next-cycle-observation", TransitionID: "MILESTONE-REFRESH", Event: "milestone_refreshed",
				Actor: "Main", IdempotencyKey: "next-cycle-observation", RetainLastTransition: true,
			})
			if err != nil || first.Revision != 1 {
				t.Fatalf("first actual new-cycle write mixed old history: revision=%d err=%v", first.Revision, err)
			}
		})
	}
}

func rotatedLifecycleSource(t *testing.T, operation string) (root, statePath, journalPath string, complete []byte) {
	t.Helper()
	root = t.TempDir()
	statePath = filepath.Join(root, "loop-state.json")
	journalPath = filepath.Join(root, "loop-events.jsonl")
	writeState(t, statePath, 7)
	_, _, _, complete = prepareRolloverSourcePair(t, statePath)
	state := readJSONMapRuntimeTest(t, statePath)
	state["bound_req"] = map[string]any{
		"id": "REQ-099", "path": "docs/requirements/REQ-099.md", "version": "v1.0.0", "sha256": strings.Repeat("0", 64),
		"status": "locked", "approved_by": "release-owner", "approved_at": "2026-10-21T00:00:00Z",
	}
	if operation == "rollover" {
		state["lifecycle"].(map[string]any)["state"] = "aborted"
	}
	state["evidence"].([]any)[0].(map[string]any)["scope_refs"] = []any{"runtime_" + operation + ":loop-test@7"}
	writeJSONMapRuntimeTest(t, statePath, state)
	lines := bytes.SplitAfter(complete, []byte("\n"))
	for path, body := range map[string][]byte{
		journalPath + ".archive.2.jsonl": bytes.Join(lines[:2], nil),
		journalPath + ".archive.5.jsonl": bytes.Join(lines[2:5], nil),
		journalPath:                      bytes.Join(lines[5:], nil),
	} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return
}
