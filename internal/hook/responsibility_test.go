package hook_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	loopassignment "github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestResponsibilityExecutionProjection(t *testing.T) {
	for _, tc := range []struct {
		name, state, mode, plan, report, status, ack string
		packet, mayEnd, recorded, waiting            bool
		pending                                      []string
	}{
		{name: "no plan", state: "reading", pending: []string{"PLAN_REPORT", "Result"}},
		{name: "plan only", state: "working", plan: "plan.json", pending: []string{"Result"}},
		{name: "registered result awaits Main", state: "reported", report: "result.json", mayEnd: true, recorded: true, waiting: true},
		{name: "acknowledged result", state: "done", report: "result.json", ack: "ack.json", mayEnd: true, recorded: true},
		{name: "compatibility terminal without evidence", state: "reported", mayEnd: false, pending: []string{"PLAN_REPORT", "Result"}},
		{name: "blocked compatibility without source", state: "blocked", mayEnd: false, pending: []string{"PLAN_REPORT", "Result"}},
		{name: "blocker report status is descriptive", state: "working", status: "blocker_report", mayEnd: false, pending: []string{"PLAN_REPORT", "Result"}},
		{name: "completion report awaits consumer", state: "working", status: "completion_report", mayEnd: true, recorded: true, waiting: true},
		{name: "queued not dispatched", state: "queued", mayEnd: true},
		{name: "one shot", state: "reading", mode: "one_shot", mayEnd: true},
		{name: "approval waits original authority", state: "reading", mode: "plan_approval_required", mayEnd: true},
		{name: "current S5 receiver", state: "working", packet: true, mayEnd: true, recorded: true, waiting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &policy.AgentContext{ID: "owner", State: tc.state, DispatchMode: tc.mode, PlanReportedRef: tc.plan, CompletionReportedRef: tc.report}
			assignment := &hookctx.AssignmentContext{AssignmentID: "assignment", OwnerAgentID: "owner", ReportStatus: tc.status, CompletionAckRef: tc.ack}
			if tc.ack != "" {
				assignment.CompletionRef = "result.json"
				assignment.AcknowledgedReports = map[string]string{"result.json": "report-sha"}
			}
			f := hook.ProjectResponsibility(a, assignment, tc.packet)
			if f.Owner != a.ID || f.AssignmentID != assignment.AssignmentID || f.ExecutionMayEnd != tc.mayEnd || f.DeliveryRecorded != tc.recorded || f.ConsumerPending != tc.waiting || !f.MissingClosingContract {
				t.Fatalf("unexpected facts: %+v", f)
			}
			if tc.recorded && f.ConsumerOwner != "main" || !tc.recorded && f.ConsumerOwner != "" {
				t.Fatalf("invented consumer: %+v", f)
			}
			if len(tc.pending) != len(f.OwnPending) || len(tc.pending) > 0 && !reflect.DeepEqual(f.OwnPending, tc.pending) {
				t.Fatalf("own pending=%v want %v", f.OwnPending, tc.pending)
			}
			assignment.DoneWhen = []string{"original assertion holds"}
			if hook.ProjectResponsibility(a, assignment, tc.packet).MissingClosingContract {
				t.Fatal("declared closing contract was ignored")
			}
		})
	}
	if f := hook.ProjectResponsibility(nil, nil, false); f.Owner != "" || f.ExecutionMayEnd || f.DeliveryRecorded {
		t.Fatalf("unknown owner fabricated facts: %+v", f)
	}
	foreign := &hookctx.AssignmentContext{OwnerAgentID: "peer", CompletionRef: "peer-result.json", ReportStatus: "blocker_report"}
	if f := hook.ProjectResponsibility(&policy.AgentContext{ID: "owner", State: "working"}, foreign, false); f.ExecutionMayEnd || f.DeliveryRecorded || f.AssignmentID != "" {
		t.Fatalf("peer delivery leaked into owner facts: %+v", f)
	}
}

