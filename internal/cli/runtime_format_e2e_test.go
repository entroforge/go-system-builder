package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestBindAndHumanUnbindPreserveOriginalRuntimeFormat(t *testing.T) {
	for _, mode := range []string{"legacy11", "new12", "packet12"} {
		t.Run(mode, func(t *testing.T) {
			root := newBindTestRoot(t)
			path := filepath.Join(root, ".claude/loop-state.json")
			if mode == "legacy11" {
				// Explicit existing 1.1 deployment fixture; it never enables new fields.
				state := readJSONMap(t, path)
				state["schema_version"] = "1.1.0"
				delete(state["configuration"].(map[string]any), "optimization")
				entities := state["entities"].(map[string]any)
				delete(entities, "work_packets")
				delete(entities, "submissions")
				writeJSONMap(t, path, state)
				if err := os.Remove(filepath.Join(root, ".claude/runtime-initialization.json")); err != nil {
					t.Fatal(err)
				}
			} else if mode == "packet12" {
				for _, rel := range []string{"loop-state.json", "loop-events.jsonl", "hook-decisions.jsonl", "runtime-initialization.json"} {
					if err := os.Remove(filepath.Join(root, ".claude", rel)); err != nil {
						t.Fatal(err)
					}
				}
				stageCLI(t, "init", "--root", root, "--packet")
			}
			source := readJSONMap(t, path)
			stageCLI(t, "req", "bind", "--root", commitStageFixture(t, root), "--req", "docs/requirements/REQ-099.md", "--dev-branch", "test-development", "--release-upstream", "origin/release", "--approved-by", "alice")
			bound := readJSONMap(t, path)
			if bound["schema_version"] != source["schema_version"] || runtime.PacketEnabled(bound) != runtime.PacketEnabled(source) {
				t.Fatal("REQ bind changed runtime format or consumer choice")
			}
			stageCLI(t, "req", "unbind", "--root", root, "--approved-by", "alice", "--reason", "End the format acceptance fixture")
			fresh := readJSONMap(t, path)
			if fresh["schema_version"] != source["schema_version"] || runtime.PacketEnabled(fresh) != runtime.PacketEnabled(source) {
				t.Fatal("human unbind implicitly migrated the deployment")
			}
			if fresh["runtime_id"] != "loop-inactive" || fresh["bound_req"] != nil {
				t.Fatal("human unbind did not retain the original reset semantics")
			}
			if mode == "legacy11" {
				if _, exists := fresh["configuration"].(map[string]any)["optimization"]; exists {
					t.Fatal("legacy reset added optimization fields")
				}
			}
			var out, stderr bytes.Buffer
			if code := cli.Run([]string{"init", "--root", root, "--packet"}, nil, &out, &stderr); code == 0 {
				t.Fatal("init changed an existing inactive runtime after reset")
			}
		})
	}
}
