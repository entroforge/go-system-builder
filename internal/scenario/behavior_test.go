package scenario_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/scenario"
)

type changingBehaviorReader struct {
	fileview.Reader
	reads int
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

func TestBehaviorProjectionRejectsSourceChangeDuringRead(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	reader := &changingBehaviorReader{Reader: fileview.Disk{Root: root}}
	if _, err := scenario.ProjectBehaviorWithFiles(root, "investor-workbench", reader); err == nil || !strings.Contains(err.Error(), "changed during behavior projection") {
		t.Fatalf("source change mixed old examples and new hashes: %v", err)
	}
}
