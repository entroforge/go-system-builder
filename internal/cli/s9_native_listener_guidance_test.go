package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9NativeListenerKeepsPrivateOutputsOutOfMainAuthoring(t *testing.T) {
	f := newS9ListenerFixture(t, false)
	packets, err := runtime.RepairPlanReportPackets(f.state)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Plan packet: %v %v", packets, err)
	}
	p := packets[0]
	brief, source, result := nativeS9Paths(p)
	writeRepairLeadFile(t, f.root, brief, "original native brief")
	// This fixture represents an existing preparation with legacy author
	// coordinates. The immutable pin must exist; reminders cannot guess them.
	writeRepairLeadFile(t, f.root, source, `{ "private_submission_version": 0 }`)
	// Private authoring need not exist in Main. The reminder must not turn
	// that absence into a missing-input prerequisite or a Main write target.
	if err := os.Remove(resolveRootPath(f.root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	for _, owner := range []string{"Main", p.Binding.OwnerAgentID} {
		request := policy.Input{Event: "PreToolUse", AgentID: owner, CWD: f.root}
		got := s9QuestionnaireContext(context.Background(), f.root, request, nil, runtime.Snapshot{State: f.state}, owner, false)
		for _, want := range []string{resolveRootPath(f.root, brief), "read-only professional question", "worktree-relative PlanReport " + p.AnswerPath, result, "PreToolUse", "Stop/SubagentStop fallback"} {
			if !strings.Contains(got, want) {
				t.Fatalf("owner=%s omitted %q: %s", owner, want, got)
			}
		}
		if strings.Contains(got, resolveRootPath(f.root, p.AnswerPath)) || strings.Contains(got, "view/answer is not prepared") {
			t.Fatalf("owner=%s was directed to Main's private output: %s", owner, got)
		}
		reads := s9QuestionnaireReadOrder(f.root, f.state, request)
		if !slices.Contains(reads, resolveRootPath(f.root, brief)) || slices.Contains(reads, resolveRootPath(f.root, p.AnswerPath)) {
			t.Fatalf("owner=%s read order confuses private output with Main source: %v", owner, reads)
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	if string(before) != string(after) {
		t.Fatal("reminder changed Runtime")
	}
	if _, err := os.Stat(resolveRootPath(f.root, p.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("reminder manufactured Main authoring output: %v", err)
	}
}
