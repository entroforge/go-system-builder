package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/tests/fixtures/catalog"
)

const s5FixtureSeedName = "s5-two-reviewers-issued"
const s5FixtureMetadata = ".claude/test-s5-seed.json"

type s5FixtureSeedMetadata struct {
	SourceRoot string              `json:"source_root"`
	TaskID     string              `json:"task_id"`
	TaskPath   string              `json:"task_path"`
	Packets    []workpacket.Packet `json:"packets"`
}

// buildS5FixtureSeed stops immediately after the original CLI registers the
// workgroup and the original Writer issues the reviewers' questionnaires.
// No professional answer or reception is prefilled by this seed.
func buildS5FixtureSeed(t *testing.T, destination string) error {
	t.Helper()
	f := newStageAnswerFixture(t)
	root, err := workspace.Canonical(f.root)
	if err != nil {
		return err
	}
	if len(f.packets) != 2 {
		return fmt.Errorf("S5 fixture has %d issued questionnaires, want two", len(f.packets))
	}
	if err := catalog.CopyTree(root, destination); err != nil {
		return err
	}
	data, err := json.Marshal(s5FixtureSeedMetadata{SourceRoot: root, TaskID: f.taskID, TaskPath: f.taskPath, Packets: f.packets})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, s5FixtureMetadata), append(data, '\n'), 0o600)
}

func s5FixtureCatalogForTest(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, local := catalog.ForTest(t)
	if local {
		if err := c.Prepare(s5FixtureSeedName, func(root string) error { return buildS5FixtureSeed(t, root) }); err != nil {
			t.Fatalf("prepare %s: %v", s5FixtureSeedName, err)
		}
	}
	return c
}

func s5FixtureFromCatalog(t *testing.T, c *catalog.Catalog) stageAnswerFixture {
	t.Helper()
	root := t.TempDir()
	if err := c.Materialize(s5FixtureSeedName, root); err != nil {
		t.Fatalf("materialize %s: %v", s5FixtureSeedName, err)
	}
	root, err := workspace.Canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := materializeS5FixtureCoordinates(root)
	if err != nil {
		t.Fatalf("materialize S5 fixture coordinates: %v", err)
	}
	return stageAnswerFixture{root: root, taskID: metadata.TaskID, taskPath: metadata.TaskPath, packets: metadata.Packets}
}

// materializeS5FixtureCoordinates changes only the known machine-owned root
// coordinates of this S5 seed. Professional content and unknown fields are
// retained as-is; the original consumer validates the resulting Runtime.
func materializeS5FixtureCoordinates(root string) (s5FixtureSeedMetadata, error) {
	var meta s5FixtureSeedMetadata
	path := filepath.Join(root, s5FixtureMetadata)
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, fmt.Errorf("read S5 seed metadata: %w", err)
	}
	if err := json.Unmarshal(data, &meta); err != nil || !filepath.IsAbs(meta.SourceRoot) || meta.SourceRoot == root || len(meta.Packets) != 2 {
		return meta, errors.New("invalid S5 seed metadata")
	}
	if err := relocateS5StateFile(filepath.Join(root, ".claude", "loop-state.json"), meta.SourceRoot, root); err != nil {
		return meta, err
	}
	checkpoints := filepath.Join(root, ".claude", "runtime-checkpoints")
	if err := filepath.WalkDir(checkpoints, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "state.json" {
			return relocateS5StateFile(path, meta.SourceRoot, root)
		}
		return nil
	}); err != nil {
		return meta, fmt.Errorf("relocate S5 checkpoint: %w", err)
	}
	checkpoint, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil {
		return meta, fmt.Errorf("read relocated S5 checkpoint: %w", err)
	}
	if _, err := runtime.ReadRuntimeCheckpoint(root, checkpoint, semantic.RuntimeCandidateValidator{}); err != nil {
		return meta, fmt.Errorf("validate relocated S5 checkpoint: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return meta, fmt.Errorf("remove S5 test metadata: %w", err)
	}
	return meta, nil
}

