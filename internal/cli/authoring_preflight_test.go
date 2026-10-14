package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func authoringTree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAuthoringHelpAndLintNeverRecoverOrWriteRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"loop-state.json", "loop-state.json.lock", "loop-state.json.lock.process", "loop-state.json.pending"} {
		if err := os.WriteFile(filepath.Join(root, ".claude", path), []byte("malformed/locked fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := schema.ReadAsset("review-plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), plan, 0644); err != nil {
		t.Fatal(err)
	}
	before := authoringTree(t, root)
	for _, command := range []string{"s7 lint", "runtime human-decision scaffold", "runtime human-decision lint", "runtime operation"} {
		var out, diagnostic bytes.Buffer
		args := append(strings.Fields(command), "--root", root, "--help")
		if code := cli.Run(args, strings.NewReader(""), &out, &diagnostic); code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "-root") {
			t.Fatalf("%s help: %d %s %s", command, code, &out, &diagnostic)
		}
	}
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"s7", "lint", "--root", root, "--file", "plan.json", "--author-only"}, strings.NewReader(""), &out, &diagnostic)
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("code=%d %s %s", code, &out, &diagnostic)
	}
	if report["mode"] != "author_only" || report["transition_ready"] != false {
		t.Fatalf("unexpected report: %s", &out)
	}
	out.Reset()
	diagnostic.Reset()
	if code := cli.Run([]string{"s7", "lint", "--root", root, "--file", "plan.json"}, strings.NewReader(""), &out, &diagnostic); code == 0 {
		t.Fatal("malformed Runtime was silently ignored")
	}
	if after := authoringTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only authoring changed files")
	}
}
