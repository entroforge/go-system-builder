package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9GraphListenerDoesNotRunControllerOrDuplicateStopProof(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	for _, phase := range []string{"repair_readback", "planning", "reproducing", "fixing", "targeted_reverification", "ready_for_full_review"} {
		mapFieldCLI(f.state, "lifecycle")["phase"] = phase
		f.write(t)
		before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		for _, event := range []string{"SessionStart", "PreCompact", "PreToolUse", "SubagentStart", "SubagentStop", "TeammateIdle", "Stop"} {
			result, observed := graphControlForHook(f.root, policy.Input{Event: event, AgentID: "builder-listener", CWD: f.worker})
			if !observed || result.QualityGate.Status != "" || result.Decision.Guidance == nil || result.Decision.Guidance.Blocked {
				t.Fatalf("%s %s entered Controller/duplicate completion proof: %+v", phase, event, result)
			}
			if len(result.Decision.Guidance.Missing) != 0 || strings.Contains(result.Decision.Guidance.Action, "completion_report") {
				t.Fatalf("%s %s retained generic completion work: %+v", phase, event, result.Decision.Guidance)
			}
		}
		after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		if string(before) != string(after) {
			t.Fatal("graph reminder changed Runtime")
		}
	}
	mapFieldCLI(f.state, "lifecycle")["phase"] = "bug_report_review"
	f.write(t)
	if _, observed := graphControlForHook(f.root, policy.Input{Event: "PreToolUse"}); observed {
		t.Fatal("legacy BUG phase lost original cycle")
	}
	mapFieldCLI(f.state, "lifecycle")["phase"] = "fixing"
	mapFieldCLI(f.state, "configuration")["optimization"] = map[string]any{"packet": false}
	f.write(t)
	if _, observed := graphControlForHook(f.root, policy.Input{Event: "PreToolUse"}); observed {
		t.Fatal("nonpacket repair lost original cycle")
	}
}

func newS9ExecutionOriginalFixture(t *testing.T) (string, runtime.Snapshot, repair.RepairPlan, repair.ArtifactRef, repair.ArtifactRef) {
	t.Helper()
	root := repairLeadFreshRoot(t)
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# protocol\n")
	writeRepairLeadFile(t, root, "agents/test-builder.md", "# Test Builder\n")
	state := repairLeadApprovedCase(t, root, "investigation-case-execution-listener", []map[string]any{
		{"id": "unit-one", "description": "restore one boundary", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"}},
		{"id": "unit-two", "description": "restore second boundary", "scope": []string{"internal/api/two.go"}, "assertion_ids": []string{"all"}},
	})
	state["schema_version"] = "1.2.0"
	mapFieldCLI(state, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	current := repairDispatchSnapshot(t, root)
	opened, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision, Actor: "Main"}, SessionID: "repair-session-execution-listener", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: opened.Revision, Actor: "Main"}, PlanID: "repair-plan-execution-listener", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	return root, compiled, plan, sessionRef, planRef
}

func TestS9EntryReadbackImmediatelyDeliversFreshMainDivision(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadApprovedCase(t, root, "investigation-case-entry-readback", []map[string]any{{"id": "unit-entry", "description": "restore boundary", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"}}})
	state["schema_version"] = "1.2.0"
	mapFieldCLI(state, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	if _, err := prepareRepairEntry(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repairDispatchSnapshot(t, root).State); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root})
	repairDispatchAssertUnchanged(t, root, before)
	current := repairDispatchSnapshot(t, root)
	paths := repairDispatchPaths(root, current.State)
	if mapFieldCLI(current.State, "lifecycle")["phase"] != "planning" || len(paths) != 1 || !strings.Contains(got, paths[0]) || !strings.Contains(got, "Builder division") {
		t.Fatalf("readback stayed on stale cursor: phase=%v paths=%v context=%s", mapFieldCLI(current.State, "lifecycle"), paths, got)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("fresh Main division not prepared: %v", err)
	}
}

func TestS9EntryPendingTargetedIncludesExactVerifierDivision(t *testing.T) {
	root, snapshot, _, _ := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	mapFieldCLI(snapshot.State, "lifecycle")["phase"] = "targeted_reverification"
	repairPointerCLI(snapshot.State)["status"] = "targeted_reverification"
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	paths := repairVerifierDispatchPaths(root, snapshot.State)
	if _, err := prepareRepairEntry(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repairDispatchSnapshot(t, root).State); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root})
	repairDispatchAssertUnchanged(t, root, before)
	if len(paths) != 1 || !strings.Contains(got, paths[0]) || !strings.Contains(got, "independent verifier division") {
		t.Fatalf("Targeted source/dispatch gap hid Main's exact selection questionnaire: paths=%v context=%s", paths, got)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("pending verifier division was not prepared: %v", err)
	}
}

func TestS9OrdinaryPreDoesNotBeginRecordedWork(t *testing.T) {
	f := newS9ListenerFixture(t, false)
	mapFieldCLI(f.state, "lifecycle")["phase"] = "reproducing"
	repairPointerCLI(f.state)["status"] = "reproducing"
	f.write(t)
	before := repairDispatchRuntimeBytes(t, f.root)
	currentQuestionnaireLifecycleContext(f.root, policy.Input{Event: "PreToolUse", AgentID: f.packet.binding.OwnerAgentID, CWD: f.root})
	repairDispatchAssertUnchanged(t, f.root, before)
}
