package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6SourceBranchesPrecedeActualInputReads(t *testing.T) {
	root, _, r := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(r)
	body, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	read := strings.Index(text, "Read the actual TASK")
	if read < 0 {
		t.Fatal("actual brief lacks TASK input")
	}
	for _, branch := range []string{"First start, before any responsibility work:", "Normal resume of this same responsibility:", "Unknown or mixed source:"} {
		if pos := strings.Index(text, branch); pos < 0 || pos >= read {
			t.Fatalf("%q must precede TASK reads: %s", branch, text)
		}
	}
	for _, want := range []string{"continue your existing Plan", "even when HEAD no longer equals", "do not infer normal resume from those alone", "another source has been mixed in"} {
		if !strings.Contains(text[:read], want) {
			t.Fatalf("missing source distinction %q", want)
		}
	}
}

func TestNativeS6OriginalSourceResumeWithOwnCommitAndDirtyWorkReceivesResult(t *testing.T) {
	root, worker, r := nativeDeliveryFixture(t)
	brief, plan, result := assignmentrecord.NativePaths(r)
	// This worker actually starts at the prepared source, then makes its own
	// product commit. Its private Plan survives a later resume with dirty work.
	nativeCommit(t, worker)
	nativeAnswer(t, worker, r)
	originalPlan, err := os.ReadFile(filepath.Join(worker, plan))
	if err != nil {
		t.Fatal(err)
	}
	resultAnswer, err := os.ReadFile(filepath.Join(worker, result))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, result)); err != nil {
		t.Fatal(err)
	}
	personal, err := workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(worker, "src/pkg/unfinished.txt")
	draft := []byte("Ongoing personal work retained across resume.\n")
	if err := os.WriteFile(dirty, draft, 0600); err != nil {
		t.Fatal(err)
	}
	if code, out := nativeEvent(t, root, worker, "SubagentStart", filepath.Join(root, brief), false); code != 0 {
		t.Fatalf("resume: %d %s", code, out)
	}
	// Save the actual professional Result after resuming, then use the original
	// public receiver. Receipt does not silently clean or integrate this work.
	if err := os.WriteFile(filepath.Join(worker, result), resultAnswer, 0600); err != nil {
		t.Fatal(err)
	}
	if code, out := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result), false); code != 0 {
		t.Fatalf("resumed delivery: %d %s", code, out)
	}
	if text := mainNativeReturn(t, root, filepath.Join(root, brief)); !strings.Contains(text, "Result received") {
		t.Fatalf("resume was not received: %s", text)
	}
	if head, _ := workspace.Git(t.Context(), worker, "rev-parse", "HEAD"); head != personal {
		t.Fatal("resume changed personal commit")
	}
	if got, err := os.ReadFile(dirty); err != nil || !bytes.Equal(got, draft) {
		t.Fatal("resume discarded dirty work")
	}
	if got, err := os.ReadFile(filepath.Join(worker, plan)); err != nil || !bytes.Equal(got, originalPlan) {
		t.Fatal("resume rewrote original Plan")
	}
}

func TestNativeS6DifferentSourceCannotUseNormalResumeToReceive(t *testing.T) {
	root, worker, r := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(r)
	// A real unrelated root retains the same visible files and author identity;
	// neither is permission to describe it as the original execution context.
	for _, args := range [][]string{{"checkout", "--orphan", "native/wrong-source"}, {"add", "--all"}, {"commit", "-m", "Unrelated source root"}} {
		if _, err := workspace.Git(t.Context(), worker, args...); err != nil {
			t.Fatal(err)
		}
	}
	nativeCommit(t, worker)
	nativeAnswer(t, worker, r)
	before := stageSnapshot(t, root)
	if code, out := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result), false); code != 0 || !strings.Contains(out, "Native S6 reception incomplete") || !strings.Contains(out, "does not contain the declared source commit") {
		t.Fatalf("wrong source accepted as resume: %d %s", code, out)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("wrong source created completion")
	}
	if _, err := os.Stat(filepath.Join(worker, result)); err != nil {
		t.Fatal("wrong source handoff lost existing Result")
	}
}
