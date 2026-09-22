package dispatch

import (
	"github.com/entroforge/go-system-builder/internal/semantic"
	"strings"
	"testing"
)

func TestAdvisoryNamesEligibleAndUnintegratedWork(t *testing.T) {
	b := Board{Plan: &semantic.DispatchPlan{Path: "docs/dev/tasks/index-REQ-042.md"}, Rows: []semantic.DispatchRow{
		{Task: semantic.DispatchTask{ID: "TASK-042-01"}, State: "queued"},
		{Task: semantic.DispatchTask{ID: "TASK-042-02"}, State: "reported"},
		{Task: semantic.DispatchTask{ID: "TASK-042-03"}, State: "waiting", Reason: "await verified integration: TASK-042-02"},
	}}
	got := strings.Join(Advisory(b), "\n")
	for _, want := range []string{"eligible tasks", "TASK-042-01", "awaiting verified integration", "TASK-042-02", "waiting:", "actual available capacity"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if len(b.Next) != 0 {
		t.Fatal("advisory selected a batch without declared capacity")
	}
}
func TestAdvisoryBoundsLongBacklogs(t *testing.T) {
	root, state := boardFixture(t)
	b, err := Load(root, state, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(Advisory(b), "\n"); !strings.Contains(got, "TASK-042-01") {
		t.Fatal(got)
	}
	if got := boundedItems([]string{"1", "2", "3", "4", "5", "6"}); got != "1; 2; 3; 4; +2 more (s6 status)" {
		t.Fatal(got)
	}
}
