package submission

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func planningIssueFixture(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	stateBytes, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["runtime_id"] = "loop-planning-publication"
	state["revision"] = 0
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 3}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["bound_req"] = map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "status": "locked", "approved_by": "user", "approved_at": "2026-06-18T00:00:00Z", "metadata": map[string]any{"ui_impact": "none"}, "workspace": map[string]any{"project_root": root, "dev_branch": "test-development", "release_upstream": "main"}}
	state["documents"] = []any{map[string]any{"id": "REQ-001", "kind": "req", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "status": "locked", "generation": 1}, map[string]any{"id": "ARCHITECTURE-001", "kind": "design", "path": "docs/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "status": "locked", "generation": 1}}
	state["entities"].(map[string]any)["work_packets"] = []any{}
	state["entities"].(map[string]any)["submissions"] = []any{}
	state["evidence"] = []any{}
	write("docs/requirements/REQ-001.md", []byte("# REQ-001\n> Status: locked\n> Version: v1.0.0\n"))
	write("docs/architecture/ARCHITECTURE-001.md", []byte("# Architecture\n> Status: locked\n> Version: v1.0.0\n"))
	if err := os.MkdirAll(filepath.Join(root, "docs/design"), 0700); err != nil {
		t.Fatal(err)
	}
	def, err := os.ReadFile("../../docs/control/loop-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	write("docs/control/loop-definition.json", def)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	write(".claude/loop-state.json", data)
	write(".claude/loop-events.jsonl", nil)
	return &Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Files: fileview.Disk{Root: root}, Validator: semantic.RuntimeCandidateValidator{}}
}

func TestContractIssueProtectsOccupiedPathsAndReplaysOriginal(t *testing.T) {
	s := planningIssueFixture(t)
	// Use a first issuance to obtain the original deterministic paths, then run
	// a separate fresh Runtime with those paths occupied by unrelated files.
	first, err := s.IssueContracts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other := planningIssueFixture(t)
	for _, path := range []string{first.ViewPath, first.AnswerPath} {
		full := filepath.Join(other.Root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("unknown owner"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := other.IssueContracts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ViewPath == first.ViewPath || got.AnswerPath == first.AnswerPath {
		t.Fatal("claimed an unregistered path")
	}
	for _, path := range []string{first.ViewPath, first.AnswerPath} {
		data, err := os.ReadFile(filepath.Join(other.Root, path))
		if err != nil || string(data) != "unknown owner" {
			t.Fatalf("occupied path changed: %s %v", path, err)
		}
	}
	answer := filepath.Join(other.Root, got.AnswerPath)
	if err := os.WriteFile(answer, []byte("author editing"), 0600); err != nil {
		t.Fatal(err)
	}
	replay, err := other.IssueContracts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if replay.ViewPath != got.ViewPath || replay.AnswerPath != got.AnswerPath {
		t.Fatal("replay lost registered paths")
	}
	data, err := os.ReadFile(answer)
	if err != nil || string(data) != "author editing" {
		t.Fatalf("replay changed author answer: %v", err)
	}
}

func TestPlanningPublicationConcurrentExclusiveCreation(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	type result struct {
		view, answer string
		err          error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, a, e := publishPlanningFiles(root, false, ".claude/views/q.json", ".claude/answers/a.json", 1024, func(v, a string) (any, any) {
				return map[string]string{"view": v, "answer": a}, map[string]string{"answer": "draft"}
			})
			results <- result{v, a, e}
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if seen[r.view] || seen[r.answer] {
			t.Fatal("concurrent writers shared ownership")
		}
		seen[r.view] = true
		seen[r.answer] = true
	}
}

func TestContractIssueConcurrentReturnsRegisteredResponsibility(t *testing.T) {
	s := planningIssueFixture(t)
	type result struct {
		view, answer string
		err          error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, err := s.IssueContracts(context.Background())
			results <- result{p.ViewPath, p.AnswerPath, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var first result
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if first.view == "" {
			first = r
		} else if first.view != r.view || first.answer != r.answer {
			t.Fatalf("different registered responsibilities: %+v %+v", first, r)
		}
	}
}

func TestTaskPlanningIssueReplaysRegisteredOriginal(t *testing.T) {
	s := planningIssueFixture(t)
	raw, err := os.ReadFile(s.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.StatePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	binding, err := loopruntime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildTaskPlanning(binding, workpacket.TaskPlanningInputs{Contracts: []workpacket.Reference{{ID: "CONTRACTS-001", Path: "docs/dev/contracts/CONTRACTS-001.md", Version: "v1"}, {ID: "BE-001", Path: "docs/dev/contracts/BE-001.md", Version: "v1"}}, ClauseUniverse: []workpacket.TaskClause{{ContractID: "BE-001", Section: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loopruntime.RecordTaskPlanningPacket(s.Root, s.StatePath, s.JournalPath, p, s.Files, s.Validator); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(s.Root, p.AnswerPath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, p.AnswerPath), []byte("author editing"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.IssueTaskPlanning(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ViewPath != p.ViewPath || got.AnswerPath != p.AnswerPath {
		t.Fatal("lost registered S4 paths")
	}
	data, err := os.ReadFile(filepath.Join(s.Root, p.AnswerPath))
	if err != nil || string(data) != "author editing" {
		t.Fatalf("changed S4 answer: %v", err)
	}
}

func TestPlanningPublicationPreservesOccupiedFileDirectoryAndSymlink(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "parent-file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			view := ".claude/work-packets/source/view.json"
			occupied := filepath.Join(root, view)
			if kind == "parent-file" {
				occupied = filepath.Dir(occupied)
			}
			if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file", "parent-file":
				if err := os.WriteFile(occupied, []byte("unknown"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(occupied, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("unrelated", occupied); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(occupied)
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := publishPlanningFiles(root, false, view, ".claude/submissions/source/Main/answer.json", 1024, func(v, a string) (any, any) {
				return map[string]string{"view": v, "answer": a}, map[string]string{"answer": "draft"}
			})
			if err != nil {
				t.Fatal(err)
			}
			if got == view {
				t.Fatal("claimed occupied path")
			}
			after, err := os.Lstat(occupied)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("occupied entry changed: %v", err)
			}
		})
	}
}
