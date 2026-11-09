package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/tests/fixtures/catalog"
)

const s9PreDispatchSeedName = "s9-repair-plan-before-dispatch"
const s9PreDispatchMetadata = ".claude/test-s9-pre-dispatch-seed.json"
const s9SeedOutputEnv = "LOOP_TEST_S9_SEED_OUTPUT"

type s9PreDispatchMetadataRecord struct {
	SourceRoot string            `json:"source_root"`
	Plan       repair.RepairPlan `json:"plan"`
	Division   string            `json:"division"`
}

// buildS9PreDispatchSeed stops before the actual dispatch, worktree and Plan.
// The original fixture uses simulated professional inputs and the original
// repair Writers; the saved files are not human decisions or completed work.
func buildS9PreDispatchSeed(t *testing.T, destination string) error {
	t.Helper()
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	// The original fixture stores this spelling of root in Runtime; resolving
	// host symlinks here can change it (for example /var versus /private/var).
	source := filepath.Clean(root)
	if !filepath.IsAbs(source) {
		return fmt.Errorf("S9 source root is not absolute: %q", source)
	}
	division, err := filepath.Rel(root, division)
	if err != nil || !filepath.IsLocal(division) {
		return fmt.Errorf("S9 fixture dispatch path is not local: %q: %v", division, err)
	}
	if err := catalog.CopyTree(root, destination); err != nil {
		return err
	}
	data, err := json.Marshal(s9PreDispatchMetadataRecord{SourceRoot: source, Plan: plan, Division: division})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, s9PreDispatchMetadata), append(data, '\n'), 0o600)
}

// TestPrepareS9PreDispatchFixtureSeed is invoked by the job's single catalog
// initializer in this test binary. It is otherwise skipped in ordinary tests.
// The output must be the empty private directory supplied by that initializer.
func TestPrepareS9PreDispatchFixtureSeed(t *testing.T) {
	output := os.Getenv(s9SeedOutputEnv)
	if output == "" {
		t.Skip("no S9 fixture seed requested")
	}
	if err := buildS9PreDispatchSeed(t, output); err != nil {
		t.Fatalf("build S9 pre-dispatch seed: %v", err)
	}
}

func s9PreDispatchCatalogForTest(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, local := catalog.ForTest(t)
	if local {
		if err := c.Prepare(s9PreDispatchSeedName, func(root string) error {
			return buildS9PreDispatchSeed(t, root)
		}); err != nil {
			t.Fatalf("prepare %s: %v", s9PreDispatchSeedName, err)
		}
	}
	return c
}

func s9PreDispatchFixtureFromCatalog(t *testing.T, c *catalog.Catalog) (string, repair.RepairPlan, string) {
	t.Helper()
	root := t.TempDir()
	if err := c.Materialize(s9PreDispatchSeedName, root); err != nil {
		t.Fatalf("materialize %s: %v", s9PreDispatchSeedName, err)
	}
	root, err := workspace.Canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := relocateS9PreDispatch(root)
	if err != nil {
		t.Fatalf("relocate S9 pre-dispatch fixture: %v", err)
	}
	return root, metadata.Plan, filepath.Join(root, metadata.Division)
}

