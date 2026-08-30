package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const artifactRoot = ".claude/review/repair"

func nowOr(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func repositoryPath(root, relative string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(relative) == "" {
		return "", errors.New("repository root and artifact path are required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	full := relative
	if !filepath.IsAbs(relative) {
		full = filepath.Join(rootAbs, filepath.Clean(filepath.FromSlash(relative)))
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes repository root: %s", relative)
	}
	return full, nil
}

func relativePath(root, path string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside repository root", path)
	}
	return filepath.ToSlash(rel), nil
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileRef(relative string, data []byte) ArtifactRef {
	return ArtifactRef{Path: filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))), SHA256: sha256Bytes(data)}
}

func writeImmutable(root, relative, schemaName string, document any) (ArtifactRef, error) {
	data, err := canonicalJSON(document)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("encode %s: %w", relative, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
		return ArtifactRef{}, fmt.Errorf("validate %s: %w", relative, err)
	}
	path, err := repositoryPath(root, relative)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ArtifactRef{}, fmt.Errorf("create artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("write immutable artifact %s: %w", relative, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return ArtifactRef{}, fmt.Errorf("write immutable artifact %s: %w", relative, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return ArtifactRef{}, fmt.Errorf("close immutable artifact %s: %w", relative, err)
	}
	return fileRef(relative, data), nil
}

func readArtifact(root string, ref ArtifactRef, schemaName string) ([]byte, error) {
	path, err := repositoryPath(root, ref.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact %s: %w", ref.Path, err)
	}
	actual := sha256Bytes(data)
	if ref.SHA256 == "" || actual != ref.SHA256 {
		return nil, fmt.Errorf("artifact %s hash drift: expected %s, got %s", ref.Path, ref.SHA256, actual)
	}
	if schemaName != "" {
		if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
			return nil, fmt.Errorf("artifact %s schema invalid: %w", ref.Path, err)
		}
	}
	return data, nil
}

func decodeArtifact(root string, ref ArtifactRef, schemaName string, target any) error {
	data, err := readArtifact(root, ref, schemaName)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode artifact %s: %w", ref.Path, err)
	}
	return nil
}

func normalizePath(path string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
}

func pathMatches(path, rule string) bool {
	path, rule = normalizePath(path), normalizePath(rule)
	if rule == "all" {
		return true
	}
	return path == rule || strings.HasPrefix(path, strings.TrimSuffix(rule, "/")+"/")
}

func scopeAllows(path string, prospective, forbidden []string) error {
	for _, rule := range forbidden {
		if pathMatches(path, rule) {
			return fmt.Errorf("changed artifact %q is inside forbidden_scope %q", path, rule)
		}
	}
	for _, rule := range prospective {
		if pathMatches(path, rule) {
			return nil
		}
	}
	return fmt.Errorf("changed artifact %q is outside prospective_scope", path)
}

// captureRepositoryBaseline records the implementation surface at Session
// open. Control-plane files are deliberately excluded: they are mutated by
// the runtime while the repair is running and are not implementation output.
//
// RC-03 (EH-8) narrowing: the exclusion is explicit, not blanket ".claude".
// Known control-plane subtrees (.claude/review, .claude/evidence,
// .claude/workgroups and the mutable state files) are skipped, but an
// unexpected .claude path (e.g., .claude/foo/product.go) is treated as
// product surface — a repair that hides product writes under .claude must be
// visible to the Session diff instead of silently excluded. Any legitimate
// control-plane write must be under the known subtrees; product writes under
// .claude remain product drift.
func captureRepositoryBaseline(root string) ([]ArtifactRef, string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, "", fmt.Errorf("resolve repository root: %w", err)
	}
	artifacts := []ArtifactRef{}
	err = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		if parts[0] == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if parts[0] == ".claude" {
			if isControlPlanePath(relSlash, entry.IsDir()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
		} else if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read baseline artifact %s: %w", relSlash, err)
		}
		artifacts = append(artifacts, ArtifactRef{ID: "baseline-" + strings.NewReplacer("/", "-", "\\", "-").Replace(relSlash), Path: relSlash, SHA256: sha256Bytes(data), Status: "modified"})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("capture repository baseline: %w", err)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		lines = append(lines, artifact.Path+":"+artifact.SHA256)
	}
	return artifacts, sha256Bytes([]byte(strings.Join(lines, "\n"))), nil
}

