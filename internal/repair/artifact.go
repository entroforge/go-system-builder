package repair

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/diagnosticstorage"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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

func fileRef(relative string, data []byte) ArtifactRef {
	return ArtifactRef{Path: filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))}
}

// responsibilityArtifactPath preserves legacy short paths and splits long
// issued identifiers into reversible bounded components. No content digest is
// needed and already issued paths remain readable.
// QuestionnaireRoot identifies the original Main questionnaire directory for
// a newly issued repair responsibility without depending on content digests.
func QuestionnaireRoot(runtimeID, sessionID string) string {
	data, _ := json.Marshal([]string{runtimeID, sessionID})
	encoded := base64.RawURLEncoding.EncodeToString(data)
	parts := []string{".claude", "submissions", "s9", "p"}
	for len(encoded) > 120 {
		parts = append(parts, encoded[:120])
		encoded = encoded[120:]
	}
	parts = append(parts, encoded)
	return filepath.ToSlash(filepath.Join(parts...))
}

// ArtifactStoragePath returns the location chosen for an original S9 responsibility.
func ArtifactStoragePath(category, id string) string { return responsibilityArtifactPath(category, id) }

func responsibilityArtifactPath(category, id string) string {
	if len(id) <= 200 {
		return filepath.ToSlash(filepath.Join(artifactRoot, category, id+".json"))
	}
	parts := []string{artifactRoot, category, "p"}
	for len(id) > 120 {
		parts = append(parts, id[:120])
		id = id[120:]
	}
	parts = append(parts, id+".json")
	return filepath.ToSlash(filepath.Join(parts...))
}

func writeImmutable(root, relative, schemaName string, document any) (ArtifactRef, error) {
	data, err := canonicalJSON(document)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("encode %s: %w", relative, err)
	}
	if len(data) > 20<<20 {
		return ArtifactRef{}, fmt.Errorf("immutable S9 artifact %s exceeds 20 MiB after final serialization; reduce professional answer content", relative)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
		return ArtifactRef{}, fmt.Errorf("validate %s: %w", relative, err)
	}
	path, err := repositoryPath(root, relative)
	if err != nil {
		return ArtifactRef{}, err
	}
	relativeRef, err := relativePath(root, path)
	if err != nil {
		return ArtifactRef{}, err
	}
	parent, name, err := workpacket.OpenParent(root, relativeRef, true)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("open immutable artifact parent: %w", err)
	}
	defer parent.Close()
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("write immutable artifact %s: %w", relative, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = parent.Remove(name)
		return ArtifactRef{}, fmt.Errorf("write immutable artifact %s: %w", relative, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = parent.Remove(name)
		return ArtifactRef{}, fmt.Errorf("sync immutable artifact %s: %w", relative, err)
	}
	if err := file.Close(); err != nil {
		_ = parent.Remove(name)
		return ArtifactRef{}, fmt.Errorf("close immutable artifact %s: %w", relative, err)
	}
	// Preserve an artifact when parent durability is unknown. Cleanup must use
	// the original Writer's pending/reference/SHA checks, never an eager remove.
	if err := runtimepkg.SyncDirectory(filepath.Dir(path)); err != nil {
		return ArtifactRef{}, fmt.Errorf("sync immutable artifact parent %s: %w", relative, err)
	}
	return fileRef(relative, data), nil
}

