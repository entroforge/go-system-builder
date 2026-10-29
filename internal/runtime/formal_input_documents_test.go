package runtime

import (
	"testing"
)

func TestCurrentFormalInputDocumentsOutsideS5UsesOnlyCurrentRegisteredLockedDocs(t *testing.T) {
	state := map[string]any{
		"baseline":  map[string]any{"generation": 4},
		"lifecycle": map[string]any{"state": "building"},
		"documents": []any{
			map[string]any{"id": "REQ-001", "kind": "req", "path": "docs/REQ.md", "version": "v1", "sha256": "req-sha", "status": "locked", "generation": 4},
			map[string]any{"id": "TASK-001", "kind": "task", "path": "docs/TASK-001.md", "version": "v1", "sha256": "task-sha", "status": "registered", "generation": 4},
			map[string]any{"id": "TASK-002", "kind": "task", "path": "docs/TASK-002.md", "version": "v1", "sha256": "complete-sha", "status": "complete", "generation": 4},
			map[string]any{"id": "TASK-OLD", "kind": "task", "path": "docs/TASK-OLD.md", "version": "v1", "sha256": "old-sha", "status": "locked", "generation": 3},
		},
	}

	documents, err := CurrentFormalInputDocuments(t.TempDir(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 {
		t.Fatalf("formal documents = %+v, want current locked/registered only", documents)
	}
	if documents[0].Reference.Path != "docs/REQ.md" || documents[0].Kind != "req" || documents[1].Reference.Path != "docs/TASK-001.md" || documents[1].Kind != "task" {
		t.Fatalf("formal input projection = %+v", documents)
	}
}
