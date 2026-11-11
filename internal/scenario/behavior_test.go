package scenario_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/scenario"
)

type changingBehaviorReader struct {
	fileview.Reader
	reads int
}

func TestBehaviorSourceRetainsStandardJSONComposition(t *testing.T) {
	var outer struct {
		scenario.BehaviorSource
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"path":"docs/source","extra":"kept"}`), &outer); err != nil || outer.Path != "docs/source" || outer.Extra != "kept" {
		t.Fatalf("outer source lost: %+v, %v", outer, err)
	}
}

func (r *changingBehaviorReader) ReadFile(path string) ([]byte, error) {
	data, err := r.Reader.ReadFile(path)
	if filepath.Base(path) == "scenario-model.json" {
		r.reads++
		if r.reads > 1 {
			data = append(data, '\n')
		}
	}
	return data, err
}

func TestBehaviorProjectionReadsOriginalSourceOnce(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	reader := &changingBehaviorReader{Reader: fileview.Disk{Root: root}}
	examples, err := scenario.ProjectBehaviorWithFiles(root, "investor-workbench", reader)
	if err != nil || len(examples) == 0 {
		t.Fatalf("projection did not use the readable source and cases: %v", err)
	}
	if reader.reads != 1 {
		t.Fatalf("source read %d times; want once", reader.reads)
	}
}
