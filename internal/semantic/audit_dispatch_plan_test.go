package semantic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func TestAuditDispatchPlanResourcesHeadingLevels(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "TASK template third-level heading", path: "../../docs/dev/tasks/TASK-template.md"},
		{name: "checked-in task second-level heading", path: "../../docs/examples/dispatch-plan/project/docs/dev/tasks/TASK-042-01.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			rows := sectionTable(string(data), "Resources")
			if len(rows) != 1 || len(rows[0]) != 2 || rows[0][0] != "Resource" || rows[0][1] != "Purpose" {
				t.Fatalf("Resources table was not parsed from %s: %#v", tc.path, rows)
			}
		})
	}
}

// TestAuditDispatchPlanWavesV1Fixture exercises the actual template syntax,
// TASK membership closure, dependency extraction, and the combined task/
// resource graph on the checked-in multi-wave example.
func TestAuditDispatchPlanWavesV1Fixture(t *testing.T) {
	root := dispatchFixture(t)
	plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Problems) != 0 {
		t.Fatalf("checked-in waves-v1 fixture rejected: %#v", plan.Problems)
	}
	if plan.Path != "docs/dev/tasks/index-REQ-042.md" || plan.REQ != "REQ-042" || len(plan.Tasks) != 6 {
		t.Fatalf("unexpected parsed plan: path=%q req=%q tasks=%d", plan.Path, plan.REQ, len(plan.Tasks))
	}
	if plan.Tasks[0].Wave != 1 || plan.Tasks[3].Wave != 2 || plan.Tasks[5].Wave != 3 {
		t.Fatalf("unexpected wave extraction: %#v", plan.Tasks)
	}
	if got := strings.Join(plan.Tasks[3].Dependencies, ","); got != "TASK-042-01,TASK-042-02" {
		t.Fatalf("TASK-042-04 dependencies=%q", got)
	}
	result, err := TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 0 || result.Tasks != 6 || result.ClausesTotal != 1 || result.ClausesCovered != 1 {
		t.Fatalf("tasks check did not close fixture: %+v", result)
	}
}

// TestAuditDispatchPlanMembershipAndREQClosure verifies that support TASKs
// cannot be omitted and that a different REQ cannot be smuggled into the
// selected plan through a relative link.
func TestAuditDispatchPlanMembershipAndREQClosure(t *testing.T) {
	t.Run("support task omission", func(t *testing.T) {
		root := dispatchFixture(t)
		data, err := os.ReadFile(filepath.Join(root, "docs/dev/tasks/TASK-042-01.md"))
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), "TASK-042-01", "TASK-042-07"))
		if err := os.WriteFile(filepath.Join(root, "docs/dev/tasks/TASK-042-07.md"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if !containsDispatchProblem(plan, "TASK missing from dispatch plan") || !containsDispatchProblem(plan, "TASK-042-07") {
			t.Fatalf("omitted support task was not diagnosed: %#v", plan.Problems)
		}
	})

	t.Run("cross REQ link", func(t *testing.T) {
		root := dispatchFixture(t)
		data, err := os.ReadFile(filepath.Join(root, "docs/dev/tasks/TASK-042-02.md"))
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), "REQ-042", "REQ-099"))
		if err := os.WriteFile(filepath.Join(root, "docs/dev/tasks/TASK-099-02.md"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		mutatePlanFile(t, root, "docs/dev/tasks/index-REQ-042.md", "TASK-042-02.md", "TASK-099-02.md")
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if !containsDispatchProblem(plan, "unknown or cross-REQ TASK link") {
			t.Fatalf("cross-REQ task link was not rejected: %#v", plan.Problems)
		}
	})
}

