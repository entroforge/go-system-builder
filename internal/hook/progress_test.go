package hook

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func progressPacket(t *testing.T, revision int, conflict string, deny bool) []byte {
	t.Helper()
	d := policy.Decision{Decision: "allow", Guidance: &policy.Guidance{Stage: "S10", LifecycleState: "release_audit", Action: "inspect audit producer", ReadOrder: []string{"AGENTS.md"}, Automation: []string{"never release automatically"}}}
	if deny {
		d.Decision = "deny"
		d.RuleID = "phase_product_write"
		d.Reason = "product frozen"
		d.Recovery = []string{"route through repair"}
	}
	b, _, err := PreToolUseWithQualityGate(d, controller.ControlResult{QualityGate: controller.QualityGateResult{Status: controller.StatusUnknown, ObservedRevision: revision, Conflicts: []string{conflict}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func unpack(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProgressSeparatesDisplayAndModelContext(t *testing.T) {
	p := unpack(t, progressPacket(t, 1, "evidence:bad:producer", false))
	h := p["hookSpecificOutput"].(map[string]any)
	context := h["additionalContext"].(string)
	if !strings.Contains(context, "evidence:bad:producer") || !strings.Contains(context, "Next: inspect audit producer") {
		t.Fatal(context)
	}
	if strings.Contains(context, "Read in order") {
		t.Fatal("ordinary calls repeat full read list")
	}
	if h["permissionDecisionReason"] == context {
		t.Fatal("allow reason duplicates recovery context")
	}
	if len([]rune(p["systemMessage"].(string))) > 481 {
		t.Fatal("unbounded notice")
	}
}
func TestNoticeDedupIsSessionScopedAndNeverHidesDenialOrContext(t *testing.T) {
	root := t.TempDir()
	first, commit := PrepareNotice(root, "session", "agent", "PreToolUse", progressPacket(t, 1, "a", false))
	commit()
	if unpack(t, first)["systemMessage"] == nil {
		t.Fatal("first notice hidden")
	}
	same, _ := PrepareNotice(root, "session", "agent", "PreToolUse", progressPacket(t, 2, "a", false))
	p := unpack(t, same)
	if p["systemMessage"] != nil {
		t.Fatal("revision-only update repeated")
	}
	if p["hookSpecificOutput"].(map[string]any)["additionalContext"] == nil {
		t.Fatal("model lost context")
	}
	for _, tc := range []struct {
		s, a, c string
		deny    bool
	}{{"other", "agent", "a", false}, {"session", "other", "a", false}, {"session", "agent", "b", false}, {"session", "agent", "a", true}} {
		out, done := PrepareNotice(root, tc.s, tc.a, "PreToolUse", progressPacket(t, 3, tc.c, tc.deny))
		done()
		if unpack(t, out)["systemMessage"] == nil {
			t.Fatalf("changed/denied notice hidden: %+v", tc)
		}
	}
	_, reset := PrepareNotice(root, "session", "agent", "SessionStart", []byte(`{"systemMessage":"resume"}`))
	reset()
	out, _ := PrepareNotice(root, "session", "agent", "PreToolUse", progressPacket(t, 4, "a", false))
	if unpack(t, out)["systemMessage"] == nil {
		t.Fatal("resume failed to reset notice")
	}
}
func TestUndeliveredNoticeIsNotCached(t *testing.T) {
	root := t.TempDir()
	b := progressPacket(t, 1, "a", false)
	PrepareNotice(root, "s", "a", "PreToolUse", b) // stdout fails; no commit
	out, _ := PrepareNotice(root, "s", "a", "PreToolUse", b)
	if unpack(t, out)["systemMessage"] == nil {
		t.Fatal("undelivered message suppressed")
	}
}
func TestStageAdvanceRestoresCompleteRecoveryContext(t *testing.T) {
	d := policy.Decision{Decision: "allow", Guidance: &policy.Guidance{Stage: "S10", ReadOrder: []string{"AGENTS.md"}, Automation: []string{"never release automatically"}}}
	b, _, err := PreToolUseWithQualityGate(d, controller.ControlResult{QualityGate: controller.QualityGateResult{Status: controller.StatusAdvanced, TransitionCommitted: true}})
	if err != nil {
		t.Fatal(err)
	}
	context := unpack(t, b)["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	if !strings.Contains(context, "Read in order: AGENTS.md") || !strings.Contains(context, "never release automatically") {
		t.Fatal(context)
	}
}

func TestProducerConflictExplainsCanonicalResponsibility(t *testing.T) {
	b, _, err := PreToolUseWithQualityGate(policy.Decision{Decision: "allow"}, controller.ControlResult{QualityGate: controller.QualityGateResult{Status: controller.StatusUnknown, GateID: "GATE-RELEASE-AUDIT-APPROVED", Conflicts: []string{"evidence:bad:producer"}}})
	if err != nil {
		t.Fatal(err)
	}
	context := unpack(t, b)["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	for _, want := range []string{"evidence:bad:producer", "Release Auditor", "Do not relabel registered evidence"} {
		if !strings.Contains(context, want) {
			t.Fatal(context)
		}
	}
}

func TestNoticeCacheFailureIsFailOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".claude"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	b := progressPacket(t, 1, "a", false)
	for i := 0; i < 2; i++ {
		out, commit := PrepareNotice(root, "s", "a", "PreToolUse", b)
		commit()
		if unpack(t, out)["systemMessage"] == nil {
			t.Fatal("cache failure suppressed notice")
		}
	}
}
func TestConcurrentNoticeWritersPreserveModelAndSafety(t *testing.T) {
	root := t.TempDir()
	b := progressPacket(t, 1, "a", false)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, commit := PrepareNotice(root, "s", "a", "PreToolUse", b)
			commit()
			p := unpack(t, out)
			if p["hookSpecificOutput"].(map[string]any)["additionalContext"] == nil {
				t.Error("lost context")
			}
		}()
	}
	wg.Wait()
	deny := progressPacket(t, 2, "a", true)
	for i := 0; i < 2; i++ {
		out, commit := PrepareNotice(root, "s", "a", "PreToolUse", deny)
		commit()
		if unpack(t, out)["systemMessage"] == nil {
			t.Fatal("denial hidden")
		}
	}
}
