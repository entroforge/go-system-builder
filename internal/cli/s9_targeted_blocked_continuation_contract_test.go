package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestS9TargetedBlockedNaturallyReceivesUpdatedBlockedAndFail(t *testing.T) {
	root, path, answer := nativeS9TargetedListenerFixture(t)
	actor := "opaque-targeted-continuation"
	parent := filepath.Join(t.TempDir(), "session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": "Perform the original independent verification " + path}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func() string {
		t.Helper()
		payload := map[string]any{"hook_event_name": "SubagentStop", "session_id": "targeted-continuation", "agent_id": actor, "agent_type": "general-purpose", "cwd": root, "transcript_path": parent, "agent_transcript_path": child, "last_assistant_message": path}
		raw, _ := json.Marshal(payload)
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
			t.Fatalf("original Targeted stop failed: code=%d out=%s diagnostic=%s", code, &out, &diagnostic)
		}
		return out.String() + diagnostic.String()
	}
	answer.ContinuityReason = "first honest blocking observation"
	writeRepairLeadJSON(t, path, answer)
	firstOutput := invoke()
	first := repairDispatchSnapshot(t, root)
	firstPointer := repairPointerCLI(first.State)
	if refs := repairArtifactRefs(firstPointer, "targeted_reverification_artifacts", ""); len(refs) != 1 || stringValue(firstPointer["status"]) != "blocked" {
		t.Fatalf("original blocked answer not received: refs=%+v status=%v output=%s", refs, firstPointer["status"], firstOutput)
	}
	firstRef := firstPointer["targeted_reverification_artifacts"].([]any)[0].(map[string]any)
	professionalNote := map[string]any{"sha256": "business description", "uncertainty": true}
	firstRef["sha256"] = map[string]any{"historical": true}
	firstRef["author_note"] = professionalNote
	writeRepairLeadJSON(t, filepath.Join(root, ".claude/loop-state.json"), first.State)
	firstArtifact, err := os.ReadFile(filepath.Join(root, firstRef["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	invoke()
	replayed := repairDispatchSnapshot(t, root)
	replayedRefs := repairPointerCLI(replayed.State)["targeted_reverification_artifacts"].([]any)
	if len(replayedRefs) != 1 || !reflect.DeepEqual(replayedRefs[0].(map[string]any)["author_note"], professionalNote) {
		t.Fatal("original Targeted replay lost its professional extension")
	}
	answer.ContinuityReason = "further observation: same work remains blocked"
	writeRepairLeadJSON(t, path, answer)
	secondOutput := invoke()
	second := repairDispatchSnapshot(t, root)
	secondPointer := repairPointerCLI(second.State)
	if refs := repairArtifactRefs(secondPointer, "targeted_reverification_artifacts", ""); len(refs) != 2 || stringValue(secondPointer["status"]) != "blocked" {
		t.Fatalf("updated blocked answer not preserved: refs=%+v status=%v output=%s", refs, secondPointer["status"], secondOutput)
	}
	if stringValue(secondPointer["blocker_resolved_at"]) != "" || stringValue(secondPointer["blocker_resolved_by"]) != "" {
		t.Fatalf("another blocked answer falsely resolved blocker: %+v", secondPointer)
	}
	retained := secondPointer["targeted_reverification_artifacts"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(retained["author_note"], professionalNote) {
		t.Fatal("new current Targeted reference lost original professional extension")
	}
	if _, exists := retained["sha256"]; exists {
		t.Fatal("new current Targeted reference registered old machine digest")
	}
	if data, err := os.ReadFile(filepath.Join(root, firstRef["path"].(string))); err != nil || !bytes.Equal(data, firstArtifact) {
		t.Fatalf("Targeted continuation rewrote original artifact: %v", err)
	}
	answer.Result, answer.FailureClass, answer.ContinuityReason = "fail", "", ""
	writeRepairLeadJSON(t, path, answer)
	thirdOutput := invoke()
	third := repairDispatchSnapshot(t, root)
	thirdPointer := repairPointerCLI(third.State)
	if refs := repairArtifactRefs(thirdPointer, "targeted_reverification_artifacts", ""); len(refs) != 3 {
		t.Fatalf("new honest failure not received: refs=%+v output=%s", refs, thirdOutput)
	}
	if stringValue(thirdPointer["blocker_resolved_at"]) != "" || stringValue(thirdPointer["blocker_resolved_by"]) != "" {
		t.Fatalf("a new conclusion falsely certified blocker resolution: %+v", thirdPointer)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte("targeted_reverification_unblocked")) {
		t.Fatal("ordinary honest answers manufactured an unblock event")
	}
}
