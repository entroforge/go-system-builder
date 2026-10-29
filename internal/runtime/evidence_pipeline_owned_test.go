package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRecordEvidenceRejectsPipelineOwnedRepairKindsBeforeArtifactRead(t *testing.T) {
	for _, kind := range []string{"repair_result_blocker", "repair_result_resolution"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "loop-state.json")
			journalPath := filepath.Join(root, "loop-events.jsonl")
			writeExampleRuntime(t, statePath)
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(root, "must-not-be-opened.json")

			_, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
				ExpectedRevision: 1,
				ID:               "evt-evidence-owned-test",
				Kind:             kind,
				Path:             artifact,
				ProducedBy:       []string{"agent-s9-test"},
				ResponsibilityID: "BUILD-WORK-PACKAGE",
			})
			if err == nil || !strings.Contains(err.Error(), "pipeline-owned") || !strings.Contains(err.Error(), kind) {
				t.Fatalf("manual RecordEvidence(%s) error = %v, want named pipeline-owned refusal before path/state handling", kind, err)
			}

			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
				t.Fatal("pipeline-owned registration refusal changed Runtime state or journal")
			}
			if _, err := os.Stat(artifact); !os.IsNotExist(err) {
				t.Fatalf("manual registration read/created the absent artifact path: stat err=%v", err)
			}

			// A missing Runtime pair is a stronger ordering probe: the named
			// pipeline-owned refusal must be returned before Snapshot attempts
			// to open either state or journal.
			missingState := filepath.Join(root, "absent-state.json")
			missingJournal := filepath.Join(root, "absent-journal.jsonl")
			_, err = runtime.RecordEvidence(root, missingState, missingJournal, runtime.EvidenceRequest{
				ExpectedRevision: 1,
				ID:               "evt-evidence-owned-no-state",
				Kind:             kind,
				Path:             artifact,
				ProducedBy:       []string{"agent-s9-test"},
			})
			if err == nil || !strings.Contains(err.Error(), "pipeline-owned") || !strings.Contains(err.Error(), kind) {
				t.Fatalf("manual RecordEvidence(%s) with absent Runtime pair error = %v, want early named refusal", kind, err)
			}
			for _, absent := range []string{missingState, missingJournal} {
				if _, err := os.Stat(absent); !os.IsNotExist(err) {
					t.Fatalf("pipeline-owned refusal created Runtime file %s: %v", absent, err)
				}
			}
		})
	}
}
