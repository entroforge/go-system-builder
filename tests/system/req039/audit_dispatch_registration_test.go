package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Seed a reviewed S6 projection. The separate Hook spine test covers creating
// that projection through S4/S5; these cases exercise the actual Writer entry.
func s4AuditRegistrationRoot(t *testing.T) string {
	t.Helper()
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 1)
	if err := os.RemoveAll(filepath.Join(root, "docs/dev/tasks")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repoRoot(t), "docs/examples/dispatch-plan/project/docs/dev/tasks")
	if err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, "docs/dev/tasks", rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, bytes.ReplaceAll(b, []byte("REQ-042"), []byte("REQ-039")), 0644)
	}); err != nil {
		t.Fatal(err)
	}
	p, err := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-039")
	if err != nil || len(p.Problems) > 0 {
		t.Fatalf("plan fixture: %v %+v", err, p)
	}
	docs := []any{}
	add := func(id, kind, path string) {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "version": "v1.0.0", "status": "locked", "generation": 1, "sha256": fmt.Sprintf("%x", sha256.Sum256(b))})
	}
	add("dispatch-plan:REQ-039", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		add(task.ID, "task", task.Path)
	}
	state["documents"] = docs
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	ms := state["milestone"].(map[string]any)
	ms["stage"], ms["lifecycle_state"], ms["lifecycle_phase"] = "S6", "building", nil
	writeSystemState(t, root, state)
	return root
}

