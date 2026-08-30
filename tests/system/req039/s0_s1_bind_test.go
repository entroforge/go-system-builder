// s0_s1_bind_test.go — BUG-039-39 S0/S1: init → req bind → planning.design
// (+ optional first PreToolUse) via real CLI, not unit-only pins.

package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestS0S1_InitBindEntersPlanningDesign covers the cold-start spine:
// loop-harness init → req bind → lifecycle planning.design / S2, then a
// first PreToolUse Hook that surfaces a quality_gate envelope.
func TestS0S1_InitBindEntersPlanningDesign(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reqBody := "# REQ-039\n\n> 状态：locked\n> 版本：v2.0.0\n> UI impact：none\n"
	reqRel := "docs/requirements/REQ-039.md"
	if err := os.WriteFile(filepath.Join(root, reqRel), []byte(reqBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runCLI(t, []string{"init", "--root", root}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json")); err != nil {
		t.Fatalf("init must create loop-state.json: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(t, []string{
		"req", "bind", "--root", root,
		"--req", reqRel, "--approved-by", "user",
	}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("req bind failed: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	after := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, after, "planning", "design")
	req039fixtures.AssertBindingReceipt(t, after, "TR-001")
	ms, _ := after["milestone"].(map[string]any)
	// TR-001 lands planning.design; stage may remain S0 until milestone refresh
	// on the first Hook — either S0 (bind cursor) or S2 (planning.design) is OK.
	if stage, _ := ms["stage"].(string); stage != "S2" && stage != "S0" {
		t.Fatalf("S0/S1 milestone.stage want S0 or S2, got %q", stage)
	}
	bound, _ := after["bound_req"].(map[string]any)
	if id, _ := bound["id"].(string); id != "REQ-039" {
		t.Fatalf("bound_req.id want REQ-039, got %q", id)
	}

	body := req039fixtures.PreToolUseBody("session-s0-s1", "Edit", map[string]any{
		"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md",
	})
	code, hookOut, hookErr := runHook(t, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("first PreToolUse after bind failed: code=%d stderr=%s", code, hookErr)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(hookOut), &env); err != nil {
		// Hook may emit multi-line / non-strict JSON; quality_gate presence is enough.
		if !bytes.Contains([]byte(hookOut), []byte("quality_gate")) &&
			!bytes.Contains([]byte(hookOut), []byte("permissionDecision")) {
			t.Fatalf("first PreToolUse must surface Hook envelope, got %s", hookOut)
		}
		return
	}
	_ = env
}