func isControlPlanePath(rel string, isDir bool) bool {
	_ = isDir
	if rel == ".claude/loop-state.json" || rel == ".claude/loop-events.jsonl" || rel == ".claude/loop-metrics.json" || rel == ".claude/settings.json" || rel == ".claude/settings.local.json" {
		return true
	}
	for _, prefix := range []string{".claude/review/", ".claude/evidence/", ".claude/workgroups/", ".claude/plans/", ".claude/bin/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// ComputeSessionChangeset derives the actual implementation delta from the
// immutable Session baseline and the current repository. It is the authority
// used by result submission; agents may describe a change, but cannot invent
// one or omit one.
func ComputeSessionChangeset(root string, session RepairSession) ([]ArtifactRef, error) {
	current, _, err := captureRepositoryBaseline(root)
	if err != nil {
		return nil, err
	}
	base := map[string]ArtifactRef{}
	for _, artifact := range session.BaselineArtifacts {
		base[normalizePath(artifact.Path)] = artifact
	}
	now := map[string]ArtifactRef{}
	for _, artifact := range current {
		now[normalizePath(artifact.Path)] = artifact
	}
	paths := map[string]bool{}
	for path := range base {
		paths[path] = true
	}
	for path := range now {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changed := []ArtifactRef{}
	for _, path := range ordered {
		before, hadBefore := base[path]
		after, hadAfter := now[path]
		switch {
		case !hadBefore && hadAfter:
			after.Status = "added"
			changed = append(changed, after)
		case hadBefore && !hadAfter:
			before.Status = "deleted"
			changed = append(changed, before)
		case hadBefore && hadAfter && before.SHA256 != after.SHA256:
			after.Status = "modified"
			changed = append(changed, after)
		}
	}
	return changed, nil
}

// ComputeSessionChangesetRecord wraps the authoritative Session diff in the
// persisted Changeset envelope used by Impact and Handoff.
func ComputeSessionChangesetRecord(root string, session RepairSession) (Changeset, error) {
	artifacts, err := ComputeSessionChangeset(root, session)
	if err != nil {
		return Changeset{}, err
	}
	if len(artifacts) == 0 {
		return Changeset{}, errors.New("actual Session diff is empty")
	}
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		lines = append(lines, artifact.Path+":"+artifact.SHA256)
	}
	sort.Strings(lines)
	digest := sha256Bytes([]byte(strings.Join(lines, "\n")))
	return Changeset{SchemaVersion: "1.0.0", RecordType: "repair_changeset", ChangesetID: "repair-changeset-" + digest[:16], SessionID: session.SessionID, Source: "session_diff", Artifacts: artifacts, Digest: digest, ComputedAt: nowOr(time.Time{})}, nil
}

func exactIDs(expected, actual []string) error {
	expectedSet, actualSet := map[string]bool{}, map[string]bool{}
	for _, id := range expected {
		if expectedSet[id] {
			return fmt.Errorf("duplicate expected repair unit %q", id)
		}
		expectedSet[id] = true
	}
	for _, id := range actual {
		if actualSet[id] {
			return fmt.Errorf("duplicate submitted repair unit %q", id)
		}
		actualSet[id] = true
	}
	var missing, extra []string
	for id := range expectedSet {
		if !actualSet[id] {
			missing = append(missing, id)
		}
	}
	for id := range actualSet {
		if !expectedSet[id] {
			extra = append(extra, id)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		return fmt.Errorf("exact repair-unit coverage failed: missing=%v extra=%v", missing, extra)
	}
	return nil
}
