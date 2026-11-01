package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestFindingSupplementOriginalCLIDoesNotRequireDigest(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "running", 1)
	seedCLIFinding(t, root, state)
	row := state["entities"].(map[string]any)["findings"].([]any)[0].(map[string]any)
	delete(row, "sha256")
	fixtures.WriteState(t, root, state)
	observation := filepath.Join(root, "observation.json")
	input := []byte(`{"author":"agent-intake","new_observation":"A genuine follow-up","evidence_refs":["command: checked"],"hash":{"legacy":"ignored"}}`)
	if err := os.WriteFile(observation, input, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"runtime", "finding-supplement", "--root", root, "--finding", "finding-1", "--file", observation, "--in-round-note"}
	first, stderr, code := runCLI(t, root, args...)
	if code != 0 {
		t.Fatalf("original CLI failed: %d %s", code, stderr)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(first), &receipt); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["hash"]; ok {
		t.Fatal("original CLI registered a new machine hash")
	}
	second, stderr, code := runCLI(t, root, args...)
	if code != 0 {
		t.Fatalf("original CLI replay failed: %d %s", code, stderr)
	}
	var replay map[string]any
	if err := json.Unmarshal([]byte(second), &replay); err != nil {
		t.Fatal(err)
	}
	if replay["supplement_id"] != receipt["supplement_id"] || replay["revision"] != receipt["revision"] {
		t.Fatalf("original observation was not reused: %v then %v", receipt, replay)
	}
}
