package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planningOutput(t *testing.T, context string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": context}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPlanningContextDedupKeepsResponsibilityAndRecoveryBoundaries(t *testing.T) {
	root := t.TempDir()
	original := planningOutput(t, "S2 packet /view; answer /answer; missing conclusion")
	first, commit := PreparePlanningContext(root, "session", "Main", "PreToolUse", "runtime/g1/e2", original)
	if !bytes.Equal(first, original) {
		t.Fatal("first delivery was reduced")
	}
	// A failed stdout write must not record delivery.
	beforeCommit, _ := PreparePlanningContext(root, "session", "Main", "PreToolUse", "runtime/g1/e2", original)
	if !bytes.Equal(beforeCommit, original) {
		t.Fatal("undelivered context was cached")
	}
	commit()
	same, _ := PreparePlanningContext(root, "session", "Main", "PreToolUse", "runtime/g1/e2", original)
	if !strings.Contains(string(same), "guidance is unchanged") || bytes.Contains(same, []byte("permissionDecision")) {
		t.Fatalf("duplicate delivery not reduced without permissions: %s", same)
	}
	for _, test := range []struct{ name, session, agent, responsibility, context string }{
		{"session", "other", "Main", "runtime/g1/e2", "S2 packet /view; answer /answer; missing conclusion"},
		{"owner", "session", "other", "runtime/g1/e2", "S2 packet /view; answer /answer; missing conclusion"},
		{"generation", "session", "Main", "runtime/g2/e2", "S2 packet /view; answer /answer; missing conclusion"},
		{"entry", "session", "Main", "runtime/g1/e3", "S2 packet /view; answer /answer; missing conclusion"},
		{"runtime", "session", "Main", "other/g1/e2", "S2 packet /view; answer /answer; missing conclusion"},
		{"path", "session", "Main", "runtime/g1/e2", "S2 packet /new-view; answer /answer; missing conclusion"},
		{"diagnostic", "session", "Main", "runtime/g1/e2", "S2 accepted; architecture document must be locked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := planningOutput(t, test.context)
			out, _ := PreparePlanningContext(root, test.session, test.agent, "PreToolUse", test.responsibility, packet)
			if !bytes.Equal(out, packet) {
				t.Fatalf("new responsibility or diagnostic hidden: %s", out)
			}
		})
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		_, reset := PreparePlanningContext(root, "session", "Main", event, "", original)
		reset()
		out, commit := PreparePlanningContext(root, "session", "Main", "PreToolUse", "runtime/g1/e2", original)
		if !bytes.Equal(out, original) {
			t.Fatalf("%s did not restore context", event)
		}
		commit()
	}
}

func TestPlanningContextWithoutIdentityOrCacheAlwaysDelivers(t *testing.T) {
	original := planningOutput(t, "exact current responsibility")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".claude"), []byte("unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"session", ""} {
		for i := 0; i < 2; i++ {
			out, commit := PreparePlanningContext(root, session, "Main", "PreToolUse", "runtime/g1/e2", original)
			commit()
			if !bytes.Equal(out, original) {
				t.Fatalf("cache failure or missing session hid context: %s", out)
			}
		}
	}
	root = t.TempDir()
	_, commit := PreparePlanningContext(root, "session", "Main", "PreToolUse", "runtime/g1/e2", original)
	commit()
	for _, event := range []string{"PostToolUse", "Stop"} {
		out, _ := PreparePlanningContext(root, "session", "Main", event, "runtime/g1/e2", original)
		if !bytes.Equal(out, original) {
			t.Fatalf("non-observation %s was reduced", event)
		}
	}
	out, _ := PreparePlanningContext(root, "session", "Main", "PreToolUse", "", original)
	if !bytes.Equal(out, original) {
		t.Fatal("unknown responsibility was reduced")
	}
}
