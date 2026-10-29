package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS2OrdinaryObservationKeepsFullProofAtOriginalSave(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeMetrics, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": "docs/project-map.md"}})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("ordinary %s: %d %s %s", event, code, &out, &diagnostic)
		}
		if !strings.Contains(out.String(), filepath.Join(root, packet.ViewPath)) || !strings.Contains(out.String(), filepath.Join(root, packet.AnswerPath)) {
			t.Fatalf("%s lost the original S2 responsibility: %s", event, &out)
		}
		if after, _ := os.ReadFile(statePath); !bytes.Equal(after, beforeState) {
			t.Fatalf("ordinary %s still runs a mutating full S2 control cycle", event)
		}
		if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, beforeJournal) {
			t.Fatalf("ordinary %s changed original S2 journal", event)
		}
		afterMetrics, err := metrics.NewStore(root).Read()
		if err != nil || !reflect.DeepEqual(afterMetrics.GateEvaluations, beforeMetrics.GateEvaluations) {
			t.Fatalf("ordinary %s re-evaluated full quality gates: before=%v after=%v err=%v", event, beforeMetrics.GateEvaluations, afterMetrics.GateEvaluations, err)
		}
		if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID); err != nil || accepted {
			t.Fatalf("ordinary %s received private professional work: %t %v", event, accepted, err)
		}
	}
	// Only the original professional save receives the answer and progresses.
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "S2 SUBMISSION RECEIPT") {
		t.Fatalf("original design save did not return its receipt: %d %s %s", code, &out, &diagnostic)
	}
	snapshot := stageSnapshot(t, root)
	record, accepted, err := runtime.FindSubmission(snapshot.State, packet.ID)
	if err != nil || !accepted || record.ControlStatus != "advanced" || snapshot.State["lifecycle"].(map[string]any)["phase"] != "contracts" {
		t.Fatalf("S2 original save lost full proof or followup: %+v %v", record, err)
	}
}
