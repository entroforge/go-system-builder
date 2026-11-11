package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
)

// This starts from the existing approved S9 fixture, but establishes the empty
// Impact and independent verifier through their original consumers. Targeted
// itself is received solely through the public hook. It is not an S0-to-S11
// platform test and does not claim that the synthetic checks ran.
func targetedPublicChainFixture(t *testing.T) (string, string, repair.TargetedReverificationRequest, policy.Input) {
	t.Helper()
	root, _, packet, _, _ := nativeS9WorkerFixture(t)
	writeRepairLeadFile(t, root, ".claude/agents/test-builder.md", "# Test Builder\n")
	before := repairDispatchSnapshot(t, root)
	for key, value := range targetedHistoricalPointerData() {
		repairPointerCLI(before.State)[key] = value
	}
	writeRepairLeadJSON(t, filepath.Join(root, ".claude/loop-state.json"), before.State)
	if _, err := prepareRepairImpactQuestionnaire(root, before.State); err != nil {
		t.Fatal(err)
	}
	main := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
	out, diagnostic := s9ReferencePublicHook(t, root, main)
	snapshot := repairDispatchSnapshot(t, root)
	pointer := repairPointerCLI(snapshot.State)
	assertTargetedHistoricalPointerData(t, snapshot.State)
	if stringValue(pointer["status"]) != "targeted_reverification" || stringValue(pointer["impact_ref"]) == "" {
		t.Fatalf("original empty Impact was not received: pointer=%+v output=%s diagnostic=%s", pointer, out, diagnostic)
	}
	if stringValue(pointer["result_ref"]) != "" || stringValue(pointer["changeset_ref"]) != "" {
		t.Fatalf("fixture unexpectedly produced Final/Changeset before Targeted: %+v", pointer)
	}
	divisions := repairVerifierDispatchPaths(root, snapshot.State)
	if len(divisions) != 1 {
		t.Fatalf("original independent verifier division: %v", divisions)
	}
	feedback := repairVerifierDispatchSave(t, root, divisions[0], []repairVerifierDispatchChoice{{AssignmentID: packet.Binding.RepairAssignmentID, IndependentVerification: true}})
	snapshot = repairDispatchSnapshot(t, root)
	selections, err := repairTargetedSelections(root, snapshot.State)
	if err != nil || len(selections) != 1 {
		t.Fatalf("original verifier selection: %+v %v; feedback=%s", selections, err, feedback)
	}
	path, answer, err := prepareRepairTargetedQuestionnaire(root, snapshot.State, selections[0])
	if err != nil {
		t.Fatal(err)
	}

	// Real ordinary native events point at the parent transcript; the actual
	// child task carries the exact original questionnaire reference.
	actor := "targeted-public-child"
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	task, err := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": "Perform the original independent verification using " + path}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, append(task, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("original Main transcript\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := policy.Input{CWD: root, SessionID: "targeted-public-session", AgentID: actor, AgentType: "general-purpose", TranscriptPath: parent, ToolName: "Read", ToolInput: map[string]any{"file_path": "internal/api/one.go"}}
	return root, path, answer, input
}

func targetedHistoricalPointerData() map[string]any {
	return map[string]any{
		"impact_sha256": map[string]any{"old": true}, "handoff_sha256": nil,
		"review_plan_seed_sha256": float64(7), "changeset_sha256": []any{"old"},
		"implementation_baseline_digest": "historical value",
		"author_note":                    map[string]any{"sha256": "business description"},
	}
}

func assertTargetedHistoricalPointerData(t *testing.T, state map[string]any) {
	t.Helper()
	pointer := repairPointerCLI(state)
	for key, expected := range targetedHistoricalPointerData() {
		value, exists := pointer[key]
		if !exists || !reflect.DeepEqual(value, expected) {
			t.Fatalf("original repair history %s changed: %#v", key, pointer)
		}
	}
}

func targetedPublicEvent(t *testing.T, root string, input policy.Input, event string) (string, string) {
	t.Helper()
	input.Event = event
	if event == "SubagentStop" {
		input.AgentTranscriptPath = filepath.Join(strings.TrimSuffix(input.TranscriptPath, ".jsonl"), "subagents", "agent-"+input.AgentID+".jsonl")
		// The task already supplies the accurate original reference. Returning a
		// path or special completion phrase is not required of the author.
		input.LastAssistantMessage = ""
	}
	return s9ReferencePublicHook(t, root, input)
}

func targetedPublicSavedResult(t *testing.T, root string) (repair.TargetedReverification, repair.ArtifactRef) {
	t.Helper()
	state := repairDispatchSnapshot(t, root).State
	refs := repairArtifactRefs(repairPointerCLI(state), "targeted_reverification_artifacts", "")
	if len(refs) != 1 {
		t.Fatalf("want one original Targeted receipt, got %+v", refs)
	}
	value, err := repair.ValidateTargetedReverification(root, refs[0])
	if err != nil {
		t.Fatalf("read accepted original Targeted: %v", err)
	}
	return value, refs[0]
}