func TestResponsibilityStatusNeedsCurrentOriginalProof(t *testing.T) {
	assignment := &hookctx.AssignmentContext{AssignmentID: "assignment", OwnerAgentID: "owner"}
	cases := []struct {
		name       string
		agent      policy.AgentContext
		proofs     hook.ResponsibilityProofs
		packet     bool
		wantStatus string
	}{
		{name: "active label is not completion", agent: policy.AgentContext{State: "done"}, wantStatus: "active"},
		{name: "generic report remains active", agent: policy.AgentContext{State: "reported", CompletionReportedRef: "result.json"}, wantStatus: "active"},
		{name: "exact original completion acknowledgement completes", agent: policy.AgentContext{State: "done"}, proofs: hook.ResponsibilityProofs{CompletionAcknowledgementAccepted: true}, wantStatus: "done"},
		{name: "current S5 consumer completes", agent: policy.AgentContext{State: "active"}, packet: true, wantStatus: "done"},
		{name: "current S7 consumer completes", agent: policy.AgentContext{State: "active", ReviewAssignment: true}, proofs: hook.ResponsibilityProofs{ReviewResultConsumed: true}, wantStatus: "done"},
		{name: "bare blocked state stays active", agent: policy.AgentContext{State: "blocked"}, wantStatus: "active"},
		{name: "original unresolved blocker projects blocked", agent: policy.AgentContext{State: "blocked", WorkBlockedRef: "blocker.json"}, wantStatus: "blocked"},
		{name: "resolved blocker is no longer current", agent: policy.AgentContext{State: "blocked", WorkBlockedRef: "blocker.json", BlockerResolvedRef: "resolution.json"}, wantStatus: "active"},
		{name: "approved shutdown projects cancelled", agent: policy.AgentContext{State: "cancelled", Activity: "stopped", ShutdownApprovedRef: "shutdown.json"}, wantStatus: "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := tc.agent
			if agent.ID == "" {
				// Responsibility projection intentionally drops Assignment context
				// when the owner binding is absent; this table exercises lifecycle
				// proof semantics for the exact bound owner.
				agent.ID = assignment.OwnerAgentID
			}
			facts := hook.ProjectResponsibilityWithProofs(&agent, assignment, tc.packet, tc.proofs)
			if string(facts.Status) != tc.wantStatus {
				t.Fatalf("status=%q want %q; facts=%+v", facts.Status, tc.wantStatus, facts)
			}
			if facts.CurrentResponsibility != "assignment" || facts.NextAction == "" {
				t.Fatalf("current responsibility/action missing: %+v", facts)
			}
		})
	}
}

func TestS9BlockerEndsOnlyOnCurrentTypedProof(t *testing.T) {
	assignment := &hookctx.AssignmentContext{
		AssignmentID: "repair-assignment-1", OwnerAgentID: "builder-1",
	}
	bare := &policy.AgentContext{
		ID: "builder-1", State: "blocked", Activity: "blocked", LifecycleEvent: "work_blocked",
		DispatchMode: "one_shot", RepairResultAssignment: true, RepairAssignmentID: "repair-assignment-1",
		WorkBlockedRef: "unverified-message.json", CompletionReportedRef: "generic-completion.json",
	}
	withoutProof := hook.ProjectResponsibilityWithProofs(bare, assignment, false, hook.ResponsibilityProofs{})
	if withoutProof.Status != "active" || withoutProof.ExecutionMayEnd || withoutProof.DeliveryRecorded ||
		withoutProof.ConsumerOwner != "" || withoutProof.ConsumerPending {
		t.Fatalf("bare S9 blocked/completion/one-shot facts authorized ending: %+v", withoutProof)
	}

	withProof := hook.ProjectResponsibilityWithProofs(bare, assignment, false, hook.ResponsibilityProofs{
		RepairResultBlockerCurrent: true,
	})
	if withProof.Status != "blocked" || !withProof.ExecutionMayEnd || withProof.DeliveryRecorded ||
		withProof.ConsumerOwner != "main" || !withProof.ConsumerPending || len(withProof.OwnPending) != 0 ||
		withProof.NextAction != "await_consumer" {
		t.Fatalf("current typed S9 blocker did not end only the Worker execution: %+v", withProof)
	}

	resolved := *bare
	resolved.BlockerResolvedRef = "resolution.json"
	resolvedFacts := hook.ProjectResponsibilityWithProofs(&resolved, assignment, false, hook.ResponsibilityProofs{})
	if resolvedFacts.Status != "active" || resolvedFacts.ExecutionMayEnd || resolvedFacts.ConsumerOwner != "" {
		t.Fatalf("resolved S9 history remained a current blocker: %+v", resolvedFacts)
	}
}

func TestS9ResolvedAttemptWaitsForMainWithoutCompletingWorker(t *testing.T) {
	agent := &policy.AgentContext{ID: "builder-1", State: "active", Activity: "working",
		LifecycleEvent: "blocker_resolved", RepairResultAssignment: true, AssignmentID: "assignment-1", RepairAssignmentID: "repair-assignment-1"}
	assignment := &hookctx.AssignmentContext{AssignmentID: "assignment-1", OwnerAgentID: "builder-1"}
	facts := hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{RepairResultResumePending: true})
	if facts.Status != "active" || facts.NextAction != "await_consumer" || facts.ConsumerOwner != "main" ||
		!facts.ConsumerPending || facts.ExecutionMayEnd || facts.DeliveryRecorded || len(facts.OwnPending) != 0 {
		t.Fatalf("resolution revived or completed the old Worker attempt: %+v", facts)
	}
}

