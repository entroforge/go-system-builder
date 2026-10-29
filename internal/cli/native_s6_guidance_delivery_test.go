package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6StartRequiresExactResponsibility(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(r)
	before := stageSnapshot(t, root)
	for _, scenario := range []struct {
		actor, role, task string
		want              bool
	}{
		{"unrelated-explorer", "Explore", "Read sources only", false},
		{"unrelated-same-role", "backend-builder", "Read sources only", false},
		{"unknown-without-prompt", "backend-builder", "", false},
		{"opaque-native", "backend-builder", filepath.Join(root, brief), true},
		{r.OwnerAgentID, "backend-builder", "", true},
	} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStart", "agent_id": scenario.actor, "agent_type": scenario.role, "cwd": source, "last_assistant_message": scenario.task})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStart"}, bytes.NewReader(body), &out, &diagnostic)
		if code != 0 || strings.Contains(out.String(), "commit and save Result") != scenario.want {
			t.Fatalf("%s Start: %d %s %s", scenario.actor, code, &out, &diagnostic)
		}
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("Start created responsibility facts")
	}
}

func TestNativeS6MainDistinguishesPendingBlockerAndReceptionFailure(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(r)
	briefPath, resultPath := filepath.Join(root, brief), filepath.Join(source, result)
	before := stageSnapshot(t, root)
	pending := mainNativeReturnResponse(t, root, briefPath, map[string]any{"status": "async_launched", "isAsync": true})
	if !strings.Contains(pending, "launch acknowledgement") || strings.Contains(pending, "Native return for ") || strings.Contains(pending, "Main reads the actual native return") {
		t.Fatalf("pending execution: %s", pending)
	}
	marker := "Required private fixture x17 is unavailable"
	stageJSON(t, source, result, map[string]any{"summary": "Checks unfinished", "risks_blockers": []string{marker}, "closing_assessment": map[string]any{"disposition": "blocked"}})
	if code, output := nativeEvent(t, root, source, "SubagentStop", briefPath+"\n"+resultPath, false); code != 0 {
		t.Fatalf("blocker: %d %s", code, output)
	}
	// A later edit must not be mistaken for a newly received professional answer.
	stageJSON(t, source, result, map[string]any{"summary": "UNRECEIVED PRIVATE EDIT"})
	blocked := mainNativeReturn(t, root, briefPath)
	for _, want := range []string{marker, "blocked handoff", r.AssignmentID, briefPath, resultPath, "Main reads the actual native return"} {
		if !strings.Contains(blocked, want) {
			t.Fatalf("missing %q in Main context: %s", want, blocked)
		}
	}
	if strings.Contains(blocked, "UNRECEIVED PRIVATE EDIT") || blocked == pending {
		t.Fatalf("blocker lost its attempted-reception meaning: %s", blocked)
	}
	// No Plan makes this a different, concrete reception failure.
	if code, output := nativeEvent(t, root, source, "SubagentStop", briefPath+"\n"+resultPath, false); code != 2 {
		t.Fatalf("missing Plan: %d %s", code, output)
	}
	// A blocked Stop has not established a completed platform return. Its
	// recorded diagnostic remains visible in an explicit unknown observation.
	failure := mainNativeReturnResponse(t, root, briefPath, map[string]any{"status": "unknown"})
	if !strings.Contains(failure, "Platform execution is unknown here") || !strings.Contains(failure, "Stop returned code 2 to the Worker") || !strings.Contains(failure, "reception failure") || !strings.Contains(failure, "professional Plan") || strings.Contains(failure, marker) || strings.Contains(failure, "Native return for ") {
		t.Fatalf("failure did not replace blocker: %s", failure)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("display diagnostic created completion facts")
	}
	// Later ordinary Main observation retains the specific error.
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "session_id": "failure-resume", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "go.mod")}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "professional Plan") {
		t.Fatalf("later observation: %d %s %s", code, &out, &diagnostic)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", briefPath+"\n"+resultPath, true); code != 0 {
		t.Fatalf("bounded second Stop: %d %s", code, output)
	}
	returned := mainNativeReturn(t, root, briefPath)
	if !strings.Contains(returned, "Stop allowed the incomplete return to Main") || strings.Contains(returned, "Stop returned code 2") {
		t.Fatalf("Main missed actual Stop outcome: %s", returned)
	}
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	if code, output := nativeEvent(t, root, source, "SubagentStop", briefPath+"\n"+resultPath, false); code != 0 {
		t.Fatalf("recovery: %d %s", code, output)
	}
	accepted := mainNativeReturn(t, root, briefPath)
	if !strings.Contains(accepted, "Result received") || strings.Contains(accepted, "reception failure") || strings.Contains(accepted, marker) {
		t.Fatalf("successful reception retained stale diagnostic: %s", accepted)
	}
}