func s4AuditManifest(t *testing.T, root, suffix, taskID, write, output string) string {
	t.Helper()
	taskPath := "docs/dev/tasks/" + taskID + ".md"
	b, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	aid, agent := "assignment-audit-"+suffix, "builder-audit-"+suffix
	m := map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-manifest-audit-" + suffix, "version": "v1.0.0",
		"runtime_id": "loop-system-test", "req_id": "REQ-039", "baseline_generation": 1, "review_round": nil,
		"platform_team_id": "platform-audit", "workgroup_id": "workgroup-audit-" + suffix, "workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": taskID, "path": taskPath, "version": "v1.0.0", "sha256": fmt.Sprintf("%x", sha256.Sum256(b))}},
		"risk_tags": []any{}, "responsibility_dispositions": []any{map[string]any{"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "bounded implementation", "assignment_ids": []string{aid}, "na_rationale": nil, "evidence_ref": taskPath}},
		"assignments":      []any{map[string]any{"assignment_id": aid, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder", "scope": []string{write}, "agent_id": agent, "agent_definition_ref": "agents/backend-builder.md", "skill_refs": []string{"code-quality"}, "read_paths": []string{taskPath}, "write_paths": []string{write}, "output_paths": []string{output}, "depends_on": []string{}, "reuse_decision": "create", "grouping_rationale": "one reviewed task", "status": "planned", "dispatch_mode": "plan_checkpoint"}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1, "quantity_rationale": "one task owner",
		"validation": map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-09-19T00:00:00Z"},
	}
	path := filepath.Join(root, ".claude", "manifest-audit-"+suffix+".json")
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func s4AuditRegister(t *testing.T, root, manifest, task string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runCLI(t, []string{"runtime", "register-workgroup", "--root", root, "--manifest", manifest, "--task-id", task, "--task", filepath.Join(root, "docs/dev/tasks", task+".md")}, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestS4AuditRegistrationChecksActualInputs(t *testing.T) {
	for _, kind := range []string{"normal", "dependency", "write-scope", "dirty-task", "duplicate-owner"} {
		t.Run(kind, func(t *testing.T) {
			root := s4AuditRegistrationRoot(t)
			task, write, output := "TASK-042-02", "web/pages/source", "web/pages/evidence/registration-result.json"
			if kind == "dependency" {
				task, write, output = "TASK-042-04", "web/features", "web/features/registration-result.json"
			}
			if kind == "write-scope" {
				write = "server/api"
			}
			manifest := s4AuditManifest(t, root, "first", task, write, output)
			if kind == "dirty-task" {
				p := filepath.Join(root, "docs/dev/tasks", task+".md")
				b, _ := os.ReadFile(p)
				if err := os.WriteFile(p, append(b, []byte("\nDIRTY\n")...), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "duplicate-owner" {
				if code, out, errOut := s4AuditRegister(t, root, manifest, task); code != 0 {
					t.Fatalf("positive register: %d %s %s", code, out, errOut)
				}
				manifest = s4AuditManifest(t, root, "second", task, write, output)
			}
			before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
			code, out, errOut := s4AuditRegister(t, root, manifest, task)
			if kind == "normal" {
				if code != 0 {
					t.Fatalf("positive register: %d %s %s", code, out, errOut)
				}
				return
			}
			want := map[string]string{"dependency": "awaits verified integration", "write-scope": "exceeds reviewed TASK", "dirty-task": "bytes differ", "duplicate-owner": "already has an owner"}[kind]
			if code == 0 || !strings.Contains(out+errOut, want) {
				t.Fatalf("expected %s rejection: %d %s %s", kind, code, out, errOut)
			}
			after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("rejected registration mutated Runtime")
			}
		})
	}
}

func TestS4AuditOutputPathsCannotExpandReviewedScope(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	manifest := s4AuditManifest(t, root, "output", "TASK-042-02", "web/pages", "server/api")
	before, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := s4AuditRegister(t, root, manifest, "TASK-042-02")
	if code == 0 || !strings.Contains(out+errOut, "exceeds reviewed TASK") || !strings.Contains(out+errOut, "TASK-042-02") {
		t.Fatalf("out-of-plan output path was not rejected: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	after, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected output scope expansion mutated Runtime")
	}
	activation := filepath.Join(root, ".claude/evidence/workgroup-audit-output/TASK-042-02/activation-builder-audit-output.json")
	if _, err := os.Stat(activation); !os.IsNotExist(err) {
		t.Fatalf("rejected output scope expansion left activation artifact: %v", err)
	}
}

func TestS4AuditEvidenceRootOutputCannotExpandReviewedScope(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	manifest := s4AuditManifest(t, root, "evidence-output", "TASK-042-02", "web/pages/source", ".claude/evidence/result.json")
	before, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := s4AuditRegister(t, root, manifest, "TASK-042-02")
	if code == 0 || !strings.Contains(out+errOut, "exceeds reviewed TASK") || !strings.Contains(out+errOut, "TASK-042-02") {
		t.Fatalf("unreviewed evidence output path was not rejected: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	after, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected evidence output scope expansion mutated Runtime")
	}
	activation := filepath.Join(root, ".claude/evidence/workgroup-audit-evidence-output/TASK-042-02/activation-builder-audit-evidence-output.json")
	if _, err := os.Stat(activation); !os.IsNotExist(err) {
		t.Fatalf("rejected evidence output scope expansion left activation artifact: %v", err)
	}
}

func TestS4AuditOutputPathsWithinReviewedScopeAreAccepted(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	output := "web/pages/evidence/result.json"
	write := "web/pages/source"
	manifest := s4AuditManifest(t, root, "output-accepted", "TASK-042-02", write, output)
	code, out, errOut := s4AuditRegister(t, root, manifest, "TASK-042-02")
	if code != 0 {
		t.Fatalf("in-scope output path must register: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	activation := filepath.Join(root, ".claude/evidence/workgroup-audit-output-accepted/TASK-042-02/activation-builder-audit-output-accepted.json")
	data, err := os.ReadFile(activation)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		AllowedWritePaths []string `json:"allowed_write_paths"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, path := range envelope.AllowedWritePaths {
		paths[path] = true
	}
	if !paths[write] || !paths[output] {
		t.Fatalf("activation omitted reviewed output path: %#v", envelope.AllowedWritePaths)
	}
}
