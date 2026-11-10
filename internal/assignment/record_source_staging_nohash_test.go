package assignment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestRecordSourceStagingUsesAlternateRootsWithoutAdoptingOccupants(t *testing.T) {
	for _, kind := range []string{"file", "internal-symlink", "external-symlink", "candidate-file", "candidate-directory", "candidate-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			path := ".claude/evidence/runtime/g1/assignments/a/records/r1/sources/source-1.bin"
			occupied := filepath.Join(root, ".claude/evidence")
			var target string
			if kind == "internal-symlink" || kind == "external-symlink" {
				target = filepath.Join(root, "unknown-target")
				if kind == "external-symlink" {
					target = t.TempDir()
				}
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "unknown.txt"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, occupied); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(occupied, []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(kind, "candidate-") {
				candidate := filepath.Join(root, ".claude/evidence-alt-1")
				switch kind {
				case "candidate-file":
					if err := os.WriteFile(candidate, []byte("source"), 0600); err != nil {
						t.Fatal(err)
					}
				case "candidate-directory":
					candidate = filepath.Join(candidate, strings.TrimPrefix(path, ".claude/evidence/"))
					if err := os.MkdirAll(candidate, 0700); err != nil {
						t.Fatal(err)
					}
				case "candidate-symlink":
					if err := os.Symlink("unknown", candidate); err != nil {
						t.Fatal(err)
					}
				}
				assertSourceStagingPreservesEntry(t, root, path, candidate)
			}
			assertSourceStagingPreservesEntry(t, root, path, occupied)
			if target != "" {
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 || entries[0].Name() != "unknown.txt" {
					t.Fatalf("symlink target changed: %v %v", entries, err)
				}
			}
		})
	}
}

func assertSourceStagingPreservesEntry(t *testing.T, root, path, occupied string) {
	t.Helper()
	before, err := os.Lstat(occupied)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{SourceSnapshots: []assignmentrecord.SourceSnapshot{{SourceRef: "docs/task.md", SourceKind: "task", Path: path}}}
	var staged []loopruntime.ArtifactCleanupRequest
	got, err := publishRegisteredRecordSources(root, map[string]any{"revision": 4}, record, map[string][]byte{path: []byte("source")}, &staged, workpacket.PublishNewBounded)
	if err != nil {
		t.Fatal(err)
	}
	actual := got.SourceSnapshots[0].Path
	if !strings.HasPrefix(actual, ".claude/evidence-alt-") || len(staged) != 1 || staged[0].ArtifactPath != actual || staged[0].ExpectedRevision != 4 {
		t.Fatalf("wrong actual registration: record=%+v staging=%+v", got, staged)
	}
	if record.SourceSnapshots[0].Path != path {
		t.Fatal("modified caller's original snapshot")
	}
	data, err := os.ReadFile(filepath.Join(root, actual))
	if err != nil || string(data) != "source" {
		t.Fatalf("actual snapshot: %q %v", data, err)
	}
	after, err := os.Lstat(occupied)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("occupied object changed: %v", err)
	}
	if before.Mode().IsRegular() {
		data, err := os.ReadFile(occupied)
		if err != nil || string(data) != "source" {
			t.Fatalf("unknown bytes changed: %q %v", data, err)
		}
	}
}

func TestRecordSourceStagingRejectsBlockedCommonParent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".claude"), []byte("unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	var staged []loopruntime.ArtifactCleanupRequest
	_, err := publishRegisteredRecordSources(root, map[string]any{"revision": 0}, Record{}, map[string][]byte{".claude/evidence/source.bin": []byte("source")}, &staged, workpacket.PublishNewBounded)
	if err == nil || len(staged) != 0 {
		t.Fatalf("blocked common parent accepted: %v %+v", err, staged)
	}
}

