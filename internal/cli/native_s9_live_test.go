package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func s9LiveShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func verifyS9LiveNativeEvents(t *testing.T, artifacts string, stream []byte, want nativeLiveHandoffExpectation) {
	owner := want.Owner
	t.Helper()
	files, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	starts, stops := map[string]int64{}, map[string]bool{}
	ordinary := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			Event   string         `json:"event"`
			Started int64          `json:"started_ns"`
			Before  int            `json:"revision_before"`
			After   int            `json:"revision_after"`
			Exit    int            `json:"exit_code"`
			Stdout  string         `json:"stdout"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		id, _ := row.Payload["agent_id"].(string)
		if id == "" || id == "Main" {
			continue
		}
		if id == owner {
			t.Fatal("platform ID replaced by engineering owner")
		}
		switch row.Event {
		case "SubagentStart":
			starts[id] = row.Started
			if row.Exit != 0 || row.Before != row.After {
				t.Fatalf("Start controlled execution: %s", data)
			}
		case "SubagentStop":
			if row.Exit == 0 {
				stops[id] = true
			}
		default:
			ordinary++
			if row.Exit != 0 || row.Stdout != "" || row.Before != row.After {
				t.Fatalf("ordinary Worker hook managed execution: %s", data)
			}
		}
	}
	if len(starts) < 1 || len(stops) != len(starts) || ordinary == 0 {
		t.Fatalf("missing native lifecycle/ordinary events: starts=%v stops=%v ordinary=%d", starts, stops, ordinary)
	}
	first, timestamp := "", int64(0)
	for id, at := range starts {
		if !stops[id] {
			t.Fatalf("missing actual Stop for %s", id)
		}
		if first == "" || at < timestamp {
			first, timestamp = id, at
		}
	}
	spawn, resume := 0, 0
	sendID := ""
	for _, line := range bytes.Split(stream, []byte("\n")) {
		var row struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					ID    string         `json:"id"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		if row.Type == "result" && row.IsError {
			t.Fatal("real Claude returned an error result frame")
		}
		for _, part := range row.Message.Content {
			if part.Type != "tool_use" || part.Name != "Agent" && part.Name != "Task" && part.Name != "SendMessage" {
				continue
			}
			if part.Name == "SendMessage" {
				sendID = part.ID
				id, _ := part.Input["to"].(string)
				resume++
				if id != first {
					t.Fatalf("resume did not use actual original Agent ID: %s != %s", id, first)
				}
			} else {
				spawn++
				if part.Input["isolation"] != "worktree" {
					t.Fatal("real Agent did not own worktree isolation")
				}
			}
		}
	}
	if spawn != 1 || resume != 1 {
		t.Fatalf("expected one actual native spawn and resume: %d %d", spawn, resume)
	}
	if err := CheckNativeLiveContinuationEvidence(artifacts, stream, true); err != nil {
		t.Fatal(err)
	}
	want.Actor, want.ToolID, want.Continuation = first, sendID, true
	for _, line := range bytes.Split(stream, []byte("\n")) {
		var frame map[string]any
		if json.Unmarshal(line, &frame) == nil && frame["type"] == "system" && frame["subtype"] == "task_started" && frame["task_id"] == first && frame["tool_use_id"] == sendID {
			want.Run, _ = frame["run_id"].(string)
		}
	}
	if err := CheckNativeLiveHandoffEvidence(artifacts, stream, want); err != nil {
		t.Fatalf("Main engineering handoff after actual completion: %v", err)
	}
	t.Logf("LIVE S9 actual actors=%v; %d ordinary Worker hooks silent/read-only", starts, ordinary)
}

