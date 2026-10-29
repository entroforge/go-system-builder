package assignment

import (
	"bytes"
	"fmt"
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
	return publishRegisteredRecordSources(root, state, captured, artifacts, staged, workpacket.PublishBounded)
}

func publishRegisteredRecordSources(root string, state map[string]any, captured Record, artifacts map[string][]byte, staged *[]loopruntime.ArtifactCleanupRequest, publish func(string, string, []byte, int64) ([]byte, error)) (Record, error) {
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
	for _, path := range paths {
		body := artifacts[path]
		// Publish may have linked final bytes before a sync/readback error.
		// Track intent first; original cleanup validates actual SHA/references.
		*staged = append(*staged, loopruntime.ArtifactCleanupRequest{ExpectedRevision: revision, ArtifactPath: path, ArtifactSHA256: sha256Of(body), ReferencedPaths: referenced})
		published, err := publish(root, path, body, 16<<20)
		if err != nil {
			return record, fmt.Errorf("publish Record source snapshot: %w", err)
		}
		if !bytes.Equal(body, published) {
			return record, fmt.Errorf("Record source snapshot conflicts with original immutable bytes")
		}
	}
	return captured, nil
}
