package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Unlike nativeS9WorkerFixture this creates neither a product commit nor a
// Result, even temporarily. Original dispatch creates the packet and pin;
// git creates the actual isolated checkout before the author saves only Plan.
func nativeS9PlanOnlyFixture(t *testing.T, issuedFeedback ...*string) (string, string, workpacket.RepairPlanReportPacket, policy.Input) {
	t.Helper()
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	feedback := repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	if len(issuedFeedback) > 0 {
		*issuedFeedback[0] = feedback
	}
	snap := repairDispatchSnapshot(t, root)
	packets := nativeS9PlanPackets(root, snap.State)
	if len(packets) != 1 || snap.State["workspace"] != nil {
		t.Fatalf("original issuance must not execute work: %+v", packets)
	}
	p := packets[0]
	worker := filepath.Join(t.TempDir(), "plan-only-worker")
	repairLeadGit(t, root, "worktree", "add", "-b", "native-s9-plan-only", worker, "HEAD")
	planPath, result := nativeS9TestAuthorPaths(t, root, p)
	if _, err := os.Stat(filepath.Join(worker, result)); !os.IsNotExist(err) {
		t.Fatalf("Result must never exist in this fixture: %v", err)
	}
	answer := workpacket.RepairPlanReportProfessionalAnswer{
		Plan:          "Synthetic planning fixture: inspect the existing first API authority before implementation; no product changes have been made.",
		RedChecks:     []workpacket.RepairPlanReportCheck{{Name: "existing first authority", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"internal/api/one.go"}}},
		ProposedPaths: plan.Assignments[0].Scope,
	}
	if _, err := os.Stat(filepath.Join(worker, "internal/api/one.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(worker, planPath)), 0700); err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, filepath.Join(worker, planPath), answer)
	brief := nativeS9BriefPath(p)
	in := policy.Input{Event: "SubagentStop", AgentID: "plan-only-child", SessionID: "plan-only-parent", CWD: worker, AgentType: "backend-builder", LastAssistantMessage: "Original Main assignment: ./" + brief + ". Only prepare the professional Plan before implementation; do not modify product or tests and do not commit."}
	nativeS9SplitChild(t, &in)
	in.LastAssistantMessage = "The professional plan is saved; implementation has not started."
	return root, worker, p, in
}

func nativeS9AssertPlanOnlyBoundary(t *testing.T, root, worker string, p workpacket.RepairPlanReportPacket, accepted bool) {
	t.Helper()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
	if err != nil || receipt.Accepted != accepted {
		t.Fatalf("original Plan receipt: %+v %v; want accepted=%t", receipt, err, accepted)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry != nil && len(registry.Executions) != 0 {
		t.Fatalf("Plan-only reception created execution: %+v %v", registry, err)
	}
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, snap.State, p.Binding.OwnerAgentID)
	if err != nil || len(packets) != 0 {
		t.Fatalf("Plan-only reception issued Result/Candidate: %+v %v", packets, err)
	}
	_, result := nativeS9TestAuthorPaths(t, root, p)
	if _, err := os.Stat(filepath.Join(worker, result)); !os.IsNotExist(err) {
		t.Fatalf("Plan-only reception fabricated Result: %v", err)
	}
	if rootHead, workerHead := repairLeadGit(t, root, "rev-parse", "HEAD"), repairLeadGit(t, worker, "rev-parse", "HEAD"); rootHead != workerHead {
		t.Fatalf("Plan-only fixture gained a product commit: %s != %s", rootHead, workerHead)
	}
	if got := repairLeadGit(t, worker, "diff", "HEAD", "--"); got != "" {
		t.Fatalf("Plan-only reception changed tracked products: %s", got)
	}
}

func TestNativeS9BeforeImplementationPlanOnlyPublicEvents(t *testing.T) {
	// One original source for each event, without a spelling/event Cartesian
	// product. Read Post is deliberately only an independent observation.
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "ReadPostObservation", "IssuedDispatchPrompt"} {
		t.Run(event, func(t *testing.T) {
			var issued string
			root, worker, p, in := nativeS9PlanOnlyFixture(t, &issued)
			planPath, result := nativeS9TestAuthorPaths(t, root, p)
			brief := nativeS9BriefPath(p)
			ref := "./" + brief
			if event == "PreToolUse" {
				ref = filepath.Join(root, brief)
			} else if event == "PostToolUse" {
				ref = brief
			}
			in.LastAssistantMessage = "Original Main assignment: " + ref + ". Prepare only Plan; no development or commits."
			if event == "IssuedDispatchPrompt" {
				// Forward the actual decoded issuance output, with no path repair.
				var payload struct {
					Output struct {
						Context string `json:"additionalContext"`
					} `json:"hookSpecificOutput"`
				}
				if err := json.Unmarshal([]byte(issued), &payload); err != nil {
					t.Fatal(err)
				}
				in.LastAssistantMessage = payload.Output.Context
				if !strings.Contains(in.LastAssistantMessage, "carry this exact S9 brief") {
					t.Fatalf("actual issuance lost dispatch guidance: %s", issued)
				}
			}
			nativeS9SplitChild(t, &in)
			in.LastAssistantMessage = "Plan saved, no implementation."
			in.Event, in.ToolName = event, "Read"
			if event == "IssuedDispatchPrompt" {
				in.Event = "SubagentStop"
			}
			in.ToolInput = map[string]any{"file_path": filepath.Join(worker, "internal/api/one.go")}
			if event == "ReadPostObservation" {
				in.Event = "PostToolUse"
				in.ToolResponse = map[string]any{"is_error": false}
			}
			if event == "PostToolUse" {
				content, err := os.ReadFile(filepath.Join(worker, planPath))
				if err != nil {
					t.Fatal(err)
				}
				in.ToolName = "Write"
				in.ToolInput = map[string]any{"file_path": filepath.Join(worker, planPath), "content": string(content)}
				in.ToolResponse = map[string]any{"is_error": true}
				before := repairDispatchRuntimeBytes(t, root)
				s9ReferencePublicHook(t, root, in)
				repairDispatchAssertUnchanged(t, root, before)
				if err := os.WriteFile(filepath.Join(worker, planPath), content, 0600); err != nil {
					t.Fatal(err)
				}
				in.ToolResponse = map[string]any{"is_error": false}
			}
			nativeS9AssertPlanOnlyBoundary(t, root, worker, p, false)
			out, diagnostic := s9ReferencePublicHook(t, root, in)
			nativeS9AssertPlanOnlyBoundary(t, root, worker, p, true)
			feedback := out + diagnostic
			if !strings.Contains(feedback, "accepted") {
				t.Errorf("Plan receipt is unclear: %s", feedback)
			}
			if in.Event == "SubagentStop" && !strings.Contains(feedback, "Main") {
				t.Errorf("Stop lost Main continuation: %s", feedback)
			}
			if strings.Contains(feedback, "may remain pending") || strings.Contains(feedback, "failed attempt does not confirm a receipt") {
				t.Errorf("known Plan receipt confused with unknown Writer progress: %s", feedback)
			}
			for _, want := range []string{"PlanReport", p.Binding.OwnerAgentID, filepath.Join(worker, planPath), filepath.Join(worker, result)} {
				if !strings.Contains(feedback, want) {
					t.Errorf("missing actual Plan-only feedback %q: %s", want, feedback)
				}
			}
			before := repairDispatchRuntimeBytes(t, root)
			s9ReferencePublicHook(t, root, in)
			repairDispatchAssertUnchanged(t, root, before)
			nativeS9AssertPlanOnlyBoundary(t, root, worker, p, true)
		})
	}
}

func TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan(t *testing.T) {
	for _, kind := range []string{"missing", "draft", "field_error"} {
		t.Run(kind, func(t *testing.T) {
			root, worker, p, in := nativeS9PlanOnlyFixture(t)
			planPath, _ := nativeS9TestAuthorPaths(t, root, p)
			path := filepath.Join(worker, planPath)
			switch kind {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "draft":
				if err := os.WriteFile(path, []byte(`{"plan":`), 0600); err != nil {
					t.Fatal(err)
				}
			case "field_error":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var answer map[string]any
				if err := json.Unmarshal(data, &answer); err != nil {
					t.Fatal(err)
				}
				answer["red_checks"].([]any)[0].(map[string]any)["result"] = "invented"
				writeRepairLeadJSON(t, path, answer)
			}
			before := repairDispatchRuntimeBytes(t, root)
			out, diagnostic := s9ReferencePublicHook(t, root, in)
			repairDispatchAssertUnchanged(t, root, before)
			nativeS9AssertPlanOnlyBoundary(t, root, worker, p, false)
			feedback := out + diagnostic
			for _, want := range []string{path, "Main"} {
				if !strings.Contains(feedback, want) {
					t.Errorf("unready Plan lost original responsibility %q: %s", want, feedback)
				}
			}
			if kind == "field_error" && !strings.Contains(feedback, "/red_checks/0/result") {
				t.Errorf("lost original professional field rejection: %s", feedback)
			}
		})
	}
}

func TestNativeS9PlanOnlyStopPreservesReceiptAfterPrivatePlanRemoval(t *testing.T) {
	for _, resultState := range []string{"missing", "draft", "no-conclusion"} {
		t.Run(resultState, func(t *testing.T) {
			root, worker, p, in := nativeS9PlanOnlyFixture(t)
			planPath, resultPath := nativeS9TestAuthorPaths(t, root, p)
			plan, result := filepath.Join(worker, planPath), filepath.Join(worker, resultPath)
			if resultState == "draft" {
				if err := os.WriteFile(result, []byte(`{"verdict":`), 0600); err != nil {
					t.Fatal(err)
				}
			} else if resultState == "no-conclusion" {
				writeRepairLeadJSON(t, result, map[string]any{"verdict": ""})
			}
			out, diagnostic := s9ReferencePublicHook(t, root, in)
			for _, want := range []string{"accepted", "Main", p.Binding.OwnerAgentID, plan, result} {
				if !strings.Contains(out+diagnostic, want) {
					t.Errorf("first Stop lost %q: %s%s", want, out, diagnostic)
				}
			}
			if err := os.Remove(plan); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			out, diagnostic = s9ReferencePublicHook(t, root, in)
			repairDispatchAssertUnchanged(t, root, before)
			for _, want := range []string{"accepted", "Main", plan, result} {
				if !strings.Contains(out+diagnostic, want) {
					t.Errorf("accepted replay lost %q: %s%s", want, out, diagnostic)
				}
			}
			snapshot := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snapshot.State)
			if err != nil || registry != nil && len(registry.Executions) != 0 {
				t.Fatalf("Plan-only Stop created execution: %+v %v", registry, err)
			}
		})
	}
}
