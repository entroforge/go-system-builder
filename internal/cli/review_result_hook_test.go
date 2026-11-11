package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// A file under a plausible S7 directory is not an issuance. The PostToolUse
// adapter must consume the route and reject it without falling through to an
// older S5 submission handler or mutating the control Runtime.
func TestSavedReviewResultHookRejectsUnissuedPathWithoutRuntimeWrite(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "S7", "forged", "reviewer-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{
		"title": "forged S7 answer",
	})

	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": path},
		"teammate_name":   "reviewer-spec",
		"cwd":             f.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("PostToolUse exit=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	if strings.Contains(out.String(), "accepted=true") || strings.Contains(out.String(), `"accepted":true`) {
		t.Fatalf("unissued S7 path was represented as received: %s", out.String())
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("unissued path changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
}

// An arbitrary teammate name cannot turn an unissued path into a current S7
// questionnaire; the registered packet path remains the source of routing.
func TestSavedReviewResultHookDoesNotTreatUnissuedTeammatePathAsCurrent(t *testing.T) {
	f := newStageAnswerFixture(t)
	path := filepath.Join(f.root, ".claude", "submissions", "S7", "forged", "reviewer-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{
		"title": "forged S7 answer",
	})
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": path},
		"teammate_name":   "reviewer-spec",
		"cwd":             f.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("PostToolUse exit=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	if strings.Contains(out.String(), "accepted=true") || strings.Contains(out.String(), `"accepted":true`) {
		t.Fatalf("unissued teammate path was represented as received: %s", out.String())
	}
}

