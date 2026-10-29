package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func coreBuilderFixture(t *testing.T, configure ...func(string, map[string]any)) stageAnswerFixture {
	return coreBuilderFixtureChecks(t, []string{"go test ./..."}, configure...)
}

func coreHookAdditionalContext(t *testing.T, raw string) string {
	t.Helper()
	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode Hook additionalContext: %v; raw=%s", err, raw)
	}
	return payload.HookSpecificOutput.AdditionalContext
}

func coreBuilderFixtureChecks(t *testing.T, checks []string, configure ...func(string, map[string]any)) stageAnswerFixture {
	return coreBuilderFixtureProfiles(t, checks, nil, configure...)
}
func coreBuilderFixtureProfiles(t *testing.T, checks []string, profiles []checkexec.Profile, configure ...func(string, map[string]any)) stageAnswerFixture {
	return coreBuilderFixtureProfilesReadSources(t, checks, profiles, nil, nil, configure...)
}
func coreBuilderFixtureWithReadSources(t *testing.T, readPaths []string, sources map[string][]byte) stageAnswerFixture {
	return coreBuilderFixtureProfilesReadSources(t, []string{"go test ./..."}, nil, readPaths, sources)
}
func coreBuilderFixtureProfilesReadSources(t *testing.T, checks []string, profiles []checkexec.Profile, readPaths []string, readSources map[string][]byte, configure ...func(string, map[string]any)) stageAnswerFixture {
	t.Helper()
	f := newStageAnswerFixtureMode(t, true, configure...)
	var lastReceiptError, lastStage string
	var lastWaiting any
	deferredFollowup := false
	for _, p := range f.packets {
		f.begin(t, p, nil)
		r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
		lastReceiptError, lastStage, lastWaiting = r.Error, r.CurrentStage, r.WaitingOn
		deferredFollowup = false
		for _, waiting := range r.WaitingOn {
			if waiting.Reason == "system_followup" && waiting.Owner == "Main" && waiting.Ref != "" {
				deferredFollowup = true
			}
		}
		if !r.WorkComplete {
			t.Fatal("original S5 responsibility not accepted")
		}
	}
	if stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "building" && deferredFollowup {
		// Accepted work may leave a durable control followup when the Hook's
		// bounded budget expires. Resume through the actual Main consumer;
		// never seed a transition, repeat the answer or relax the original gate.
		recovered := stageCLI(t, "runtime", "stage-answer", "recover", "--root", f.root)
		t.Logf("original Main resumed declared S5 followup: %s", recovered)
	}
	if stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "building" {
		snapshot := stageSnapshot(t, f.root)
		milestone, _ := snapshot.State["milestone"].(map[string]any)
		t.Fatalf("actual S5 did not reach S6: receipt_error=%s receipt_stage=%s waiting=%v lifecycle=%v blocker=%v missing=%v action=%v recovery=%v", lastReceiptError, lastStage, lastWaiting, snapshot.State["lifecycle"], milestone["blocker"], milestone["missing"], milestone["action"], milestone["recovery"])
	}
	role, err := os.ReadFile("../../agents/backend-builder.md")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(f.root, ".claude/agents/backend-builder.md"), role, 0600)
	for _, name := range []string{"backend-engineering", "testing-strategy", "code-quality", "api-contracts"} {
		data, err := os.ReadFile(filepath.Join("../../skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, ".claude/skills", name, "SKILL.md")
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, data, 0600)
	}
	os.MkdirAll(filepath.Join(f.root, "src"), 0700)
	os.WriteFile(filepath.Join(f.root, "go.mod"), []byte("module fixture.invalid/core\n\ngo 1.26\n"), 0600)
	os.WriteFile(filepath.Join(f.root, "src/value.go"), []byte("package src\nfunc Value() int { return 1 }\n"), 0600)
	os.WriteFile(filepath.Join(f.root, "src/value_test.go"), []byte("package src\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=1 { t.Fatal(\"value\") } }\n"), 0600)
	for path, data := range readSources {
		full := filepath.Join(f.root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	addArgs := []string{"add", "go.mod", "src"}
	for path := range readSources {
		addArgs = append(addArgs, filepath.ToSlash(path))
	}
	for _, args := range [][]string{addArgs, {"commit", "-m", "fixture product base"}} {
		if _, err := workspace.Git(context.Background(), f.root, args...); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(f.root, ".claude/workgroups/REQ-039/review/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	m["manifest_id"], m["workgroup_id"], m["workgroup_kind"] = "team-manifest-core-builder", "workgroup-core-builder", "builder"
	m["planned_agent_count"], m["max_parallel_agents"] = 1, 1
	m["quantity_rationale"] = "One actual independent backend work package with owned tests"
	a := m["assignments"].([]any)[0].(map[string]any)
	a["assignment_id"], a["agent_id"], a["responsibility_id"] = "assignment-core-builder", "core-builder", "BUILD-WORK-PACKAGE"
	a["role_family"], a["agent_definition_ref"] = "backend-builder", ".claude/agents/backend-builder.md"
	a["skill_refs"] = []string{"testing-strategy", "api-contracts"}
	a["write_paths"], a["output_paths"] = []string{"src/**"}, []string{"docs/reports/core-builder.json"}
	if readPaths != nil {
		m["assignment_record_contract"] = assignmentrecord.AssignmentRecordContract
		a["read_paths"] = append([]string{}, readPaths...)
	}
	a["required_checks"] = checks
	if profiles != nil {
		a["check_profiles"] = profiles
	}
	a["done_when"] = []string{"Own the scoped implementation and tests; submit the original completion report for Main integration"}
	m["assignments"] = []any{a}
	m["responsibility_dispositions"] = []any{map[string]any{"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "actual backend work package", "assignment_ids": []string{"assignment-core-builder"}, "na_rationale": nil, "evidence_ref": nil}}
	m["separation_edges"] = []any{}
	path := ".claude/workgroups/REQ-039/core-builder/manifest.json"
	stageJSON(t, f.root, path, m)
	beforeRegistration := stageSnapshot(t, f.root)
	stageCLI(t, "runtime", "register-workgroup", "--root", f.root, "--manifest", path, "--task-id", f.taskID, "--task", f.taskPath)
	if readPaths != nil {
		registered, found, err := assignmentrecord.ForOwner(stageSnapshot(t, f.root).State, "core-builder")
		if err != nil || !found {
			t.Fatalf("read-closure fixture did not register its canonical Assignment Record: found=%t err=%v", found, err)
		}
		if !reflect.DeepEqual(registered.ReadPaths, readPaths) {
			t.Fatalf("canonical Assignment Record read paths = %v, want %v", registered.ReadPaths, readPaths)
		}
	}
	beforeTasks := beforeRegistration.State["entities"].(map[string]any)["tasks"].([]any)
	afterTasks := stageSnapshot(t, f.root).State["entities"].(map[string]any)["tasks"].([]any)
	for _, raw := range beforeTasks {
		prior := raw.(map[string]any)
		if prior["id"] != f.taskID {
			continue
		}
		for _, nextRaw := range afterTasks {
			next := nextRaw.(map[string]any)
			if next["id"] != f.taskID {
				continue
			}
			owners := next["owner_agent_ids"].([]any)
			found := false
			for _, owner := range owners {
				if owner == "core-builder" {
					found = true
				}
			}
			if !found {
				t.Fatal("original registration failed to bind Builder TASK owner")
			}
			for _, owner := range prior["owner_agent_ids"].([]any) {
				kept := false
				for _, current := range owners {
					if current == owner {
						kept = true
					}
				}
				if !kept {
					t.Fatal("registration lost earlier TASK owner")
				}
			}
			delete(prior, "owner_agent_ids")
			delete(next, "owner_agent_ids")
			if !reflect.DeepEqual(prior, next) {
				t.Fatal("binding a dispatched owner changed other TASK facts")
			}
		}
	}
	return f
}
func prepareCoreBuilder(t *testing.T, f stageAnswerFixture, profile string) workspace.Execution {
	t.Helper()
	args := []string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", "assignment-core-builder", "--agent", "core-builder", "--check-location", "main"}
	if profile != "" {
		args = append(args, "--context-profile", profile)
	}
	output := stageCLI(t, args...)
	var parsed struct {
		workspace.Execution
		Mode string `json:"context_mode"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatal(err)
	}
	want := "shadow"
	if profile == "backend-core" {
		want = "enabled"
	}
	if parsed.Mode != want {
		t.Fatalf("actual prepare mode %s want %s", parsed.Mode, want)
	}
	return parsed.Execution
}

func TestWorkspacePrepareFreezesCanonicalDeclaredReadSources(t *testing.T) {
	paths := []string{"docs/architecture/data-model/model.md", "docs/architecture/state/reservation.md"}
	sources := map[string][]byte{
		paths[0]: []byte("# Data model\nThe contract model is the source of truth.\n"),
		paths[1]: []byte("# Reservation state\nThe reservation is immutable until accepted.\n"),
	}
	root, taskID, taskPath := newS6RecordContextFixtureWithReadSources(t, sources)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	manifestBytes, err := os.ReadFile(filepath.Join(root, manifestRef))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	readPaths := assignment["read_paths"].([]any)
	for _, path := range paths {
		readPaths = append(readPaths, path)
	}
	assignment["read_paths"] = readPaths
	for _, raw := range assignment["check_profiles"].([]any) {
		raw.(map[string]any)["location"] = "main"
	}
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	snapshot := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, "agent-s6-native-context")
	if err != nil || !found {
		t.Fatalf("original canonical Assignment Record was not registered: found=%t err=%v", found, err)
	}
	for _, rel := range paths {
		if !slices.Contains(record.ReadPaths, rel) {
			t.Fatalf("canonical Assignment Record omitted declared source %s: %v", rel, record.ReadPaths)
		}
	}
	output := stageCLI(t, "runtime", "workspace", "prepare", "--root", root, "--assignment", record.AssignmentID, "--agent", record.OwnerAgentID, "--check-location", "main")
	var prepared struct {
		workspace.Execution
		Mode string `json:"context_mode"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatalf("decode original Workspace execution: %v\n%s", err, output)
	}
	e := prepared.Execution
	bound := snapshot.State["bound_req"].(map[string]any)
	devBranch := bound["workspace"].(map[string]any)["dev_branch"].(string)
	head, err := workspace.Git(context.Background(), root, "rev-parse", "refs/heads/"+devBranch)
	if err != nil || head != e.BaseCommit {
		t.Fatalf("prepared base is not the bound development commit: base=%s branch=%s err=%v", e.BaseCommit, head, err)
	}
	for _, rel := range paths {
		want := sources[rel]
		if e.Inputs[rel] != workpacket.Digest(want) {
			t.Fatalf("Execution omitted or changed canonical read input %s", rel)
		}
		worker, err := os.ReadFile(filepath.Join(e.Path, ".claude/inputs", filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(worker, want) {
			t.Fatalf("Worker input differs from canonical read source %s: %v", rel, err)
		}
		gitBlob, err := exec.Command("git", "-C", root, "show", e.BaseCommit+":"+rel).Output()
		if err != nil || !bytes.Equal(gitBlob, want) {
			t.Fatalf("test source was not the prepared regular Git blob %s: %v", rel, err)
		}
		if workpacket.Digest(worker) != e.Inputs[rel] {
			t.Fatalf("Worker input SHA differs from Execution for %s", rel)
		}
	}
}

func TestWorkspacePlanRejectsAddingInputsToPreparedExecution(t *testing.T) {
	f := coreBuilderFixture(t)
	e := prepareCoreBuilder(t, f, "")
	before := stageSnapshot(t, f.root)
	registry, err := workspace.Decode(before.State)
	if err != nil {
		t.Fatal(err)
	}
	old := registry.Executions[e.AssignmentID]
	_, err = registry.Plan(context.Background(), before.State, e.AssignmentID, e.AgentID, old.WritePaths, old.Checks, []string{"docs/architecture/data-model/model.md"})
	if err == nil || !strings.Contains(err.Error(), "lacks frozen input") {
		t.Fatalf("prepared execution accepted a new source input in place: %v", err)
	}
	after := stageSnapshot(t, f.root)
	if after.Revision != before.Revision {
		t.Fatal("rejecting an execution retrofit wrote Runtime")
	}
	current, err := workspace.Decode(after.State)
	if err != nil || !reflect.DeepEqual(current.Executions[e.AssignmentID], old) {
		t.Fatalf("rejecting an execution retrofit changed the frozen execution: %v", err)
	}
}

func TestWorkspaceBackendCoreInstallsFixedRoleWithoutChangingActivation(t *testing.T) {
	f := coreBuilderFixture(t)
	before, err := os.ReadFile(filepath.Join(f.root, ".claude/agents/backend-builder.md"))
	if err != nil {
		t.Fatal(err)
	}
	source := assignment.ActivationSourceEntry{AgentID: "core-builder", AgentDefinitionRef: ".claude/agents/backend-builder.md", SkillRefs: []string{"testing-strategy", "api-contracts"}, WritePaths: []string{"src/**"}, OutputPaths: []string{"docs/reports/core-builder.json"}}
	ref, err := assignment.PreStageActivationEnvelope(f.root, "workgroup-core-builder", f.taskID, "core-builder", "plan_checkpoint", source)
	if err != nil {
		t.Fatal(err)
	}
	authBefore, _ := os.ReadFile(filepath.Join(f.root, ref))
	e := prepareCoreBuilder(t, f, "backend-core")
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	c, err := registry.ReadDispatchContext(e)
	if err != nil {
		t.Fatal(err)
	}
	if c.GeneratedRole == nil || c.Selection.Mode != "enabled" || !reflect.DeepEqual(c.Selection.AuthorizationSkillRefs, source.SkillRefs) {
		t.Fatal("enabled context replaced authorization or omitted frozen role")
	}
	actual, err := os.ReadFile(filepath.Join(e.Path, c.GeneratedRole.Target))
	if err != nil {
		t.Fatal(err)
	}
	if workpacket.Digest(actual) != c.GeneratedRole.SHA256 {
		t.Fatal("Worker did not install actual fixed generated role")
	}
	frontmatter := strings.SplitN(string(actual), "---", 3)[1]
	if strings.Count(frontmatter, "  - ") != 4 || strings.Contains(frontmatter, "  - gorm") {
		t.Fatalf("broad role still preloaded: %s", frontmatter)
	}
	for _, skill := range c.Selection.Selected {
		data, err := os.ReadFile(filepath.Join(e.Path, skill.Source))
		if err != nil || workpacket.Digest(data) != skill.SHA256 {
			t.Fatalf("selected extra Skill is unreadable: %s %v", skill.Source, err)
		}
	}
	if err := registry.ValidateBootstrap(e); err != nil {
		t.Fatal(err)
	}
	authAfter, _ := os.ReadFile(filepath.Join(f.root, ref))
	mainAfter, _ := os.ReadFile(filepath.Join(f.root, ".claude/agents/backend-builder.md"))
	if !bytes.Equal(authBefore, authAfter) || !bytes.Equal(before, mainAfter) {
		t.Fatal("core loading changed Main role or original activation bytes")
	}
	var auth assignment.ActivationEnvelope
	json.Unmarshal(authAfter, &auth)
	var originalAuth assignment.ActivationEnvelope
	json.Unmarshal(authBefore, &originalAuth)
	if !reflect.DeepEqual(auth.AllowedCommandClasses, originalAuth.AllowedCommandClasses) {
		t.Fatalf("selected backend core changed original commands: %v -> %v", originalAuth.AllowedCommandClasses, auth.AllowedCommandClasses)
	}
	data, err := os.ReadFile(filepath.Join(e.Path, workspace.PlanDraftPath))
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	json.Unmarshal(data, &plan)
	plan["objective"] = "Implement the registered backend change and its owned assertions"
	plan["steps"] = []any{map[string]any{"description": "Implement the scoped behavior and adjust owned tests", "target": "src/value.go"}}
	plan["assertion_checks"] = []any{map[string]any{"assertion": "Value and owned regression test match the agreed result", "oracle": "go test ./... passes against the scoped candidate"}}
	plan["risks_blockers"] = []any{}
	stageJSON(t, e.Path, workspace.PlanDraftPath, plan)
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", mustReadCoreFile(t, e.Path, workspace.PlanDraftPath)); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "workspace", "begin", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	output := stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	if !strings.Contains(output, "\"context_mode\":\"enabled\"") {
		t.Fatal("ready retry silently reset mode")
	}
	var out, errout bytes.Buffer
	rev := stageSnapshot(t, f.root).Revision
	if code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--context-profile", "shadow"}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != rev {
		t.Fatal("ready profile was silently rewritten")
	}
}
func mustReadCoreFile(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func beginCoreBuilder(t *testing.T, e workspace.Execution) {
	t.Helper()
	var plan map[string]any
	json.Unmarshal(mustReadCoreFile(t, e.Path, workspace.PlanDraftPath), &plan)
	plan["objective"] = "Implement the registered backend value and its owned regression assertions"
	plan["steps"] = []any{map[string]any{"description": "Update the scoped behavior and its owned regression test", "target": "src/value.go"}}
	plan["assertion_checks"] = []any{map[string]any{"assertion": "Updated Value returns the agreed result", "oracle": "Owned go test ./... passes for the scoped candidate"}}
	plan["risks_blockers"] = []any{}
	stageJSON(t, e.Path, workspace.PlanDraftPath, plan)
	stageCLI(t, "runtime", "workspace", "begin", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
}

func assertCoreBuilderReceipt(t *testing.T, root, owner, id string, complete, integrated bool) qualitygate.BuilderReceipt {
	t.Helper()
	control, err := workspace.ResolveControl(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, control)
	output := stageCLI(t, "runtime", "task-receipt", "--root", root, "--agent-id", owner, "--assignment-id", id)
	var receipt qualitygate.BuilderReceipt
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.WorkComplete != complete || receipt.Integrated != integrated || receipt.ChecksSource != "original_report" {
		checkpointPath := integration.CheckpointPath(control, receipt.RuntimeID, receipt.Generation, id)
		checkpointBytes, _ := os.ReadFile(checkpointPath)
		t.Fatalf("unexpected receipt: %+v; original checkpoint: %s", receipt, checkpointBytes)
	}
	if complete && !integrated && (len(receipt.OwnPending) != 0 || len(receipt.WaitingOn) != 1 || receipt.WaitingOn[0].Owner != "main") {
		t.Fatalf("Main integration became owner pending: %+v", receipt)
	}
	if integrated && len(receipt.WaitingOn) != 0 {
		t.Fatalf("current verified merge still pending: %+v", receipt)
	}
	if stageSnapshot(t, control).Revision != before.Revision {
		t.Fatal("read-only receipt wrote Runtime")
	}
	return receipt
}

func TestWorkspaceBackendCoreLaunchCommitAndOriginalVerifiedIntegration(t *testing.T) {
	runCoreBuilderIntegration(t, coreBuilderFixture(t))
}
func runCoreBuilderIntegration(t *testing.T, f stageAnswerFixture, afterReport ...func(workspace.Execution)) {
	t.Helper()
	e := prepareCoreBuilder(t, f, "backend-core")
	beginCoreBuilder(t, e)
	assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, false, false)
	beforeStop := stageSnapshot(t, f.root)
	if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "Stop", ""); code != 2 {
		t.Fatalf("Builder without Result stopped: %d %s", code, msg)
	}
	if stageSnapshot(t, f.root).Revision != beforeStop.Revision {
		t.Fatal("blocked Worker Stop ran Main's control cycle")
	}
	if code, msg := stageHookCode(t, f.root, f.root, e.AgentID, "Stop", ""); code != 2 {
		t.Fatalf("Worker Stop accepted Main CWD: %d %s", code, msg)
	}
	guardPayload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "agent_id": e.AgentID, "cwd": e.Path, "stop_hook_active": true})
	var guardOut, guardErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "Stop", "--root", f.root}, bytes.NewReader(guardPayload), &guardOut, &guardErr); code != 0 {
		t.Fatalf("Worker Stop ignored original loop guard: %d %s", code, guardErr.String())
	}
	if stageSnapshot(t, f.root).Revision != beforeStop.Revision {
		t.Fatal("Worker Stop diagnostic/loop guard wrote Main state")
	}
	fakeDir := t.TempDir()
	fake := `#!/usr/bin/python3
import hashlib,json,pathlib,sys
args=sys.argv[1:]
role=args[args.index('--agent')+1]
context=json.loads(pathlib.Path('.claude/inputs/assignment-context.json').read_text())
role_bytes=pathlib.Path('.claude/agents/'+role+'.md').read_bytes()
reads=[]
for skill in context['context_selection']['selected']:
    actual=pathlib.Path(skill['source']).read_bytes()
    assert hashlib.sha256(actual).hexdigest()==skill['sha256']
    reads.append(skill['name'])
print(json.dumps({'cwd':str(pathlib.Path.cwd()),'role':role,'role_sha256':hashlib.sha256(role_bytes).hexdigest(),'read_skills':reads,'mode':context['context_selection']['mode'],'allowed_tools':args[args.index('--allowedTools')+1],'prompt':args[-1]}))
`
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil || registry == nil {
		t.Fatalf("load original S6 workspace before launch: %v", err)
	}
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil {
		t.Fatal(err)
	}
	mainCode, rawGuidance := stageHookCode(t, f.root, f.root, "Main", "PreToolUse", "")
	guidance := coreHookAdditionalContext(t, rawGuidance)
	if mainCode != 0 ||
		!strings.Contains(guidance, "No platform session is recorded yet") ||
		!strings.Contains(guidance, `--root "`+f.root+`" --assignment `+e.AssignmentID+` --agent `+e.AgentID) ||
		!strings.Contains(guidance, filepath.Join(e.Path, filepath.FromSlash(paths.Plan))) ||
		!strings.Contains(guidance, filepath.Join(e.Path, filepath.FromSlash(paths.Completion))) ||
		strings.Contains(guidance, "Fill PLAN") || strings.Contains(guidance, "fill Result") {
		t.Fatalf("Main must launch the ready registered execution once and coordinate, not fill its questionnaires: code=%d guidance=%s", mainCode, guidance)
	}
	output := stageCLI(t, "runtime", "workspace", "launch", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	var child map[string]any
	if err := json.Unmarshal([]byte(output), &child); err != nil {
		t.Fatal(err)
	}
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	e = b.Executions[e.AssignmentID]
	if e.PlatformSessionID == "" {
		t.Fatal("original workspace launch did not persist its platform session")
	}
	mainCode, rawGuidance = stageHookCode(t, f.root, f.root, "Main", "PreToolUse", "")
	guidance = coreHookAdditionalContext(t, rawGuidance)
	if mainCode != 0 ||
		!strings.Contains(guidance, "recorded platform session "+e.PlatformSessionID) ||
		!strings.Contains(guidance, "reserved before process start") ||
		!strings.Contains(guidance, "only if it stopped before reporting") ||
		!strings.Contains(guidance, "--resume") || strings.Contains(guidance, "No platform session is recorded yet") ||
		strings.Contains(guidance, "Fill PLAN") || strings.Contains(guidance, "fill Result") {
		t.Fatalf("Main must track the original recorded session and resume only that session when needed: code=%d guidance=%s", mainCode, guidance)
	}
	workerCode, rawGuidance := stageHookCode(t, f.root, e.Path, e.AgentID, "PreToolUse", filepath.Join(e.Path, filepath.FromSlash(paths.Completion)))
	guidance = coreHookAdditionalContext(t, rawGuidance)
	if workerCode != 0 ||
		!strings.Contains(guidance, filepath.Join(e.Path, filepath.FromSlash(paths.Completion))) ||
		!strings.Contains(guidance, "After the accepted PLAN and actual TASK work") {
		t.Fatalf("registered Builder lost its exact Result path and instructions: code=%d guidance=%s", workerCode, guidance)
	}
	c, err := b.ReadDispatchContext(e)
	if err != nil {
		t.Fatal(err)
	}
	if child["cwd"] != e.Path || child["role"] != "backend-builder" || child["role_sha256"] != c.GeneratedRole.SHA256 || child["mode"] != "enabled" || child["allowed_tools"] != "Read,Glob,Grep,Write,Edit,Bash" {
		t.Fatal("child role/CWD/tools differ from the original fixed execution")
	}
	if !strings.Contains(child["prompt"].(string), "fixed backend core profile is enabled") {
		t.Fatal("launch still described a shadow profile")
	}
	if len(child["read_skills"].([]any)) != len(c.Selection.Selected) {
		t.Fatal("fake process did not read every delivered source")
	}
	os.WriteFile(filepath.Join(e.Path, "src/value.go"), []byte("package src\nfunc Value() int { return 2 }\n"), 0600)
	os.WriteFile(filepath.Join(e.Path, "src/value_test.go"), []byte("package src\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=2 { t.Fatal(\"value\") } }\n"), 0600)
	stageJSON(t, e.Path, ".loop-submissions/commit.json", workspace.CommitRequest{ExpectedHead: e.BaseCommit, Message: "Implement the scoped value and owned test", Paths: []string{"src/value.go", "src/value_test.go"}})
	stageCLI(t, "runtime", "workspace", "commit", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	// This is an actual local test run for the author's report, not an invented
	// check receipt; Main's original integration runs the required check again.
	command := exec.Command("go", "test", "./...")
	command.Dir = e.Path
	command.Env = os.Environ()
	checked, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("owned candidate check: %v %s", err, checked)
	}
	checkPath := ".claude/evidence/owned-core-check.txt"
	os.MkdirAll(filepath.Dir(filepath.Join(e.Path, checkPath)), 0700)
	os.WriteFile(filepath.Join(e.Path, checkPath), checked, 0600)
	examples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	json.Unmarshal(examples, &messages)
	var completion map[string]any
	for _, m := range messages {
		if m["message_type"] == "completion_report" {
			completion = m
			break
		}
	}
	completion["runtime_id"], completion["agent_id"], completion["agent_definition_ref"] = e.RuntimeID, e.AgentID, ".claude/agents/backend-builder.md"
	completion["task_id"], completion["team_id"] = f.taskID, "workgroup-core-builder"
	completion["summary"] = "Updated the registered scoped value and verified its owned regression test"
	completion["changed_paths"] = []string{"src/value.go", "src/value_test.go"}
	completion["checks"] = []any{map[string]any{"name": "owned value regression", "command": "go test ./...", "result": "pass", "evidence_ref": checkPath}}
	completion["evidence_refs"] = []string{checkPath}
	stageJSON(t, e.Path, ".loop-submissions/completion.json", completion)
	stageCLI(t, "runtime", "workspace", "report", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	for _, observe := range afterReport {
		observe(e)
	}
	assertCoreReceptionAdvice(t, f, e.AssignmentID, true)
	assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, false)
	assertCoreBuilderReceipt(t, e.Path, e.AgentID, e.AssignmentID, true, false)
	for _, command := range []string{workspace.ReceiptInvocation(e), workspace.ReceiptInvocation(e) + "; touch forbidden"} {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": e.Path, "agent_id": e.AgentID, "tool_name": "Bash", "tool_input": map[string]any{"command": command}})
		var out, errOut bytes.Buffer
		code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", f.root}, bytes.NewReader(payload), &out, &errOut)
		if code != 0 || strings.Contains(out.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("PreToolUse observer denied or failed for command %q: %d %s %s", command, code, out.String(), errOut.String())
		}
	}
	currentReceipt := assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, false)
	canonicalReport := filepath.Join(f.root, currentReceipt.DomainRefs[0])
	reportBytes, err := os.ReadFile(canonicalReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalReport, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	invalidReceipt := assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, false, false)
	if invalidReceipt.Accepted || len(invalidReceipt.OwnPending) != 0 || len(invalidReceipt.WaitingOn) != 1 || invalidReceipt.WaitingOn[0].Reason != "result_evidence_invalid" {
		t.Fatalf("lost domain evidence re-assigned author work: %+v", invalidReceipt)
	}
	if err := os.WriteFile(canonicalReport, reportBytes, 0600); err != nil {
		t.Fatal(err)
	}
	assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, false)
	var peerOut, peerErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-receipt", "--root", e.Path, "--agent-id", f.packets[0].Binding.Owner, "--assignment-id", e.AssignmentID}, nil, &peerOut, &peerErr); code == 0 {
		t.Fatal("Worker queried peer identity through Main root routing")
	}
	loaded, err := hookctx.LoadFull(f.root, e.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	facts := hook.ProjectResponsibility(loaded.PolicyContext.Agent, hook.AssignmentForAgent(loaded.Assignments, e.AgentID), false)
	if !facts.DeliveryRecorded || !facts.ExecutionMayEnd || !facts.ConsumerPending || facts.ConsumerOwner != "main" || len(facts.OwnPending) != 0 {
		t.Fatalf("recorded Builder delivery confused with pending Main integration: %+v", facts)
	}
	for _, event := range []string{"Stop", "SubagentStop", "TeammateIdle"} {
		payload := map[string]any{"hook_event_name": event, "cwd": e.Path, "session_id": "owned-close"}
		if event == "Stop" {
			payload["session_id"] = e.PlatformSessionID
		} else if event == "SubagentStop" {
			payload["agent_id"] = e.AgentID
		} else {
			payload["teammate_name"], payload["team_name"] = e.AgentID, "workgroup-core-builder"
		}
		data, _ := json.Marshal(payload)
		var out, errOut bytes.Buffer
		before := stageSnapshot(t, f.root)
		if code := cli.Run([]string{"hook", "--event", event, "--root", f.root}, bytes.NewReader(data), &out, &errOut); code != 0 {
			t.Fatalf("%s reawakened delivered Builder for Main integration: code=%d %s", event, code, errOut.String())
		}
		if event == "Stop" && stageSnapshot(t, f.root).Revision != before.Revision {
			t.Fatal("delivered Worker Stop ran Main's control cycle")
		}
	}
	integrated := stageCLI(t, "runtime", "task-integrate", "--root", f.root, "--assignment-id", e.AssignmentID, "--agent-id", e.AgentID)
	if !strings.Contains(integrated, "complete") {
		t.Fatalf("original integration did not complete: %s", integrated)
	}
	assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, true)
	assertCoreReceptionAdvice(t, f, e.AssignmentID, false)
	main, err := os.ReadFile(filepath.Join(f.root, "src/value.go"))
	if err != nil || !bytes.Contains(main, []byte("return 2")) {
		t.Fatal("actual product change was not integrated")
	}
}