func TestCurrentResponsibilityFailsClosedOnOwnerOrAssignmentMismatch(t *testing.T) {
	snapshot := loopruntime.Snapshot{State: map[string]any{}}
	for _, tc := range []struct {
		name       string
		agent      policy.AgentContext
		assignment *hookctx.AssignmentContext
	}{
		{
			name:  "owner mismatch",
			agent: policy.AgentContext{ID: "owner", AssignmentID: "assignment-1", State: "active", Activity: "working"},
			assignment: &hookctx.AssignmentContext{
				AssignmentID: "assignment-1", OwnerAgentID: "peer",
			},
		},
		{
			name:  "assignment mismatch",
			agent: policy.AgentContext{ID: "owner", AssignmentID: "assignment-1", State: "active", Activity: "working"},
			assignment: &hookctx.AssignmentContext{
				AssignmentID: "assignment-2", OwnerAgentID: "owner",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := hook.CurrentResponsibility(t.TempDir(), snapshot, &tc.agent, tc.assignment, true)
			if facts.ExecutionMayEnd || facts.DeliveryRecorded || facts.ConsumerOwner != "" {
				t.Fatalf("mismatched responsibility produced completion facts: %+v", facts)
			}
			if facts.Status != "active" || facts.NextAction == "" {
				t.Fatalf("mismatched responsibility lost its shared status projection: %+v", facts)
			}
		})
	}
}

func TestS6ResponsibilityDoneRequiresCurrentBuilderReceipt(t *testing.T) {
	agent := &policy.AgentContext{ID: "builder-1", State: "done"}
	assignment := &hookctx.AssignmentContext{
		AssignmentID: "assignment-1", OwnerAgentID: agent.ID,
		RoleFamily: "backend-builder", CompletionRef: "result.json", CompletionAckRef: "ack.json",
		AcknowledgedReports: map[string]string{"result.json": strings.Repeat("a", 64)},
	}
	facts := hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{CompletionAcknowledgementAccepted: true})
	if facts.Status != "active" {
		t.Fatalf("generic acknowledgement replaced the current S6 Builder receipt: %+v", facts)
	}
	facts = hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{BuilderWorkComplete: true})
	if facts.Status != "done" {
		t.Fatalf("current Builder work-complete receipt did not project done: %+v", facts)
	}
}

func TestS7ReviewerCompletionNotificationDoesNotRecordDelivery(t *testing.T) {
	agent := &policy.AgentContext{
		ID: "reviewer-1", State: "working", ReviewAssignment: true,
		DispatchMode:          "one_shot",
		CompletionReportedRef: "completion-message.json",
	}
	assignment := &hookctx.AssignmentContext{
		AssignmentID: "assignment-qa-1", OwnerAgentID: agent.ID,
		ReportStatus: "completion_report", CompletionRef: "completion-message.json",
	}
	facts := hook.ProjectResponsibility(agent, assignment, false)
	if facts.DeliveryRecorded || facts.ExecutionMayEnd || facts.ConsumerOwner != "" || facts.ConsumerPending {
		t.Fatalf("generic completion notification satisfied an S7 ReviewAssignment: %+v", facts)
	}
}

func TestS8InvestigatorRequiresOriginalReceiptForCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, state, mode string
	}{
		{name: "generic completion report", state: "working"},
		{name: "reported terminal label", state: "reported"},
		{name: "done terminal label", state: "done"},
		{name: "completed terminal label", state: "completed"},
		{name: "closed terminal label", state: "closed"},
		{name: "one shot cannot replace packet receipt", state: "working", mode: "one_shot"},
		{name: "approval dispatch cannot replace packet receipt", state: "working", mode: "plan_approval_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &policy.AgentContext{
				ID: "investigator-1", AssignmentID: "assignment-investigator-1", State: tc.state,
				DispatchMode: tc.mode, HypothesisResultAssignment: true,
				CompletionReportedRef: "completion-message.json",
			}
			assignment := &hookctx.AssignmentContext{
				AssignmentID: agent.AssignmentID, OwnerAgentID: agent.ID,
				ReportStatus: "completion_report", CompletionRef: "completion-message.json",
			}
			facts := hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{})
			if facts.DeliveryRecorded || facts.ExecutionMayEnd || facts.ConsumerOwner != "" {
				t.Fatalf("generic completion or dispatch mode satisfied an issued S8 question: %+v", facts)
			}
		})
	}

	agent := &policy.AgentContext{
		ID: "investigator-1", AssignmentID: "assignment-investigator-1", State: "working",
		HypothesisResultAssignment: true,
	}
	assignment := &hookctx.AssignmentContext{AssignmentID: agent.AssignmentID, OwnerAgentID: agent.ID}
	for _, proof := range []bool{false, true} {
		facts := hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{HypothesisResultConsumed: proof})
		if facts.DeliveryRecorded != proof || facts.ExecutionMayEnd != proof || facts.ConsumerOwner != map[bool]string{true: "main", false: ""}[proof] {
			t.Fatalf("consumption proof=%t produced incorrect S8 responsibility: %+v", proof, facts)
		}
	}
}