// A supplied --agent value cannot make an unissued S7-looking path current;
// the original Runtime packet selector remains the source of routing.
func TestStageAnswerTerminalConsumeRejectsUnissuedS7Path(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "S7", "round-1", "reviewer-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{
		"title": "terminal supplied S7 answer",
	})

	var out, errout bytes.Buffer
	code := cli.Run([]string{
		"runtime", "stage-answer", "consume", "--root", f.root,
		"--agent", "reviewer-spec", "--path", path,
	}, nil, &out, &errout)
	if code == 0 {
		t.Fatalf("terminal --agent was accepted as S7 author identity: stdout=%s stderr=%s", out.String(), errout.String())
	}
	if !strings.Contains(errout.String(), "answer path has no exact current registered issuance") {
		t.Fatalf("S7 terminal rejection did not come from exact registered-path selection: %s", errout.String())
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("terminal S7 consume changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
}

func TestStageAnswerConsumeAcceptsIssuedS7AnswerWithoutAgent(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	if err := os.MkdirAll(filepath.Join(root, "internal", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "example", "service.go"), []byte("worktree baseline"), 0o644); err != nil {
		t.Fatal(err)
	}
	req039fixtures.WriteState(t, root, state)
	// Bind the fixture's actual REQ authority root to its Git workspace before
	// registering and issuing the S7 packet.
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	planPath := minimalReviewPlan(t, t.TempDir(), "review-plan-stage-answer-cli")
	var out, errout bytes.Buffer
	if _, err := review.RegisterPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{ExpectedRevision: -1, PlanPath: planPath}); err != nil {
		t.Fatalf("register original S7 Plan: %v", err)
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "agent-cli-consume")
	snapshot := stageSnapshot(t, root)
	for _, raw := range snapshot.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "agent-cli-consume" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", snapshot.State)
	out.Reset()
	errout.Reset()
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"}, nil, &out, &errout); code != 0 {
		t.Fatalf("issue S7 questionnaire: code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var issued []struct {
		ID         string         `json:"packet_id"`
		ViewPath   string         `json:"view_path"`
		AnswerPath string         `json:"answer_path"`
		Draft      map[string]any `json:"draft"`
	}
	if err := json.Unmarshal(out.Bytes(), &issued); err != nil || len(issued) != 1 {
		t.Fatalf("decode exact issued S7 questionnaire: packets=%d err=%v stdout=%s", len(issued), err, out.String())
	}
	issued[0].Draft["answer"] = map[string]any{
		"claim_results": []any{map[string]any{
			"claim_id": "claim-dv-1", "conclusion": "pass", "method": "traceability",
			"observed": "the fixture subject satisfies the assigned traceability claim", "evidence_refs": []string{},
		}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{},
		"site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass",
	}
	stageJSON(t, root, issued[0].AnswerPath, issued[0].Draft)

	out.Reset()
	errout.Reset()
	answerPath := filepath.Join(root, filepath.FromSlash(issued[0].AnswerPath))
	beforeNative := stageSnapshot(t, root).Revision
	// A Main event or a Worker with only a final-message path is not an
	// original platform dispatch and cannot claim this other responsibility.
	if code, _, diagnostic := nativeReviewEvent(t, root, "PostToolUse", "", answerPath); code != 0 || stageSnapshot(t, root).Revision != beforeNative {
		t.Fatalf("Main tool event consumed another author: %d %s", code, diagnostic)
	}
	code, actorOut, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "unrelated-worker", answerPath)
	if code != 0 || stageSnapshot(t, root).Revision != beforeNative {
		t.Fatalf("unassociated Worker consumed another author: %d %s", code, diagnostic)
	}
	// The unrelated actor's Stop did not become the owner's receipt: the
	// original consumer did not report a reception, and no result receipt was
	// written for the issued responsibility.
	if strings.Contains(diagnostic, "received by the original consumer") || strings.Contains(actorOut, "accepted=true") {
		t.Fatalf("unassociated Worker consumed another author: %d %s %s", code, actorOut, diagnostic)
	}
	assignments := stageSnapshot(t, root).State["review"].(map[string]any)["assignments"].(map[string]any)
	if row, _ := assignments["assignment-dv-1"].(map[string]any); row["result_receipt"] != nil {
		t.Fatalf("unassociated Worker wrote the owner's receipt: %v", row["result_receipt"])
	}
	if code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", answerPath}, nil, &out, &errout); code != 0 {
		t.Fatalf("consume exact issued S7 answer without --agent: code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var receipt struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || !receipt.Accepted {
		t.Fatalf("original S7 consumer did not accept exact registered answer: receipt=%+v err=%v stdout=%s", receipt, err, out.String())
	}
	// The original consumer still receives the original answer through the
	// original path: the unrelated actor's Stop neither blocked nor pre-empted
	// that reception, so the ownership gate is not a blanket "never receive".
	assignments = stageSnapshot(t, root).State["review"].(map[string]any)["assignments"].(map[string]any)
	if row, _ := assignments["assignment-dv-1"].(map[string]any); row["result_receipt"] == nil {
		t.Fatalf("the original consumer did not write the owner's receipt: %v", assignments)
	}
	received := stageSnapshot(t, root).Revision
	out.Reset()
	errout.Reset()
	if code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", answerPath}, nil, &out, &errout); code != 0 || stageSnapshot(t, root).Revision != received {
		t.Fatalf("exact original consumer replay changed reception: code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}

}

// Stop is a fallback for the same proven platform sidechain and original task;
// it does not derive identity from a Worker's final text.
func TestStageAnswerS7NativeStopUsesOriginalSidechain(t *testing.T) {
	root, packet := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, packet)
	payload := s7ListenerPayload(t, "opaque-s7-stop-actor", "Review original "+filepath.Join(root, packet.ViewPath))
	payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
	payload["last_assistant_message"] = "I completed the original assigned review."
	_, diagnostic := s7ListenerEvent(t, root, "SubagentStop", payload)
	if !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("original sidechain Stop was not received: %s", diagnostic)
	}
	receipt := minimalAnswerReviewReceipt(t, root, packet)
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("original Stop did not persist receipt: %+v", receipt)
	}
	accepted := stageSnapshot(t, root).Revision
	s7ListenerEvent(t, root, "SubagentStop", payload)
	if after := stageSnapshot(t, root).Revision; after != accepted {
		t.Fatalf("original Stop replay changed revision %d -> %d", accepted, after)
	}
}
