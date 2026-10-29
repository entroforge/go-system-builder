package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/doclinks"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

//	GSB_S9_CONVERGENCE_LIVE=1 GSB_CLAUDE_BIN=/absolute/path/to/claude \
//	  go test ./internal/cli -run '^TestClaudeNativeS9ConvergenceLive$' -count=1 -v -timeout 20m
//
// The Case, approval and professional conclusions are synthetic test inputs.
// Main and both native actors run on the actual platform with the production
// Hook and original consumers. The driver never replays an event or authors an
// S9 answer. This is not a product-quality or real-user acceptance test.
func TestClaudeNativeS9ConvergenceLive(t *testing.T) {
	preflightOnly := os.Getenv("GSB_S9_CONVERGENCE_PREFLIGHT_ONLY") == "1"
	if os.Getenv("GSB_S9_CONVERGENCE_LIVE") != "1" && !preflightOnly {
		t.Skip("opt-in real Claude S9 convergence test")
	}
	if goruntime.GOOS != "linux" && goruntime.GOOS != "darwin" {
		t.Skip("the installed shell launcher requires Linux or macOS")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(home, ".claude", "minimax")
	if info, err := os.Stat(envFile); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("authorized Claude environment file unavailable: %v", err)
	}
	claude := os.Getenv("GSB_CLAUDE_BIN")
	if claude == "" {
		claude = "claude"
	}
	artifacts := os.Getenv("GSB_S9_CONVERGENCE_ARTIFACTS")
	if artifacts == "" {
		artifacts, err = os.MkdirTemp("", "gsb-s9-convergence-live-")
	} else {
		artifacts, err = filepath.Abs(artifacts)
		if err == nil {
			err = os.MkdirAll(artifacts, 0700)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Real S9 observations retained at %s", artifacts)

	before := s9ConvergenceSourceSnapshot(t, repo)
	binary := filepath.Join(artifacts, "production-loop-harness")
	buildContext, buildCancel := context.WithTimeout(t.Context(), 3*time.Minute)
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, "./cmd/loop-harness")
	build.Dir = repo
	build.Env = append(s9ConvergenceWithoutEnv(os.Environ(), "GOFLAGS"), "GOFLAGS=")
	buildOutput, buildErr := build.CombinedOutput()
	buildCancel()
	if err := os.WriteFile(filepath.Join(artifacts, "build.log"), buildOutput, 0600); err != nil {
		t.Fatal(err)
	}
	if buildErr != nil {
		t.Fatalf("production build failed: %v; see %s", buildErr, artifacts)
	}
	if after := s9ConvergenceSourceSnapshot(t, repo); !bytes.Equal(before, after) {
		t.Fatal("source changed during build; do not run a mixed-source platform experiment")
	}
	if err := os.WriteFile(filepath.Join(artifacts, "source-snapshot.json"), before, 0600); err != nil {
		t.Fatal(err)
	}
	versionContext, versionCancel := context.WithTimeout(t.Context(), 15*time.Second)
	versionCommand := s9ConvergenceClaudeCommand(versionContext, envFile, claude, "--version")
	version, versionErr := versionCommand.Output()
	versionCancel()
	if versionErr != nil {
		t.Fatalf("Claude version unavailable: %v", versionErr)
	}

	root := repairLeadFreshRoot(t)
	// Root is frozen by the upstream Case/session. Archive the actual fixture at
	// the end; the archive is evidence, never a replacement runtime or worktree.
	t.Cleanup(func() {
		if err := copyS9PlatformFixture(root, filepath.Join(artifacts, "archive-main")); err != nil {
			t.Errorf("archive the actual fixture: %v", err)
		}
	})
	s9ConvergenceInstall(t, root, repo, artifacts, binary)
	if after := s9ConvergenceSourceSnapshot(t, repo); !bytes.Equal(before, after) {
		t.Fatal("source changed during asset preparation; do not run mixed-source platform evidence")
	}
	writeRepairLeadFile(t, root, "go.mod", "module synthetic.s9.convergence\n\ngo 1.26\n")
	writeRepairLeadFile(t, root, "internal/api/one.go", "package api\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepairLeadFile(t, root, "internal/api/one_test.go", "package api\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 || Add(-2, 1) != -1 { t.Fatal(\"fixture sum mismatch\") } }\n")
	// The installed framework tools are present as shipped. Their Go programs
	// belong to the harness, not to this synthetic product's Go module.
	writeRepairLeadFile(t, root, "tools/go.mod", "module synthetic.s9.convergence.installed.tools\n\ngo 1.26\n")
	s9ConvergenceCheckFixture(t, root, artifacts)
	if preflightOnly {
		t.Logf("Synthetic product fixture checks passed without starting Claude; artifacts %s", artifacts)
		return
	}
	// Put actual installed roles/skills/settings into the committed base so the
	// platform's isolated worktree receives the same assets, not placeholders.
	repairLeadCommitFixture(t, root)
	repairLeadGit(t, root, "add", "-f", ".claude/agents", ".claude/skills", ".claude/settings.json", ".claude/loop.md")
	repairLeadApprovedCase(t, root, "investigation-case-convergence-live", []map[string]any{{
		"id": "unit-one", "description": "Check the synthetic already-stable source authority", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"},
	}})
	snapshot := repairDispatchSnapshot(t, root)
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, _, err = repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-convergence-live", CreatedBy: "Main",
	})
	if err != nil {
		t.Fatalf("prepare the approved synthetic Session: %v", err)
	}
	snapshot, plan, _, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, PlanID: "repair-plan-convergence-live", CreatedBy: "Main",
	})
	if err != nil || len(plan.Assignments) != 1 {
		t.Fatalf("prepare one original repair assignment: %+v %v", plan, err)
	}
	if _, err := prepareRepairEntry(root, statePath, journalPath, snapshot.State); err != nil {
		t.Fatalf("prepare original Main division: %v", err)
	}
	head := repairLeadGit(t, root, "rev-parse", "HEAD")

	prompt := `This is a disposable synthetic S9 exercise. The pre-existing S8 finding, approval, Case and Contract are synthetic historical inputs, not a real user or product assessment. Do not claim to have observed a historical failure. The single synthetic API already contains the intended behavior: carry out the assigned professional observations and actual checks without product edits or a product commit. The product module's go test ./... and go build ./... are available checks.

Work from the current installed project instructions, Skills, original materials and actual Hook context. For this synthetic division choose backend-builder for the one Builder Assignment, and one independent verifier covering all current Contract assertions. Main performs its own division and dispatch. Run the native Builder in its own isolated worktree, and ask it to read its installed role, relevant Skills, and the original materials supplied for its responsibility. Run each native Agent synchronously and wait for its actual completed return. Main follows the original integration and necessary checks, then performs its own Impact and independent-verifier division. A different native actor independently verifies the final Main checkout using its own installed role, Skills and original question. Follow the original handoff until the new S7 ReviewPlan seed is prepared, then stop before its professional review.

Report a real blocker rather than inventing completion. This exercise does not ask you to investigate harness configuration, the test driver or observation machinery, or to develop a real product. Do not edit Runtime, journal, receipts, hooks, settings or installed guidance; do not fabricate or replay Hook events or start another Claude process.`
	if err := os.WriteFile(filepath.Join(artifacts, "prompt.txt"), []byte(prompt), 0600); err != nil {
		t.Fatal(err)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, filepath.Join(artifacts, "conditions.json"), map[string]any{
		"main_root": root, "fixture": "one approved synthetic no-change assignment", "synthetic_professional_input": true,
		"claude_binary": claude, "claude_version": strings.TrimSpace(string(version)), "permission_mode": "bypassPermissions",
		"environment": "source ~/.claude/minimax; contents not recorded", "setting_sources": "project",
		"controller_redelivery": false, "production_overlay": false, "production_binary_sha256": fmt.Sprintf("%x", sha256.Sum256(binaryBytes)),
		"cli_timeout_seconds": 900, "test_timeout": "run with -timeout 20m", "initial_head": head,
		"fixture_checks": []string{"go test ./...", "go build ./..."}, "installed_tools": "separate fixture Go module; shipped files retained",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	command := s9ConvergenceClaudeCommand(ctx, envFile, claude, "-p", "--permission-mode", "bypassPermissions", "--output-format", "stream-json", "--verbose", "--include-hook-events", "--setting-sources", "project")
	command.Dir, command.Stdin = root, strings.NewReader(prompt)
	stdout, err := os.Create(filepath.Join(artifacts, "stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(filepath.Join(artifacts, "stderr.log"))
	if err != nil {
		stdout.Close()
		t.Fatal(err)
	}
	command.Stdout, command.Stderr = stdout, stderr
	started := time.Now()
	processErr := command.Run()
	stdout.Close()
	stderr.Close()
	writeRepairLeadJSON(t, filepath.Join(artifacts, "process.json"), map[string]any{
		"started_at": started.UTC().Format(time.RFC3339Nano), "elapsed_ms": time.Since(started).Milliseconds(),
		"error": fmt.Sprint(processErr), "context_error": fmt.Sprint(ctx.Err()), "cli_binary": claude,
	})
	final, finalErr := loopruntime.NewStore(statePath, journalPath).Snapshot()
	writeRepairLeadJSON(t, filepath.Join(artifacts, "terminal.json"), map[string]any{
		"runtime": final.State, "snapshot_error": fmt.Sprint(finalErr), "process_error": fmt.Sprint(processErr),
	})
	if processErr != nil || finalErr != nil {
		t.Fatalf("real platform did not complete: process=%v snapshot=%v; inspect %s", processErr, finalErr, artifacts)
	}
	s9ConvergenceVerify(t, root, artifacts, final, head)
	t.Logf("Real native S9 reached its original Handoff and fresh S7 seed; synthetic domain inputs only; artifacts %s", artifacts)
}

// Verify the exact product-wide commands before asking the model to use them.
// The logs belong to the disposable fixture and do not stand in for Main's
// later independently selected and recorded checks.
func s9ConvergenceCheckFixture(t *testing.T, root, artifacts string) {
	t.Helper()
	var log bytes.Buffer
	for _, args := range [][]string{{"test", "./..."}, {"build", "./..."}} {
		fmt.Fprintf(&log, "$ go %s\n", strings.Join(args, " "))
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		command := exec.CommandContext(ctx, "go", args...)
		command.Dir = root
		command.Env = append(s9ConvergenceWithoutEnv(s9ConvergenceWithoutEnv(os.Environ(), "GOFLAGS"), "GOWORK"), "GOFLAGS=", "GOWORK=off")
		output, err := command.CombinedOutput()
		cancel()
		log.Write(output)
		fmt.Fprintf(&log, "exit: %v\n", err)
		if writeErr := os.WriteFile(filepath.Join(artifacts, "fixture-check.log"), log.Bytes(), 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil {
			t.Fatalf("synthetic product check go %s failed: %v; see %s", strings.Join(args, " "), err, artifacts)
		}
	}
}

func s9ConvergenceWithoutEnv(env []string, name string) []string {
	result := make([]string, 0, len(env))
	for _, value := range env {
		if !strings.HasPrefix(value, name+"=") {
			result = append(result, value)
		}
	}
	return result
}

func s9ConvergenceClaudeCommand(ctx context.Context, envFile, claude string, args ...string) *exec.Cmd {
	// The environment file is sourced without echoing its contents, then the
	// requested executable is invoked as an argv element, not shell text.
	script := `source "$1" >/dev/null 2>&1 || exit $?; shift; exec "$@"`
	argv := append([]string{"--noprofile", "--norc", "-c", script, "s9-convergence", envFile, claude}, args...)
	return exec.CommandContext(ctx, "bash", argv...)
}

func s9ConvergenceSourceSnapshot(t *testing.T, repo string) []byte {
	t.Helper()
	git := func(args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = repo
		data, err := command.Output()
		if err != nil {
			t.Fatalf("source identity git %v: %v", args, err)
		}
		return data
	}
	head := strings.TrimSpace(string(git("rev-parse", "HEAD")))
	diff := git("diff", "--binary", "HEAD")
	untracked := git("ls-files", "--others", "--exclude-standard", "-z")
	paths := strings.Split(strings.TrimSuffix(string(untracked), "\x00"), "\x00")
	sort.Strings(paths)
	files := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		full := filepath.Join(repo, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatalf("untracked source %s: %v", path, err)
		}
		var data []byte
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(full)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(link)
		} else if info.Mode().IsRegular() {
			data, err = os.ReadFile(full)
			if err != nil {
				t.Fatalf("untracked source %s: %v", path, err)
			}
		} else {
			t.Fatalf("untracked source is not a regular file or link: %s", path)
		}
		files = append(files, map[string]any{"path": path, "mode": info.Mode().String(), "sha256": fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	value, err := json.MarshalIndent(map[string]any{
		"head": head, "tracked_diff_sha256": fmt.Sprintf("%x", sha256.Sum256(diff)), "untracked": files,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(value, '\n')
}

func s9ConvergenceInstall(t *testing.T, root, repo, artifacts, binary string) {
	t.Helper()
	for _, pair := range [][2]string{{"docs", "docs"}, {"agents", ".claude/agents"}, {"skills", ".claude/skills"}, {"tools", "tools"}} {
		if err := copyS9PlatformFixture(filepath.Join(repo, pair[0]), filepath.Join(root, pair[1])); err != nil {
			t.Fatalf("install current %s: %v", pair[0], err)
		}
	}
	for _, pair := range [][2]string{{"AGENTS-template.md", "AGENTS.md"}, {"loop-template.md", ".claude/loop.md"}, {"docs/project-map-template.md", "docs/project-map.md"}, {"packaging/README.installed.md", "docs/README.md"}, {"packaging/DOCUMENT-MAP.installed.md", "docs/DOCUMENT-MAP.md"}, {"packaging/project.gitattributes", ".gitattributes"}, {"tools/loop-harness-launcher.sh", ".claude/bin/loop-harness"}} {
		data, err := os.ReadFile(filepath.Join(repo, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		if pair[0] == "AGENTS-template.md" {
			data = bytes.ReplaceAll(data, []byte("{project name}"), []byte("Synthetic S9 Convergence"))
			data = bytes.ReplaceAll(data, []byte("{One sentence describing the project goal. Use `{unknown}` when needed.}"), []byte("A disposable synthetic fixture for S9 workflow observation."))
		}
		path := filepath.Join(root, pair[1])
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0600)
		if pair[0] == "tools/loop-harness-launcher.sh" {
			mode = 0700
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	// Match the production installer's source-to-installed Markdown mapping;
	// merely copying a Skill into .claude would break its cross-root links.
	entries := [][2]string{{"docs", "docs"}, {"skills", ".claude/skills"}, {"agents", ".claude/agents"}, {".claude/bin", ".claude/bin"}, {"AGENTS-template.md", "AGENTS.md"}, {"loop-template.md", ".claude/loop.md"}, {"settings.json", ".claude/settings.json"}, {"loop-harness.md", ".claude/bin/loop-harness.md"}, {"tools", "tools"}, {"project.gitattributes", ".gitattributes"}}
	toInstalled := func(rel string) string {
		for _, entry := range entries {
			if rel == entry[0] {
				return entry[1]
			}
			if strings.HasPrefix(rel, entry[0]+"/") {
				return entry[1] + strings.TrimPrefix(rel, entry[0])
			}
		}
		return rel
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		old := rel
		for _, mapping := range entries {
			if rel == mapping[1] {
				old = mapping[0]
				break
			}
			if strings.HasPrefix(rel, mapping[1]+"/") {
				old = mapping[0] + strings.TrimPrefix(rel, mapping[1])
				break
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, doclinks.Relocate(data, old, rel, toInstalled), info.Mode().Perm())
	}); err != nil {
		t.Fatalf("relocate installed documentation links: %v", err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, ".claude", "bin", "loop-harness-"+goruntime.GOOS+"-"+goruntime.GOARCH)
	if err := os.WriteFile(installed, data, 0700); err != nil {
		t.Fatal(err)
	}
	settingsBytes, err := os.ReadFile(filepath.Join(repo, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "original-settings.json"), settingsBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatal(err)
	}
	observer := filepath.Join(artifacts, "observe-hook.py")
	if err := os.WriteFile(observer, []byte(s9ConvergenceHookObserver), 0700); err != nil {
		t.Fatal(err)
	}
	for event, raw := range settings["hooks"].(map[string]any) {
		for index, registration := range raw.([]any) {
			for hookIndex, hook := range registration.(map[string]any)["hooks"].([]any) {
				hook.(map[string]any)["command"] = fmt.Sprintf("python3 %s %s %s %s %d %d", s9LiveShellQuote(observer), s9LiveShellQuote(artifacts), s9LiveShellQuote(root), s9LiveShellQuote(event), index, hookIndex)
			}
		}
	}
	writeRepairLeadJSON(t, filepath.Join(root, ".claude/settings.json"), settings)
}

// This wrapper executes the unchanged installed command, forwards its exact
// streams and exit code, and records only the observed platform request and
// read-only before/after Runtime facts. It never invents or redelivers events.
const s9ConvergenceHookObserver = `import sys,os,json,time,subprocess,uuid
from pathlib import Path
artifacts,main,event,i,j=sys.argv[1:]
raw=sys.stdin.buffer.read()
payload=json.loads(raw)
original=json.loads((Path(artifacts)/'original-settings.json').read_text())['hooks'][event][int(i)]['hooks'][int(j)]['command']
def state():
 try:
  s=json.loads((Path(main)/'.claude/loop-state.json').read_text())
  return {'revision':s.get('revision'),'lifecycle':s.get('lifecycle'),'repair':(s.get('review') or {}).get('repair'),'executions':(s.get('workspace') or {}).get('executions',{})}
 except Exception:
  return None
before=state()
start=time.time_ns()
p=subprocess.run(['bash','--noprofile','--norc','-c',original],input=raw,capture_output=True)
finish=time.time_ns()
row={'event':event,'started_ns':start,'finished_ns':finish,'project_dir':os.environ.get('CLAUDE_PROJECT_DIR'),'process_cwd':os.getcwd(),'state_before':before,'state_after':state(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'payload':payload}
(Path(artifacts)/('hook-'+str(start)+'-'+uuid.uuid4().hex+'.json')).write_text(json.dumps(row,ensure_ascii=False))
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
`

type s9ConvergenceHookRow struct {
	Event   string         `json:"event"`
	Started int64          `json:"started_ns"`
	Ended   int64          `json:"finished_ns"`
	Before  map[string]any `json:"state_before"`
	After   map[string]any `json:"state_after"`
	Payload map[string]any `json:"payload"`
	Exit    int            `json:"exit_code"`
	Stdout  string         `json:"stdout"`
}

func s9ConvergenceRef(t *testing.T, root string, ref repair.ArtifactRef) {
	t.Helper()
	if ref.Path == "" || ref.SHA256 == "" {
		t.Fatalf("missing original artifact path/hash: %+v", ref)
	}
	data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
	if err != nil || workpacket.Digest(data) != ref.SHA256 {
		t.Fatalf("original artifact %s is unavailable or changed: %v", ref.Path, err)
	}
}

func s9ConvergenceVerify(t *testing.T, root, artifacts string, snap loopruntime.Snapshot, head string) {
	t.Helper()
	pointer := repairPointerCLI(snap.State)
	review := mapFieldCLI(snap.State, "review")
	entry := mapFieldCLI(review, "round_entry")
	if lifecycleState(snap.State) != "verification" || pointer["status"] != "closed" || integerValue(review["round"]) != 2 || entry["transition_id"] != "TR-012" {
		t.Fatalf("original S9 did not open the fresh S7: lifecycle=%s repair=%v round=%v entry=%v; see %s", lifecycleState(snap.State), pointer["status"], review["round"], entry, artifacts)
	}
	finalRefs := repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256")
	if len(finalRefs) != 1 {
		t.Fatalf("expected the one original Final: %+v", finalRefs)
	}
	s9ConvergenceRef(t, root, finalRefs[0])
	final, err := repair.ValidateRepairResult(root, finalRefs[0])
	if err != nil || final.Receipt == nil || final.Receipt.CandidateRef == "" || final.Result != "pass" || len(final.ChangedArtifacts) != 0 || len(final.Checks) == 0 {
		t.Fatalf("original no-change Final or Main checks missing: %+v %v", final, err)
	}
	for _, check := range final.Checks {
		if check.Result != "pass" || check.Command == "" {
			t.Fatalf("Main check was not actually recorded as passing: %+v", check)
		}
	}
	candidate := repair.ArtifactRef{Path: final.Receipt.CandidateRef, SHA256: final.Receipt.CandidateSHA256}
	s9ConvergenceRef(t, root, candidate)
	if final.Receipt.Provenance.ExecutionRoot == "" || final.Receipt.Provenance.ExecutionRoot == root {
		t.Fatalf("Final lacks its real native worktree source: %+v", final.Receipt.Provenance)
	}
	impactRef, err := repairPointerRef(pointer, "impact_ref", "impact_sha256", "original Impact")
	if err != nil {
		t.Fatal(err)
	}
	s9ConvergenceRef(t, root, impactRef)
	impact, err := repair.ValidateChangeImpact(root, impactRef)
	if err != nil || len(impact.ChangedArtifacts) != 0 || len(impact.ChangeTypes) != 0 || len(impact.Decisions) == 0 || len(impact.RequiredReverificationIDs) == 0 {
		t.Fatalf("no-change Impact/independent obligation missing: %+v %v", impact, err)
	}
	targets := repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "")
	if len(targets) != 1 {
		t.Fatalf("expected one actual independent Targeted result: %+v", targets)
	}
	s9ConvergenceRef(t, root, targets[0])
	target, err := repair.ValidateTargetedReverification(root, targets[0])
	if err != nil || target.Result != "pass" || target.PerformingAssignmentID == target.OriginalAssignmentID || len(target.AssertionResults) == 0 {
		t.Fatalf("independent Targeted result missing: %+v %v", target, err)
	}
	for _, assertion := range target.AssertionResults {
		if assertion.Result != "pass" || len(assertion.EvidenceRefs) == 0 {
			t.Fatalf("independent assertion lacks a passing fixture observation: %+v", assertion)
		}
	}
	handoffRef, err := repairPointerRef(pointer, "handoff_ref", "handoff_sha256", "original Handoff")
	if err != nil {
		t.Fatal(err)
	}
	s9ConvergenceRef(t, root, handoffRef)
	handoff, err := repair.ValidateRepairHandoff(root, handoffRef)
	if err != nil || len(handoff.TargetedReverificationRefs) != 1 || handoff.TargetedReverificationRefs[0].Path != targets[0].Path || handoff.ChangeImpactRef.Path != impactRef.Path {
		t.Fatalf("Handoff did not retain the actual Impact/Targeted facts: %+v %v", handoff, err)
	}
	s9ConvergenceRef(t, root, handoff.ChangesetRef)
	changes, err := repair.ValidateChangeset(root, handoff.ChangesetRef)
	if err != nil || len(changes.Artifacts) != 0 {
		t.Fatalf("no-change handoff manufactured a product change: %+v %v", changes, err)
	}
	seed := repair.ArtifactRef{Path: stringValue(pointer["review_plan_seed_ref"]), SHA256: stringValue(pointer["review_plan_seed_sha256"])}
	s9ConvergenceRef(t, root, seed)
	if entry["review_plan_seed_ref"] != seed.Path || entry["repair_handoff_ref"] != handoffRef.Path {
		t.Fatalf("TR-012 does not bind the original handoff and seed: %+v", entry)
	}
	var seedObject map[string]any
	readRepairLeadJSON(t, resolveRootPath(root, seed.Path), &seedObject)
	if len(objectSliceAny(seedObject["frozen_subjects"])) == 0 {
		t.Fatal("fresh S7 seed lacks actual frozen verification subjects")
	}
	for _, path := range []string{reviewPlanQuestionnairePath(root, snap.State), reviewPlanGuidePath(reviewPlanQuestionnairePath(root, snap.State))} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fresh S7 preparation unavailable at %s: %v", path, err)
		}
	}
	if got := repairLeadGit(t, root, "rev-parse", "HEAD"); got != head {
		t.Fatalf("no-change fixture unexpectedly changed Main HEAD: %s -> %s", head, got)
	}
	plans, err := loopruntime.CurrentRepairPlanReportPackets(root, snap.State, final.Receipt.Provenance.OwnerAgentID)
	if err != nil || len(plans) != 1 || plans[0].Binding.ManifestAssignmentID != final.Receipt.Provenance.ExecutionAssignmentID {
		t.Fatalf("original issued Builder Plan identity unavailable: %v %+v", err, plans)
	}
	plan := plans[0]
	brief, _, _ := nativeS9Paths(plan)
	targetPath, err := repairTargetedQuestionnairePath(root, snap.State, target.PerformingAssignmentID)
	if err != nil {
		t.Fatalf("original registered Targeted question unavailable: %v", err)
	}
	verifierOwner := stringMapCLI(pointer["assignment_owners"])[target.PerformingAssignmentID]
	builderDefinition := stringValue(contextAgent(snap.State, plan.Binding.OwnerAgentID)["definition_ref"])
	verifierDefinition := stringValue(contextAgent(snap.State, verifierOwner)["definition_ref"])
	if verifierOwner == "" || builderDefinition == "" || verifierDefinition == "" {
		t.Fatal("original registered Builder or verifier role is missing")
	}
	expected := s9ConvergenceExpectedReads{
		brief: filepath.Join(root, brief), planQuestion: resolveRootPath(root, plan.ViewPath), targetQuestion: targetPath,
		builderDefinition: builderDefinition, verifierDefinition: verifierDefinition,
	}
	rows := s9ConvergenceHooks(t, artifacts)
	s9ConvergenceCheckEvents(t, rows, root, candidate.Path, final.Receipt.Provenance.ExecutionAssignmentID, final.Receipt.Provenance.ExecutionRoot, targets[0].Path, expected)
}

func s9ConvergenceHooks(t *testing.T, artifacts string) []s9ConvergenceHookRow {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []s9ConvergenceHookRow
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var row s9ConvergenceHookRow
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatalf("read actual Hook observation %s: %v", path, err)
		}
		if row.Ended < row.Started {
			t.Fatalf("invalid actual Hook times: %s", path)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Started < rows[j].Started })
	return rows
}

func s9ConvergenceObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func s9ConvergenceHasTarget(state map[string]any, path string) bool {
	for _, ref := range repairArtifactRefs(s9ConvergenceObject(state["repair"]), "targeted_reverification_artifacts", "", "") {
		if ref.Path == path {
			return true
		}
	}
	return false
}

type s9ConvergenceExpectedReads struct {
	brief, planQuestion, targetQuestion   string
	builderDefinition, verifierDefinition string
}

func s9ConvergenceReadMatches(row s9ConvergenceHookRow, expected ...string) bool {
	if row.Event != "PostToolUse" || row.Exit != 0 {
		return false
	}
	input := s9ConvergenceObject(row.Payload["tool_input"])
	response := s9ConvergenceObject(row.Payload["tool_response"])
	if response["is_error"] == true || response["interrupted"] == true || response["exit_code"] != nil && response["exit_code"] != float64(0) {
		return false
	}
	cwd := stringValue(row.Payload["cwd"])
	var paths []string
	switch row.Payload["tool_name"] {
	case "Read":
		paths = []string{stringValue(input["file_path"])}
	case "Bash":
		words, err := nativeLiveCommandWords(stringValue(input["command"]))
		if err != nil || len(words) < 2 {
			return false
		}
		switch filepath.Base(words[0]) {
		case "cat", "sed", "head", "tail", "grep", "rg", "awk":
			paths = words[1:]
		default:
			return false
		}
	default:
		return false
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		for _, want := range expected {
			if want != "" && filepath.Clean(path) == filepath.Clean(want) {
				return true
			}
		}
	}
	return false
}

func s9ConvergenceCheckEvents(t *testing.T, rows []s9ConvergenceHookRow, root, candidate, assignment, source, target string, expected s9ConvergenceExpectedReads) {
	t.Helper()
	var builder, verifier string
	var candidateAt, targetAt int64
	for _, row := range rows {
		if row.Exit != 0 || row.Event != "PreToolUse" && row.Event != "PostToolUse" && row.Event != "SubagentStop" {
			continue
		}
		actor := stringValue(row.Payload["agent_id"])
		if actor == "" || actor == "Main" {
			continue
		}
		beforeExecution := s9ConvergenceObject(s9ConvergenceObject(row.Before["executions"])[assignment])
		afterExecution := s9ConvergenceObject(s9ConvergenceObject(row.After["executions"])[assignment])
		if stringValue(beforeExecution["delivery_ref"]) != candidate && stringValue(afterExecution["delivery_ref"]) == candidate && samePath(stringValue(row.Payload["cwd"]), source) {
			builder, candidateAt = actor, row.Ended
		}
		if !s9ConvergenceHasTarget(row.Before, target) && s9ConvergenceHasTarget(row.After, target) && samePath(stringValue(row.Payload["cwd"]), root) {
			verifier, targetAt = actor, row.Ended
		}
	}
	if builder == "" || verifier == "" || builder == verifier || targetAt <= candidateAt {
		t.Fatalf("actual child events did not receive distinct Builder and Targeted originals in order: builder=%q verifier=%q candidate=%d target=%d", builder, verifier, candidateAt, targetAt)
	}
	starts, stops := map[string]int64{}, map[string]int64{}
	returns := map[string][]int64{}
	reads := map[string]map[string]bool{builder: {}, verifier: {}, "Main": {}}
	mainContext, integration := false, false
	for _, row := range rows {
		actor := stringValue(row.Payload["agent_id"])
		if actor == "" {
			actor = "Main"
		}
		if row.Exit != 0 {
			continue
		}
		if row.Event == "SubagentStart" && starts[actor] == 0 {
			starts[actor] = row.Started
		}
		if row.Event == "SubagentStop" && stops[actor] == 0 {
			stops[actor] = row.Ended
		}
		input := s9ConvergenceObject(row.Payload["tool_input"])
		response := s9ConvergenceObject(row.Payload["tool_response"])
		tool := stringValue(row.Payload["tool_name"])
		if row.Event == "PostToolUse" && actor == "Main" && (tool == "Agent" || tool == "Task") && input["run_in_background"] != true && response["status"] == "completed" && response["is_error"] != true && response["interrupted"] != true {
			id := stringValue(response["agentId"])
			if id == "" {
				id = stringValue(response["agent_id"])
			}
			if id != "" {
				returns[id] = append(returns[id], row.Started)
			}
		}
		if actor == "Main" && row.Started >= candidateAt && row.Started < targetAt && strings.Contains(row.Stdout, assignment) && strings.Contains(row.Stdout, "runtime workspace integrate") {
			mainContext = true
		}
		if row.Event != "PostToolUse" || response["is_error"] == true || response["interrupted"] == true {
			continue
		}
		if actor == "Main" && tool == "Bash" && row.Started >= candidateAt && row.Started < targetAt {
			command := stringValue(input["command"])
			if strings.Contains(command, "runtime workspace integrate") && strings.Contains(command, "--check-command") && (response["exit_code"] == nil || response["exit_code"] == float64(0)) {
				integration = true
			}
		}
		if reads[actor] == nil {
			continue
		}
		if actor == "Main" {
			for fact, path := range map[string]string{
				"agents": filepath.Join(root, "AGENTS.md"), "protocol": filepath.Join(root, "docs/control/agent-protocol.md"),
				"loop": filepath.Join(root, ".claude/skills/loop-orchestration/SKILL.md"), "repair": filepath.Join(root, ".claude/skills/bug-resolution/SKILL.md"),
			} {
				reads[actor][fact] = reads[actor][fact] || s9ConvergenceReadMatches(row, path)
			}
		} else if actor == builder {
			reads[actor]["role"] = reads[actor]["role"] || s9ConvergenceReadMatches(row, resolveRootPath(root, expected.builderDefinition), resolveRootPath(source, expected.builderDefinition))
			reads[actor]["skill"] = reads[actor]["skill"] || s9ConvergenceReadMatches(row, filepath.Join(root, ".claude/skills/agent-dispatch/SKILL.md"), filepath.Join(source, ".claude/skills/agent-dispatch/SKILL.md"))
			reads[actor]["brief"] = reads[actor]["brief"] || s9ConvergenceReadMatches(row, expected.brief)
			reads[actor]["question"] = reads[actor]["question"] || s9ConvergenceReadMatches(row, expected.planQuestion)
		} else if actor == verifier {
			reads[actor]["role"] = reads[actor]["role"] || s9ConvergenceReadMatches(row, resolveRootPath(root, expected.verifierDefinition))
			reads[actor]["skill"] = reads[actor]["skill"] || s9ConvergenceReadMatches(row, filepath.Join(root, ".claude/skills/agent-dispatch/SKILL.md"))
			reads[actor]["target"] = reads[actor]["target"] || s9ConvergenceReadMatches(row, expected.targetQuestion)
		}
	}
	for _, actor := range []string{builder, verifier} {
		completed := false
		for _, at := range returns[actor] {
			completed = completed || starts[actor] != 0 && stops[actor] > starts[actor] && at >= stops[actor]
		}
		if !completed {
			t.Fatalf("missing synchronous completed parent return after this actor's actual Stop: actor=%s starts=%v stops=%v completed_returns=%v", actor, starts, stops, returns)
		}
	}
	if !mainContext || !integration {
		t.Fatalf("Main did not receive the original integration guidance and run the actual check command: context=%t integration=%t", mainContext, integration)
	}
	for actor, required := range map[string][]string{"Main": {"agents", "protocol", "loop", "repair"}, builder: {"role", "skill", "brief", "question"}, verifier: {"role", "skill", "target"}} {
		for _, fact := range required {
			if !reads[actor][fact] {
				t.Fatalf("actual %s did not read its installed %s material; observations=%v", actor, fact, reads[actor])
			}
		}
	}
}
