package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalResultCannotBecomeEmptyS7Baseline(t *testing.T) {
	for _, body := range []string{
		`{"kind":"completion_report","assignment_record":{"assignment_revision":2}}`,
		`{"kind":"completion_report","assignment_record":null,"changed_paths":[]}`,
		`{"kind":"completion_report","assignment_record":{},"changed_paths":["src/claimed.go"]}`,
	} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			data := []byte(body)
			if err := os.WriteFile(filepath.Join(root, "result.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			state := baseDraftState(t)
			state["evidence"] = []any{map[string]any{
				"id": "canonical-result", "kind": "completion_report",
				"path": "result.json", "sha256": sha256Of(data),
				"baseline_generation": 1, "status": "valid",
				"scope_refs": []any{"src/claimed.go"},
			}}
			paths, diagnostics := ChangedPathsForRootDetailed(root, state)
			if paths != nil || len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "original Main facts") {
				t.Fatalf("canonical Result must fail closed, got paths=%v diagnostics=%v", paths, diagnostics)
			}
			if err := validateS7BaselineProjection(root, state); err == nil {
				t.Fatal("canonical Result must block original ReviewPlan baseline validation")
			}
			if err := validateCoverageInventory(root, state, &Plan{}); err == nil || !strings.Contains(err.Error(), "S7_BASELINE_PROJECTION") {
				t.Fatalf("shared coverage projection must preserve source failure instead of accepting an empty denominator: %v", err)
			}
		})
	}
}

func TestLegacyVerifiedEmptyS7SurfaceStillSupported(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"kind":"completion_report","changed_paths":[]}`)
	if err := os.WriteFile(filepath.Join(root, "completion.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := completionChangedPaths(root, map[string]any{"path": "completion.json", "sha256": sha256Of(data)})
	if err != nil || len(paths) != 0 {
		t.Fatalf("legacy empty surface changed: paths=%v err=%v", paths, err)
	}
}
