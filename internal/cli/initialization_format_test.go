package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func initFormatRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func initMap(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
func putInitMap(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFreshInitializationUses13WithExplicitPacketChoice(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			root := initFormatRoot(t)
			args := []string{"init", "--root", root}
			if enabled {
				args = append(args, "--packet")
			} else {
				args = append(args, "--packet=false")
			}
			var out, stderr bytes.Buffer
			if code := Run(args, nil, &out, &stderr); code != 0 {
				t.Fatalf("init: %d %s", code, stderr.String())
			}
			state := initMap(t, root)
			if state["schema_version"] != "1.3.0" || runtime.PacketEnabled(state) != enabled {
				t.Fatal("fresh initialization did not bind the selected format")
			}
			service, err := submission.New(root)
			if err != nil {
				t.Fatalf("fresh inactive observation required nonexistent S5 authority: %v", err)
			}
			if packets, err := service.Issue(""); err != nil || len(packets) != 0 {
				t.Fatalf("fresh S0 issued a signing responsibility: %v", err)
			}

			for _, field := range []string{"work_packets", "submissions"} {
				if len(state["entities"].(map[string]any)[field].([]any)) != 0 {
					t.Fatal("init minted domain facts")
				}
			}
			data, err := os.ReadFile(filepath.Join(root, ".claude/runtime-initialization.json"))
			if err != nil {
				t.Fatal(err)
			}
			var receipt map[string]any
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			stateData, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if receipt["runtime_schema_version"] != "1.3.0" || receipt["initial_state_sha256"] != fmt.Sprintf("%x", sha256.Sum256(stateData)) {
				t.Fatal("initialization receipt does not bind the published state")
			}
			before := append([]byte(nil), stateData...)
			if code := Run([]string{"init", "--root", root, "--packet"}, nil, &out, &stderr); code == 0 {
				t.Fatal("init changed an existing Runtime")
			}
			after, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refused init mutated existing state")
			}
		})
	}
}

