package buildsurface

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var commitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type limitedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("Git output exceeds product baseline read budget")
	}
	return b.buffer.Write(p)
}

func gitBytes(root string, limit int, args ...string) ([]byte, error) {
	return gitBytesContext(context.Background(), root, limit, args...)
}

func gitBytesContext(parent context.Context, root string, limit int, args ...string) ([]byte, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "--no-pager", "--literal-pathspecs", "-c", "core.fsmonitor=false"}, args...)...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out := &limitedOutput{limit: limit}
	stderr := &limitedOutput{limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read product baseline Git %s: %w", args[0], err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out.buffer.Bytes(), nil
}

func gitText(root string, args ...string) (string, error) {
	return gitTextContext(context.Background(), root, args...)
}
func gitTextContext(ctx context.Context, root string, args ...string) (string, error) {
	b, err := gitBytesContext(ctx, root, 2<<20, args...)
	return strings.TrimSpace(string(b)), err
}
func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func safePath(p string) bool {
	return p != "" && p != "." && p != ".." && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "../") && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00")
}

// ProductPath excludes only the existing control and verification write
// surfaces. Specification/document changes remain part of the real diff.
// .loop-submissions/ is framework staging (S7 review answers, Worker drafts,
// packet publication temps), never product: an S7 answer landing there must
// not stale the round that issued it.
func ProductPath(p string) bool {
	for _, prefix := range []string{".git", ".claude", ".worktrees", "docs/reports", "e2e-workspace", ".loop-submissions"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return false
		}
	}
	return true
}

func treeBlob(root, commit, file string) (string, []byte, error) {
	return treeBlobContext(context.Background(), root, commit, file)
}
func treeBlobContext(ctx context.Context, root, commit, file string) (string, []byte, error) {
	if !commitPattern.MatchString(commit) || !safePath(file) {
		return "", nil, fmt.Errorf("Git blob requires exact commit and safe path")
	}
	b, err := fixedGitBytes(ctx, root, 2<<20, "ls-tree", "-z", commit, "--", file)
	if err != nil {
		return "", nil, err
	}
	if len(b) == 0 {
		return "", nil, fmt.Errorf("product blob %s missing at pinned commit", file)
	}
	records := bytes.Split(bytes.TrimSuffix(b, []byte{0}), []byte{0})
	if len(records) != 1 {
		return "", nil, fmt.Errorf("product blob path is ambiguous")
	}
	meta, name, ok := bytes.Cut(records[0], []byte{'\t'})
	parts := strings.Fields(string(meta))
	if !ok || string(name) != file || len(parts) != 3 || parts[1] != "blob" || (parts[0] != "100644" && parts[0] != "100755") || !commitPattern.MatchString(parts[2]) {
		return "", nil, fmt.Errorf("product path is not an exact regular Git blob: %s", file)
	}
	data, err := fixedGitBytes(ctx, root, 16<<20, "cat-file", "blob", parts[2])
	return parts[2], data, err
}

