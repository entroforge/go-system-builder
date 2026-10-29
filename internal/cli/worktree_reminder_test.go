package cli

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorktreeIntegrationReminderTargetsCompletedBuilderWork(t *testing.T) {
	registry := &workspace.ExecutionRegistry{Executions: map[string]workspace.Execution{
		"s5-review": {
			AssignmentID: "s5-review", RuntimeID: "runtime-current", BaselineGeneration: 4,
			Purpose: workspace.DocumentReviewPurpose,
		},
	}}
	cases := []struct {
		name       string
		assignment hookctx.AssignmentContext
		want       bool
	}{
		{
			name: "S5 document review execution",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "s5-review", RoleFamily: "backend-builder",
				ResponsibilityIDs: []string{"BUILD-WORK-PACKAGE"}, CompletionRef: "result-ref",
			},
			want: false,
		},
		{
			name: "S5 document verifier",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "s5-native", RoleFamily: "document-verifier",
				ResponsibilityIDs: []string{"DV-SPEC-CONSISTENCY"}, CompletionRef: "result-ref",
			},
			want: false,
		},
		{
			name: "S7 QA review",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "s7-qa", RoleFamily: "qa",
				ResponsibilityIDs: []string{"QA-MODULE-CODE"}, CompletionRef: "result-ref",
			},
			want: false,
		},
		{
			name: "S8 investigation",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "s8-investigation", RoleFamily: "investigator",
				ResponsibilityIDs: []string{"S8-CAUSAL-INVESTIGATION"}, CompletionRef: "result-ref",
			},
			want: false,
		},
		{
			name: "active Builder has no Result yet",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "builder-planned", RoleFamily: "backend-builder",
				ResponsibilityIDs: []string{"BUILD-WORK-PACKAGE"}, ReportStatus: "planned",
			},
			want: false,
		},
		{
			name: "blocked Builder is not ready for integration",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "builder-blocked", RoleFamily: "backend-builder",
				ResponsibilityIDs: []string{"BUILD-WORK-PACKAGE"}, State: "blocked", CompletionRef: "prior-result-ref",
			},
			want: false,
		},
		{
			name: "active Builder with recorded Result keeps Main integration",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "builder-reported", RoleFamily: "backend-builder",
				ResponsibilityIDs: []string{"BUILD-WORK-PACKAGE"}, State: "active", CompletionRef: "result-ref",
			},
			want: true,
		},
		{
			name: "completed Builder retains Main integration",
			assignment: hookctx.AssignmentContext{
				AssignmentID: "builder-complete", RoleFamily: "backend-builder",
				ResponsibilityIDs: []string{"BUILD-WORK-PACKAGE"}, State: "done", CompletionRef: "result-ref",
				WorktreePath: "/project/.worktrees/builder-complete",
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRemindWorktreeIntegration(tc.assignment, registry, "runtime-current", 4)
			if got != tc.want {
				t.Fatalf("shouldRemindWorktreeIntegration() = %t, want %t", got, tc.want)
			}
		})
	}

	fact := worktreeIntegrationFact(cases[len(cases)-1].assignment)
	if !strings.Contains(fact, "runtime task-integrate --assignment-id builder-complete") {
		t.Fatalf("completed Builder lost Main integration action: %s", fact)
	}
	if strings.Contains(fact, "report=planned") || strings.Contains(fact, "state=") {
		t.Fatalf("reminder exposed machine status fields: %s", fact)
	}
}
