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
	"github.com/entroforge/go-system-builder/internal/runtime"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// S2 receives the authority checkout's actual design. The original S3 source
// consumer freezes committed inputs and rejects missing or dirty authority
// sources. A child checkout cannot substitute for either responsibility.
func TestAuditStageSourcesUseOriginalConsumerBoundaryE2E(t *testing.T) {
	root := auditSourceRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.EnsureStateRoot(state, root)
	writeAuditFile(t, root, "docs/design/README.md", "# Design sources\n")
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

	// Receiving S2 work does not require committing each professional save.
	// Its exit registers the actual authority design for the next consumer.
	writeAuditFile(t, root, architecturePath, string(architecture))
	_, qg = auditHook(t, root, executionRoot, "audit-untracked")
	if status := stringValueAudit(qg["status"]); status != "satisfied" && status != "advanced" {
		t.Fatalf("actual authority design must satisfy S2, got status=%q qg=%v", status, qg)
	}
	if committed, _ := qg["transition_committed"].(bool); !committed {
		t.Fatalf("received S2 design must commit the automatic transition, qg=%v", qg)
	}
	if !strings.Contains(gotEvidenceRefsAudit(qg), "ev-audit-design") {
		t.Fatalf("disk evidence must remain usable after the formal commit, qg=%v", qg)
	}
	final := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, final, "planning", "contracts")
	// The legacy Hook fixture above has no packet configuration. Enable the
	// current S3 reader only in this in-memory fixture, retaining the actual
	// registered REQ/design sources. This is not a Runtime migration or an
	// assertion that S3 issuance succeeded.
	final["schema_version"] = "1.2.0"
	final["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	before := readSystemRuntimePair(t, root)
	resolve := func(wantError string) {
		t.Helper()
		view, err := fileview.New(root, "refs/heads/test-development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if err != nil {
			t.Fatal(err)
		}
		inputs, err := runtime.ResolveContractInputs(root, final, view)
		if wantError != "" {
			if err == nil || !strings.Contains(err.Error(), wantError) || !strings.Contains(err.Error(), architecturePath) {
				t.Fatalf("S3 source failure must identify %s and %q: %v", architecturePath, wantError, err)
			}
		} else if err != nil || len(inputs.Designs) != 1 || inputs.Designs[0].SHA256 != architectureSHA {
			t.Fatalf("S3 failed to freeze the actual committed design: %+v %v", inputs.Designs, err)
		}
		assertSystemRuntimePairUnchanged(t, before)
	}
	resolve("read S3 source through original file view")
	// Staging still does not deliver a committed S3 input.
	runGitIn(t, root, "add", architecturePath)
	resolve("read S3 source through original file view")
	runGitIn(t, root, "commit", "-qm", "authority architecture")
	resolve("")
	writeAuditFile(t, root, architecturePath, string(architecture)+"\nUncommitted source change.\n")
	resolve("S3 frozen input differs from original checkout SHA")
	writeAuditFile(t, root, architecturePath, string(architecture))

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
