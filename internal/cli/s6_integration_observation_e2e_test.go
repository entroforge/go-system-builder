package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6IntegrationReportsWorktreeCandidatesWithoutGating(t *testing.T) {
	root, source, record := s6HandoffFixture(t)
	mainPrivate := filepath.Join(root, "personal-notes.txt")
	if err := os.WriteFile(mainPrivate, []byte("private Main material\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source, "src/pkg/proof.txt")
	if err := os.WriteFile(path, []byte("staged product candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "add", "src/pkg/proof.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unstaged product candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "mv", "src/pkg/input_test.go", "src/pkg/renamed_test.go"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "src/pkg/renamed_test.go")); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(source, "src/pkg/not-yet-known.txt")
	if err := os.WriteFile(extra, []byte("untracked candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := workspace.Git(t.Context(), source, "status", "--porcelain=v1", "--untracked-files=normal", "--ignored=matching")
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	start := time.Now()
	code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID}, nil, &output, &diagnostic)
	t.Logf("original task-integrate elapsed: %s", time.Since(start))
	if code != 0 {
		t.Fatalf("original integration returned %d: %s %s", code, &output, &diagnostic)
	}
	var result struct {
		State       string   `json:"state"`
		Blocked     bool     `json:"blocked"`
		Integration []string `json:"integration"`
		Handoff     string   `json:"handoff"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	facts := strings.Join(result.Integration, " ")
	for _, want := range []string{"Source branch", "changed path(s)", "Worktree status", "src/pkg/proof.txt", "src/pkg/input_test.go", "src/pkg/renamed_test.go", "src/pkg/not-yet-known.txt", "not part of the branch merge", "unknown"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("missing %q from actual Main output: %s", want, &output)
		}
	}
	if result.Blocked || result.State != "integrated" || !strings.Contains(result.Handoff, "TR-006") {
		t.Fatalf("candidate status became a gate: %s", &output)
	}
	if data, err := os.ReadFile(mainPrivate); err != nil || string(data) != "private Main material\n" {
		t.Fatalf("Main private material changed: %q %v", data, err)
	}
	for _, ref := range []string{".claude/loop-state.json", ".claude/loop-events.jsonl"} {
		data, err := os.ReadFile(filepath.Join(root, ref))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("not-yet-known.txt")) || bytes.Contains(data, []byte("Worktree status at")) {
			t.Fatalf("advisory status was recorded in %s: %s", ref, data)
		}
	}
	after, err := workspace.Git(t.Context(), source, "status", "--porcelain=v1", "--untracked-files=normal", "--ignored=matching")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("original worktree/index changed:\nbefore %s\nafter %s", before, after)
	}
	mainBytes, err := os.ReadFile(filepath.Join(root, "src/pkg/proof.txt"))
	if err != nil || string(mainBytes) != "Actual implementation evidence.\n" {
		t.Fatalf("Main did not receive the committed branch (or silently included candidate): %q %v", mainBytes, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unstaged product candidate\n" {
		t.Fatalf("uncommitted work lost: %q %v", data, err)
	}
}
