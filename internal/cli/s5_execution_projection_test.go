package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5RevisionHookProjectsOnlyExactExecution(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	old := f.prepare(t, p)
	f.begin(t, p, &old)
	accepted := saveStageHook(t, f.root, old.Path, old.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	oldAnswer, err := os.ReadFile(filepath.Join(old.Path, workpacket.WorkerAnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.Revise(context.Background(), accepted.SubmissionKey, "Recheck the specific frozen observation")
	if err != nil {
		t.Fatal(err)
	}
	fresh := f.prepare(t, next)
	before := stageSnapshot(t, f.root).Revision
	for _, event := range []string{"SessionStart", "SubagentStart"} {
		code, message := stageHookCode(t, f.root, fresh.Path, fresh.AgentID, event, "")
		if code != 0 || !strings.Contains(message, filepath.Join(fresh.Path, workpacket.WorkerAnswerPath)) || strings.Contains(message, old.Path) || strings.Contains(message, "is accepted") || strings.Contains(message, "retired/superseded") {
			t.Fatalf("new execution inherited historical projection: %d %s", code, message)
		}
	}
	if code, message := stageHookCode(t, f.root, fresh.Path, fresh.AgentID, "Stop", ""); code != 2 || !strings.Contains(message, filepath.Join(fresh.Path, workpacket.WorkerAnswerPath)) || strings.Contains(message, "S6") {
		t.Fatalf("new responsibility was not independently pending: %d %s", code, message)
	}
	if code, message := stageHookCode(t, f.root, old.Path, old.AgentID, "Stop", ""); code != 0 || !strings.Contains(message, p.ID) || strings.Contains(message, fresh.Path) {
		t.Fatalf("late historical Stop borrowed current responsibility: %d %s", code, message)
	}
	if code, message := stageHookCode(t, f.root, fresh.Path, f.packets[1].Binding.Owner, "Stop", ""); code != 2 || !strings.Contains(message, "different responsibilities") {
		t.Fatalf("conflicting actor/root was selected: %d %s", code, message)
	}
	if code, message := stageHookCode(t, f.root, f.root, fresh.AgentID, "Stop", ""); code != 2 || !strings.Contains(message, "exact registered execution root") {
		t.Fatalf("unassociated root selected by owner: %d %s", code, message)
	}
	if code, message := stageHookCode(t, f.root, fresh.Path, f.packets[1].Binding.Owner, "PreToolUse", ""); code != 0 || strings.Contains(message, `"permissionDecision":"deny"`) {
		t.Fatalf("ordinary tool was blocked: %d %s", code, message)
	}
	// The exact installed execution remains sufficient when the top-level
	// platform payload omits actor/session fields. No new credential is needed.
	if code, message := stageHookCode(t, f.root, fresh.Path, "", "Stop", ""); code != 2 || !strings.Contains(message, filepath.Join(fresh.Path, workpacket.WorkerAnswerPath)) || strings.Contains(message, old.Path) {
		t.Fatalf("exact execution required a new platform identity: %d %s", code, message)
	}
	for _, cwd := range []string{fresh.Path, f.root} {
		body, err := json.Marshal(map[string]any{"hook_event_name": "Stop", "agent_id": fresh.AgentID, "cwd": cwd, "stop_hook_active": true})
		if err != nil {
			t.Fatal(err)
		}
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", f.root, "--event", "Stop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("active Stop hook repeated its block at %s: %d %s %s", cwd, code, &out, &diagnostic)
		}
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("read-only execution projections changed Runtime")
	}
	received := saveStageHook(t, f.root, fresh.Path, fresh.AgentID, workpacket.WorkerAnswerPath, completedDraft(next, "pass"))
	if !received.WorkComplete {
		t.Fatalf("new answer not received: %+v", received)
	}
	after := stageSnapshot(t, f.root).Revision
	replay := saveStageHook(t, f.root, fresh.Path, fresh.AgentID, workpacket.WorkerAnswerPath, completedDraft(next, "pass"))
	if !replay.WorkComplete || stageSnapshot(t, f.root).Revision != after {
		t.Fatal("answer replay repeated reception")
	}
	if preserved, err := os.ReadFile(filepath.Join(old.Path, workpacket.WorkerAnswerPath)); err != nil || !bytes.Equal(preserved, oldAnswer) {
		t.Fatalf("new attempt changed original answer bytes: %v", err)
	}
}
