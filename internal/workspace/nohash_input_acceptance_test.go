package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionInputsUseOriginalLocationsWithoutHistoricalDigests(t *testing.T) {
	registry, execution, _, _ := workerFixture(t)
	input := filepath.Join(registry.MainRoot, "input.md")
	if err := os.WriteFile(input, []byte("current professional input"), 0600); err != nil {
		t.Fatal(err)
	}
	execution.Inputs["input.md"] = "historical-digest-is-not-an-eligibility-check"
	if err := registry.ValidateInputs(execution); err != nil {
		t.Fatalf("actual input was rejected using historical digest: %v", err)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateInputs(execution); err == nil {
		t.Fatal("missing actual input should remain visible")
	}
}
