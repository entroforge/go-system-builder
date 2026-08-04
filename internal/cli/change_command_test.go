package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRuntimeChangeCreateCommandBuildsChecksFromInput(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"id":"CHG-001","summary":"fix timeout","class":"bugfix","risk":"medium","scope":{"include":["internal/client/**"],"exclude":[]},"work_items":[{"id":"W-1","text":"fix timeout","owner":"main","write_paths":["internal/client/**"]}]}`)
	inputPath := filepath.Join(root, "change-input.json")
	if err := os.WriteFile(inputPath, input, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "change", "create", "--root", root, "--state", "loop-state.json", "--journal", "loop-events.jsonl",
		"--expected-revision", "1", "--input", "change-input.json",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runtime change create failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("command output is not JSON: %v", err)
	}
	if result["Revision"] != float64(2) {
		t.Fatalf("unexpected result: %#v", result)
	}
	stateAfter, err := os.ReadFile(filepath.Join(root, "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateAfter), `"id": "CHG-001"`) {
		t.Fatalf("Change Record was not stored: %s", stateAfter)
	}
}
