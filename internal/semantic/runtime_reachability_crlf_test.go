package semantic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateRuntimeReachabilityIgnoresHistoricalDigests(t *testing.T) {
	root := t.TempDir()
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-CRLF.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	req := []byte("# REQ-CRLF\r\n\r\n状态：locked\r\n")
	if err := os.WriteFile(reqPath, req, 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"bound_req": map[string]any{
			"path":   "docs/requirements/REQ-CRLF.md",
			"sha256": "obsolete-nonstandard",
		},
		"documents": []any{map[string]any{"id": "REQ-CRLF", "path": "docs/requirements/REQ-CRLF.md", "sha256": "wrong"}},
		"evidence":  []any{map[string]any{"id": "E-1", "path": "docs/requirements/REQ-CRLF.md", "sha256": "wrong"}},
	}
	stateData, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeReachability(root); err != nil {
		t.Fatalf("existing source must not require historical digests: %v", err)
	}
	if err := os.Remove(reqPath); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeReachability(root); err == nil {
		t.Fatal("missing original source must still be reported")
	}
}
