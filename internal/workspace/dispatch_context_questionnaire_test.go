package workspace

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func TestWorkerPlanDraftOnlyProjectsForAssignmentsThatNeedGenericPlan(t *testing.T) {
	base := agentcontext.Assignment{
		AssignmentID: "assignment-example", Owner: "agent-example", TaskID: "TASK-example",
		TeamID: "team-example", Role: "backend-builder", AgentDefinitionRef: "agents/backend-builder.md",
		DispatchMode: "plan_checkpoint",
	}

	draft, questions := workerPlanDraft(Execution{}, base, "loop-example", "2026-10-15T00:00:00Z", []string{"src/example.go"})
	if draft == nil || questions == nil {
		t.Fatal("S6 Builder plan_checkpoint must retain its professional PLAN draft")
	}

	for _, role := range []string{"document-verifier", "delivery-verifier", "qa", "e2e-tester", "investigator"} {
		t.Run(role, func(t *testing.T) {
			dispatch := base
			dispatch.Role = role
			if draft, questions := workerPlanDraft(Execution{}, dispatch, "loop-example", "2026-10-15T00:00:00Z", nil); draft != nil || questions != nil {
				t.Fatalf("%s domain questionnaire must not project a duplicate generic PLAN: draft=%v questions=%v", role, draft, questions)
			}
		})
	}

	documentReview := base
	documentReview.Role = "custom-reviewer"
	if draft, questions := workerPlanDraft(Execution{Purpose: DocumentReviewPurpose}, documentReview, "loop-example", "2026-10-15T00:00:00Z", nil); draft != nil || questions != nil {
		t.Fatalf("issued S5 document review must not project a generic PLAN: draft=%v questions=%v", draft, questions)
	}

	approval := base
	approval.DispatchMode = "plan_approval_required"
	if draft, questions := workerPlanDraft(Execution{}, approval, "loop-example", "2026-10-15T00:00:00Z", nil); draft != nil || questions != nil {
		t.Fatalf("explicit approval must not be replaced by an unanswered generic draft: draft=%v questions=%v", draft, questions)
	}
}
