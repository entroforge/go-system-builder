package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

// Routing fixture only: Builder and independent verifier are actually issued
// by their original planning/dispatch consumers. Domain Result and Impact below
// are explicitly synthetic storage prerequisites, never acceptance receipts.
func s9ReferenceMixedTargetedFixture(t *testing.T) (string, string, repair.TargetedReverificationRequest) {
	t.Helper()
	root, plan, _, _, builderDivision := repairExecutionPlanningFixture(t)
	// Planning installs only the backend Builder definition. Independent
	// registration resolves its own test-builder definition in the same selected
	// installed layout; it must not borrow a legacy root-level role asset.
	writeRepairLeadFile(t, root, ".claude/agents/test-builder.md", "# Test Builder\n")
	repairDispatchSave(t, root, builderDivision, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	snapshot := repairDispatchSnapshot(t, root)
	builder := stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])[plan.Assignments[0].AssignmentID]
	if builder == "" || len(nativeS9PlanPackets(root, snapshot.State)) != 1 {
		t.Fatal("planning did not issue the original Builder packet")
	}
	division := repairVerifierDispatchPaths(root, snapshot.State)[0]
	feedback := repairVerifierDispatchSave(t, root, division, []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true}})
	t.Logf("original independent verifier division: %s", feedback)
	snapshot = repairDispatchSnapshot(t, root)
	pointer := repairPointerCLI(snapshot.State)
	original := plan.Assignments[0].AssignmentID
	performing := repair.VerificationAssignmentID(original)
	owner := stringMapCLI(pointer["assignment_owners"])[performing]
	if owner == "" || owner == builder || !strings.Contains(feedback, "role=test-builder") || !strings.Contains(feedback, "Registration is complete") {
		t.Fatalf("independent verifier division did not register its original owner: builder=%q verifier=%q feedback=%s", builder, owner, feedback)
	}
	agentFound := false
	for _, agent := range objectSliceAny(mapFieldCLI(snapshot.State, "entities")["agents"]) {
		if stringValue(agent["id"]) == owner {
			agentFound = stringValue(agent["role"]) == "test-builder"
		}
	}
	if !agentFound {
		t.Fatalf("independent verifier owner lacks original test-builder Agent: %s; feedback=%s", owner, feedback)
	}
	shared, err := repair.UsesSharedTargetedAuthoring(root, snapshot.State, original, performing, owner)
	if err != nil || !shared {
		t.Fatalf("original registered shared Targeted contract: %t %v", shared, err)
	}
	units := []repair.RepairUnitResult{}
	for _, id := range plan.Assignments[0].UnitIDs {
		units = append(units, repair.RepairUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-repair"}})
	}
	_, finalRef, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{
		Contract: repair.ContractRef{Path: stringValue(pointer["contract_ref"])},
		Session:  repair.ArtifactRef{Path: stringValue(pointer["path"])},
		Plan:     repair.ArtifactRef{Path: stringValue(pointer["plan_ref"])},
		ResultID: "repair-result-targeted-source-guard", ProducerAgentID: builder, AssignmentID: original,
		UnitResults: units, Result: "pass", ChangedArtifacts: []repair.ChangedArtifact{},
		BeforeFixChecks: []repair.RepairCheck{{Name: "synthetic pre-fix observation", Command: "test -f internal/api/one.go", Result: "fail", EvidenceRefs: []string{"test://synthetic-source-guard-red"}}},
		Checks:          []repair.RepairCheck{{Name: "synthetic post-fix observation", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-green"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pointer["result_refs"] = []any{map[string]any{"path": finalRef.Path}}
	// Synthetic Impact establishes the current independent observation slots;
	// TASK/manifest source authority above comes from the actual dispatch path.
	impact := repair.ChangeImpactRequest{ImpactID: "impact-targeted-source-guard", RuntimeID: stringValue(snapshot.State["runtime_id"]), ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{stringValue(pointer["case_id"])}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts:          []repair.ArtifactRef{{ID: "changed-one", Path: "internal/api/one.go"}},
		Decisions:                 []repair.ImpactDecision{{SourceID: "changed-one", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-ROOT", Decision: "reverify", Scope: []string{"internal/api/one.go"}, Rationale: "synthetic source observation", RecoveryEvidence: []string{"test://source-guard"}}},
		RequiredReverificationIDs: []string{"reverify-source-guard"}, AnalyzedBy: "Main"}
	_, impactRef, err := repair.CreateChangeImpact(root, impact)
	if err != nil {
		t.Fatal(err)
	}
	pointer["impact_ref"], pointer["status"] = impactRef.Path, "targeted_reverification"
	pointer["required_reverification_ids"] = []any{"reverify-source-guard"}
	mapFieldCLI(snapshot.State, "lifecycle")["phase"] = "targeted_reverification"
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	selections, err := repairTargetedSelections(root, snapshot.State)
	if err != nil || len(selections) != 1 {
		t.Fatalf("original exact Targeted selection: %+v %v", selections, err)
	}
	path, draft, err := prepareRepairTargetedQuestionnaire(root, snapshot.State, selections[0])
	if err != nil {
		t.Fatal(err)
	}
	draft.Result, draft.FailureClass = "blocked", "blocked"
	for i := range draft.AssertionResults {
		draft.AssertionResults[i].Result = "blocked"
		draft.AssertionResults[i].EvidenceRefs = []string{"test://synthetic-observed-environment-blocker"}
	}
	writeRepairLeadJSON(t, path, draft)
	return root, path, draft
}
