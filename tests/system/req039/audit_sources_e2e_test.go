package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestAuditCommittedStageSourcesE2E exercises the source contract through the
// real Hook/controller path. The authority root starts at a committed REQ but
// has no committed architecture document. A Builder-like execution worktree
// commits that document on a child branch; the authority must still report
// not_ready. The same bytes are then staged and left dirty, with the same
// result, before the authority development branch commits them and advances.
//
// The planning_design envelope is deliberately kept under the root control
// plane (.claude/evidence), which is an explicit disk source. It therefore
// proves that a stage may consume disk evidence while formal Markdown is
// required to come from the pinned development-branch Git tree. The Hook is
// invoked with cwd pointing at the execution worktree while --root points at
// the authority root, so a cwd mix-up cannot silently make the child branch
// count as delivered.
func TestAuditCommittedStageSourcesE2E(t *testing.T) {
	root := auditSourceRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.EnsureStateRoot(state, root)
	// This commits the REQ, loop definition and policy, but no architecture
	// document. .claude remains an ignored control-plane directory.
	writeSystemState(t, root, state)

	architecturePath := "docs/architecture/ARCHITECTURE-039-audit.md"
	architecture := []byte("# ARCHITECTURE-039-audit\n\n> Status: locked\n> Version: v1.0.0\n")
	architectureSHA := sha256HexAudit(architecture)
	writeAuditEvidence(t, root, state, architecturePath, architectureSHA)

	// The child branch has a real committed formal document. It remains
	// outside the bound development branch and therefore cannot satisfy the
	// authority gate.
	executionRoot := filepath.Join(t.TempDir(), "execution")
	runGitIn(t, root, "worktree", "add", "-b", "audit/child", executionRoot, "test-development")
	t.Cleanup(func() { _ = exec.Command("git", "-C", root, "worktree", "remove", "--force", executionRoot).Run() })
	writeAuditFile(t, executionRoot, architecturePath, string(architecture))
	runGitIn(t, executionRoot, "add", architecturePath)
	runGitIn(t, executionRoot, "commit", "-qm", "child architecture")

	// The Hook reads authoritative state from root while the payload reports
	// the actual execution cwd. A child-branch commit is not an authority-tree
	// commit.
	_, qg := auditHook(t, root, executionRoot, "audit-child-commit")
	auditWantNotReady(t, qg, "child branch commit")
	if got := fmt.Sprint(qg["missing"]); !strings.Contains(got, "document:design:locked") {
		t.Fatalf("child branch must leave the formal document missing, got missing=%v", qg["missing"])
	}
	if strings.Contains(fmt.Sprint(qg["missing"]), "evidence:planning_design") {
		t.Fatalf("child branch must not make the root disk evidence the blocker, got missing=%v", qg["missing"])
	}

	// An untracked copy in the authority checkout is still not a Git-tree
	// input. The disk evidence remains consumable, so the missing fact should
	// continue to name only the formal document.
	writeAuditFile(t, root, architecturePath, string(architecture))
	_, qg = auditHook(t, root, executionRoot, "audit-untracked")
	auditWantNotReady(t, qg, "untracked formal document")

	// Staging changes the index only; the pinned commit is unchanged.
	runGitIn(t, root, "add", architecturePath)
	_, qg = auditHook(t, root, executionRoot, "audit-staged")
	auditWantNotReady(t, qg, "staged formal document")

	// Once the same document is committed on the bound development branch,
	// the next real Hook may advance PTR-PLAN-01. This also proves the disk
	// evidence was valid and consumed by the same source snapshot.
	runGitIn(t, root, "commit", "-qm", "authority architecture")
	_, qg = auditHook(t, root, executionRoot, "audit-authority-commit")
	if status := stringValueAudit(qg["status"]); status != "satisfied" && status != "advanced" {
		t.Fatalf("committed formal document must satisfy/advance the gate, got status=%q qg=%v", status, qg)
	}
	if committed, _ := qg["transition_committed"].(bool); !committed {
		t.Fatalf("committed formal document must commit the automatic transition, qg=%v", qg)
	}
	if !strings.Contains(gotEvidenceRefsAudit(qg), "ev-audit-design") {
		t.Fatalf("disk evidence must remain usable after the formal commit, qg=%v", qg)
	}
	final := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, final, "planning", "contracts")

	// A dirty test can pass in the execution checkout while still being absent
	// from the authority commit. Keep this explicit because a green test run
	// must not be reported as evidence for a different tree.
	writeAuditFile(t, executionRoot, "go.mod", "module example.com/audit\n\ngo 1.23\n")
	writeAuditFile(t, executionRoot, "dirty_test.go", `package audit

import "testing"

func TestDirtyOnly(t *testing.T) {}
`)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = executionRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dirty execution test should run in execution root: %v\n%s", err, output)
	}
	view, err := fileview.New(root, "refs/heads/test-development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := view.ReadFile("dirty_test.go"); err == nil {
		t.Fatal("a dirty execution test must not be readable from the authority commit tree")
	}
}