func TestAuditDispatchPlanDAGAndResourceChecks(t *testing.T) {
	t.Run("resource order joins task cycle", func(t *testing.T) {
		root := dispatchFixture(t)
		mutatePlanFile(t, root, "docs/dev/tasks/index-REQ-042.md", "| --- | --- | --- | --- |", "| --- | --- | --- | --- |\n| TASK-042-04 | TASK-042-01 | shared port | reverse |")
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if !containsDispatchProblem(plan, "dispatch dependency/resource cycle") {
			t.Fatalf("joint task/resource cycle was not detected: %#v", plan.Problems)
		}
	})

	t.Run("malformed and missing task dependency", func(t *testing.T) {
		root := dispatchFixture(t)
		mutateTaskDependencies(t, root, "TASK-042-01.md", "NOT-A-TASK")
		result, err := TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(result.Problems, "\n"), "not machine-tracked") {
			t.Fatalf("malformed dependency was silently discarded: %#v", result.Problems)
		}

		root = dispatchFixture(t)
		mutateTaskDependencies(t, root, "TASK-042-01.md", "TASK-042-99")
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if !containsDispatchProblem(plan, "missing predecessor TASK-042-99") {
			t.Fatalf("missing dependency target was not rejected: %#v", plan.Problems)
		}
	})
}

func TestAuditDispatchPlanScopeAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		old  string
		new  string
		want string
	}{
		{name: "absolute write path", path: "docs/dev/tasks/TASK-042-01.md", old: "packages/validation", new: "/tmp/escape", want: "concrete repository-relative write paths"},
		{name: "parent write path", path: "docs/dev/tasks/TASK-042-01.md", old: "packages/validation", new: "../escape", want: "concrete repository-relative write paths"},
		{name: "glob write path", path: "docs/dev/tasks/TASK-042-01.md", old: "packages/validation", new: "packages/*", want: "concrete repository-relative write paths"},
		{name: "draft plan", path: "docs/dev/tasks/index-REQ-042.md", old: "> Status: complete", new: "> Status: draft", want: "Status must be complete"},
		{name: "zero revision", path: "docs/dev/tasks/index-REQ-042.md", old: "> Revision: 1", new: "> Revision: 0", want: "Revision must be a positive integer"},
		{name: "checked plan item", path: "docs/dev/tasks/index-REQ-042.md", old: "- [ ] [共享库]", new: "- [x] [共享库]", want: "unchecked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := dispatchFixture(t)
			mutatePlanFile(t, root, tc.path, tc.old, tc.new)
			plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil {
				t.Fatal(err)
			}
			if !containsDispatchProblem(plan, tc.want) {
				t.Fatalf("expected %q, got problems=%#v warnings=%#v", tc.want, plan.Problems, plan.Warnings)
			}
		})
	}

	t.Run("same-wave overlap is a warning", func(t *testing.T) {
		root := dispatchFixture(t)
		mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-02.md", "web/pages", "packages/validation")
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Problems) != 0 || !containsDispatchWarning(plan, "same-wave write/resource overlap") {
			t.Fatalf("same-wave overlap should be review warning: problems=%#v warnings=%#v", plan.Problems, plan.Warnings)
		}
	})

	t.Run("same-wave resource overlap is extracted", func(t *testing.T) {
		root := dispatchFixture(t)
		row := "| Resource | Purpose |\n| --- | --- |"
		withResource := row + "\n| resource:test-db | shared test database |"
		mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-01.md", row, withResource)
		mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-02.md", row, withResource)
		plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Problems) != 0 || !containsDispatchWarning(plan, "same-wave write/resource overlap") {
			t.Fatalf("same-wave resource overlap was not surfaced: problems=%#v warnings=%#v", plan.Problems, plan.Warnings)
		}
		if len(plan.Tasks) < 2 || strings.Join(plan.Tasks[0].Resources, ",") != "resource:test-db" || strings.Join(plan.Tasks[1].Resources, ",") != "resource:test-db" {
			t.Fatalf("legal resource declarations were not retained: tasks=%#v", plan.Tasks)
		}
	})
}

