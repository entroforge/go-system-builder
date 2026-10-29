package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
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
	if !strings.Contains(out.String(), "S7 SUBMISSION RECEIPT") || !strings.Contains(out.String(), "no current registered issuance") {
		t.Fatalf("unissued S7 path did not receive a fail-closed S7 receipt: %s", out.String())
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
	if !strings.Contains(out.String(), "S7 SUBMISSION RECEIPT") || !strings.Contains(out.String(), "no current registered issuance") {
		t.Fatalf("unissued teammate path was not rejected by original packet routing: %s", out.String())
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
	if !strings.Contains(errout.String(), "S7 answer path has no current registered issuance") {
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
	snapshot := stageSnapshot(t, root)

	planPath := minimalReviewPlan(t, t.TempDir(), "review-plan-stage-answer-cli")
	var out, errout bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan", "--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl", "--expected-revision", strconv.Itoa(snapshot.Revision), "--file", planPath,
	}, nil, &out, &errout); code != 0 {
		t.Fatalf("register S7 Plan: code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "agent-cli-consume")
	snapshot = stageSnapshot(t, root)
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
			"observed": "the frozen fixture subject matches the registered Plan digest", "evidence_refs": []string{},
		}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{},
		"site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass",
	}
	stageJSON(t, root, issued[0].AnswerPath, issued[0].Draft)

	out.Reset()
	errout.Reset()
	answerPath := filepath.Join(root, filepath.FromSlash(issued[0].AnswerPath))
	beforeNative := stageSnapshot(t, root).Revision
	for _, event := range []string{"SubagentStart", "PreToolUse", "PostToolUse"} {
		if code, _, diagnostic := nativeReviewEvent(t, root, event, "opaque-native-reviewer", answerPath); code != 0 {
			t.Fatalf("native S7 %s: %d %s", event, code, diagnostic)
		}
		if stageSnapshot(t, root).Revision != beforeNative {
			t.Fatalf("native S7 %s changed Runtime", event)
		}
	}
	if code, _, diagnostic := nativeReviewEvent(t, root, "PostToolUse", "", answerPath); code != 0 || stageSnapshot(t, root).Revision != beforeNative {
		t.Fatalf("Main received S7 answer before native Stop: %d %s", code, diagnostic)
	}
	if code, output, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "opaque-native-reviewer", answerPath); code != 0 || output != "" || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("native S7 Stop did not receive actual professional answer: %d %s %s", code, output, diagnostic)
	}
	if context := mainNativeReturn(t, root, filepath.Join(root, issued[0].ViewPath)); !strings.Contains(context, `"accepted":true`) || !strings.Contains(context, issued[0].ID) {
		t.Fatalf("Main missed original S7 reception: %s", context)
	}
	received := stageSnapshot(t, root).Revision
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "different-native-actor-after-resume", answerPath); code != 0 {
		t.Fatalf("native S7 replay: %d %s", code, diagnostic)
	}
	if stageSnapshot(t, root).Revision != received {
		t.Fatal("native S7 replay changed Runtime")
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
}
