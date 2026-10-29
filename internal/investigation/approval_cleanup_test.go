package investigation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestApprovalArtifactCleanupRetainsPendingReferencedOrChangedBytes(t *testing.T) {
	for _, mode := range []string{"pending", "referenced", "different-digest", "unreferenced"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath := filepath.Join(root, "state.json"), filepath.Join(root, "events.jsonl")
			body, err := schema.ReadAsset("loop-state.example.json")
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(body, &state); err != nil {
				t.Fatal(err)
			}
			state["revision"] = 1
			const path = ".claude/review/investigation/contracts/approved-r2.json"
			if mode == "referenced" {
				state["evidence"] = []any{map[string]any{"path": path}}
			}
			body, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, body, 0600); err != nil {
				t.Fatal(err)
			}
			artifact := []byte("original immutable approval artifact")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, path), artifact, 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "pending" {
				if err := os.WriteFile(statePath+".commit-pending.json", []byte(`{"state":{"pending_reference":"`+path+`"}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sha := workpacket.Digest(artifact)
			if mode == "different-digest" {
				sha = strings.Repeat("0", 64)
			}
			writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
			removed, err := cleanupApprovalArtifact(writer, runtime.Snapshot{State: state, Revision: 1}, path, sha)
			if err != nil {
				t.Fatal(err)
			}
			if removed != (mode == "unreferenced") {
				t.Fatalf("cleanup %s removed=%t", mode, removed)
			}
			if !removed {
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Fatalf("recovery material disappeared: %v", err)
				}
			}
		})
	}
}
