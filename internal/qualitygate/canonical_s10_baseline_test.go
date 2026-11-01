package qualitygate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
)

func TestS10DiagnosticBaselineDoesNotRequireInventedMainProjection(t *testing.T) {
	root := t.TempDir()
	manifest := validS10Manifest(t, "acceptance")
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path": "s10/acceptance-manifest.json", "audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Root = root
	files := input.Files.(memoryFiles)
	files["s10/acceptance-manifest.json"] = manifest
	envelope := []byte(`{"kind":"completion_report","assignment_record":{"assignment_revision":2}}`)
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evidence/canonical-result.json"), envelope, 0600); err != nil {
		t.Fatal(err)
	}
	files["evidence/canonical-result.json"] = envelope
	input.Snapshot.State["evidence"] = append(input.Snapshot.State["evidence"].([]any), map[string]any{
		"id": "ev-canonical-result", "kind": "completion_report", "path": "evidence/canonical-result.json",
		"status": "valid", "baseline_generation": 1,
		"produced_by": []any{"builder-1"}, "invalidated_by": nil,
		"responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{},
	})
	if baseline, err := qualitygate.S10ExternalBaseline(root, input.Snapshot.State, nil); err != nil || len(baseline.ChangedPaths) != 0 {
		t.Fatalf("unknown Main facts invented changed paths or blocked observation: %+v %v", baseline, err)
	}
}
