package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS6DispatchCLIUsesReviewedGitInputs(t *testing.T) {
	root := s6Fixture(t)
	source := "../../docs/examples/dispatch-plan/project"
	if err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0644)
	}); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	git("init", "-b", "dev")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("add", "docs")
	git("commit", "-m", "plan")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	b, _ := os.ReadFile(statePath)
	state := map[string]any{}
	json.Unmarshal(b, &state)
	state["bound_req"] = map[string]any{"id": "REQ-042", "workspace": map[string]any{"dev_branch": "dev"}}
	p, e := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil {
		t.Fatal(e)
	}
	docs := []any{}
	add := func(id, kind, path string) {
		b, _ := os.ReadFile(filepath.Join(root, path))
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "version": "1", "status": "complete", "sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "generation": 1})
	}
	add("dispatch-plan:REQ-042", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		add(task.ID, "task", task.Path)
	}
	state["documents"] = docs
	state["evidence"] = []any{}
	b, _ = json.Marshal(state)
	os.WriteFile(statePath, b, 0644)
	// The parent receives actual candidates in additionalContext, not terminal-only systemMessage.
	guidance := cli.BuildGuidanceForState(root, state, "SessionStart", policy.Input{})
	wire, exitCode, wireErr := hook.RenderWithAdditionalContext(root, "SessionStart", policy.Decision{Decision: "allow", Guidance: &guidance}, policy.RuntimeContext{}, "")
	if wireErr != nil || exitCode != 0 {
		t.Fatalf("wire: %d %v", exitCode, wireErr)
	}
	var packet struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if e := json.Unmarshal(wire, &packet); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"S6 current Main action: dispatch_ready", "eligible tasks", "TASK-042-01", "TASK-042-02", "TASK-042-03"} {
		if !strings.Contains(packet.HookSpecificOutput.AdditionalContext, want) {
			t.Fatalf("missing Agent context %s: %s", want, wire)
		}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"s6", "status", "--root", root, "--capacity", "3", "--json"}
	if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	var board struct {
		Next []string `json:"next"`
	}
	if e = json.Unmarshal(stdout.Bytes(), &board); e != nil || len(board.Next) != 3 {
		t.Fatalf("%s %v", stdout.String(), e)
	}
	before, _ := os.ReadFile(statePath)
	journalBefore, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	for _, tc := range []struct {
		name              string
		flags             []string
		budget            *int
		next, recommended int
	}{
		{"unknown capacity", nil, nil, 0, 0},
		{"explicit zero", []string{"--capacity", "0"}, intPointer(0), 0, 0},
		{"declared capacity", []string{"--capacity", "3"}, intPointer(3), 3, 3},
		{"advisory ceiling", []string{"--capacity", "3", "--max-unreceived-results", "1"}, intPointer(1), 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			adviceArgs := append([]string{"s6", "status", "--root", root, "--advice", "--json"}, tc.flags...)
			if code := cli.Run(adviceArgs, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("%d %s", code, stderr.String())
			}
			var result struct {
				Next   []string `json:"next"`
				Advice struct {
					Budget      *int     `json:"dispatch_budget"`
					Recommended []string `json:"recommended_tasks"`
					Advisory    bool     `json:"advisory_only"`
				} `json:"reception_advice"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if (result.Advice.Budget == nil) != (tc.budget == nil) || (tc.budget != nil && *result.Advice.Budget != *tc.budget) || len(result.Next) != tc.next || len(result.Advice.Recommended) != tc.recommended || !result.Advice.Advisory {
				t.Fatalf("wrong capacity or authority: %s", stdout.String())
			}
			if tc.budget == nil && (strings.Contains(stdout.String(), "capacity exhausted") || strings.Contains(stdout.String(), "declare_capacity")) {
				t.Fatalf("unknown capacity became a dispatch prerequisite: %s", &stdout)
			}
			stdout.Reset()
			textArgs := append([]string{"s6", "status", "--root", root, "--advice"}, tc.flags...)
			if code := cli.Run(textArgs, nil, &stdout, &stderr); code != 0 {
				t.Fatalf("text status: %d %s", code, &stderr)
			}
			if tc.budget == nil {
				for _, want := range []string{"total capacity unknown", "Next batch: not calculated", "original s6 prepare readiness checks", "not a registration, permission or prepare prerequisite"} {
					if !strings.Contains(stdout.String(), want) {
						t.Fatalf("missing unknown capacity guidance %q: %s", want, &stdout)
					}
				}
				if strings.Contains(stdout.String(), "capacity exhausted") || strings.Contains(stdout.String(), "total capacity 0") {
					t.Fatalf("unknown capacity reported as zero: %s", &stdout)
				}
			} else if tc.name == "explicit zero" && (!strings.Contains(stdout.String(), "total capacity 0") || !strings.Contains(stdout.String(), "available capacity exhausted")) {
				t.Fatalf("explicit zero capacity lost original slot semantics: %s", &stdout)
			}
		})
	}
	afterAdvice, _ := os.ReadFile(statePath)
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(before, afterAdvice) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("advice mutated original runtime/journal")
	}
	// A dirty copy must not alter the approved Git input or write runtime state.
	path := filepath.Join(root, p.Path)
	os.WriteFile(path, []byte("dirty uncommitted plan"), 0644)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("dirty disk replaced committed plan: %s", stderr.String())
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("status mutated runtime")
	}
}

func intPointer(value int) *int { return &value }
