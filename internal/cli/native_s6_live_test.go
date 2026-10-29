package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This opt-in test spends real model requests. The subprocess is the native
// parent session: only Claude Code itself creates the Builder and its worktree.
// GSB_CLAUDE_LIVE=1 GSB_CLAUDE_ENV_FILE=~/.claude/minimax go test ./internal/cli -run '^TestClaudeCodeNativeS6Live$' -v -timeout 30m
func TestClaudeCodeNativeS6Live(t *testing.T) {
	if os.Getenv("GSB_CLAUDE_LIVE") != "1" {
		t.Skip("opt-in real Claude Code/model test; set GSB_CLAUDE_LIVE=1")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foreground", "background", "resume"} {
		t.Run(name, func(t *testing.T) { runClaudeNativeS6Live(t, name, name == "background") })
	}
}

func runClaudeNativeS6Live(t *testing.T, name string, background bool) {
	t.Helper()
	artifacts, err := os.MkdirTemp("", "gsb-claude-native-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LIVE artifacts retained at %s", artifacts)
	root, task, taskPath := newS6RecordContextFixtureWithReadSources(t, map[string][]byte{
		"go.mod":              []byte("module native.live.fixture\n\ngo 1.26\n"),
		"src/pkg/input.go":    []byte("package pkg\n\nfunc Add(a, b int) int { return 0 }\n"),
		"src/pkg/sum_test.go": []byte("package pkg\nimport \"testing\"\nfunc TestAdd(t *testing.T) { if Add(2,3)!=5 || Add(-2,1)!=-1 { t.Fatal(\"Add must return the sum\") } }\n"),
	})
	t.Cleanup(func() {
		// Preserve machine evidence even when the real host run fails. The
		// disposable fixture itself is removed by testing after this cleanup.
		for _, relative := range []string{".claude/loop-state.json", ".claude/loop-events.jsonl", ".claude/hook-decisions.jsonl"} {
			if data, err := os.ReadFile(filepath.Join(root, relative)); err == nil {
				_ = os.WriteFile(filepath.Join(artifacts, filepath.Base(relative)), data, 0600)
			}
		}
		if data, err := workspace.Git(context.Background(), root, "log", "--format=fuller", "-8"); err == nil {
			_ = os.WriteFile(filepath.Join(artifacts, "git-log.txt"), []byte(data), 0600)
		}
	})
	taskBytes, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	taskBytes = append(taskBytes, []byte("\n## Implementation objective\n\nImplement Add(a,b int) in src/pkg/input.go to return a+b. Preserve the committed tests; cover positive and negative operands. Save a professional plan before editing, run go test ./src/pkg, commit only product changes, and return the actual professional Result.\n")...)
	if err := os.WriteFile(filepath.Join(root, taskPath), taskBytes, 0600); err != nil {
		t.Fatal(err)
	}
	state := stageSnapshot(t, root).State
	state["schema_version"] = "1.3.0"
	for _, raw := range state["documents"].([]any) {
		row := raw.(map[string]any)
		if row["path"] == taskPath {
			row["sha256"] = workpacket.Digest(taskBytes)
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", state)
	if _, err := workspace.Git(t.Context(), root, "add", taskPath); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "commit", "-m", "Reviewed live Add implementation objective"); err != nil {
		t.Fatal(err)
	}
	red := exec.CommandContext(t.Context(), "go", "test", "./src/pkg")
	red.Dir = root
	redOutput, redErr := red.CombinedOutput()
	_ = os.WriteFile(filepath.Join(artifacts, "baseline-red.log"), redOutput, 0600)
	if redErr == nil {
		t.Fatal("live baseline must really fail before the model implements Add")
	}
	binary := filepath.Join(root, ".claude/bin/loop-harness")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/loop-harness")
	build.Dir = filepath.Clean(filepath.Join("..", ".."))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual Hook binary: %v %s", err, output)
	}
	settingsData, err := os.ReadFile(filepath.Join(build.Dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "original-settings.json"), settingsData, 0600); err != nil {
		t.Fatal(err)
	}
	recorder := filepath.Join(artifacts, "observe-hook.py")
	if err := os.WriteFile(recorder, []byte(claudeNativeHookObserver), 0700); err != nil {
		t.Fatal(err)
	}
	for event, raw := range settings["hooks"].(map[string]any) {
		for i, registration := range raw.([]any) {
			for j, rawHook := range registration.(map[string]any)["hooks"].([]any) {
				hook := rawHook.(map[string]any)
				hook["command"] = fmt.Sprintf("python3 %s %s %s %s %d %d", liveShellQuote(recorder), liveShellQuote(artifacts), liveShellQuote(root), liveShellQuote(event), i, j)
			}
		}
	}
	stageJSON(t, root, ".claude/settings.json", settings)
	stageCLI(t, "s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder")
	before := stageSnapshot(t, root)
	r, found, err := assignmentrecord.ForOwner(before.State, "native-"+task)
	if err != nil || !found {
		t.Fatalf("prepared engineering Record: %+v %v", r, err)
	}
	brief, _, _ := assignmentrecord.NativePaths(r)
	prompt := fmt.Sprintf(`This is a real native S6 platform acceptance test in the disposable repository %s. Work only in this repository and its native worktrees. The S5/TASK inputs are committed test preconditions; no S6 result or check PASS exists.
Preparation is complete. Your first tool call must use the native Agent tool to delegate to subagent_type backend-builder with isolation=worktree, name=live-%s, run_in_background=%t. Do not implement the TASK yourself. Do not start a separate Claude process, Team, workspace launcher, or framework begin/activation. Carry the EXACT absolute task brief %s in the child's task prompt and tell it to read that brief and execute the real Add objective. The child must personally save its professional plan, implement Add, execute go test ./src/pkg, commit the product and save its Result in its actual native worktree. Return the exact brief and absolute Result reference; state blockers honestly. Never author or inject a completion receipt.
If background=true, immediately after launching write a short progress note to %s while the child runs, then wait using Claude Code's native agent mechanisms. Do not assume async launch is completion.
After the real child returns, use Bash at Main to run exactly: %s runtime task-integrate --root %s --assignment-id %s --agent-id %s
This is Main integration, separate from the child's Hook. Do not manufacture runtime fields or repair machine files. If blocked, report the actual error. After integration, end this S6-only diagnostic turn with a concise factual result. Do not start S7 or unrelated work.`, root, name, background, filepath.Join(root, brief), filepath.Join(artifacts, "background-parent-progress.txt"), binary, root, r.AssignmentID, r.OwnerAgentID)
	if name == "resume" {
		prompt += "\nAdditional RESUME scenario: after the first native child return, BEFORE Main integration, use SendMessage with to=the ACTUAL returned agent ID and message containing the exact original brief and Result references. Ask that same child to verify its existing committed product and existing professional Result without editing either, then return the exact brief and absolute Result again. Do not spawn a replacement. Wait for the actual completion notification and original SubagentStop before integrating; immediate SendMessage success only confirms sending. Integrate only after this native resumed return."
	}
	_ = os.WriteFile(filepath.Join(artifacts, "prompt.txt"), []byte(prompt), 0600)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	script := `source "$1" >/dev/null 2>&1; shift; exec claude -p --permission-mode bypassPermissions --output-format stream-json --verbose --include-hook-events --setting-sources user,project --strict-mcp-config --mcp-config '{"mcpServers":{}}' --no-chrome`
	envFile := os.Getenv("GSB_CLAUDE_ENV_FILE")
	if envFile == "" {
		t.Fatal("GSB_CLAUDE_ENV_FILE must identify the user-authorized provider configuration")
	}
	cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script, "claude-native-live", envFile)
	cmd.Dir, cmd.Stdin = root, strings.NewReader(prompt)
	stdout, err := os.Create(filepath.Join(artifacts, "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.Create(filepath.Join(artifacts, "claude.stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("real Claude session failed after %s: %v; inspect %s", time.Since(started), err, artifacts)
	}
	stream, err := os.ReadFile(filepath.Join(artifacts, "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stream, []byte(`"name":"Agent"`)) && !bytes.Contains(stream, []byte(`"name":"Task"`)) {
		t.Fatal("no actual native Agent tool call in the real transcript")
	}
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
	if err != nil || !found || checkpoint.State != integration.StateComplete || checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("real Main did not integrate/verify/ack/clean up: %+v %v; inspect %s", checkpoint, err, artifacts)
	}
	accepted := stageSnapshot(t, root)
	resultRef := ""
	for _, raw := range accepted.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID {
			if row["activation_ref"] != nil || row["readback_ref"] != nil {
				t.Fatal("real run fabricated activation/readback")
			}
			resultRef, _ = row["completion_reported_ref"].(string)
		}
	}
	if resultRef == "" {
		t.Fatal("no original accepted Result from actual SubagentStop")
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(receipt, `"integrated":true`) || !strings.Contains(receipt, `"work_complete":true`) {
		t.Fatalf("actual receipt incomplete: %s", receipt)
	}
	_ = os.WriteFile(filepath.Join(artifacts, "task-receipt.json"), []byte(receipt), 0600)
	checkpointBytes, _ := json.MarshalIndent(checkpoint, "", "  ")
	_ = os.WriteFile(filepath.Join(artifacts, "checkpoint.json"), checkpointBytes, 0600)
	resultBytes, _ := workpacket.ReadFile(root, resultRef, 20<<20)
	_ = os.WriteFile(filepath.Join(artifacts, "accepted-result.json"), resultBytes, 0600)
	platformID := verifyClaudeNativeLifecycle(t, artifacts, r.OwnerAgentID, background)
	verifyClaudeNativeCalls(t, stream, name, platformID, background)
	if err := cli.CheckNativeLiveContinuationEvidence(artifacts, stream, name == "resume"); err != nil {
		t.Fatal(err)
	}
	t.Logf("LIVE native %s completed in %s; real evidence: %s", name, time.Since(started), artifacts)
}

