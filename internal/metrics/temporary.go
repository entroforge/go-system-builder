package metrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// The current writer uses this reserved name, while earlier CreateTemp-based
// writers used a canonical uint32 decimal suffix. This namespace identifies
// storage only; it is not proof of ownership or permission to remove a file.
var currentTemporaryPath = regexp.MustCompile(`^\.claude/\.loop-metrics-v1-[0-9a-f]{32}\.tmp$`)
var legacyTemporaryPath = regexp.MustCompile(`^\.claude/\.loop-metrics-(0|[1-9][0-9]{0,9})\.tmp$`)

// IsTemporaryPath matches the producer's exact current or historical name.
// Consumers must separately establish their own status and ownership rules.
func IsTemporaryPath(path string) bool {
	if currentTemporaryPath.MatchString(path) {
		return true
	}
	match := legacyTemporaryPath.FindStringSubmatch(path)
	if match == nil {
		return false
	}
	_, err := strconv.ParseUint(match[1], 10, 32)
	return err == nil
}

// RecognizeTemporary checks only the exact name and its filesystem shape.
// The .claude parent must exist and be a real directory; only the leaf may
// disappear between status and inspection as a result of atomic rename.
// It never reads file contents or changes the filesystem. The caller must
// establish untracked status and protect all previously owned paths first.
func RecognizeTemporary(ctx context.Context, root, path string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !IsTemporaryPath(path) {
		return false, nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer r.Close()
	before, err := r.Lstat(".claude")
	if errors.Is(err, os.ErrNotExist) {
		return false, ctx.Err()
	}
	if err != nil {
		return false, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return false, ctx.Err()
	}
	dir, err := r.OpenRoot(".claude")
	if errors.Is(err, os.ErrNotExist) {
		return false, ctx.Err()
	}
	if err != nil {
		return false, err
	}
	defer dir.Close()
	opened, err := dir.Stat(".")
	if err != nil {
		return false, err
	}
	if !os.SameFile(before, opened) {
		return false, ctx.Err()
	}
	info, statErr := dir.Lstat(strings.TrimPrefix(path, ".claude/"))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	// Recheck the parent itself: a missing/replaced directory is not a leaf
	// rename, even when the pinned directory handle is still readable.
	after, err := r.Lstat(".claude")
	if errors.Is(err, os.ErrNotExist) {
		return false, ctx.Err()
	}
	if err != nil {
		return false, fmt.Errorf("recheck metrics temporary parent: %w", err)
	}
	if !after.IsDir() || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		return false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return errors.Is(statErr, os.ErrNotExist) || info.Mode().IsRegular(), nil
}
