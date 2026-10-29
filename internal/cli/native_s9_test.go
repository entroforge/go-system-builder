package cli

import (
	"bytes"
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
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func nativeS9WorkerFixture(t *testing.T, assertionCopies ...int) (string, string, workpacket.RepairPlanReportPacket, policy.Input, workpacket.RepairResultProfessionalAnswer) {
	t.Helper()
	root, plan, _, _, division := repairExecutionPlanningFixture(t, assertionCopies...)
	writeRepairLeadFile(t, root, "go.mod", "module native.s9.fixture\n\ngo 1.26\n")
	repairLeadCommitFixture(t, root)
	feedback := repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	snap := repairDispatchSnapshot(t, root)
	packets := nativeS9PlanPackets(root, snap.State)
	if len(packets) != 1 || snap.State["workspace"] != nil {
		t.Fatalf("preparation created an execution: %s", feedback)
	}
	p := packets[0]
	brief, _, _ := nativeS9Paths(p)
	planPath, result, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if !strings.Contains(feedback, filepath.Join(root, brief)) || !strings.Contains(feedback, "private PlanReport output relative to the actual native worktree") || strings.Contains(feedback, "PlanReport answer: "+filepath.Join(root, p.AnswerPath)) || strings.Contains(feedback, "Main normally runs runtime workspace prepare") {
		t.Fatal(feedback)
	}
	worker := filepath.Join(t.TempDir(), "native-worker")
	repairLeadGit(t, root, "worktree", "add", "-b", "native-s9-test", worker, "HEAD")
	input := policy.Input{Event: "SubagentStart", AgentID: "opaque-native-id-1", SessionID: "parent-session", CWD: root, AgentType: "backend-builder"}
	before := repairDispatchRuntimeBytes(t, root)
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(out.String(), "hookEventName") {
		t.Fatalf("Start: %t %d %s %s", handled, code, &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	var draft workpacket.RepairPlanReportDraft
	readRepairLeadJSON(t, filepath.Join(root, p.AnswerPath), &draft)
	draft.Answer, _ = json.Marshal(workpacket.RepairPlanReportProfessionalAnswer{Plan: "Restore the first API authority and verify the discriminator", RedChecks: []workpacket.RepairPlanReportCheck{{Name: "first authority", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"internal/api/one.go"}}}, ProposedPaths: plan.Assignments[0].Scope})
	if err := os.MkdirAll(filepath.Dir(filepath.Join(worker, planPath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, planPath), draft.Answer); err != nil {
		t.Fatal(err)
	}
	writeRepairLeadFile(t, worker, "internal/api/one.go", "package api\n\nconst Restored = true\n")
	repairLeadGit(t, worker, "add", "internal/api/one.go")
	repairLeadGit(t, worker, "commit", "-qm", "Restore first authority")
	answer := workpacket.RepairResultProfessionalAnswer{Verdict: "completed", BeforeFixResults: []workpacket.RepairResultBeforeFixResult{}, UnitResults: []workpacket.RepairResultUnitResult{}, DetectionGapArtifacts: []string{}, EvidenceRefs: []string{"internal/api/one.go"}, ScopeDeviations: []string{}, ResidualRisks: []string{}}
	for _, id := range plan.Assignments[0].AssertionIDs {
		answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{AssertionID: id, ObservedRed: "synthetic fixture observation before repair", EvidenceRefs: []string{"internal/api/one.go"}})
	}
	for _, id := range plan.Assignments[0].UnitIDs {
		answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{"internal/api/one.go"}, ImplementationSummary: "Restored first API authority", RestoredInvariantRefs: []string{}})
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(worker, result)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
		t.Fatal(err)
	}
	input.LastAssistantMessage = filepath.Join(root, brief) + "\n" + filepath.Join(worker, result)
	return root, worker, p, input, answer
}

