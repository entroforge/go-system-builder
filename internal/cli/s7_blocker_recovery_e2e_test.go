package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS7OriginalBlockerBodyRecoveryAndReplay(t *testing.T) {
	lost := true
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	planPath := minimalReviewPlan(t, root, "review-plan-native-recovery")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", planPath)
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-recovery")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "reviewer-recovery" {
			agent["dispatch_mode"] = "plan_checkpoint"
			agent["task_ids"] = []any{"TASK-039-01"}
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("issue original responsibility: %s (%v)", issued, err)
	}
	packet := packets[0]
	evidence := "docs/reports/review/recovery-observation.txt"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, evidence)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, evidence), []byte("fixture observation: rejected input reaches a success response\n"), 0600); err != nil {
		t.Fatal(err)
	}
	encounter := map[string]any{
		"journey_summary":  "trace the rejected input through the response selection branch",
		"inspection_entry": "internal/example/service.go:1", "symbol_trail": "reject -> success",
		"wall_action": "inspect the rejection branch", "first_bad_checkpoint": "the success branch is selected",
		"last_good_checkpoint": "input is rejected", "terminal_state": "success response is selected",
	}
	siteLost := []any{}
	if lost {
		delete(encounter, "last_good_checkpoint")
		siteLost = append(siteLost, map[string]any{"finding_id": "finding-recovery", "reason": "The original rejection boundary trace is gone; the remaining output cannot reconstruct it."})
	}
	answer := map[string]any{
		"claim_results": []any{map[string]any{
			"claim_id": "claim-dv-1", "conclusion": "fail", "method": "traceability",
			"observed": "rejected input selects a success response", "evidence_refs": []string{"path:" + evidence},
		}},
		"blocked_claims": []any{}, "checks": []any{}, "deviations": []any{}, "site_lost": siteLost,
		"verification_artifact_digest": nil, "verdict": "finding",
		"findings": []any{map[string]any{
			"finding_id": "finding-recovery", "claim_id": "claim-dv-1", "severity": "P2",
			"expected": "rejected input returns an error", "authority_refs": []string{"REQ-WORKTREE"},
			"observed": "rejected input selects success", "observation_mode": "code_inspection",
			"reproducibility": "once_with_deterministic_trace", "evidence_refs": []string{"path:" + evidence},
			"encounter": encounter,
		}},
	}
	packet.Draft.Answer, _ = json.Marshal(answer)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "native-recovery-actor", filepath.Join(root, packet.AnswerPath)); code != 0 {
		t.Fatalf("native result handoff: %d %s", code, diagnostic)
	}
	after := stageSnapshot(t, root)
	reviewState := after.State["review"].(map[string]any)
	row := reviewState["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)

	if workstate.ReviewAssignmentActivity(row) != "blocked" {
		t.Fatal("original site_lost was not recorded")
	}
	before := after.Revision
	explanation := "Restored the original rejection boundary trace and fixed the fixture TODO marker; the original evidence is readable again."
	args := []string{"runtime", "agent-event", "--root", root, "--event", "blocker_resolved", "--agent-id", "reviewer-recovery", "--body", explanation}
	for _, invalid := range []struct {
		name string
		args []string
	}{
		{"other-owner", []string{"runtime", "agent-event", "--root", root, "--event", "blocker_resolved", "--agent-id", "unregistered-reviewer", "--body", explanation}},
		{"other-event", []string{"runtime", "agent-event", "--root", root, "--event", "work_blocked", "--agent-id", "reviewer-recovery", "--body", explanation}},
		{"empty-explanation", []string{"runtime", "agent-event", "--root", root, "--event", "blocker_resolved", "--agent-id", "reviewer-recovery", "--body", "  "}},
	} {
		var out, diag bytes.Buffer
		if code := cli.Run(invalid.args, strings.NewReader(""), &out, &diag); code == 0 {
			t.Fatalf("%s unexpectedly accepted: %s", invalid.name, &out)
		}
		if stageSnapshot(t, root).Revision != before {
			t.Fatalf("%s changed original state", invalid.name)
		}
	}
	stageCLI(t, args...)
	resolved := stageSnapshot(t, root)
	if resolved.Revision != before+1 {
		t.Fatalf("resolution must use one original lifecycle CAS: %d -> %d", before, resolved.Revision)
	}
	var owner map[string]any
	for _, raw := range resolved.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "reviewer-recovery" {
			owner = agent
		}
	}
	if workstate.AgentActivity(owner) != "working" || owner["blocker_resolved_ref"] == nil {
		t.Fatalf("original owner not resumed: %v", owner)
	}
	resolvedRow := resolved.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if workstate.ReviewAssignmentActivity(resolvedRow) != "blocked" || resolvedRow["result_ref"] != nil {
		t.Fatal("technical recovery must not consume the professional answer")
	}
	stageCLI(t, args...)
	if stageSnapshot(t, root).Revision != resolved.Revision {
		t.Fatal("same resolution replay advanced state")
	}
	for _, event := range []string{"SessionStart", "PreCompact", "PreToolUse"} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "session_id": "s7-recovery-body", "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "README.md")}})
		var out, diag bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diag); code != 0 {
			t.Fatalf("%s: %s", event, &diag)
		}
		text := out.String() + diag.String()
		if event == "PreCompact" {
			if text != "" {
				t.Fatalf("PreCompact must reset delivery quietly: %s", text)
			}
			continue
		}
		if !strings.Contains(text, "blocker_resolved already recorded") || !strings.Contains(text, packet.AnswerPath) {
			t.Fatalf("recovery lost accepted receipt or original answer: %s", text)
		}
	}
	// A working projection and message file without its original event are not acceptance.
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(journal), []byte("\n"))
	var retained [][]byte
	for _, line := range lines {
		if !bytes.Contains(line, []byte("blocker_resolved")) {
			retained = append(retained, line)
		}
	}
	if err := os.WriteFile(journalPath, append(bytes.Join(retained, []byte("\n")), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := cli.Run(args, strings.NewReader(""), &out, &diag); code == 0 {
		t.Fatalf("missing original event accepted replay: %s", &out)
	}
	// A schema-valid lifecycle event for another owner cannot prove this owner's recovery.
	var wrongOwnerLines [][]byte
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(line), "blocker_resolved") {
			event["event_id"] = strings.ReplaceAll(event["event_id"].(string), "reviewer-recovery", "other-reviewer")
			event["idempotency_key"] = strings.ReplaceAll(event["idempotency_key"].(string), "reviewer-recovery", "other-reviewer")
			line, _ = json.Marshal(event)
		}
		wrongOwnerLines = append(wrongOwnerLines, line)
	}
	if err := os.WriteFile(journalPath, append(bytes.Join(wrongOwnerLines, []byte("\n")), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var forgedState map[string]any
	if err := json.Unmarshal(originalState, &forgedState); err != nil {
		t.Fatal(err)
	}
	stateJournal := forgedState["journal"].(map[string]any)
	stateJournal["last_event_id"] = strings.ReplaceAll(stateJournal["last_event_id"].(string), "reviewer-recovery", "other-reviewer")
	stageJSON(t, root, ".claude/loop-state.json", forgedState)
	out.Reset()
	diag.Reset()
	if code := cli.Run(args, strings.NewReader(""), &out, &diag); code == 0 || !strings.Contains(diag.String(), "no original AGENT-LIFECYCLE") {
		t.Fatalf("another owner's receipt proved recovery: %s %s", &out, &diag)
	}
	// Restore history and finish the same professional answer through its native return.
	if err := os.WriteFile(statePath, originalState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	answer["site_lost"] = []any{}
	encounter["last_good_checkpoint"] = "input is rejected"
	packet.Draft.Answer, _ = json.Marshal(answer)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", "native-recovery-actor", filepath.Join(root, packet.AnswerPath)); code != 0 {
		t.Fatalf("same Reviewer return: %s", diag)
	}
	consumed := stageSnapshot(t, root).State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if workstate.ReviewAssignmentActivity(consumed) != "consumed" {
		t.Fatalf("same original answer was not consumed: %v", consumed)
	}
}
