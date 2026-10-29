package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestS4RejectedSaveAttemptPreservesAcceptedResponsibility(t *testing.T) {
	for _, stage := range []string{"S4", "S5", "replaced"} {
		t.Run(stage, func(t *testing.T) {
			root, p := newTaskPlanningAnswerFixture(t)
			stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
			s, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := pendingMainFollowupContext(s, p.ID)
			accepted, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
			cancel()
			if err != nil || !accepted.Accepted {
				t.Fatalf("real S4 reception: %+v %v", accepted, err)
			}
			if stage == "S5" {
				stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
				settleMainPlanningFollowups(t, root)
			} else if stage == "replaced" {
				reworkCLI(t, root, "tasks")
			}
			before := stageSnapshot(t, root)
			original, found, err := runtime.FindSubmission(before.State, p.ID)
			if err != nil || !found {
				t.Fatalf("original reception missing: %v", err)
			}
			for _, malformed := range []bool{false, true} {
				draft := completedTaskPlanningDraft(p)
				if malformed {
					draft.Answer.Conclusion = nil
				} else {
					draft.Answer.TaskPaths = []string{"docs/dev/tasks/TASK-039-02.md"}
				}
				stageJSON(t, root, p.AnswerPath, draft)
				var out, diagnostic bytes.Buffer
				code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath}, nil, &out, &diagnostic)
				if code == 0 {
					t.Fatal("modified answer was accepted")
				}
				var receipt submission.Receipt
				if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
					t.Fatalf("receipt JSON: %v: %s %s", err, out.String(), diagnostic.String())
				}
				assertS4RejectedAttempt(t, stage, accepted, receipt)
				body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "cwd": root})
				out.Reset()
				diagnostic.Reset()
				if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
					t.Fatalf("hook: %d %s %s", code, out.String(), diagnostic.String())
				}
				var hook struct {
					Output struct {
						Context string `json:"additionalContext"`
					} `json:"hookSpecificOutput"`
				}
				if err := json.Unmarshal(out.Bytes(), &hook); err != nil {
					t.Fatalf("hook JSON: %v: %s", err, out.String())
				}
				_, encoded, ok := strings.Cut(hook.Output.Context, "S4 SUBMISSION RECEIPT: ")
				if !ok || json.Unmarshal([]byte(encoded), &receipt) != nil {
					t.Fatalf("hook receipt missing: %s", hook.Output.Context)
				}
				assertS4RejectedAttempt(t, stage, accepted, receipt)
				after := stageSnapshot(t, root)
				retained, _, err := runtime.FindSubmission(after.State, p.ID)
				if err != nil || after.Revision != before.Revision || !reflect.DeepEqual(original, retained) {
					t.Fatalf("rejected attempt changed history/revision: %d -> %d, %v", before.Revision, after.Revision, err)
				}
			}
		})
	}
}

func assertS4RejectedAttempt(t *testing.T, stage string, original, got submission.Receipt) {
	t.Helper()
	if !got.Accepted || got.AttemptStatus != "rejected" || got.Error == "" || got.SubmissionKey != original.SubmissionKey || !reflect.DeepEqual(got.DomainRefs, original.DomainRefs) || !strings.Contains(got.NextAction, "新内容未接收") {
		t.Fatalf("contradictory attempt receipt: %+v", got)
	}
	if stage == "replaced" {
		if got.WorkComplete || got.ResponsibilityStatus != "replaced" || !strings.Contains(got.NextAction, "当前监听") || strings.Contains(got.NextAction, "stage-answer rework") || strings.Contains(got.NextAction, "stage-answer recover") {
			t.Fatalf("old responsibility revived: %+v", got)
		}
	} else if !got.WorkComplete || got.ResponsibilityStatus != "accepted" {
		t.Fatalf("original current acceptance lost: %+v", got)
	}
	if stage == "S4" && !strings.Contains(got.NextAction, "--target tasks --reason '") {
		t.Fatalf("missing executable S4 rework: %s", got.NextAction)
	}
	if stage == "S5" && (!strings.Contains(got.NextAction, "真实发现") || strings.Contains(got.NextAction, "--target tasks")) {
		t.Fatalf("incorrect S5 route: %s", got.NextAction)
	}
}
