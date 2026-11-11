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
		// The responsible actor receives the original S6 responsibility context;
		// an unrelated or unbound actor does not.
		if code != 0 || strings.Contains(out.String(), "save Result in your own worktree") != scenario.want {
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

// The fixed-source alignment is retired: the native preparation registers no
// source proof, the brief pins no Git OID, and the Builder works on the actual
// platform commit. What still has to hold is the ordinary Git preservation
// around that: the delivery's read set is not rewritten, Main's own HEAD is not
// moved, the platform source branch keeps the actual platform commit, and a
// repeated preparation neither overwrites that branch nor discards the
// Builder's personal commit or unfinished work.
//
// L4-worktree-governance.md:140 — "不计算、登记或匹配 source／target／merge／tested
// 的 Git OID"; L4-workflow-convergence.md:312 — "旧入口、旧 schema、旧 Hook／CLI、
// 旧消费者及兼容适配连同仅为它们存在的测试一并删除"; L3-S6-build.md:316 — "删除
// frozen execution 的观察、接收与恢复分支".
func TestNativeS6FixedSourceAlignmentAfterMainAdvances(t *testing.T) {
	root, _, r := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(r)
	sourceRef := filepath.Join(root, ".claude/assignments/native", r.AssignmentID, "r1/source.json")
	// The retired source proof is not registered.
	if _, err := os.Stat(sourceRef); !os.IsNotExist(err) {
		t.Fatalf("native preparation registered the retired source proof: %v", err)
	}
	// The brief pins no Git OID and names the actual inputs instead. This is
	// stronger than the retired assertion: no OID pinning of any shape may
	// exist, not only one pinned base.
	body, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, retired := range []string{
		"align using this exact ordinary Git command",
		"git switch -c",
		"git rev-parse HEAD",
		"First start, before any responsibility work:",
		"even when HEAD no longer equals",
		"Unknown or mixed source:",
	} {
		if strings.Contains(text, retired) {
			t.Fatalf("retired fixed-source pinning %q reappeared in the brief: %s", retired, text)
		}
	}
	for _, want := range []string{
		"Read the actual TASK at worktree-relative path",
		"Read TASK, specifications and product sources in your actual worktree",
		"Preserve existing work; report missing inputs to Main",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("current brief missing %q: %s", want, text)
		}
	}
	// Main advances after the native preparation: the delivery's read set moves
	// on with Main. The marker stays valid for each file type so the Builder's
	// own real checks can still run in the worktree.
	marker := func(path string) []byte {
		switch {
		case strings.HasSuffix(path, ".go"), filepath.Base(path) == "go.mod":
			return []byte("\n// Later Main source change\n")
		default:
			return []byte("\nLater Main source change\n")
		}
	}
	paths := append([]string{r.Task.Ref, "src/pkg/input_test.go", "go.mod"}, r.ReadPaths...)
	current := map[string][]byte{}
	for _, path := range paths {
		if _, exists := current[path]; exists {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		advanced := append(body, marker(path)...)
		current[path] = advanced
		if err := os.WriteFile(filepath.Join(root, path), advanced, 0600); err != nil {
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
	// The Builder works on the actual platform commit; no retired base is
	// pinned for it.
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD"); head != later {
		t.Fatalf("native source is not on the actual platform commit %s: %s", later, head)
	}
	// The actual read set is not rewritten: the worker reads exactly the
	// delivery's current read set, not a stale pinned copy.
	for path, want := range current {
		got, err := os.ReadFile(filepath.Join(worker, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("actual read set changed at %s: %v", path, err)
		}
	}
	// Main's own HEAD is not moved by the native work.
	if head, _ := workspace.Git(t.Context(), root, "rev-parse", "HEAD"); head != later {
		t.Fatal("native preparation changed Main")
	}
	// The platform source branch still points at the actual platform commit.
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "native/late-head"); head != later {
		t.Fatal("native preparation discarded platform source branch")
	}
	// The retired source proof is still not registered after Main advanced.
	if _, err := os.Stat(sourceRef); !os.IsNotExist(err) {
		t.Fatalf("Main advance republished the retired source proof: %v", err)
	}
	// Repeating the preparation cannot overwrite an existing branch or personal
	// work.
	nativeCommit(t, worker)
	personal, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
	dirty := filepath.Join(worker, "src/pkg/unfinished.txt")
	if err := os.WriteFile(dirty, []byte("preserve ongoing work"), 0600); err != nil {
		t.Fatal(err)
	}
	// The repeated preparation really runs again and hands the original
	// responsibility back, so the preservation facts below are observed after a
	// real second preparation and not after a skipped one.
	if code, output := nativeEvent(t, root, worker, "SubagentStart", filepath.Join(root, brief), false); code != 0 || !strings.Contains(output, "Native S6") {
		t.Fatalf("repeated preparation did not deliver the original responsibility: %d %s", code, output)
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD"); head != personal {
		t.Fatal("repeated preparation discarded personal commits")
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != "preserve ongoing work" {
		t.Fatal("repeated preparation lost unfinished work")
	}
	// The existing branch is not overwritten by the repeated preparation: it
	// still carries the Builder's own commit instead of being reset to the
	// platform commit or moved elsewhere.
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "native/late-head"); head != personal {
		t.Fatalf("repeated preparation overwrote the existing branch: %s", head)
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "native/late-head"); head == later {
		t.Fatal("repeated preparation reset the existing branch to the platform commit")
	}
	// The retired source proof is still not registered after the repeat.
	if _, err := os.Stat(sourceRef); !os.IsNotExist(err) {
		t.Fatalf("repeated preparation republished the retired source proof: %v", err)
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
