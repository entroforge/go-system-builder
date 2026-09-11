package semantic_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateRuntimeReachabilityAcceptsCanonicalCRLFREQFingerprint(t *testing.T) {
	root := t.TempDir()
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-CRLF.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	req := []byte("# REQ-CRLF\r\n\r\n状态：locked\r\n")
	if err := os.WriteFile(reqPath, req, 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := sha256.Sum256([]byte("# REQ-CRLF\n\n状态：locked\n"))
	state := map[string]any{
		"bound_req": map[string]any{
			"path":   "docs/requirements/REQ-CRLF.md",
			"sha256": fmt.Sprintf("%x", canonical),
		},
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
		t.Fatalf("canonical CRLF bound REQ fingerprint should validate: %v", err)
	}
}