func TestNativeS9ActualWorktreeStopReceivesWithoutLauncherOrPlatformIdentity(t *testing.T) {
	root, worker, p, input, _ := nativeS9WorkerFixture(t)
	before := repairDispatchRuntimeBytes(t, root)
	var out, diagnostic bytes.Buffer
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "TeammateIdle"} {
		out.Reset()
		diagnostic.Reset()
		input.Event = event
		if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || out.Len() != 0 {
			t.Fatalf("ordinary %s: %t %d %s %s", event, handled, code, &out, &diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
	input.Event = "SubagentStop"
	input.CWD = root // Real platform payloads may keep the parent CWD.
	out.Reset()
	diagnostic.Reset()
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || out.Len() != 0 {
		t.Fatalf("Stop: %t %d %s %s", handled, code, &out, &diagnostic)
	}
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil {
		t.Fatalf("returned Git source was not recorded: %v %s", err, &diagnostic)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	if e.Path != worker || e.DeliveryRef == "" || e.PlatformSessionID != "" || e.BootstrapSHA256 != "" || e.AgentID == input.AgentID {
		t.Fatalf("native delivery was replaced by execution management: %#v %s", e, &diagnostic)
	}
	for _, path := range []string{".claude/workspace-launch", ".claude/loop-bootstrap.json"} {
		if _, err := os.Stat(filepath.Join(worker, path)); !os.IsNotExist(err) {
			t.Fatalf("native Worker gained %s", path)
		}
	}
	before = repairDispatchRuntimeBytes(t, root)
	input.AgentID = "opaque-native-id-after-resume"
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("same delivery after native resume: %s", &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	// Main verifies and merges through the original independent consumer.
	out.Reset()
	diagnostic.Reset()
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "go test ./internal/api && git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic); code != 0 {
		t.Fatalf("original Main integration: %d %s %s", code, &out, &diagnostic)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("original integration did not clean the received worktree: %v", err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("late Stop after cleanup reopened delivery: %s", &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	// A dependent responsibility selected after the first integration must
	// also receive a native brief, without falling back to workspace prepare.
	plan, err := repairDispatchPlan(root, repairDispatchSnapshot(t, root).State)
	if err != nil {
		t.Fatal(err)
	}
	division := repairDispatchPaths(root, repairDispatchSnapshot(t, root).State)[0]
	feedback := repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[1].AssignmentID, RoleFamily: "backend-builder"}})
	if !strings.Contains(feedback, "exact S9 brief") || strings.Contains(feedback, "Main runs through a normal tool") {
		t.Fatalf("late native responsibility fell back to legacy preparation: %s", feedback)
	}
	latest := repairDispatchSnapshot(t, root)
	if len(nativeS9PlanPackets(root, latest.State)) != 2 {
		t.Fatal("late native Plan question/brief missing")
	}
	if registry, err := workspace.Decode(latest.State); err != nil || len(registry.Executions) != 1 {
		t.Fatalf("late dispatch precreated an execution: %v", err)
	}
}

func TestNativeS9ObservesActualGitBaseAfterDependencyAdvancesMain(t *testing.T) {
	root, worker, p, input, _ := nativeS9WorkerFixture(t)
	prepared := repairLeadGit(t, root, "rev-parse", "HEAD")
	writeRepairLeadFile(t, root, "internal/api/two.go", "package api\nconst DependencyReady = true\n")
	repairLeadGit(t, root, "add", "internal/api/two.go")
	repairLeadGit(t, root, "commit", "-qm", "Integrate predecessor")
	actualBase := repairLeadGit(t, root, "rev-parse", "HEAD")
	repairLeadGit(t, worker, "rebase", "--onto", actualBase, prepared)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("native source with newer dependency base rejected: %t %d %s", handled, code, &diagnostic)
	}
	registry, err := workspace.Decode(repairDispatchSnapshot(t, root).State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].BaseCommit != actualBase {
		t.Fatalf("prepared baseline was mistaken for actual native worktree base: %v", err)
	}
}

func TestNativeS9BlockerResumesThroughOriginalResolutionWithoutPrepare(t *testing.T) {
	root, worker, p, input, complete := nativeS9WorkerFixture(t)
	result := nativeS9TestResultPath(t, root, p)
	blocked := complete
	blocked.Verdict, blocked.ResidualRisks = "blocked", []string{"The independent check environment is temporarily unavailable"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), blocked); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("real blocker not received: %t %d %s", handled, code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), snapshot.State)
	if err != nil || len(packets) != 1 || workstate.AgentActivity(contextAgent(snapshot.State, p.Binding.OwnerAgentID)) != "blocked" {
		t.Fatalf("original blocker not recorded: %v %s", err, &diagnostic)
	}
	resolution, err := workpacket.RepairResultResolutionAuthoringPath(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	readRepairLeadJSON(t, filepath.Join(root, resolution), &draft)
	mapFieldCLI(draft, "s9_resolution")["rationale"] = "The check environment is available again; continue the same Contract responsibility"
	if err := persistRepairLeadQuestionnaire(filepath.Join(root, resolution), draft); err != nil {
		t.Fatal(err)
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	receipt, err := service.SaveRepairResultResolutionFrom(t.Context(), filepath.Join(root, resolution), nil)
	if err != nil || !receipt.Accepted || strings.Contains(receipt.NextAction, "显式运行原 runtime workspace prepare") || !strings.Contains(receipt.NextAction, "原生 resume") {
		t.Fatalf("native resolution returned legacy preparation: %+v %v", receipt, err)
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), complete); err != nil {
		t.Fatal(err)
	}
	input.AgentID = "new-platform-actor-after-resume"
	out.Reset()
	diagnostic.Reset()
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("native resume not received: %t %d %s", handled, code, &diagnostic)
	}
	snapshot = repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("native successor candidate missing: %v %s", err, &diagnostic)
	}
}