func TestS9BuilderNeedsCandidateAndCurrentOwnerChecks(t *testing.T) {
	for _, tc := range []struct {
		name, state, mode string
	}{
		{name: "generic completion report", state: "working"},
		{name: "reported terminal label", state: "reported"},
		{name: "done terminal label", state: "done"},
		{name: "blocked terminal label", state: "blocked"},
		{name: "one shot cannot replace candidate", state: "working", mode: "one_shot"},
		{name: "approval dispatch cannot replace candidate", state: "working", mode: "plan_approval_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &policy.AgentContext{
				ID: "builder-1", AssignmentID: "manifest-a", RepairAssignmentID: "repair-a",
				State: tc.state, DispatchMode: tc.mode, RepairResultAssignment: true,
				CompletionReportedRef: "completion-message.json",
			}
			assignment := &hookctx.AssignmentContext{AssignmentID: "manifest-a", OwnerAgentID: agent.ID, ReportStatus: "completion_report"}
			facts := hook.ProjectResponsibilityWithProofs(agent, assignment, true, hook.ResponsibilityProofs{})
			if facts.DeliveryRecorded || facts.ExecutionMayEnd || facts.ConsumerOwner != "" {
				t.Fatalf("generic completion or dispatch mode satisfied S9 responsibility: %+v", facts)
			}
		})
	}
	agent := &policy.AgentContext{ID: "builder-1", AssignmentID: "manifest-a", RepairAssignmentID: "repair-a", State: "reported", RepairResultAssignment: true}
	assignment := &hookctx.AssignmentContext{AssignmentID: "manifest-a", OwnerAgentID: agent.ID}
	facts := hook.ProjectResponsibilityWithProofs(agent, assignment, false, hook.ResponsibilityProofs{RepairResultWorkComplete: true})
	if !facts.DeliveryRecorded || !facts.ExecutionMayEnd || !facts.ConsumerPending || facts.ConsumerOwner != "main" {
		t.Fatalf("complete candidate plus owner checks must end worker responsibility while Main remains pending: %+v", facts)
	}
}

func TestS7ManifestBlockerStatusIsNotAuthority(t *testing.T) {
	agent := &policy.AgentContext{ID: "reviewer-1", State: "working", ReviewAssignment: true}
	assignment := &hookctx.AssignmentContext{
		AssignmentID: "assignment-qa-1", OwnerAgentID: agent.ID,
		ReportStatus: "blocker_report", CompletionRef: "unverified-blocker.json",
	}
	facts := hook.ProjectResponsibility(agent, assignment, false, false, false)
	if facts.ExecutionMayEnd || facts.DeliveryRecorded {
		t.Fatalf("S7 manifest status authorized ending without a validated source: %+v", facts)
	}
}

func TestProfessionalQuestionnaireAssignmentsDoNotRequireGenericPlan(t *testing.T) {
	cases := []struct {
		name  string
		agent policy.AgentContext
	}{
		{name: "S5 document review", agent: policy.AgentContext{ID: "reviewer", State: "reading", DocumentReviewAssignment: true}},
		{name: "S7 review result", agent: policy.AgentContext{ID: "reviewer", State: "reading", ReviewAssignment: true}},
		{name: "S8 hypothesis result", agent: policy.AgentContext{ID: "investigator", State: "reading", HypothesisResultAssignment: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := hook.ProjectResponsibilityWithProofs(&tc.agent, nil, false, hook.ResponsibilityProofs{})
			for _, pending := range facts.OwnPending {
				if pending == "PLAN_REPORT" {
					t.Fatalf("professional answer assignment retained generic plan prerequisite: %+v", facts)
				}
			}
		})
	}
}

func TestConsumedS7ReviewResultCompletesReviewerResponsibility(t *testing.T) {
	root, state, agent := canonicalReviewerReceiptFixture(t)
	// The persisted legacy S7 row is still `consumed`, while the shared Agent
	// context exposes its five-state projection plus the original activity.
	agent.State, agent.Activity = "active", "reported"
	if !hook.ReviewResultConsumed(root, state, agent) {
		t.Fatal("exact current consumed ReviewResult was not recognized")
	}
	assignment := &hookctx.AssignmentContext{
		AssignmentID: agent.AssignmentID, OwnerAgentID: agent.ID, TaskID: "TASK-007",
	}
	facts := hook.CurrentResponsibility(root, loopruntime.Snapshot{State: state}, agent, assignment, false)
	if !facts.DeliveryRecorded || !facts.ExecutionMayEnd || facts.ConsumerPending || facts.ConsumerOwner != "main" {
		t.Fatalf("consumed S7 Result did not complete the responsibility: %+v", facts)
	}
}

