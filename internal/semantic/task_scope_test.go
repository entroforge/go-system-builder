package semantic

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func TestScopePathsReachDispatchAndS5WithoutNotesOrOtherRows(t *testing.T) {
	for _, cell := range []string{
		"simulation/products/, tests/a_test.go",
		"`simulation/products/`, `tests/a_test.go`",
		"`simulation/products/`, `tests/a_test.go`（SIMULATION 占位区；本演练不写产品源码）",
		"`simulation/products/`, `tests/a_test.go` (simulation output area)",
	} {
		t.Run(cell, func(t *testing.T) {
			root := dispatchFixture(t)
			mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-01.md", "| prospective write paths | `packages/validation` |", "| read paths | `outside/read/` |\n| output paths | `outside/output/` |\n| forbidden write paths | `outside/forbidden/` |\n| prospective write paths | "+cell+" |")
			p, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil || len(p.Problems) != 0 {
				t.Fatalf("dispatch: %+v, %v", p, err)
			}
			want := []string{"simulation/products/", "tests/a_test.go"}
			if !reflect.DeepEqual(p.Tasks[0].Writes, want) {
				t.Fatalf("dispatch scope = %q", p.Tasks[0].Writes)
			}
			checked, err := TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil || len(checked.Problems) != 0 {
				t.Fatalf("S4: %+v, %v", checked, err)
			}
			for _, context := range checked.TaskContexts {
				if context.TaskID == "TASK-042-01" && !reflect.DeepEqual(context.Writes, want) {
					t.Fatalf("S5 scope = %q", context.Writes)
				}
			}
		})
	}
}

func TestScopeRejectsAmbiguousInputBeforeFreezeWithOriginalLocation(t *testing.T) {
	for _, cell := range []string{
		"`simulation/products/` trailing garbage", "`a/`, b/", "a/, `b/`", "`a/` (note), `b/`",
		"`a/`（note", "`a/`（note） garbage", "`a/` (note）", "`a/` (note) (again)",
		"`a/", "`{path}`", "`../outside/`", "`/absolute/`", "`a/*.go`", "a/ b/", "a/,", "`a/`,", "`a|b/`", "`.`", "`a?b`", "`a/`(note(with nested parens))",
	} {
		t.Run(cell, func(t *testing.T) {
			root := dispatchFixture(t)
			ref := "docs/dev/tasks/TASK-042-01.md"
			mutatePlanFile(t, root, ref, "`packages/validation`", cell)
			content, err := os.ReadFile(filepath.Join(root, ref))
			if err != nil {
				t.Fatal(err)
			}
			line := strings.Count(strings.Split(string(content), "| prospective write paths")[0], "\n") + 1
			result, err := TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil {
				t.Fatal(err)
			}
			location := ref + ":" + strconv.Itoa(line) + ": Scope prospective write paths"
			if !strings.Contains(strings.Join(result.Problems, "\n"), location) {
				t.Fatalf("missing %q in %v", location, result.Problems)
			}
			plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil || len(plan.Problems) == 0 || len(plan.Tasks[0].Writes) != 0 {
				t.Fatalf("invalid scope usable: %+v %v", plan, err)
			}
		})
	}
}