func TestNativeS9MalformedProfessionalArtifactIsNotReceived(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	result := nativeS9TestResultPath(t, root, p)
	data, _ := json.Marshal(answer)
	var malformed map[string]any
	if err := json.Unmarshal(data, &malformed); err != nil {
		t.Fatal(err)
	}
	malformed["detection_gap_artifacts"] = []any{map[string]any{"path": "internal/api/one.go"}}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), malformed); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	// Go versions differ in whether UnmarshalTypeError.Field includes a slice
	// index. Require the actual field and expected type, not that formatting.
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(diagnostic.String(), "detection_gap_artifacts") || !strings.Contains(diagnostic.String(), "string") {
		t.Fatalf("malformed professional field lacks exact feedback: %t %d %s", handled, code, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	summary, err := nativeReturnSummary(root, repairDispatchSnapshot(t, root), nativeReturnSubject{kind: "s9-plan-report-v1", id: p.ID})
	if err != nil || !strings.Contains(summary, "detection_gap_artifacts") || !strings.Contains(summary, filepath.Join(worker, result)) || !strings.Contains(summary, p.Binding.OwnerAgentID) {
		t.Fatalf("Main lost exact field/path/owner diagnostic: %v %s", err, summary)
	}

}

func TestNativeS9NullProfessionalReferenceIsNotConvertedToNotApplicable(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	result := nativeS9TestResultPath(t, root, p)
	data, _ := json.Marshal(answer)
	var malformed map[string]any
	if err := json.Unmarshal(data, &malformed); err != nil {
		t.Fatal(err)
	}
	malformed["migration_ref"] = nil
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), malformed); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(diagnostic.String(), "migration_ref") {
		t.Fatalf("unknown reference became an empty declaration: %t %d %s", handled, code, &diagnostic)
	}
	registry, err := workspace.Decode(repairDispatchSnapshot(t, root).State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef != "" {
		t.Fatalf("null reference created a candidate: %v", err)
	}
}