// relocateS9PreDispatch changes only the machine-owned bound root in this
// particular seed. It does not rewrite arbitrary strings or professional data.
func relocateS9PreDispatch(root string) (s9PreDispatchMetadataRecord, error) {
	var metadata s9PreDispatchMetadataRecord
	path := filepath.Join(root, s9PreDispatchMetadata)
	raw, err := os.ReadFile(path)
	if err != nil {
		return metadata, fmt.Errorf("read seed metadata: %w", err)
	}
	if err := json.Unmarshal(raw, &metadata); err != nil || !filepath.IsAbs(metadata.SourceRoot) || metadata.SourceRoot == root || !filepath.IsLocal(metadata.Division) || len(metadata.Plan.Assignments) != 2 {
		return metadata, errors.New("invalid S9 pre-dispatch seed metadata")
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		return metadata, fmt.Errorf("read S9 state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(stateRaw))
	decoder.UseNumber()
	var state map[string]any
	if err := decoder.Decode(&state); err != nil || state == nil {
		return metadata, fmt.Errorf("decode S9 state: %v", err)
	}
	bound, _ := state["bound_req"].(map[string]any)
	boundWorkspace, _ := bound["workspace"].(map[string]any)
	if boundWorkspace == nil || boundWorkspace["project_root"] != metadata.SourceRoot {
		return metadata, errors.New("S9 seed bound workspace does not match source root")
	}
	boundWorkspace["project_root"] = root
	stateRaw, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return metadata, err
	}
	info, err := os.Stat(statePath)
	if err != nil {
		return metadata, err
	}
	if err := os.WriteFile(statePath, append(stateRaw, '\n'), info.Mode().Perm()); err != nil {
		return metadata, err
	}
	if _, err := runtime.NewStore(statePath, filepath.Join(root, ".claude", "loop-events.jsonl")).Snapshot(); err != nil {
		return metadata, fmt.Errorf("read relocated S9 Runtime: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, metadata.Division)); err != nil {
		return metadata, fmt.Errorf("read relocated S9 division: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func TestS9PreDispatchCatalogIsolationAndOriginalConsumer(t *testing.T) {
	c := s9PreDispatchCatalogForTest(t)
	firstRoot, plan, division := s9PreDispatchFixtureFromCatalog(t, c)
	secondRoot, _, _ := s9PreDispatchFixtureFromCatalog(t, c)
	secondBefore := repairDispatchRuntimeBytes(t, secondRoot)

	// Dispatch and the worker checkout must be produced separately for the
	// instance under test, not replayed as completed seed facts.
	var issued string
	root, worker, packet, input := nativeS9PlanOnlyWithPlanning(t, firstRoot, plan, division, &issued)
	if issued == "" {
		t.Fatal("original S9 dispatch did not return its feedback")
	}
	nativeS9AssertPlanOnlyBoundary(t, root, worker, packet, false)
	s9ReferencePublicHook(t, root, input)
	nativeS9AssertPlanOnlyBoundary(t, root, worker, packet, true)
	repairDispatchAssertUnchanged(t, secondRoot, secondBefore)
	// A fresh copy of the unchanged seed cannot inherit the first instance's
	// real Git worktree, branch, packet or receipt.
	thirdRoot, thirdPlan, thirdDivision := s9PreDispatchFixtureFromCatalog(t, c)
	if branches := repairLeadGit(t, thirdRoot, "branch", "--list", "native-s9-plan-only"); branches != "" {
		t.Fatalf("seed inherited another instance's branch: %s", branches)
	}
	if !reflect.DeepEqual(plan, thirdPlan) || thirdDivision != filepath.Join(thirdRoot, mustS9DivisionRel(t, thirdRoot, thirdDivision)) {
		t.Fatal("fresh S9 instance lost its pre-dispatch metadata")
	}
}

func mustS9DivisionRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("non-local S9 division path %q: %v", path, err)
	}
	return rel
}

func TestS9PreDispatchCoordinatesRejectWrongRootAndKeepUnknownFields(t *testing.T) {
	// This case specifically tests relocation while the original root is
	// still present, so it must own the source fixture for its whole lifetime.
	// A job-provided seed's generating subprocess has already exited.
	c, err := catalog.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Prepare(s9PreDispatchSeedName, func(root string) error {
		return buildS9PreDispatchSeed(t, root)
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := c.Materialize(s9PreDispatchSeedName, root); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(root, s9PreDispatchMetadata)
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata s9PreDispatchMetadataRecord
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(metadata.SourceRoot); err != nil {
		t.Fatalf("original root must still exist during relocation: %v", err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	decoder := json.NewDecoder(bytes.NewReader(stateRaw))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil {
		t.Fatal(err)
	}
	// Professional text and unknown fields must never be globally rewritten.
	state["test_unknown_field"] = "preserve " + metadata.SourceRoot
	changed, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := relocateS9PreDispatch(root); err != nil {
		t.Fatal(err)
	}
	relocated, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(relocated, &after); err != nil || after["test_unknown_field"] != "preserve "+metadata.SourceRoot {
		t.Fatalf("unknown field changed during relocation: %v, %v", after["test_unknown_field"], err)
	}

	badRoot := t.TempDir()
	if err := c.Materialize(s9PreDispatchSeedName, badRoot); err != nil {
		t.Fatal(err)
	}
	metadata.SourceRoot = filepath.Join(t.TempDir(), "wrong-original")
	bad, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRoot, s9PreDispatchMetadata), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := relocateS9PreDispatch(badRoot); err == nil {
		t.Fatal("accepted a seed with an unrelated original root")
	}
}
