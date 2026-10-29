package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The invalid field belongs to the external index, so both bounded observation
// and the ordinary routing Snapshot fail before a stage can be selected.
func TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute(t *testing.T) {
	root, packet := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, packet)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	good, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(good, &state); err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, value := range state["entities"].(map[string]any)["work_packets"].([]any) {
		row := value.(map[string]any)
		if row["packet_id"] == packet.ID {
			if row["body_ref"] == nil {
				t.Fatal("fixture did not issue an external body index")
			}
			row["title"] = "illegal external index field"
			changed = true
		}
	}
	if !changed {
		t.Fatal("issued packet index missing")
	}
	invalid, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	invalid = append(invalid, '\n')
	if err := os.WriteFile(statePath, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	store := runtime.NewStore(statePath, journalPath)
	_, cause := store.Snapshot()
	if cause == nil || !strings.Contains(cause.Error(), "title") {
		t.Fatalf("fixture did not cause the expected Snapshot failure: %v", cause)
	}
	if _, err := store.ObservationSnapshot(); err == nil {
		t.Fatal("bounded observation unexpectedly accepted the invalid index")
	}
	child := s7ListenerPayload(t, packet.Binding.OwnerAgentID, "Review "+filepath.Join(root, packet.ViewPath))
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		out, diagnostic := s7ListenerEvent(t, root, event, child)
		if out != "" || diagnostic != "" {
			t.Fatalf("%s with an unverified child identity must be silent: %s %s", event, out, diagnostic)
		}
		out, diagnostic = s7ListenerEvent(t, root, event, map[string]any{"agent_id": "unverified-unknown"})
		if out != "" || diagnostic != "" {
			t.Fatalf("%s with an unknown identity must be silent: %s %s", event, out, diagnostic)
		}
	}
	assertContext := func(event, out string) {
		t.Helper()
		var payload struct {
			Output struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("%s has no event context: %v %s", event, err, out)
		}
		if payload.Output.Event != event {
			t.Fatalf("%s returned the wrong event: %s", event, out)
		}
		for _, want := range []string{statePath, cause.Error(), "current stage and professional reception cannot be verified", "Main must restore", "Ordinary tools remain available"} {
			if !strings.Contains(payload.Output.Context, want) {
				t.Fatalf("%s lost %q: %s", event, want, out)
			}
		}
		if strings.Contains(out, "permissionDecision") {
			t.Fatalf("%s changed ordinary tool permission: %s", event, out)
		}
	}
	start, _ := s7ListenerEvent(t, root, "SubagentStart", child)
	assertContext("SubagentStart", start)
	stop, _ := s7ListenerEvent(t, root, "SubagentStop", child)
	var notice struct {
		Message string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(stop), &notice); err != nil || !strings.Contains(notice.Message, cause.Error()) || !strings.Contains(notice.Message, statePath) {
		t.Fatalf("SubagentStop lost its user-side diagnostic: %v %s", err, stop)
	}
	// These are fresh, actual Main events; no delivery to Main is inferred from
	// a child event or persisted into a new notification queue.
	for _, event := range []string{"PreToolUse", "PostToolUse", "SessionStart"} {
		out, _ := s7ListenerEvent(t, root, event, nil)
		assertContext(event, out)
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(invalid, after) {
		t.Fatalf("failure observation changed Runtime: %v", err)
	}
	if after, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(journal, after) {
		t.Fatalf("failure observation changed journal: %v", err)
	}
	if err := os.WriteFile(statePath, good, 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", child)
	if !strings.Contains(out, "accepted=true") || !strings.Contains(out, packet.ID) {
		t.Fatalf("restored original source did not resume its original receiver: %s %s", out, diagnostic)
	}
	main, _ := s7ListenerEvent(t, root, "PreToolUse", nil)
	if strings.Contains(main, "Runtime snapshot at") && strings.Contains(main, "could not be verified") {
		t.Fatalf("a later event retained the old read error: %s", main)
	}
}
