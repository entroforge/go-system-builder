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
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
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
	for _, actualCWD := range []string{other, nested} {
		out.Reset()
		diagnostic.Reset()
		body, _ = json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-reviewer", "session_id": "parent", "cwd": actualCWD, "last_assistant_message": filepath.Join(root, p.ViewPath)})
		if code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 {
			t.Fatalf("native return blocked %d %s %s", code, &out, &diagnostic)
		}
		if !strings.Contains(diagnostic.String(), "no source binding for actual native worktree") || !strings.Contains(diagnostic.String(), "no answer was inspected or received") {
			t.Fatal(diagnostic.String())
		}
		if after := stageSnapshot(t, root); after.Revision != before {
			t.Fatal("unbound worktree consumed Main original")
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
