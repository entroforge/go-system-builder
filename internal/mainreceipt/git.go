package mainreceipt

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

const gitBlobBundleLimit = 64 << 20

type rawChange struct {
	path      string
	status    string
	oldObject string
	newObject string
}

func verifyGitContribution(ctx context.Context, root string, cp checkpoint, facts assignmentresult.MainFacts, finalHead string) ([]ChangedFile, error) {
	if err := verifyMerge(ctx, root, cp, finalHead); err != nil {
		return nil, err
	}
	if !fullObjectID(facts.TestedHead) {
		return nil, fmt.Errorf("tested head is not a full Git object id")
	}
	if err := verifyCommit(ctx, root, facts.TestedHead); err != nil {
		return nil, fmt.Errorf("resolve tested head: %w", err)
	}
	if err := requireAncestor(ctx, root, facts.MergeCommit, facts.TestedHead); err != nil {
		return nil, fmt.Errorf("TestedHead does not descend from the original Main merge: %w", err)
	}
	if err := requireAncestor(ctx, root, facts.TestedHead, finalHead); err != nil {
		return nil, fmt.Errorf("final pinned target does not descend from the tested Main head: %w", err)
	}
	changes, err := readRawDiff(ctx, root, facts.MergeBase, facts.SourceHead)
	if err != nil {
		return nil, err
	}
	if len(changes) != len(facts.ChangedPaths) {
		return nil, fmt.Errorf("actual Git diff path count differs from MainFacts")
	}
	files := make([]ChangedFile, 0, len(changes))
	blobCache := map[string]treeBlob{}
	var blobBytesRead int64
	for i, path := range facts.ChangedPaths {
		change, ok := changes[path]
		if !ok || i > 0 && facts.ChangedPaths[i-1] >= path {
			return nil, fmt.Errorf("actual Git diff path set differs from sorted MainFacts")
		}
		base, err := readBlobAt(ctx, root, facts.MergeBase, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read merge-base blob for %q: %w", path, err)
		}
		target, err := readBlobAt(ctx, root, cp.TargetHead, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read pre-merge target blob for %q: %w", path, err)
		}
		source, err := readBlobAt(ctx, root, facts.SourceHead, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read source blob for %q: %w", path, err)
		}
		merged, err := readBlobAt(ctx, root, facts.MergeCommit, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read merged blob for %q: %w", path, err)
		}
		tested, err := readBlobAt(ctx, root, facts.TestedHead, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read tested blob for %q: %w", path, err)
		}
		final, err := readBlobAt(ctx, root, finalHead, path, blobCache, &blobBytesRead)
		if err != nil {
			return nil, fmt.Errorf("read final pinned blob for %q: %w", path, err)
		}
		oldObject, newObject := change.oldObject, change.newObject
		if allZeroObjectID(oldObject) {
			oldObject = ""
		}
		if allZeroObjectID(newObject) {
			newObject = ""
		}
		if base.objectID != oldObject || source.objectID != newObject {
			return nil, fmt.Errorf("raw diff blob ids differ from actual merge-base/source trees for %q: raw=(%s,%s) trees=(%s,%s)", path, change.oldObject, change.newObject, base.objectID, source.objectID)
		}
		if err := validateStatus(change.status, base.objectID, source.objectID); err != nil {
			return nil, fmt.Errorf("actual Git status for %q: %w", path, err)
		}
		files = append(files, ChangedFile{
			Path: path, Status: change.status,
			BaseBlobSHA256: base.sha256, TargetBlobSHA256: target.sha256,
			SourceBlobSHA256: source.sha256, MergeBlobSHA256: merged.sha256,
			TestedBlobSHA256: tested.sha256, FinalBlobSHA256: final.sha256,
		})
	}
	return files, nil
}

// verifyMerge checks provenance shared by verified and preserved attempts.
// It does not claim that any required check ran or passed.
func verifyMerge(ctx context.Context, root string, cp checkpoint, finalHead string) error {
	for name, value := range map[string]string{
		"target head": cp.TargetHead, "source head": cp.SourceHead,
		"merge base": cp.MergeBase, "merge commit": cp.MergeCommit,
		"final target head": finalHead,
	} {
		if !fullObjectID(value) {
			return fmt.Errorf("%s is not a full Git object id", name)
		}
		if err := verifyCommit(ctx, root, value); err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
	}
	actualBase, err := gitOutput(ctx, root, "merge-base", cp.TargetHead, cp.SourceHead)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(actualBase)) != cp.MergeBase {
		return fmt.Errorf("MainFacts merge base differs from the actual target/source merge base")
	}
	parents, err := gitOutput(ctx, root, "rev-list", "--parents", "-n", "1", cp.MergeCommit)
	if err != nil {
		return fmt.Errorf("read actual MergeCommit parents: %w", err)
	}
	parentIDs := strings.Fields(string(parents))
	if len(parentIDs) != 3 || parentIDs[0] != cp.MergeCommit || parentIDs[1] != cp.TargetHead || parentIDs[2] != cp.SourceHead {
		return fmt.Errorf("MergeCommit is not the original two-parent Main merge [TargetHead, SourceHead]")
	}
	if err := requireAncestor(ctx, root, cp.MergeCommit, finalHead); err != nil {
		return fmt.Errorf("final pinned target does not contain the original Main merge: %w", err)
	}
	return nil
}

type treeBlob struct {
	objectID string
	sha256   string
	size     int64
}

