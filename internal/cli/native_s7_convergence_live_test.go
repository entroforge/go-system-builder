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

	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// This spends real model requests and retains private evidence in a temporary
// directory. It starts at synthetic registered S7 responsibilities, not S0.
//
// GSB_CLAUDE_S7_LIVE=1 GSB_CLAUDE_ENV_FILE="$HOME/.claude/minimax" GSB_CLAUDE_BIN=/path/to/claude go test ./internal/cli -run '^TestClaudeCodeNativeS7ConvergenceLive$' -count=1 -v -timeout 20m
func TestClaudeCodeNativeS7ConvergenceLive(t *testing.T) {
	if os.Getenv("GSB_CLAUDE_S7_LIVE") != "1" {
		t.Skip("opt-in real Claude Code S7 convergence test")
	}
	envFile := os.Getenv("GSB_CLAUDE_ENV_FILE")
	if envFile == "" {
		t.Fatal("GSB_CLAUDE_ENV_FILE must name the authorized provider configuration")
	}
	claudeBin := os.Getenv("GSB_CLAUDE_BIN")
	if claudeBin == "" {
		claudeBin = "claude"
	}
	artifacts, err := os.MkdirTemp("", "gsb-claude-native-s7-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("S7 live artifacts retained at %s", artifacts)
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	// Build and install the current source through the real release/install
	// boundary. This preserves installed roles, Skills, templates and Hook
	// registrations; the synthetic work starts only after installation.
	archive := filepath.Join(artifacts, "release.tar.gz")
	release := exec.CommandContext(t.Context(), "bash", "packaging/build-release.sh", "s7-live", archive)
	release.Dir = repo
	s7LiveRunSetup(t, release, filepath.Join(artifacts, "build-release.log"))
	staged := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(staged, 0700); err != nil {
		t.Fatal(err)
	}
	extract := exec.CommandContext(t.Context(), "tar", "-xzf", archive, "-C", staged)
	s7LiveRunSetup(t, extract, filepath.Join(artifacts, "extract.log"))
	source := filepath.Join(staged, "vibe-coding-loop-template-s7-live")
	root := filepath.Join(t.TempDir(), "installed-project")
	install := exec.CommandContext(t.Context(), filepath.Join(source, ".claude/bin/loop-harness"), "install", "--source", source, "--root", root)
	s7LiveRunSetup(t, install, filepath.Join(artifacts, "install.log"))

	var packets []workpacket.ReviewResultPacket
	t.Cleanup(func() {
		for _, rel := range []string{".claude/loop-state.json", ".claude/loop-events.jsonl", ".claude/hook-decisions.jsonl", "review-plan.json"} {
			s7LiveArchiveFile(root, artifacts, rel)
		}
		for _, p := range packets {
			s7LiveArchiveFile(root, artifacts, p.ViewPath)
			s7LiveArchiveFile(root, artifacts, p.AnswerPath)
		}
		_ = filepath.WalkDir(filepath.Join(root, "s10"), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr == nil && !entry.IsDir() {
				if rel, err := filepath.Rel(root, path); err == nil {
					s7LiveArchiveFile(root, artifacts, rel)
				}
			}
			return nil
		})
		s7LiveArchiveTranscripts(artifacts)
	})

	// Only the product and registered upstream Plan/dispatch are test inputs.
	// No Reviewer conclusion, receipt, CleanRound or S10 state is supplied.
	req := "# REQ-039\n> Status: locked\n> Version: v2.0.0\n\nThe example Marker function returns the literal ready. The quality review checks that the implementation is a direct constant return with no hidden branch or suppressed error.\n"
	reqPath := filepath.Join(root, "docs/requirements/REQ-039-loop-control-plane.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte(req), 0644); err != nil {
		t.Fatal(err)
	}
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	planPath := minimalReviewPlan(t, root, "review-plan-native-s7-live")
	product := []byte("package example\n\nfunc Marker() string { return \"ready\" }\n")
	if err := os.WriteFile(filepath.Join(root, "internal/example/service.go"), product, 0644); err != nil {
		t.Fatal(err)
	}
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatal(err)
	}
	plan["frozen_subjects"].([]any)[0].(map[string]any)["sha256"] = workpacket.Digest(product)
	claims := plan["claims"].([]any)
	delivery := claims[0].(map[string]any)
	delivery["target"], delivery["assertion"], delivery["oracle"], delivery["method"] = "internal/example/service.go", "Marker returns the literal ready", "the implementation returns the value stated in REQ-039", "inspect the frozen function and requirement"
	delivery["source_refs"] = []string{"REQ-039"}
	quality := claims[1].(map[string]any)
	quality["target"], quality["assertion"], quality["oracle"], quality["method"] = "internal/example/service.go", "Marker returns the constant directly without a branch or suppressed error", "the implementation contains only the direct constant return", "inspect the frozen function body"
	quality["source_refs"] = []string{"REQ-039"}
	stageJSON(t, root, "review-plan.json", plan)
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", planPath)
	for _, assignment := range []string{"assignment-dv-1", "assignment-qa-1"} {
		markAssignmentDispatched(t, root, assignment, "live-"+assignment)
	}
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("original S7 issuance did not provide two packets: %v", err)
	}
	byAssignment := map[string]workpacket.ReviewResultPacket{}
	for _, packet := range packets {
		byAssignment[packet.Binding.AssignmentID] = packet
		var answer map[string]any
		if err := json.Unmarshal(packet.Draft.Answer, &answer); err != nil || answer["verdict"] != nil {
			t.Fatalf("issued Reviewer draft is not an unanswered original: %v", err)
		}
	}
	dv, okDV := byAssignment["assignment-dv-1"]
	qa, okQA := byAssignment["assignment-qa-1"]
	if !okDV || !okQA {
		t.Fatal("issued responsibilities do not match the fixture")
	}
	before := stageSnapshot(t, root)
	if before.State["review"].(map[string]any)["clean_round"] != nil {
		t.Fatal("fixture supplied a CleanRound instead of the original consumer")
	}

	// Forward the exact installed Hook commands and keep their complete real
	// payload/output/timing. Matchers, event registrations and timeouts remain
	// the installed ones; the observer never sends a synthetic event.
	settingsPath := filepath.Join(root, ".claude/settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "original-settings.json"), settingsData, 0600); err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatal(err)
	}
	recorder := filepath.Join(artifacts, "observe-hook.py")
	if err := os.WriteFile(recorder, []byte(s7ConvergenceHookObserver), 0700); err != nil {
		t.Fatal(err)
	}
	for event, raw := range settings["hooks"].(map[string]any) {
		for i, registration := range raw.([]any) {
			for j, rawHook := range registration.(map[string]any)["hooks"].([]any) {
				rawHook.(map[string]any)["command"] = fmt.Sprintf("python3 %s %s %s %s %d %d", liveShellQuote(recorder), liveShellQuote(artifacts), liveShellQuote(root), liveShellQuote(event), i, j)
			}
		}
	}
	stageJSON(t, root, ".claude/settings.json", settings)

	prompt := fmt.Sprintf(`You are Main in the disposable project %s. The registered ReviewPlan, assignments and original questionnaires are synthetic test preconditions. No Reviewer conclusion or receipt has been supplied.

Use the native Agent tool to delegate the delivery responsibility to subagent_type delivery-verifier, carrying this exact original view %s and answer %s. After it returns, delegate the QA responsibility to subagent_type qa, carrying this exact original view %s and answer %s. Both use the shared project checkout. Each Reviewer must read its own issued materials, independently inspect the actual frozen source and requirement, fill only its own original professional answer, save it, and report its actual work. The project requirement is that Marker returns the literal ready; the quality responsibility concerns its direct constant return. The issued questions determine the answer's shape and the Reviewer determines its own conclusions.

Do not fill either Reviewer's answer, edit the product or Runtime, create a second report, use a submit/done marker, or make unrelated tool calls merely to trigger progress. Do not use worktree isolation or start another Claude process. After the two actual returns, report the next responsibility and exact path only if you actually received them; otherwise say what was unavailable. Stop without completing that next responsibility.`, root, filepath.Join(root, dv.ViewPath), filepath.Join(root, dv.AnswerPath), filepath.Join(root, qa.ViewPath), filepath.Join(root, qa.AnswerPath))
	if err := os.WriteFile(filepath.Join(artifacts, "prompt.txt"), []byte(prompt), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	// Do not inspect or print the provider configuration. Its output is
	// suppressed, and failure to source it cannot launch a different provider.
	script := `umask 077; gsb_s7_env_file="$1"; gsb_s7_executable="$2"; gsb_s7_version_path="$3"; source "$gsb_s7_env_file" >/dev/null 2>&1 || exit 71; "$gsb_s7_executable" --version > "$gsb_s7_version_path" 2>&1 || exit 72; exec "$gsb_s7_executable" -p --permission-mode bypassPermissions --output-format stream-json --verbose --include-hook-events --setting-sources user,project --strict-mcp-config --mcp-config '{"mcpServers":{}}' --no-chrome`
	cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script, "claude-native-s7", envFile, claudeBin, filepath.Join(artifacts, "claude-version.txt"))
	cmd.Dir, cmd.Stdin = root, strings.NewReader(prompt)
	stdout, err := os.OpenFile(filepath.Join(artifacts, "claude.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(filepath.Join(artifacts, "claude.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		stdout.Close()
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	runErr := cmd.Run()
	stdout.Close()
	stderr.Close()
	if err := os.WriteFile(filepath.Join(artifacts, "duration.txt"), []byte(time.Since(started).String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("real Claude session failed after %s: %v; inspect %s", time.Since(started), runErr, artifacts)
	}
	stream, err := os.ReadFile(filepath.Join(artifacts, "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("original S7 exit did not reach acceptance; inspect %s", artifacts)
	}
	assignments := after.State["review"].(map[string]any)["assignments"].(map[string]any)
	for _, p := range packets {
		if assignments[p.Binding.AssignmentID].(map[string]any)["result_receipt"] == nil {
			t.Fatalf("original Reviewer receipt missing for %s; inspect %s", p.ID, artifacts)
		}
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !s7LiveHasTransition(journal, "TR-009") {
		t.Fatalf("acceptance lacks original TR-009 journal event; inspect %s", artifacts)
	}
	manifest := filepath.Join(root, "s10", after.State["runtime_id"].(string), "g1", "r1", "acceptance-manifest.json")
	if info, err := os.Stat(manifest); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("original S10 material was not prepared; inspect %s", artifacts)
	}
	if err := s7LiveCheckEvents(artifacts, root, packets, manifest); err != nil {
		t.Fatalf("real native handoff evidence: %v; inspect %s", err, artifacts)
	}
	if !s7LiveMainTextContains(stream, manifest) {
		t.Fatalf("Main did not report the actual handed-off S10 path; inspect %s", artifacts)
	}
	t.Logf("S7 native reception, Main handoff, TR-009 and S10 material observed; artifacts %s", artifacts)
}

func s7LiveRunSetup(t *testing.T, cmd *exec.Cmd, logPath string) {
	t.Helper()
	output, err := cmd.CombinedOutput()
	if writeErr := os.WriteFile(logPath, output, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil {
		t.Fatalf("S7 live setup failed: %v; inspect %s", err, logPath)
	}
}

func s7LiveArchiveFile(root, artifacts, rel string) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	target := filepath.Join(artifacts, "project", rel)
	if os.MkdirAll(filepath.Dir(target), 0700) == nil {
		_ = os.WriteFile(target, data, 0600)
	}
}

type s7LiveHookObservation struct {
	Event    string         `json:"event"`
	Started  int64          `json:"started_ns"`
	Finished int64          `json:"finished_ns"`
	Exit     int            `json:"exit_code"`
	Stdout   string         `json:"stdout"`
	Payload  map[string]any `json:"payload"`
}

func s7LiveHookObservations(artifacts string) ([]s7LiveHookObservation, error) {
	files, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		return nil, err
	}
	var observations []s7LiveHookObservation
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var row s7LiveHookObservation
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, err
		}
		observations = append(observations, row)
	}
	return observations, nil
}

