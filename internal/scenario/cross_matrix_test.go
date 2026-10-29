package scenario_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

func writeCrossMatrix(t *testing.T, root, module string, matrix map[string]any) {
	t.Helper()
	writeJSON(t, filepath.Join(root, "docs/design/prototypes", module, "cross-matrix.json"), matrix)
}

// TestCrossMatrixRequiredAndValidated pins the carrier: a module package
// without cross-matrix.json is incomplete, and references must resolve.
func TestCrossMatrixRequiredAndValidated(t *testing.T) {
	// Exercise each shipped template entry unchanged in shape against real fixture values.
	template, err := os.ReadFile(filepath.Join("..", "..", "docs/design/prototypes/cross-matrix-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	template = []byte(strings.NewReplacer("example-module", "investor-workbench", "FACT-EXAMPLE", "fact-investor", "REQ-<id>/FR-<id>", "REQ-INV-001", "S-002", "S-001", "branch-<id>", "branch-allow").Replace(string(template)))
	var matrix map[string]any
	if err := json.Unmarshal(template, &matrix); err != nil {
		t.Fatal(err)
	}
	entries := matrix["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected the two shipped example entries, got %d", len(entries))
	}
	for i, entry := range entries {
		templateRoot := newScenarioRoot(t, "investor-workbench", "ordinary")
		// Entries are exercised alone: trim the model to the single branch
		// the entry covers (or drop the uncovered required branch for a
		// no_branch_reason entry) — otherwise the new branch-floor check
		// (correctly) rejects the half-covered matrix.
		singleModel := loadModel(t, templateRoot)
		branches := singleModel["rules"].([]any)[0].(map[string]any)["branches"].([]any)
		if want, ok := entry.(map[string]any)["branch"].(string); ok {
			kept := []any{}
			for _, b := range branches {
				if b.(map[string]any)["id"].(string) == want {
					kept = append(kept, b)
				}
			}
			if len(kept) != 1 {
				t.Fatalf("shipped template entry %d covers unknown branch %q", i, want)
			}
			singleModel["rules"].([]any)[0].(map[string]any)["branches"] = kept
		} else {
			// no_branch_reason entry: make the uncovered branch optional so
			// the floor does not fire for this single-cell exercise.
			for _, b := range branches {
				b.(map[string]any)["required"] = false
			}
		}
		writeModel(t, templateRoot, singleModel)
		matrix["entries"] = []any{entry}
		writeCrossMatrix(t, templateRoot, "investor-workbench", matrix)
		if _, err := scenario.GenerateModule(templateRoot, "investor-workbench"); err != nil {
			t.Fatalf("shipped template entry %d must generate with fixture references: %v", i, err)
		}
	}

	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if err := os.Remove(filepath.Join(root, "docs/design/prototypes", "investor-workbench", "cross-matrix.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "cross-matrix.json") {
		t.Fatalf("missing cross-matrix must fail generate, got %v", err)
	}

	// Unknown branch reference.
	root = newScenarioRoot(t, "investor-workbench", "ordinary")
	writeCrossMatrix(t, root, "investor-workbench", map[string]any{
		"module": "investor-workbench",
		"entries": []any{map[string]any{
			"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": "branch-nonexistent",
		}},
	})
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "unknown branch") {
		t.Fatalf("unknown branch reference must fail, got %v", err)
	}

	// Silence is not N/A: neither branch nor reason.
	root = newScenarioRoot(t, "investor-workbench", "ordinary")
	writeCrossMatrix(t, root, "investor-workbench", map[string]any{
		"module": "investor-workbench",
		"entries": []any{map[string]any{
			"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001",
		}},
	})
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !(strings.Contains(err.Error(), "silence is not N/A") || strings.Contains(err.Error(), "anyOf")) {
		t.Fatalf("cell without branch or reason must fail, got %v", err)
	}

	// A no-branch reason is a valid cell shape. The model branches are marked
	// optional here so the shape check is isolated from the branch floor
	// (which is pinned separately below).
	root = newScenarioRoot(t, "investor-workbench", "ordinary")
	optionalModel := loadModel(t, root)
	for _, b := range optionalModel["rules"].([]any)[0].(map[string]any)["branches"].([]any) {
		b.(map[string]any)["required"] = false
	}
	writeModel(t, root, optionalModel)
	writeCrossMatrix(t, root, "investor-workbench", map[string]any{
		"module": "investor-workbench",
		"entries": []any{map[string]any{
			"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "no_branch_reason": "covered by the same rule's negative branch",
		}},
	})
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatalf("reasoned no-branch cell must pass, got %v", err)
	}
}

// TestCaseIDPatternPinned pins the denominator format.
func TestCaseIDPatternPinned(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	model := loadModel(t, root)
	branch := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)
	branch["case_id"] = "case_lower"
	writeModel(t, root, model)
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !(strings.Contains(err.Error(), "verification denominator") || strings.Contains(err.Error(), "does not match pattern")) {
		t.Fatalf("lowercase case id must be rejected, got %v", err)
	}
}

// bindREQ writes a minimal runtime + REQ fixture so the matrix's REQ joins
// have a live denominator to check against.
func bindREQ(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs/requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	req := "# REQ-INV-001\n\n> 状态：locked\n\n| ID | 描述 | 指向 |\n|---|---|---|\n| FR-001 | screen investors | S-001 |\n"
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-INV-001.md"), []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"lifecycle": map[string]any{"state": "planning", "phase": "design"},
		"bound_req": map[string]any{"id": "REQ-INV-001", "path": "docs/requirements/REQ-INV-001.md"},
	}
	writeJSON(t, filepath.Join(root, ".claude/loop-state.json"), state)
}

