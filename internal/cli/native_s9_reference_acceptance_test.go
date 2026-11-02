package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// References come from actual issued responsibilities. Alias spelling is only
// responsibility selection; it supplies no actor, checkout or Result evidence.
func TestNativeS9ResponsibilityAliasesAndLatestActualTask(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	choices := []repairDispatchChoice{}
	for _, assignment := range plan.Assignments {
		choices = append(choices, repairDispatchChoice{AssignmentID: assignment.AssignmentID, RoleFamily: "backend-builder"})
	}
	repairDispatchSave(t, root, division, choices)
	routes, err := nativeS9Routes(t.Context(), repairDispatchSnapshot(t, root).State)
	if err != nil || len(routes) != 2 {
		t.Fatalf("original issued responsibilities: %+v %v", routes, err)
	}
	relative := nativeS9RouteBrief(routes[0])
	absolute := filepath.Join(root, relative)
	aliases := nativeS9ResponsibilityRefs(root, relative)
	if len(aliases) != 3 {
		t.Fatalf("expected only three supported spellings: %+v", aliases)
	}
	seen := map[string]bool{}
	for _, ref := range aliases {
		seen[ref] = true
	}
	for _, ref := range []string{absolute, relative, "./" + relative} {
		for _, suffix := range []string{"", ".", ". 下一句", ".\tNext", ".\nNext", "`。"} {
			if !seen[ref] || !nativeS9ResponsibilityMatch("Original task: `"+ref+suffix, root, relative) {
				t.Errorf("original responsibility spelling not selected: %q (%+v)", ref+suffix, aliases)
			}
		}
	}
	for _, ref := range []string{
		filepath.Base(relative), "../" + relative, "prefix" + relative,
		relative + ".old", relative + ".backup", relative + "./child", relative + "..", relative + "/extra", "././" + relative,
		"/arbitrary/" + relative + ".", "prefix" + relative + ".",
		filepath.Join(t.TempDir(), relative), "https://example.invalid/" + relative,
		"file://" + absolute, "$MAIN_ROOT/" + relative, "${MAIN_ROOT}/" + relative,
	} {
		if nativeS9ResponsibilityMatch(ref, root, relative) {
			t.Errorf("unsupported reference selected original: %q", ref)
		}
	}
	selectCount := func(text string) int {
		count := 0
		for _, route := range routes {
			if nativeS9ResponsibilityMatch(text, root, nativeS9RouteBrief(route)) {
				count++
			}
		}
		return count
	}
	if got := selectCount(strings.Join(aliases, "\n")); got != 1 {
		t.Fatalf("equivalent references became multiple responsibilities: %d", got)
	}
	other := nativeS9RouteBrief(routes[1])
	if got := selectCount(relative + ". 下一句\n./" + other + "."); got != 2 {
		t.Fatalf("different original responsibilities lost ambiguity: %d", got)
	}

	// Latest actual user text must be searched with the same alias set used
	// for final selection. Assistant and tool_result quotations confer nothing.
	data := nativeTranscriptTestRow("user", "Original assignment "+absolute)
	data = append(data, nativeTranscriptTestRow("user", "Resumed current assignment ./"+relative+". 下一句")...)
	data = append(data, nativeTranscriptTestRow("assistant", "Quoted obsolete assignment "+absolute)...)
	data = append(data, nativeTranscriptTestRow("user", []map[string]string{{"type": "tool_result", "text": "Quoted obsolete assignment " + absolute}})...)
	reader, _ := nativeTranscriptTestReader(t, data)
	sources, diagnostic := reader.taskSourcesMatching("Finished", aliases, true, nativeS9BriefReferenceMatch)
	text := sources.text()
	if diagnostic.Reason != "latest_exact_task" || !strings.Contains(text, "Resumed current assignment ./"+relative+". 下一句") || strings.Contains(text, "Original assignment") || strings.Contains(text, "Quoted obsolete") {
		t.Fatalf("latest actual user alias not selected: %q %+v", text, diagnostic)
	}
	transcript := filepath.Join(t.TempDir(), "latest-s9-user-task.jsonl")
	if err := os.WriteFile(transcript, data, 0600); err != nil {
		t.Fatal(err)
	}
	actual := nativeS9TaskSources(policy.Input{AgentTranscriptPath: transcript, LastAssistantMessage: "Finished"}, root, []string{absolute}, absolute)
	if !strings.Contains(actual.Task, "Resumed current assignment ./"+relative+". 下一句") || strings.Contains(actual.Task, "Original assignment") {
		t.Fatalf("production latest-task search differs from registered brief matching: %+v", actual)
	}
	projected := actual.s9ResponsibilityText(root, []string{absolute}, absolute)
	if !nativeS6ExactReference(projected, absolute) {
		t.Fatalf("latest task not projected for final responsibility selection: %q", projected)
	}
	// Neither shared exact references nor non-brief responsibilities acquire
	// sentence punctuation from this Builder-only user-task rule.
	if nativeS6ExactReference(absolute+".", absolute) || nativeS9TaskResponsibilityMatch(relative+". Next", root, absolute, nil) {
		t.Fatal("S9 brief punctuation escaped into exact Result/Targeted references")
	}
}