func changedPaths(root, start, final string) ([]Path, error) {
	return changedPathsContext(context.Background(), root, start, final)
}
func changedPathsContext(ctx context.Context, root, start, final string) ([]Path, error) {
	if !commitPattern.MatchString(start) || !commitPattern.MatchString(final) {
		return nil, fmt.Errorf("product baseline requires exact start/final commit IDs")
	}
	if _, err := fixedGitBytes(ctx, root, 4096, "merge-base", "--is-ancestor", start, final); err != nil {
		return nil, fmt.Errorf("product start commit is not an ancestor of final Main: %w", err)
	}
	b, err := fixedGitBytes(ctx, root, 2<<20, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", start, final, "--")
	if err != nil {
		return nil, err
	}
	paths := []Path{}
	if len(b) == 0 {
		return paths, nil
	}
	if b[len(b)-1] != 0 {
		return nil, fmt.Errorf("product diff is not NUL terminated")
	}
	parts := bytes.Split(b[:len(b)-1], []byte{0})
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("invalid product name/status diff")
	}
	for i := 0; i < len(parts); i += 2 {
		file, status := string(parts[i+1]), string(parts[i])
		if !safePath(file) {
			return nil, fmt.Errorf("unsafe literal product diff path")
		}
		if !ProductPath(file) {
			continue
		}
		row := Path{Path: file}
		commit := final
		switch status {
		case "A":
			row.Status = "added"
		case "M", "T":
			row.Status = "modified"
		case "D":
			row.Status = "deleted"
			row.PreimageCommit = start
			commit = start
		default:
			return nil, fmt.Errorf("unsupported product diff status %s", status)
		}
		oid, data, err := treeBlobContext(ctx, root, commit, file)
		if err != nil {
			return nil, err
		}
		row.BlobOID, row.SHA256 = oid, hashBytes(data)
		if status == "D" {
			deleted, err := fixedGitText(ctx, root, "log", "--format=%H", "--diff-filter=D", "--no-renames", "-1", start+".."+final, "--", file)
			if err != nil {
				return nil, err
			}
			if !commitPattern.MatchString(deleted) {
				return nil, fmt.Errorf("deleted product path has no real in-range deletion commit: %s", file)
			}
			if _, err := fixedGitBytes(ctx, root, 4096, "merge-base", "--is-ancestor", deleted, final); err != nil {
				return nil, err
			}
			row.DeletedByCommit = deleted
			parents, parentErr := fixedGitText(ctx, root, "show", "-s", "--format=%P", deleted)
			parentIDs := strings.Fields(parents)
			if parentErr != nil {
				return nil, parentErr
			}
			if len(parentIDs) == 0 || !commitPattern.MatchString(parentIDs[0]) {
				return nil, fmt.Errorf("deleted product path has no exact preimage commit: %s", file)
			}
			preOID, preBytes, preErr := treeBlobContext(ctx, root, parentIDs[0], file)
			if preErr != nil {
				return nil, preErr
			}
			deletedEntry, deletionErr := fixedGitBytes(ctx, root, 4096, "ls-tree", "-z", deleted, "--", file)
			if deletionErr != nil {
				return nil, deletionErr
			}
			if len(deletedEntry) != 0 {
				return nil, fmt.Errorf("claimed deletion commit retains product path: %s", file)
			}
			row.PreimageCommit, row.BlobOID, row.SHA256 = parentIDs[0], preOID, hashBytes(preBytes)
			finalEntry, err := fixedGitBytes(ctx, root, 4096, "ls-tree", "-z", final, "--", file)
			if err != nil {
				return nil, err
			}
			if len(finalEntry) != 0 {
				return nil, fmt.Errorf("deleted product path still exists at final Main: %s", file)
			}
		}
		paths = append(paths, row)
		if len(paths) > 10000 {
			return nil, fmt.Errorf("product path set exceeds read budget")
		}
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
	return paths, nil
}

func verifyCheckout(root, final string) error {
	return verifyCheckoutContext(context.Background(), root, final)
}
func verifyCheckoutContext(ctx context.Context, root, final string) error {
	head, err := gitTextContext(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err
	}
	if head != final {
		return fmt.Errorf("Main checkout HEAD differs from pinned final product commit")
	}
	b, err := gitBytesContext(ctx, root, 2<<20, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return err
	}
	for _, row := range bytes.Split(b, []byte{0}) {
		if len(row) == 0 {
			continue
		}
		if len(row) < 4 {
			return fmt.Errorf("invalid product checkout status")
		}
		file := string(row[3:])
		// Git represents an untracked nested repository as a directory even
		// with --untracked-files=all. Remove only that status marker before
		// validating the literal path and applying the existing product view.
		if string(row[:2]) == "??" {
			file = strings.TrimSuffix(file, "/")
		}
		if !safePath(file) {
			return fmt.Errorf("unsafe product checkout status path: %q", file)
		}
		if ProductPath(file) {
			return fmt.Errorf("product checkout differs from pinned Main: %s", file)
		}
	}
	return nil
}
