package cli

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/integration"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationStatusMissingLockIsReadOnlyWithoutRuntime(t *testing.T) {
	root := t.TempDir()
	var out, stderr bytes.Buffer
	if code := Run([]string{"runtime", "workspace", "integration-status", "--root", root}, nil, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var observed integration.WorkspaceObservation
	if err := json.Unmarshal(out.Bytes(), &observed); err != nil || observed.LockState != "not_created" || observed.Authorizes {
		t.Fatal(out.String(), err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("read-only status created Runtime/lock directories")
	}
}
