package repair

import (
	"fmt"
	"testing"
)

func TestPostReviewCommittedLogDeletion(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, ".gitignore", "*.log\n")
	boundaryWrite(t, root, "testdata/golden.log", "expected\n")
	boundaryGit(t, root, "add", "-f", ".gitignore", "testdata/golden.log")
	boundaryGit(t, root, "-c", "user.name=Review", "-c", "user.email=review@example.invalid", "commit", "-m", "fixture")
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	boundaryGit(t, root, "rm", "testdata/golden.log")
	boundaryGit(t, root, "-c", "user.name=Review", "-c", "user.email=review@example.invalid", "commit", "-m", "delete fixture")
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == "testdata/golden.log" && c.Status == "deleted" {
			return
		}
	}
	t.Fatalf("committed tracked fixture deletion disappeared: changes=%+v excluded=%v", changes, excludedBaselinePaths(root, []string{"testdata/golden.log"}))
}
func TestPostReviewTrackedHookPrefix(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	rel := ".claude/hooks/custom-policy.go"
	boundaryWrite(t, root, rel, "package policy\n")
	boundaryGit(t, root, "add", rel)
	captured, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range captured {
		if a.Path == rel {
			return
		}
	}
	t.Fatalf("tracked hook implementation excluded as journal: %s", rel)
}

// Historical ownership must survive all ordinary ways of removing a file
// from the index, including a file that remains on disk under a new ignore rule.
func TestHistoricalLogOwnership(t *testing.T) {
	for _, action := range []string{"staged-delete", "committed-delete", "rename", "untrack", "ignore-change"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			boundaryGit(t, root, "init")
			rel := "testdata/golden.log"
			boundaryWrite(t, root, ".gitignore", "")
			boundaryWrite(t, root, rel, "original\n")
			boundaryGit(t, root, "add", ".")
			commit := func() {
				boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "test")
			}
			commit()
			before, _, err := captureRepositoryBaseline(root)
			if err != nil {
				t.Fatal(err)
			}
			session := RepairSession{BaselineArtifacts: before}
			switch action {
			case "staged-delete", "committed-delete":
				boundaryGit(t, root, "rm", rel)
			case "rename":
				boundaryGit(t, root, "mv", rel, "testdata/renamed.log")
			default:
				boundaryGit(t, root, "rm", "--cached", rel)
			}
			if action != "staged-delete" {
				commit()
			}
			boundaryWrite(t, root, ".gitignore", "*.log\n")
			if action == "ignore-change" {
				boundaryWrite(t, root, rel, "changed\n")
			}
			changes, err := ComputeSessionChangeset(root, session)
			if err != nil {
				t.Fatal(err)
			}
			want := "deleted"
			if action == "untrack" {
				want = ""
			}
			if action == "ignore-change" {
				want = "modified"
			}
			got := ""
			for _, c := range changes {
				if c.Path == rel {
					got = c.Status
				}
			}
			if got != want {
				t.Fatalf("status=%q want=%q changes=%v", got, want, changes)
			}
			if excludedBaselinePaths(root, []string{rel})[rel] {
				t.Fatal("historical membership lost")
			}
		})
	}
}

func TestHookCodeProtectedAndDecisionJournalExcluded(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	code := ".claude/hooks/custom-policy.go"
	journal := ".claude/hook-decisions.jsonl"
	boundaryWrite(t, root, code, "package policy\n")
	boundaryWrite(t, root, journal, "{}\n")
	boundaryGit(t, root, "add", code)
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	boundaryWrite(t, root, code, "package changed\n")
	boundaryWrite(t, root, journal, "{}\n{}\n")
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != code {
		t.Fatalf("changes=%v", changes)
	}
	for _, p := range []string{".claude/hook-policy.go", ".claude/hook-decisions.jsonl.go", ".claude/hooks/custom-policy.go"} {
		if isControlPlanePath(p, false) {
			t.Fatalf("code exempted: %s", p)
		}
	}
}

func TestTrackedIndexProtectsAncestorsNotSimilarPrefixes(t *testing.T) {
	b := baselineBoundary{}
	b.addTracked("src/deep/code.go")
	for _, p := range []string{"src", "src/deep", "src/deep/code.go"} {
		if !b.hasTracked(p) {
			t.Fatalf("missing ownership: %s", p)
		}
	}
	for _, p := range []string{"sr", "src/deeper", "src/deep/code", "src/absent.go"} {
		if b.hasTracked(p) {
			t.Fatalf("invented ownership: %s", p)
		}
	}
}

func BenchmarkTrackedOwnership(b *testing.B) {
	for _, n := range []int{2000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			index := baselineBoundary{}
			paths := make([]string, n)
			for i := range paths {
				paths[i] = fmt.Sprintf("src/pkg%d/file.go", i)
				index.addTracked(paths[i])
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, p := range paths {
					if !index.hasTracked(p) || !index.hasTracked("src") || index.hasTracked("missing") {
						b.Fatal("incorrect index")
					}
				}
			}
		})
	}
}
