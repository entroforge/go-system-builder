package runtime_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

type rotatedResetFixture struct {
	root, statePath, journalPath, markerPath, archiveDir string
	marker                                               map[string]any
	fresh                                                map[string]any
}

func pendingRotatedReset(t *testing.T, operation string) rotatedResetFixture {
	t.Helper()
	root, statePath, journalPath, complete := rotatedLifecycleSource(t, operation)
	state := mustRead(t, statePath)
	archive := filepath.Join(root, "archive", "original-reset")
	if err := os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"loop-state.json": state, "loop-events.jsonl": complete} {
		if err := os.WriteFile(filepath.Join(archive, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fresh := freshInactiveState(t)
	var segments []any
	for _, path := range []string{journalPath + ".archive.2.jsonl", journalPath + ".archive.5.jsonl", journalPath} {
		data := mustRead(t, path)
		segments = append(segments, map[string]any{"name": filepath.Base(path), "size": len(data), "sha256": sha256HexForTest(data)})
	}
	marker := map[string]any{
		"schema_version": "1.0.0", "fresh_state": fresh,
		"record": map[string]any{
			"archive_dir": archive, "runtime_id": "loop-test", "revision": 7,
			"archive_state_sha256": sha256HexForTest(state), "archive_journal_sha256": sha256HexForTest(complete),
		},
		"approval":            map[string]any{"approved_by": "release-owner", "evidence_id": "ev-approval"},
		"source_state_sha256": sha256HexForTest(state), "source_journal_sha256": sha256HexForTest(complete),
		"source_runtime_id": "loop-test", "source_revision": 7, "source_journal_segments": segments,
	}
	if operation == "unbind" {
		marker["disposition"] = "unbound"
	}
	f := rotatedResetFixture{root: root, statePath: statePath, journalPath: journalPath, markerPath: statePath + ".rollover-pending.json", archiveDir: archive, marker: marker, fresh: fresh}
	writeJSONMapRuntimeTest(t, f.markerPath, marker)
	return f
}

func TestRotatedLifecycleOriginalPendingRecoversEveryCleanupWindow(t *testing.T) {
	for _, operation := range []string{"rollover", "unbind"} {
		for _, window := range []string{"source", "target-state", "active-reset", "one-segment-deleted", "all-segments-deleted"} {
			t.Run(operation+"/"+window, func(t *testing.T) {
				f := pendingRotatedReset(t, operation)
				archiveBefore := mustRead(t, filepath.Join(f.archiveDir, "loop-events.jsonl"))
				if window != "source" {
					writeJSONMapRuntimeTest(t, f.statePath, f.fresh)
				}
				if window != "source" && window != "target-state" {
					if err := os.WriteFile(f.journalPath, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if window == "one-segment-deleted" || window == "all-segments-deleted" {
					if err := os.Remove(f.journalPath + ".archive.2.jsonl"); err != nil {
						t.Fatal(err)
					}
				}
				if window == "all-segments-deleted" {
					if err := os.Remove(f.journalPath + ".archive.5.jsonl"); err != nil {
						t.Fatal(err)
					}
				}
				writer := runtime.NewWriter(f.statePath, f.journalPath, f.root, integrityTestValidator{})
				if recovered, err := writer.RecoverPendingOperations(); err != nil || !recovered {
					t.Fatalf("original pending could not finish %s: recovered=%t err=%v", window, recovered, err)
				}
				segments, err := runtime.JournalSegmentPaths(f.journalPath)
				if err != nil || len(segments) != 0 || len(mustRead(t, f.journalPath)) != 0 {
					t.Fatalf("recovered target retained old source: segments=%v err=%v", segments, err)
				}
				state, err := writer.Snapshot()
				if err != nil || state.Revision != 0 || state.State["runtime_id"] != "loop-inactive" {
					t.Fatalf("recovery produced a different target: %+v err=%v", state, err)
				}
				if !bytes.Equal(archiveBefore, mustRead(t, filepath.Join(f.archiveDir, "loop-events.jsonl"))) {
					t.Fatal("cleanup changed the archived source")
				}
				stateBefore, journalBefore := mustRead(t, f.statePath), mustRead(t, f.journalPath)
				if recovered, err := writer.RecoverPendingOperations(); err != nil || recovered {
					t.Fatalf("completed pending replay was not idempotent: recovered=%t err=%v", recovered, err)
				}
				if !bytes.Equal(stateBefore, mustRead(t, f.statePath)) || !bytes.Equal(journalBefore, mustRead(t, f.journalPath)) {
					t.Fatal("completed pending replay wrote another pair")
				}
			})
		}
	}
}

func TestRotatedLifecyclePendingRejectsUnknownSourcesBeforeAnyCleanup(t *testing.T) {
	for _, change := range []string{"source-missing-segment", "source-missing-active", "unknown-segment", "changed-segment", "changed-active", "changed-archive", "unknown-state", "escaped-name", "changed-size", "duplicate-sequence", "wrong-tail-sequence", "missing-binding", "wrong-approval", "target-unknown-segment", "target-changed-segment", "target-changed-active", "target-symlink"} {
		t.Run(change, func(t *testing.T) {
			f := pendingRotatedReset(t, "rollover")
			first := f.journalPath + ".archive.2.jsonl"
			segments := f.marker["source_journal_segments"].([]any)
			if len(change) >= 7 && change[:7] == "target-" {
				writeJSONMapRuntimeTest(t, f.statePath, f.fresh)
			}
			switch change {
			case "source-missing-segment":
				if err := os.Remove(first); err != nil {
					t.Fatal(err)
				}
			case "source-missing-active":
				if err := os.Remove(f.journalPath); err != nil {
					t.Fatal(err)
				}
			case "unknown-segment", "target-unknown-segment":
				if err := os.WriteFile(f.journalPath+".archive.6.jsonl", mustRead(t, first), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-segment", "target-changed-segment":
				if err := os.WriteFile(first, []byte("unknown source bytes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-active", "target-changed-active":
				if err := os.WriteFile(f.journalPath, []byte("unknown active bytes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-archive":
				if err := os.WriteFile(filepath.Join(f.archiveDir, "loop-events.jsonl"), []byte("unknown archive bytes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-state":
				state := readJSONMapRuntimeTest(t, f.statePath)
				state["runtime_id"] = "loop-unrelated"
				writeJSONMapRuntimeTest(t, f.statePath, state)
			case "escaped-name":
				segments[0].(map[string]any)["name"] = "../must-not-delete"
			case "changed-size":
				segments[0].(map[string]any)["size"] = 1
			case "duplicate-sequence":
				segments[1].(map[string]any)["name"] = filepath.Base(first)
			case "wrong-tail-sequence":
				segments[0].(map[string]any)["name"] = filepath.Base(f.journalPath) + ".archive.1.jsonl"
			case "missing-binding":
				delete(f.marker, "source_journal_segments")
			case "wrong-approval":
				f.marker["approval"].(map[string]any)["approved_by"] = "another-person"
			case "target-symlink":
				outside := filepath.Join(f.root, "must-not-delete")
				if err := os.WriteFile(outside, mustRead(t, first), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(first); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, first); err != nil {
					t.Fatal(err)
				}
			}
			writeJSONMapRuntimeTest(t, f.markerPath, f.marker)
			before := rotationResetFiles(t, f.root)
			if _, err := runtime.NewWriter(f.statePath, f.journalPath, f.root, integrityTestValidator{}).RecoverPendingOperations(); err == nil {
				t.Fatal("unknown or unapproved source was treated as cleanup authority")
			}
			after := rotationResetFiles(t, f.root)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected recovery changed source, archive, marker or target files:\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func rotationResetFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// The original process lock is advisory metadata, not source/target data.
		if filepath.Ext(path) == ".process" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			files[rel] = "symlink:" + target
			return nil
		}
		files[rel] = sha256HexForTest(mustRead(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