func TestNativeS9UnknownAndAmbiguousReferencesNeverClaimResponsibility(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	rows := []repairDispatchChoice{}
	for _, a := range plan.Assignments {
		rows = append(rows, repairDispatchChoice{AssignmentID: a.AssignmentID, RoleFamily: "backend-builder"})
	}
	repairDispatchSave(t, root, division, rows)
	snap := repairDispatchSnapshot(t, root)
	packets := nativeS9PlanPackets(root, snap.State)
	if len(packets) != 2 {
		t.Fatalf("missing independent native briefs: %d", len(packets))
	}
	before := repairDispatchRuntimeBytes(t, root)
	var text string
	for _, p := range packets {
		brief, _, _ := nativeS9Paths(p)
		text += filepath.Join(root, brief) + "\n"
	}
	for _, body := range []string{"unrelated native exploration", text} {
		var out, diagnostic bytes.Buffer
		if handled, code := nativeS9Hook(root, policy.Input{Event: "SubagentStop", AgentID: "opaque-native-id", CWD: root, LastAssistantMessage: body}, &out, &diagnostic); !handled || code != 0 {
			t.Fatalf("unknown/ambiguous return: %t %d %s", handled, code, &diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}

func TestNativeS9MissingPreparationKeepsMainOnOriginalDivision(t *testing.T) {
	root, _, _, _, division := repairExecutionPlanningFixture(t)
	before := repairDispatchRuntimeBytes(t, root)
	snapshot := repairDispatchSnapshot(t, root)
	if len(nativeS9PlanPackets(root, snapshot.State)) != 0 {
		t.Fatal("fixture already has a native brief")
	}
	context, handled := s9PacketQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, snapshot)
	if !handled || !strings.Contains(context, division) || !strings.Contains(context, "Save selected rows") || !strings.Contains(context, "Claude Code native Agent") {
		t.Fatalf("missing original Main preparation recovery: %t %s", handled, context)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9MissingWorktreePlanDoesNotBorrowMainAnswer(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "current-private"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			fixture := nativeS9WorkerFixture
			if legacy {
				fixture = nativeS9LegacyWorkerFixture
			}
			root, worker, p, input, _ := fixture(t)
			plan, result := nativeS9TestAuthorPaths(t, root, p)
			local := filepath.Join(worker, plan)
			raw, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			mainDraft := p.Draft
			mainDraft.Answer = raw
			mainPath := filepath.Join(root, p.AnswerPath)
			if err := persistRepairLeadQuestionnaire(mainPath, mainDraft); err != nil {
				t.Fatal(err)
			}
			mainBefore, err := os.ReadFile(mainPath)
			if err != nil {
				t.Fatal(err)
			}
			resultPath := filepath.Join(worker, result)
			resultBefore, err := os.ReadFile(resultPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			handled, code := nativeS9Hook(root, input, &out, &diagnostic)
			want := "native private original must be exactly untracked"
			if legacy {
				want = "Main's draft cannot substitute"
			}
			for _, detail := range []string{want, local, "Main owns recovery"} {
				if !handled || code != 0 || !strings.Contains(diagnostic.String(), detail) {
					t.Fatalf("missing actual source borrowed Main or lost %q: %t %d %s %s", detail, handled, code, &out, &diagnostic)
				}
			}
			repairDispatchAssertUnchanged(t, root, before)
			for path, expected := range map[string][]byte{mainPath: mainBefore, resultPath: resultBefore} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, expected) {
					t.Fatalf("missing Plan changed original %s: %v", path, err)
				}
			}
			service, err := submission.NewReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			if receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID); err != nil || receipt.Accepted {
				t.Fatalf("missing Plan received a receipt: %+v %v", receipt, err)
			}
			registry, err := workspace.Decode(repairDispatchSnapshot(t, root).State)
			if err != nil || registry != nil && len(registry.Executions) != 0 {
				t.Fatalf("missing Plan fabricated a candidate: %+v %v", registry, err)
			}
		})
	}
}

