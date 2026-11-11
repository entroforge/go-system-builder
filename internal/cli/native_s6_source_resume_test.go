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

// TestNativeS6SourceBranchesPrecedeActualInputReads pins that the retired
// fixed-source preparation branches stay out of the Builder brief. Those
// branches asked the Builder to decide its own execution source from Git state
// before reading any input. L3-S6-build.md:320 — "Hook 不扫描 Git、登记或比对
// HEAD／base／commit OID、检查祖先关系或逐文件散列作为接收资格"; L4-state-transition-core.md:138
// — "Hook 所在的接收、Writer、事务恢复和后继均不计算、登记或校验散列". The
// current brief points the Builder at the actual TASK inputs in its actual
// worktree and leaves an unknown source to Main.
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
	for _, retired := range []string{
		"First start, before any responsibility work:",
		"Normal resume of this same responsibility:",
		"Unknown or mixed source:",
		"continue your existing Plan",
		"even when HEAD no longer equals",
		"do not infer normal resume from those alone",
		"another source has been mixed in",
		"git rev-parse HEAD",
		"git switch -c",
	} {
		if strings.Contains(text, retired) {
			t.Fatalf("retired fixed-source branch %q reappeared in the brief: %s", retired, text)
		}
	}
	for _, want := range []string{
		"Read the actual TASK at worktree-relative path",
		"Read TASK, specifications and product sources in your actual worktree",
		"Preserve existing work; report missing inputs to Main",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("current brief missing %q: %s", want, text)
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

// TestNativeS6DifferentSourceIsNotJudgedBySourceCommit pins that the retired
// source-commit qualification no longer refuses a delivery. The recorded
// responsibility, the issued paths and the professional content decide the
// reception; where the source really came from is a Main/executor question.
// L3-S6-build.md:320 — "Hook 不扫描 Git、登记或比对 HEAD／base／commit OID、检查
// 祖先关系或逐文件散列作为接收资格；来源未知时保留原卷和已收事实，由 Main／系统
// 处理"; L4-hook-platform-wiring.md:81 — "不另遍历 Git worktree、比对 HEAD／branch／pin
// 或认证 actor 与来源根来授予答卷资格".
func TestNativeS6DifferentSourceIsNotJudgedBySourceCommit(t *testing.T) {
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
	code, out := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result), false)
	if code != 0 {
		t.Fatalf("retired source-commit qualification refused the delivery: %d %s", code, out)
	}
	if strings.Contains(out, "does not contain the declared source commit") {
		t.Fatalf("retired source-commit qualification reappeared: %s", out)
	}
	// The delivery is received on the recorded responsibility and the
	// professional content; where the work really came from stays a
	// Main/executor question.
	if text := mainNativeReturn(t, root, filepath.Join(root, brief)); !strings.Contains(text, "Result received") {
		t.Fatalf("delivery was not received on the original responsibility: %s", text)
	}
	if _, err := os.Stat(filepath.Join(worker, result)); err != nil {
		t.Fatal("wrong source handoff lost existing Result")
	}
}