func TestS7ReviewResultReceiptRejectsMismatchAndStaleState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "assignment result reference mismatch", mutate: func(state map[string]any) {
			reviewState := state["review"].(map[string]any)
			row := reviewState["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
			row["result_ref"] = ".claude/evidence/loop-s7-test/g7/reviews/reviewer-1/other.json"
		}},
		{name: "result owner mismatch", mutate: func(state map[string]any) {
			reviewState := state["review"].(map[string]any)
			reviewState["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)["agent_id"] = "reviewer-2"
		}},
		{name: "dispatched task set mismatch", mutate: func(state map[string]any) {
			entities := state["entities"].(map[string]any)
			entities["agents"].([]any)[0].(map[string]any)["task_ids"] = []any{"TASK-OTHER"}
		}},
		{name: "duplicate id at same path under another kind", mutate: func(state map[string]any) {
			row := state["evidence"].([]any)[0].(map[string]any)
			duplicate := map[string]any{}
			for key, value := range row {
				duplicate[key] = value
			}
			duplicate["kind"] = "completion_report"
			state["evidence"] = append(state["evidence"].([]any), duplicate)
		}},
		{name: "duplicate id at another path", mutate: func(state map[string]any) {
			row := state["evidence"].([]any)[0].(map[string]any)
			duplicate := map[string]any{}
			for key, value := range row {
				duplicate[key] = value
			}
			duplicate["path"] = ".claude/evidence/other.json"
			state["evidence"] = append(state["evidence"].([]any), duplicate)
		}},
		{name: "old review round", mutate: func(state map[string]any) {
			state["review"].(map[string]any)["round"] = 4
		}},
		{name: "old baseline generation", mutate: func(state map[string]any) {
			state["baseline"].(map[string]any)["generation"] = 8
		}},
		{name: "invalidated evidence", mutate: func(state map[string]any) {
			state["evidence"].([]any)[0].(map[string]any)["status"] = "invalid"
		}},
		{name: "valid status retains invalidation marker", mutate: func(state map[string]any) {
			state["evidence"].([]any)[0].(map[string]any)["invalidated_by"] = "EV-OLD"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, state, agent := canonicalReviewerReceiptFixture(t)
			tc.mutate(state)
			if hook.ReviewResultConsumed(root, state, agent) {
				t.Fatal("mismatched or stale ReviewResult satisfied the current assignment")
			}
		})
	}
}

func TestCompletedReviewerWaitingForIdleUsesConsumedReceipt(t *testing.T) {
	root := writeCanonicalReviewerRuntime(t)
	decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
	if controlled {
		t.Fatalf("the completed Reviewer must be allowed to idle, got %#v", decision)
	}

	root = writeCanonicalReviewerRuntime(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{}
	state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)["status"] = "dispatched"
	state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)["result_ref"] = nil
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	decision, controlled = hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
	if !controlled || decision.Decision != "deny" {
		t.Fatalf("reported Reviewer without a consumed ReviewResult must remain responsible, got %#v controlled=%v", decision, controlled)
	}
}

func TestStopIdleS7OneShotAndManifestBlockerNeedCanonicalProof(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mode           string
		manifestStatus string
	}{
		{name: "one shot dispatch", mode: "one_shot", manifestStatus: "active"},
		{name: "manifest blocker status", mode: "plan_checkpoint", manifestStatus: "blocker_report"},
		{name: "manifest blocked status", mode: "plan_checkpoint", manifestStatus: "blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeCanonicalReviewerRuntime(t)
			editCanonicalReviewerRuntime(t, root, func(state, manifest map[string]any) {
				unconsumeCanonicalReviewer(state)
				state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["dispatch_mode"] = tc.mode
				manifest["assignments"].([]any)[0].(map[string]any)["dispatch_mode"] = tc.mode
				manifest["assignments"].([]any)[0].(map[string]any)["status"] = tc.manifestStatus
			})
			decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
			if !controlled || decision.Decision != "deny" {
				t.Fatalf("unconsumed S7 assignment bypassed the Result requirement: %#v controlled=%v", decision, controlled)
			}
		})
	}
}

func TestStopIdleBoundS7OwnerFailsClosedOnMissingOrMismatchedContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{name: "missing manifest", mutate: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, ".claude/workgroups/REQ-001/team-review/manifest.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "manifest names another owner", mutate: func(t *testing.T, root string) {
			editCanonicalReviewerRuntime(t, root, func(_, manifest map[string]any) {
				manifest["assignments"].([]any)[0].(map[string]any)["agent_id"] = "reviewer-other"
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeCanonicalReviewerRuntime(t)
			editCanonicalReviewerRuntime(t, root, func(state, _ map[string]any) { unconsumeCanonicalReviewer(state) })
			tc.mutate(t, root)
			decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
			if !controlled || decision.Decision != "deny" {
				t.Fatalf("known Runtime-bound S7 owner with unusable context was not blocked: %#v controlled=%v", decision, controlled)
			}
			if decision.Reason == "" || !containsHookTestText(decision.Reason, "cannot be verified") {
				t.Fatalf("binding failure lacks an actionable reason: %#v", decision)
			}
			if _, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "not-registered"}); controlled {
				t.Fatal("unknown Agent must preserve the original fail-open behavior")
			}
		})
	}
}