// TestCrossMatrixJoinsBoundREQ pins the matrix↔model↔REQ joins: a cell may
// not run parallel to the AC↔CASE chain.
func TestCrossMatrixJoinsBoundREQ(t *testing.T) {
	cell := func(extra map[string]any) map[string]any {
		entry := map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001"}
		for k, v := range extra {
			entry[k] = v
		}
		return map[string]any{"module": "investor-workbench", "entries": []any{entry}}
	}

	cases := []struct {
		name    string
		matrix  map[string]any
		wantErr string
	}{
		{"undeclared FR", cell(map[string]any{"branch": "branch-allow", "req_ref": "REQ-INV-001/FR-999"}), "does not declare"},
		{"foreign REQ not cited by this branch", cell(map[string]any{"branch": "branch-allow", "req_ref": "REQ-OTHER/FR-001"}), "never cites"},
		{"branch rule never cites the cell", cell(map[string]any{"branch": "branch-allow", "req_ref": "REQ-INV-001/FR-001"}), "never cites"},
		{"trivial no-branch reason", cell(map[string]any{"no_branch_reason": "."}), "not a rationale"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			bindREQ(t, root)
			writeCrossMatrix(t, root, "investor-workbench", tc.matrix)
			_, err := scenario.GenerateModule(root, "investor-workbench")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("cited FR-level cell passes", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		bindREQ(t, root)
		model := loadModel(t, root)
		model["rules"].([]any)[0].(map[string]any)["source_refs"] = []any{"REQ-INV-001/FR-001"}
		writeModel(t, root, model)
		writeCrossMatrix(t, root, "investor-workbench", map[string]any{"module": "investor-workbench", "entries": []any{
			cell(map[string]any{"branch": "branch-allow", "req_ref": "REQ-INV-001/FR-001"})["entries"].([]any)[0],
			cell(map[string]any{"branch": "branch-reject", "req_ref": "REQ-INV-001/FR-001"})["entries"].([]any)[0],
		}})
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
			t.Fatalf("cell joining a citing rule must pass, got %v", err)
		}
	})
}

// TestCrossMatrixCompletenessFloor pins the hunt floor: silence is not coverage.
func TestCrossMatrixCompletenessFloor(t *testing.T) {
	t.Run("unhunted story", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		if err := os.WriteFile(filepath.Join(root, "docs/design/prototypes", "investor-workbench", "stories.md"), []byte("# Stories\n\n## S-001\n\n## S-002\n\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "never hunts story") {
			t.Fatalf("unhunted story must fail, got %v", err)
		}
	})
	t.Run("story id mid-heading is still floored", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		if err := os.WriteFile(filepath.Join(root, "docs/design/prototypes", "investor-workbench", "stories.md"), []byte("# Stories\n\n## S-001\n\n## 用户故事 S-002 备注\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "never hunts story") {
			t.Fatalf("mid-heading story must count toward the floor, got %v", err)
		}
	})
	t.Run("unhunted fact", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		model := loadModel(t, root)
		model["facts"] = append(model["facts"].([]any), map[string]any{
			"id": "fact-unhunted", "partitions": []any{map[string]any{"id": "any", "value": "any"}},
		})
		writeModel(t, root, model)
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "never hunts fact") {
			t.Fatalf("unhunted fact must fail, got %v", err)
		}
	})
}

// TestCrossMatrixRequiredBranchFloor pins the branch floor: every required
// branch must appear in at least one cell with a branch reference. A
// no_branch_reason cell covers the cell, not the branch.
func TestCrossMatrixRequiredBranchFloor(t *testing.T) {
	t.Run("unhunted required branch", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		writeCrossMatrix(t, root, "investor-workbench", map[string]any{"module": "investor-workbench", "entries": []any{
			map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": "branch-allow"},
		}})
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), `never hunts required branch "branch-reject"`) {
			t.Fatalf("unhunted required branch must fail, got %v", err)
		}
	})
	t.Run("optional branch needs no cell", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		optionalModel := loadModel(t, root)
		optionalModel["rules"].([]any)[0].(map[string]any)["branches"].([]any)[1].(map[string]any)["required"] = false
		writeModel(t, root, optionalModel)
		writeCrossMatrix(t, root, "investor-workbench", map[string]any{"module": "investor-workbench", "entries": []any{
			map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": "branch-allow"},
		}})
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
			t.Fatalf("optional branch without a cell must pass, got %v", err)
		}
	})
	t.Run("reason-only cell does not cover the branch", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		optionalModel := loadModel(t, root)
		for _, b := range optionalModel["rules"].([]any)[0].(map[string]any)["branches"].([]any) {
			b.(map[string]any)["required"] = false
		}
		optionalModel["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["required"] = true
		writeModel(t, root, optionalModel)
		writeCrossMatrix(t, root, "investor-workbench", map[string]any{"module": "investor-workbench", "entries": []any{
			map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "no_branch_reason": "covered elsewhere by an equivalent branch"},
		}})
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), `never hunts required branch "branch-allow"`) {
			t.Fatalf("reason-only cell must not cover a required branch, got %v", err)
		}
	})
}

func TestCrossMatrixSameCellDifferentBranches(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "different branches", true: "same branch"}[duplicate], func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			second := "branch-reject"
			if duplicate {
				second = "branch-allow"
			}
			writeCrossMatrix(t, root, "investor-workbench", map[string]any{"module": "investor-workbench", "entries": []any{
				map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": "branch-allow"},
				map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": second},
			}})
			_, err := scenario.GenerateModule(root, "investor-workbench")
			if duplicate {
				if err == nil || !strings.Contains(err.Error(), "duplicates") {
					t.Fatalf("want duplicate error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