func relocateS5StateFile(path, oldRoot, newRoot string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read S5 state %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var state map[string]any
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode S5 state %s: %w", path, err)
	}
	if state == nil {
		return fmt.Errorf("decode S5 state %s: expected an object", path)
	}
	object := func(parent map[string]any, key string) map[string]any {
		if parent == nil {
			return nil
		}
		child, _ := parent[key].(map[string]any)
		return child
	}
	set := func(parent map[string]any, key, old, new string) error {
		if parent == nil {
			return nil
		}
		value, exists := parent[key]
		if !exists || value == nil {
			return nil
		}
		if value != old {
			return fmt.Errorf("S5 coordinate %s has unexpected value %v", key, value)
		}
		parent[key] = new
		return nil
	}
	if err := set(object(object(state, "bound_req"), "workspace"), "project_root", oldRoot, newRoot); err != nil {
		return err
	}
	workspaceState := object(state, "workspace")
	if err := set(workspaceState, "main_root", oldRoot, newRoot); err != nil {
		return err
	}
	if err := set(workspaceState, "common_dir", filepath.Join(oldRoot, ".git"), filepath.Join(newRoot, ".git")); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func TestS5FixtureCatalogKeepsInstancesAndSeedIndependent(t *testing.T) {
	catalogRoot := t.TempDir()
	c, err := catalog.Create(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	var sourceRoot string
	if err := c.Prepare(s5FixtureSeedName, func(destination string) error {
		if err := buildS5FixtureSeed(t, destination); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(destination, s5FixtureMetadata))
		if err != nil {
			return err
		}
		var metadata s5FixtureSeedMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			return err
		}
		sourceRoot = metadata.SourceRoot
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourceRoot); err != nil {
		t.Fatalf("original fixture must remain present during replay: %v", err)
	}
	seedRoot := filepath.Join(catalogRoot, "seeds", s5FixtureSeedName, "root")
	before := dryRunTree(t, seedRoot)
	first := s5FixtureFromCatalog(t, c)
	second := s5FixtureFromCatalog(t, c)
	packet := first.packets[0]
	stageJSON(t, first.root, packet.AnswerPath, completedDraft(packet, "pass"))
	if code, _, diagnostic := nativeReviewEvent(t, first.root, "SubagentStop", "actual-native-reviewer", filepath.Join(first.root, packet.ViewPath)); code != 0 {
		t.Fatalf("first private fixture must use the original Stop consumer: %d %s", code, diagnostic)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, first.root).State, packet.ID); err != nil || !accepted {
		t.Fatalf("first instance did not receive the answer: accepted=%t err=%v", accepted, err)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, second.root).State, second.packets[0].ID); err != nil || accepted {
		t.Fatalf("second instance changed: accepted=%t err=%v", accepted, err)
	}
	if after := dryRunTree(t, seedRoot); !reflect.DeepEqual(before, after) {
		t.Fatal("receiving an answer in the instance changed the catalog seed")
	}
}

func TestS5FixtureCoordinatesRejectBrokenInputsAndKeepUnknownFields(t *testing.T) {
	catalogRoot := t.TempDir()
	c, err := catalog.Create(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Prepare(s5FixtureSeedName, func(destination string) error {
		return buildS5FixtureSeed(t, destination)
	}); err != nil {
		t.Fatal(err)
	}
	t.Run("missing-checkpoints", func(t *testing.T) {
		root := t.TempDir()
		if err := c.Materialize(s5FixtureSeedName, root); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(root, ".claude", "runtime-checkpoints")); err != nil {
			t.Fatal(err)
		}
		if _, err := materializeS5FixtureCoordinates(root); err == nil {
			t.Fatal("missing original checkpoint must be reported")
		}
	})
	t.Run("wrong-root", func(t *testing.T) {
		root := t.TempDir()
		if err := c.Materialize(s5FixtureSeedName, root); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".claude", "loop-state.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		state["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = filepath.Join(root, "wrong")
		stageJSON(t, root, ".claude/loop-state.json", state)
		if _, err := materializeS5FixtureCoordinates(root); err == nil {
			t.Fatal("unexpected original root must be reported")
		}
	})
	t.Run("unknown-field", func(t *testing.T) {
		root := t.TempDir()
		if err := c.Materialize(s5FixtureSeedName, root); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".claude", "loop-state.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		state["test_unknown"] = "free-form contents that must stay untouched"
		stageJSON(t, root, ".claude/loop-state.json", state)
		if _, err := materializeS5FixtureCoordinates(root); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		if state["test_unknown"] != "free-form contents that must stay untouched" {
			t.Fatal("unknown state changed during S5 coordinate relocation")
		}
	})
}
