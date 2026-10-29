package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func receivedS6MainPre(t *testing.T, root string, sessions ...string) string {
	t.Helper()
	session := "received-s6-main"
	if len(sessions) > 0 {
		session = sessions[0]
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "session_id": session, "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "go.mod")}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("Main Pre: %d %s %s", code, &out, &diagnostic)
	}
	return out.String()
}

func TestNativeS6RecordedResultProjectsActualUnfinishedWork(t *testing.T) {
	for _, disposition := range []string{"blocked", "incomplete", "complete"} {
		t.Run(disposition, func(t *testing.T) {
			root, source, r := nativeDeliveryFixtureVersion(t, "1.3.0")
			nativeCommit(t, source)
			nativeAnswer(t, source, r)
			brief, _, resultPath := assignmentrecord.NativePaths(r)
			data, err := os.ReadFile(filepath.Join(source, resultPath))
			if err != nil {
				t.Fatal(err)
			}
			var answer map[string]any
			if err := json.Unmarshal(data, &answer); err != nil {
				t.Fatal(err)
			}
			closing := answer["closing_assessment"].(map[string]any)
			closing["disposition"] = disposition
			if disposition != "complete" {
				answer["risks_blockers"] = []string{"Actual dependency service remains unavailable"}
				answer["scope_deviations"] = []string{"Required API branch still unfinished"}
				condition := closing["conditions"].([]any)[0].(map[string]any)
				condition["judgment"] = disposition
				condition["rationale"] = "Required integration fixture has not been exercised"
			}
			stageJSON(t, source, resultPath, answer)
			if code, out := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, resultPath), false); code != 0 {
				t.Fatalf("Stop: %d %s", code, out)
			}
			receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
			if !strings.Contains(receipt, `"accepted":true`) || !strings.Contains(receipt, `"work_complete":`+map[bool]string{true: "true", false: "false"}[disposition == "complete"]) {
				t.Fatal(receipt)
			}
			stageJSON(t, source, resultPath, map[string]any{"summary": "UNRECEIVED PRIVATE ANSWER"})
			// The first ordinary Pre observation may persist the existing Gate
			// milestone. Once that original writer has observed this delivery,
			// projecting its professional answer must introduce no Runtime facts.
			receivedS6MainPre(t, root, "received-s6-warmup")
			before, _ := json.Marshal(stageSnapshot(t, root).State)
			journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{mainNativeReturn(t, root, filepath.Join(root, brief)), receivedS6MainPre(t, root)} {
				for _, want := range []string{"Result received", "recorded disposition: " + disposition, "Main checks", "task-receipt", "identical received Result"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q: %s", want, text)
					}
				}
				if strings.Contains(text, "UNRECEIVED PRIVATE ANSWER") || strings.Contains(text, "for merge, verification, acknowledgement and cleanup") || strings.Contains(text, "unassociated checkout") {
					t.Fatal(text)
				}
				if disposition != "complete" {
					for _, want := range []string{"Actual dependency service remains unavailable", "Required API branch still unfinished", "Required integration fixture has not been exercised", "Done when[0]", "--revise-assignment", "--revision-intent-key", r.Task.ID, r.RoleFamily, "before merge", "With that original rework checkpoint"} {
						if !strings.Contains(text, want) {
							t.Fatalf("missing %q: %s", want, text)
						}
					}
					if strings.Contains(text, "ready for Main") {
						t.Fatalf("unfinished work treated as qualified delivery: %s", text)
					}
				} else {
					for _, want := range []string{"task-integrate", "after an actual Main check failure", "--retry-preserved"} {
						if !strings.Contains(text, want) {
							t.Fatalf("missing %q: %s", want, text)
						}
					}
				}
			}
			after, _ := json.Marshal(stageSnapshot(t, root).State)
			afterJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
			if !bytes.Equal(before, after) || !bytes.Equal(journal, afterJournal) {
				t.Fatal("read-only projection mutated Runtime")
			}
			// Losing the immutable accepted source must not fall back to the private answer.
			agents := stageSnapshot(t, root).State["entities"].(map[string]any)["agents"].([]any)
			var acceptedPath string
			for _, raw := range agents {
				a := raw.(map[string]any)
				if a["id"] == r.OwnerAgentID {
					acceptedPath, _ = a["completion_reported_ref"].(string)
				}
			}
			if acceptedPath == "" {
				t.Fatal("missing accepted path")
			}
			acceptedBytes, err := os.ReadFile(filepath.Join(root, acceptedPath))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(root, acceptedPath)); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{mainNativeReturn(t, root, filepath.Join(root, brief)), receivedS6MainPre(t, root)} {
				for _, want := range []string{filepath.Join(root, acceptedPath), "unavailable", "task-receipt", r.AssignmentID, "No completion or integration eligibility can be inferred"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q: %s", want, text)
					}
				}
				if strings.Contains(text, "UNRECEIVED PRIVATE ANSWER") || strings.Contains(text, "recorded disposition:") {
					t.Fatal(text)
				}
			}
			if disposition != "complete" {
				if err := os.WriteFile(filepath.Join(root, acceptedPath), acceptedBytes, 0600); err != nil {
					t.Fatal(err)
				}
				head, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				var blockedOut, blockedErr bytes.Buffer
				code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, nil, &blockedOut, &blockedErr)
				if code != 1 || !strings.Contains(blockedErr.String(), "closing contract is pending") {
					t.Fatalf("original closing blocker not returned: %d %s %s", code, &blockedOut, &blockedErr)
				}
				checkpoint, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPathForRecord(root, r.Reference()))
				if err != nil || !found || checkpoint.State != integration.StatePreserved || checkpoint.MergeCommit != "" {
					t.Fatalf("original closing blocker checkpoint missing: %+v %v", checkpoint, err)
				}
				afterHead, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
				if err != nil || head != afterHead {
					t.Fatal("unclosed accepted Result was merged")
				}
				prepared := stageCLI(t, "s6", "prepare", "--root", root, "--task", r.Task.ID, "--role", r.RoleFamily, "--revise-assignment", r.AssignmentID, "--revision-intent-key", "actual-unfinished-result-correction")
				records, err := assignmentrecord.RecordsFromState(stageSnapshot(t, root).State)
				if err != nil {
					t.Fatal(err)
				}
				current, found, err := assignmentrecord.FindRecord(records, r.RuntimeID, r.BaselineGeneration, r.AssignmentID)
				if err != nil || !found || current.Revision != r.Revision+1 || !strings.Contains(prepared, "native Agent") {
					t.Fatalf("original revision preparation not reachable: %+v %v %s", current, err, prepared)
				}
				afterHead, err = workspace.Git(t.Context(), root, "rev-parse", "HEAD")
				if err != nil || head != afterHead {
					t.Fatal("rework preparation changed Main HEAD")
				}
			}
		})
	}
}
