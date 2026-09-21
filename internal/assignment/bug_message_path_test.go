package assignment_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// TestBugMessagePathAcceptsRelativeAndAbsoluteFiles pins the domain contract
// used by both direct callers and the CLI: relative message paths are resolved
// under root, while an absolute path is consumed as supplied (including a
// caller-owned file outside root).
func TestBugMessagePathAcceptsRelativeAndAbsoluteFiles(t *testing.T) {
	cases := []struct {
		name string
		path func(root, message string) string
	}{
		{
			name: "relative_to_root",
			path: func(root, message string) string {
				return filepath.ToSlash(filepath.Join("messages", filepath.Base(message)))
			},
		},
		{
			name: "absolute_external",
			path: func(root, message string) string { return message },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			setupBugRuntime(t, root, "pending_approval")
			messageDir := filepath.Join(root, "messages")
			if tc.name == "absolute_external" {
				messageDir = t.TempDir()
			}
			if err := os.MkdirAll(messageDir, 0o755); err != nil {
				t.Fatal(err)
			}
			message := writeAgentExample(t, root, messageDir, "readback_response", "agent-message", "TASK-012", 3)
			requestPath := tc.path(root, message)
			snapshot, err := assignment.AdvanceBug(root,
				filepath.Join(root, ".claude", "loop-state.json"),
				filepath.Join(root, ".claude", "loop-events.jsonl"),
				assignment.BugEventRequest{
					ExpectedRevision: 3,
					BugID:            "BUG-001",
					Event:            "bug_accepted",
					MessagePath:      requestPath,
				})
			if err != nil {
				t.Fatalf("AdvanceBug with %s message path failed: %v", tc.name, err)
			}
			if snapshot.Revision != 4 {
				t.Fatalf("revision = %d, want 4", snapshot.Revision)
			}
			assertBugState(t, root, "BUG-001", "accepted")
		})
	}
}

func TestBugMessagePathRejectsMissingOrInvalidWithoutCommit(t *testing.T) {
	cases := []struct {
		name        string
		messagePath func(t *testing.T, root string) string
		write       []byte
		want        string
	}{
		{
			name: "missing_relative",
			messagePath: func(t *testing.T, root string) string {
				return "messages/missing.json"
			},
			want: "read BUG message",
		},
		{
			name: "missing_absolute_external",
			messagePath: func(t *testing.T, root string) string {
				return filepath.Join(t.TempDir(), "missing.json")
			},
			want: "read BUG message",
		},
		{
			name: "directory_path",
			messagePath: func(t *testing.T, root string) string {
				return root
			},
			want: "read BUG message",
		},
		{
			name: "invalid_schema",
			messagePath: func(t *testing.T, root string) string {
				path := filepath.Join(root, "messages", "invalid.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				return path
			},
			write: []byte(`{"not":"an Agent message"}`),
			want:  "message validation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			setupBugRuntime(t, root, "pending_approval")
			statePath := filepath.Join(root, ".claude", "loop-state.json")
			journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			messagePath := tc.messagePath(t, root)
			if tc.write != nil {
				if err := os.WriteFile(messagePath, tc.write, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err = assignment.AdvanceBug(root, statePath, journalPath, assignment.BugEventRequest{
				ExpectedRevision: 3,
				BugID:            "BUG-001",
				Event:            "bug_accepted",
				MessagePath:      messagePath,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(beforeState, afterState) {
				t.Fatal("rejected message changed runtime state")
			}
			afterJournal, readErr := os.ReadFile(journalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("rejected message appended a journal event")
			}
			var state map[string]any
			if err := json.Unmarshal(afterState, &state); err != nil {
				t.Fatal(err)
			}
			if state["revision"] != float64(3) {
				t.Fatalf("rejected message changed revision: %#v", state["revision"])
			}
			assertBugState(t, root, "BUG-001", "pending_approval")
		})
	}
}
