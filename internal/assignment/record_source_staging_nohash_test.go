package assignment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRecordSourceStagingPreservesUnknownFilesAndRegistersCreatedPath(t *testing.T) {
	root := t.TempDir()
	path := ".claude/evidence/source-1.bin"
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	// Identical contents do not make an unregistered file ours.
	if err := os.WriteFile(full, []byte("original source"), 0600); err != nil {
		t.Fatal(err)
	}
	record := Record{SourceSnapshots: []assignmentrecord.SourceSnapshot{{SourceRef: "docs/task.md", SourceKind: "task", Path: path}}}
	var staged []loopruntime.ArtifactCleanupRequest
	got, err := publishRegisteredRecordSources(root, map[string]any{"revision": 0}, record, map[string][]byte{path: []byte("original source")}, &staged, workpacket.PublishNewBounded)
	if err != nil {
		t.Fatal(err)
	}
	actual := got.SourceSnapshots[0].Path
	if actual == path || len(staged) != 1 || staged[0].ArtifactPath != actual {
		t.Fatalf("wrong ownership: record=%+v staged=%+v", got, staged)
	}
	if _, err := os.Stat(filepath.Join(root, actual)); err != nil {
		t.Fatalf("created source missing: %v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil || string(data) != "original source" {
		t.Fatalf("unknown file changed: %v", err)
	}
}

func TestRecordSourceStagingDoesNotClaimFailedPublication(t *testing.T) {
	root := t.TempDir()
	var staged []loopruntime.ArtifactCleanupRequest
	_, err := publishRegisteredRecordSources(root, map[string]any{"revision": 0}, Record{}, map[string][]byte{".claude/unknown": []byte("source")}, &staged, func(string, string, []byte, int64) (bool, error) { return false, os.ErrPermission })
	if err == nil || len(staged) != 0 {
		t.Fatalf("failed publication produced ownership: %v %+v", err, staged)
	}
}

func TestRecordSourceStagingPreservesOccupiedKinds(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "parent-file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := ".claude/evidence/source/source.bin"
			occupied := filepath.Join(root, path)
			if kind == "parent-file" {
				occupied = filepath.Dir(occupied)
			}
			if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file", "parent-file":
				if err := os.WriteFile(occupied, []byte("source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(occupied, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("unrelated", occupied); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(occupied)
			if err != nil {
				t.Fatal(err)
			}
			var staged []loopruntime.ArtifactCleanupRequest
			record := Record{SourceSnapshots: []assignmentrecord.SourceSnapshot{{SourceRef: "docs/task.md", SourceKind: "task", Path: path}}}
			got, err := publishRegisteredRecordSources(root, map[string]any{"revision": 0}, record, map[string][]byte{path: []byte("source")}, &staged, workpacket.PublishNewBounded)
			if err != nil {
				t.Fatal(err)
			}
			if got.SourceSnapshots[0].Path == path {
				t.Fatal("claimed occupied path")
			}
			after, err := os.Lstat(occupied)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("occupied entry changed: %v", err)
			}
		})
	}
}

func TestRegisterS6PreservesOccupiedSourceAndRecordsCreatedPath(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: "src"})
	narrowRegistrationFixtureScope(t, fixture)
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	writeRegistrationJSON(t, statePath, state)
	occupied := filepath.Join(fixture.root, ".claude/evidence/loop-s6-registration/g1/assignments/assignment-s6-registration/records/r1/sources/source-1.bin")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("unknown owner"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := Register(fixture.root, statePath, journalPath, Request{ExpectedRevision: 8, ManifestPath: filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)), TaskID: fixture.taskID, TaskPath: filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)), OccurredAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	rows := next.State["entities"].(map[string]any)["assignments"].([]any)
	raw, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, snap := range record.SourceSnapshots {
		if snap.Path == ".claude/evidence/assignment-source-candidate-1/source-1.bin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("actual created path absent from record: %+v", record.SourceSnapshots)
	}
	data, err := os.ReadFile(occupied)
	if err != nil || string(data) != "unknown owner" {
		t.Fatalf("unknown source changed: %v", err)
	}
}
