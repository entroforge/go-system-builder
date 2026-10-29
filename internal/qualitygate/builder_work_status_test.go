package qualitygate_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestCurrentBuilderReceiptProjectsOriginalBlockerAndCancellation(t *testing.T) {
	cases := []struct {
		name       string
		agentState string
		agentEvent string
		taskState  string
		taskEvent  string
		mutate     func(agent, task map[string]any)
		want       workstate.Status
	}{
		{
			name:       "agent work blocker",
			agentState: "blocked", agentEvent: "work_blocked",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, _ map[string]any) { agent["work_blocked_ref"] = "evidence/blocker.json" },
			want:   workstate.Blocked,
		},
		{
			name:       "document conflict",
			agentState: "blocked", agentEvent: "document_conflict_reported",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, _ map[string]any) { agent["readback_ref"] = "evidence/conflict.json" },
			want:   workstate.Blocked,
		},
		{
			name:       "task blocker",
			agentState: "active", agentEvent: "work_started",
			taskState: "blocked", taskEvent: "task_blocked",
			mutate: func(_, task map[string]any) { task["blocker_evidence"] = "evidence/task-blocker.json" },
			want:   workstate.Blocked,
		},
		{
			name:       "agent shutdown",
			agentState: "cancelled", agentEvent: "shutdown_approved",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, _ map[string]any) { agent["shutdown_approved_ref"] = "evidence/shutdown.json" },
			want:   workstate.Cancelled,
		},
		{
			name:       "task cancellation",
			agentState: "active", agentEvent: "work_started",
			taskState: "cancelled", taskEvent: "task_cancelled",
			mutate: func(_, task map[string]any) { task["cancellation_reason"] = "scope withdrawn" },
			want:   workstate.Cancelled,
		},
		{
			name:       "assignment replacement after ownership transfer",
			agentState: "cancelled", agentEvent: "assignment_revision_issued",
			taskState: "pending", taskEvent: "assignment_revision_issued",
			mutate: func(_, task map[string]any) { task["owner_agent_ids"] = []any{"peer"} },
			want:   workstate.Cancelled,
		},
		{
			name:       "done entity still needs the original consumer result",
			agentState: "done", agentEvent: "completion_acknowledged",
			taskState: "done", taskEvent: "task_closing_contract_passed",
			want: workstate.Active,
		},
		{
			name:       "blocked label without original blocker reference",
			agentState: "blocked", agentEvent: "work_blocked",
			taskState: "active", taskEvent: "builder_activated",
			want: workstate.Active,
		},
		{
			name:       "shutdown label without approval reference",
			agentState: "cancelled", agentEvent: "shutdown_approved",
			taskState: "active", taskEvent: "builder_activated",
			want: workstate.Active,
		},
		{
			name:       "task blocker without original evidence",
			agentState: "active", agentEvent: "work_started",
			taskState: "blocked", taskEvent: "task_blocked",
			want: workstate.Active,
		},
		{
			name:       "wrong owner cannot project responsibility facts",
			agentState: "blocked", agentEvent: "work_blocked",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, task map[string]any) {
				agent["work_blocked_ref"] = "evidence/blocker.json"
				task["owner_agent_ids"] = []any{"peer"}
			},
			want: workstate.Pending,
		},
		{
			name:       "wrong assignment cannot project responsibility facts",
			agentState: "blocked", agentEvent: "work_blocked",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, _ map[string]any) {
				agent["work_blocked_ref"] = "evidence/blocker.json"
				agent["prompt_ref"] = "manifest#other-assignment"
			},
			want: workstate.Pending,
		},
		{
			name:       "resolved blocker is no longer current",
			agentState: "active", agentEvent: "blocker_resolved",
			taskState: "active", taskEvent: "builder_activated",
			mutate: func(agent, _ map[string]any) {
				agent["work_blocked_ref"] = "evidence/old-blocker.json"
				agent["blocker_resolved_ref"] = "evidence/resolution.json"
			},
			want: workstate.Active,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBatchP0Input(t)
			state := b.input.Snapshot.State
			state["schema_version"] = workstate.UnifiedVersion
			entities := state["entities"].(map[string]any)
			task := entities["tasks"].([]any)[0].(map[string]any)
			task["state"], task["last_lifecycle_event"] = tc.taskState, tc.taskEvent
			task["owner_agent_ids"] = []any{"builder-1"}
			agent := map[string]any{
				"id": "builder-1", "state": tc.agentState, "last_lifecycle_event": tc.agentEvent,
				"task_ids": []any{"TASK-TEST-01"}, "prompt_ref": "manifest#assignment-1",
			}
			entities["agents"] = []any{agent}
			if tc.mutate != nil {
				tc.mutate(agent, task)
			}

			receipt := qualitygate.CurrentBuilderReceipt(b.input, qualitygate.BuilderReceiptRequest{
				Owner: "builder-1", AssignmentID: "assignment-1", TaskID: "TASK-TEST-01",
			})
			if got := receipt.Status(); got != tc.want {
				t.Fatalf("Status() = %q, want %q (receipt=%+v)", got, tc.want, receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}

func TestBuilderReceiptDoesNotInferDoneFromDiagnosticLabels(t *testing.T) {
	receipt := qualitygate.BuilderReceipt{
		ResponsibilityStatus: "complete",
		ProfessionalStatus:   "complete",
	}
	if got := receipt.Status(); got == workstate.Done {
		t.Fatalf("Status() = %q; diagnostic labels cannot prove original consumer acceptance", got)
	}
}

func TestCurrentBuilderReceiptProjectsLegacyBlockerAndCancellationFacts(t *testing.T) {
	cases := []struct {
		name       string
		agentState string
		taskState  string
		mutate     func(agent, task map[string]any)
		want       workstate.Status
	}{
		{
			name:       "legacy agent blocker reference",
			agentState: "blocked",
			taskState:  "in_progress",
			mutate: func(agent, _ map[string]any) {
				agent["work_blocked_ref"] = "evidence/legacy-blocker.json"
			},
			want: workstate.Blocked,
		},
		{
			name:       "legacy task cancellation reason",
			agentState: "working",
			taskState:  "cancelled",
			mutate: func(_, task map[string]any) {
				task["cancellation_reason"] = "scope withdrawn"
			},
			want: workstate.Cancelled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBatchP0Input(t)
			state := b.input.Snapshot.State
			state["schema_version"] = "1.2.0"
			entities := state["entities"].(map[string]any)
			task := entities["tasks"].([]any)[0].(map[string]any)
			task["state"], task["owner_agent_ids"] = tc.taskState, []any{"builder-1"}
			agent := map[string]any{
				"id": "builder-1", "state": tc.agentState,
				"task_ids": []any{"TASK-TEST-01"}, "prompt_ref": "manifest#assignment-1",
			}
			entities["agents"] = []any{agent}
			if tc.mutate != nil {
				tc.mutate(agent, task)
			}

			receipt := qualitygate.CurrentBuilderReceipt(b.input, qualitygate.BuilderReceiptRequest{
				Owner: "builder-1", AssignmentID: "assignment-1", TaskID: "TASK-TEST-01",
			})
			if got := receipt.Status(); got != tc.want {
				t.Fatalf("Status() = %q, want %q (receipt=%+v)", got, tc.want, receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}

func TestCurrentCanonicalBuilderReceiptSuppressesCompletionWhenResponsibilityIsBlockedOrCancelled(t *testing.T) {
	for _, tc := range []struct {
		name        string
		agentState  string
		agentEvent  string
		taskState   string
		taskEvent   string
		mutateAgent func(map[string]any)
		mutateTask  func(map[string]any)
		want        workstate.Status
	}{
		{
			name:       "current blocker supersedes previously complete answer",
			agentState: "blocked", agentEvent: "work_blocked",
			taskState: "active", taskEvent: "builder_activated",
			mutateAgent: func(agent map[string]any) {
				agent["work_blocked_ref"] = "evidence/current-blocker.json"
			},
			want: workstate.Blocked,
		},
		{
			name:       "current cancellation supersedes previously complete answer",
			agentState: "cancelled", agentEvent: "shutdown_approved",
			taskState: "cancelled", taskEvent: "task_cancelled",
			mutateAgent: func(agent map[string]any) {
				agent["shutdown_approved_ref"] = "evidence/shutdown.json"
			},
			mutateTask: func(task map[string]any) {
				task["cancellation_reason"] = "scope withdrawn"
			},
			want: workstate.Cancelled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCanonicalReceiptFixtureWithClosing(t)
			state := fixture.input.Snapshot.State
			state["schema_version"] = workstate.UnifiedVersion
			entities := state["entities"].(map[string]any)
			agent := entities["agents"].([]any)[0].(map[string]any)
			agent["state"], agent["last_lifecycle_event"] = tc.agentState, tc.agentEvent
			task := entities["tasks"].([]any)[0].(map[string]any)
			task["state"], task["last_lifecycle_event"] = tc.taskState, tc.taskEvent
			if tc.mutateAgent != nil {
				tc.mutateAgent(agent)
			}
			if tc.mutateTask != nil {
				tc.mutateTask(task)
			}

			receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
			if receipt.Accepted || receipt.WorkComplete || receipt.Integrated || receipt.Status() != tc.want {
				t.Fatalf("current terminal fact did not suppress stale complete receipt: %+v", receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}
