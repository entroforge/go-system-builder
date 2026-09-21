package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestAuditDispatchStatusJSONReadOnlyAndCapacity(t *testing.T) {
	root := auditCLIPlanFixture(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "status", "--root", root, "--capacity", "2", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dispatch status failed: code=%d stderr=%s", code, stderr.String())
	}
	var board dispatch.Board
	if err := json.Unmarshal(stdout.Bytes(), &board); err != nil {
		t.Fatalf("invalid dispatch JSON %q: %v", stdout.String(), err)
	}
	if board.Legacy {
		t.Fatalf("registered waves-v1 plan was projected as legacy: %+v", board)
	}
	if got, want := board.Next, []string{"TASK-042-01", "TASK-042-02"}; !auditCLISameStrings(got, want) {
		t.Fatalf("capacity two next batch %v, want %v", got, want)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("s6 status --json mutated loop-state.json")
	}
}

func TestAuditDispatchStatusDeclaresCapacityAndLegacyRecovery(t *testing.T) {
	root := auditCLIPlanFixture(t)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status without capacity failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"S6 dispatch plan:",
		"Next batch: []",
		"Declare actual platform capacity with --capacity",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("capacity guidance missing %q:\n%s", want, stdout.String())
		}
	}

	legacy := s6Fixture(t)
	before, err := os.ReadFile(filepath.Join(legacy, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"s6", "status", "--root", legacy}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("legacy status failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "legacy: no reviewed dispatch plan; existing execution remains recoverable") || !strings.Contains(stdout.String(), "TASK-A") {
		t.Fatalf("legacy projection did not remain readable:\n%s", stdout.String())
	}
	after, err := os.ReadFile(filepath.Join(legacy, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("legacy s6 status mutated loop-state.json")
	}
}

func auditCLIPlanFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := "../../docs/examples/dispatch-plan/project"
	if err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(source, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
			return mkdirErr
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	auditCLIGit(t, root, "init", "-b", "dev")
	auditCLIGit(t, root, "config", "user.name", "Test")
	auditCLIGit(t, root, "config", "user.email", "test@example.invalid")
	auditCLIGit(t, root, "add", ".")
	auditCLIGit(t, root, "commit", "-m", "baseline")

	p, err := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if err != nil || len(p.Problems) > 0 {
		t.Fatalf("fixture plan invalid: %+v %v", p, err)
	}
	docs := []any{}
	addDocument := func(id, kind, path string) {
		b, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		docs = append(docs, map[string]any{
			"id": id, "kind": kind, "path": path,
			"sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "status": "complete", "generation": 1,
		})
	}
	addDocument("dispatch-plan:REQ-042", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		addDocument(task.ID, "task", task.Path)
	}
	state := map[string]any{
		"schema_version": "1.1.0", "runtime_id": "audit-cli", "revision": 1,
		"lifecycle": map[string]any{"state": "building", "phase": nil, "phase_revision": 0},
		"baseline":  map[string]any{"generation": 1, "captured_at": "2026-09-19T00:00:00Z"},
		"review":    map[string]any{"round": 0, "clean_round": nil},
		"bound_req": map[string]any{"id": "REQ-042", "workspace": map[string]any{"dev_branch": "dev"}},
		"documents": docs,
		"entities":  map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"evidence":  []any{}, "pause": nil,
		"journal": map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil},
	}
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "loop-state.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func auditCLIGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func auditCLISameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
