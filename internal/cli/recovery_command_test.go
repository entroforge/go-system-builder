package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestRuntimeRecoverInspectDoesNotRequireReadableActiveState(t *testing.T) {
	root := newRecoveryCommandRoot(t)
	corrupt := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"runtime_id":"loop-REQ-900"}`)...)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.WriteFile(statePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "recover", "inspect",
		"--root", root,
		"--req", "docs/requirements/REQ-900.md",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("recover inspect must not parse the active runtime as a precondition: code=%d stderr=%s", code, stderr.String())
	}

	var inventory struct {
		REQ struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"req"`
		ActiveRuntime struct {
			Readable bool   `json:"readable"`
			SHA256   string `json:"sha256"`
		} `json:"active_runtime"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
		t.Fatalf("inspect output must be JSON: %v\n%s", err, stdout.String())
	}
	if inventory.REQ.ID != "REQ-900" || inventory.REQ.Path != "docs/requirements/REQ-900.md" || inventory.REQ.SHA256 != "" {
		t.Fatalf("inspect must retain the explicit REQ without a digest: %#v", inventory.REQ)
	}
	if inventory.ActiveRuntime.Readable || inventory.ActiveRuntime.SHA256 != "" {
		t.Fatalf("inspect must retain the unreadable Runtime without a digest: %#v", inventory.ActiveRuntime)
	}
	if strings.Contains(stdout.String(), `"sha256"`) {
		t.Fatalf("inspect registered a digest: %s", stdout.String())
	}
}

func TestRuntimeRecoverInspectRequiresExplicitREQ(t *testing.T) {
	root := newRecoveryCommandRoot(t)
	var stdout, stderr bytes.Buffer
	code := cli.Run(
		[]string{"runtime", "recover", "inspect", "--root", root},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("recover inspect without --req code=%d, want usage error 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "requires --req") {
		t.Fatalf("missing explicit-REQ guidance: %s", stderr.String())
	}
}

func newRecoveryCommandRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "docs", "requirements"),
		filepath.Join(root, ".claude"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	req := "# REQ-900\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-900.md"), []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), []byte("legacy-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRuntimeRecoverApplyPreservesUnresolvedOriginalPending(t *testing.T) {
	root := newRecoveryCommandRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	pendingPath := statePath + ".commit-pending.json"
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	pending := []byte(`{"schema_version":"1.0.0","state":{"revision":9},"old_sha256":"historical-only"}`)
	if err := os.WriteFile(pendingPath, pending, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "recover", "apply", "--root", root, "--plan", ".claude/recovery/unavailable/plan.json", "--approved-by", "actual-test-operator"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "original pending Runtime operation is unresolved") {
		t.Fatalf("unknown original pending must be reported: code=%d stderr=%s", code, stderr.String())
	}
	got, err := os.ReadFile(pendingPath)
	if err != nil || !bytes.Equal(got, pending) {
		t.Fatal("unknown original pending was removed or rewritten")
	}
	got, err = os.ReadFile(statePath)
	if err != nil || !bytes.Equal(got, originalState) {
		t.Fatal("unknown original Runtime was overwritten")
	}
}