func verifyClaudeNativeCalls(t *testing.T, stream []byte, scenario, platformID string, background bool) {
	t.Helper()
	spawns, resumes, results := 0, 0, 0
	for _, line := range bytes.Split(stream, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		// User/tool result rows may use string content; only assistant tool
		// calls and the final result are relevant here.
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.Type == "result" {
			results++
			if row.IsError {
				t.Fatal("real model session returned an error result")
			}
		}
		if row.Type != "assistant" {
			continue
		}
		for _, tool := range row.Message.Content {
			if tool.Type != "tool_use" || (tool.Name != "Agent" && tool.Name != "Task" && tool.Name != "SendMessage") {
				continue
			}
			if tool.Name == "SendMessage" {
				resume, _ := tool.Input["to"].(string)
				resumes++
				if resume != platformID {
					t.Fatalf("native resume changed platform identity: %s != %s", resume, platformID)
				}
				continue
			}
			spawns++
			actualBackground, _ := tool.Input["run_in_background"].(bool)
			if tool.Input["subagent_type"] != "backend-builder" || tool.Input["isolation"] != "worktree" || actualBackground != background {
				t.Fatalf("unexpected actual native dispatch parameters: %+v", tool.Input)
			}
		}
	}
	wantResumes := 0
	if scenario == "resume" {
		wantResumes = 1
	}
	// Background notifications and resume can produce multiple native SDK
	// result frames. Completion is established by the actual Main receipt.
	if spawns != 1 || resumes != wantResumes || results == 0 {
		t.Fatalf("actual native calls: spawns=%d resumes=%d results=%d", spawns, resumes, results)
	}
}

