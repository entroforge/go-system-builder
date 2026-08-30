// scope_p0_test.go pins the L3-S6 P0-2/P0-3 wiring: the write-scope audit
// (real changed paths ⊆ declared WritePaths) and the real command check
// runner.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func scopeAssignment(worktree, branch, target string, writePaths []string) hookctx.AssignmentContext {
	row := assignmentContext(worktree, branch, target)
	row.WritePaths = writePaths
	return row
}

// TestInspectOutOfScopeDiffPreservesWorktree: a source diff touching files
// outside the assignment's declared WritePaths must block the integration
// (worktree preserved via the caller) with the offending paths named.
func TestInspectOutOfScopeDiffPreservesWorktree(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	// feature-commit changes one in-scope and one out-of-scope file.
	fr.fileContents["feature-commit:internal/order/handler.go"] = "new"
	fr.fileContents["feature-commit:internal/unrelated/rogue.go"] = "new"

	req := InspectRequest{
		Root:               root,
		Assignment:         scopeAssignment(wt, "feature", "develop", []string{"internal/order/**"}),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{SkipCompletionCheck: true})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("out-of-scope diff must not be ready, blockers=%v", insp.Blockers)
	}
	if !containsBlockerPrefix(insp.Blockers, ErrScopeViolation.Error()) {
		t.Fatalf("expected scope-violation blocker, got %v", insp.Blockers)
	}
	if len(insp.OutOfScopeDiff) != 1 || insp.OutOfScopeDiff[0] != "internal/unrelated/rogue.go" {
		t.Fatalf("out-of-scope diff = %v, want [internal/unrelated/rogue.go]", insp.OutOfScopeDiff)
	}
}

// TestInspectInScopeDiffPassesAudit: the same diff with both files inside
// the declared scope passes the audit and stays ready.
func TestInspectInScopeDiffPassesAudit(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	fr.fileContents["feature-commit:internal/order/handler.go"] = "new"
	fr.fileContents["feature-commit:internal/order/repo/store.go"] = "new"

	req := InspectRequest{
		Root: root,
		Assignment: scopeAssignment(wt, "feature", "develop", []string{
			"internal/order/**", "internal/order",
		}),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{SkipCompletionCheck: true})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !insp.Ready {
		t.Fatalf("in-scope diff must be ready, blockers=%v", insp.Blockers)
	}
	if len(insp.OutOfScopeDiff) != 0 {
		t.Fatalf("out-of-scope diff = %v, want none", insp.OutOfScopeDiff)
	}
}

// TestPathMatchesPattern pins the glob semantics the audit relies on:
// directory prefixes, `**` subtrees and single-segment `*`.
func TestPathMatchesPattern(t *testing.T) {
	cases := []struct {
		file, pattern string
		want          bool
	}{
		{"internal/order/handler.go", "internal/order/**", true},
		{"internal/order/sub/repo/store.go", "internal/order/**", true},
		{"internal/orderliness/rogue.go", "internal/order/**", false},
		{"internal/order/handler.go", "internal/order", true},
		{"internal/order/a/b.go", "internal/order", true},
		{"internal/other/a.go", "internal/order", false},
		{"internal/order/handler.go", "internal/*/handler.go", true},
		{"internal/order/other.go", "internal/*/handler.go", false},
		{"docs/any/deep/path.md", "**", true},
		{"a.go", "*.go", true},
		{"b/priv.go", "*.go", false},
	}
	for _, tc := range cases {
		if got := pathMatchesPattern(tc.file, tc.pattern); got != tc.want {
			t.Errorf("pathMatchesPattern(%q, %q) = %v, want %v", tc.file, tc.pattern, got, tc.want)
		}
	}
}

// TestCommandCheckRunnerRunsRealCommands: the default runner executes
// through the shell in the repo root and fails on non-zero exits — the
// wiring that makes `verified` mean "checks actually ran".
func TestCommandCheckRunnerRunsRealCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommandCheckRunner(context.Background(), root, "test -f marker.txt"); err != nil {
		t.Fatalf("passing check failed: %v", err)
	}
	if err := CommandCheckRunner(context.Background(), root, "test -f absent.txt"); err == nil {
		t.Fatal("failing check must return an error")
	}
	// The command runs with the repo root as cwd.
	if err := CommandCheckRunner(context.Background(), root, "test -f \"$PWD/marker.txt\""); err != nil {
		t.Fatalf("check must run in repo root: %v", err)
	}
}

// TestInspectRequiredChecksRunViaRunner: Inspect records each configured
// check as pass/fail from the runner outcome and blocks on failure —
// with a runner wired, "skip" is no longer reachable.
func TestInspectRequiredChecksRunViaRunner(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
		CheckRunner: func(ctx context.Context, root, command string) error {
			if command == "go test ./..." {
				return nil
			}
			return stringErr("boom")
		},
		RequiredChecks: []string{"go test ./...", "go vet ./..."},
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("failing check must block readiness, blockers=%v", insp.Blockers)
	}
	if len(insp.RequiredChecks) != 2 {
		t.Fatalf("checks = %v, want two recorded results", insp.RequiredChecks)
	}
	if insp.RequiredChecks[0].Status != "pass" || insp.RequiredChecks[1].Status != "fail" {
		t.Fatalf("check statuses = %v/%v, want pass/fail",
			insp.RequiredChecks[0].Status, insp.RequiredChecks[1].Status)
	}
}
