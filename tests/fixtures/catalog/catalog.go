// Package catalog stores job-local test seeds and materializes private copies.
// It is test infrastructure, not a product persistence or identity protocol.
//
// A catalog has exactly one initializer: Create returns the only writer, and
// that writer is used by the job's preparation process. Load and ForTest with
// an injected catalog return readers only. Parallel seed initialization by
// independent processes and recovery of an interrupted initializer are not
// supported; an unpublished temporary directory is never treated as a seed.
// The caller owns the catalog directory and its lifetime.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
)

const Environment = "LOOP_TEST_FIXTURE_CATALOG"

const marker = "fixture catalog v1\n"

var seedName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Catalog is a job-local seed collection. Published seeds are immutable via
// this API. A catalog returned by Load never permits publishing a seed.
type Catalog struct {
	root   string
	writer bool
	mu     sync.Mutex
}

type seedStatus struct {
	Error string `json:"error,omitempty"`
}

// Create initializes an empty, caller-owned directory. Its result is the only
// writer; it must finish all desired preparations before readers are started.
func Create(root string) (*Catalog, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("fixture catalog directory: %w", err)
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("fixture catalog directory %q is not empty", root)
	}
	// Atomic mkdir makes simultaneous initializers fail rather than share a
	// writer. This directory is retained as the initialization marker.
	if err := os.Mkdir(filepath.Join(root, ".catalog"), 0o700); err != nil {
		return nil, fmt.Errorf("initialize fixture catalog: %w", err)
	}
	if err := os.Mkdir(filepath.Join(root, "seeds"), 0o700); err != nil {
		return nil, fmt.Errorf("initialize fixture seeds: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".catalog", "format"), []byte(marker), 0o600); err != nil {
		return nil, fmt.Errorf("write fixture catalog marker: %w", err)
	}
	return &Catalog{root: root, writer: true}, nil
}

// Load opens an existing catalog for consumption; it never creates or repairs
// one. An explicitly injected missing or corrupt catalog must be reported.
func Load(root string) (*Catalog, error) {
	if root == "" {
		return nil, errors.New("fixture catalog path is empty")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".catalog", "format"))
	if err != nil {
		return nil, fmt.Errorf("open fixture catalog %q: %w", root, err)
	}
	if string(raw) != marker {
		return nil, fmt.Errorf("open fixture catalog %q: invalid format marker", root)
	}
	info, err := os.Stat(filepath.Join(root, "seeds"))
	if err != nil {
		return nil, fmt.Errorf("open fixture catalog seeds: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("open fixture catalog: seeds is not a directory")
	}
	return &Catalog{root: root}, nil
}

// ForTest uses the explicitly injected catalog, or creates a private local
// catalog. The bool is true only for the local catalog's initializer. Local
// callers must prepare their own seeds; no directory owned by another test is
// retained or automatically discovered.
func ForTest(t testing.TB) (*Catalog, bool) {
	t.Helper()
	if root, ok := os.LookupEnv(Environment); ok {
		c, err := Load(root)
		if err != nil {
			t.Fatalf("%s: %v", Environment, err)
		}
		return c, false
	}
	c, err := Create(t.TempDir())
	if err != nil {
		t.Fatalf("create local fixture catalog: %v", err)
	}
	return c, true
}

// Prepare invokes build once for an unbound, self-contained seed and records
// either its output or its error. No other seed is invalidated by a failure.
// The builder must not leave background writers running after it returns.
// An ordinary private Git repository can be copied, but linked worktrees,
// external Git objects and active locks cannot. Bound Runtime relocation is
// seed-specific and must be performed by the consumer after materialization.
func (c *Catalog) Prepare(name string, build func(root string) error) error {
	if !c.writer {
		return errors.New("cannot prepare seed through a read-only catalog")
	}
	if !seedName.MatchString(name) {
		return fmt.Errorf("invalid fixture seed name %q", name)
	}
	if build == nil {
		return fmt.Errorf("prepare fixture seed %q: nil builder", name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	parent := filepath.Join(c.root, "seeds")
	final := filepath.Join(parent, name)
	if _, err := os.Lstat(final); err == nil {
		return fmt.Errorf("fixture seed %q is already recorded", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect fixture seed %q: %w", name, err)
	}
	tmp, err := os.MkdirTemp(parent, ".preparing-")
	if err != nil {
		return fmt.Errorf("prepare fixture seed %q: %w", name, err)
	}
	defer os.RemoveAll(tmp)
	root := filepath.Join(tmp, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		return fmt.Errorf("prepare fixture seed %q: %w", name, err)
	}
	buildErr := build(root)
	if buildErr == nil {
		buildErr = validateTree(root)
	}
	status := seedStatus{}
	if buildErr != nil {
		status.Error = buildErr.Error()
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("discard failed fixture seed %q: %w", name, err)
		}
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode fixture seed %q status: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "status.json"), raw, 0o600); err != nil {
		return fmt.Errorf("record fixture seed %q status: %w", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("publish fixture seed %q: %w", name, err)
	}
	if buildErr != nil {
		return fmt.Errorf("prepare fixture seed %q: %w", name, buildErr)
	}
	return nil
}

// Materialize copies a published seed to an empty, caller-owned directory.
// It never hard-links mutable files. Destination cleanup belongs to the caller.
func (c *Catalog) Materialize(name, destination string) error {
	if !seedName.MatchString(name) {
		return fmt.Errorf("invalid fixture seed name %q", name)
	}
	seed := filepath.Join(c.root, "seeds", name)
	raw, err := os.ReadFile(filepath.Join(seed, "status.json"))
	if err != nil {
		return fmt.Errorf("open fixture seed %q status: %w", name, err)
	}
	var status seedStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("decode fixture seed %q status: %w", name, err)
	}
	if status.Error != "" {
		return fmt.Errorf("fixture seed %q preparation failed: %s", name, status.Error)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return fmt.Errorf("open destination for fixture seed %q: %w", name, err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("destination for fixture seed %q is not empty", name)
	}
	if err := CopyTree(filepath.Join(seed, "root"), destination); err != nil {
		return fmt.Errorf("materialize fixture seed %q: %w", name, err)
	}
	return nil
}
