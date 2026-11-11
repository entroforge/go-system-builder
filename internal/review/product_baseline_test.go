package review

import (
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

func TestFrozenSubjectsForProductBaselineIncludesTasksAndDeletedPaths(t *testing.T) {
	snapshot := &buildsurface.Snapshot{
		Tasks: []buildsurface.Task{
			{ID: "TASK-B", Path: "tasks/B.md"},
			{ID: "TASK-A", Path: "tasks/A.md"},
		},
		Paths: []buildsurface.Path{
			{Path: "internal/add.go", Status: "added"},
			{Path: "internal/remove.go", Status: "deleted"},
			{Path: "internal/change.go", Status: "modified"},
		},
	}

	got, err := frozenSubjectsForProductBaseline(snapshot)
	if err != nil {
		t.Fatalf("frozenSubjectsForProductBaseline: %v", err)
	}
	want := []FrozenSubject{
		{Path: "internal/add.go", Kind: "changed_surface"},
		{Path: "internal/change.go", Kind: "changed_surface"},
		{Path: "internal/remove.go", Kind: "deleted"},
		{Path: "tasks/A.md", Kind: "task"},
		{Path: "tasks/B.md", Kind: "task"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frozen subjects = %#v, want %#v", got, want)
	}
}

func TestFrozenSubjectsForProductBaselineRejectsDeletedLockedTask(t *testing.T) {
	_, err := frozenSubjectsForProductBaseline(&buildsurface.Snapshot{
		Tasks: []buildsurface.Task{{ID: "TASK-A", Path: "tasks/A.md"}},
		Paths: []buildsurface.Path{{Path: "tasks/A.md", Status: "deleted"}},
	})
	if err == nil {
		t.Fatal("deleted locked TASK source was accepted")
	}
}
