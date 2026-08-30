package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRecordEvidenceAcceptsRegisteredCatalogKinds(t *testing.T) {
	for _, kind := range []string{"team_manifest", "planning_design", "completion_report"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "loop-state.json")
			journalPath := filepath.Join(root, "loop-events.jsonl")
			writeExampleRuntime(t, statePath)
			artifact := filepath.Join(root, kind+".json")
			if err := os.WriteFile(artifact, []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
				ExpectedRevision: 1,
				ID:               "EV-CATALOG-" + kind,
				Kind:             kind,
				Path:             filepath.Base(artifact),
				ProducedBy:       []string{"catalog-test"},
				Validator:        testCandidateValidator(),
			}); err != nil {
				t.Fatalf("RecordEvidence(%s) rejected registered catalog kind: %v", kind, err)
			}
		})
	}
}
