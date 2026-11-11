package qualitygate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

type s2ProductView struct {
	root         string
	readEvidence bool
}

func (v *s2ProductView) ReadFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, ".claude/") {
		v.readEvidence = true
		return nil, fmt.Errorf("control report is not a product file")
	}
	return fileview.Disk{Root: v.root}.ReadFile(path)
}
func (v *s2ProductView) ReadDir(path string) ([]os.DirEntry, error) {
	return fileview.Disk{Root: v.root}.ReadDir(path)
}

func TestS2DesignGateReadsRegisteredReportFromControlRoot(t *testing.T) {
	root := t.TempDir()
	report := ".claude/evidence-alt-1/submissions/current/answer.json"
	architecture := "docs/architecture/ARCHITECTURE-001.md"
	for _, path := range []string{report, architecture} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, architecture), []byte("# Architecture\n> Status: locked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	envelope, _ := json.Marshal(map[string]any{"runtime_id": "loop-s2", "baseline_generation": 1, "conclusion": "pass", "subject_refs": []any{map[string]any{"path": architecture}}})
	if err := os.WriteFile(filepath.Join(root, report), envelope, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"schema_version": "1.2.0", "runtime_id": "loop-s2", "configuration": map[string]any{"optimization": map[string]any{"packet": true}}, "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "design", "phase_revision": 3}, "bound_req": map[string]any{"status": "locked", "path": "docs/requirements/REQ-001.md"}, "entities": map[string]any{"submissions": []any{map[string]any{"evidence_id": "ev-original", "origin_state": "planning", "origin_phase": "design", "origin_entry": 3}}}, "evidence": []any{map[string]any{"id": "ev-original", "kind": "planning_design", "status": "valid", "baseline_generation": 1, "path": report}}}
	view := &s2ProductView{root: root}
	result := evaluatePlanningDesign(Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: view}, Evaluation{}, GateSpec{})
	if result.Status != StatusSatisfied || len(result.EvidenceRefs) != 1 || result.EvidenceRefs[0] != "ev-original" || view.readEvidence {
		t.Fatalf("original registered control report lost: %+v read via product view=%v", result, view.readEvidence)
	}
	if err := os.Remove(filepath.Join(root, architecture)); err != nil {
		t.Fatal(err)
	}
	result = evaluatePlanningDesign(Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: view}, Evaluation{}, GateSpec{})
	if result.Status != StatusNotReady {
		t.Fatalf("missing actual architecture was treated as ready: %+v", result)
	}
}
