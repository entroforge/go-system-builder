package cli

import (
	"strings"
	"testing"
)

// TestS9HandoffStatesMainSuccessorWithoutRetiredCommand pins that the S9
// handoff names Main's real successor instead of the retired copyable
// `runtime workspace integrate` command. The old command was removed with the
// old wiring: L4-workflow-convergence.md:312 — "旧入口、旧 schema、旧 Hook／CLI、
// 旧消费者及兼容适配连同仅为它们存在的测试一并删除"; L3-S6-build.md:316 —
// "删除 frozen execution 的观察、接收与恢复分支，不以旧协议建立第二条作者路径".
// L4-workflow-convergence.md:228 keeps the actual work and independent
// re-verification with Main and removes the Final/Changeset proof gate.
func TestS9HandoffStatesMainSuccessorWithoutRetiredCommand(t *testing.T) {
	root := "/tmp/project $(printf expanded) `printf expanded` 'quotes' and spaces"
	assignment, owner := "assignment'one", "owner $literal"
	text := s9CandidateAction(root, assignment, owner)
	for _, want := range []string{
		"Original Candidate is received",
		"Main takes the actual contribution",
		"performs necessary integration and checks",
		"saves its original ChangeImpact",
		"no Final, commit, proof or mandatory workspace integrate action precedes its reception",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("current S9 handoff missing %q: %s", want, text)
		}
	}
	for _, unwanted := range []string{"runtime workspace integrate", "using this exact command"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("retired handoff command %q reappeared: %s", unwanted, text)
		}
	}
}
