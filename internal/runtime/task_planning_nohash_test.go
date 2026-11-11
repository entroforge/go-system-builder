package runtime_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestTaskPlanningUsesAcceptedS3PathsWithoutHashOrCheckoutClosure(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	bound := state["bound_req"].(map[string]any)
	bound["sha256"] = "historical-requirement-digest"
	for _, raw := range state["documents"].([]any) {
		raw.(map[string]any)["sha256"] = "historical-document-digest"
	}
	// A sibling outside the accepted S3 responsibility is not a new S4
	// reception qualification.
	writeTaskPlanningFile(t, root, "docs/dev/contracts/FE-EXTRA.md", "unrelated draft")
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs.Contracts) != 2 {
		t.Fatalf("original S3 paths not projected: %+v", inputs.Contracts)
	}
}