func verifyClaudeNativeLifecycle(t *testing.T, artifacts, logicalOwner string, background bool) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	type observation struct {
		Event    string         `json:"event"`
		Started  int64          `json:"started_ns"`
		Finished int64          `json:"finished_ns"`
		Before   int            `json:"revision_before"`
		After    int            `json:"revision_after"`
		ExitCode int            `json:"exit_code"`
		Stdout   string         `json:"stdout"`
		Payload  map[string]any `json:"payload"`
	}
	starts, stops := map[string]int64{}, map[string]int64{}
	var parentProgress []int64
	ordinary := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var row observation
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		id, _ := row.Payload["agent_id"].(string)
		if id == "" {
			if row.Event == "PostToolUse" && row.Payload["tool_name"] == "Write" {
				response, _ := row.Payload["tool_response"].(map[string]any)
				if response["filePath"] == filepath.Join(artifacts, "background-parent-progress.txt") {
					parentProgress = append(parentProgress, row.Started)
				}
			}
			continue
		}
		if id == logicalOwner {
			t.Fatal("platform identity was replaced by the engineering owner")
		}
		switch row.Event {
		case "SubagentStart":
			starts[id] = row.Started
			if row.ExitCode != 0 || row.Before != row.After {
				t.Fatalf("Start mutated Runtime or failed: %+v", row)
			}
		case "SubagentStop":
			if row.ExitCode == 0 {
				stops[id] = row.Finished
			}
		default:
			ordinary++
			if row.ExitCode != 0 || row.Stdout != "" || (!background && row.Before != row.After) {
				t.Fatalf("ordinary Worker hook controlled execution: %+v", row)
			}
		}
	}
	if len(starts) == 0 || len(starts) != len(stops) || ordinary == 0 {
		t.Fatalf("missing real lifecycle/Worker events: starts=%v stops=%v ordinary=%d", starts, stops, ordinary)
	}
	for id, start := range starts {
		stop, found := stops[id]
		if !found || stop <= start {
			t.Fatalf("actual platform lifecycle does not match for %s", id)
		}
		if background {
			progressed := false
			for _, write := range parentProgress {
				progressed = progressed || (write > start && write < stop)
			}
			if !progressed {
				t.Fatalf("no actual Main Write while native background child ran: %v", parentProgress)
			}
		}
	}
	t.Logf("LIVE verified native identities %v; %d ordinary Worker events returned without execution control", starts, ordinary)
	// The continuation checker separately verifies every actual same-actor
	// Start/Stop and the real completion notification; no actor aliases.
	firstID, firstStart := "", int64(0)
	for id, start := range starts {
		if firstID == "" || start < firstStart {
			firstID, firstStart = id, start
		}
	}
	return firstID
}

func liveShellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

const claudeNativeHookObserver = `import sys,os,json,time,subprocess,uuid
from pathlib import Path
artifacts,main,event,i,j=sys.argv[1:]
raw=sys.stdin.buffer.read()
payload=json.loads(raw)
original=json.loads((Path(artifacts)/'original-settings.json').read_text())['hooks'][event][int(i)]['hooks'][int(j)]['command']
def revision():
 try: return json.loads((Path(main)/'.claude/loop-state.json').read_text()).get('revision')
 except Exception: return None
before=revision()
start=time.time_ns()
p=subprocess.run(['bash','--noprofile','--norc','-c',original],input=raw,capture_output=True)
row={'event':event,'started_ns':start,'finished_ns':time.time_ns(),'project_dir':os.environ.get('CLAUDE_PROJECT_DIR'),'process_cwd':os.getcwd(),'revision_before':before,'revision_after':revision(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'payload':payload}
(Path(artifacts)/('hook-'+str(start)+'-'+uuid.uuid4().hex+'.json')).write_text(json.dumps(row,ensure_ascii=False))
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr)
sys.exit(p.returncode)
`