func s7LiveCheckEvents(artifacts, root string, packets []workpacket.ReviewResultPacket, manifest string) error {
	observations, err := s7LiveHookObservations(artifacts)
	if err != nil {
		return err
	}
	actors := map[string]string{}
	starts, stops := map[string][]s7LiveHookObservation{}, map[string][]s7LiveHookObservation{}
	mainReceipt := map[string]bool{}
	mainNext := false
	for _, row := range observations {
		actor, _ := row.Payload["agent_id"].(string)
		if actor != "" && actor != "Main" {
			switch row.Event {
			case "SubagentStart":
				if row.Exit == 0 && row.Started > 0 && row.Finished >= row.Started {
					starts[actor] = append(starts[actor], row)
				}
			case "SubagentStop":
				if row.Exit == 0 && row.Started > 0 && row.Finished >= row.Started {
					stops[actor] = append(stops[actor], row)
				}
			case "PreToolUse", "PostToolUse":
				if row.Exit != 0 || !strings.Contains(row.Stdout, "accepted=true") {
					continue
				}
				if row.Event == "PostToolUse" {
					response, _ := row.Payload["tool_response"].(map[string]any)
					if row.Payload["error"] != nil && row.Payload["error"] != "" || response["is_error"] == true {
						return fmt.Errorf("failed tool was treated as reception")
					}
				}
				if row.Payload["cwd"] != root {
					return fmt.Errorf("Reviewer reception did not use the shared Main checkout")
				}
				for _, p := range packets {
					if strings.Contains(row.Stdout, p.ID) {
						if old := actors[p.ID]; old != "" && old != actor {
							return fmt.Errorf("multiple actors claimed %s", p.ID)
						}
						actors[p.ID] = actor
					}
				}
			}
			continue
		}
	}
	roles := map[string]string{"assignment-dv-1": "delivery-verifier", "assignment-qa-1": "qa"}
	for _, row := range observations {
		actor, _ := row.Payload["agent_id"].(string)
		if actor != "" && actor != "Main" {
			continue
		}
		if row.Event != "PostToolUse" || row.Exit != 0 || (row.Payload["tool_name"] != "Agent" && row.Payload["tool_name"] != "Task") {
			continue
		}
		input, _ := row.Payload["tool_input"].(map[string]any)
		if input["isolation"] == "worktree" {
			return fmt.Errorf("Main dispatched an isolated Reviewer")
		}
		task, _ := input["prompt"].(string)
		response, _ := row.Payload["tool_response"].(map[string]any)
		if input["run_in_background"] == true || response["status"] != "completed" || response["is_error"] == true || response["interrupted"] == true || row.Started <= 0 {
			continue
		}
		returnedActor, _ := response["agentId"].(string)
		if returnedActor == "" {
			returnedActor, _ = response["agent_id"].(string)
		}
		completed := false
		for _, start := range starts[returnedActor] {
			for _, stop := range stops[returnedActor] {
				if start.Finished <= stop.Started && stop.Finished <= row.Started {
					completed = true
				}
			}
		}
		if !completed {
			continue
		}
		for _, p := range packets {
			if input["subagent_type"] == roles[p.Binding.AssignmentID] && strings.Contains(task, filepath.Join(root, p.ViewPath)) && strings.Contains(task, filepath.Join(root, p.AnswerPath)) && strings.Contains(row.Stdout, p.ID) && strings.Contains(row.Stdout, `\"accepted\":true`) && returnedActor == actors[p.ID] {
				mainReceipt[p.ID] = true
				if strings.Contains(row.Stdout, "TR-009") && strings.Contains(row.Stdout, manifest) {
					mainNext = true
				}
			}
		}
	}
	if len(actors) != len(packets) || actors[packets[0].ID] == actors[packets[1].ID] {
		return fmt.Errorf("both original results were not received by distinct real child tool events")
	}
	for _, p := range packets {
		if !mainReceipt[p.ID] {
			return fmt.Errorf("missing synchronous completed Main return after real Start/Stop and original receipt for %s", p.ID)
		}
	}
	if !mainNext {
		return fmt.Errorf("Main Agent return did not deliver TR-009 and the exact S10 material path")
	}
	return nil
}

