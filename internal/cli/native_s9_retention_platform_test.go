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

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This opt-in experiment uses synthetic professional answers and real Claude
// events. Controller redelivery below is explicitly NOT a natural platform
// event or proof of automatic production continuation.
func TestClaudeCodeNativeS9RetentionPlatform(t *testing.T) {
	if os.Getenv("GSB_S9_RETENTION_PLATFORM") != "1" {
		t.Skip("opt-in bounded Claude platform/source-retention experiment")
	}
	permissionMode := os.Getenv("GSB_S9_RETENTION_PERMISSION_MODE")
	if permissionMode == "" {
		permissionMode = "acceptEdits"
	}
	if permissionMode != "acceptEdits" && permissionMode != "bypassPermissions" {
		t.Fatal("only normal acceptEdits or an explicitly requested bypass contrast is supported")
	}
	claude := os.Getenv("GSB_S9_RETENTION_CLAUDE")
	envFile := os.Getenv("GSB_CLAUDE_ENV_FILE")
	if claude == "" || envFile == "" {
		t.Fatal("explicit Claude executable and environment file are required; credentials are never printed")
	}
	artifacts, err := os.MkdirTemp("", "gsb-s9-retention-platform-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Platform artifacts retained: %s", artifacts)
	originalRoot, plan, _, _, division := repairExecutionPlanningFixture(t)
	// The approved fixture freezes its original project_root. Execute there;
	// relocating it would invalidate authority. The later archive is evidence
	// only and is never used as a replacement checkout or Runtime source.
	root := originalRoot
	t.Cleanup(func() {
		if err := copyS9PlatformFixture(root, filepath.Join(artifacts, "archive-main")); err != nil {
			t.Errorf("archive actual fixture evidence: %v", err)
		}
	})
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, ".claude/bin/loop-harness")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/loop-harness")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual framework Hook: %v %s", err, output)
	}
	version, err := exec.CommandContext(t.Context(), claude, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(version), "2.1.311") {
		t.Fatalf("unexpected Claude version: %s", version)
	}
	observer := filepath.Join(artifacts, "observe.py")
	if err := os.WriteFile(observer, []byte(s9RetentionPlatformObserver), 0700); err != nil {
		t.Fatal(err)
	}
	settingsBytes, err := os.ReadFile(filepath.Join(repo, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatal(err)
	}
	for event, raw := range settings["hooks"].(map[string]any) {
		for _, registration := range raw.([]any) {
			for _, hook := range registration.(map[string]any)["hooks"].([]any) {
				hook.(map[string]any)["command"] = fmt.Sprintf("python3 %s %s %s %s", s9LiveShellQuote(observer), s9LiveShellQuote(artifacts), s9LiveShellQuote(root), s9LiveShellQuote(event))
			}
		}
	}
	settings["permissions"] = map[string]any{"allow": []string{"Agent", "Read", "Write(/" + root + "/**)", "Edit(/" + root + "/**)", "Bash(pwd)", "Bash(git status *)", "Bash(git rev-parse *)"}, "additionalDirectories": []string{root}}
	writeRepairLeadJSON(t, filepath.Join(root, ".claude/settings.json"), settings)
	ignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	writeRepairLeadFile(t, root, ".gitignore", string(ignore)+"\nlogs/\n")
	repairLeadGit(t, root, "add", ".gitignore")
	repairLeadGit(t, root, "add", "-f", ".claude/settings.json")
	repairLeadGit(t, root, "commit", "-qm", "Configure isolated normal-permission platform observation")
	feedback := repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	if err := os.WriteFile(filepath.Join(artifacts, "preparation-feedback.txt"), []byte(feedback), 0600); err != nil {
		t.Fatal(err)
	}
	packets := nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State)
	if len(packets) != 1 {
		t.Fatalf("expected exactly one original S9 responsibility, got %d: %s", len(packets), feedback)
	}
	p := packets[0]
	brief, _, resultPath := nativeS9Paths(p)
	logRef := "path:logs/s9-retention-platform.log"
	professionalPlan := workpacket.RepairPlanReportProfessionalAnswer{Plan: "SYNTHETIC platform-only fixture. Save original sample artifacts without implementing or evaluating any product behavior.", RedChecks: []workpacket.RepairPlanReportCheck{{Name: "synthetic storage observation", Command: "not executed: platform storage fixture only", Result: "blocked", EvidenceRefs: []string{logRef}}}, ProposedPaths: []string{}}
	professionalResult := workpacket.RepairResultProfessionalAnswer{Verdict: "completed", BeforeFixResults: []workpacket.RepairResultBeforeFixResult{}, UnitResults: []workpacket.RepairResultUnitResult{}, DetectionGapArtifacts: []string{}, EvidenceRefs: []string{logRef}, ScopeDeviations: []string{}, ResidualRisks: []string{"SYNTHETIC fixture only: no product repair or verification was performed"}}
	for _, id := range plan.Assignments[0].AssertionIDs {
		professionalResult.BeforeFixResults = append(professionalResult.BeforeFixResults, workpacket.RepairResultBeforeFixResult{AssertionID: id, ObservedRed: "SYNTHETIC fixture input; no product behavior was evaluated and no RED/PASS claim is made about a real product", EvidenceRefs: []string{logRef}})
	}
	for _, id := range plan.Assignments[0].UnitIDs {
		professionalResult.UnitResults = append(professionalResult.UnitResults, workpacket.RepairResultUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{logRef}, ImplementationSummary: "SYNTHETIC status exercises original storage/consumer wiring only; zero product change or validation", RestoredInvariantRefs: []string{}})
	}
	planJSON, _ := json.MarshalIndent(professionalPlan, "", "  ")
	resultJSON, _ := json.MarshalIndent(professionalResult, "", "  ")
	typedBlocker := filepath.Join(root, ".claude/evidence/s9-source", p.ID, "typed")
	writeRepairLeadFile(t, root, filepath.ToSlash(filepath.Join(".claude/evidence/s9-source", p.ID, "typed")), "controlled fault: prevent typed publication after original Plan reception")
	prompt := fmt.Sprintf(`This is an isolated platform storage experiment. Do not implement, fix, test, or commit product code. All professional text below is explicitly SYNTHETIC and makes no product correctness claim.
Your first tool must be a single native Agent call with subagent_type general-purpose, isolation worktree, run_in_background false. Its prompt must carry the EXACT original brief %s and all instructions/sample JSON below. Do not use a Team or another Claude process. Do not use controller recovery commands, change hooks/settings, or edit Runtime.
The child must read that exact brief. For this controlled experiment it must NOT do the brief's usual product-development work. In its actual native worktree, first Write logs/s9-retention-platform.log containing exactly SYNTHETIC_PLATFORM_RETENTION_CHECK followed by a newline. Then Write the following synthetic Plan at %s, relative to its actual worktree, BEFORE saving the Result. Then Write the synthetic Result at %s, relative to its actual worktree. Use the Write tool with absolute paths rooted in its own actual CWD; create no other files. No Git commit, seal, or additional author action. Ordinary tools remain available, but no development work is requested.
PLAN JSON:
%s
RESULT JSON:
%s
The child's final response must contain the EXACT brief %s and its own absolute Result path. Do not quote JSON in the final response. After the native child returns, parent must report the returned exact references and finish without repairing the controlled framework failure or performing further tools. This lets the platform finish its natural nochange-worktree lifecycle.`, filepath.Join(root, brief), p.AnswerPath, resultPath, planJSON, resultJSON, filepath.Join(root, brief))
	if err := os.WriteFile(filepath.Join(artifacts, "prompt.txt"), []byte(prompt), 0600); err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, filepath.Join(artifacts, "conditions.json"), map[string]any{"platform_version": strings.TrimSpace(string(version)), "permission_mode": permissionMode, "setting_sources": "project", "bypass": permissionMode == "bypassPermissions", "ordinary_tools_available": true, "professional_work": "synthetic fixture only; no product development", "fault": "regular file blocks typed-directory publication after Plan", "recovery": "controller redelivery of saved real Stop; not natural platform continuation", "packet_id": p.ID, "main_root": root, "source_selection_trust": "original non-Main actor plus exact references and checkout; no independent actor/worktree ownership proof"})
	mainHead := repairLeadGit(t, root, "rev-parse", "HEAD")
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	script := `source "$1" >/dev/null 2>&1; shift; exec "$@"`
	command := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script, "s9-platform", envFile, claude, "-p", "--permission-mode", permissionMode, "--output-format", "stream-json", "--verbose", "--include-hook-events", "--setting-sources", "project", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-chrome")
	command.Dir, command.Stdin = root, strings.NewReader(prompt)
	stdout, err := os.Create(filepath.Join(artifacts, "stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.Create(filepath.Join(artifacts, "stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		t.Fatalf("actual Claude process failed (see retained artifacts): %v", err)
	}
	stops, err := filepath.Glob(filepath.Join(artifacts, "stop-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stops) != 1 {
		t.Fatalf("expected one real SubagentStop payload, found %d; inspect events", len(stops))
	}
	raw, err := os.ReadFile(stops[0])
	if err != nil {
		t.Fatal(err)
	}
	var stop struct {
		AgentID string `json:"agent_id"`
		CWD     string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &stop); err != nil {
		t.Fatal(err)
	}
	partial := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(partial.State)
	if err != nil || registry == nil {
		t.Fatalf("actual Stop did not register source: %v", err)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	if e.DeliveryRef != "" || repair.NativeS9SourceReference(e).Path == "" {
		t.Fatalf("controlled pre-Candidate fault not reached: %+v", e)
	}
	if stop.AgentID == "" || stop.AgentID == "Main" || e.Path == root {
		t.Fatal("real original native event/source coordinates missing")
	}
	if _, err := os.Stat(e.Path); !os.IsNotExist(err) {
		t.Fatalf("platform did not naturally clean original nochange tree %s: %v", e.Path, err)
	}
	retained, err := repair.RetainedNativeS9Source(t.Context(), root, e, p.Binding)
	if err != nil || retained == nil {
		t.Fatalf("real Stop retained source failed after cleanup: %v", err)
	}
	if len(retained.Evidence) != 1 {
		t.Fatalf("actual local evidence missing: %+v", retained.Evidence)
	}
	if err := os.Remove(typedBlocker); err != nil {
		t.Fatal(err)
	}
	// Only the external test controller releases the fault and redelivers the
	// captured real payload. This is mechanism recovery after REAL cleanup.
	var out, diagnostic bytes.Buffer
	started := time.Now()
	code := Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(raw), &out, &diagnostic)
	writeRepairLeadJSON(t, filepath.Join(artifacts, "controller-recovery.json"), map[string]any{"kind": "controller_redelivery_not_platform_event", "source_payload": stops[0], "exit_code": code, "elapsed_ms": time.Since(started).Milliseconds(), "stdout": out.String(), "stderr": diagnostic.String()})
	if code != 0 {
		t.Fatalf("original public consumer redelivery failed: %d %s", code, &diagnostic)
	}
	final := repairDispatchSnapshot(t, root)
	finalRegistry, err := workspace.Decode(final.State)
	if err != nil {
		t.Fatal(err)
	}
	delivered := finalRegistry.Executions[e.AssignmentID]
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: delivered.DeliveryRef, SHA256: delivered.DeliverySHA256})
	if err != nil {
		t.Fatalf("original consumer did not accept retained input: %v %s", err, &diagnostic)
	}
	if candidate.Provenance == nil || candidate.Provenance.ExecutionRoot != e.Path || candidate.SourceHead != mainHead || len(candidate.Proposal.ChangedArtifacts) != 0 {
		t.Fatalf("retained source provenance/nochange mismatch: %+v", candidate)
	}
	if repairLeadGit(t, root, "rev-parse", "HEAD") != mainHead {
		t.Fatal("experiment changed product commit")
	}
	if _, err := os.Stat(e.Path); !os.IsNotExist(err) {
		t.Fatal("controller recreated cleaned source")
	}
	writeRepairLeadJSON(t, filepath.Join(artifacts, "result.json"), map[string]any{"platform_version": strings.TrimSpace(string(version)), "permission_mode": permissionMode, "actual_stop_actor": stop.AgentID, "original_source_root": e.Path, "source_gone_after_platform_exit": true, "source_ref": repair.NativeS9SourceReference(e), "git_ref": retained.GitRef, "source_head": retained.Head, "original_plan_bytes_sha256": workpacket.Digest(retained.Plan), "original_result_bytes_sha256": workpacket.Digest(retained.Result), "local_evidence": retained.Evidence, "candidate_ref": delivered.DeliveryRef, "candidate_sha256": delivered.DeliverySHA256, "controller_redelivery": true, "natural_automatic_terminal_state_proven": false, "product_diff_empty": true, "final_impact_targeted_fresh_s7": "not executed in this platform experiment"})
	t.Logf("Real Stop retained input and natural cleanup verified; controller redelivery received original Candidate. Not proof of natural automatic completion. Artifacts %s", artifacts)
}

