package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS7SharedOriginalCannotBeBorrowedByUnboundNativeWorktree(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-shared-original")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	// The manual `runtime review-plan` registration route is retired. Main
	// saves the Hook-provided questionnaire and the original consumer receives
	// it (L3-S7 §7.1, L4-workflow-convergence §"接线与验收"). The registration
	// therefore comes from the exact original questionnaire path and the
	// original consumer, never from a hand-written Runtime pointer.
	registered := stageSnapshot(t, root)
	registered.State["review"].(map[string]any)["round_entry"] = map[string]any{
		"plan_id": "review-plan-shared-original", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil,
	}
	stageJSON(t, root, ".claude/loop-state.json", registered.State)
	planPath := review.PlanQuestionnairePath(root, registered.State)
	if planPath == "" {
		t.Fatal("the current S7 ReviewPlan questionnaire path is unavailable")
	}
	entryQuestionnaireHook(t, root, "PreToolUse", planPath)
	authored, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, authored, 0o644); err != nil {
		t.Fatal(err)
	}
	received := entryQuestionnaireHook(t, root, "PostToolUse", planPath)
	if !strings.Contains(received, "review-plan: registered review-plan-shared-original") {
		t.Fatalf("the original consumer did not register the Plan: %s", received)
	}
	// Reverse: the registration came from the original questionnaire path and
	// the original consumer. A Plan written to any other path is not
	// registered.
	offQuestionnaire := filepath.Join(root, "review-plan.json")
	if err := os.WriteFile(offQuestionnaire, authored, 0o644); err != nil {
		t.Fatal(err)
	}
	beforeOther := stageSnapshot(t, root)
	entryQuestionnaireHook(t, root, "PostToolUse", offQuestionnaire)
	afterOther := stageSnapshot(t, root)
	if afterOther.Revision != beforeOther.Revision {
		t.Fatal("a Plan outside the original questionnaire path was registered")
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-shared-original")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "reviewer-shared-original" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)

	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("packets %v %v", packets, err)
	}
	p := packets[0]
	if !strings.HasPrefix(p.AnswerPath, ".loop-submissions/s7/") {
		t.Fatal(p.AnswerPath)
	}
	var claims []any
	for _, id := range p.Binding.ClaimIDs {
		claims = append(claims, map[string]any{"claim_id": id, "conclusion": "pass", "method": "read original frozen source", "observed": "the actual frozen subject satisfies the assigned claim", "evidence_refs": []string{}})
	}
	p.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": claims, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass"})
	stageJSON(t, root, p.AnswerPath, p.Draft)
	other := filepath.Join(t.TempDir(), "reviewer")
	if output, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", other, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("worktree: %s %v", output, err)
	}
	before := stageSnapshot(t, root).Revision
	var out, diagnostic bytes.Buffer
	var body []byte
	nested := filepath.Join(other, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	// The other checkout is not the registered original's own checkout.
	//
	// The retired checkout guard must not come back in any form: the original
	// responsibility is decided by the registered issuance and the original
	// consumer, never by the Git checkout of the reporting sub-session
	// (L4-workflow-convergence.md:191 — "本人事件从同一平台子会话的真实原
	// 任务选择"). The Stop from the other checkout therefore reaches the same
	// original consumer, and what it must never do is become the owner of the
	// original.
	consumedRevision := -1
	for _, actualCWD := range []string{other, nested} {
		out.Reset()
		diagnostic.Reset()
		body, _ = json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-reviewer", "session_id": "parent", "cwd": actualCWD, "last_assistant_message": filepath.Join(root, p.ViewPath)})
		if code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 {
			t.Fatalf("native return blocked %d %s %s", code, &out, &diagnostic)
		}
		// Reverse: the retired wording that judged the responsibility by Git
		// checkout state stays retired.
		for _, retired := range []string{"no source binding for actual native worktree", "no answer was inspected or received"} {
			if strings.Contains(diagnostic.String(), retired) {
				t.Fatalf("the reception chain still judges the responsibility by Git checkout state (%q): %s", retired, diagnostic.String())
			}
		}
		// The original consumer received the professional answer for the
		// registered original owner. The reporting sub-session never becomes
		// the owner, so it cannot borrow the original: the accepted receipt is
		// signed for the registered owner, never for the acting session.
		state := stageSnapshot(t, root).State
		receipt, accepted, err := review.IssuedResultReceiptContext(t.Context(), root, state, p.ID, p.Binding.OwnerAgentID)
		if err != nil || !accepted {
			t.Fatalf("the other checkout did not reach the original consumer for the registered owner: accepted=%t err=%v diagnostic=%s", accepted, err, diagnostic.String())
		}
		if receipt.ProducerAgentID != p.Binding.OwnerAgentID {
			t.Fatalf("the accepted S7 answer was signed for another owner: %+v", receipt)
		}
		if receipt.ProducerAgentID == "opaque-reviewer" {
			t.Fatalf("the reporting sub-session borrowed the registered original: %+v", receipt)
		}
		// The other checkout registers no second S7 responsibility: the
		// Runtime still carries exactly one issued S7 packet and its
		// registered original owner.
		packets, err := runtime.ReviewResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), state)
		if err != nil || len(packets) != 1 || packets[0].ID != p.ID || packets[0].Binding.OwnerAgentID != p.Binding.OwnerAgentID {
			t.Fatalf("the other checkout changed the registered S7 responsibility: %+v err=%v", packets, err)
		}
		// The original is consumed once. A repeated native return from the
		// other checkout reuses the accepted original fact instead of taking
		// Main's original again.
		if consumedRevision < 0 {
			consumedRevision = stageSnapshot(t, root).Revision
			if consumedRevision <= before {
				t.Fatalf("the original professional answer was never received: revision=%d before=%d diagnostic=%s", consumedRevision, before, diagnostic.String())
			}
		} else if after := stageSnapshot(t, root); after.Revision != consumedRevision {
			t.Fatal("the repeated native return consumed Main original again")
		}
	}
	// The same original remains usable by its supported shared-checkout context.
	body, _ = json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-reviewer", "session_id": "parent", "cwd": root, "last_assistant_message": filepath.Join(root, p.ViewPath)})
	out.Reset()
	diagnostic.Reset()
	if code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || !strings.Contains(diagnostic.String(), "received by the original consumer") {
		t.Fatalf("shared reception %d %s %s", code, &out, &diagnostic)
	}
}
