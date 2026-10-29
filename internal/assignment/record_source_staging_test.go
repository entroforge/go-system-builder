package assignment

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRegisterS6SourceSnapshotsPersistOriginalBytesWithoutReplacingAuthority(t *testing.T) {
	f, _, _, record, _ := registerAgentBeginFixture(t, true)
	if len(record.SourceSnapshots) == 0 {
		t.Fatal("Register must freeze source audit bytes at initial issuance")
	}
	var manifestSnapshot string
	for _, snapshot := range record.SourceSnapshots {
		original, err := assignmentinputs.Read(f.root, snapshot.SourceRef)
		if err != nil {
			t.Fatal(err)
		}
		captured, err := workpacket.ReadFile(f.root, snapshot.Path, 16<<20)
		if err != nil || !bytes.Equal(original, captured) || sha256Of(captured) != snapshot.SHA256 || snapshot.SHA256 != snapshot.SourceSHA256 {
			t.Fatalf("Register did not preserve exact source %s: %v", snapshot.SourceRef, err)
		}
		if snapshot.SourceKind == "manifest" && snapshot.SourceRef == record.Manifest.Ref {
			manifestSnapshot = snapshot.Path
		}
	}
	if manifestSnapshot == "" {
		t.Fatal("Record source bundle omits original manifest")
	}
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(record.Manifest.Ref)), []byte("changed live manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) { return assignmentinputs.Read(f.root, ref) }); err == nil {
		t.Fatal("audit snapshot must not substitute for current frozen source authority")
	}
	oldBytes, err := workpacket.ReadFile(f.root, manifestSnapshot, 16<<20)
	if err != nil || sha256Of(oldBytes) != record.Manifest.SHA256 {
		t.Fatalf("history source was lost after live drift: %v", err)
	}
}

func TestRecordSourceStagingTracksPublishedBytesBeforeReadbackFailure(t *testing.T) {
	f, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
	writer := loopruntime.NewWriter(statePath, journalPath, f.root, semantic.RuntimeCandidateValidator{})
	snapshot, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("source bytes linked before a simulated readback error\n")
	rel := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/records/r2/sources/%s.bin", record.RuntimeID, record.BaselineGeneration, record.AssignmentID, sha256Of(body))
	var staged []loopruntime.ArtifactCleanupRequest
	_, err = publishRegisteredRecordSources(f.root, snapshot.State, record, map[string][]byte{rel: body}, &staged, func(root, path string, data []byte, limit int64) ([]byte, error) {
		if _, err := workpacket.PublishBounded(root, path, data, limit); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated failure after final publication")
	})
	if err == nil || len(staged) != 1 || staged[0].ArtifactPath != rel {
		t.Fatalf("post-publication cleanup intent: %v %+v", err, staged)
	}
	if removed, err := writer.RemoveUnreferencedArtifact(staged[0]); err != nil || !removed {
		t.Fatalf("original unreferenced cleanup: %t %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced linked artifact remains: %v", err)
	}
}
