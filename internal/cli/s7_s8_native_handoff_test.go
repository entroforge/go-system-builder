package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The registered dispatch and professional observations are fixture inputs.
// Exercise real issuance, native reception, TR-008 and Main intake without
// manually sealing a batch, changing the lifecycle or creating a Case.
func TestS7PartialP0NativeReturnReachesS8Intake(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	planPath := minimalReviewPlan(t, root, "review-plan-native-p0-handoff")
	var plan map[string]any
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	claims := plan["claims"].([]any)
	second := map[string]any{}
	for key, value := range claims[0].(map[string]any) {
		second[key] = value
	}
	second["claim_id"], second["target"] = "claim-dv-unobserved", "internal/unsafe-continuation"
	plan["claims"] = append(claims, second)
	plan["assignments"].([]any)[0].(map[string]any)["claim_ids"] = []string{"claim-dv-1", "claim-dv-unobserved"}
	stageJSON(t, root, "review-plan.json", plan)
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", planPath)
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-native-p0")
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-pending-qa")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "reviewer-native-p0" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("issue original Reviewer packet: %s (%v)", issued, err)
	}
	var packet, pending workpacket.ReviewResultPacket
	for _, issued := range packets {
		if issued.Binding.AssignmentID == "assignment-dv-1" {
			packet = issued
		} else if issued.Binding.AssignmentID == "assignment-qa-1" {
			pending = issued
		}
	}
	if packet.ID == "" || pending.ID == "" {
		t.Fatal("original Reviewer responsibilities were not both issued")
	}
	evidence := "docs/reports/review/native-p0.txt"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, evidence)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, evidence), []byte("fixture trace: the rejection path deletes stored data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	answer := map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": "claim-dv-1", "conclusion": "fail", "method": "inspect the rejection branch", "observed": "rejected input reaches a destructive deletion", "evidence_refs": []string{"path:" + evidence}}},
		"blocked_claims": []any{}, "checks": []any{}, "deviations": []any{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "finding",
		"findings": []any{map[string]any{
			"claim_id": "claim-dv-1", "severity": "P0", "expected": "rejected input preserves stored data", "authority_refs": []string{"REQ-WORKTREE"},
			"observed": "the rejection branch reaches destructive deletion", "observation_mode": "code_inspection", "reproducibility": "once_with_deterministic_trace", "evidence_refs": []string{"path:" + evidence},
			"encounter": map[string]any{"journey_summary": "trace the input rejection branch to the destructive storage operation", "inspection_entry": "internal/example/service.go:1", "symbol_trail": "reject -> delete", "wall_action": "inspect the rejection branch", "first_bad_checkpoint": "deletion is reachable after rejection", "terminal_state": "review stopped before executing the destructive path", "capture_gaps": []string{"Stopped further checks to avoid data destruction; the remaining delivery Claim has not been observed."}},
		}},
	}
	packet.Draft.Answer, _ = json.Marshal(answer)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	answerPath := filepath.Join(root, packet.AnswerPath)
	before := stageSnapshot(t, root).Revision
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		if code, _, diagnostic := nativeReviewEvent(t, root, event, "opaque-p0-reviewer", answerPath); code != 0 || stageSnapshot(t, root).Revision != before {
			t.Fatalf("ordinary Worker %s received work or blocked tools: %d %s", event, code, diagnostic)
		}
	}
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "opaque-p0-reviewer", answerPath); code != 0 || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("partial P0 native reception: %d %s", code, diagnostic)
	}
	advanced := stageSnapshot(t, root)
	lifecycle := advanced.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "bug_resolution" || lifecycle["phase"] != "investigation" {
		t.Fatalf("natural Finding reception did not continue through TR-008 to S8: %v", lifecycle)
	}
	reviewState := advanced.State["review"].(map[string]any)
	rows := reviewState["assignments"].(map[string]any)
	pendingRow := rows[pending.Binding.AssignmentID].(map[string]any)
	if pendingRow["agent_id"] != pending.Binding.OwnerAgentID || workstate.ReviewAssignmentActivity(pendingRow) != "dispatched" || pendingRow["result_ref"] != nil || pendingRow["result_receipt"] != nil {
		t.Fatalf("Finding return lost or completed the still-issued QA responsibility: %#v", pendingRow)
	}
	stored, found, err := runtime.FindReviewResultPacketContext(runtime.WithPacketBodyRoot(context.Background(), root), advanced.State, pending.ID)
	if err != nil || !found || !reflect.DeepEqual(stored, pending) {
		t.Fatalf("Finding return replaced the pending original packet: %v", err)
	}
	for _, id := range []string{"claim-dv-unobserved", "claim-qa-1"} {
		claim := reviewState["claims"].(map[string]any)[id].(map[string]any)
		if claim["disposition"] == "pass" || claim["result_id"] != nil {
			t.Fatalf("unexecuted Claim %s was reported as observed: %#v", id, claim)
		}
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeJournal, _ := os.ReadFile(journalPath)
	originalAnswer, _ := os.ReadFile(answerPath)
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "resumed-p0-reviewer", answerPath); code != 0 || stageSnapshot(t, root).Revision != advanced.Revision {
		t.Fatalf("original return replay was not idempotent: %d %s", code, diagnostic)
	}
	afterState, _ := os.ReadFile(statePath)
	afterJournal, _ := os.ReadFile(journalPath)
	afterAnswer, _ := os.ReadFile(answerPath)
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) || !bytes.Equal(originalAnswer, afterAnswer) {
		t.Fatal("original return replay rewrote accepted facts or the author's answer")
	}
	mainHook := func(event, tool, path string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "tool_name": tool, "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{}})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("Main %s: %d %s %s", event, code, &out, &diagnostic)
		}
		return out.String()
	}
	var paths []string
	err = filepath.WalkDir(filepath.Join(root, ".claude/submissions/s8"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "intake.json" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil || len(paths) != 1 {
		t.Fatalf("S8 exact intake was not prepared by the original return: %v %v", paths, err)
	}
	stageJSON(t, root, strings.TrimPrefix(paths[0], root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate this exact received destructive rejection observation as a provisional causal group; no common root cause is asserted."})
	// The installed matcher must deliver the same custom-tool event tested below.
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	data, err = os.ReadFile(filepath.Join("..", "..", "settings.json"))
	if err != nil || json.Unmarshal(data, &settings) != nil {
		t.Fatalf("read installed hook source: %v", err)
	}
	matched := false
	for _, entry := range settings.Hooks["PostToolUse"] {
		matched = matched || entry.Matcher == "*"
	}
	if !matched {
		t.Fatal("PostToolUse registration does not cover custom-tool saves")
	}
	feedback := mainHook("PostToolUse", "mcp__sandbox__write_file", paths[0])
	if !strings.Contains(feedback, "intake committed Case") {
		t.Fatalf("S8 professional intake was not received: %s", feedback)
	}
	received := stageSnapshot(t, root).Revision
	if feedback := mainHook("PostToolUse", "mcp__sandbox__write_file", paths[0]); !strings.Contains(feedback, "already received") || stageSnapshot(t, root).Revision != received {
		t.Fatalf("S8 intake replay duplicated acceptance: %s", feedback)
	}
}