func assertCoreReceptionAdvice(t *testing.T, f stageAnswerFixture, assignmentID string, pending bool) {
	t.Helper()
	before := stageSnapshot(t, f.root)
	output := stageCLI(t, "s6", "status", "--root", f.root, "--advice", "--capacity", "1", "--max-unreceived-results", "1", "--json")
	var result struct {
		Advice struct {
			Action struct {
				Kind    string   `json:"action_kind"`
				Subject string   `json:"subject"`
				Argv    []string `json:"argv"`
			} `json:"current_action"`
			Unreceived int  `json:"unreceived"`
			Budget     *int `json:"dispatch_budget"`
		} `json:"reception_advice"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("reception advice changed original Runtime")
	}
	if pending {
		if result.Advice.Unreceived != 1 || result.Advice.Action.Subject != assignmentID || result.Advice.Action.Kind != "receive_integration" || result.Advice.Budget == nil || *result.Advice.Budget != 0 || len(result.Advice.Action.Argv) == 0 {
			t.Fatalf("actual report not prioritized: %s", output)
		}
		original := stageCLI(t, "runtime", "workspace", "pending", "--root", f.root)
		var rows []struct {
			Command []string `json:"command_argv"`
		}
		if err := json.Unmarshal([]byte(original), &rows); err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].Command, result.Advice.Action.Argv) {
			t.Fatalf("advice changed original reception entrance: %s %s", output, original)
		}
	} else if result.Advice.Unreceived != 0 || result.Advice.Action.Subject == assignmentID || result.Advice.Budget == nil || *result.Advice.Budget != 1 {
		t.Fatalf("actual verified reception did not release advice budget: %s", output)
	}
}

func TestWorkspaceBackendCoreDefaultShadowAndReplacementStayFixed(t *testing.T) {
	t.Run("default remains shadow", func(t *testing.T) {
		f := coreBuilderFixture(t)
		e := prepareCoreBuilder(t, f, "")
		main := mustReadCoreFile(t, f.root, ".claude/agents/backend-builder.md")
		worker := mustReadCoreFile(t, e.Path, ".claude/agents/backend-builder.md")
		if !bytes.Equal(main, worker) {
			t.Fatal("default silently reduced preloads")
		}
		before := stageSnapshot(t, f.root).Revision
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--context-profile", "backend-core"}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before {
			t.Fatal("existing shadow execution was retrofitted")
		}
	})
	t.Run("explicit replacement retains generated role", func(t *testing.T) {
		f := coreBuilderFixture(t)
		old := prepareCoreBuilder(t, f, "backend-core")
		registry, _ := workspace.Decode(stageSnapshot(t, f.root).State)
		first, err := registry.ReadDispatchContext(old)
		if err != nil {
			t.Fatal(err)
		}
		oldRole := mustReadCoreFile(t, old.Path, first.GeneratedRole.Target)
		stageCLI(t, "runtime", "workspace", "replace", "--root", f.root, "--assignment", old.AssignmentID, "--agent", old.AgentID, "--execution-generation", "1", "--reason", "Recover the same undelivered Builder with its fixed profile")
		registry, _ = workspace.Decode(stageSnapshot(t, f.root).State)
		next := registry.Executions[old.AssignmentID]
		c, err := registry.ReadDispatchContext(next)
		if err != nil {
			t.Fatal(err)
		}
		if next.Generation != 2 || c.Selection.Mode != "enabled" || !reflect.DeepEqual(c.GeneratedRole, first.GeneratedRole) || !reflect.DeepEqual(c.Selection, first.Selection) {
			t.Fatal("replacement silently changed profile")
		}
		if !bytes.Equal(oldRole, mustReadCoreFile(t, next.Path, c.GeneratedRole.Target)) || !bytes.Equal(oldRole, mustReadCoreFile(t, old.Path, c.GeneratedRole.Target)) {
			t.Fatal("replacement changed generated bytes or old tree")
		}
		before := stageSnapshot(t, f.root).Revision
		original := mustReadCoreFile(t, f.root, c.GeneratedRole.Source)
		os.WriteFile(filepath.Join(f.root, c.GeneratedRole.Source), []byte("changed fixed generated source"), 0600)
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", next.AssignmentID, "--agent", next.AgentID}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before {
			t.Fatal("generated source drift rewrote the fixed execution")
		}
		if !bytes.Equal(oldRole, mustReadCoreFile(t, next.Path, c.GeneratedRole.Target)) {
			t.Fatal("source drift overwrote Worker assets")
		}
		os.WriteFile(filepath.Join(f.root, c.GeneratedRole.Source), original, 0600)
	})
}

func TestWorkspaceBackendCoreRejectsWrongStageRoleBeforeRuntimeMutation(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root).Revision
	var out, errout bytes.Buffer
	p := f.packets[0]
	if code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner, "--context-profile", "backend-core"}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before {
		t.Fatal("S5 reviewer got an enabled Builder profile")
	}
}
