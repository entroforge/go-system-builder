package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeploymentCheckRejectsForeignJournal(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		"loop-state.json":   `{"runtime_id":"REQ-1","journal":{"last_sequence":11}}`,
		"loop-events.jsonl": `{"runtime_id":"loop-inactive","sequence":1}`,
	} {
		if err := os.WriteFile(filepath.Join(root, ".claude", path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	if code := runDeploymentCheck([]string{"--root", root}, &out, &stderr); code != 1 {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "state/journal runtime_id mismatch") {
		t.Fatal(out.String())
	}
}