func TestTargetedPublicMinimalPassAutomaticallyEntersFreshS7(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, path, answer, input := targetedPublicChainFixture(t)
			if len(answer.AssertionResults) == 0 {
				t.Fatal("fixture must exercise noncritical empty assertion rows")
			}
			answer.Result, answer.FailureClass = "pass", ""
			for i := range answer.AssertionResults {
				answer.AssertionResults[i].Result = ""
				answer.AssertionResults[i].EvidenceRefs = nil
			}
			writeRepairLeadJSON(t, path, answer)
			out, diagnostic := targetedPublicEvent(t, root, input, event)
			value, _ := targetedPublicSavedResult(t, root)
			if value.Result != "pass" || len(value.AssertionResults) != len(answer.AssertionResults) {
				t.Fatalf("original minimal conclusion altered: %+v", value)
			}
			for _, row := range value.AssertionResults {
				if row.Result != "" || len(row.EvidenceRefs) != 0 {
					t.Fatalf("empty optional observation was fabricated: %+v", row)
				}
			}
			state := repairDispatchSnapshot(t, root).State
			assertTargetedHistoricalPointerData(t, state)
			life, pointer := mapFieldCLI(state, "lifecycle"), repairPointerCLI(state)
			if stringValue(life["state"]) != "verification" || stringValue(life["phase"]) != "running" {
				t.Fatalf("accepted Targeted did not automatically reach new S7: life=%+v repair=%+v output=%s diagnostic=%s", life, pointer, out, diagnostic)
			}
			if stringValue(pointer["result_ref"]) != "" || stringValue(pointer["changeset_ref"]) != "" {
				t.Fatalf("new S7 fabricated Final/Changeset: %+v", pointer)
			}
			if stringValue(pointer["review_plan_seed_ref"]) == "" {
				t.Fatalf("new S7 has no original planning entry: %+v", pointer)
			}
		})
	}
}

func TestTargetedPublicNegativeConclusionKeepsRealMeaning(t *testing.T) {
	for _, verdict := range []string{"fail", "blocked"} {
		t.Run(verdict, func(t *testing.T) {
			root, path, answer, input := targetedPublicChainFixture(t)
			answer.Result, answer.FailureClass = verdict, ""
			for i := range answer.AssertionResults {
				answer.AssertionResults[i].Result = ""
				answer.AssertionResults[i].EvidenceRefs = nil
			}
			writeRepairLeadJSON(t, path, answer)
			out, diagnostic := targetedPublicEvent(t, root, input, "PreToolUse")
			value, _ := targetedPublicSavedResult(t, root)
			if value.Result != verdict {
				t.Fatalf("real original verdict changed: %+v output=%s diagnostic=%s", value, out, diagnostic)
			}
			if verdict == "fail" && value.FailureClass != "" {
				t.Fatalf("unclassified failure invented a cause: %+v", value)
			}
			state := repairDispatchSnapshot(t, root).State
			life, pointer := mapFieldCLI(state, "lifecycle"), repairPointerCLI(state)
			if stringValue(life["state"]) == "verification" || stringValue(pointer["status"]) == "ready_for_full_review" {
				t.Fatalf("%s was treated as pass: life=%+v repair=%+v", verdict, life, pointer)
			}
		})
	}
}

func TestTargetedPublicHandoffFailureKeepsOriginalReceipt(t *testing.T) {
	root, path, answer, input := targetedPublicChainFixture(t)
	answer.Result, answer.FailureClass = "pass", ""
	writeRepairLeadJSON(t, path, answer)
	// An actual filesystem preparation failure after the Targeted commit must
	// preserve the accepted answer. No Runtime or receipt is hand-written.
	handoffDir := filepath.Join(root, ".claude", "review", "repair", "handoffs")
	if err := os.Remove(handoffDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("prepare real destination failure: %v", err)
	}
	if err := os.WriteFile(handoffDir, []byte("synthetic destination unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic := targetedPublicEvent(t, root, input, "PreToolUse")
	value, original := targetedPublicSavedResult(t, root)
	if value.Result != "pass" {
		t.Fatalf("original Targeted lost after Handoff failure: %+v output=%s diagnostic=%s", value, out, diagnostic)
	}
	if err := os.Remove(handoffDir); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"runtime", "repair", "handoff", "complete", "--root", root}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("original system recovery failed: code=%d output=%s diagnostic=%s", code, &stdout, &stderr)
	}
	value, recovered := targetedPublicSavedResult(t, root)
	if value.Result != "pass" || recovered.Path != original.Path {
		t.Fatalf("Handoff recovery re-received or replaced accepted Targeted: before=%+v after=%+v", original, recovered)
	}
	life := mapFieldCLI(repairDispatchSnapshot(t, root).State, "lifecycle")
	if stringValue(life["state"]) != "verification" {
		t.Fatalf("system recovery did not continue to new S7: %+v", life)
	}
}
