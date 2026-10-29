package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Professional observations are synthetic fixture answers. Native events,
// real Git worktrees, Main commands and the original domain consumers execute;
// this is not a Claude platform or product acceptance test.
func TestNativeS9WholeBatchNoChangeKeepsMainChecksAndIndependentReview(t *testing.T) {
	for _, outcome := range []string{"complete", "interrupted", "missing-assertion", "implementer"} {
		t.Run(outcome, func(t *testing.T) { testNativeS9NoChangeContractOutcome(t, outcome, false) })
	}
}

func TestNativeS9NoChangeAfterPlatformCleanup(t *testing.T) {
	for _, outcome := range []string{"complete", "missing-assertion", "implementer", "recreated-directory", "rewritten-target", "moved-branch"} {
		t.Run(outcome, func(t *testing.T) { testNativeS9NoChangeContractOutcome(t, outcome, true) })
	}
}

func TestNativeS9RetainedSourceResumesOriginalConsumersAfterPreCandidateCleanup(t *testing.T) {
	testNativeS9NoChangeContractOutcome(t, "source-interrupted", true)
}

func testNativeS9NoChangeContractOutcome(t *testing.T, outcome string, platformCleanup bool) {
	t.Helper()
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	writeRepairLeadFile(t, root, ".claude/agents/test-builder.md", "# Test Builder\n")
	for _, name := range agentcontext.CoreSkills("test-builder") {
		path := resolveRootPath(root, ".claude/skills/"+name+"/SKILL.md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			writeRepairLeadFile(t, root, ".claude/skills/"+name+"/SKILL.md", "# "+name+"\n")
		}
	}
	head := repairLeadGit(t, root, "rev-parse", "HEAD")
	var diagnostic bytes.Buffer
	for index, assignment := range plan.Assignments {
		repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: assignment.AssignmentID, RoleFamily: "backend-builder"}})
		packets := nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State)
		var packet workpacket.RepairPlanReportPacket
		for _, p := range packets {
			if p.Binding.RepairAssignmentID == assignment.AssignmentID {
				packet = p
			}
		}
		if packet.ID == "" {
			t.Fatal("normal native dispatch did not issue the assigned Plan")
		}
		brief, _, resultPath := nativeS9Paths(packet)
		worker := filepath.Join(t.TempDir(), "worker")
		repairLeadGit(t, root, "worktree", "add", "-b", fmt.Sprintf("no-change-%d", index), worker, "HEAD")
		input := policy.Input{Event: "SubagentStart", CWD: root, AgentID: fmt.Sprintf("opaque-no-change-%d", index), AgentType: "backend-builder", LastAssistantMessage: resolveRootPath(root, brief)}
		diagnostic.Reset()
		if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
			t.Fatalf("native Start: %t %d %s", handled, code, &diagnostic)
		}
		for _, path := range []string{packet.AnswerPath, resultPath} {
			if err := os.MkdirAll(filepath.Dir(resolveRootPath(worker, path)), 0700); err != nil {
				t.Fatal(err)
			}
		}
		writeRepairLeadJSON(t, resolveRootPath(worker, packet.AnswerPath), workpacket.RepairPlanReportProfessionalAnswer{
			Plan:          "Inspect the assigned existing behavior; report actual checks and any remaining defect without manufacturing a change",
			ProposedPaths: []string{}, RedChecks: []workpacket.RepairPlanReportCheck{{Name: "existing source", Command: "test -f " + assignment.Scope[0], Result: "pass", EvidenceRefs: []string{assignment.Scope[0]}}},
		})
		answer := workpacket.RepairResultProfessionalAnswer{Verdict: "completed", BeforeFixResults: []workpacket.RepairResultBeforeFixResult{}, UnitResults: []workpacket.RepairResultUnitResult{}, DetectionGapArtifacts: []string{}, EvidenceRefs: assignment.Scope, ScopeDeviations: []string{}, ResidualRisks: []string{}}
		for _, id := range assignment.AssertionIDs {
			answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{AssertionID: id, ObservedRed: "Synthetic fixture observation: the existing behavior satisfies this responsibility; no product edit is proposed", EvidenceRefs: assignment.Scope})
		}
		for _, id := range assignment.UnitIDs {
			answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{UnitID: id, Status: "pass", EvidenceRefs: assignment.Scope, ImplementationSummary: "Inspected the existing source and found no necessary product contribution", RestoredInvariantRefs: []string{}})
		}
		writeRepairLeadJSON(t, resolveRootPath(worker, resultPath), answer)
		input.Event, input.LastAssistantMessage = "SubagentStop", resolveRootPath(root, brief)+"\n"+resolveRootPath(worker, resultPath)
		typedBlocker := filepath.Join(root, ".claude/evidence/s9-source", packet.ID, "typed")
		if outcome == "source-interrupted" {
			writeRepairLeadFile(t, root, filepath.ToSlash(filepath.Join(".claude/evidence/s9-source", packet.ID, "typed")), "injected interruption before typed publication")
			diagnostic.Reset()
			if handled, _ := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || !strings.Contains(diagnostic.String(), "incomplete") {
				t.Fatalf("injected source interruption was not reported: %s", &diagnostic)
			}
			partial := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(partial.State)
			if err != nil || registry == nil {
				t.Fatalf("no durable source: %v", err)
			}
			e := registry.Executions[packet.Binding.ManifestAssignmentID]
			if repair.NativeS9SourceReference(e).Path == "" || e.DeliveryRef != "" {
				t.Fatalf("capture became acceptance or was missing: %+v; %s", e, &diagnostic)
			}
			if len(repairArtifactRefs(repairPointerCLI(partial.State), "plan_report_refs", "", "")) == 0 {
				t.Fatal("interruption did not occur after original Plan acceptance")
			}
			source, err := repair.RetainedNativeS9Source(context.Background(), root, e, packet.Binding)
			if err != nil || source == nil {
				t.Fatalf("retained source unavailable: %v", err)
			}
			originalPlan := append([]byte(nil), source.Plan...)
			pendingMarker := stopFingerprintPending(t, root)
			repairLeadGit(t, root, "worktree", "remove", "--force", worker)
			repairLeadGit(t, root, "branch", "-D", fmt.Sprintf("no-change-%d", index))
			if err := os.Remove(typedBlocker); err != nil {
				t.Fatal(err)
			}
			diagnostic.Reset()
			_, recoveredDiagnostic := stopPendingPublic(t, root, input)
			diagnostic.WriteString(recoveredDiagnostic)
			if !strings.Contains(recoveredDiagnostic, "recovered the original pending Writer") {
				t.Fatalf("public source recovery did not recover Writer: %s", recoveredDiagnostic)
			}
			if _, err := os.Stat(pendingMarker); !os.IsNotExist(err) {
				t.Fatalf("Writer marker not retired: %v", err)
			}
			source, err = repair.RetainedNativeS9Source(context.Background(), root, e, packet.Binding)
			if err != nil || !bytes.Equal(source.Plan, originalPlan) {
				t.Fatalf("Plan input changed: %v", err)
			}
			if _, err := os.Stat(worker); !os.IsNotExist(err) {
				t.Fatalf("recovery recreated Worker: %v", err)
			}
		} else {
			diagnostic.Reset()
			if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
				t.Fatalf("native no-change Stop: %t %d %s", handled, code, &diagnostic)
			}
		}
		received := repairDispatchSnapshot(t, root)
		registry, err := workspace.Decode(received.State)
		if err != nil || registry == nil {
			t.Fatalf("original native delivery absent: %v %s", err, &diagnostic)
		}
		e := registry.Executions[packet.Binding.ManifestAssignmentID]
		candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256})
		if err != nil || candidate.SourceHead != head || len(candidate.Proposal.ChangedArtifacts) != 0 {
			t.Fatalf("no-change delivery invented a contribution: %+v %v", candidate, err)
		}
		if platformCleanup && outcome != "source-interrupted" {
			repairLeadGit(t, root, "worktree", "remove", "--force", worker)
			repairLeadGit(t, root, "branch", "-D", fmt.Sprintf("no-change-%d", index))
		}
		args := []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}
		before := repairDispatchRuntimeBytes(t, root)
		diagnostic.Reset()
		if code := runWorkspaceDelivery(args, &bytes.Buffer{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "check") {
			t.Fatalf("no-change was finalized without actual Main checks: %d %s", code, &diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
		diagnostic.Reset()
		args = append(args, "--check-command", "test -f "+assignment.Scope[0]+" && git diff --exit-code HEAD -- internal/api")
		negative := outcome == "recreated-directory" || outcome == "rewritten-target" || outcome == "moved-branch"
		if negative {
			switch outcome {
			case "recreated-directory":
				if err := os.MkdirAll(worker, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(worker, "unreceived.txt"), []byte("preserve this later work"), 0600); err != nil {
					t.Fatal(err)
				}
			case "rewritten-target":
				tree := repairLeadGit(t, root, "rev-parse", "HEAD^{tree}")
				replacement := repairLeadGit(t, root, "commit-tree", tree, "-m", "Unrelated target with identical bytes")
				repairLeadGit(t, root, "update-ref", "refs/heads/"+e.TargetBranch, replacement)
			case "moved-branch":
				tree := repairLeadGit(t, root, "rev-parse", "HEAD^{tree}")
				later := repairLeadGit(t, root, "commit-tree", tree, "-p", head, "-m", "Different source after the original native reception")
				repairLeadGit(t, root, "branch", fmt.Sprintf("no-change-%d", index), later)
			}
			before := repairDispatchRuntimeBytes(t, root)
			currentHead := repairLeadGit(t, root, "rev-parse", "HEAD")
			if code := runWorkspaceDelivery(args, &bytes.Buffer{}, &diagnostic); code == 0 {
				t.Fatalf("missing checkout borrowed invalid %s source: %s", outcome, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
			if repairLeadGit(t, root, "rev-parse", "HEAD") != currentHead {
				t.Fatal("rejected source changed Main")
			}
			if outcome == "recreated-directory" {
				if _, err := os.Stat(filepath.Join(worker, "unreceived.txt")); err != nil {
					t.Fatalf("later unreceived work was removed: %v", err)
				}
			}
			return
		}
		if code := runWorkspaceDelivery(args, &bytes.Buffer{}, &diagnostic); code != 0 {
			t.Fatalf("original Main no-change integration: %d %s", code, &diagnostic)
		}
		final := repairDispatchSnapshot(t, root)
		refs := repairArtifactRefs(repairPointerCLI(final.State), "result_refs", "result_ref", "result_sha256")
		if len(refs) != index+1 {
			t.Fatalf("exact original Final batch not retained: %+v", refs)
		}
		for _, ref := range refs {
			result, err := repair.ValidateRepairResult(root, ref)
			if err != nil || result.Result != "pass" || len(result.ChangedArtifacts) != 0 || len(result.Checks) == 0 || result.Checks[0].Result != "pass" {
				t.Fatalf("no-change Final lost Main check facts: %+v %v", result, err)
			}
		}
		if got := repairLeadGit(t, root, "rev-parse", "HEAD"); got != head {
			t.Fatalf("no-change manufactured a Main commit: %s -> %s", head, got)
		}
	}
	current := repairDispatchSnapshot(t, root)
	impactPath, err := repairLeadQuestionnairePath(root, current.State, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	var impact repair.ChangeImpactRequest
	readRepairLeadJSON(t, impactPath, &impact)
	if impact.ChangedArtifacts == nil || len(impact.ChangedArtifacts) != 0 || len(impact.ChangeTypes) != 0 || len(impact.RequiredReverificationIDs) != 1 {
		t.Fatalf("empty accepted batch fabricated a change or dropped verification: %+v", impact)
	}
	before := repairDispatchRuntimeBytes(t, root)
	diagnostic.Reset()
	runSavedRepairLeadQuestionnaire(root, repairLeadSaveInput(root, impactPath, "Main"), &diagnostic, &bytes.Buffer{})
	repairDispatchAssertUnchanged(t, root, before)
	if !strings.Contains(diagnostic.String(), "incomplete") {
		t.Fatalf("empty professional judgment did not name the gap: %s", &diagnostic)
	}
	impact.Decisions = []repair.ImpactDecision{{SourceID: impact.SourceCaseIDs[0], TargetID: "root-1", Relation: "requires-verification", RuleID: "IM-CONTRACT", Decision: "reverify", Scope: []string{}, Rationale: "The synthetic existing behavior needs no product edit; independently check all original Contract assertions before closing", RecoveryEvidence: []string{stringValue(repairPointerCLI(current.State)["result_ref"])}}}
	var stagedImpact repair.ArtifactRef
	if outcome == "interrupted" {
		// Simulate the existing system Create succeeding before Commit was
		// interrupted. The author does not preserve or copy its machine time.
		impact.AnalyzedAt = time.Now().UTC()
		_, stagedImpact, err = repair.CreateChangeImpact(root, impact)
		if err != nil {
			t.Fatal(err)
		}
	}
	professionalImpact := map[string]any{
		"change_types": impact.ChangeTypes, "decisions": impact.Decisions,
		"escalation_level":         impact.EscalationLevel,
		"invalidated_evidence_ids": impact.InvalidatedEvidenceIDs,
		"superseded_evidence_ids":  impact.SupersededEvidenceIDs,
		"retained_evidence_ids":    impact.RetainedEvidenceIDs,
	}
	if outcome == "interrupted" {
		original := impact.Decisions[0].Rationale
		impact.Decisions[0].Rationale = "different professional answer after the original artifact was written"
		writeRepairLeadJSON(t, impactPath, professionalImpact)
		diagnostic.Reset()
		runSavedRepairLeadQuestionnaire(root, repairLeadSaveInput(root, impactPath, "Main"), &diagnostic, &bytes.Buffer{})
		repairDispatchAssertUnchanged(t, root, before)
		if !strings.Contains(diagnostic.String(), "did not create") {
			t.Fatalf("interrupted Impact replaced the original professional content: %s", &diagnostic)
		}
		impact.Decisions[0].Rationale = original
	}
	// Normal authors submit only these original professional fields. The
	// system rebinds the accepted Final diff, identities, obligations and time.
	writeRepairLeadJSON(t, impactPath, professionalImpact)
	diagnostic.Reset()
	if handled, code := runSavedRepairLeadQuestionnaire(root, repairLeadSaveInput(root, impactPath, "Main"), &diagnostic, &bytes.Buffer{}); !handled || code != 0 || !strings.Contains(diagnostic.String(), "committed through the original Runtime Writer") {
		t.Fatalf("no-change Impact not received: %t %d %s", handled, code, &diagnostic)
	}
	current = repairDispatchSnapshot(t, root)
	if outcome == "interrupted" && (repairPointerCLI(current.State)["impact_ref"] != stagedImpact.Path || repairPointerCLI(current.State)["impact_sha256"] != stagedImpact.SHA256) {
		t.Fatal("interrupted Impact retry replaced the original immutable source")
	}
	if repairPointerCLI(current.State)["status"] != "targeted_reverification" {
		t.Fatalf("empty batch skipped original independent responsibility: %+v", repairPointerCLI(current.State))
	}
	division, err = repairLeadQuestionnairePath(root, current.State, "independent-verifier-dispatch.json")
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, division, []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true, AgentID: "no-change-independent"}})
	diagnostic.Reset()
	if handled, code := runSavedRepairVerifierDispatch(root, repairLeadSaveInput(root, division, "Main"), &diagnostic, &bytes.Buffer{}); !handled || code != 0 {
		t.Fatalf("original independent dispatch: %t %d %s", handled, code, &diagnostic)
	}
	current = repairDispatchSnapshot(t, root)
	paths := repairLeadQuestionnairePaths(root, current.State)
	if len(paths) != 1 {
		t.Fatalf("exact Contract verification answer missing: %+v %s", paths, &diagnostic)
	}
	var target repair.TargetedReverificationRequest
	readRepairLeadJSON(t, paths[0], &target)
	if len(target.AssertionResults) != 3 {
		t.Fatalf("no-change dropped original symptom/root/gap assertions: %+v", target)
	}
	target.Result = "pass"
	for i := range target.AssertionResults {
		target.AssertionResults[i].Result, target.AssertionResults[i].EvidenceRefs = "pass", []string{"test://synthetic-original-contract-observation"}
	}
	if outcome == "implementer" {
		// The original domain consumer must refuse self-verification even for
		// an implementer with no product edits. Dispatch labels grant nothing.
		target.PerformingAssignmentID = plan.Assignments[0].AssignmentID
		_, ref, err := repair.CreateTargetedReverification(root, target)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := repair.CommitTargetedReverification(root, resolveRootPath(root, ".claude/loop-state.json"), resolveRootPath(root, ".claude/loop-events.jsonl"), repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision}, Reverification: ref})
		if err != nil || repairPointerCLI(observed.State)["status"] != "targeted_reverification" || !strings.Contains(stringValue(repairPointerCLI(observed.State)["next_action"]), "independent") {
			t.Fatalf("no-change implementer borrowed an independent PASS: %+v %v", observed.State, err)
		}
		return
	}
	if outcome == "missing-assertion" {
		target.AssertionResults = target.AssertionResults[:1]
	}
	var stagedTarget repair.ArtifactRef
	if outcome == "interrupted" {
		target.PerformedAt = time.Now().UTC()
		_, stagedTarget, err = repair.CreateTargetedReverification(root, target)
		if err != nil {
			t.Fatal(err)
		}
	}
	professionalTarget := map[string]any{"assertion_results": target.AssertionResults, "result": target.Result, "failure_class": target.FailureClass}
	if outcome == "interrupted" {
		before := repairDispatchRuntimeBytes(t, root)
		original := target.AssertionResults[0].EvidenceRefs
		target.AssertionResults[0].EvidenceRefs = []string{"logs/different-observation.txt"}
		writeRepairLeadJSON(t, paths[0], professionalTarget)
		diagnostic.Reset()
		runSavedRepairLeadQuestionnaire(root, repairLeadSaveInput(root, paths[0], "no-change-independent"), &diagnostic, &bytes.Buffer{})
		repairDispatchAssertUnchanged(t, root, before)
		if !strings.Contains(diagnostic.String(), "did not create") {
			t.Fatalf("interrupted Targeted replaced the original professional content: %s", &diagnostic)
		}
		target.AssertionResults[0].EvidenceRefs = original
	}
	writeRepairLeadJSON(t, paths[0], professionalTarget)
	diagnostic.Reset()
	input := policy.Input{Event: "SubagentStop", CWD: root, AgentID: "opaque-independent", LastAssistantMessage: paths[0]}
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("independent no-change Stop: %t %d %s", handled, code, &diagnostic)
	}
	closed := repairDispatchSnapshot(t, root)
	ptr := repairPointerCLI(closed.State)
	if outcome == "missing-assertion" {
		if ptr["status"] != "targeted_reverification" || stringValue(ptr["handoff_ref"]) != "" || integerValue(mapFieldCLI(closed.State, "review")["round"]) != 1 || !strings.Contains(stringValue(ptr["next_action"]), "root_invariant_assertions") {
			t.Fatalf("no-change skipped missing Contract observations: %+v", ptr)
		}
		return
	}
	if outcome == "interrupted" {
		refs := repairArtifactRefs(ptr, "targeted_reverification_artifacts", "", "")
		if len(refs) != 1 || refs[0].Path != stagedTarget.Path || refs[0].SHA256 != stagedTarget.SHA256 {
			t.Fatalf("interrupted Targeted retry replaced the original immutable source: %+v", refs)
		}
	}
	if ptr["status"] != "closed" || mapFieldCLI(closed.State, "lifecycle")["state"] != "verification" || integerValue(mapFieldCLI(closed.State, "review")["round"]) != 2 {
		t.Fatalf("no-change did not open a fresh ordinary S7: %+v %s", ptr, &diagnostic)
	}
	handoff, err := repair.ValidateRepairHandoff(root, repair.ArtifactRef{Path: stringValue(ptr["handoff_ref"]), SHA256: stringValue(ptr["handoff_sha256"])})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := repair.ValidateChangeset(root, handoff.ChangesetRef)
	if err != nil || changes.Artifacts == nil || len(changes.Artifacts) != 0 || changes.SessionID != stringValue(ptr["session_id"]) {
		t.Fatalf("Handoff did not preserve the actual empty Session changes: %+v %v", changes, err)
	}
	var seed map[string]any
	readRepairLeadJSON(t, resolveRootPath(root, stringValue(ptr["review_plan_seed_ref"])), &seed)
	if len(objectSliceAny(seed["frozen_subjects"])) == 0 {
		t.Fatal("new S7 has no actual frozen verification object")
	}
	if got := repairLeadGit(t, root, "rev-parse", "HEAD"); got != head {
		t.Fatalf("closure manufactured a commit: %s -> %s", head, got)
	}
	before = repairDispatchRuntimeBytes(t, root)
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); code != 0 {
		t.Fatalf("same independent result failed replay: %s", &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if _, err := os.Stat(resolveRootPath(root, handoff.ChangesetRef.Path)); err != nil {
		t.Fatal(err)
	}
}
