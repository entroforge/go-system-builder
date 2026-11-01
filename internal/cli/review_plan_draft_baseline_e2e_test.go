package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
)

func s7BaselinePost(t *testing.T, root, tool, path string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": tool, "tool_input": map[string]any{"file_path": path}})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("Post hook failed: %d %s %s", code, &out, &diag)
	}
	return out.String()
}

func TestS7InitialDraftNaturalObservationAndExplicitSave(t *testing.T) {
	for _, mode := range []string{"write", "edit", "changed"} {
		t.Run(mode, func(t *testing.T) {
			root := worktreeProjectRoot(t)
			state := stageSnapshot(t, root).State
			path := review.PlanQuestionnairePath(root, state)
			s7MainNaturalPre(t, root)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			guideInfo, err := os.Stat(strings.TrimSuffix(path, ".json") + ".md")
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			for i := 0; i < 2; i++ {
				s7MainNaturalPre(t, root)
				if review.PlanPointerFromState(stageSnapshot(t, root).State) != nil {
					t.Fatal("an unchanged generated draft was registered on ordinary Pre")
				}
			}
			t.Logf("S7 unchanged observations: calls=2 plan_bytes=%d guide_bytes=%d wall=%s", len(original), guideInfo.Size(), time.Since(start))
			// A path exposed by an unrelated tool is not evidence of a write.
			s7BaselinePost(t, root, "mcp__read", path)
			if review.PlanPointerFromState(stageSnapshot(t, root).State) != nil {
				t.Fatal("an unrelated MCP path was treated as an explicit save")
			}
			if mode == "changed" {
				var plan map[string]any
				if err := json.Unmarshal(original, &plan); err != nil {
					t.Fatal(err)
				}
				claim := plan["claims"].([]any)[0].(map[string]any)
				claim["target"] = "real product input containing TODO(planner)"
				data, _ := json.Marshal(plan)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				s7MainNaturalPre(t, root)
			} else {
				tool := "Write"
				if mode == "edit" {
					tool = "Edit"
				}
				s7BaselinePost(t, root, tool, path)
			}
			if review.PlanPointerFromState(stageSnapshot(t, root).State) == nil {
				t.Fatal("the natural author expression was not received")
			}
			guide := strings.TrimSuffix(path, ".json") + ".md"
			if err := os.Remove(guide); err != nil {
				t.Fatal(err)
			}
			s7MainNaturalPre(t, root)
			if _, err := os.Stat(guide); !os.IsNotExist(err) {
				t.Fatalf("accepted plan tried to recreate its initial observation baseline: %v", err)
			}
		})
	}
}

func TestS7OldWithoutBaselineAndWrongPlanIDContinueNaturally(t *testing.T) {
	root := worktreeProjectRoot(t)
	state := stageSnapshot(t, root).State
	path := review.PlanQuestionnairePath(root, state)
	s7MainNaturalPre(t, root)
	guide := strings.TrimSuffix(path, ".json") + ".md"
	if err := os.WriteFile(guide, []byte("# Existing historical guidance\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Missing old baseline does not require another author action.
	s7MainNaturalPre(t, root)
	ptr := review.PlanPointerFromState(stageSnapshot(t, root).State)
	if ptr == nil {
		t.Fatal("old draft without a baseline was blocked")
	}
	newPath := minimalReviewPlan(t, t.TempDir(), "review-plan-user-different-id")
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s7MainNaturalPre(t, root)
	snap := stageSnapshot(t, root)
	accepted, after, err := review.LoadPlan(root, snap.State)
	if err != nil || after.PlanID != ptr.PlanID || accepted.Claims[0].Assertion != "REQ covered" {
		t.Fatalf("same original responsibility failed to retain identity and accept expression: %+v %v", after, err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("reception overwrote the original authored file")
	}
}