func TestFreshInitializationEnablesQuestionnairesWithoutExtraFlag(t *testing.T) {
	root := initFormatRoot(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code != 0 {
		t.Fatalf("init: %d %s", code, stderr.String())
	}
	state := initMap(t, root)
	if !runtime.PacketEnabled(state) {
		t.Fatal("fresh init requires a hidden opt-in for the normal questionnaire flow")
	}
	if state["lifecycle"].(map[string]any)["state"] != "inactive" {
		t.Fatal("enabling questionnaires must not approve or bind a requirement")
	}
}

func TestPendingInitializationKeepsOriginalVersionAndMode(t *testing.T) {
	for _, version := range []string{"1.1.0", "1.2.0", "1.3.0"} {
		for _, enabled := range []bool{false, true} {
			if version == "1.1.0" && enabled {
				continue
			}
			t.Run(version+fmt.Sprint(enabled), func(t *testing.T) {
				root := initFormatRoot(t)
				fresh, err := inactiveRuntimeState(root, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
				if err != nil {
					t.Fatal(err)
				}
				fresh["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = enabled
				fresh, err = runtime.PreserveRuntimeFormat(map[string]any{
					"schema_version": version,
					"configuration": map[string]any{
						"optimization": fresh["configuration"].(map[string]any)["optimization"],
					},
				}, fresh)
				if err != nil {
					t.Fatal(err)
				}
				marker := filepath.Join(root, ".claude/loop-init-pending.json")
				putInitMap(t, marker, initPending{SchemaVersion: "1.0.0", FreshState: fresh})
				before, err := os.ReadFile(marker)
				if err != nil {
					t.Fatal(err)
				}
				var out, stderr bytes.Buffer
				conflicting := fmt.Sprintf("--packet=%t", !enabled)
				if code := Run([]string{"init", "--root", root, conflicting}, nil, &out, &stderr); code == 0 {
					t.Fatal("explicit mode changed a pending initialization")
				}
				after, err := os.ReadFile(marker)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("mode conflict rewrote the original marker")
				}
				if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code != 0 {
					t.Fatalf("original pending retry: %d %s", code, stderr.String())
				}
				state := initMap(t, root)
				if state["schema_version"] != version || runtime.PacketEnabled(state) != enabled {
					t.Fatal("pending initialization silently migrated")
				}
			})
		}
	}
}

func TestPendingInitializationValidatesBeforeAnyPublication(t *testing.T) {
	root := initFormatRoot(t)
	fresh, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	fresh["unknown_runtime_field"] = true
	marker := filepath.Join(root, ".claude/loop-init-pending.json")
	putInitMap(t, marker, initPending{SchemaVersion: "1.0.0", FreshState: fresh})
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code == 0 {
		t.Fatal("invalid pending state published")
	}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl", "hook-decisions.jsonl", "runtime-initialization.json"} {
		if _, err := os.Stat(filepath.Join(root, ".claude", name)); !os.IsNotExist(err) {
			t.Fatal("invalid pending state partially published")
		}
	}
}

func TestRecoveryFormatPreservesEmptySourcesAndRefusesIssuedFacts(t *testing.T) {
	for _, mode := range []string{"legacy", "1.3 empty", "1.3 packet", "1.3 received", "unknown new", "future version"} {
		t.Run(mode, func(t *testing.T) {
			root := initFormatRoot(t)
			fresh, err := inactiveRuntimeState(root, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			preserve13 := func() error {
				preserved, preserveErr := runtime.PreserveRuntimeFormat(map[string]any{
					"schema_version": "1.3.0",
					"configuration": map[string]any{
						"optimization": fresh["configuration"].(map[string]any)["optimization"],
					},
				}, fresh)
				if preserveErr == nil {
					fresh = preserved
				}
				return preserveErr
			}
			switch mode {
			case "future version":
				fresh["schema_version"] = "9.0.0"
			case "legacy":
				fresh, err = runtime.PreserveRuntimeFormat(map[string]any{"schema_version": "1.1.0"}, fresh)
				if err != nil {
					t.Fatal(err)
				}
			case "1.3 empty":
				if err := preserve13(); err != nil {
					t.Fatal(err)
				}
			case "1.3 packet":
				if err := preserve13(); err != nil {
					t.Fatal(err)
				}
				fresh["entities"].(map[string]any)["work_packets"] = []any{map[string]any{"packet_id": "do-not-invent"}}
			case "1.3 received":
				if err := preserve13(); err != nil {
					t.Fatal(err)
				}
				fresh["entities"].(map[string]any)["submissions"] = []any{map[string]any{"submission_key": "do-not-invent"}}
			case "unknown new":
				fresh = map[string]any{"corrupt": true}
				putInitMap(t, filepath.Join(root, ".claude/runtime-initialization.json"), map[string]any{"runtime_schema_version": "1.3.0"})
			}
			path := filepath.Join(root, ".claude/loop-state.json")
			putInitMap(t, path, fresh)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			inventory := recovery.Inventory{Inputs: []recovery.InventoryInput{{Kind: recovery.InputKindRuntimeState, Path: ".claude/loop-state.json", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
			seed, err := recoverySeedRuntime(root, inventory, time.Now().UTC())
			if mode == "legacy" || mode == "1.3 empty" {
				if err != nil || seed["schema_version"] != fresh["schema_version"] {
					t.Fatalf("format changed in reconstruction: %v", err)
				}
			} else if err == nil {
				t.Fatal("document reconstruction dropped or invented issued domain facts")
			} else if mode == "1.3 packet" || mode == "1.3 received" {
				if !strings.Contains(err.Error(), "recovery cannot reconstruct issued packet/reception indices from documents") {
					t.Fatalf("issued 1.3 facts failed for an unexpected reason: %v", err)
				}
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(data, after) {
				t.Fatal("recovery format selection mutated source")
			}
		})
	}
}

func TestPendingInitializationConflictDoesNotPublishMissingMembers(t *testing.T) {
	for _, conflict := range []string{"hook-decisions.jsonl", "runtime-initialization.json"} {
		t.Run(conflict, func(t *testing.T) {
			root := initFormatRoot(t)
			fresh, err := inactiveRuntimeState(root, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, ".claude/loop-init-pending.json")
			putInitMap(t, marker, initPending{SchemaVersion: "1.0.0", FreshState: fresh})
			conflictPath := filepath.Join(root, ".claude", conflict)
			if err := os.WriteFile(conflictPath, []byte("conflicting existing bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code == 0 {
				t.Fatal("known conflicting initialization member accepted")
			}
			for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
				if _, err := os.Stat(filepath.Join(root, ".claude", name)); !os.IsNotExist(err) {
					t.Fatal("known conflict partially published the Runtime pair")
				}
			}
			after, err := os.ReadFile(marker)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("conflict rewrote pending source")
			}
		})
	}
}