func TestRegisterS6SourceSnapshotsWithBlockedEvidenceRoot(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: "src"})
			narrowRegistrationFixtureScope(t, fixture)
			state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
			writeRegistrationJSON(t, statePath, state)
			occupied := filepath.Join(fixture.root, ".claude/evidence")
			target := t.TempDir()
			if kind == "file" {
				if err := os.WriteFile(occupied, []byte("unknown"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(target, occupied); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(occupied)
			if err != nil {
				t.Fatal(err)
			}
			next, err := Register(fixture.root, statePath, journalPath, Request{ExpectedRevision: 8, ManifestPath: filepath.Join(fixture.root, fixture.manifestRef), TaskID: fixture.taskID, TaskPath: filepath.Join(fixture.root, fixture.taskRef), OccurredAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			records, err := assignmentrecord.RecordsFromState(next.State)
			if err != nil || len(records) != 1 {
				t.Fatalf("records: %+v %v", records, err)
			}
			if records[0].Task.Ref != fixture.taskRef || records[0].Manifest.Ref != fixture.manifestRef {
				t.Fatalf("original authority changed: %+v", records[0])
			}
			for _, snapshot := range records[0].SourceSnapshots {
				if !strings.HasPrefix(snapshot.Path, ".claude/evidence-alt-") {
					t.Fatalf("fixed evidence root still registered: %+v", snapshot)
				}
				if _, err := os.ReadFile(filepath.Join(fixture.root, snapshot.Path)); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("unknown target changed: %v %v", entries, err)
			}
			after, err := os.Lstat(occupied)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("root occupant changed: %v", err)
			}
			if kind == "file" {
				body, err := os.ReadFile(occupied)
				if err != nil || string(body) != "unknown" {
					t.Fatalf("root occupant bytes changed: %q %v", body, err)
				}
			}
		})
	}
}

func TestRegisterS6RevisionSourceSnapshotsPreservePriorPathsAndOccupiedCandidates(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginFixture(t, true)
	setRevisionManifest(t, fixture, "team-manifest-s6-registration-r2", "workgroup-s6-registration-r2", "platform-s6-r2", "agent-s6-registration-r2")
	prior := map[string][]byte{}
	for _, snapshot := range previous.SourceSnapshots {
		body, err := os.ReadFile(filepath.Join(fixture.root, snapshot.Path))
		if err != nil {
			t.Fatal(err)
		}
		prior[snapshot.Path] = body
	}
	// Preserve the old registered evidence root and obstruct only the new
	// revision's preferred and legacy candidate parents.
	for _, path := range []string{
		".claude/evidence/loop-s6-registration/g1/assignments/assignment-s6-registration/records/r2",
		".claude/evidence/assignment-source-candidate-1",
	} {
		full := filepath.Join(fixture.root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("unknown"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	issued, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 9,
		ManifestPath:     filepath.Join(fixture.root, fixture.manifestRef),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, fixture.taskRef),
		Revision:         &RevisionRequest{AssignmentID: previous.AssignmentID, ExpectedRecord: previous.Reference(), IntentKey: "source-alternate-root"},
		OccurredAt:       time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := assignmentrecord.RecordsFromState(issued.State)
	if err != nil {
		t.Fatal(err)
	}
	current, found, err := assignmentrecord.FindRecord(records, previous.RuntimeID, previous.BaselineGeneration, previous.AssignmentID)
	if err != nil || !found || current.Revision != 2 {
		t.Fatalf("revision missing: %+v %v %v", current, found, err)
	}
	for _, snapshot := range current.SourceSnapshots {
		if !strings.HasPrefix(snapshot.Path, ".claude/evidence-alt-") {
			t.Fatalf("new revision did not record actual path: %+v", snapshot)
		}
		if _, err := os.ReadFile(filepath.Join(fixture.root, snapshot.Path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, record := range records {
		if record.Revision != 1 {
			continue
		}
		if !reflect.DeepEqual(record.SourceSnapshots, previous.SourceSnapshots) {
			t.Fatalf("historical snapshot paths changed: %+v", record.SourceSnapshots)
		}
		for _, snapshot := range record.SourceSnapshots {
			body, err := os.ReadFile(filepath.Join(fixture.root, snapshot.Path))
			if err != nil || !bytes.Equal(body, prior[snapshot.Path]) {
				t.Fatalf("historical snapshot changed: %+v %v", snapshot, err)
			}
		}
	}
}
