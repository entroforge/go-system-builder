package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
)

// An original Builder may still be in flight after Main's empty Impact and
// the independently recorded Targeted result. This fixture advances through
// real original consumers; it never edits Runtime state or fabricates receipts.
func TestS9LateOriginalBuilderAfterTargetedPublic(t *testing.T) {
	for _, stage := range []string{"ready", "new-s7"} {
		t.Run(stage, func(t *testing.T) {
			root, worker, packet, in, _ := nativeS9WorkerFixture(t)
			writeRepairLeadFile(t, root, ".claude/agents/test-builder.md", "# Test Builder\n")
			before := repairDispatchSnapshot(t, root)
			impact, err := prepareRepairImpactQuestionnaire(root, before.State)
			if err != nil {
				t.Fatal(err)
			}
			_ = impact
			main := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
			out, diagnostic := s9ReferencePublicHook(t, root, main)
			snapshot := repairDispatchSnapshot(t, root)
			if got := stringValue(repairPointerCLI(snapshot.State)["status"]); got != "targeted_reverification" {
				t.Fatalf("original Impact did not begin targeted: status=%s out=%s diagnostic=%s", got, out, diagnostic)
			}
			divisions := repairVerifierDispatchPaths(root, snapshot.State)
			if len(divisions) != 1 {
				t.Fatalf("original verifier division not prepared: %v", divisions)
			}
			choice := repairVerifierDispatchChoice{AssignmentID: packet.Binding.RepairAssignmentID, IndependentVerification: true}
			feedback := repairVerifierDispatchSave(t, root, divisions[0], []repairVerifierDispatchChoice{choice})
			snapshot = repairDispatchSnapshot(t, root)
			selections, err := repairTargetedSelections(root, snapshot.State)
			if err != nil || len(selections) != 1 {
				t.Fatalf("original Targeted not prepared: selections=%+v err=%v feedback=%s", selections, err, feedback)
			}
			sel := selections[0]
			targetPath, draft, err := prepareRepairTargetedQuestionnaire(root, snapshot.State, sel)
			if err != nil {
				t.Fatalf("prepare original independent Targeted: %v feedback=%s", err, feedback)
			}
			draft.Result, draft.FailureClass = "pass", ""
			draft.PerformedAt = time.Now().UTC()
			for i := range draft.AssertionResults {
				draft.AssertionResults[i].Result = "pass"
				draft.AssertionResults[i].EvidenceRefs = []string{"test://synthetic-targeted"}
			}
			writeRepairLeadJSON(t, targetPath, draft)
			value, ref, err := repair.CreateTargetedReverification(root, draft)
			if err != nil {
				t.Fatalf("original Targeted author result: %v", err)
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			ready, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{
				RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: snapshot.Revision, Actor: sel.verifierOwner}, Reverification: ref, Record: &value,
			})
			if err != nil {
				t.Fatalf("original Targeted consumer: %v", err)
			}
			if got := stringValue(repairPointerCLI(ready.State)["status"]); got != "ready_for_full_review" {
				t.Fatalf("expected real ready after Targeted; status=%s", got)
			}
			if stage == "new-s7" {
				msg, err := completeRepairExit(root)
				if err != nil {
					t.Fatalf("original Handoff could not open new S7: %v; %s", err, msg)
				}
			}
			cursorBefore := mapFieldCLI(repairDispatchSnapshot(t, root).State, "lifecycle")
			beforeState, beforePhase := stringValue(cursorBefore["state"]), stringValue(cursorBefore["phase"])
			if stage == "new-s7" && beforeState != "verification" {
				t.Fatalf("Handoff did not reach S7: %v", cursorBefore)
			}
			in.Event, in.CWD = "SubagentStop", worker
			nativeS9SplitChild(t, &in)
			out, diagnostic = s9ReferencePublicHook(t, root, in)
			after := repairDispatchSnapshot(t, root)
			cursorAfter := mapFieldCLI(after.State, "lifecycle")
			if stringValue(cursorAfter["state"]) != beforeState || stringValue(cursorAfter["phase"]) != beforePhase {
				t.Fatalf("late original Builder changed current cursor: before=%s/%s after=%v", beforeState, beforePhase, cursorAfter)
			}
			contributions, err := repair.ReceivedRepairContributions(root, after.State)
			if err != nil || len(contributions) != 1 || contributions[0].AssignmentID != packet.Binding.RepairAssignmentID {
				t.Fatalf("late original Builder not received: %+v err=%v out=%s diagnostic=%s", contributions, err, out, diagnostic)
			}
			if strings.Contains(strings.ToLower(out+diagnostic), "already reverified") {
				t.Fatalf("late result falsely claimed already verified: %s %s", out, diagnostic)
			}
		})
	}
}
