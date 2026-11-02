package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func stopCommitPending(t *testing.T, root string) string {
	t.Helper()
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	before, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	life := before.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	id := fmt.Sprintf("pending-fixture-%d", time.Now().UnixNano())
	generation := 0
	if baseline, ok := before.State["baseline"].(map[string]any); ok {
		switch n := baseline["generation"].(type) {
		case int:
			generation = n
		case float64:
			generation = int(n)
		}
	}
	after, err := writer.Update(before.Revision, runtime.Mutation{EventID: id, RequestID: id, TransitionID: "PENDING-FIXTURE", Event: "pending_fixture", Actor: "orchestrator", IdempotencyKey: id, RuntimeID: before.State["runtime_id"].(string), BaselineGeneration: generation, From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(journal)), "\n")
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"schema_version": "1.0.0", "previous_revision": before.Revision, "request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": after.State, "journal_event": event})
	if err != nil {
		t.Fatal(err)
	}
	marker := statePath + ".commit-pending.json"
	if err := os.WriteFile(marker, data, 0600); err != nil {
		t.Fatal(err)
	}
	return marker
}

func stopPendingPublic(t *testing.T, root string, in policy.Input) (string, string) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("public hook: %d %s %s", code, &out, &diag)
	}
	return out.String(), diag.String()
}

func TestNativeStopPendingS9AcceptedPlanContinuesSamePublicEvent(t *testing.T) {
	root, worker, p, in, _ := nativeS9WorkerFixture(t)
	// The original Plan is already accepted before this interrupted Result
	// window. Keep all original Plan/source checks in the subsequent Stop.
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var preparation bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, p, service, &preparation, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(t.Context(), p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(t.Context(), root, p, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IssueRepairResults(t.Context(), p.Binding.OwnerAgentID); err != nil {
		t.Fatal(err)
	}
	marker := stopCommitPending(t, root)
	in.Event = "SubagentStop"
	_, diag := stopPendingPublic(t, root, in)
	if !strings.Contains(diag, "recovered the original pending Writer") {
		t.Fatal(diag)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("same original Stop did not receive S9 candidate: %v %s", err, diag)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	stopPendingPublic(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeStopPendingS8OnlyRecoversOriginalWriter(t *testing.T) {
	root := investigationLeadTestRoot(t)
	investigationLeadTestIngest(t, root)
	marker := stopCommitPending(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	ctx, timings := metrics.WithCallTimings(context.Background())
	in := policy.Input{Context: ctx, Event: "SubagentStop", AgentID: "opaque-investigator", CWD: root, LastAssistantMessage: "original S8 task"}
	var diag bytes.Buffer
	if handled, code := nativeHookRoutes(root, in, &bytes.Buffer{}, &diag); !handled || code != 0 || !strings.Contains(diag.String(), "recovered the original pending Writer") {
		t.Fatalf("S8 route: %t %d %s", handled, code, &diag)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if timings.Snapshot()["hook/native_routes/task_text"].Count != 0 {
		t.Fatal("S8 professional selector ran after shared recovery")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("shared pending not retired: %v", err)
	}
}

func TestNativeStopPendingOnlyExactEventAndActor(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	marker := stopCommitPending(t, root)
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []policy.Input{
		{Event: "SubagentStop", AgentID: "Main", CWD: root},
		{Event: "SubagentStop", CWD: root},
		{Event: "SubagentStart", AgentID: "opaque", CWD: root},
		{Event: "Stop", AgentID: "opaque", CWD: root},
		{Event: "PreToolUse", AgentID: "opaque", CWD: root, ToolName: "Bash"},
		{Event: "PostToolUse", AgentID: "opaque", CWD: root, ToolName: "Write"},
	} {
		stopPendingPublic(t, root, in)
		after, err := os.ReadFile(marker)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s/%s changed pending: %v", in.Event, in.AgentID, err)
		}
	}
}

func TestNativeStopPendingFailureAndCancellationRetainOriginalBoundary(t *testing.T) {
	for _, mode := range []string{"invalid-marker", "canceled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			root, _, _, in, _ := nativeS9WorkerFixture(t)
			marker := stopCommitPending(t, root)
			if mode == "invalid-marker" {
				if err := os.WriteFile(marker, []byte("invalid durable marker"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := repairDispatchRuntimeBytes(t, root)
			markerBefore, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "expired" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			in.Event = "SubagentStop"
			in.Context = ctx
			var diag bytes.Buffer
			nativeHookRoutes(root, in, &bytes.Buffer{}, &diag)
			repairDispatchAssertUnchanged(t, root, before)
			after, err := os.ReadFile(marker)
			if err != nil || !bytes.Equal(markerBefore, after) {
				t.Fatalf("failed attempt changed pending: %v", err)
			}
			if mode == "invalid-marker" && !strings.Contains(diag.String(), "could not recover the original pending Writer") {
				t.Fatal(diag.String())
			}
			if strings.Contains(diag.String(), "answer received") || strings.Contains(diag.String(), "recovered the original pending Writer;") {
				t.Fatal("failure claimed recovery or reception")
			}
		})
	}
}

func TestNativeStopPendingFrozenS9RetainsSamePublicStopGate(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var preparation bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &preparation, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(t.Context(), root, plan, worker); err != nil {
		t.Fatal(err)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("registry: %v", err)
	}
	// Select the independent frozen channel before original Result issuance;
	// bootstrap uses real bytes and the original registry's own validation.
	execution := registry.Executions[plan.Binding.ManifestAssignmentID]
	execution.Purpose = ""
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Bootstrap(execution, binary); err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateBootstrap(execution); err != nil {
		t.Fatal(err)
	}
	registry.Executions[execution.AssignmentID] = execution
	snap.State["workspace"] = workspace.Encode(registry)
	putInitMap(t, service.StatePath, snap.State)
	packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("frozen issuance: %d %v", len(packets), err)
	}
	in := policy.Input{Event: "SubagentStop", AgentID: plan.Binding.OwnerAgentID, CWD: worker, SessionID: "frozen-stop-session", AgentType: "backend-builder", LastAssistantMessage: filepath.Join(worker, packets[0].AnswerPath)}
	call := func() (int, string) {
		t.Helper()
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out, diag bytes.Buffer
		code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(body), &out, &diag)
		return code, out.String() + diag.String()
	}
	before := repairDispatchRuntimeBytes(t, root)
	baselineCode, baseline := call()
	// The retired frozen unfinished Stop gate is gone. L4-workflow-convergence.md:342 —
	// "删除旧 C／D、frozen、typed、Candidate／Final、source pin 的并行接收、来源证明及
	// 恢复接线"; L4-hook-platform-wiring.md:83 — "当前消费者按当前真实责任及派发关联
	// 处理". The current consumer reports the honest fact instead of blocking the
	// return: the answer has not been established as received and the work is not
	// established as complete.
	if baselineCode != 0 {
		t.Fatalf("retired frozen unfinished Stop gate still blocks the return: %d %s", baselineCode, baseline)
	}
	for _, want := range []string{"has not been established as received", "not established as complete"} {
		if !strings.Contains(baseline, want) {
			t.Fatalf("unfinished return did not report %q: %s", want, baseline)
		}
	}
	marker := stopCommitPending(t, root)
	code, feedback := call()
	if code != baselineCode || !strings.Contains(feedback, "recovered the original pending Writer") {
		t.Fatalf("pending swallowed frozen Stop: %d %s; baseline %d %s", code, feedback, baselineCode, baseline)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
}
