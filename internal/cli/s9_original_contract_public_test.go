package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

func TestS9OriginalContractPublicMinimalPlan(t *testing.T) {
	root, worker, packet, in := nativeS9PlanOnlyFixture(t)
	plan, _ := nativeS9TestAuthorPaths(t, root, packet)
	// The author saves the flat professional answer in the current shape:
	// the plan, the red checks and the proposed paths. A partial body is no
	// longer accepted as a minimal plan (L3-S9 §"文件不存在、草稿未就绪或
	// 字段缺失时保留原稿,反馈实际文件与字段").
	writeRepairLeadJSON(t, filepath.Join(worker, plan), workpacket.RepairPlanReportProfessionalAnswer{
		Plan:          "Inspect the original API behavior and apply the approved correction.",
		RedChecks:     []workpacket.RepairPlanReportCheck{{Name: "regression", Command: "go test ./...", Result: "pass"}},
		ProposedPaths: []string{},
	})
	out, diagnostic := s9ReferencePublicHook(t, root, in)
	service, err := submission.NewReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("minimal original Plan: %+v %v; output=%s; diagnostic=%s", receipt, err, out, diagnostic)
	}
}

// s9OriginalReceiver separates the public Hook process from direct original
// consumer coverage. Each scenario creates its own untouched fixture.
type s9OriginalReceiver func(*testing.T, string, policy.Input) (string, string)

// s9OriginalInProcessReceiver exercises the first original consumer/Writer in
// the race-instrumented test process without imposing a platform Hook budget.
// Public event routing and its watchdog are covered by the separate Harness
// scenarios below, not by this direct consumer call.
func s9OriginalInProcessReceiver(t *testing.T, root string, in policy.Input) (string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	if in.AgentID == "Main" {
		snapshot := repairDispatchSnapshot(t, root)
		path, err := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		handled, code := saveRepairImpactQuestionnaireBytes(root, snapshot, path, data, &out, t.Context())
		if !handled || code != 0 {
			t.Fatalf("original Impact consumer: handled=%t code=%d output=%s", handled, code, &out)
		}
	} else {
		in.Context = t.Context()
		handled, code := nativeS9Hook(root, in, &out, &diagnostic)
		if !handled || code != 0 {
			t.Fatalf("original Builder consumer: handled=%t code=%d output=%s diagnostic=%s", handled, code, &out, &diagnostic)
		}
	}
	return out.String(), diagnostic.String()
}

func TestS9OriginalContractPublicEarlyEmptyImpactKeepsBuilderAndUpdates(t *testing.T) {
	binary := factory.BuildHarness(t)
	public := func(t *testing.T, root string, in policy.Input) (string, string) {
		return s9BlockerPublicHook(t, binary, root, in)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		t.Run(event, func(t *testing.T) {
			runS9EarlyImpactKeepsBuilderAndUpdates(t, event, public)
		})
	}
	t.Run("in-process-original-consumers", func(t *testing.T) {
		runS9EarlyImpactKeepsBuilderAndUpdates(t, "PreToolUse", s9OriginalInProcessReceiver)
	})
}

func runS9EarlyImpactKeepsBuilderAndUpdates(t *testing.T, event string, receive s9OriginalReceiver) {
	t.Helper()
	root, worker, packet, in, _ := nativeS9WorkerFixture(t)
	before := repairDispatchSnapshot(t, root)
	impact, err := prepareRepairImpactQuestionnaire(root, before.State)
	if err != nil {
		t.Fatal(err)
	}
	main := policy.Input{Event: event, AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
	receive(t, root, main)
	early := repairDispatchSnapshot(t, root)
	p := repairPointerCLI(early.State)
	first := stringValue(p["impact_ref"])
	if first == "" || stringValue(p["status"]) != "targeted_reverification" {
		t.Fatalf("empty Impact not received through %s: %+v", event, p)
	}
	if stringValue(p["result_ref"]) != "" || len(repairArtifactRefs(p, "targeted_reverification_artifacts", "")) != 0 || early.State["workspace"] != nil {
		t.Fatalf("empty template invented completed work: %+v", p)
	}
	// The original Builder is still in flight when Main's template is received.
	in.Event, in.CWD = "SubagentStop", worker
	nativeS9SplitChild(t, &in)
	receive(t, root, in)
	after := repairDispatchSnapshot(t, root)
	contributions, err := repair.ReceivedRepairContributions(root, after.State)
	if err != nil || len(contributions) != 1 || contributions[0].AssignmentID != packet.Binding.RepairAssignmentID {
		t.Fatalf("late original Builder lost: %+v %v", contributions, err)
	}
	if stringValue(repairPointerCLI(after.State)["result_ref"]) != "" {
		t.Fatal("Impact manufactured a Final")
	}
	writeRepairLeadJSON(t, impact, map[string]any{"change_types": []string{"implementation"}, "decisions": []any{}})
	main.Event = "PreToolUse"
	receive(t, root, main)
	updated := repairPointerCLI(repairDispatchSnapshot(t, root).State)
	if stringValue(updated["impact_ref"]) == first {
		t.Fatalf("existing impact_ref closed Main's original judgment: %+v", updated)
	}
	if _, err := os.Stat(resolveRootPath(root, first)); err != nil {
		t.Fatalf("earlier accepted Impact disappeared: %v", err)
	}
}

func TestS9OriginalContractPublicImpactDoesNotRequireOtherOriginals(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	before := repairDispatchSnapshot(t, root)
	if _, err := prepareRepairImpactQuestionnaire(root, before.State); err != nil {
		t.Fatal(err)
	}
	planRef := stringValue(repairPointerCLI(before.State)["plan_ref"])
	if planRef == "" {
		t.Fatal("original fixture did not register a RepairPlan")
	}
	if err := os.Remove(resolveRootPath(root, planRef)); err != nil {
		t.Fatal(err)
	}
	// A different professional source becoming unavailable cannot revoke or
	// prevent reception of Main's independently understandable original.
	// Targeted preparation may remain pending and is assessed separately.
	main := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
	out, diagnostic := s9ReferencePublicHook(t, root, main)
	after := repairPointerCLI(repairDispatchSnapshot(t, root).State)
	if stringValue(after["impact_ref"]) == "" {
		t.Fatalf("original Impact was not accepted when another source was unavailable: %v; output=%s; diagnostic=%s", after, out, diagnostic)
	}
}