func readArtifact(root string, ref ArtifactRef, schemaName string) ([]byte, error) {
	path, err := repositoryPath(root, ref.Path)
	if err != nil {
		return nil, err
	}
	rel, err := relativePath(root, path)
	if err != nil {
		return nil, err
	}
	data, err := workpacket.ReadFile(root, rel, 20<<20)
	if err != nil {
		return nil, fmt.Errorf("read artifact %s: %w", ref.Path, err)
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

// ignoreBaselinePath reports whether a repository-relative path must never
// enter (or continue to gate) a session authority baseline. Control-plane
// bookkeeping (#11) and dependency/build caches (#13: vite pre-bundle
// metadata, dist output, coverage) regenerate during ordinary verification
// runs and are never authority-relevant.
func ignoreBaselinePath(rel string, roots ...string) bool {
	if isControlPlanePath(rel, false, roots...) {
		return true
	}

	return false
}

var auditIndexScratchPattern = regexp.MustCompile(`^\.claude/hook-audit-index-[0-9]+\.scratch$`)

func isControlPlanePath(rel string, isDir bool, roots ...string) bool {
	if !isDir && len(roots) > 0 && diagnosticstorage.Recognizes(context.Background(), roots[0], rel) {
		return true
	}
	// This exact scratch belongs to rebuildable audit cache publication. It is
	// not an accepted audit event or any professional receipt/authority.
	if !isDir && auditIndexScratchPattern.MatchString(rel) {
		if len(roots) == 0 {
			return true
		}
		info, err := os.Lstat(filepath.Join(roots[0], filepath.FromSlash(rel)))
		return err == nil && info.Mode().IsRegular()
	}
	// Do not prune the body directory: a neighboring product file must remain
	// visible. Only actual complete packets with a matching content identity are
	// storage projections. Missing/corrupt candidates conservatively remain in
	// the product inventory and cannot silently acquire a directory exemption.
	if !isDir && len(roots) > 0 && strings.HasPrefix(rel, ".claude/packet-bodies/") {
		recognized, err := runtimepkg.ClassifyPacketBodyStoragePathsContext(context.Background(), roots[0], []string{rel})
		if err == nil && len(recognized) == 1 {
			return true
		}
	}
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
	if rel == ".claude/hook-decisions.jsonl" || rel == ".claude/hook-decisions.jsonl.index" {
		return true
	}
	if strings.HasPrefix(rel, ".claude/") && (strings.HasSuffix(rel, ".lock") || strings.HasSuffix(rel, ".lock.process")) {
		return true
	}
	for _, prefix := range []string{
		".claude/review/", ".claude/evidence/", ".claude/workgroups/", ".claude/plans/", ".claude/bin/",
		".claude/submissions/", ".claude/work-packets/", ".claude/assignment-context/",
		".claude/runtime-checkpoints/", ".claude/hook-notices/", ".claude/hook-metrics/",
	} {
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

// runtimeLogPaths identifies candidate runtime log output for new baseline
// capture. Stored membership takes precedence over this
// current-state heuristic when checking an existing session.
func runtimeLogPaths(root string, paths []string) map[string]bool {
	candidates := make([]string, 0, len(paths))
	for _, path := range paths {
		if isRuntimeLogShape(path) {
			candidates = append(candidates, normalizePath(path))
		}
	}
	return gitIgnoredPaths(root, candidates)
}

// ComputeSessionChangeset returns only paths already available from this
// Session's accepted contributions. No saved baseline can establish the full
// history of changes, so missing contributions remain unknown rather than
// becoming an empty change set or a whole-tree digest comparison.
func ComputeSessionChangeset(root string, session RepairSession) ([]ArtifactRef, error) {
	record, err := ComputeSessionChangesetRecord(root, session)
	if err != nil {
		return nil, err
	}
	return record.Artifacts, nil
}

// ComputeSessionChangesetRecord projects known accepted contributions. A new
// Session does not retain historical file contents or a Git baseline, so a
// repository scan cannot honestly reconstruct its prior changes.
func ComputeSessionChangesetRecord(root string, session RepairSession) (Changeset, error) {
	data, err := workpacket.ReadFile(root, ".claude/loop-state.json", 20<<20)
	if err != nil {
		return Changeset{}, fmt.Errorf("Session changes are unknown: current accepted responsibilities unavailable: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return Changeset{}, fmt.Errorf("Session changes are unknown: decode current responsibilities: %w", err)
	}
	if stringField(repairPointer(state)["session_id"]) != session.SessionID {
		return Changeset{}, errors.New("Session changes are unknown: the current responsibility belongs to a different Session")
	}
	return CurrentRepairResultChangeset(root, state)
}

// CurrentRepairResultChangeset projects the existing accepted Final Result
// batch for both Impact and Handoff. It does not rescan the working directory
// or replay Contract/Plan/source qualification.
func CurrentRepairResultChangeset(root string, state map[string]any) (Changeset, error) {
	pointer := repairPointer(state)
	results, err := ReceivedRepairContributions(root, state)
	if err != nil {
		return Changeset{}, err
	}
	if len(results) == 0 {
		return Changeset{}, errors.New("Session changes are unknown: no original contribution has been received")
	}
	return repairResultChangeset(stringField(pointer["session_id"]), results)
}

// Read the existing accepted Final batch without replaying upstream proofs.
func currentRepairResults(root string, pointer map[string]any) ([]RepairResult, error) {
	refs, err := currentRepairResultRefs(pointer)
	if err != nil {
		return nil, err
	}
	results := make([]RepairResult, 0, len(refs))
	for _, ref := range refs {
		data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
		if err != nil {
			return nil, err
		}
		var result RepairResult
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func repairResultChangeset(sessionID string, results []RepairResult) (Changeset, error) {
	changed, err := aggregateRepairResultArtifacts(results)
	if err != nil {
		return Changeset{}, err
	}
	artifacts := make([]ArtifactRef, 0, len(changed))
	for _, item := range changed {
		artifacts = append(artifacts, ArtifactRef{Path: item.Path, Status: item.Status})
	}
	return repairChangesetRecord(sessionID, "accepted_results", artifacts)
}

func repairChangesetRecord(sessionID, source string, artifacts []ArtifactRef) (Changeset, error) {
	projected := append([]ArtifactRef{}, artifacts...)
	changeset := Changeset{SchemaVersion: "1.0.0", RecordType: "repair_changeset", SessionID: sessionID, Source: source, Artifacts: projected, ComputedAt: nowOr(time.Time{})}
	changeset.ChangesetID = repairChangesetID(changeset)
	return changeset, nil
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
