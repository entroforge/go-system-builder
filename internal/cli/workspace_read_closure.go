package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const assignmentReadEntryLimit = 256

// freezeAssignmentReadClosure resolves canonical Assignment read declarations
// against the bound development branch and returns the selected source paths.
func freezeAssignmentReadClosure(ctx context.Context, root string, state map[string]any, declarations []string, originalCommit string) (*fileview.View, []string, map[string]string, error) {
	if len(declarations) == 0 {
		return nil, nil, map[string]string{}, nil
	}
	if len(declarations) > assignmentReadEntryLimit {
		return nil, nil, nil, fmt.Errorf("Assignment read declarations exceed %d entries", assignmentReadEntryLimit)
	}
	// Existing paths are used as locations, not an OID-based proof. The
	// professional owner remains responsible for the actual task inputs.
	branch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return nil, nil, nil, err
	}
	ref := "refs/heads/" + branch
	view, err := fileview.New(root, ref, []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pin Assignment read sources: %w", err)
	}

	selected := map[string]bool{}
	seenEntries := map[string]bool{}
	entryCount := 0
	noteEntry := func(rel string) error {
		if rel == "." || seenEntries[rel] {
			return nil
		}
		seenEntries[rel] = true
		entryCount++
		if entryCount > assignmentReadEntryLimit {
			return fmt.Errorf("Assignment read closure exceeds %d Git entries", assignmentReadEntryLimit)
		}
		return nil
	}
	seenDeclarations := map[string]bool{}
	for _, raw := range declarations {
		pattern, directory, err := normalizeAssignmentReadPattern(raw)
		if err != nil {
			return nil, nil, nil, err
		}
		if seenDeclarations[pattern] {
			return nil, nil, nil, fmt.Errorf("duplicate Assignment read declaration %q", raw)
		}
		seenDeclarations[pattern] = true
		if !directory && !strings.Contains(pattern, "*") {
			parent := path.Dir(pattern)
			entries, parentErr := view.ReadDir(parent)
			if parentErr != nil {
				return nil, nil, nil, fmt.Errorf("Assignment read source is missing: %s", pattern)
			}
			foundDirectory := false
			for _, entry := range entries {
				if entry.Name() != path.Base(pattern) {
					continue
				}
				if err := noteEntry(pattern); err != nil {
					return nil, nil, nil, err
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return nil, nil, nil, fmt.Errorf("Assignment read source refuses Git symlink: %s", pattern)
				}
				if entry.IsDir() {
					foundDirectory = true
				} else {
					selected[pattern] = true
				}
				break
			}
			if selected[pattern] {
				continue
			}
			if !foundDirectory {
				return nil, nil, nil, fmt.Errorf("Assignment read source is missing: %s", pattern)
			}
		}

		walkRoot := assignmentReadWalkRoot(pattern)
		if walkRoot != "." {
			if _, err := view.ReadDir(walkRoot); err != nil {
				return nil, nil, nil, fmt.Errorf("Assignment read directory is missing or not a Git tree: %s", walkRoot)
			}
			if err := noteEntry(walkRoot); err != nil {
				return nil, nil, nil, err
			}
		}
		matches := 0
		if err := walkAssignmentReadTree(view, walkRoot, pattern, noteEntry, selected, &matches); err != nil {
			return nil, nil, nil, err
		}
		if matches == 0 {
			return nil, nil, nil, fmt.Errorf("Assignment read declaration has no regular source files: %s", raw)
		}
	}

	paths := make([]string, 0, len(selected))
	for rel := range selected {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	locations := make(map[string]string, len(paths))
	for _, rel := range paths {
		locations[rel] = rel
	}

	return view, paths, locations, nil
}

func normalizeAssignmentReadPattern(raw string) (pattern string, directory bool, err error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasPrefix(raw, "/") ||
		strings.ContainsAny(raw, "\\\x00\r\n") {
		return "", false, fmt.Errorf("invalid Assignment read declaration %q", raw)
	}
	directory = strings.HasSuffix(raw, "/")
	pattern = raw
	if directory {
		pattern = strings.TrimSuffix(pattern, "/")
	}
	if pattern == "" || strings.HasSuffix(pattern, "/") || filepath.IsAbs(filepath.FromSlash(pattern)) {
		return "", false, fmt.Errorf("invalid Assignment read declaration %q", raw)
	}
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || i == 0 && strings.Contains(part, ":") {
			return "", false, fmt.Errorf("Assignment read declaration escapes or is not canonical: %q", raw)
		}
	}
	if path.Clean(pattern) != pattern {
		return "", false, fmt.Errorf("Assignment read declaration is not canonical: %q", raw)
	}
	return pattern, directory, nil
}

func assignmentReadWalkRoot(pattern string) string {
	wildcard := strings.IndexByte(pattern, '*')
	if wildcard < 0 {
		return pattern
	}
	prefix := pattern[:wildcard]
	slash := strings.LastIndexByte(prefix, '/')
	if slash < 0 || slash == 0 {
		return "."
	}
	return prefix[:slash]
}

func walkAssignmentReadTree(view *fileview.View, directory, pattern string, noteEntry func(string) error, selected map[string]bool, matches *int) error {
	entries, err := view.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("cannot expand Assignment read source %s: %w", pattern, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if strings.ContainsAny(entry.Name(), "\\\x00\r\n") {
			return fmt.Errorf("Assignment read expansion contains a non-canonical Git path segment")
		}
		rel := entry.Name()
		if directory != "." {
			rel = directory + "/" + entry.Name()
		}
		if err := noteEntry(rel); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Assignment read expansion refuses Git symlink: %s", rel)
		}
		if entry.IsDir() {
			if err := walkAssignmentReadTree(view, rel, pattern, noteEntry, selected, matches); err != nil {
				return err
			}
			continue
		}
		if workspace.PathMatchesScope(rel, pattern) {
			selected[rel] = true
			(*matches)++
		}
	}
	return nil
}
