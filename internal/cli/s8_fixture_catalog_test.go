package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/tests/fixtures/catalog"
)

const s8FixtureSeedName = "s8-two-investigators-issued"
const s8FixtureMetadata = ".claude/test-s8-seed.json"
const s8FixturePrepareEnv = "LOOP_TEST_FIXTURE_CATALOG_PREPARE"
const s9CatalogSeedName = "s9-repair-plan-before-dispatch"
const s9CatalogSeedOutputEnv = "LOOP_TEST_S9_SEED_OUTPUT"

type s8FixtureSeedMetadata struct {
	SourceRoot string                              `json:"source_root"`
	Packets    []workpacket.HypothesisResultPacket `json:"packets"`
}

// prepareS8FixtureSeed uses the existing original S7/S8 Writer chain. The
// platform dispatch and professional finding are simulated fixture inputs;
// the issued packets and Case are real outputs of the existing test chain.
func prepareS8FixtureSeed(t *testing.T, c *catalog.Catalog) {
	t.Helper()
	if err := c.Prepare(s8FixtureSeedName, func(seedRoot string) error {
		return buildS8FixtureSeed(t, seedRoot)
	}); err != nil {
		t.Fatalf("prepare %s: %v", s8FixtureSeedName, err)
	}
}

func buildS8FixtureSeed(t *testing.T, seedRoot string) error {
	t.Helper()
	root, packets := s8TwoIssuedInvestigatorsFixture(t)
	source, err := workspace.Canonical(root)
	if err != nil {
		return err
	}
	if len(packets) != 2 {
		return fmt.Errorf("S8 fixture requires two issued packets, got %d", len(packets))
	}
	if err := catalog.CopyTree(root, seedRoot); err != nil {
		return err
	}
	// The seed stops at issuance. Each test authors the same synthetic
	// response in its private instance before exercising the real listener.
	for _, p := range packets {
		stageJSON(t, seedRoot, p.AnswerPath, p.Draft)
	}
	data, err := json.Marshal(s8FixtureSeedMetadata{SourceRoot: source, Packets: packets})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(seedRoot, s8FixtureMetadata), append(data, '\n'), 0o600)
}

// A catalog is initialized once, but each seed owns its result. A failed or
// filtered builder records its own error and never prevents later builders.
type jobFixtureSeed struct {
	name  string
	build func(*testing.T, string) error
}