func s9ReferencePublicHook(t *testing.T, root string, in policy.Input) (string, string) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
		t.Fatalf("public %s: %d %s %s", in.Event, code, &out, &diagnostic)
	}
	if in.Event == "PreToolUse" || in.Event == "PostToolUse" {
		nativeS9ListenerFeedback(t, &out) // Reception must not deny the ordinary tool.
	}
	return out.String(), diagnostic.String()
}

func TestNativeS9RelativePublicEventsReceiveOriginalPlan(t *testing.T) {
	// One representative alias exercises each original event. Spelling and
	// boundaries above do not multiply this Git/Writer integration fixture.
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, worker, packet, in, _ := nativeS9WorkerFixture(t)
			planPath, result, err := nativeS9AuthorPaths(root, packet)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(worker, result)); err != nil {
				t.Fatal(err)
			}
			brief := nativeS9BriefPath(packet)
			in.Event, in.CWD, in.ToolName = event, worker, "Read"
			in.ToolInput = map[string]any{"file_path": "internal/api/one.go"}
			ref := brief
			if event == "PreToolUse" {
				ref = filepath.Join(root, brief)
			}
			if event == "SubagentStop" {
				ref = "./" + brief
			}
			in.LastAssistantMessage = "Use original Main responsibility " + ref
			nativeS9SplitChild(t, &in)
			in.LastAssistantMessage = "Still working on the assigned repair."
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || receipt.Accepted {
				t.Fatalf("fixture already accepted Plan: %+v %v", receipt, err)
			}
			if event == "PostToolUse" {
				// A successful save Post identifies the exact private artifact.
				in.ToolName = "Write"
				content, err := os.ReadFile(filepath.Join(worker, planPath))
				if err != nil {
					t.Fatal(err)
				}
				in.ToolInput = map[string]any{"file_path": filepath.Join(worker, planPath), "content": string(content)}
				before := repairDispatchRuntimeBytes(t, root)
				in.ToolResponse = map[string]any{"is_error": true}
				s9ReferencePublicHook(t, root, in)
				repairDispatchAssertUnchanged(t, root, before)
				if err := os.WriteFile(filepath.Join(worker, planPath), content, 0600); err != nil {
					t.Fatal(err)
				}
				in.ToolResponse = map[string]any{"is_error": false}
			}
			s9ReferencePublicHook(t, root, in)
			if receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || !receipt.Accepted {
				t.Fatalf("relative %s missed original Plan consumer: %+v %v", event, receipt, err)
			}
			if _, err := os.Stat(filepath.Join(worker, result)); !os.IsNotExist(err) {
				t.Fatalf("Plan-only event fabricated Result: %v", err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			s9ReferencePublicHook(t, root, in)
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestNativeS9RelativeMainCompletedKeepsOriginalLiveSourceContract(t *testing.T) {
	root, worker, packet, stop, _ := nativeS9WorkerFixture(t)
	unknown := "unreceived-reference-test.txt"
	writeRepairLeadFile(t, worker, unknown, "not an accepted product contribution\n")
	stop.Event, stop.CWD = "SubagentStop", worker
	nativeS9SplitChild(t, &stop)
	s9ReferencePublicHook(t, root, stop)
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original incomplete execution: %+v %v", registry, err)
	}
	execution := registry.Executions[packet.Binding.ManifestAssignmentID]
	// The Worker's own Stop falls back to the author's naturally saved original
	// answer and hands the real result over. The unknown file in the checkout is
	// not part of the accepted contribution.
	if execution.Path != worker || execution.DeliveryRef == "" {
		t.Fatalf("original Stop did not hand off the received candidate: %+v", execution)
	}
	if err := os.Remove(filepath.Join(worker, unknown)); err != nil {
		t.Fatal(err)
	}
	brief := nativeS9BriefPath(packet)
	in := policy.Input{Event: "PostToolUse", CWD: root, ToolName: "Agent", ToolUseID: "relative-completed-return", ToolInput: map[string]any{"prompt": "Continue original assigned repair ./" + brief + ". 下一句"}, ToolResponse: map[string]any{"status": "completed", "agentId": stop.AgentID, "worktreePath": worker, "worktreeBranch": execution.Branch}}
	before := repairDispatchRuntimeBytes(t, root)
	delete(in.ToolResponse, "worktreePath")
	in.ToolResponse["text"] = "worktreePath: " + worker
	s9ReferencePublicHook(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
	in.ToolResponse["worktreePath"] = worker
	in.ToolInput["prompt"] = "Unrelated task"
	in.ToolResponse["text"] = brief
	s9ReferencePublicHook(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
	in.ToolInput["prompt"] = "Continue original assigned repair ./" + brief + ". 下一句"
	s9ReferencePublicHook(t, root, in)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, repairDispatchSnapshot(t, root).State, packet.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result packet: %+v %v", packets, err)
	}
	if receipt, err := service.RepairResultReceipt(packet.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("relative completed lacks original Candidate receipt: %+v %v", receipt, err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	s9ReferencePublicHook(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9RelativeTargetedPublicEventUsesIndependentOriginal(t *testing.T) {
	// This fixture has synthetic domain Result/Impact prerequisites. It proves
	// Targeted routing and original Writer reception, not an end-to-end repair.
	root, path, draft := nativeS9TargetedListenerFixture(t)
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	in := policy.Input{Event: "PreToolUse", CWD: root, AgentID: "relative-independent-child", ToolName: "Read", ToolInput: map[string]any{"file_path": "internal/api/one.go"}, LastAssistantMessage: "Independently verify original questionnaire ./" + filepath.ToSlash(relative)}
	nativeS9SplitChild(t, &in)
	in.LastAssistantMessage = ""
	before := repairDispatchRuntimeBytes(t, root)
	strict := in
	strict.LastAssistantMessage = "Independent questionnaire ./" + filepath.ToSlash(relative) + ". 下一句"
	nativeS9SplitChild(t, &strict)
	strict.LastAssistantMessage = ""
	s9ReferencePublicHook(t, root, strict)
	repairDispatchAssertUnchanged(t, root, before)
	// A relative responsibility reference cannot grant source ownership. The
	// authority route names the responsibility; the caller's checkout neither
	// grants it nor blocks it. Old C/D source verification is not a gate.
	worker := filepath.Join(t.TempDir(), "unrelated-targeted")
	repairLeadGit(t, root, "worktree", "add", "-b", "relative-targeted-other", worker, "HEAD")
	in.CWD = worker
	s9ReferencePublicHook(t, root, in)
	refs := repairArtifactRefs(repairPointerCLI(repairDispatchSnapshot(t, root).State), "targeted_reverification_artifacts", "")
	if len(refs) != 1 {
		t.Fatalf("relative independent original lacks durable observation: %+v", refs)
	}
	received, err := repair.ValidateTargetedReverification(root, refs[0])
	if err != nil || received.PerformingAssignmentID != draft.PerformingAssignmentID || received.Result != draft.Result || len(received.AssertionResults) != len(draft.AssertionResults) {
		t.Fatalf("original independent consumer received different observations: %+v %v", received, err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	in.CWD = root
	s9ReferencePublicHook(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
	s9ReferencePublicHook(t, root, in)
	repairDispatchAssertUnchanged(t, root, before)
}

// A cross-role fixture issues the Builder packet while still planning. The
// existing Targeted-only fixture deliberately uses a legacy Builder, so it
// cannot prove ambiguity with an issued native Builder responsibility.
func TestNativeS9RelativeMixedOriginalResponsibilitiesRemainAmbiguous(t *testing.T) {
	root, path, _ := s9ReferenceMixedTargetedFixture(t)
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := nativeS9Routes(t.Context(), repairDispatchSnapshot(t, root).State)
	if err != nil || len(routes) != 1 {
		t.Fatalf("original issued Builder: %+v %v", routes, err)
	}
	in := policy.Input{Event: "PreToolUse", CWD: root, AgentID: "mixed-independent-child", ToolName: "Read", ToolInput: map[string]any{"file_path": "internal/api/one.go"}, LastAssistantMessage: "Independent questionnaire ./" + filepath.ToSlash(relative) + "\nBuilder background " + nativeS9RouteBrief(routes[0]) + ". 下一句"}
	nativeS9SplitChild(t, &in)
	in.LastAssistantMessage = ""
	before := repairDispatchRuntimeBytes(t, root)
	_, diagnostic := s9ReferencePublicHook(t, root, in)
	if !strings.Contains(diagnostic, "both Builder and Targeted") {
		t.Fatalf("mixed original responsibilities lost ambiguity: %s", diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

// New relative spellings belong only to a real user task. The historical
// absolute-final Stop route still receives the original Plan and Candidate
// without requiring a readable task transcript or opaque actor ownership.
func TestNativeS9RelativeFinalAndQuotedTasksDoNotAuthorizeStop(t *testing.T) {
	root, worker, packet, in, _ := nativeS9WorkerFixture(t)
	brief := nativeS9BriefPath(packet)
	in.Event, in.CWD = "SubagentStop", worker
	before := repairDispatchRuntimeBytes(t, root)
	for _, ref := range []string{brief, "./" + brief, brief + ".", "./" + brief + ".", filepath.Join(root, brief) + "."} {
		in.LastAssistantMessage = "Finished original responsibility " + ref
		in.AgentTranscriptPath = ""
		s9ReferencePublicHook(t, root, in)
		repairDispatchAssertUnchanged(t, root, before)
	}
	in.LastAssistantMessage = "Actual unrelated user task"
	nativeS9SplitChild(t, &in)
	original, err := os.ReadFile(in.AgentTranscriptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, quoted := range [][]byte{
		nativeTranscriptTestRow("assistant", "Quoted original ./"+brief),
		nativeTranscriptTestRow("user", []map[string]string{{"type": "tool_result", "text": "Quoted original ./" + brief}}),
	} {
		if err := os.WriteFile(in.AgentTranscriptPath, append(append([]byte(nil), original...), quoted...), 0600); err != nil {
			t.Fatal(err)
		}
		in.LastAssistantMessage = "Finished ./" + brief
		s9ReferencePublicHook(t, root, in)
		repairDispatchAssertUnchanged(t, root, before)
	}
	in.AgentTranscriptPath = ""
	in.LastAssistantMessage = filepath.Join(root, brief)
	s9ReferencePublicHook(t, root, in)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || !receipt.Accepted {
		t.Fatalf("original absolute-final Plan fallback: %+v %v", receipt, err)
	}
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, repairDispatchSnapshot(t, root).State, packet.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result packet: %+v %v", packets, err)
	}
	if receipt, err := service.RepairResultReceipt(packet.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("original absolute-final Candidate fallback: %+v %v", receipt, err)
	}
}

func TestNativeS9ActualTaskAndAbsoluteFinalSelectTogether(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	var choices []repairDispatchChoice
	for _, assignment := range plan.Assignments {
		choices = append(choices, repairDispatchChoice{AssignmentID: assignment.AssignmentID, RoleFamily: "backend-builder"})
	}
	repairDispatchSave(t, root, division, choices)
	routes, err := nativeS9Routes(t.Context(), repairDispatchSnapshot(t, root).State)
	if err != nil || len(routes) != 2 {
		t.Fatalf("original issued routes: %+v %v", routes, err)
	}
	first, second := nativeS9RouteBrief(routes[0]), nativeS9RouteBrief(routes[1])
	in := policy.Input{Event: "PreToolUse", CWD: root, AgentID: "task-final-child", ToolName: "Read", ToolInput: map[string]any{"file_path": "internal/api/one.go"}, LastAssistantMessage: "Original task ./" + first + ". 下一句"}
	nativeS9SplitChild(t, &in)
	in.LastAssistantMessage = filepath.Join(root, second)
	before := repairDispatchRuntimeBytes(t, root)
	_, diagnostic := s9ReferencePublicHook(t, root, in)
	if !strings.Contains(diagnostic, "multiple exact briefs") {
		t.Fatalf("task and original absolute final lost ambiguity: %s", diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	in.LastAssistantMessage = filepath.Join(root, first)
	sources := nativeS9TaskSources(in, root, []string{filepath.Join(root, first), filepath.Join(root, second)}, filepath.Join(root, first), filepath.Join(root, second))
	text := sources.s9ResponsibilityText(root, []string{filepath.Join(root, first), filepath.Join(root, second)}, filepath.Join(root, first), filepath.Join(root, second))
	count := 0
	for _, route := range routes {
		if nativeS6ExactReference(text, filepath.Join(root, nativeS9RouteBrief(route))) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("same original across task/final was not deduplicated: %d %q", count, text)
	}
}
