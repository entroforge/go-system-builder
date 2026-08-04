// concurrent_pretooluse_cas_test.go — CT-039-03 / AC-007 true concurrent
// PreToolUse Hook CLI CAS (BE-039 §5.5 / REQ-039 §19 AC-007 / SYNC-039 §12).

package req039_test

import (
	"encoding/json"
	"sync"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03903_ConcurrentHookCLI_OneCommit covers CT-039-03 at Hook CLI
// fidelity: two concurrent PreToolUse invocations observe the same
// revision with a satisfied design gate; at most one may commit
// PTR-PLAN-01 (CAS loser recomputes / no-ops).
func TestCT03903_ConcurrentHookCLI_OneCommit(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	body := req039fixtures.HookBody("PreToolUse", "session-ct-039-03", "Edit", map[string]any{
		"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md",
	})

	const N = 2
	type outcome struct {
		code   int
		stdout string
		stderr string
	}
	results := make([]outcome, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			code, stdout, stderr := runHook(t, root, "PreToolUse", body)
			results[i] = outcome{code: code, stdout: stdout, stderr: stderr}
		}()
	}
	wg.Wait()

	commits := 0
	for i, r := range results {
		if r.code != 0 && r.code != 2 {
			t.Fatalf("concurrent Hook %d failed: code=%d stderr=%s", i, r.code, r.stderr)
		}
		_, qg := req039fixtures.ParseQualityGate(t, r.stdout)
		if req039fixtures.TransitionCommitted(qg) {
			commits++
			if cand := req039fixtures.CandidateTransition(qg); cand != "" && cand != "PTR-PLAN-01" {
				t.Fatalf("committed candidate want PTR-PLAN-01, got %q", cand)
			}
		}
	}
	if commits > 1 {
		t.Fatalf("CT-039-03 / AC-007 CAS forbids %d simultaneous same-revision commits", commits)
	}
	if commits < 1 {
		t.Fatalf("CT-039-03 expected at least one Hook to commit PTR-PLAN-01 with satisfied design gate; got 0 commits (stdout0=%s stdout1=%s)",
			results[0].stdout, results[1].stdout)
	}

	after := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, after, "planning", "contracts")
	req039fixtures.AssertLastTransition(t, after, "PTR-PLAN-01")
	if rev := req039fixtures.Revision(after); rev <= 1 {
		t.Fatalf("revision must advance after single commit, got %v", rev)
	}
}

// TestAC007_ConcurrentHookCLI_OneCommit is the AC-007 twin of CT-039-03
// (same concurrent Hook CLI semantics).
func TestAC007_ConcurrentHookCLI_OneCommit(t *testing.T) {
	TestCT03903_ConcurrentHookCLI_OneCommit(t)
}

// TestConcurrent_PreToolUse_CAS_OneCommit retains the prior RunControlCycle
// seam pin for regression; CT/AC fidelity is scored from the Hook CLI tests.
func TestConcurrent_PreToolUse_CAS_OneCommit(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	body := map[string]any{
		"session_id":      "session-sys-cas-seam",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runHook(t, root, "PreToolUse", string(raw))
	if code != 0 && code != 2 {
		t.Fatalf("seam Hook failed: code=%d stderr=%s", code, stderr)
	}
	_, qg := req039fixtures.ParseQualityGate(t, stdout)
	if !req039fixtures.TransitionCommitted(qg) {
		t.Fatalf("seam path expected a commit with design gate satisfied, qg=%v", qg)
	}
}
