package qualitygate

import (
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

type readOnlyS5Files map[string][]byte

func (files readOnlyS5Files) ReadFile(path string) ([]byte, error) {
	data, ok := files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return append([]byte(nil), data...), nil
}

func TestLegacyPacketS5KeepsReadOnlyFileViewCompatibility(t *testing.T) {
	path := "docs/requirements/REQ-001.md"
	data := []byte("# Requirement\n")
	state := map[string]any{
		"schema_version": "1.2.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"lifecycle":      map[string]any{"state": "document_verification", "phase_revision": float64(3)},
		"baseline":       map[string]any{"generation": float64(1)},
		"documents": []any{map[string]any{
			"id": "REQ-001", "kind": "req", "path": path, "version": "1",
			"sha256": sha256Hex(data), "status": "locked", "generation": float64(1),
		}},
		"entities": map[string]any{"work_packets": []any{}},
	}
	input := Input{
		Root:     ".",
		Snapshot: runtime.Snapshot{Revision: 4, State: state},
		Files:    readOnlyS5Files{path: data},
	}
	documents, err := verifiedCurrentDocumentsWithS5Facts(input)
	if err != nil {
		t.Fatalf("legacy S5 projection rejected a read-only FileView: %v", err)
	}
	if len(documents) != 1 || documents[0].Path != path {
		t.Fatalf("legacy S5 projection = %+v, want the verified registered REQ", documents)
	}
}
