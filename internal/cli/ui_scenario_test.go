package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS2UIProjectionInventoriesFilesWithoutProfessionalValidation(t *testing.T) {
	root := t.TempDir()
	req := uiInventoryREQ(t, root, "docs/design/prototypes/investor-workbench/")
	if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	// Existing but invalid authoring content must be diagnosed by the explicit
	// scenario consumer, not parsed afresh during every ordinary tool call.
	model := filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json")
	if err := os.WriteFile(model, []byte("unfinished JSON"), 0644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"bound_req": map[string]any{"path": req, "metadata": map[string]any{"ui_impact": "changed"}}}
	projection := contractFor("S2", state, root)
	if strings.Contains(strings.Join(projection.Missing, ","), "ui_design_package") {
		t.Fatalf("passive projection ran professional validation: %v", projection.Missing)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"scenario", "validate", "--module", "investor-workbench", "--root", root}, nil, &out, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "scenario-model.json") {
		t.Fatalf("explicit scenario check lost actual invalid input: %d %s %s", code, &out, &diagnostic)
	}
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	projection = contractFor("S2", state, root)
	if !strings.Contains(strings.Join(projection.Missing, ","), "investor-workbench/scenario-model.json") {
		t.Fatalf("inventory discarded the exact missing input: %v", projection.Missing)
	}
}
