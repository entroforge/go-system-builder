package assignment

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// stageRegisteredRecordSources runs only inside the original Register Apply.
// Source snapshots preserve audit bytes; current authority still verifies its
// original file view. The caller retains these artifacts through Store pending
// recovery and removes only confirmed unreferenced staging on rejection.
func stageRegisteredRecordSources(root string, state map[string]any, record Record, staged *[]loopruntime.ArtifactCleanupRequest) (Record, error) {
	captured, artifacts, err := assignmentrecord.CaptureSourceSnapshots(record, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(root, ref)
	})
	if err != nil {
		return record, fmt.Errorf("capture Record authority source snapshots: %w", err)
	}
	return publishRegisteredRecordSources(root, state, captured, artifacts, staged, workpacket.PublishNewBounded)
}

func publishRegisteredRecordSources(root string, state map[string]any, captured Record, artifacts map[string][]byte, staged *[]loopruntime.ArtifactCleanupRequest, publish func(string, string, []byte, int64) (bool, error)) (Record, error) {
	record := captured
	revision, err := commitRevision(-1, state)
	if err != nil {
		return record, err
	}
	referenced := stateArtifactPaths(state)
	paths := make([]string, 0, len(artifacts))
	for path := range artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	record.SourceSnapshots = append([]assignmentrecord.SourceSnapshot(nil), captured.SourceSnapshots...)
	for _, path := range paths {
		body := artifacts[path]
		for attempt := 0; ; attempt++ {
			actual := path
			if attempt > 0 {
				actual = filepath.ToSlash(filepath.Join(".claude/evidence", fmt.Sprintf("assignment-source-candidate-%d", attempt), filepath.Base(path)))
			}
			created, err := publish(root, actual, body, 16<<20)
			if err != nil {
				return record, fmt.Errorf("publish Record source snapshot: %w", err)
			}
			if !created {
				if err := workpacket.CheckPath(root, ".claude/evidence/assignment-source-probe/source.bin"); err != nil {
					return record, fmt.Errorf("Assignment source storage unavailable: %w", err)
				}
				continue
			}
			// Ownership comes from exclusive creation, never matching contents.
			// If publication reports an error after linking, retain the orphan.
			*staged = append(*staged, loopruntime.ArtifactCleanupRequest{ExpectedRevision: revision, ArtifactPath: actual, ArtifactBytes: append([]byte(nil), body...), ReferencedPaths: referenced})
			for i := range record.SourceSnapshots {
				if record.SourceSnapshots[i].Path == path {
					record.SourceSnapshots[i].Path = actual
				}
			}
			break
		}
	}
	return record, nil
}