func TestNativeS9ActualBlockerCanReturnWithoutPrivatePlan(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	plan, result := nativeS9TestAuthorPaths(t, root, p)
	missingPlan := filepath.Join(worker, plan)
	if err := os.Remove(missingPlan); err != nil {
		t.Fatal(err)
	}
	answer.Verdict = "blocked"
	answer.ResidualRisks = []string{"actual dependency unavailable; Main must supply it before repair"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(diagnostic.String(), "Main owns recovery") || !strings.Contains(diagnostic.String(), "native private original must be exactly untracked") || !strings.Contains(diagnostic.String(), missingPlan) {
		t.Fatalf("actual blocker forced a successful Plan: %t %d %s %s", handled, code, &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9ReceiverSourceFailureBelongsToMain(t *testing.T) {
	root, _, p, input, _ := nativeS9WorkerFixture(t)
	_, pin, _ := nativeS9Paths(p)
	if err := os.Remove(filepath.Join(root, pin)); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(diagnostic.String(), "Main owns recovery") || !strings.Contains(diagnostic.String(), pin) {
		t.Fatalf("receiver failure sent back to author: %t %d %s", handled, code, &diagnostic)
	}
	state := repairDispatchSnapshot(t, root).State
	if registry, err := workspace.Decode(state); err != nil {
		t.Fatal(err)
	} else if registry != nil && len(registry.Executions) != 0 {
		t.Fatal("source failure fabricated a delivery")
	}
}

func TestNativeS9ResumeReceivesLatestActualTaskWithoutToolResultGuess(t *testing.T) {
	root, _, p, input, _ := nativeS9WorkerFixture(t)
	refs := input.LastAssistantMessage
	input.LastAssistantMessage = "Finished the resumed responsibility."
	input.Event = "SubagentStop"
	input.AgentTranscriptPath = filepath.Join(t.TempDir(), "resume.jsonl")
	var rows bytes.Buffer
	appendRow := func(content any) {
		data, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		rows.Write(data)
		rows.WriteByte('\n')
	}
	appendRow("Earlier unrelated task /retired/brief.md")
	appendRow([]map[string]string{{"type": "tool_result", "text": refs}})
	call := func() (bool, int, string) {
		if err := os.WriteFile(input.AgentTranscriptPath, rows.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		var out, diagnostic bytes.Buffer
		handled, code := nativeS9Hook(root, input, &out, &diagnostic)
		return handled, code, diagnostic.String()
	}
	before := repairDispatchRuntimeBytes(t, root)
	if handled, code, output := call(); !handled || code != 0 {
		t.Fatalf("unrelated return: %t %d %s", handled, code, output)
	}
	repairDispatchAssertUnchanged(t, root, before)
	appendRow("Resumed actual repair: " + refs)
	if handled, code, output := call(); !handled || code != 0 || !strings.Contains(output, "professional Result received") {
		t.Fatalf("latest actual repair was not received: %t %d %s", handled, code, output)
	}
	registry, err := workspace.Decode(repairDispatchSnapshot(t, root).State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].Purpose != workspace.NativeDeliveryPurpose {
		t.Fatalf("missing original native delivery: %+v %v", registry, err)
	}
}

func TestNativeMainReturnShowsOriginalS9CandidateWithoutPrivateAnswers(t *testing.T) {
	root, worker, p, input, _ := nativeS9WorkerFixture(t)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("native Stop: %t %d %s", handled, code, &diagnostic)
	}
	plan, result := nativeS9TestAuthorPaths(t, root, p)
	for _, ref := range []string{plan, result} {
		if err := os.Remove(filepath.Join(worker, ref)); err != nil {
			t.Fatal(err)
		}
	}
	brief, _, _ := nativeS9Paths(p)
	before := repairDispatchRuntimeBytes(t, root)
	out.Reset()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(root, brief)}})
	if code := Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("Main return: %d %s", code, &diagnostic)
	}
	var message struct {
		Output struct {
			Text string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &message); err != nil || !strings.Contains(message.Output.Text, `"accepted":true`) || !strings.Contains(message.Output.Text, "Main") {
		t.Fatalf("Main missed original candidate: %v %s", err, &out)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9MissingBriefDoesNotTurnAcceptedPlanIntoLegacyStartup(t *testing.T) {
	root, _, packet, input, _ := nativeS9WorkerFixture(t)
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("native Stop: %t %d %s", handled, code, &diagnostic)
	}
	brief, _, _ := nativeS9Paths(packet)
	if err := os.Remove(resolveRootPath(root, brief)); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	receipt, err := service.SaveRepairPlanReportFrom(contextForHook(input), packet.Binding.OwnerAgentID, filepath.Join(root, packet.AnswerPath), root, nil)
	if err != nil || !receipt.Accepted || !strings.Contains(receipt.NextAction, "Main/系统恢复") || !strings.Contains(receipt.NextAction, filepath.Join(root, brief)) || strings.Contains(receipt.NextAction, "runtime workspace prepare") || strings.Contains(receipt.NextAction, "runtime workspace bind") {
		t.Fatalf("missing native material became a legacy startup requirement: %+v %v", receipt, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9MissingCurrentPacketDoesNotRequestLegacyExecutionChoice(t *testing.T) {
	// Historical Plan facts alone are not a registered frozen execution.
	// Packet-enabled normal reminders retain the original preparation owner.
	root, _, path, _ := repairExecutionFixture(t, true)
	snapshot := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	got := stageRepairExecutionPrepareContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, snapshot)
	if !strings.Contains(got, path) || !strings.Contains(got, "Main/system must restore") || strings.Contains(got, "awaits check_location") || strings.Contains(got, "runtime workspace prepare") || strings.Contains(got, "runtime workspace bind") {
		t.Fatalf("missing native material became another execution questionnaire/startup: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9FirstIncompleteStopReturnsExactGapToMainWithoutAcceptance(t *testing.T) {
	for _, missing := range []string{"Result", "PlanReport"} {
		t.Run(missing, func(t *testing.T) {
			root, worker, p, input, _ := nativeS9WorkerFixture(t)
			brief, _, _ := nativeS9Paths(p)
			plan, result := nativeS9TestAuthorPaths(t, root, p)
			local := filepath.Join(worker, result)
			if missing == "PlanReport" {
				local = filepath.Join(worker, plan)
			}
			original, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || !strings.Contains(diagnostic.String(), "Main owns recovery") || !strings.Contains(diagnostic.String(), local) {
				t.Fatalf("first incomplete Stop: %t %d %s", handled, code, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
			snap := repairDispatchSnapshot(t, root)
			summary, err := nativeReturnSummary(root, snap, nativeReturnSubject{kind: "s9-plan-report-v1", id: p.ID, ref: filepath.Join(root, brief)})
			if err != nil || !strings.Contains(summary, local) || !strings.Contains(summary, "Consumer diagnostic") || !strings.Contains(summary, "Neither PlanReport nor Builder Result is accepted") {
				t.Fatalf("Main lacks exact incomplete reception: %v %s", err, summary)
			}
			if strings.Contains(summary, "后台") {
				t.Fatalf("unfinished responsibility treated as running: %s", summary)
			}
			// A repaired private artifact does not erase the captured historical
			// attempt before a new Stop receives it. Display is not revalidation;
			// changed responsibility/source bindings still suppress old context.
			if err := os.WriteFile(local, original, 0600); err != nil {
				t.Fatal(err)
			}
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); !strings.Contains(got, local) || !strings.Contains(got, "observed then, not revalidated") {
				t.Fatalf("private correction erased or revalidated historical context: %s", got)
			}
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			snap.Revision++
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); !strings.Contains(got, local) {
				t.Fatalf("unrelated reception/revision erased the original diagnostic: %s", got)
			}
			pointer := repairPointerCLI(snap.State)
			originalPlan := pointer["plan_ref"]
			pointer["plan_ref"] = "replacement-plan.json"
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); got != "" {
				t.Fatalf("old diagnostic survived a replaced source: %s", got)
			}
			pointer["plan_ref"] = originalPlan
			owners := mapFieldCLI(pointer, "assignment_owners")
			owners["unrelated-received-assignment"] = "other-builder"
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); !strings.Contains(got, local) {
				t.Fatalf("another assignment erased this responsibility's diagnostic: %s", got)
			}
			delete(owners, "unrelated-received-assignment")
			originalOwner := owners[p.Binding.RepairAssignmentID]
			owners[p.Binding.RepairAssignmentID] = "replacement-builder"
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); got != "" {
				t.Fatalf("old diagnostic survived a replaced responsibility: %s", got)
			}
			owners[p.Binding.RepairAssignmentID] = originalOwner
			if got := nativeS9ReturnDiagnostic(root, snap, p.ID); !strings.Contains(got, local) {
				t.Fatalf("restored original coordinates lost the unchanged diagnostic: %s", got)
			}
			if err := os.WriteFile(local, original, 0600); err != nil {
				t.Fatal(err)
			}
			diagnostic.Reset()
			if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
				t.Fatal(diagnostic.String())
			}
			accepted := repairDispatchSnapshot(t, root)
			if got := nativeS9ReturnDiagnostic(root, accepted, p.ID); got != "" {
				t.Fatalf("accepted Result retained old failure: %s", got)
			}
			service, err := submission.NewReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			packets, err := runtime.CurrentRepairResultPackets(root, accepted.State, p.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("Result packet missing: %v", err)
			}
			receipt, err := service.RepairResultReceipt(p.Binding.OwnerAgentID, packets[0].ID)
			if err != nil || !receipt.Accepted {
				t.Fatalf("restored original responsibility not accepted: %v %#v", err, receipt)
			}
		})
	}
}