func prepareJobFixtureSeeds(t *testing.T, c *catalog.Catalog, seeds []jobFixtureSeed) []error {
	t.Helper()
	var failures []error
	for _, seed := range seeds {
		seed := seed
		err := c.Prepare(seed.name, func(root string) error {
			var buildErr error
			ran := false
			ok := t.Run("build-"+seed.name, func(t *testing.T) {
				ran = true
				// A synchronous panic belongs to this seed. Preserve the stack
				// and let the initializer attempt the remaining seeds.
				defer func() {
					if value := recover(); value != nil {
						buildErr = fmt.Errorf("%s seed builder panicked: %v\n%s", seed.name, value, debug.Stack())
						t.Error(buildErr)
					}
				}()
				buildErr = seed.build(t, root)
				if buildErr != nil {
					t.Error(buildErr)
				}
			})
			if !ran {
				return fmt.Errorf("%s seed build subtest excluded by test selection", seed.name)
			}
			if !ok {
				if buildErr != nil {
					return fmt.Errorf("%s seed construction failed: %w", seed.name, buildErr)
				}
				return fmt.Errorf("%s seed construction failed; see initializer output", seed.name)
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// The S9 builder uses package cli private test helpers and cannot be called by
// cli_test. Run exactly that test in this test binary, with a bounded lifetime
// and a private output directory. No product API is exposed for fixtures.
func buildS9CatalogSeed(t *testing.T, root string) error {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	timeout := 20 * time.Minute
	if deadline, ok := t.Deadline(); ok {
		if remaining := time.Until(deadline) - 5*time.Second; remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return errors.New("no time remains to prepare the S9 seed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPrepareS9PreDispatchFixtureSeed$", "-test.timeout="+timeout.String(), "-test.v")
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != s8FixturePrepareEnv && key != catalog.Environment && key != s9CatalogSeedOutputEnv {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, s9CatalogSeedOutputEnv+"="+root)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("S9 fixture subprocess timed out after %s: %w; output:\n%s", timeout, ctx.Err(), output)
	}
	if err != nil {
		return fmt.Errorf("S9 fixture subprocess failed: %w; output:\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/test-s9-pre-dispatch-seed.json")); err != nil {
		return fmt.Errorf("S9 fixture subprocess did not publish its seed: %w; output:\n%s", err, output)
	}
	return nil
}

// TestPrepareFixtureCatalog is the explicit one-job initializer. Ordinary
// tests skip it; the caller supplies an empty directory with job lifetime.
func TestPrepareFixtureCatalog(t *testing.T) {
	root := os.Getenv(s8FixturePrepareEnv)
	if root == "" {
		t.Skip("no job fixture catalog requested")
	}
	c, err := catalog.Create(root)
	if err != nil {
		t.Fatalf("create fixture catalog: %v", err)
	}
	failures := prepareJobFixtureSeeds(t, c, []jobFixtureSeed{
		{name: s5FixtureSeedName, build: buildS5FixtureSeed},
		{name: s8FixtureSeedName, build: buildS8FixtureSeed},
		{name: s9CatalogSeedName, build: buildS9CatalogSeed},
	})
	if len(failures) != 0 {
		t.Errorf("fixture catalog preparation failed (%d seeds): %v", len(failures), failures)
	}
}

func TestFixtureCatalogRejectsFilteredGenerator(t *testing.T) {
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPrepareFixtureCatalog$/not-build$", "-test.v")
	cmd.Env = append(os.Environ(), s8FixturePrepareEnv+"="+root)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("filtered seed generator unexpectedly succeeded: %s", output)
	}
	c, err := catalog.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{s5FixtureSeedName, s8FixtureSeedName, s9CatalogSeedName} {
		want := name + " seed build subtest excluded by test selection"
		if !strings.Contains(string(output), want) {
			t.Fatalf("generator did not explain filtered %s: %s", name, output)
		}
		if err := c.Materialize(name, t.TempDir()); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("filtered build did not publish a failed %s seed: %v", name, err)
		}
	}
}

const fixtureFailureProbeEnv = "LOOP_TEST_FIXTURE_FAILURE_PROBE"

// The subprocess exercises a Fatal and a panic while the parent verifies that
// both are diagnosed and a later seed can still be consumed.
func TestFixtureCatalogFailureProbe(t *testing.T) {
	root := os.Getenv(fixtureFailureProbeEnv)
	if root == "" {
		t.Skip("fixture failure isolation probe not requested")
	}
	c, err := catalog.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	failures := prepareJobFixtureSeeds(t, c, []jobFixtureSeed{
		{name: "first-fails", build: func(t *testing.T, _ string) error { t.Fatal("deliberate seed failure"); return nil }},
		{name: "second-panics", build: func(_ *testing.T, _ string) error { panic("deliberate seed panic") }},
		{name: "third-succeeds", build: func(_ *testing.T, root string) error {
			return os.WriteFile(filepath.Join(root, "ready"), []byte("ok"), 0o600)
		}},
	})
	if len(failures) != 2 {
		t.Errorf("expected two seed failures, got %v", failures)
	}
}

func TestFixtureCatalogFailureDoesNotSkipLaterSeed(t *testing.T) {
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestFixtureCatalogFailureProbe$", "-test.v")
	cmd.Env = append(os.Environ(), fixtureFailureProbeEnv+"="+root)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "deliberate seed failure") {
		t.Fatalf("failure probe did not fail as expected: err=%v output=%s", err, output)
	}
	c, err := catalog.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Materialize("first-fails", t.TempDir()); err == nil || !strings.Contains(err.Error(), "first-fails seed construction failed") {
		t.Fatalf("failed seed did not retain its diagnostic: %v", err)
	}
	if err := c.Materialize("second-panics", t.TempDir()); err == nil || !strings.Contains(err.Error(), "deliberate seed panic") || !strings.Contains(err.Error(), "goroutine") {
		t.Fatalf("panicking seed did not retain its diagnostic and stack: %v", err)
	}
	third := t.TempDir()
	if err := c.Materialize("third-succeeds", third); err != nil {
		t.Fatalf("later independent seed was not prepared: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(third, "ready")); err != nil || string(data) != "ok" {
		t.Fatalf("later seed is incomplete: %q %v", data, err)
	}
}

func s8FixtureCatalogForTest(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, local := catalog.ForTest(t)
	if local {
		prepareS8FixtureSeed(t, c)
	}
	return c
}

func s8FixtureFromCatalog(t *testing.T, c *catalog.Catalog) (string, []workpacket.HypothesisResultPacket) {
	t.Helper()
	root := t.TempDir()
	if err := c.Materialize(s8FixtureSeedName, root); err != nil {
		t.Fatalf("materialize %s: %v", s8FixtureSeedName, err)
	}
	root, err := workspace.Canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := materializeS8FixtureCoordinates(root)
	if err != nil {
		t.Fatalf("materialize S8 fixture coordinates: %v", err)
	}
	for _, p := range packets {
		p.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"This particular boundary remains inconclusive."}`)
		stageJSON(t, root, p.AnswerPath, p.Draft)
	}
	return root, packets
}

// materializeS8FixtureCoordinates relocates only the known machine-owned
// coordinates in this particular seed. It never rewrites arbitrary strings,
// professional material, journal records, or unknown fields.
func materializeS8FixtureCoordinates(root string) ([]workpacket.HypothesisResultPacket, error) {
	metadataPath := filepath.Join(root, s8FixtureMetadata)
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("read seed metadata: %w", err)
	}
	var metadata s8FixtureSeedMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata.SourceRoot == "" || len(metadata.Packets) != 2 {
		return nil, errors.New("invalid S8 seed metadata")
	}
	if !filepath.IsAbs(metadata.SourceRoot) || metadata.SourceRoot == root {
		return nil, errors.New("invalid S8 seed source coordinate")
	}

	statePath := filepath.Join(root, ".claude/loop-state.json")
	refs, err := relocateS8StateFile(statePath, metadata.SourceRoot, root)
	if err != nil {
		return nil, err
	}
	checkpoints := filepath.Join(root, ".claude/runtime-checkpoints")
	if err := filepath.WalkDir(checkpoints, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "state.json" {
			_, err := relocateS8StateFile(path, metadata.SourceRoot, root)
			return err
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("relocate checkpoint: %w", err)
	}
	for ref := range refs {
		if !filepath.IsLocal(filepath.FromSlash(ref)) {
			return nil, fmt.Errorf("invalid S7 receipt reference %q", ref)
		}
		path := filepath.Join(root, filepath.FromSlash(ref))
		if err := relocateS8EvidenceFile(path, metadata.SourceRoot, root); err != nil {
			return nil, err
		}
	}
	// The copied audit index includes the old log path. It is only a cache:
	// its original reader rebuilds it from the retained audit log on mismatch.
	checkpoint, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil {
		return nil, fmt.Errorf("read relocated checkpoint index: %w", err)
	}
	if _, err := runtime.ReadRuntimeCheckpoint(root, checkpoint, semantic.RuntimeCandidateValidator{}); err != nil {
		return nil, fmt.Errorf("validate relocated checkpoint: %w", err)
	}
	if err := os.Remove(metadataPath); err != nil {
		return nil, fmt.Errorf("remove test-only seed metadata: %w", err)
	}
	return metadata.Packets, nil
}

func readS8FixtureObject(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("expected JSON object")
	}
	return value, nil
}

func writeS8FixtureObject(path string, value map[string]any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func s8Object(parent map[string]any, key string) map[string]any {
	value, _ := parent[key].(map[string]any)
	return value
}

func relocateS8Field(parent map[string]any, key, old, next string) error {
	if parent == nil {
		return nil
	}
	value, exists := parent[key]
	if !exists || value == nil {
		return nil
	}
	if value != old {
		return fmt.Errorf("coordinate %s has unexpected value %v", key, value)
	}
	parent[key] = next
	return nil
}

func relocateS8StateFile(path, old, next string) (map[string]bool, error) {
	state, err := readS8FixtureObject(path)
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	refs := make(map[string]bool)
	set := func(parent map[string]any, key, source, target string) error {
		return relocateS8Field(parent, key, source, target)
	}
	if err := set(state, "root", old, next); err != nil {
		return nil, err
	}
	if err := set(s8Object(s8Object(state, "bound_req"), "workspace"), "project_root", old, next); err != nil {
		return nil, err
	}
	ws := s8Object(state, "workspace")
	if err := set(ws, "main_root", old, next); err != nil {
		return nil, err
	}
	if err := set(ws, "common_dir", filepath.Join(old, ".git"), filepath.Join(next, ".git")); err != nil {
		return nil, err
	}
	receipt := func(row map[string]any) error {
		if ref, ok := row["result_ref"].(string); ok && ref != "" {
			refs[ref] = true
		}
		return set(row, "execution_root", old, next)
	}
	for _, item := range s8Object(s8Object(state, "review"), "assignments") {
		if row, ok := item.(map[string]any); ok {
			if err := receipt(s8Object(row, "result_receipt")); err != nil {
				return nil, err
			}
		}
	}
	if rows, ok := s8Object(state, "entities")["work_packets"].([]any); ok {
		for _, item := range rows {
			row, _ := item.(map[string]any)
			reception := s8Object(row, "review_result_reception")
			if err := set(reception, "execution_root", old, next); err != nil {
				return nil, err
			}
			if err := receipt(s8Object(reception, "receipt")); err != nil {
				return nil, err
			}
		}
	}
	if err := writeS8FixtureObject(path, state); err != nil {
		return nil, err
	}
	return refs, nil
}

func relocateS8EvidenceFile(path, old, next string) error {
	value, err := readS8FixtureObject(path)
	if err != nil {
		return fmt.Errorf("read S7 receipt %s: %w", path, err)
	}
	if err := relocateS8Field(value, "execution_root", old, next); err != nil {
		return err
	}
	if err := writeS8FixtureObject(path, value); err != nil {
		return fmt.Errorf("relocate S7 receipt %s: %w", path, err)
	}
	return nil
}

func TestS8FixtureCatalogIsolationAndCoordinates(t *testing.T) {
	c, err := catalog.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prepareS8FixtureSeed(t, c)
	before := t.TempDir()
	if err := c.Materialize(s8FixtureSeedName, before); err != nil {
		t.Fatal(err)
	}
	metadataRaw, err := os.ReadFile(filepath.Join(before, s8FixtureMetadata))
	if err != nil {
		t.Fatal(err)
	}
	var metadata s8FixtureSeedMetadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(metadata.SourceRoot); err != nil {
		t.Fatalf("original root must still exist for this test: %v", err)
	}
	seedState, err := os.ReadFile(filepath.Join(before, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, p := s8FixtureFromCatalog(t, c)
	second, q := s8FixtureFromCatalog(t, c)
	secondBefore, err := os.ReadFile(filepath.Join(second, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	task := fmt.Sprintf("This single synthetic actor is explicitly assigned the following original responsibilities.\nS8 responsibilities: investigate `%s` (owner `%s`). Background comparison only: investigate `%s` (owner `%s`).", p[0].Binding.HypothesisID, p[0].Binding.OwnerAgentID, p[1].Binding.HypothesisID, p[1].Binding.OwnerAgentID)
	s7s8MultipleOriginalEvent(t, first, "PostToolUse", task)
	firstService, err := submission.New(first)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := firstService.HypothesisResultReceipt(p[0].Binding.OwnerAgentID, p[0].ID)
	if err != nil || !accepted.Accepted {
		t.Fatalf("first instance did not receive original answer: %+v, %v", accepted, err)
	}
	secondService, err := submission.New(second)
	if err != nil {
		t.Fatal(err)
	}
	untouched, err := secondService.HypothesisResultReceipt(q[0].Binding.OwnerAgentID, q[0].ID)
	if err != nil || untouched.Accepted {
		t.Fatalf("receipt leaked into second instance: %+v, %v", untouched, err)
	}
	secondAfter, err := os.ReadFile(filepath.Join(second, ".claude/loop-state.json"))
	if err != nil || string(secondAfter) != string(secondBefore) {
		t.Fatalf("second instance changed after first event: %v", err)
	}
	after := t.TempDir()
	if err := c.Materialize(s8FixtureSeedName, after); err != nil {
		t.Fatal(err)
	}
	seedAfter, err := os.ReadFile(filepath.Join(after, ".claude/loop-state.json"))
	if err != nil || string(seedAfter) != string(seedState) {
		t.Fatalf("seed changed after instance event: %v", err)
	}
}

func TestS8FixtureCoordinatesRejectBrokenInputsAndPreserveUnknownFields(t *testing.T) {
	c, err := catalog.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prepareS8FixtureSeed(t, c)
	for _, mode := range []string{"mismatched-root", "missing-checkpoint", "unknown-field"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := c.Materialize(s8FixtureSeedName, root); err != nil {
				t.Fatal(err)
			}
			root, err = workspace.Canonical(root)
			if err != nil {
				t.Fatal(err)
			}
			metadataRaw, err := os.ReadFile(filepath.Join(root, s8FixtureMetadata))
			if err != nil {
				t.Fatal(err)
			}
			var metadata s8FixtureSeedMetadata
			if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, ".claude/loop-state.json")
			state, err := readS8FixtureObject(statePath)
			if err != nil {
				t.Fatal(err)
			}
			unknown := "professional text includes old root " + metadata.SourceRoot
			switch mode {
			case "mismatched-root":
				s8Object(state, "workspace")["main_root"] = filepath.Join(t.TempDir(), "unexpected")
			case "missing-checkpoint":
				if err := os.RemoveAll(filepath.Join(root, ".claude/runtime-checkpoints")); err != nil {
					t.Fatal(err)
				}
			case "unknown-field":
				state["fixture_unknown_professional_field"] = unknown
			}
			if err := writeS8FixtureObject(statePath, state); err != nil {
				t.Fatal(err)
			}
			_, err = materializeS8FixtureCoordinates(root)
			if mode != "unknown-field" {
				if err == nil {
					t.Fatal("broken fixture unexpectedly materialized")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err = readS8FixtureObject(statePath)
			if err != nil || state["fixture_unknown_professional_field"] != unknown {
				t.Fatalf("unknown field was modified: %v %v", state["fixture_unknown_professional_field"], err)
			}
		})
	}
}