func TestStopIdleAcceptsOnlyOriginalOrCanonicalS7Blocker(t *testing.T) {
	t.Run("original work_blocked event", func(t *testing.T) {
		root := writeOriginalS7WorkBlockedRuntime(t)
		decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
		if controlled {
			t.Fatalf("validated original blocker should let the same Reviewer idle: %#v", decision)
		}
	})
	t.Run("site lost blocker", func(t *testing.T) {
		root := writeSiteLostS7BlockerRuntime(t)
		decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
		if controlled {
			t.Fatalf("validated site_lost blocker should let the same Reviewer idle: %#v", decision)
		}
	})
	t.Run("blocked status without source", func(t *testing.T) {
		root := writeCanonicalReviewerRuntime(t)
		editCanonicalReviewerRuntime(t, root, func(state, manifest map[string]any) {
			unconsumeCanonicalReviewer(state)
			state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["state"] = "blocked"
			manifest["assignments"].([]any)[0].(map[string]any)["status"] = "blocked"
		})
		decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
		if !controlled || decision.Decision != "deny" {
			t.Fatalf("bare Agent/manifest blocked status authorized ending: %#v controlled=%v", decision, controlled)
		}
	})
	t.Run("site lost index without source file", func(t *testing.T) {
		root := writeSiteLostS7BlockerRuntime(t)
		blockerRef := ".claude/evidence/loop-s7-test/g7/review-blockers/review-result-site-lost-site-lost.json"
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(blockerRef))); err != nil {
			t.Fatal(err)
		}
		decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
		if !controlled || decision.Decision != "deny" {
			t.Fatalf("missing site_lost source file authorized ending: %#v controlled=%v", decision, controlled)
		}
	})
}

func TestStopIdleSiteLostBlockerPreservesActiveRoundStatuses(t *testing.T) {
	for _, tc := range []struct {
		status  string
		allowed bool
	}{
		{status: "running", allowed: true},
		{status: "cannot_clean", allowed: true},
		{status: "discovery_draining", allowed: true},
		{status: "stale"},
		{status: "observation_sealed"},
		{status: "clean"},
		{status: "paused"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			root := writeSiteLostS7BlockerRuntime(t)
			editCanonicalReviewerRuntime(t, root, func(state, _ map[string]any) {
				state["review"].(map[string]any)["plan"].(map[string]any)["status"] = tc.status
			})
			decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: "TeammateIdle", AgentID: "reviewer-1"})
			if tc.allowed && controlled {
				t.Fatalf("site_lost blocker stopped being valid while the round accepts results: %#v", decision)
			}
			if !tc.allowed && (!controlled || decision.Decision != "deny") {
				t.Fatalf("site_lost blocker authorized ending for inactive plan status %q: %#v controlled=%v", tc.status, decision, controlled)
			}
		})
	}
}

