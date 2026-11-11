package submission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestFrozenTaskAnalysisDoesNotCopyHistoricalInputDigest(t *testing.T) {
	root := t.TempDir()
	rel := "docs/dev/tasks/TASK-001.md"
	absolute := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("> Source REQ refs: REQ-001\n> Status: complete\n\n# TASK-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Root: root, Files: fileview.Disk{Root: root}}
	p := workpacket.Packet{ID: "packet-s5-no-digest", Inputs: []workpacket.Reference{{ID: "TASK-001", Path: rel, Version: "v1"}}}
	issued, err := svc.freezeTaskAnalysis(p, "REQ-001")
	if err != nil {
		t.Fatalf("prepare original analysis: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(issued.TaskAnalysis.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Inputs []map[string]any `json:"inputs"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Inputs) != 1 || artifact.Inputs[0]["path"] != rel || artifact.Inputs[0]["version"] != "v1" {
		t.Fatalf("lost original input coordinates: %v", artifact.Inputs)
	}
	if _, exists := artifact.Inputs[0]["sha256"]; exists || strings.Contains(string(data), "historical-not-a-proof") {
		t.Fatalf("new analysis copied a historical digest: %s", data)
	}
}

func TestTaskAnalysisExcludesFrozenTaskTemplate(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "docs/dev/tasks")
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TASK-001.md", "TASK-002.md", "TASK-template.md"} {
		body := "> Source REQ refs: REQ-001\n> Status: complete\n\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(taskDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inputs := []workpacket.Reference{
		{ID: "TASK-001", Path: "docs/dev/tasks/TASK-001.md"},
		{ID: "TASK-002", Path: "docs/dev/tasks/TASK-002.md"},
		{ID: "TASK-template", Path: "docs/dev/tasks/TASK-template.md"},
	}

	paths := taskAnalysisPaths(inputs)
	wantPaths := []string{"docs/dev/tasks/TASK-001.md", "docs/dev/tasks/TASK-002.md"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("task analysis paths = %v, want %v", paths, wantPaths)
	}
	tasks, _, err := semantic.AnalyzeApprovedTaskContexts(root, fileview.Disk{Root: root}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].TaskID != "TASK-001" || tasks[1].TaskID != "TASK-002" {
		t.Fatalf("analyzed task set = %+v, want only TASK-001 and TASK-002", tasks)
	}
}

func TestTaskAnalysisStructuralCheckProjectionUsesBoundREQ(t *testing.T) {
	fixture := "../../docs/examples/dispatch-plan/project"
	root := t.TempDir()
	err := filepath.WalkDir(fixture, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignTask := filepath.Join(root, "docs/dev/tasks/TASK-999.md")
	if err := os.WriteFile(foreignTask, []byte("> Source REQ refs: REQ-999\n> Status: draft\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := fileview.Disk{Root: root}
	result, err := semantic.TasksCheckWithFiles(root, files, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	got := structuralCheckProjection(result)
	if got.Tasks != 6 || got.Cancelled != result.Cancelled || got.ClausesTotal != result.ClausesTotal || got.ClausesCovered != result.ClausesCovered || !reflect.DeepEqual(got.Problems, result.Problems) {
		t.Fatalf("structural projection = %+v, checker result = %+v", got, result)
	}
	for _, problem := range got.Problems {
		if strings.Contains(problem, "TASK-999") {
			t.Fatalf("foreign REQ task leaked into frozen structural check: %v", got)
		}
	}
}
