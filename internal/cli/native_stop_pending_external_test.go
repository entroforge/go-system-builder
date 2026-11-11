package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Simulate an interrupted original fingerprint write, with an unchanged target
// and a real coherent Runtime/journal. The production Writer validates it.
func nativeStopCommitPending(t *testing.T, root string) string {
	t.Helper()
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	before, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	life := before.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	id := fmt.Sprintf("pending-fixture-%d", time.Now().UnixNano())
	generation := 0
	if baseline, ok := before.State["baseline"].(map[string]any); ok {
		switch n := baseline["generation"].(type) {
		case int:
			generation = n
		case float64:
			generation = int(n)
		}
	}
	after, err := writer.Update(before.Revision, runtime.Mutation{EventID: id, RequestID: id, TransitionID: "PENDING-FIXTURE", Event: "pending_fixture", Actor: "orchestrator", IdempotencyKey: id, RuntimeID: before.State["runtime_id"].(string), BaselineGeneration: generation, From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(journal)), "\n")
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"schema_version": "1.0.0", "previous_revision": before.Revision, "request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": after.State, "journal_event": event})
	if err != nil {
		t.Fatal(err)
	}
	marker := statePath + ".commit-pending.json"
	if err := os.WriteFile(marker, data, 0600); err != nil {
		t.Fatal(err)
	}
	return marker
}

func pendingRawRevision(t *testing.T, root string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state.Revision
}

// assignmentResultReceipt reads the durable acceptance receipt written beside
// a consumed S7 Assignment.
func assignmentResultReceipt(t *testing.T, state map[string]any, assignmentID string) map[string]any {
	t.Helper()
	review, _ := state["review"].(map[string]any)
	assignments, _ := review["assignments"].(map[string]any)
	assignment, _ := assignments[assignmentID].(map[string]any)
	receipt, _ := assignment["result_receipt"].(map[string]any)
	return receipt
}

// nativeStopReviewEvent delivers a native SubagentStop the way the platform
// does: transcript_path names Main's session while the actor's own task lives
// in its sibling subagents transcript.
//
// The current contract resolves an issued S7 responsibility from that actor's
// own original task (blueprint/l4-mechanisms/L4-hook-platform-wiring.md:56
// "父会话转录、子会话转录与最后收尾文字不同"; :122 "成功 PostToolUse 和 Stop／
// SubagentStop 复用同一消费者兜底"), so the exact original answer reference
// must be present in the actor's own task prompt.
func nativeStopReviewEvent(t *testing.T, root, event, actor, ref string) (int, string, string) {
	t.Helper()
	rel := ref
	if strings.HasPrefix(ref, root+string(filepath.Separator)) {
		rel = strings.TrimPrefix(ref, root+string(filepath.Separator))
	}
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": "Source root: `" + root + "`. Original questionnaire: `" + rel + "`."}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte(`{"type":"user","message":{"role":"user","content":"Main's own unrelated conversation"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": actor, "session_id": "native-review-parent", "agent_type": "professional-reviewer", "cwd": root, "transcript_path": parent, "agent_transcript_path": child, "last_assistant_message": ref, "tool_name": "Write", "tool_input": map[string]any{"file_path": ref}, "tool_response": map[string]any{"filePath": ref}})
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

func TestNativeStopPendingS7ReceivesSameOriginalAnswer(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-pending-stop", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	plan := minimalReviewPlan(t, root, "review-plan-pending-stop")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	registerReviewPlanNaturally(t, root, state, "review-plan-pending-stop", plan)
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-pending")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	previousState, previousJournal := read(statePath), read(journalPath)
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v %d", err, len(packets))
	}
	targetState, targetJournal := read(statePath), read(journalPath)
	p := packets[0]
	p.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": []any{map[string]any{"claim_id": p.Binding.ClaimIDs[0], "conclusion": "pass", "method": "inspect frozen fixture", "observed": "frozen fixture satisfies assigned assertion", "evidence_refs": []string{}}}, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass"})
	stageJSON(t, root, p.AnswerPath, p.Draft)
	answerBefore, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	marker := nativeStopCommitPending(t, root)
	before := pendingRawRevision(t, root)
	// No exact task reference: recover the pair, but never guess the only packet.
	if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", "opaque-reviewer", "unrelated actual task"); code != 0 || !strings.Contains(diag, "recovered the original pending Writer") {
		t.Fatalf("unknown Stop: %d %s", code, diag)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("unknown responsibility received a private answer")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
	// Recreate the crash window of the real issuance commit, before either
	// half was published. The answer is written, but has never been received.
	var previous, target, event map[string]any
	for _, item := range []struct {
		data []byte
		into *map[string]any
	}{{previousState, &previous}, {targetState, &target}} {
		if err := json.Unmarshal(item.data, item.into); err != nil {
			t.Fatal(err)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(targetJournal), []byte("\n"))
	if err := json.Unmarshal(lines[len(lines)-1], &event); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, ".claude/loop-state.json.commit-pending.json", map[string]any{
		"schema_version": "1.0.0", "previous_revision": previous["revision"],
		"request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": target, "journal_event": event,
	})
	marker = statePath + ".commit-pending.json"
	if err := os.WriteFile(statePath, previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	code, _, diag := nativeStopReviewEvent(t, root, "SubagentStop", "opaque-reviewer", filepath.Join(root, p.AnswerPath))
	if code != 0 || !strings.Contains(diag, "recovered the original pending Writer") || !strings.Contains(diag, "original consumer") {
		t.Fatalf("same-event reception: %d %s", code, diag)
	}
	// The answer reached the original Writer when the durable acceptance
	// receipt was written beside the consumed Assignment in the same Runtime
	// CAS (internal/review/result_receipt.go:20-23). The recovered checkpoint
	// carries its own real revision, so the Runtime revision number alone no
	// longer separates "recovered only" from "recovered and accepted".
	after := stageSnapshot(t, root)
	receipt := assignmentResultReceipt(t, after.State, "assignment-dv-1")
	if receipt == nil || receipt["contract"] != "s7-review-result-receipt-v1" || receipt["assignment_id"] != "assignment-dv-1" {
		t.Fatalf("answer did not reach original Writer: %v", receipt)
	}
	answerAfter, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(answerBefore, answerAfter) {
		t.Fatalf("system changed author bytes: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
	if code, _, diag := nativeStopReviewEvent(t, root, "SubagentStop", "opaque-reviewer", filepath.Join(root, p.AnswerPath)); code != 0 || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatalf("replay duplicated acceptance: %d %s", code, diag)
	}
}

func TestNativeStopPendingS5DoesNotReceiveProfessionalAnswer(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	marker := nativeStopCommitPending(t, f.root)
	before := pendingRawRevision(t, f.root)
	if code, _, diag := nativeReviewEvent(t, f.root, "SubagentStop", "opaque-document-reviewer", filepath.Join(f.root, p.ViewPath)); code != 0 || !strings.Contains(diag, "recovered the original pending Writer") || strings.Contains(diag, "answer received") {
		t.Fatalf("S5: %d %s", code, diag)
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("S5 Stop newly received professional work")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("shared Writer did not recover: %v", err)
	}
}

func TestNativeStopPendingS6DoesNotReceiveProfessionalAnswer(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	marker := nativeStopCommitPending(t, root)
	before := pendingRawRevision(t, root)
	if code, diag := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 || !strings.Contains(diag, "recovered the original pending Writer") || strings.Contains(diag, "delivery received") {
		t.Fatalf("S6: %d %s", code, diag)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("S6 Stop newly received professional work")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("shared Writer did not recover: %v", err)
	}
}
