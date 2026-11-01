package cli

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/repair"
)

func repairExecutionPlanningFixture(t *testing.T, assertionCopies ...int) (string, repair.RepairPlan, repair.ArtifactRef, repair.ArtifactRef, string) {
	t.Helper()
	root := repairLeadFreshRoot(t)
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# Installed protocol\n")
	writeRepairLeadFile(t, root, "docs/rules/test.md", "# Fixture rules\n")
	writeRepairLeadFile(t, root, ".claude/agents/backend-builder.md", "# Backend Builder\n")
	names := append(agentcontext.CoreSkills("backend-builder"), agentcontext.RequiredSkills("BUILD-WORK-PACKAGE")...)
	names = append(names, "code-quality", "testing-strategy", "api-contracts", "state-machine-design", "integration-verification")
	for _, name := range names {
		writeRepairLeadFile(t, root, ".claude/skills/"+name+"/SKILL.md", "# "+name+"\n")
	}
	for _, path := range []string{"internal/api/one.go", "internal/api/two.go"} {
		writeRepairLeadFile(t, root, path, "package api\n")
	}
	repairLeadApprovedCase(t, root, "investigation-case-execution-choice", []map[string]any{
		{"id": "unit-one", "description": "restore first authority", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"}},
		{"id": "unit-two", "description": "restore dependent authority", "scope": []string{"internal/api/two.go"}, "assertion_ids": []string{"all"}, "depends_on": []string{"unit-one"}},
	}, assertionCopies...)
	snapshot := repairDispatchSnapshot(t, root)
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-execution-choice", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, PlanID: "repair-plan-execution-choice", CreatedBy: "Main"})
	if err != nil || len(plan.Assignments) != 2 {
		t.Fatalf("original two-assignment Plan: %#v %v", plan, err)
	}
	if _, err := prepareRepairEntry(root, statePath, journalPath, snapshot.State); err != nil {
		t.Fatal(err)
	}
	path := repairDispatchPaths(root, snapshot.State)[0]
	return root, plan, sessionRef, planRef, path
}

// Workspace-only tests deliberately register through passive dispatch setup:
// their existing legacy PlanReport fixtures do not claim the normal typed
// dispatch/PlanReport author path and must not bypass an issued-packet guard.
