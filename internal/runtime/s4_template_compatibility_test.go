package runtime_test

import (
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"testing"
)

func TestS4FrozenOldTemplateStillQualifiesOriginalInputs(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := schema.ReadAsset("stage-form-s4-template-before-guidance.json")
	if err != nil {
		t.Fatal(err)
	}
	old, err := workpacket.BuildTaskPlanningWithTemplate(binding, inputs, workpacket.Digest(oldBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateTaskPlanningPacket(root, state, old, files); err != nil {
		t.Fatalf("original issuance rejected: %v", err)
	}
	old.Draft.Instruction = "replacement prompt"
	if err := runtime.ValidateTaskPlanningPacket(root, state, old, files); err == nil {
		t.Fatal("rewritten issued template accepted")
	}
}