func s7LiveHasTransition(journal []byte, transition string) bool {
	for _, line := range bytes.Split(journal, []byte("\n")) {
		var event map[string]any
		if json.Unmarshal(line, &event) == nil && event["transition_id"] == transition {
			return true
		}
	}
	return false
}

func s7LiveMainTextContains(stream []byte, want string) bool {
	for _, line := range bytes.Split(stream, []byte("\n")) {
		var frame map[string]any
		if json.Unmarshal(line, &frame) != nil || frame["parent_tool_use_id"] != nil {
			continue
		}
		if frame["type"] == "result" {
			if value, _ := frame["result"].(string); strings.Contains(value, want) {
				return true
			}
		}
		if frame["type"] != "assistant" {
			continue
		}
		message, _ := frame["message"].(map[string]any)
		content, _ := message["content"].([]any)
		for _, raw := range content {
			part, _ := raw.(map[string]any)
			if value, _ := part["text"].(string); part["type"] == "text" && strings.Contains(value, want) {
				return true
			}
		}
	}
	return false
}

func s7LiveArchiveTranscripts(artifacts string) {
	observations, err := s7LiveHookObservations(artifacts)
	if err != nil {
		return
	}
	paths := map[string]bool{}
	for _, row := range observations {
		for _, key := range []string{"transcript_path", "agent_transcript_path"} {
			if path, _ := row.Payload[key].(string); filepath.IsAbs(path) {
				paths[path] = true
			}
		}
		parent, _ := row.Payload["transcript_path"].(string)
		actor, _ := row.Payload["agent_id"].(string)
		if filepath.IsAbs(parent) && strings.HasSuffix(parent, ".jsonl") && actor != "" && actor != "Main" && !strings.ContainsAny(actor, "/\\") {
			paths[filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")] = true
		}
	}
	dir := filepath.Join(artifacts, "transcripts")
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	index := map[string]string{}
	for path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := fmt.Sprintf("transcript-%03d.jsonl", len(index)+1)
		if os.WriteFile(filepath.Join(dir, name), data, 0600) == nil {
			index[name] = path
		}
	}
	data, _ := json.MarshalIndent(index, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "index.json"), append(data, '\n'), 0600)
}

const s7ConvergenceHookObserver = `import sys,os,json,time,subprocess,uuid
from pathlib import Path
os.umask(0o077)
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
row={'event':event,'started_ns':start,'finished_ns':time.time_ns(),'project_dir':os.environ.get('CLAUDE_PROJECT_DIR'),'process_cwd':os.getcwd(),'revision_before':before,'revision_after':revision(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'raw_payload':raw.decode(errors='replace'),'payload':payload}
(Path(artifacts)/('hook-'+str(start)+'-'+uuid.uuid4().hex+'.json')).write_text(json.dumps(row,ensure_ascii=False))
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr)
sys.exit(p.returncode)
`