// Uses the real Claude parent/Agent/Hook boundary and a synthetic approved S8
// fixture. It verifies this S9 Builder slice, not a full S8/S9/S7 acceptance.
func TestClaudeCodeNativeS9Live(t *testing.T) {
	if os.Getenv("GSB_CLAUDE_LIVE") != "1" {
		t.Skip("opt-in real Claude Code/model test")
	}
	envFile := os.Getenv("GSB_CLAUDE_ENV_FILE")
	if envFile == "" {
		t.Fatal("GSB_CLAUDE_ENV_FILE is required; never print credentials")
	}
	artifacts, err := os.MkdirTemp("", "gsb-claude-native-s9-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LIVE S9 artifacts retained at %s", artifacts)
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	writeRepairLeadFile(t, root, "go.mod", "module native.s9.live\n\ngo 1.26\n")
	writeRepairLeadFile(t, root, "internal/api/one.go", "package api\nfunc Add(a, b int) int { return 0 }\n")
	writeRepairLeadFile(t, root, "internal/api/one_test.go", "package api\nimport \"testing\"\nfunc TestAdd(t *testing.T) { if Add(2,3)!=5 || Add(-2,1)!=-1 { t.Fatal(\"Add must return the sum\") } }\n")
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"skills", "agents"} {
		if err := filepath.WalkDir(filepath.Join(repo, dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			writeRepairLeadFile(t, root, filepath.ToSlash(filepath.Join(".claude", rel)), string(data))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	repairLeadCommitFixture(t, root)
	red := exec.CommandContext(t.Context(), "go", "test", "./internal/api")
	red.Dir = root
	redOutput, redErr := red.CombinedOutput()
	_ = os.WriteFile(filepath.Join(artifacts, "baseline-red.log"), redOutput, 0600)
	if redErr == nil {
		t.Fatal("live Add baseline must actually fail")
	}
	binary := filepath.Join(root, ".claude/bin/loop-harness")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/loop-harness")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Hook: %v %s", err, output)
	}
	settingsBytes, err := os.ReadFile(filepath.Join(repo, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(artifacts, "original-settings.json"), settingsBytes, 0600)
	var settings map[string]any
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatal(err)
	}
	recorder := filepath.Join(artifacts, "observe-hook.py")
	_ = os.WriteFile(recorder, []byte(s9LiveHookObserver), 0700)
	for event, raw := range settings["hooks"].(map[string]any) {
		for i, registration := range raw.([]any) {
			for j, hook := range registration.(map[string]any)["hooks"].([]any) {
				hook.(map[string]any)["command"] = fmt.Sprintf("python3 %s %s %s %s %d %d", s9LiveShellQuote(recorder), s9LiveShellQuote(artifacts), s9LiveShellQuote(root), s9LiveShellQuote(event), i, j)
			}
		}
	}
	writeRepairLeadJSON(t, filepath.Join(root, ".claude/settings.json"), settings)
	repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	packets := nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State)
	if len(packets) != 1 {
		t.Fatal("no exact native S9 brief")
	}
	p := packets[0]
	brief, _, _ := nativeS9Paths(p)
	prompt := fmt.Sprintf(`This is a real native S9 Builder slice test in disposable repository %s. Approved S8 Case/Contract and current RepairPlan are synthetic test preconditions; no professional Builder answer or result was filled by the harness.
Your first tool call must use Claude Code native Agent, subagent_type=backend-builder, isolation=worktree. Delegate only the first repair Assignment with EXACT brief %s in the child's prompt. Tell it to read that brief and the professional question, then personally fix Add(a,b int) in internal/api/one.go to return a+b, preserving committed tests. It must personally save PlanReport with actual precheck result before editing, implement, run go test ./internal/api, commit only its product change, and save the professional Result in its actual native worktree. Return exact brief and absolute Result. The original question contains synthetic unit/assertion labels: use those exact labels and attach actual observations/evidence. Do not fabricate PASS or readback/activation. Do not launch another Claude process or use Team/workspace prepare/launch.
After the first actual child return, use SendMessage with to=its ACTUAL returned agent ID and message carrying the exact original brief/Result references. Ask it to verify its existing committed product and Result without editing either and return the exact brief/Result again. Do not create a replacement. Wait for the actual completed task notification and original SubagentStop; immediate SendMessage success only confirms sending. Then Main's first tool must be Read of %s to inspect the original receipt/current delivery. Read the actual Main PreToolUse additionalContext returned for that call. On the next model turn, follow its exact original consumer command through Bash, adding --check-command 'go test ./internal/api' for the actual necessary Main check. Preserve the exact owner/assignment. Do not choose an integration command before receiving this context, use a second actor, or claim notification alone proves acceptance.
Stop after Main integration. Leave the second repair Assignment pending; do not fill it, route a Case, run Targeted, or start fresh S7. Do not edit Runtime or manufacture receipts. If a real blocker occurs, report it honestly.`, root, filepath.Join(root, brief), filepath.Join(root, ".claude/loop-state.json"))
	_ = os.WriteFile(filepath.Join(artifacts, "prompt.txt"), []byte(prompt), 0600)
	t.Cleanup(func() {
		for _, rel := range []string{"loop-state.json", "loop-events.jsonl", "hook-decisions.jsonl"} {
			if data, err := os.ReadFile(filepath.Join(root, ".claude", rel)); err == nil {
				_ = os.WriteFile(filepath.Join(artifacts, rel), data, 0600)
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 24*time.Minute)
	defer cancel()
	script := `source "$1" >/dev/null 2>&1; shift; exec claude -p --permission-mode bypassPermissions --output-format stream-json --verbose --include-hook-events --setting-sources user,project --strict-mcp-config --mcp-config '{"mcpServers":{}}' --no-chrome`
	cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script, "claude-native-s9", envFile)
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
	if err := cmd.Run(); err != nil {
		t.Fatalf("real Claude S9 failed: %v; inspect %s", err, artifacts)
	}
	stream, err := os.ReadFile(filepath.Join(artifacts, "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	consumerCommand := fmt.Sprintf("%s runtime workspace integrate --root %s --assignment %s --agent %s", s7CaptureShellQuote(binary), s7CaptureShellQuote(root), s7CaptureShellQuote(p.Binding.ManifestAssignmentID), s7CaptureShellQuote(p.Binding.OwnerAgentID))
	verifyS9LiveNativeEvents(t, artifacts, stream, nativeLiveHandoffExpectation{
		Owner: p.Binding.OwnerAgentID, Assignment: p.Binding.ManifestAssignmentID,
		InputRef: filepath.Join(root, brief), Accepted: "The PlanReport receipt is accepted.",
		Command: consumerCommand, ActionCommand: consumerCommand + " --check-command 'go test ./internal/api'",
	})
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("no observed native source: %v", err)
	}
	source := registry.Executions[p.Binding.ManifestAssignmentID]
	if source.Purpose != workspace.NativeDeliveryPurpose || source.Status != "complete" || source.BootstrapSHA256 != "" || source.PlatformSessionID != "" || source.DeliveryRef == "" {
		t.Fatalf("actual native delivery/integration incomplete: %+v; inspect %s", source, artifacts)
	}
	if _, err := os.Stat(source.Path); !os.IsNotExist(err) {
		t.Fatal("original Main integration did not clean up")
	}
	if len(repairArtifactRefs(repairPointerCLI(snapshot.State), "result_refs", "", "")) != 1 {
		t.Fatal("not exactly one original repair Result")
	}
	t.Logf("LIVE S9 native execution + resume + original Main integration PASS; artifacts %s", artifacts)
}

const s9LiveHookObserver = `import sys,os,json,time,subprocess,uuid
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
