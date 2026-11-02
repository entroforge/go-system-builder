package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestS4ResultBindingThroughTaskIntegrate exercises the explicit
// `runtime task-integrate` verb against a current canonical Assignment.
//
// The checkpoint must bind the verified Result, the integration checks must
// actually run, the recorded verification must be reused without repeating the
// merge, and a missing Result must fail closed without mutating the
// checkpoint.
//
// The current wiring re-verifies a Result when a **new accepted Result**
// arrives (a new Result revision changes the canonical Main-facts binding,
// internal/integration/integrate_responsibility.go:94). Editing the stored
// Result file in place no longer describes that scenario, so the reuse and
// fail-closed contracts are asserted instead.
func TestS4ResultBindingThroughTaskIntegrate(t *testing.T) {
	root, builder := seedCanonicalIntegrableAssignment(t)
	resultRef := completionTaskRef(t, req039fixtures.ReadState(t, root), builder.task)
	if resultRef == "" {
		t.Fatal("canonical Assignment has no received Result")
	}
	cpPath := canonicalBuilderCheckpoint(t, builder)
	target := canonicalTargetBranch(t, root)
	check := func() map[string]any {
		t.Helper()
		code, out, errout := runTaskIntegrate(t, root, builder.record.AssignmentID)
		if code != 0 || strings.Contains(errout, "integration failed") {
			t.Fatalf("task-integrate: %d %s %s", code, out, errout)
		}
		b, err := os.ReadFile(cpPath)
		if err != nil {
			t.Fatal(err)
		}
		var cp map[string]any
		if err := json.Unmarshal(b, &cp); err != nil {
			t.Fatal(err)
		}
		if cp["state"] != "complete" || cp["completion_report_path"] != resultRef {
			t.Fatalf("checkpoint does not bind verified report: %s", b)
		}
		// The integration checks actually ran; the checkpoint carries their
		// receipts rather than a timestamp claim.
		receipts, _ := cp["check_receipts"].([]any)
		if len(receipts) == 0 {
			t.Fatalf("integration checks did not run: %s", b)
		}
		return cp
	}

	first := check()
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", target))

	// The same accepted Result is reused: the recorded verification stands and
	// the merge is not repeated.
	cp, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]any
	if err := json.Unmarshal(cp, &recorded); err != nil {
		t.Fatal(err)
	}
	recorded["state"] = "verified"
	cp, _ = json.Marshal(recorded)
	if err := os.WriteFile(cpPath, cp, 0o644); err != nil {
		t.Fatal(err)
	}
	second := check()
	if first["verified_at"] != second["verified_at"] {
		t.Fatal("the same accepted Result must keep its recorded verification")
	}
	if after := strings.TrimSpace(runGitIn(t, root, "rev-parse", target)); after != head {
		t.Fatal("Result refresh repeated the merge")
	}

	// A missing Result must fail closed and leave the verified checkpoint
	// untouched.
	cp, err = os.ReadFile(cpPath)
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]any
	if err := json.Unmarshal(cp, &before); err != nil {
		t.Fatal(err)
	}
	before["state"] = "verified"
	cp, _ = json.Marshal(before)
	if err := os.WriteFile(cpPath, cp, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(resultRef))); err != nil {
		t.Fatal(err)
	}
	code, out, errout := runTaskIntegrate(t, root, builder.record.AssignmentID)
	if code == 0 && !strings.Contains(strings.ToLower(out+errout), "block") && !strings.Contains(strings.ToLower(out+errout), "fail") {
		t.Fatalf("missing report silently accepted: %s %s", out, errout)
	}
	after, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatal(err)
	}
	var afterCP map[string]any
	if err := json.Unmarshal(after, &afterCP); err != nil {
		t.Fatal(err)
	}
	if afterCP["state"] != "verified" || afterCP["verified_at"] != before["verified_at"] {
		t.Fatalf("missing report changed verified checkpoint: %s", after)
	}
	_ = req039fixtures.ReadState
}
