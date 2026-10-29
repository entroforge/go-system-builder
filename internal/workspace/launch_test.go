package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func TestWorkerSubmissionPathProjectionRequiresAnActualPlanDraft(t *testing.T) {
	paths := newWorkerSubmissionPaths()
	questionnaire := DispatchContext{AnswerPath: AnswerDraftPath}
	projected := workerSubmissionPathProjection(questionnaire, paths)
	if _, exists := projected["plan"]; exists {
		t.Fatalf("questionnaire-only dispatch exposed a generic PLAN path: %#v", projected)
	}
	if projected["answer"] != AnswerDraftPath {
		t.Fatalf("questionnaire-only dispatch omitted its exact answer path: %#v", projected)
	}

	builder := DispatchContext{PlanDraft: map[string]any{"objective": nil}, ResultDraft: map[string]any{"summary": nil}, ProductWritePaths: []string{"src/**"}}
	projected = workerSubmissionPathProjection(builder, paths)
	if projected["plan"] != PlanDraftPath || projected["completion"] != ResultDraftPath || projected["commit"] != CommitRequestPath {
		t.Fatalf("S6 Builder lost its original lifecycle submission paths: %#v", projected)
	}
}

func TestLaunchUsesOnlyChildDirectoryAndRegisteredSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses POSIX shell")
	}
	b, e, state, ctx := workerFixture(t)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/skills"), 0700)
	roles := filepath.Join(b.MainRoot, ".claude/agents")
	os.MkdirAll(roles, 0700)
	os.WriteFile(filepath.Join(roles, "backend-builder.md"), []byte("---\nname: backend-builder\ndescription: Test worker\n---\nImplement assignment."), 0600)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Bootstrap(e, exe); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.PlatformSessionID, err = NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	state["workspace"] = Encode(b)
	saveControlState(t, b.MainRoot, state)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\npwd\nprintf '%s\\n' \"$@\"\nprintf 'parent=%s\\n' \"${CLAUDECODE-unset}\"\n"), 0700)
	t.Setenv("CLAUDECODE", "parent")
	before, _ := os.Getwd()
	mainHead, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	for _, resume := range []bool{false, true} {
		var out bytes.Buffer
		if err = b.Launch(ctx, e, fake, "backend-builder", "implement", resume, &out, &out); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		flag := "--session-id"
		if resume {
			flag = "--resume"
		}
		if !strings.HasPrefix(text, e.Path+"\n") || !strings.Contains(text, flag+"\n"+e.PlatformSessionID) || !strings.Contains(text, "parent=unset") || strings.Contains(text, "skip-permissions") {
			t.Fatalf("bad launch: %s", text)
		}
		if !strings.Contains(text, builderSubmissionOrder) {
			t.Fatal("Builder launch omitted commit-before-report order")
		}
	}
	after, _ := os.Getwd()
	head, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	if after != before || head != mainHead {
		t.Fatal("Main moved")
	}
	owner, err := SessionOwner(ctx, b.MainRoot, e.Path, e.PlatformSessionID)
	if err != nil || owner != e.AgentID {
		t.Fatalf("session owner=%q %v", owner, err)
	}
	if _, err = SessionOwner(ctx, b.MainRoot, e.Path, "unregistered"); err == nil {
		t.Fatal("unknown Worker session accepted")
	}
	owner, err = SessionOwner(ctx, b.MainRoot, b.MainRoot, e.PlatformSessionID)
	if err != nil || owner != "" {
		t.Fatalf("Main impersonated Worker: %q %v", owner, err)
	}
}

