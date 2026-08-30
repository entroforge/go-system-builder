// Package req039_test covers REQ-039 end-to-end system tests including
// CT-039-11~24 multi-step Hook scenarios and BUG-039-19 S2→S11 hook-driven path.
//
// Coverage:
//   - s2_to_s11_clean_path_test.go    : envelope conformance (retained)
//   - s2_to_s11_hook_driven_test.go   : FR-024/FR-025 real hook-driven path
//   - ct_verification_chain_test.go   : CT-039-13, CT-039-21
//   - ct_bug_correction_test.go       : CT-039-14, CT-039-22, CT-039-23
//   - ct_s11_stop_test.go             : CT-039-15
//   - ct_integration_resume_test.go   : CT-039-17
//   - concurrent_pretooluse_cas_test.go : CT-039-03 / AC-007 concurrent Hook CLI CAS
//   - ct_clean_integrate_test.go        : CT-039-09 / AC-006 (BUG-039-37 honest probe)
//   - hook_surface_test.go              : PreCompact / PostCompact-fallback / TeammateIdle
//   - compact_recovery_test.go          : post-Compact SessionStart
//   - subagent_integration_test.go      : SubagentStop integration guidance
package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// runCLI delegates to the in-tree cli.Run entry point so the system
// tests use the same code path the harness ships.
func runCLI(t *testing.T, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	t.Helper()
	return cli.Run(args, stdin, stdout, stderr)
}

// repoRoot walks up from the test source file to find go.mod, the same
// pattern used by internal/cli/ac_test.go.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not resolve repository root from test source")
	return ""
}

// freshRoot copies the docs/ authorities the Controller / minimal-safety
// policy need (loop-definition.json + hook-policy.json) into a temp
// repository. The schema-valid loop-state.json is then layered on by
// the caller via writeSystemState.
func freshRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"docs/loop-definition.json",
		"docs/hook-policy.json",
		// RC-06 (S10-3): the protected-release policy rule loads the
		// data-driven protected-commands table from the runtime root; the
		// fixture must ship the real table so Bash classification sees the
		// production surface instead of failing closed on a missing file.
		"docs/release_audits/protected_commands.json",
	} {
		source := filepath.Join(repoRoot(t), rel)
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeSystemState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	req039fixtures.EnsureStateRoot(state, root)
	path := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// systemPlanningState returns a schema-valid loop-state.json fixture
// for the requested phase / revision. It mirrors the planningState
// helper in internal/cli/ac_test.go (same fix-up: zero-SHA for
// definition/policy, full bound_req, configuration block) so the
// PostCommitValidator accepts the persisted state.
func systemPlanningState(t *testing.T, root, phase string, revision int) map[string]any {
	t.Helper()
	const zeroSHA = "0000000000000000000000000000000000000000000000000000000000000000"
	stageLetter := "2"
	switch phase {
	case "design":
		stageLetter = "2"
	case "contracts":
		stageLetter = "3"
	case "tasks":
		stageLetter = "4"
	}
	// The bound REQ must exist on disk (hook-protected path; the AC bridge
	// and reachability read it) — a minimal locked REQ with no AC rows.
	// req_baseline_unchanged is a real fingerprint guard (L3-S6 P0-4), so
	// the pinned sha must match these exact bytes.
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-039.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	reqBytes := []byte("# REQ-039\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n\n" +
		"| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-001 | controller | 控制平面 | A1 | Must |\n")
	if err := os.WriteFile(reqPath, reqBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	reqSum := sha256.Sum256(reqBytes)
	reqSHA := hex.EncodeToString(reqSum[:])
	return map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-system-test",
		"definition": map[string]any{
			"path":    "docs/loop-definition.json",
			"version": "1.1.0",
			"sha256":  "b6d545f83b7b31c9a140a1a96770c8866ebf7ef4f482c51687dfbacf38de0908",
		},
		"revision": float64(revision),
		"lifecycle": map[string]any{
			"state":          "planning",
			"phase":          phase,
			"phase_revision": 0,
		},
		"milestone": map[string]any{
			"stage":           "S" + stageLetter,
			"lifecycle_state": "planning",
			"lifecycle_phase": phase,
			"objective":       "complete the " + phase + " phase",
			"action":          "complete the planning phase for " + phase,
			"protocol_ref":    "docs/agent-protocol.md#" + phase,
			"manual_ref":      ".claude/bin/loop-harness.md",
			"primary_skill":   "specification-planning",
			"read":            []any{"docs/requirements/REQ-039.md"},
			"read_order":      []any{"LOOP RECOVERY packet (this message)", "AGENTS.md", ".claude/loop-state.json", "docs/agent-protocol.md#" + phase},
			"missing":         []any{},
			"done_when":       []any{},
			"questions":       []any{},
			"automation":      []any{"do not call loop-harness for normal continuation"},
			"integration":     []any{},
			"human_required":  false,
			"blocked":         false,
			"blocker":         nil,
			"event":           "SessionStart",
			"instruction":     "LOOP RECOVERY: you are at S" + stageLetter + ".",
			"recovery":        []any{"read docs/agent-protocol.md#" + phase, "if blocked read .claude/bin/loop-harness.md"},
			"source_revision": float64(revision),
			"updated_at":      "2026-07-30T00:00:00Z",
		},
		"authorization": map[string]any{
			"mode":        "binding",
			"command":     "loop-harness req bind",
			"actor":       "user",
			"occurred_at": "2026-07-30T00:00:00Z",
		},
		"bound_req": map[string]any{
			"id":          "REQ-039",
			"path":        "docs/requirements/REQ-039.md",
			"version":     "v1.0.0",
			"sha256":      reqSHA,
			"status":      "locked",
			"approved_by": "user",
			"approved_at": "2026-07-30T00:00:00Z",
			"metadata":    map[string]any{"ui_impact": "none"},
		},
		"baseline": map[string]any{"generation": 1, "captured_at": "2026-07-30T00:00:00Z"},
		"review":   map[string]any{"round": 0, "clean_round": nil},
		"configuration": map[string]any{
			"repair": map[string]any{
				"max_attempts_per_bug":       3,
				"max_same_contract_failures": 2,
				"max_full_review_rounds":     5,
			},
		},
		"entities":        map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"documents":       []any{},
		"evidence":        []any{},
		"blockers":        []any{},
		"pause":           nil,
		"last_transition": nil,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": 0,
			"last_event_id": nil,
		},
		"hook_control": map[string]any{
			"policy_ref":           map[string]any{"path": "docs/hook-policy.json", "version": "v2.0.0", "sha256": "8dea604dfce3a7f0869938eed5f4f6cc225261ed9f20cc8a1c2b5ddb4c5b91ec"},
			"mode":                 "enforce",
			"health":               "healthy",
			"consecutive_failures": 0,
			"last_checked_at":      nil,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// runHook is a thin wrapper around cli.Run for system tests.
func runHook(t *testing.T, root, event, body string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{"hook", "--event", event, "--root", root}, bytes.NewReader([]byte(body)), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func runHookWithRunner(t *testing.T, runner *req039fixtures.CLIRunner, root, event, body string) (int, string, string) {
	t.Helper()
	return req039fixtures.RunHook(t, runner, root, event, body)
}
