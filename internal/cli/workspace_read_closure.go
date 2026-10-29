package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const (
	assignmentReadEntryLimit = 256
	assignmentReadByteLimit  = 4 << 20
)

// freezeAssignmentReadClosure resolves canonical Assignment read declarations
// against the bound development branch. It returns exact file paths and hashes
// from one pinned Git tree only after checking that the Main checkout has the
// same regular-file bytes.
func freezeAssignmentReadClosure(ctx context.Context, root string, state map[string]any, declarations []string, originalCommit string) (*fileview.View, []string, map[string]string, error) {
	if len(declarations) == 0 {
		return nil, nil, map[string]string{}, nil
	}
	if len(declarations) > assignmentReadEntryLimit {
		return nil, nil, nil, fmt.Errorf("Assignment read declarations exceed %d entries", assignmentReadEntryLimit)
	}
	ref := originalCommit
	if ref == "" {
		branch, err := fileview.DevelopmentRef(state)
		if err != nil {
			return nil, nil, nil, err
		}
		ref = "refs/heads/" + branch
	}
	view, err := fileview.New(root, ref, []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pin Assignment read sources: %w", err)
	}
	if originalCommit == "" {
		head, err := workspace.Git(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			return nil, nil, nil, err
		}
		if head != view.Commit {
			return nil, nil, nil, fmt.Errorf("Assignment read sources are not from the prepared development HEAD")
		}
	} else if originalCommit != view.Commit {
		return nil, nil, nil, fmt.Errorf("existing execution has an invalid original Assignment source commit")
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
	hashes := make(map[string]string, len(paths))
	sizes, err := assignmentReadBlobSizes(ctx, root, view.Commit, paths)
	if err != nil {
		return nil, nil, nil, err
	}
	totalBytes := int64(0)
	for _, size := range sizes {
		totalBytes += size
		if totalBytes > assignmentReadByteLimit {
			return nil, nil, nil, fmt.Errorf("Assignment read closure exceeds %d bytes", assignmentReadByteLimit)
		}
	}
	for i, rel := range paths {
		data, err := view.ReadFile(rel)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Assignment read source is not a regular Git blob: %s: %w", rel, err)
		}
		if int64(len(data)) != sizes[i] {
			return nil, nil, nil, fmt.Errorf("pinned Git blob size changed while reading Assignment source: %s", rel)
		}
		if err := verifyAssignmentReadCheckout(root, rel, data); err != nil {
			return nil, nil, nil, err
		}
		hashes[rel] = workpacket.Digest(data)
	}
	if err := view.Verify(); err != nil {
		return nil, nil, nil, err
	}
	return view, paths, hashes, nil
}

func assignmentReadBlobSizes(ctx context.Context, root, commit string, paths []string) ([]int64, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var input strings.Builder
	for _, rel := range paths {
		if strings.ContainsAny(rel, "\x00\r\n") {
			return nil, fmt.Errorf("Assignment read source name cannot be represented in the bounded Git query")
		}
		input.WriteString(commit)
		input.WriteByte(':')
		input.WriteString(rel)
		input.WriteByte('\n')
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(queryCtx, "git", "-C", root, "cat-file", "--batch-check=%(objecttype) %(objectsize)")
	command.Stdin = strings.NewReader(input.String())
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("size pinned Assignment Git blobs: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != len(paths) {
		return nil, fmt.Errorf("pinned Assignment Git blob size query returned an incomplete closure")
	}
	sizes := make([]int64, len(lines))
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "blob" {
			return nil, fmt.Errorf("Assignment read source is not a regular Git blob: %s", paths[i])
		}
		sizes[i], err = strconv.ParseInt(fields[1], 10, 64)
		if err != nil || sizes[i] < 0 {
			return nil, fmt.Errorf("invalid pinned Assignment Git blob size: %s", paths[i])
		}
	}
	return sizes, nil
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

func verifyAssignmentReadCheckout(root, rel string, pinned []byte) error {
	if filepath.IsAbs(filepath.FromSlash(rel)) {
		return fmt.Errorf("Assignment read source is absolute: %s", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Assignment read source escapes Main: %s", rel)
	}
	current := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("Assignment read source is absent from Main checkout: %s", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Assignment read source refuses checkout symlink: %s", rel)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("Assignment read source parent is not a directory: %s", rel)
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("Assignment read source is not a regular checkout file: %s", rel)
		}
		if i == len(parts)-1 && info.Size() != int64(len(pinned)) {
			return fmt.Errorf("Assignment read source checkout differs from pinned Git blob: %s", rel)
		}
	}
	actual, err := os.ReadFile(full)
	if err != nil || !bytes.Equal(actual, pinned) {
		return fmt.Errorf("Assignment read source checkout differs from pinned Git blob: %s", rel)
	}
	return nil
}
