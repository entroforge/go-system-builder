package catalog

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// walk checks path shape. Links must resolve within the seed; dangling or
// cyclic links cannot yet be proven safe.
func walk(root string, visit func(path, rel string, info fs.FileInfo) error) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("seed root is not a directory: %s", root)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." && entry.Name() == ".git" {
			if rel != ".git" || !entry.IsDir() {
				return fmt.Errorf("unsupported Git worktree at %s", rel)
			}
			if err := validateGit(path); err != nil {
				return err
			}
		}
		if strings.HasPrefix(rel, ".git"+string(filepath.Separator)) && strings.HasSuffix(entry.Name(), ".lock") {
			return fmt.Errorf("active or stale Git lock in fixture: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir(), info.Mode().IsRegular():
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) || filepath.VolumeName(link) != "" {
				return fmt.Errorf("absolute fixture symlink %s -> %s", rel, link)
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(rel), link))
			if target != "." && !filepath.IsLocal(target) {
				return fmt.Errorf("escaping fixture symlink %s -> %s", rel, link)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve fixture symlink %s: %w", rel, err)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return err
			}
			resolvedRel, err := filepath.Rel(resolvedRoot, resolved)
			if err != nil || (resolvedRel != "." && !filepath.IsLocal(resolvedRel)) {
				return fmt.Errorf("escaping fixture symlink %s -> %s", rel, link)
			}
		default:
			return fmt.Errorf("unsupported fixture file type at %s", rel)
		}
		return visit(path, rel, info)
	})
}

// validateGit accepts only self-contained main repositories. Linked worktrees,
// external object stores, includes and external worktree locations cannot be
// materialized as private instances by copying their metadata.
func validateGit(gitDir string) error {
	for _, rel := range []string{"commondir", "objects/info/alternates", "objects/info/http-alternates"} {
		if _, err := os.Lstat(filepath.Join(gitDir, filepath.FromSlash(rel))); err == nil {
			return fmt.Errorf("unsupported shared Git metadata: %s", rel)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	worktrees, err := os.ReadDir(filepath.Join(gitDir, "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(worktrees) != 0 {
		return fmt.Errorf("fixture Git repository has linked worktrees")
	}
	config := filepath.Join(gitDir, "config")
	command := exec.Command("git", "config", "--no-includes", "--file", config, "--null", "--list")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read fixture Git config: %w", err)
	}
	for _, entry := range strings.Split(string(output), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if key == "core.worktree" || key == "include.path" || (strings.HasPrefix(key, "includeif.") && strings.HasSuffix(key, ".path")) {
			return fmt.Errorf("unsupported fixture Git config key %s", key)
		}
		if key == "core.bare" && strings.EqualFold(value, "true") {
			return fmt.Errorf("bare Git repository is not a fixture worktree")
		}
	}
	return nil
}

func validateTree(root string) error {
	return walk(root, func(_, _ string, _ fs.FileInfo) error { return nil })
}

func copyTree(source, destination string) error {
	// Restrictive source directories are applied after their children are
	// copied so read-only seeds can still be constructed on every platform.
	type directory struct {
		path string
		mode fs.FileMode
	}
	var directories []directory
	err := walk(source, func(path, rel string, info fs.FileInfo) error {
		if rel == "." {
			return nil
		}
		target := filepath.Join(destination, rel)
		switch {
		case info.IsDir():
			if err := os.Mkdir(target, 0o700); err != nil {
				return err
			}
			directories = append(directories, directory{target, info.Mode().Perm()})
			return nil
		case info.Mode().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return err
		}
	}
	return nil
}

// CopyTree copies a self-contained seed source into an empty caller-owned
// destination, with the same private-file and Git rules as Materialize.
func CopyTree(source, destination string) error {
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("fixture destination %q is not empty", destination)
	}
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	destinationReal, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return err
	}
	sourceReal, err = filepath.Abs(sourceReal)
	if err != nil {
		return err
	}
	destinationReal, err = filepath.Abs(destinationReal)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(sourceReal, destinationReal); err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return fmt.Errorf("fixture destination is inside its source")
	}
	if err := validateTree(source); err != nil {
		return err
	}
	return copyTree(source, destination)
}

func copyFile(source, destination string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