func TestS9LaunchUsesCurrentProfessionalQuestionWithoutGenericChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses POSIX shell")
	}
	for _, scenario := range []struct {
		name, phase      string
		indexed, current bool
	}{
		{"indexed_question", "fixing", true, true},
		{"missing_question", "fixing", false, false},
		{"later_responsibility", "targeted_reverification", true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			b, ctx := repository(t)
			state := map[string]any{
				"bound_req":  map[string]any{"workspace": Binding{b.MainRoot, b.Branch, "main", b.BoundHead}.Map()},
				"runtime_id": "loop-test", "revision": 0, "baseline": map[string]any{"generation": 1},
				"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
				"lifecycle":     map[string]any{"state": "bug_resolution", "phase": scenario.phase},
			}
			saveControlState(t, b.MainRoot, state)
			for _, skill := range agentcontext.LegacySkills("backend-builder") {
				path := filepath.Join(b.MainRoot, ".claude/skills", skill, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("# Installed test skill\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			roles := filepath.Join(b.MainRoot, ".claude/agents")
			if err := os.MkdirAll(roles, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(roles, "backend-builder.md"), []byte("---\nname: backend-builder\ndescription: Test worker\n---\nPerform the supplied professional work."), 0o600); err != nil {
				t.Fatal(err)
			}
			e, err := b.Plan(ctx, state, "assignment", "builder", []string{"input.md"}, []string{"test -f input.md"}, []string{"input.md"})
			if err != nil {
				t.Fatal(err)
			}
			dispatch := Dispatch{AssignmentID: e.AssignmentID, Owner: e.AgentID, TaskID: "TASK-test", TeamID: "team-test", Role: "backend-builder", AgentDefinitionRef: ".claude/agents/backend-builder.md", DispatchMode: "plan_checkpoint"}
			e, err = b.FreezeDispatchContext(e, dispatch, nil)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := b.ReadDispatchContext(e)
			if err != nil || frozen.PlanDraft != nil || frozen.PlanQuestions != nil || frozen.PlanPath != "" || frozen.ResultDraft != nil {
				t.Fatalf("S9 generated a generic draft before locating its question: %+v %v", frozen, err)
			}
			b.Executions[e.AssignmentID] = e
			if err := b.Materialize(ctx, e); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Bootstrap(e, executable); err != nil {
				t.Fatal(err)
			}
			e.BootstrapSHA256, err = BootstrapDigest(e)
			if err != nil {
				t.Fatal(err)
			}
			e.PlatformSessionID, err = NewSessionID()
			if err != nil {
				t.Fatal(err)
			}
			e.Status = "ready"
			b.Executions[e.AssignmentID] = e
			state["workspace"] = Encode(b)
			view, answer := ".claude/review/result-view.json", ".claude/review/result-answer.json"
			if scenario.indexed {
				// Existing packet index facts only; this fixture does not submit
				// a professional result or fabricate its acceptance.
				state["entities"] = map[string]any{"work_packets": []any{map[string]any{
					"template_id": "s9-repair-result-v1", "view_path": view, "answer_path": answer,
					"binding": map[string]any{"sources": map[string]any{"manifest_assignment_id": e.AssignmentID, "owner_agent_id": e.AgentID}},
				}}}
				for _, path := range []string{view, answer} {
					full := filepath.Join(e.Path, filepath.FromSlash(path))
					if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(full, []byte("{}\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			saveControlState(t, b.MainRoot, state)
			fake := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := b.Launch(ctx, e, fake, "backend-builder", "perform current S9 work", false, &out, &out); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			for _, old := range []string{"begin_command", "report_command", "commit_command", "s9_delivery_command", "plan_draft_path", "\"completion\"", "before begin", "activation flow", "only the exact lifecycle", "only the exact registered adapters"} {
				if strings.Contains(text, old) {
					t.Fatalf("S9 launch retained %q: %s", old, text)
				}
			}
			if !strings.Contains(text, "Current S9 professional work") || !strings.Contains(text, "Personally fill") || !strings.Contains(text, "real check results") {
				t.Fatalf("S9 launch lost professional save/check instructions: %s", text)
			}
			for _, required := range []string{"ordinary git add and git commit", "No-change tests require no new commit", "No extra commit form or generic completion report is required", "Main owns real integration, necessary checks"} {
				if !strings.Contains(text, required) {
					t.Fatalf("S9 launch lost %q guidance: %s", required, text)
				}
			}
			if scenario.current {
				if !strings.Contains(text, filepath.Join(e.Path, view)) || !strings.Contains(text, filepath.Join(e.Path, answer)) {
					t.Fatalf("S9 launch lost exact paths: %s", text)
				}
			} else if !strings.Contains(text, "Main/system preparation gap") || strings.Contains(text, filepath.Join(e.Path, answer)) {
				t.Fatalf("S9 missing/current responsibility received a generic or old question: %s", text)
			}
		})
	}
}
