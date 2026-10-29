package runtime_test

import (
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"testing"
)

func TestTaskPlanningReceptionRetainsIssuedMethodsAndValidatesFormalSources(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	writeTaskPlanningFile(t, root, "docs/rules/dispatch-plan.md", "updated professional method text\n")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/TASK-template.md", "updated task template guidance\n")
	if err := runtime.ValidateTaskPlanningReceptionSources(root, state, p, files); err != nil {
		t.Fatalf("method text became a new receipt gate: %v", err)
	}
	writeTaskPlanningFile(t, root, "docs/architecture/ARCHITECTURE-001.md", "> Status: locked\n> Version: v1.0.0\nchanged formal source\n")
	if err := runtime.ValidateTaskPlanningReceptionSources(root, state, p, files); err == nil {
		t.Fatal("formal design drift accepted")
	}
}
