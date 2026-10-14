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

func TestRepairLeafHelpUsesFlagsWithoutReadingRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	if err := os.WriteFile(statePath, []byte("unparseable runtime"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"session open", "plan compile", "dispatch", "plan-report submit", "execution begin", "result submit", "changeset compute", "impact create", "impact commit", "targeted create", "targeted commit", "targeted resume", "handoff create", "handoff commit", "status"} {
		t.Run(leaf, func(t *testing.T) {
			args := append([]string{"runtime", "repair"}, strings.Fields(leaf)...)
			args = append(args, "--root", root, "--help")
			var out, diagnostic bytes.Buffer
			if code := cli.Run(args, strings.NewReader(""), &out, &diagnostic); code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "-root") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
			}
		})
	}
	entries, err := os.ReadDir(filepath.Join(root, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "loop-state.json" {
		t.Fatalf("help wrote Runtime files: %v", entries)
	}
}

func TestHelpTextUsedAsAValueDoesNotHijackLeaf(t *testing.T) {
	for _, args := range [][]string{
		{"runtime", "repair", "result", "submit", "--root", t.TempDir(), "--summary", "help"},
		{"runtime", "repair", "result", "submit", "--root", t.TempDir(), "--", "--help"},
		{"runtime", "repair", "result", "submit", "--unknown-flag", "--help"},
	} {
		var out, diagnostic bytes.Buffer
		if code := cli.Run(args, strings.NewReader(""), &out, &diagnostic); code == 0 {
			t.Fatalf("data or invalid flags became successful help: %v: %s", args, &out)
		}
	}
}

func TestS10ScaffoldSeparatesKindsAndDoesNotInventPass(t *testing.T) {
	for _, kind := range []string{"acceptance", "release_audit"} {
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"s10", "manifest", "scaffold", "--kind", kind}, strings.NewReader(""), &out, &diagnostic); code != 0 {
			t.Fatalf("code=%d %s", code, &diagnostic)
		}
		var envelope map[string]any
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["kind"] != kind || envelope["conclusion"] != "<OUTCOME>" {
			t.Fatalf("unsafe/wrong scaffold: %s", &out)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"s10", "manifest", "scaffold", "--kind", "acceptance", "--outcome", "approved"}, strings.NewReader(""), &out, &diagnostic); code == 0 {
		t.Fatal("release-audit outcome accepted for acceptance")
	}
}