func readBlobAt(ctx context.Context, root, commit, file string, cache map[string]treeBlob, total *int64) (treeBlob, error) {
	var result treeBlob
	if file == "" || strings.ContainsRune(file, '\x00') || strings.HasPrefix(file, "/") || file == "." || file == ".." || strings.HasPrefix(file, "../") || strings.Contains(file, "\\") {
		return result, fmt.Errorf("unsafe repository path")
	}
	pathspec := ":(literal)" + file
	out, err := gitOutput(ctx, root, "ls-tree", "-z", "--full-tree", commit, "--", pathspec)
	if err != nil {
		return result, err
	}
	if len(out) == 0 {
		return result, nil
	}
	if out[len(out)-1] != 0 {
		return result, fmt.Errorf("Git ls-tree output is not NUL terminated")
	}
	entries := bytes.Split(out[:len(out)-1], []byte{0})
	if len(entries) != 1 {
		return result, fmt.Errorf("Git ls-tree returned an ambiguous path")
	}
	metadata, actualPath, ok := bytes.Cut(entries[0], []byte{'\t'})
	if !ok || string(actualPath) != file {
		return result, fmt.Errorf("Git ls-tree path differs from requested literal path")
	}
	fields := strings.Fields(string(metadata))
	if len(fields) != 3 || fields[1] != "blob" || !fullObjectID(fields[2]) {
		return result, fmt.Errorf("changed path is not a Git blob")
	}
	if cached, ok := cache[fields[2]]; ok {
		return cached, nil
	}
	data, err := gitOutputLimited(ctx, root, gitBlobLimit, "cat-file", "blob", fields[2])
	if err != nil {
		return result, err
	}
	if int64(len(data)) > gitBlobBundleLimit-*total {
		return result, fmt.Errorf("unique changed Git blobs exceed %d bytes", gitBlobBundleLimit)
	}
	*total += int64(len(data))
	result.objectID = fields[2]
	result.sha256 = hashBytes(data)
	result.size = int64(len(data))
	cache[fields[2]] = result
	return result, nil
}

func readRawDiff(ctx context.Context, root, base, source string) (map[string]rawChange, error) {
	out, err := gitOutput(ctx, root, "diff", "--no-renames", "--raw", "--full-index", "--abbrev=64", "-z", "--no-ext-diff", "--no-textconv", base, source, "--")
	if err != nil {
		return nil, fmt.Errorf("read original Git raw diff: %w", err)
	}
	changes := map[string]rawChange{}
	if len(out) == 0 {
		return changes, nil
	}
	if out[len(out)-1] != 0 {
		return nil, fmt.Errorf("Git raw diff output is not NUL terminated")
	}
	parts := bytes.Split(out[:len(out)-1], []byte{0})
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("Git raw diff output has an incomplete path entry")
	}
	for i := 0; i < len(parts); i += 2 {
		metadata := string(parts[i])
		if len(metadata) < 2 || metadata[0] != ':' {
			return nil, fmt.Errorf("Git raw diff metadata is malformed")
		}
		fields := strings.Fields(metadata[1:])
		if len(fields) != 5 || !fullObjectID(fields[2]) || !fullObjectID(fields[3]) {
			return nil, fmt.Errorf("Git raw diff object metadata is malformed: %q (%q)", metadata, fields)
		}
		file := string(parts[i+1])
		if file == "" || strings.ContainsRune(file, '\x00') {
			return nil, fmt.Errorf("Git raw diff contains an invalid path")
		}
		status := fields[4]
		if len(status) != 1 || !strings.Contains("AMDT", status) {
			return nil, fmt.Errorf("Git raw diff has unsupported status %q", status)
		}
		if _, exists := changes[file]; exists {
			return nil, fmt.Errorf("Git raw diff duplicates a path")
		}
		changes[file] = rawChange{path: file, status: status, oldObject: fields[2], newObject: fields[3]}
	}
	return changes, nil
}

func validateStatus(status, oldObject, newObject string) error {
	oldPresent, newPresent := !allZeroObjectID(oldObject), !allZeroObjectID(newObject)
	switch status {
	case "A":
		if oldPresent || !newPresent {
			return fmt.Errorf("addition endpoints are inconsistent")
		}
	case "D":
		if !oldPresent || newPresent {
			return fmt.Errorf("deletion endpoints are inconsistent")
		}
	case "M":
		if !oldPresent || !newPresent {
			return fmt.Errorf("modification endpoints are inconsistent")
		}
	case "T":
		if !oldPresent || !newPresent {
			return fmt.Errorf("type-change endpoints are inconsistent")
		}
	default:
		return fmt.Errorf("unsupported status %q", status)
	}
	return nil
}

func verifyCommit(ctx context.Context, root, oid string) error {
	out, err := gitOutput(ctx, root, "rev-parse", "--verify", oid+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != oid {
		return fmt.Errorf("object is not the exact pinned commit")
	}
	return nil
}

func requireAncestor(ctx context.Context, root, ancestor, descendant string) error {
	_, err := gitOutput(ctx, root, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		return fmt.Errorf("%s is not an ancestor of %s: %w", ancestor, descendant, err)
	}
	return nil
}

func fullObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return false
	}
	return strings.ToLower(value) == value
}

func allZeroObjectID(value string) bool {
	return strings.Trim(value, "0") == ""
}

func gitOutput(ctx context.Context, root string, args ...string) ([]byte, error) {
	return gitOutputLimited(ctx, root, 64<<20, args...)
}

func gitOutputLimited(ctx context.Context, root string, limit int64, args ...string) ([]byte, error) {
	return cachedGitOutput(ctx, root, limit, args...)
}
func runGitOutputLimited(ctx context.Context, root string, limit int64, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if int64(len(data)) > limit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("Git output exceeds %d bytes", limit)
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), waitErr, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}
