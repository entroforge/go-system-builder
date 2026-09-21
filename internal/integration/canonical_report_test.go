package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPostReviewMissingCanonicalResultDoesNotFallback(t *testing.T) {
	root, wt, _, _, _ := newRecoveryGitRepo(t)
	a := assignmentContext(wt, "feature", "test2")
	a.CompletionRef = ".claude/evidence/missing-canonical-result.json"
	fallback := filepath.Join(root, ".claude/evidence/current/g1/assignments", a.AssignmentID, "completion.json")
	if err := os.MkdirAll(filepath.Dir(fallback), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte(`{"message_type":"completion_report"}`), 0644); err != nil {
		t.Fatal(err)
	}
	in, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: a, TargetBranch: "test2", BaselineGeneration: 1, RuntimeID: "current"}, InspectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if in.Ready {
		t.Fatalf("missing canonical result accepted via fallback: %s", in.CompletionReportPath)
	}
}

func TestCanonicalReportResolutionBoundaries(t *testing.T) {
	root, wt, _, _, _ := newRecoveryGitRepo(t)
	a := assignmentContext(wt, "feature", "test2")
	req := InspectRequest{Root: root, Assignment: a, TargetBranch: "test2", RuntimeID: "current", BaselineGeneration: 2}
	write := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"message_type":"completion_report"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Multiple historical candidates must never be considered eligible.
	for _, rel := range []string{
		".claude/evidence/current/g1/assignments/" + a.AssignmentID + "/completion.json",
		".claude/evidence/other/g2/assignments/" + a.AssignmentID + "/completion.json",
		".claude/evidence/current/g2/assignments/other/completion.json",
	} {
		write(rel)
	}
	in, err := Inspect(context.Background(), req, InspectConfig{})
	if err != nil || in.Ready {
		t.Fatalf("historical candidates accepted: %+v %v", in, err)
	}
	canonical := ".claude/evidence/current/g2/assignments/" + a.AssignmentID + "/completion.json"
	write(canonical)
	req.Assignment.CompletionRef = canonical
	in, err = Inspect(context.Background(), req, InspectConfig{})
	if err != nil || !in.Ready || in.CompletionReportPath != canonical {
		t.Fatalf("canonical rejected: %+v %v", in, err)
	}
	if err := os.Remove(filepath.Join(root, canonical)); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshCompletionBinding(root, "current", canonical, in); err == nil {
		t.Fatal("recovery substituted another report")
	}
	in, err = Inspect(context.Background(), req, InspectConfig{})
	if err != nil || in.Ready {
		t.Fatalf("first inspection substituted another report: %+v %v", in, err)
	}
	write(canonical)
	req.Assignment.CompletionRef = ""
	in, err = Inspect(context.Background(), req, InspectConfig{})
	if err != nil || !in.Ready || in.CompletionReportPath != canonical {
		t.Fatalf("current identity discovery failed: %+v %v", in, err)
	}
	req.RuntimeID = ""
	in, err = Inspect(context.Background(), req, InspectConfig{})
	if err != nil || in.Ready {
		t.Fatalf("unknown runtime accepted: %+v %v", in, err)
	}
}