func TestNativeS6FixedSourceAlignmentAfterMainAdvances(t *testing.T) {
	root, _, r := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(r)
	sourceRef := filepath.Join(root, ".claude/assignments/native", r.AssignmentID, "r1/source.json")
	pinned, _ := os.ReadFile(sourceRef)
	var source struct {
		Base string `json:"base_commit"`
	}
	if err := json.Unmarshal(pinned, &source); err != nil {
		t.Fatal(err)
	}
	paths := append([]string{r.Task.Ref, "src/pkg/input_test.go", "go.mod"}, r.ReadPaths...)
	fixed := map[string][]byte{}
	for _, path := range paths {
		if _, exists := fixed[path]; exists {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		fixed[path] = body
		if err := os.WriteFile(filepath.Join(root, path), append(body, []byte("\nLater Main source change\n")...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := workspace.Git(t.Context(), root, "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "commit", "-m", "Main advances after native preparation"); err != nil {
		t.Fatal(err)
	}
	later, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(t.TempDir(), "late-native-worktree")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/late-head", worker, "HEAD"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	_, command, ok := strings.Cut(string(body), "align using this exact ordinary Git command: `")
	if !ok {
		t.Fatal("no executable alignment in actual brief")
	}
	command, _, ok = strings.Cut(command, "`")
	if !ok {
		t.Fatal("unterminated alignment command")
	}
	run := exec.CommandContext(t.Context(), "sh", "-c", command)
	run.Dir = worker
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("brief alignment: %v %s", err, output)
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD"); head != source.Base {
		t.Fatalf("alignment used %s instead of %s", head, source.Base)
	}
	for path, want := range fixed {
		got, err := os.ReadFile(filepath.Join(worker, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("actual fixed read set changed at %s: %v", path, err)
		}
	}
	if head, _ := workspace.Git(t.Context(), root, "rev-parse", "HEAD"); head != later {
		t.Fatal("alignment changed Main")
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "native/late-head"); head != later {
		t.Fatal("alignment discarded platform source branch")
	}
	if got, _ := os.ReadFile(sourceRef); !bytes.Equal(got, pinned) {
		t.Fatal("alignment repinned original source")
	}
	// Retrying cannot overwrite either an existing branch or personal work.
	nativeCommit(t, worker)
	personal, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
	dirty := filepath.Join(worker, "src/pkg/unfinished.txt")
	if err := os.WriteFile(dirty, []byte("preserve ongoing work"), 0600); err != nil {
		t.Fatal(err)
	}
	run = exec.CommandContext(t.Context(), "sh", "-c", command)
	run.Dir = worker
	if output, err := run.CombinedOutput(); err == nil {
		t.Fatalf("existing fixed-source branch overwritten: %s", output)
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD"); head != personal {
		t.Fatal("alignment retry discarded personal commits")
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != "preserve ongoing work" {
		t.Fatal("alignment retry lost unfinished work")
	}
}

func TestNativeS6ClosedResponsibilityPrecedesMissingReceiptAndDiagnostic(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(r)
	briefPath := filepath.Join(root, brief)
	marker := "retired fixture blocker"
	stageJSON(t, source, result, map[string]any{"summary": "unfinished", "risks_blockers": []string{marker}, "closing_assessment": map[string]any{"disposition": "blocked"}})
	if code, output := nativeEvent(t, root, source, "SubagentStop", briefPath+"\n"+filepath.Join(source, result), false); code != 0 {
		t.Fatalf("blocker: %d %s", code, output)
	}
	state := stageSnapshot(t, root).State
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID {
			row["state"] = "stopped"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", state)
	text := mainNativeReturn(t, root, briefPath)
	if !strings.Contains(text, "Original TASK is closed") || strings.Contains(text, marker) || strings.Contains(text, "A background launch can return") {
		t.Fatalf("closed responsibility revived without receipt: %s", text)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStart", briefPath, false); code != 0 || strings.Contains(output, "commit and save Result") {
		t.Fatalf("closed Start restarted implementation: %d %s", code, output)
	}
}
