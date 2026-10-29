package cli

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestS6BuilderResultPathsStayMappedToTheirRegisteredTask(t *testing.T) {
	root := t.TempDir()
	questions := []s6BuilderQuestionnaire{
		{TaskID: "TASK-A", AssignmentID: "assignment-a", ResultPath: filepath.Join(root, ".claude", "evidence", "a", "completion.json")},
		{TaskID: "TASK-B", AssignmentID: "assignment-b1", ResultPath: filepath.Join(root, ".claude", "evidence", "b1", "completion.json")},
		{TaskID: "TASK-B", AssignmentID: "assignment-b2", ResultPath: filepath.Join(root, ".claude", "evidence", "b2", "completion.json")},
		{TaskID: "TASK-B", AssignmentID: "assignment-b2-replay", ResultPath: filepath.Join(root, ".claude", "evidence", "b2", "completion.json")},
		{AssignmentID: "unmapped", ResultPath: filepath.Join(root, "unknown.json")},
	}
	got := s6BuilderResultPathsByTask(questions)
	want := map[string][]string{
		"TASK-A": {filepath.Join(root, ".claude", "evidence", "a", "completion.json")},
		"TASK-B": {
			filepath.Join(root, ".claude", "evidence", "b1", "completion.json"),
			filepath.Join(root, ".claude", "evidence", "b2", "completion.json"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Builder Result paths by TASK = %#v, want %#v", got, want)
	}
}
