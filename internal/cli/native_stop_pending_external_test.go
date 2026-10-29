package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Simulate an interrupted original fingerprint write, with an unchanged target
// and a real coherent Runtime/journal. The production Writer validates it.
func nativeStopFingerprintPending(t *testing.T, root string) string {
	t.Helper()
	state := stageSnapshot(t, root).State
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	hash := workpacket.Digest(append(encoded, '\n'))
	path := ".claude/loop-state.json.fingerprint-pending.json"
	stageJSON(t, root, path, map[string]any{"schema_version": "1.0.0", "previous_state_sha256": hash, "previous_revision": state["revision"], "state_sha256": hash, "state": state})
	return filepath.Join(root, path)
}

func TestNativeStopPendingS7ReceivesSameOriginalAnswer(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-pending-stop")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
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
	before := stageSnapshot(t, root).Revision
	marker := nativeStopFingerprintPending(t, root)
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
	hashState := func(state map[string]any) string {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return workpacket.Digest(append(data, '\n'))
	}
	eventBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, ".claude/loop-state.json.commit-pending.json", map[string]any{
		"schema_version": "1.0.0", "previous_state_sha256": hashState(previous), "previous_revision": previous["revision"],
		"state_sha256": hashState(target), "journal_event_sha256": workpacket.Digest(append(eventBytes, '\n')),
		"request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": target, "journal_event": event,
	})
	marker = statePath + ".commit-pending.json"
	if err := os.WriteFile(statePath, previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", "opaque-reviewer", filepath.Join(root, p.AnswerPath)); code != 0 || !strings.Contains(diag, "recovered the original pending Writer") || !strings.Contains(diag, "original consumer") {
		t.Fatalf("same-event reception: %d %s", code, diag)
	}
	after := stageSnapshot(t, root)
	if after.Revision <= before {
		t.Fatal("answer did not reach original Writer")
	}
	answerAfter, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(answerBefore, answerAfter) {
		t.Fatalf("system changed author bytes: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
	if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", "opaque-reviewer", filepath.Join(root, p.AnswerPath)); code != 0 || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatalf("replay duplicated acceptance: %d %s", code, diag)
	}
}

func TestNativeStopPendingS5DoesNotReceiveProfessionalAnswer(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	before := stageSnapshot(t, f.root).Revision
	marker := nativeStopFingerprintPending(t, f.root)
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
	before := stageSnapshot(t, root).Revision
	marker := nativeStopFingerprintPending(t, root)
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
