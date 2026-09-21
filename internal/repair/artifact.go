package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	boundary := newBaselineBoundary(rootAbs)
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
		if ignoreBaselinePath(relSlash) || boundary.excludes(relSlash) || (entry.IsDir() && boundary.discoverEnvironment(relSlash)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
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
		// Inventory first: excluded logs are never read or hashed.
		artifacts = append(artifacts, ArtifactRef{ID: "baseline-" + strings.NewReplacer("/", "-", "\\", "-").Replace(relSlash), Path: relSlash, Status: "modified"})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("capture repository baseline: %w", err)
	}
	// RC-16 (S9-L1): runtime log output (server/log/<date>/info.log and the
	// like) is written by the project's long-running service and by ordinary
	// verification runs while a session is open. It is git-ignored, untracked
	// exhaust — never implementation surface — so it is classified out of the
	// baseline here, forgiven by the authority gate (checkS9AuthorityFreshness)
	// and skipped by the Session diff (ComputeSessionChangeset) under one and
	// the same rule. Without this, a single appended log line staled the
	// fingerprint of every open session.
	if excluded := runtimeLogPaths(rootAbs, artifactPaths(artifacts)); len(excluded) > 0 {
		kept := make([]ArtifactRef, 0, len(artifacts))
		for _, artifact := range artifacts {
			if excluded[artifact.Path] {
				continue
			}
			kept = append(kept, artifact)
		}
		artifacts = kept
	}
	for i := range artifacts {
		f, err := os.Open(filepath.Join(rootAbs, filepath.FromSlash(artifacts[i].Path)))
		if err != nil {
			return nil, "", fmt.Errorf("read baseline artifact %s: %w", artifacts[i].Path, err)
		}
		h := sha256.New()
		_, readErr := io.Copy(h, f)
		closeErr := f.Close()
		if readErr != nil {
			return nil, "", readErr
		}
		if closeErr != nil {
			return nil, "", closeErr
		}
		artifacts[i].SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		lines = append(lines, artifact.Path+":"+artifact.SHA256)
	}
	return artifacts, sha256Bytes([]byte(strings.Join(lines, "\n"))), nil
}

// ignoreBaselinePath reports whether a repository-relative path must never
// enter (or continue to gate) a session authority baseline. Control-plane
// bookkeeping (#11) and dependency/build caches (#13: vite pre-bundle
// metadata, dist output, coverage) regenerate during ordinary verification
// runs and are never authority-relevant.
func ignoreBaselinePath(rel string) bool {
	if isControlPlanePath(rel, false) {
		return true
	}

	return false
}