func canonicalReviewerReceiptFixture(t *testing.T) (string, map[string]any, *policy.AgentContext) {
	t.Helper()
	root := t.TempDir()
	plan := &review.Plan{
		SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-r3", ReviewRound: 3,
		BaselineGeneration: 7,
		FrozenSubjects:     []review.FrozenSubject{{Path: "internal/example.go", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		Assignments:        []review.PlanAssignment{{AssignmentID: "assignment-qa-1", Lens: "qa", ClaimIDs: []string{"claim-qa-1"}}},
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planSHA := sha256HexForHookTest(planBytes)
	planRef := "docs/review-plan.json"
	if err := writeHookTestArtifact(root, planRef, planBytes); err != nil {
		t.Fatal(err)
	}
	resultID := "review-result-qa-1"
	resultRef := filepath.ToSlash(filepath.Join(".claude", "evidence", "loop-s7-test", "g7", "reviews", "reviewer-1", resultID+".json"))
	resultEnvelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": resultID, "kind": "review_result",
		"runtime_id": "loop-s7-test", "baseline_generation": 7, "review_round": 3,
		"producer_agent_id": "reviewer-1", "producer_responsibility": "QA",
		"review_plan_id": plan.ReviewPlanID, "assignment_id": "assignment-qa-1",
		"assignment_revision": 2, "subject_digest": review.SubjectDigest(plan),
		"claim_results":  []any{map[string]any{"claim_id": "claim-qa-1", "conclusion": "pass"}},
		"blocked_claims": []any{}, "verdict": "pass",
	}
	resultBytes, err := json.Marshal(resultEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	resultSHA := sha256HexForHookTest(resultBytes)
	if err := writeHookTestArtifact(root, resultRef, resultBytes); err != nil {
		t.Fatal(err)
	}

	state := map[string]any{
		"runtime_id": "loop-s7-test",
		"baseline":   map[string]any{"generation": 7},
		"review": map[string]any{
			"round": 3,
			"plan": map[string]any{
				"plan_id": plan.ReviewPlanID, "path": planRef, "sha256": planSHA,
				"revision": 2, "review_round": 3, "status": "running",
				"e2e_coverage_state": "cold_start", "submitted_at": "2026-10-14T00:00:00Z",
			},
			"assignments": map[string]any{
				"assignment-qa-1": map[string]any{
					"lens": "qa", "claim_ids": []any{"claim-qa-1"}, "status": "consumed",
					"agent_id": "reviewer-1", "result_ref": resultRef,
				},
			},
			"claims": map[string]any{
				"claim-qa-1": map[string]any{
					"lens": "qa", "applicability": "required", "disposition": "pass",
					"assignment_id": "assignment-qa-1", "result_id": resultID,
				},
			},
		},
		"evidence": []any{map[string]any{
			"id": resultID, "kind": "review_result", "path": resultRef, "sha256": resultSHA,
			"status": "valid", "baseline_generation": 7, "review_round": 3,
			"produced_by": []any{"reviewer-1"}, "invalidated_by": nil,
			"invalidation_rule": nil, "invalidation_reason": nil,
			"responsibility_id": "QA", "scope_refs": []any{},
		}},
		"entities": map[string]any{
			"agents": []any{map[string]any{
				"id": "reviewer-1", "state": "reported", "dispatch_mode": "plan_checkpoint",
				"task_ids": []any{"TASK-007"},
			}},
		},
	}
	agent := &policy.AgentContext{
		ID: "reviewer-1", State: "reported", DispatchMode: "plan_checkpoint",
		AssignmentID: "assignment-qa-1", ReviewAssignment: true, TaskIDs: []string{"TASK-007"},
	}
	return root, state, agent
}

func writeCanonicalReviewerRuntime(t *testing.T) string {
	t.Helper()
	root, receiptState, agent := canonicalReviewerReceiptFixture(t)
	baseData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(baseData, &state); err != nil {
		t.Fatal(err)
	}
	// Recreate the exact current S7 projection in the original Runtime state
	// shape consumed by Runtime.Store and hookctx.LoadFull.
	for key, value := range receiptState {
		state[key] = value
	}
	state["baseline"].(map[string]any)["captured_at"] = "2026-10-14T00:00:00Z"
	state["review"].(map[string]any)["clean_round"] = nil
	state["revision"] = 0
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "running", "phase_revision": 1}
	state["documents"] = []any{}
	state["blockers"] = []any{}
	state["pause"] = nil
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["last_transition"] = nil
	state["updated_at"] = "2026-10-14T00:00:00Z"
	manifestRef := ".claude/workgroups/REQ-001/team-review/manifest.json"
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": agent.ID, "role": "qa", "state": agent.State,
			"task_ids": []any{"TASK-007"}, "team_id": "team-review",
			"definition_ref": ".claude/agents/qa.md", "prompt_ref": manifestRef,
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"dispatch_mode": "plan_checkpoint", "plan_reported_ref": nil,
			"completion_reported_ref": "generic-completion.json", "updated_at": "2026-10-14T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-007", "state": "review", "path": "docs/dev/tasks/TASK-007.md",
			"sha256":          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"owner_agent_ids": []any{agent.ID},
		}},
		"bugs": []any{},
		"teams": []any{map[string]any{
			"id": "team-review", "platform_team_id": "platform-review", "kind": "qa", "status": "active",
			"manifest_ref": manifestRef, "responsibility_ids": []any{"QA"},
			"agent_ids": []any{agent.ID}, "review_round": 3,
		}},
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-review-manifest", "version": "1.0.0",
		"runtime_id": "loop-s7-test", "req_id": "REQ-001", "baseline_generation": 7,
		"status": "active", "workgroup_id": "team-review", "workgroup_kind": "qa",
		"assignments": []any{map[string]any{
			"assignment_id": agent.AssignmentID, "responsibility_id": "QA", "role_family": "qa",
			"agent_id": agent.ID, "agent_definition_ref": ".claude/agents/qa.md",
			"dispatch_mode": "plan_checkpoint", "skill_refs": []any{}, "read_paths": []any{},
			"depends_on": []any{}, "scope": []any{}, "write_paths": []any{}, "output_paths": []any{},
			"required_checks": []any{}, "done_when": []any{"submit one Canonical ReviewResult"}, "status": "active",
		}},
		"documents": []any{}, "responsibility_dispositions": []any{},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHookTestArtifact(root, manifestRef, manifestBytes); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func editCanonicalReviewerRuntime(t *testing.T, root string, edit func(state, manifest map[string]any)) {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".claude/workgroups/REQ-001/team-review/manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	edit(state, manifest)
	stateBytes, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

func unconsumeCanonicalReviewer(state map[string]any) {
	reviewState := state["review"].(map[string]any)
	row := reviewState["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	row["status"] = "dispatched"
	row["result_ref"] = nil
	claim := reviewState["claims"].(map[string]any)["claim-qa-1"].(map[string]any)
	claim["disposition"] = "running"
	claim["result_id"] = nil
	state["evidence"] = []any{}
}

// writeOriginalS7WorkBlockedRuntime drives the real original `runtime agent-event
// --event work_blocked` Writer path so the acceptance fixture contains the
// schema-validated message ref, Agent projection and journal binding together.
func writeOriginalS7WorkBlockedRuntime(t *testing.T) string {
	t.Helper()
	root := writeCanonicalReviewerRuntime(t)
	editCanonicalReviewerRuntime(t, root, func(state, _ map[string]any) {
		unconsumeCanonicalReviewer(state)
		state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["state"] = "working"
	})
	messageRef := ".claude/evidence/loop-s7-test/g7/messages/reviewer-1-work-blocked.json"
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "blocker_report",
		"message_id": "msg-reviewer-1-blocked", "correlation_id": "corr-reviewer-1-blocked",
		"runtime_id": "loop-s7-test", "agent_id": "reviewer-1",
		"agent_definition_ref": ".claude/agents/qa.md", "task_id": "TASK-007",
		"bug_id": nil, "team_id": "team-review", "occurred_at": "2026-10-14T00:00:01Z",
		"body": "The assigned test environment is unavailable.",
	}
	messageBytes, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHookTestArtifact(root, messageRef, messageBytes); err != nil {
		t.Fatal(err)
	}
	writeHookTestLoopDefinition(t, root)
	_, err = loopassignment.AdvanceAgent(
		root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		loopassignment.AgentEventRequest{
			ExpectedRevision: 0, AgentID: "reviewer-1", Event: "work_blocked",
			MessagePath: filepath.Join(root, filepath.FromSlash(messageRef)),
			OccurredAt:  time.Date(2026, 10, 14, 0, 0, 1, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("commit original work_blocked lifecycle event: %v", err)
	}
	return root
}

// writeSiteLostS7BlockerRuntime builds the exact S7 site_lost state and
// journal transaction shape. The full SubmitResult readiness path is covered
// by review's own integration tests; this fixture isolates the hook consumer.
func writeSiteLostS7BlockerRuntime(t *testing.T) string {
	t.Helper()
	root := writeCanonicalReviewerRuntime(t)
	writeHookTestLoopDefinition(t, root)
	editCanonicalReviewerRuntime(t, root, func(state, _ map[string]any) {
		unconsumeCanonicalReviewer(state)
		state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["state"] = "working"
	})
	const (
		resultID   = "review-result-site-lost"
		evidenceID = "review-blocker-review-result-site-lost"
		assignment = "assignment-qa-1"
		findingID  = "finding-qa-1"
		reason     = "the environment was destroyed and cannot be recaptured"
		blockedAt  = "2026-10-14T00:00:02Z"
	)
	blockerRef := ".claude/evidence/loop-s7-test/g7/review-blockers/review-result-site-lost-site-lost.json"
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": evidenceID, "kind": "review_blocker",
		"runtime_id": "loop-s7-test", "baseline_generation": 7, "review_round": 3,
		"assignment_id": assignment, "producer_agent_id": "reviewer-1",
		"finding_id": findingID, "reason": reason, "created_at": blockedAt,
	}
	artifactBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHookTestArtifact(root, blockerRef, artifactBytes); err != nil {
		t.Fatal(err)
	}
	artifactSHA := sha256HexForHookTest(artifactBytes)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	lifecycle := state["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt, err := time.Parse(time.RFC3339Nano, blockedAt)
	if err != nil {
		t.Fatal(err)
	}
	mutation := loopruntime.Mutation{
		EventID: "evt-review-site-lost-review-result-site-lost-r1", TransitionID: "REVIEW-RESULT",
		Event: "review_assignment_blocked", Actor: "orchestrator",
		IdempotencyKey: "runtime:review-site-lost:review-result-site-lost:0",
		RuntimeID:      "loop-s7-test", From: cursor, To: cursor,
		EvidenceIDs: []string{evidenceID}, OccurredAt: occurredAt, BaselineGeneration: 7,
		Message: fmt.Sprintf("Assignment %s blocked in S7: finding %s scene unrecoverable (%s); the result is not consumed and the round is not sealed", assignment, findingID, reason),
		Apply: func(state map[string]any) error {
			reviewState := state["review"].(map[string]any)
			row := reviewState["assignments"].(map[string]any)[assignment].(map[string]any)
			row["status"] = "blocked"
			row["queue_reason"] = "site_lost:" + findingID
			row["blocker_ref"] = blockerRef
			row["blocked_at"] = blockedAt
			agent := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
			agent["state"] = "blocked"
			agent["work_blocked_ref"] = blockerRef
			agent["blocker_resolved_ref"] = nil
			agent["updated_at"] = blockedAt
			state["evidence"] = []any{map[string]any{
				"id": evidenceID, "kind": "review_blocker", "path": blockerRef, "sha256": artifactSHA,
				"status": "valid", "baseline_generation": 7, "review_round": 3,
				"produced_by": []any{"reviewer-1"}, "invalidated_by": nil,
				"invalidation_rule": nil, "invalidation_reason": nil,
				"responsibility_id": "QA", "scope_refs": []any{assignment},
			}}
			return nil
		},
	}
	store := loopruntime.NewWriter(
		statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), root,
		semantic.RuntimeCandidateValidator{},
	)
	if _, err := store.Update(0, mutation); err != nil {
		t.Fatalf("commit exact site_lost Runtime transaction shape: %v", err)
	}
	return root
}

func containsHookTestText(value, needle string) bool {
	return strings.Contains(value, needle)
}

func writeHookTestLoopDefinition(t *testing.T, root string) {
	t.Helper()
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHookTestArtifact(root, "docs/control/loop-definition.json", definition); err != nil {
		t.Fatal(err)
	}
}

func writeHookTestArtifact(root, ref string, data []byte) error {
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sha256HexForHookTest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
