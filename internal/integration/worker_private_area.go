package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// WorkerPrivateAreaSpec is a call-only projection of one original native
// responsibility. The caller proves the area and frozen execution; no scratch
// file is registered or given a content identity by this projection.
type WorkerPrivateAreaSpec struct {
	Path          string
	BaseCommit    string
	WritePaths    []string
	BaselinePaths []string
}

type WorkerPrivateAreaProjection struct {
	root, path string
	writePaths []string
	baseline   []string
	tracked    map[string]bool
}

// ObserveWorkerPrivateArea reads the exact base tree and current index once.
// Empty Path means no private-area authority and performs no Git operations.
func ObserveWorkerPrivateArea(ctx context.Context, workerRoot string, spec WorkerPrivateAreaSpec) (*WorkerPrivateAreaProjection, error) {
	if spec.Path == "" {
		return nil, nil
	}
	if !privateAreaRelativePath(spec.Path) || strings.TrimSpace(spec.BaseCommit) == "" {
		return nil, fmt.Errorf("invalid original Worker private area or base commit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index, err := defaultRunner.Run(ctx, workerRoot, "ls-files", "-z", "--", spec.Path)
	if err != nil {
		return nil, fmt.Errorf("inspect Worker private area index: %w", err)
	}
	base, err := defaultRunner.Run(ctx, workerRoot, "ls-tree", "-r", "--name-only", "-z", spec.BaseCommit, "--", spec.Path)
	if err != nil {
		return nil, fmt.Errorf("inspect Worker private area base: %w", err)
	}
	if len(index)+len(base) > cleanupInventoryLimit {
		return nil, fmt.Errorf("Worker private area tracked inventory exceeds %d bytes", cleanupInventoryLimit)
	}
	p := &WorkerPrivateAreaProjection{root: workerRoot, path: spec.Path, writePaths: append([]string(nil), spec.WritePaths...), baseline: append([]string(nil), spec.BaselinePaths...), tracked: map[string]bool{}}
	for _, output := range []string{index, base} {
		for _, rel := range strings.Split(output, "\x00") {
			if rel == "" {
				continue
			}
			if !privateAreaRelativePath(rel) || rel != spec.Path && !strings.HasPrefix(rel, spec.Path+"/") {
				return nil, fmt.Errorf("Git returned a path outside the original Worker private area")
			}
			p.tracked[rel] = true
		}
	}
	return p, ctx.Err()
}

func privateAreaRelativePath(path string) bool {
	return path != "" && filepath.IsLocal(path) && filepath.ToSlash(filepath.Clean(path)) == path && !strings.Contains(path, "\\")
}

// Allows classifies a single path, without reading or following its leaf. The
// caller must separately establish that Git reports it as untracked or ignored.
func (p *WorkerPrivateAreaProjection) Allows(rel string) (bool, error) {
	if p == nil || !privateAreaRelativePath(rel) || !strings.HasPrefix(rel, p.path+"/") {
		return false, nil
	}
	for _, scope := range p.writePaths {
		if scope == rel || workspace.PathMatchesScope(rel, scope) || strings.HasPrefix(scope, rel+"/") {
			return false, nil
		}
	}
	for _, path := range p.baseline {
		if path == rel || strings.HasPrefix(rel, path+"/") || strings.HasPrefix(path, rel+"/") {
			return false, nil
		}
	}
	for path := range p.tracked {
		if path == rel || strings.HasPrefix(rel, path+"/") || strings.HasPrefix(path, rel+"/") {
			return false, nil
		}
	}
	parent, name, err := workpacket.OpenParent(p.root, rel, false)
	if err != nil {
		return false, err
	}
	defer parent.Close()
	if _, err := parent.Lstat(name); err != nil {
		return false, err
	}
	return true, nil
}

// removeWorkerPrivateAreaScratch runs only after the original verified, ACKed
// cleanup_pending checkpoint and proof of all known originals. Enumerate the
// actual untracked/ignored paths; never follow a symlink leaf or force-remove a
// checkout. The workspace lock serializes framework callers, not arbitrary
// external writers, so this is not an atomic inventory against such writers.
func removeWorkerPrivateAreaScratch(ctx context.Context, root string, area *WorkerPrivateAreaProjection, known map[string]string) error {
	if area == nil {
		return nil
	}
	status, err := defaultRunner.Run(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all", "--", area.path)
	if err != nil {
		return fmt.Errorf("inspect private scratch before cleanup: %w", err)
	}
	ignored, err := defaultRunner.Run(ctx, root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--", area.path)
	if err != nil {
		return fmt.Errorf("inspect ignored private scratch before cleanup: %w", err)
	}
	if len(status)+len(ignored) > cleanupInventoryLimit {
		return fmt.Errorf("private scratch cleanup inventory exceeds %d bytes", cleanupInventoryLimit)
	}
	paths := map[string]bool{}
	for _, entry := range strings.Split(status, "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 4 || entry[:3] != "?? " {
			return fmt.Errorf("preserve tracked or staged change in original Worker private area")
		}
		paths[entry[3:]] = true
	}
	for _, rel := range strings.Split(ignored, "\x00") {
		if rel != "" {
			paths[rel] = true
		}
	}
	if len(paths) > 10000 {
		return fmt.Errorf("private scratch cleanup inventory exceeds 10000 paths")
	}
	ordered := make([]string, 0, len(paths))
	for rel := range paths {
		ordered = append(ordered, rel)
	}
	sort.Strings(ordered)
	for _, rel := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		allowed, err := area.Allows(rel)
		if err != nil {
			return fmt.Errorf("inspect private scratch %s: %w", rel, err)
		}
		if !allowed {
			return fmt.Errorf("preserve product or unrelated Worker path %s", rel)
		}
		if digest := known[rel]; digest != "" && !fileMatchesDigest(root, rel, digest) {
			return fmt.Errorf("preserve changed original or captured Worker file %s", rel)
		}
		parent, name, err := workpacket.OpenParent(root, rel, false)
		if err != nil {
			return err
		}
		err = parent.Remove(name)
		_ = parent.Close()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove original Worker private scratch %s: %w", rel, err)
		}
	}
	return nil
}