func isControlPlanePath(rel string, isDir bool) bool {
	_ = isDir
	// .claude/multi.json is the local session launcher's environment/profile
	// file (model and connection settings, including local credentials). The
	// harness never reads it; the launcher rewrites it on every session start
	// or profile switch, so any captured copy goes stale out-of-band and can
	// never describe the implementation surface. It is control-plane local
	// state, exactly like loop-state.json, and must never enter (or gate) a
	// session baseline. The exemption names this one file only — it does not
	// widen to .claude/ as a whole.
	if rel == ".claude/loop-state.json" || rel == ".claude/loop-events.jsonl" || rel == ".claude/loop-metrics.json" || rel == ".claude/settings.json" || rel == ".claude/settings.local.json" || rel == ".claude/multi.json" {
		return true
	}
	// Hook decision journals append on every hook evaluation; they are
	// control-plane bookkeeping and must never enter a session baseline
	// (a captured copy goes stale on the builders' own next hook call).
	if strings.HasPrefix(rel, ".claude/hook") {
		return true
	}
	if strings.HasPrefix(rel, ".claude/") && (strings.HasSuffix(rel, ".lock") || strings.HasSuffix(rel, ".lock.process")) {
		return true
	}
	for _, prefix := range []string{".claude/review/", ".claude/evidence/", ".claude/workgroups/", ".claude/plans/", ".claude/bin/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// artifactPaths extracts the repository-relative paths of an artifact list.
func artifactPaths(artifacts []ArtifactRef) []string {
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

// isRuntimeLogShape reports whether a repository-relative path is *named* like
// runtime log output. Exactly two shapes qualify:
//
//   - a log file name: <name>.log, optionally followed by nothing but the
//     rotation numbers, dates and compression extensions log rotation appends
//     (info.log, info.log.1, info.log.2026-09-17, info.log.1.gz);
//   - a log-output name directly inside a log directory (logs/app.out,
//     logs/stdout).
//
// Resemblance to ".log" is not enough, and neither is living in a log
// directory: internal/audit.logic.go is a Go file, web/app.logger.ts is a
// TypeScript file, config/logback.xml is configuration, and an untracked
// server/log/debug_helper.go is a local source copy. All of them are rejected
// here and stay in the baseline.
//
// Shape alone never decides: runtimeLogPaths completes the classification with
// Git's own verdict, so a tracked log fixture stays authority-relevant and an
// unclassifiable repository keeps the strict default.
func isRuntimeLogShape(rel string) bool {
	parts := strings.Split(normalizePath(rel), "/")
	name := strings.ToLower(parts[len(parts)-1])
	if logFileName(name) {
		return true
	}
	for _, directory := range parts[:len(parts)-1] {
		if directory == "log" || directory == "logs" {
			return logOutputName(name)
		}
	}
	return false
}

// logFileName reports whether a file name ends in the log-file name family: a
// "log" segment followed by at most rotation and compression segments. It
// must be the final extension family — ".log" as the prefix of a longer
// extension (logic.go) or as an infix (logger.ts, logback.xml) is not a log.
func logFileName(name string) bool {
	segments := strings.Split(name, ".")
	index := len(segments) - 1
	for index > 0 && logRotationSegment(segments[index]) {
		index--
	}
	return index > 0 && segments[index] == "log"
}

// logRotationSegment reports whether a name segment is produced by log rotation
// or compression rather than being another file extension: a rotation number, a
// date, or a compression suffix.
func logRotationSegment(segment string) bool {
	switch segment {
	case "gz", "bz2", "xz", "zip", "zst", "zstd", "lz4", "7z":
		return true
	}
	if segment == "" {
		return false
	}
	digits := false
	for _, r := range segment {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return digits
}

// logOutputName reports whether a file name inside a log directory is shaped
// like log output. The list is an allowlist: anything carrying a source, build,
// configuration or documentation extension is repository content that merely
// lives in a log directory, never runtime exhaust.
func logOutputName(name string) bool {
	if name == "stdout" || name == "stderr" {
		return true
	}
	parts := strings.Split(name, ".")
	i := len(parts) - 1
	for i > 0 && logRotationSegment(parts[i]) {
		i--
	}
	if i < 1 {
		return false
	}
	switch parts[i] {
	case "out", "err", "stdout", "stderr":
		return true
	}
	return false
}

// gitIgnoredPaths returns the subset of repository-relative paths that Git
// classifies as ignored. Classification comes from the project's own ignore
// rules instead of a hardcoded directory list, and it is existence-independent:
// a log rotated away after the session opened still classifies. Tracked paths
// are never reported — git check-ignore consults the index — so a fixture or
// golden file committed on purpose stays authority-relevant.
//
// Ignored-and-untracked is necessary but never sufficient on its own: callers
// pair it with the explicit shape test above.
//
// Any failure (not a Git repository, git unavailable, an unexpected exit
// status) returns the empty set: the caller keeps the strict default rather
// than widening an exemption on an unverifiable surface.
func gitIgnoredPaths(root string, paths []string) map[string]bool {
	ignored := map[string]bool{}
	if len(paths) == 0 {
		return ignored
	}
	command := exec.Command("git", "-C", root, "check-ignore", "--stdin", "-z")
	command.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	output, err := command.Output()
	if err != nil {
		// Exit status 1 is "nothing matched", which is a normal answer.
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return ignored
		}
	}
	for _, path := range strings.Split(string(output), "\x00") {
		if strings.TrimSpace(path) != "" {
			ignored[normalizePath(path)] = true
		}
	}
	return ignored
}

// runtimeLogPaths returns the subset of paths that are runtime log output and
// therefore never part of the session authority surface — no matter which
// generation of the harness captured them. It is the single classification used
// by baseline capture, the S9 authority gate, and the Session diff.
func runtimeLogPaths(root string, paths []string) map[string]bool {
	candidates := make([]string, 0, len(paths))
	for _, path := range paths {
		if isRuntimeLogShape(path) {
			candidates = append(candidates, normalizePath(path))
		}
	}
	return gitIgnoredPaths(root, candidates)
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
	// RC-16 (S9-L1): sessions opened before the capture-side runtime-log
	// classification carry log output in BaselineArtifacts. The same rule is
	// applied to the stored baseline here so the Session diff stays consistent
	// with capture and with the authority gate: an appended log line is not a
	// change, and the stored baseline is never rewritten.
	logs := excludedBaselinePaths(root, artifactPaths(session.BaselineArtifacts))
	for _, artifact := range session.BaselineArtifacts {
		path := normalizePath(artifact.Path)
		if logs[path] {
			continue
		}
		// RC-17 (S9-L2): the base side applies the same control-plane
		// classification as capture and the authority gate. A stored baseline
		// captured before a path was classified as control plane (notably
		// .claude/multi.json, the launcher's local profile file) would
		// otherwise surface here as a phantom "deleted" or "modified" entry
		// that no RepairResult can legitimately claim. Stored baselines never
		// contain the other exempt classes (capture already excluded them), so
		// this filter is a no-op except for paths reclassified after session
		// open.
		if ignoreBaselinePath(path) {
			continue
		}
		base[path] = artifact
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
