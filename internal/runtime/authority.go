package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrAuthorityMismatch = errors.New("runtime writer is outside the bound authority")

// validateWriteAuthority applies to both current and pending states. Legacy
// states without a workspace binding remain migratable; once bound, neither
// an alternate --root nor alternate state/journal paths create another writer.
func (s *Store) validateWriteAuthority(state map[string]any) error {
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["workspace"] == nil {
		return nil
	}
	workspace, _ := bound["workspace"].(map[string]any)
	declared, _ := workspace["project_root"].(string)
	if declared == "" || s.root == "" {
		return fmt.Errorf("%w: project_root is required", ErrAuthorityMismatch)
	}
	root, err := authorityPath(declared)
	if err != nil {
		return err
	}
	actualRoot, err := authorityPath(s.root)
	if err != nil {
		return err
	}
	if actualRoot != root {
		return fmt.Errorf("%w: use %s (received %s); use the REQ authority root %s", ErrAuthorityMismatch, root, actualRoot, root)
	}
	// Recovery replay operates on a caller-owned staging pair, but it must
	// still be rooted at the bound project. The explicit capability is the
	// only reason to skip the active state/journal coordinate check; ordinary
	// --state/--journal writers remain strict below.
	if s.offlineRecovery {
		activePaths := make([]string, 0, 2)
		for _, activePath := range []string{
			filepath.Join(root, ".claude", "loop-state.json"),
			filepath.Join(root, ".claude", "loop-events.jsonl"),
		} {
			active, err := authorityPath(activePath)
			if err != nil {
				return err
			}
			activePaths = append(activePaths, active)
		}
		paths := []struct {
			actualPath string
		}{
			{actualPath: s.statePath},
			{actualPath: s.journalPath},
		}
		canonical := make([]string, 0, len(paths))
		for _, pair := range paths {
			path := pair.actualPath
			actual, err := authorityPath(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, actual)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("%w: offline recovery pair %q is outside the REQ authority root %s", ErrAuthorityMismatch, actual, root)
			}
			for _, active := range activePaths {
				if actual == active {
					return fmt.Errorf("%w: offline recovery pair must not use active Runtime file %s", ErrAuthorityMismatch, active)
				}
			}
			// A directory alias is harmless because it still resolves to the
			// same recovery directory. A file alias would create an independent
			// lock/marker sibling and is therefore rejected even in recovery.
			parent, err := authorityPath(filepath.Dir(path))
			if err != nil {
				return err
			}
			lexical := filepath.Join(parent, filepath.Base(path))
			if actual != lexical {
				return fmt.Errorf("%w: offline recovery file aliases cannot own an independent lock", ErrAuthorityMismatch)
			}
			canonical = append(canonical, actual)
		}
		if canonical[0] == canonical[1] {
			return fmt.Errorf("%w: offline recovery state and journal must be different files", ErrAuthorityMismatch)
		}
		return nil
	}
	for _, pair := range [][2]string{
		{s.statePath, filepath.Join(root, ".claude", "loop-state.json")},
		{s.journalPath, filepath.Join(root, ".claude", "loop-events.jsonl")},
	} {
		actual, err := authorityPath(pair[0])
		if err != nil {
			return err
		}
		want := pair[1]
		if actual != want {
			return fmt.Errorf("%w: use %s (received %s); use the REQ authority root %s", ErrAuthorityMismatch, want, actual, root)
		}
		// Directory aliases of the project are safe; file aliases would
		// give the same state a different sibling .lock and pending markers.
		parent, err := authorityPath(filepath.Dir(pair[0]))
		if err != nil {
			return err
		}
		if filepath.Join(parent, filepath.Base(pair[0])) != want {
			return fmt.Errorf("%w: state/journal file aliases cannot own an independent lock", ErrAuthorityMismatch)
		}
	}
	return nil
}

// Resolve existing ancestors too: the journal need not have been created yet.
func authorityPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", err
	}
	realParent, err := authorityPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(realParent, filepath.Base(abs)), nil
}
