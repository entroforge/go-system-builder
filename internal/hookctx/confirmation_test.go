package hookctx_test

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmationIntentComesFromPinnedSessionBytes(t *testing.T) {
	root := t.TempDir()
	rel := ".claude/review/repair/sessions/confirmation.json"
	path := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(path), 0700)
	session := []byte(`{"schema_version":"1.1.0","record_type":"repair_session","session_id":"repair-session-confirm","intent":"confirm"}`)
	os.WriteFile(path, session, 0600)
	state := map[string]any{"runtime_id": "loop-REQ-039", "revision": 1, "lifecycle": map[string]any{"state": "bug_resolution", "phase": "fixing"}, "review": map[string]any{"repair": map[string]any{"session_id": "repair-session-confirm", "path": rel, "sha256": hookSHA(session)}}}
	data, _ := json.Marshal(state)
	writeJSONL(t, filepath.Join(root, ".claude/loop-state.json"), string(data))
	writeJSONL(t, filepath.Join(root, ".claude/loop-events.jsonl"), "")
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PolicyContext.RepairIntent != "confirm" {
		t.Fatalf("intent=%q", loaded.PolicyContext.RepairIntent)
	}
	os.WriteFile(path, []byte(`{"schema_version":"1.0.0","session_id":"repair-session-confirm"}`), 0600)
	loaded, err = hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PolicyContext.RepairIntent != "unknown" {
		t.Fatal("tampered Session supplied write authority")
	}
}
