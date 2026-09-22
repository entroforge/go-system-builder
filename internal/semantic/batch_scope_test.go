package semantic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchScopeSemanticKeepsCurrentCoverageAndForeignDependenciesSeparate(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/loop-state.json", `{"bound_req":{"id":"REQ-053"}}`)
	write("docs/dev/tasks/TASK-1.md", "> Source REQ refs: REQ-052\n> Status: draft\n")
	write("docs/dev/tasks/TASK-2.md", "> Source REQ refs: REQ-053\n> Status: complete\n> Primary contract: BE-NEW\n")
	write("docs/dev/contracts/CONTRACTS-OLD.md", "> 需求：REQ-052\n\n| BE-OLD §1 |\n")
	write("docs/dev/contracts/CONTRACTS-NEW.md", "> 需求：REQ-053\n\n| BE-NEW §1 |\n")
	write("docs/dev/contracts/BE-OLD.md", "> Status: locked\n")
	write("docs/dev/contracts/BE-NEW.md", "> 需求：REQ-053\n> Status: locked\n")
	write("docs/dev/contracts/BE-ORPHAN.md", "> 需求：REQ-053\n> Status: locked\n")
	result, err := TasksCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tasks != 1 || result.ClausesTotal != 1 {
		t.Fatalf("result=%+v", result)
	}
	problems := strings.Join(result.Problems, "\n")
	if strings.Contains(problems, "TASK-1") || strings.Contains(problems, "contract BE-OLD exists") {
		t.Fatal(problems)
	}
	if !strings.Contains(problems, "contract BE-ORPHAN exists") || !strings.Contains(problems, "BE-NEW §1") {
		t.Fatal("current gaps were hidden: " + problems)
	}
}