// auditSourceRoot copies the runtime authorities and initializes a real temp
// repository through the same fixture path as the other req039 system tests.
func auditSourceRoot(t *testing.T) string {
	t.Helper()
	root := freshRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeAuditEvidence(t *testing.T, root string, state map[string]any, subjectPath, subjectSHA string) {
	t.Helper()
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-audit-design",
		"kind":                    "planning_design",
		"runtime_id":              req039fixtures.RuntimeIDFromState(state),
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       "architect-audit",
		"producer_responsibility": "Architect",
		"subject_refs": []any{map[string]any{
			"path": subjectPath, "version": "v1.0.0", "sha256": subjectSHA,
		}},
		"conclusion": "pass",
		"created_at": "2026-09-18T00:00:00Z",
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join(".claude", "evidence", "ev-audit-design.json")
	writeAuditFile(t, root, rel, string(data))
	state["evidence"] = []any{map[string]any{
		"id":                  "ev-audit-design",
		"kind":                "planning_design",
		"path":                filepath.ToSlash(rel),
		"sha256":              sha256HexAudit(data),
		"status":              "valid",
		"baseline_generation": 1,
		"review_round":        1,
		"produced_by":         []any{"architect-audit"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "Architect",
		"scope_refs":          []any{subjectPath},
	}}
	writeAuditState(t, root, state)
}

func writeAuditState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeAuditFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func auditHook(t *testing.T, root, executionRoot, session string) (string, map[string]any) {
	t.Helper()
	payload := map[string]any{
		"session_id":      session,
		"hook_event_name": "PreToolUse",
		"agent_id":        "agent-audit",
		"cwd":             executionRoot,
		"tool_name":       "Edit",
		"tool_input": map[string]any{
			"file_path": filepath.Join(executionRoot, "docs/architecture/ARCHITECTURE-039-audit.md"),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, bytes.NewReader(body), &stdout, &stderr)
	if code != 0 && code != 2 {
		t.Fatalf("audit Hook failed: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	_, qg := req039fixtures.ParseQualityGate(t, stdout.String())
	if qg == nil {
		t.Fatalf("audit Hook output missing quality_gate: %s", stdout.String())
	}
	return stdout.String(), qg
}

func auditWantNotReady(t *testing.T, qg map[string]any, label string) {
	t.Helper()
	if status := stringValueAudit(qg["status"]); status != "not_ready" {
		t.Fatalf("%s must remain not_ready, got status=%q qg=%v", label, status, qg)
	}
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("%s must not commit a transition, qg=%v", label, qg)
	}
}

func stringValueAudit(value any) string {
	result, _ := value.(string)
	return result
}

func gotEvidenceRefsAudit(qg map[string]any) string {
	return fmt.Sprint(qg["evidence_refs"])
}

func sha256HexAudit(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
