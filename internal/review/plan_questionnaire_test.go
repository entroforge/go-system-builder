package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewPlanQuestionnaireAndPriorAnswersAreControlArtifacts(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	state := map[string]any{
		"runtime_id": "loop-test", "baseline": map[string]any{"generation": 2},
		"review":   map[string]any{"round": 3},
		"entities": map[string]any{"work_packets": []any{map[string]any{"answer_path": ".claude/submissions/loop-test/g2/S3/contract-answer.json"}}},
	}
	current := PlanQuestionnairePath(root, state)
	paths := []string{
		current,
		strings.TrimSuffix(current, ".json") + ".md",
		filepath.Join(root, ".claude/worktree-reminders", strings.Repeat("a", 64)+".txt"),
		filepath.Join(root, ".claude/submissions/loop-test/g2/S3/contract-answer.json"),
		filepath.Join(root, ".claude/submissions/loop-test/g2/S7/r2/review-plan.json"),
		filepath.Join(root, ".claude/submissions/loop-test/g2/S7/r3/extra.json"),
		filepath.Join(root, ".claude/worktree-reminders/product.go"),
		filepath.Join(root, "internal/product.go"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	drift, err := detectUndeclaredProductDriftWithState(root, &Plan{}, state, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".claude/submissions/loop-test/g2/S7/r3/extra.json",
		".claude/worktree-reminders/product.go",
		"internal/product.go",
	}
	if !reflect.DeepEqual(drift, want) {
		t.Fatalf("product drift = %v, want %v", drift, want)
	}
	// A declared product is always pinned, even inside a control directory.
	plan := &Plan{FrozenSubjects: []FrozenSubject{{Path: ".claude/submissions/loop-test/g2/S7/r3/review-plan.json", SHA256: sha256Of([]byte("different\n"))}}}
	if err := verifyFrozenSubjects(root, plan, state); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("declared frozen subject was not checked: %v", err)
	}
}
