package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/recovery"
)

func TestResolveRecoveryPathRejectsSymlinkOutsideRepository(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "candidate.json")
	if err := os.WriteFile(outsideFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "candidate.json")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolveRecoveryPath(root, link); err == nil {
		t.Fatal("resolveRecoveryPath accepted a symlink outside the repository")
	}
}

func TestMergeRecoveryProjectionPreservesDurableBUGEntities(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", relative))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	seed, err := inactiveRuntimeState(root, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	seed["root"] = root
	seedData, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "candidate-state.json")
	if err := os.WriteFile(statePath, seedData, 0o600); err != nil {
		t.Fatal(err)
	}

	// This is the shape produced by recovery.Import for a document-derived
	// BUG: it is durable data, but has no live finder/lease to revive.
	imported := recovery.ImportResult{
		Entities: map[string][]map[string]any{
			"agents": {},
			"tasks":  {},
			"bugs": {{
				"id":                          "BUG-099",
				"state":                       "accepted",
				"path":                        "docs/reports/bugs/BUG-099.md",
				"severity":                    "P3",
				"attempt_count":               0,
				"same_contract_failure_count": 0,
				"original_finder_agent_ids":   []any{},
			}},
			"teams": {},
		},
	}
	candidate := seed
	mergeRecoveryProjectionState(candidate, root, imported)
	entities, ok := candidate["entities"].(map[string]any)
	if !ok {
		t.Fatalf("candidate entities missing: %#v", candidate["entities"])
	}
	bugs, ok := entities["bugs"].([]any)
	if !ok || len(bugs) != 1 {
		t.Fatalf("durable BUG entity was dropped: %#v", entities["bugs"])
	}
	bug, _ := bugs[0].(map[string]any)
	if bug["id"] != "BUG-099" || bug["path"] != "docs/reports/bugs/BUG-099.md" {
		t.Fatalf("preserved BUG entity = %#v", bug)
	}
	for _, field := range []string{"agents", "teams"} {
		values, ok := entities[field].([]any)
		if !ok || len(values) != 0 {
			t.Fatalf("transient %s records must remain empty: %#v", field, entities[field])
		}
	}
}
