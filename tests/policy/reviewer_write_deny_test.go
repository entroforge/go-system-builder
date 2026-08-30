package policy_test

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// The S7 frozen-baseline invariant (L3-S7 §1.4.1, §8) plus the RC-04
// phase-level freeze: verification and every non-fixing bug_resolution
// phase, acceptance, and release_audit all freeze the product surface.
// Only .claude/, docs/reports/, docs/release_audits/, and the ReviewPlan's
// declared verification artifact workspace remain writable; product writes
// elsewhere hard-deny. The sole product-write exception is
// bug_resolution.fixing through an approved RepairContract scope (tested in
// internal/policy repair matrix, not here). Non-verification stages are no
// longer open for product writes after RC-04.
func TestReviewerProductWriteDecision(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	workspace := "/tmp/e2e-workspace"

	cases := []struct {
		name      string
		state     string
		tool      string
		path      string
		workspace string
		wantBlock bool
	}{
		{"product code in verification", "verification", "Edit", "internal/controller/cycle.go", "", true},
		{"product code via MultiEdit", "verification", "MultiEdit", "web/src/app/page.vue", "", true},
		{"locked spec via Write", "verification", "Write", "docs/contracts/BE-039.md", "", true},
		{"control plane allowed", "verification", "Write", ".claude/evidence/x/review-result-1.json", "", false},
		{"report projection allowed", "verification", "Edit", "docs/reports/qa/QA-1.md", "", false},
		{"verification workspace allowed", "verification", "Write", "/tmp/e2e-workspace/spec/flow.spec.ts", workspace, false},
		{"other absolute path denied", "verification", "Write", "/tmp/other/x.ts", workspace, true},
		{"bash never hits this rule", "verification", "Bash", "", "", false},
		{"building stage open", "building", "Edit", "internal/controller/cycle.go", "", false},
		{"bug_resolution frozen (RC-04)", "bug_resolution", "Edit", "internal/controller/cycle.go", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := policy.Input{
				Event:    "PreToolUse",
				ToolName: tc.tool,
				ToolInput: map[string]any{
					"file_path": tc.path,
				},
				Runtime: policy.RuntimeContext{
					CurrentState: tc.state,
					// This matrix exercises lexical surface classification. The
					// filesystem symlink containment path is covered separately
					// with a real ProjectRoot in internal/policy tests.
					ProjectRoot:           "",
					VerificationWorkspace: tc.workspace,
				},
			}
			decision, err := engine.Evaluate(input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if tc.wantBlock && decision.Decision != "deny" {
				t.Fatalf("want deny, got %q (rule=%q)", decision.Decision, decision.RuleID)
			}
			if !tc.wantBlock && (decision.Decision == "deny" || decision.Decision == "block") {
				t.Fatalf("want allow, got %s (%s: %s)", decision.Decision, decision.RuleID, decision.Reason)
			}
		})
	}
}