func TestAuditDispatchPlanNextDispatchFacts(t *testing.T) {
	root := dispatchFixture(t)
	plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if err != nil || len(plan.Problems) != 0 {
		t.Fatalf("fixture not dispatchable: plan=%+v err=%v", plan, err)
	}
	facts := map[string]DispatchFact{
		"TASK-042-01": {State: "integrated"},
		"TASK-042-02": {State: "integrated"},
		"TASK-042-03": {State: "running"},
	}
	rows, next := NextDispatch(plan, facts, 1)
	if strings.Join(next, ",") != "TASK-042-04" {
		t.Fatalf("ready successor was not released independently of W1: next=%v rows=%v", next, rows)
	}
	facts["TASK-042-01"] = DispatchFact{State: "reported"}
	_, next = NextDispatch(plan, facts, 3)
	if len(next) != 0 {
		t.Fatalf("reported predecessor incorrectly released its consumer: %v", next)
	}
}

func containsDispatchProblem(plan *DispatchPlan, needle string) bool {
	return strings.Contains(strings.Join(plan.Problems, "\n"), needle)
}

func containsDispatchWarning(plan *DispatchPlan, needle string) bool {
	return strings.Contains(strings.Join(plan.Warnings, "\n"), needle)
}

func mutateTaskDependencies(t *testing.T, root, task, dependency string) {
	t.Helper()
	path := filepath.Join(root, "docs/dev/tasks", task)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	marker := "## Resources"
	idx := strings.Index(content, marker)
	if idx < 0 {
		t.Fatalf("%s has no Resources section", task)
	}
	insert := "| " + dependency + " | required |\n\n"
	content = content[:idx] + insert + content[idx:]
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAuditDispatchPlanMalformedResourceOrderProbe is a normal acceptance
// check: a non-empty resource-order row must be diagnosed when its TASK
// endpoints are malformed.
func TestAuditDispatchPlanMalformedResourceOrderProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  string
	}{
		{name: "non-TASK before", row: "| malformed-before | TASK-042-01 | resource:test-db | missing task id |"},
		{name: "short row", row: "| TASK-042-04 | TASK-042-01 | resource:test-db |"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := dispatchFixture(t)
			mutatePlanFile(t, root, "docs/dev/tasks/index-REQ-042.md", "| --- | --- | --- | --- |", "| --- | --- | --- | --- |\n"+tc.row)
			plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
			if err != nil {
				t.Fatal(err)
			}
			if !containsDispatchProblem(plan, "invalid resource order") {
				t.Fatalf("malformed resource-order row was silently ignored: %#v", plan.Problems)
			}
		})
	}
}

// TestAuditDispatchPlanMalformedTaskResourceProbe is a normal acceptance
// check: non-empty TASK resource rows must use resource:<stable-name>.
func TestAuditDispatchPlanMalformedTaskResourceProbe(t *testing.T) {
	root := dispatchFixture(t)
	row := "| Resource | Purpose |\n| --- | --- |"
	bad := row + "\n| shared-test-db | mutable database |"
	mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-01.md", row, bad)
	mutatePlanFile(t, root, "docs/dev/tasks/TASK-042-02.md", row, bad)
	plan, err := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	if !containsDispatchProblem(plan, "resource") {
		t.Fatalf("malformed TASK resource was silently dropped: %#v", plan.Problems)
	}
}

// TestAuditDispatchPlanRelativeRootProbe verifies that the public README
// command behaves identically for relative and absolute repository roots.
func TestAuditDispatchPlanRelativeRootProbe(t *testing.T) {
	relRoot := filepath.Join("..", "..", "docs", "examples", "dispatch-plan", "project")
	absRoot, err := filepath.Abs(relRoot)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := TasksCheckWithFiles(relRoot, fileview.Disk{Root: relRoot}, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := TasksCheckWithFiles(absRoot, fileview.Disk{Root: absRoot}, "REQ-042")
	if err != nil {
		t.Fatal(err)
	}
	if len(absolute.Problems) != 0 {
		t.Fatalf("absolute fixture unexpectedly failed: %#v", absolute.Problems)
	}
	if len(relative.Problems) != 0 {
		t.Fatalf("relative root must match absolute root, got problems: %#v", relative.Problems)
	}
	if relative.Tasks != absolute.Tasks || relative.ClausesTotal != absolute.ClausesTotal || relative.ClausesCovered != absolute.ClausesCovered {
		t.Fatalf("relative root result differs from absolute root: relative=%+v absolute=%+v", relative, absolute)
	}
}
