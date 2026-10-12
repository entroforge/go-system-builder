package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/testfixture"
)

func TestRelatedModulePublicCLIClosure(t *testing.T) {
	testRelatedModulePublicCLIClosure(t, false)
}
func TestRelatedModuleRevisedCLIClosure(t *testing.T) {
	testRelatedModulePublicCLIClosure(t, true)
}
func testRelatedModulePublicCLIClosure(t *testing.T, revise bool) {
	root := t.TempDir()
	raw, e := schema.ReadAsset("loop-state.example.json")
	if e != nil {
		t.Fatal(e)
	}
	var state map[string]any
	json.Unmarshal(raw, &state)
	state["runtime_id"] = "loop-REQ-001"
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "planned", "phase_revision": 0}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["documents"] = []any{}
	state["evidence"] = []any{}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	graph := review.ModuleRelations{SchemaVersion: "1.0.0", Modules: []review.ModuleDeclaration{{ID: "customers", OwnedPaths: []string{"server/customers/", "web/customers/"}, CasesPath: "docs/design/prototypes/customers/cases.json", ContractRefs: []string{}, BrowserApplicability: "required"}, {ID: "orders", OwnedPaths: []string{"server/orders/"}, CasesPath: "docs/design/prototypes/orders/cases.json", ContractRefs: []string{}, BrowserApplicability: "required"}}, Resources: []review.ModuleResource{{ID: "customer-status", Paths: []string{"server/customers/status.go"}, ProviderModuleIDs: []string{"customers"}, ConsumerModuleIDs: []string{"orders"}, SourceRefs: []string{"docs/dev/contracts/SYNC-001.md"}}}}
	graphBytes, _ := json.Marshal(graph)
	sp, _ := testfixture.RelatedModules(t, root, state, graphBytes)
	call := func(ok bool, args ...string) string {
		t.Helper()
		var out, errout bytes.Buffer
		args = append(args, "--root", root)
		code := Run(args, bytes.NewReader(nil), &out, &errout)
		if (code == 0) != ok {
			t.Fatalf("CLI %v code=%d stdout=%s stderr=%s", args, code, out.String(), errout.String())
		}
		return out.String()
	}
	authorPath := filepath.Join(root, ".claude/review/author-plan.json")
	if err := os.MkdirAll(filepath.Dir(authorPath), 0755); err != nil {
		t.Fatal(err)
	}
	call(true, "s7", "draft", "--out", authorPath)
	raw, e = os.ReadFile(authorPath)
	if e != nil {
		t.Fatal(e)
	}
	var plan review.Plan
	json.Unmarshal(raw, &plan)
	if len(plan.ModuleScope.Candidates) != 2 {
		t.Fatalf("CLI lost related orders: %+v", plan.ModuleScope)
	}
	scopeSourceBytes := testfixture.Write(t, root, ".claude/evidence/scope-decision.txt", "independent scope assessment observation")
	scopeSource := "path:.claude/evidence/scope-decision.txt#sha256=" + testfixture.Digest(scopeSourceBytes)
	for i := range plan.Claims {
		if len(plan.Claims[i].SourceRefs) == 0 {
			plan.Claims[i].SourceRefs = []string{"server/customers/status.go"}
		}
		if revise && plan.Claims[i].Lens == "e2e" {
			plan.Claims[i].SourceRefs = append(plan.Claims[i].SourceRefs, scopeSource)
		}
		plan.Claims[i].Oracle = strings.ReplaceAll(plan.Claims[i].Oracle, "TODO(planner):", "synthetic oracle:")
	}
	plan.Claims = append(plan.Claims, review.Claim{ClaimID: "claim-dv-1", Lens: "delivery", Target: "server/customers/status.go", Assertion: "REQ acceptance delivered", Oracle: "acceptance is traceable to provider behavior", Method: "traceability", Applicability: "required", SourceRefs: []string{"REQ-001", "server/customers/status.go"}})
	plan.Assignments = append(plan.Assignments, review.PlanAssignment{AssignmentID: "assignment-dv-1", Lens: "delivery", ClaimIDs: []string{"claim-dv-1"}, NonOverlapBoundary: "delivery traceability", ExecutionWave: "static"})
	testfixture.Write(t, root, ".claude/review/author-plan.json", plan)
	call(true, "s7", "lint", "--file", authorPath)
	// Authoring rejection must not write Runtime or journal success.
	before, _ := os.ReadFile(sp)
	bad := plan
	badScope := *plan.ModuleScope
	badScope.Candidates = badScope.Candidates[:1]
	badScope.Dispositions = badScope.Dispositions[:1]
	bad.ModuleScope = &badScope
	testfixture.Write(t, root, ".claude/review/bad.json", bad)
	call(false, "runtime", "review-plan", "--file", filepath.Join(root, ".claude/review/bad.json"))
	after, _ := os.ReadFile(sp)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected CLI plan mutated Runtime")
	}
	call(true, "runtime", "review-plan", "--file", authorPath, "--operation-id", "related-register")
	call(true, "runtime", "review-plan", "--file", authorPath, "--operation-id", "related-register")
	// Seed the independent reviewer dispatch authority in this disposable
	// fixture. Result submission, CleanRound and promotion use public CLI.
	raw, _ = os.ReadFile(sp)
	json.Unmarshal(raw, &state)
	agents := []any{}
	rows := state["review"].(map[string]any)["assignments"].(map[string]any)
	ordered := []review.PlanAssignment{}
	for _, wave := range []string{"static", "behavior"} {
		for _, a := range plan.Assignments {
			if a.ExecutionWave == wave {
				ordered = append(ordered, a)
			}
		}
	}
	for _, a := range ordered {
		role := "qa"
		if a.Lens == "delivery" {
			role = "delivery-verifier"
		}
		if a.Lens == "e2e" {
			role = "e2e-tester"
		}
		agent := "agent-" + a.AssignmentID
		agents = append(agents, map[string]any{"id": agent, "role": role, "state": "working", "task_ids": []any{}, "team_id": "team-review-1", "definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-001/m.json#" + a.AssignmentID, "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-10-12T00:00:00Z"})
		rows[a.AssignmentID].(map[string]any)["agent_id"] = agent
		rows[a.AssignmentID].(map[string]any)["status"] = "dispatched"
	}
	state["entities"].(map[string]any)["agents"] = agents
	testfixture.Write(t, root, ".claude/loop-state.json", state)
	submit := func(i int, a review.PlanAssignment, revision int, suffix string) {
		t.Helper()
		evidencePath := fmt.Sprintf(".claude/evidence/synthetic-observation-%d.txt", i)
		evidence := testfixture.Write(t, root, evidencePath, "synthetic protocol observation; not a business/browser acceptance result\n")
		claimResults := []review.ClaimResult{}
		for _, id := range a.ClaimIDs {
			claimResults = append(claimResults, review.ClaimResult{ClaimID: id, Conclusion: "pass", Observed: "synthetic obligation observed", EvidenceRefs: []string{"path:" + evidencePath + "#sha256=" + testfixture.Digest(evidence)}})
		}
		result := review.Result{SchemaVersion: "1.0.0", ResultID: fmt.Sprintf("review-result-cli-%d%s", i, suffix), AssignmentID: a.AssignmentID, AssignmentRevision: revision, ReviewPlanID: plan.ReviewPlanID, ReviewRound: 1, BaselineGeneration: 1, ProducerAgentID: "agent-" + a.AssignmentID + suffix, SubjectDigest: review.SubjectDigest(&plan), ClaimResults: claimResults, Verdict: "pass"}
		if a.Lens == "e2e" {
			digest, e := review.WorkspaceDigest(root, *plan.VerificationArtifactWorkspace)
			if e != nil {
				t.Fatal(e)
			}
			result.VerificationArtifactDigest = &digest
		}
		resultPath := fmt.Sprintf(".claude/review/result-%d%s.json", i, suffix)
		testfixture.Write(t, root, resultPath, result)
		call(true, "runtime", "review-result", "submit", "--assignment-id", a.AssignmentID, "--result", filepath.Join(root, resultPath))
	}
	revision, suffix := 1, ""
	revised := false
	for i, a := range ordered {
		submit(i, a, revision, suffix)
		if revise && !revised && a.Lens == "e2e" {
			plan.ModuleScope.Dispositions[0].Rationale += "; separately reviewed scope decision"
			testfixture.Write(t, root, ".claude/review/revised.json", plan)
			call(true, "runtime", "review-plan", "revise", "--file", filepath.Join(root, ".claude/review/revised.json"), "--source-ref", scopeSource, "--affected-surface", "docs/design/prototypes")
			call(false, "verification", "clean-round")
			raw, err := os.ReadFile(sp)
			if err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(raw, &state)
			rows := state["review"].(map[string]any)["assignments"].(map[string]any)
			agents := state["entities"].(map[string]any)["agents"].([]any)
			for _, browser := range ordered {
				if browser.Lens != "e2e" {
					continue
				}
				row := rows[browser.AssignmentID].(map[string]any)
				if row["status"] != "planned" || row["result_ref"] != nil {
					t.Fatal("revision retained browser result")
				}
				agent := "agent-" + browser.AssignmentID + "-r2"
				agents = append(agents, map[string]any{"id": agent, "role": "e2e-tester", "state": "working", "task_ids": []any{}, "team_id": "team-review-1", "definition_ref": "agents/e2e-tester.md", "prompt_ref": ".claude/workgroups/REQ-001/m.json#" + browser.AssignmentID, "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-10-12T00:00:00Z"})
				row["status"], row["agent_id"] = "dispatched", agent
			}
			state["entities"].(map[string]any)["agents"] = agents
			testfixture.Write(t, root, ".claude/loop-state.json", state)
			revision, suffix, revised = 2, "-r2", true
			submit(i, a, revision, suffix)
		}
	}
	if revise && !revised {
		t.Fatal("revision scenario did not execute")
	}
	call(true, "verification", "clean-round")
	call(true, "s7", "status")
	// Removing the projection's digest cannot waive the immutable Result's
	// workspace binding at a completion gate.
	originalState, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(originalState, &state)
	for _, raw := range state["review"].(map[string]any)["assignments"].(map[string]any) {
		row := raw.(map[string]any)
		if row["lens"] == "e2e" {
			delete(row, "artifact_digest")
			break
		}
	}
	testfixture.Write(t, root, ".claude/loop-state.json", state)
	call(false, "verification", "clean-round")
	testfixture.Write(t, root, ".claude/loop-state.json", originalState)
	// A changed provider graph blocks even when status still reads clean.
	raw, _ = os.ReadFile(sp)
	var clean map[string]any
	json.Unmarshal(raw, &clean)
	original, err := os.ReadFile(filepath.Join(root, review.ModuleRelationsPath))
	if err != nil {
		t.Fatal(err)
	}
	graph.Resources[0].ConsumerModuleIDs = []string{"customers"}
	testfixture.Write(t, root, review.ModuleRelationsPath, graph)
	testfixture.Git(t, root, "add", review.ModuleRelationsPath)
	testfixture.Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "drifted scope")
	call(false, "verification", "clean-round")
	call(false, "runtime", "transition", "--id", "TR-009", "--actor", "orchestrator", "--evidence", "clean_round_record=clean-round-r1")
	testfixture.Write(t, root, review.ModuleRelationsPath, original)
	testfixture.Git(t, root, "add", review.ModuleRelationsPath)
	testfixture.Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "restore pinned scope")
	call(true, "runtime", "transition", "--id", "TR-009", "--actor", "orchestrator", "--evidence", "clean_round_record=clean-round-r1")
	raw, _ = os.ReadFile(sp)
	json.Unmarshal(raw, &state)
	if state["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatal("TR-009 did not reach acceptance")
	}
	// Keep an optional exact disposable fixture for native/legacy binary checks.
	if destination := os.Getenv("RELATED_MODULE_CLI_FIXTURE"); destination != "" && !revise {
		if e := copyInstallTree(root, destination); e != nil {
			t.Fatal(e)
		}
	}
}