func copyS9PlatformFixture(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, dest)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, body, info.Mode().Perm())
	})
}

const s9RetentionPlatformObserver = `import sys,os,json,time,subprocess,hashlib
from pathlib import Path
artifacts,main,event=sys.argv[1:]
raw=sys.stdin.buffer.read();payload=json.loads(raw);started=time.time_ns();cwd=payload.get('cwd','')
def state():
 try:
  s=json.loads((Path(main)/'.claude/loop-state.json').read_text());p=s.get('review',{}).get('repair',{})
  return {'revision':s.get('revision'),'plan_report_refs':p.get('plan_report_refs'),'executions':s.get('workspace',{}).get('executions',{})}
 except Exception:return None
def observation():
 result={'cwd_exists':os.path.isdir(cwd)}
 if os.path.isdir(cwd):
  for name,args in [('head',['rev-parse','HEAD']),('status',['status','--porcelain']),('common',['rev-parse','--path-format=absolute','--git-common-dir'])]:
   p=subprocess.run(['git','-C',cwd]+args,capture_output=True);result[name]=p.stdout.decode(errors='replace').strip() if p.returncode==0 else None
 return result
before=state();source_before=observation()
if event=='SubagentStop' and payload.get('agent_id'):
 (Path(artifacts)/('stop-'+str(started)+'.json')).write_bytes(raw)
p=subprocess.run([str(Path(main)/'.claude/bin/loop-harness'),'hook','--root',main,'--event',event],input=raw,capture_output=True)
row={'event':event,'started_ns':started,'finished_ns':time.time_ns(),'actor':payload.get('agent_id'),'permission_mode':payload.get('permission_mode'),'cwd':cwd,'source_before':source_before,'source_after':observation(),'state_before':before,'state_after':state(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'payload_keys':sorted(payload),'tool':payload.get('tool_name'),'file_path':payload.get('tool_input',{}).get('file_path')}
with (Path(artifacts)/'events.jsonl').open('a') as f:f.write(json.dumps(row)+'\n')
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
`
