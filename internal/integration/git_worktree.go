package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/diagnosticstorage"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// worktreeClean accepts named runtime projections and the exact pinned
// Harness files appropriate to a Main or Worker checkout. Product changes,
// staged changes, deletions and renames still block.
func worktreeClean(ctx context.Context, root string) (bool, error) {
	workerAssets, err := workerBootstrapProjection(ctx, root)
	if err != nil {
		return false, err
	}
	return worktreeCleanWithProjection(ctx, root, workerAssets)
}

// Metrics staging is a Main checkout observation only. The worker and
// cleanup callers of worktreeClean must not acquire deletion authority from a
// reserved filename.
func mainWorktreeClean(ctx context.Context, root string) (bool, error) {
	workerAssets, err := workerBootstrapProjection(ctx, root)
	if err != nil {
		return false, err
	}
	return worktreeCleanWithMetrics(ctx, root, workerAssets, nil, true)
}

func worktreeCleanWithProjection(ctx context.Context, root string, workerAssets map[string]string, logEvidence ...map[string]string) (bool, error) {
	return worktreeCleanWithPrivateArea(ctx, root, workerAssets, nil, logEvidence...)
}

func worktreeCleanWithPrivateArea(ctx context.Context, root string, workerAssets map[string]string, area *WorkerPrivateAreaProjection, logEvidence ...map[string]string) (bool, error) {
	return worktreeCleanWithMetrics(ctx, root, workerAssets, area, false, logEvidence...)
}

func worktreeCleanWithMetrics(ctx context.Context, root string, workerAssets map[string]string, area *WorkerPrivateAreaProjection, allowMetrics bool, logEvidence ...map[string]string) (bool, error) {
	out, err := defaultRunner.Run(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	entries := strings.Split(out, "\x00")
	var registeredWorktrees map[string]bool
	mainRootChecked, mainRoot := false, false
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		if len(entry) < 4 {
			return false, nil
		}
		status, path := entry[:2], filepath.ToSlash(filepath.Clean(entry[3:]))
		if status != " M" && status != "??" {
			return false, nil
		}
		if allowMetrics && status == "??" && entry[3:] == path && metrics.IsTemporaryPath(path) {
			protected := workerAssets[path] != ""
			for _, evidence := range logEvidence {
				protected = protected || evidence[path] != ""
			}
			if !protected {
				recognized, err := metrics.RecognizeTemporary(ctx, root, path)
				if err != nil {
					return false, fmt.Errorf("inspect metrics temporary: %w", err)
				}
				if recognized {
					continue
				}
			}
		}
		if status == "??" && area != nil && strings.HasPrefix(path, area.path+"/") {
			allowed, err := area.Allows(path)
			if err != nil {
				return false, err
			}
			if !allowed {
				return false, nil
			}
			// An accepted original or captured check never becomes anonymous
			// scratch merely because its bytes changed.
			known := false
			for _, evidence := range append([]map[string]string{workerAssets}, logEvidence...) {
				if digest := evidence[path]; digest != "" {
					known = true
					if !fileMatchesDigest(root, path, digest) {
						return false, nil
					}
				}
			}
			if known {
				continue
			}
			continue
		}
		if status == "??" && len(logEvidence) > 0 && logEvidence[0][path] != "" && fileMatchesDigest(root, path, logEvidence[0][path]) {
			continue
		}
		if runtimeProjection(path, root) {
			continue
		}
		bodyProjection, err := runtimeBodyProjection(ctx, root, path)
		if err != nil {
			return false, err
		}
		if bodyProjection {
			continue
		}
		if (status == " M" || status == "??") && workerAssets[path] != "" && fileMatchesDigest(root, path, workerAssets[path]) {
			continue
		}
		if status == "??" && mainHarnessProjectionPath(path) {
			if strings.HasPrefix(path, ".worktrees/") {
				if registeredWorktrees == nil {
					registeredWorktrees, _ = registeredMainWorktreeProjections(ctx, root)
				}
				if registeredWorktrees[path] {
					continue
				}
			} else if !mainRootChecked {
				mainRoot = isOriginalMainWorkspace(ctx, root)
				mainRootChecked = true
			}
			if mainRoot && !strings.HasPrefix(path, ".worktrees/") {
				continue
			}
		}
		return false, nil
	}
	return true, nil
}

func isOriginalMainWorkspace(ctx context.Context, root string) bool {
	registry, state, err := workspace.Load(root)
	if err != nil {
		return false
	}
	if registry == nil {
		records, err := assignmentrecord.RecordsFromState(state)
		if err != nil {
			return false
		}
		for _, record := range records {
			if record.Contract == assignmentrecord.NativeDeliveryContract {
				return fileview.ValidateAuthority(root, state) == nil
			}
		}
		return false
	}
	main, err := workspace.Canonical(root)
	return err == nil && main == registry.MainRoot && registry.Validate(ctx, main) == nil
}

func workerWorktreeClean(ctx context.Context, mainRoot, workerRoot string, logEvidence ...map[string]string) (bool, error) {
	return workerWorktreeCleanWithPrivateArea(ctx, mainRoot, workerRoot, nil, logEvidence...)
}

func workerWorktreeCleanWithPrivateArea(ctx context.Context, mainRoot, workerRoot string, area *WorkerPrivateAreaProjection, logEvidence ...map[string]string) (bool, error) {
	pinned, err := registeredWorkerBootstrapPin(ctx, mainRoot, workerRoot)
	if err != nil {
		return false, err
	}
	var projection map[string]string
	if pinned {
		projection, err = workerBootstrapProjection(ctx, workerRoot)
		if err != nil {
			return false, err
		}
		if len(projection) == 0 {
			return false, fmt.Errorf("Main-pinned Worker bootstrap pointer is missing")
		}
	} else if _, err := os.Lstat(filepath.Join(workerRoot, ".claude", "loop-workspace.json")); err == nil {
		return false, fmt.Errorf("Worker control pointer has no matching Main-pinned execution")
	} else if !os.IsNotExist(err) {
		return false, err
	}
	native, err := nativeDeliveryProjection(mainRoot, workerRoot, false)
	if err != nil {
		return false, err
	}
	if projection == nil {
		projection = map[string]string{}
	}
	for path, digest := range native {
		projection[path] = digest
	}
	if len(logEvidence) > 1 {
		for path, digest := range logEvidence[1] {
			projection[path] = digest
		}
	}
	return worktreeCleanWithPrivateArea(ctx, workerRoot, projection, area, logEvidence...)
}

func registeredWorkerBootstrapPin(ctx context.Context, mainRoot, workerRoot string) (bool, error) {
	registry, _, err := workspace.Load(mainRoot)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if registry == nil {
		return false, nil
	}
	main, err := workspace.Canonical(mainRoot)
	if err != nil || main != registry.MainRoot {
		return false, fmt.Errorf("Worker receipt lookup requires its original Main checkout")
	}
	if err := registry.Validate(ctx, main); err != nil {
		return false, err
	}
	worker, err := workspace.Canonical(workerRoot)
	if err != nil {
		return false, err
	}
	for _, execution := range registry.AllExecutions() {
		path, err := workspace.Canonical(execution.Path)
		if err == nil && path == worker && execution.Path == worker && execution.BootstrapSHA256 != "" {
			return true, nil
		}
	}
	return false, nil
}

// workerBootstrapProjection resolves a Worker's generated files only through
// its pointer, the original Main execution registry, and Main's pinned receipt
// digest. It grants no projection to files outside that receipt.
func workerBootstrapProjection(ctx context.Context, workerRoot string) (map[string]string, error) {
	return workerBootstrapProjectionAt(ctx, workerRoot, false)
}

// During verified cleanup, an asset already restored to HEAD or removed need
// not match its installation bytes. The original receipt itself stays pinned.
func workerBootstrapProjectionAt(ctx context.Context, workerRoot string, restoring bool) (map[string]string, error) {
	pointerPath := filepath.Join(workerRoot, ".claude", "loop-workspace.json")
	info, err := os.Lstat(pointerPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Worker control pointer is not a regular file")
	}
	data, err := os.ReadFile(pointerPath)
	if err != nil {
		return nil, err
	}
	var pointer struct {
		ControlRoot string `json:"control_root"`
		Assignment  string `json:"assignment_id"`
		Runtime     string `json:"runtime_id"`
		Generation  int    `json:"execution_generation"`
	}
	if err := json.Unmarshal(data, &pointer); err != nil || pointer.ControlRoot == "" || pointer.Assignment == "" || pointer.Runtime == "" || pointer.Generation < 1 {
		return nil, fmt.Errorf("invalid Worker control pointer")
	}
	mainRoot, err := workspace.Canonical(pointer.ControlRoot)
	if err != nil {
		return nil, err
	}
	worker, err := workspace.Canonical(workerRoot)
	if err != nil || worker == mainRoot {
		return nil, fmt.Errorf("Worker control pointer does not identify another workspace")
	}
	registry, state, err := workspace.Load(mainRoot)
	if err != nil {
		return nil, fmt.Errorf("Worker Main execution registry unavailable: %w", err)
	}
	if registry == nil {
		return nil, fmt.Errorf("Worker Main execution registry unavailable")
	}
	if registry.MainRoot != mainRoot {
		return nil, fmt.Errorf("Worker pointer differs from its registered Main root")
	}
	if err := registry.Validate(ctx, mainRoot); err != nil {
		return nil, err
	}
	e, ok := registry.Execution(pointer.Assignment, pointer.Runtime, workspace.Generation(state))
	if !ok || e.AssignmentID != pointer.Assignment || e.RuntimeID != pointer.Runtime || e.Generation != pointer.Generation || e.Path != worker || e.BootstrapSHA256 == "" {
		return nil, fmt.Errorf("Worker pointer is stale or has no Main-pinned bootstrap receipt")
	}
	coordinate := e
	if restoring {
		coordinate.BootstrapSHA256 = ""
	}
	if err := registry.ValidateExecution(ctx, coordinate, false); err != nil {
		return nil, err
	}
	pointerBytes, err := json.Marshal(map[string]any{
		"control_root": mainRoot, "assignment_id": e.AssignmentID,
		"runtime_id": e.RuntimeID, "execution_generation": e.Generation,
	})
	if err != nil || string(pointerBytes) != string(data) {
		return nil, fmt.Errorf("Worker control pointer differs from its exact registered identity")
	}
	pointerHash := sha256.Sum256(pointerBytes)
	pointerDigest := hex.EncodeToString(pointerHash[:])
	receiptPath := filepath.Join(worker, ".claude", "loop-bootstrap.json")
	receiptBytes, err := os.ReadFile(receiptPath)
	if restoring && os.IsNotExist(err) {
		// The last attempt may have removed the receipt immediately before
		// interruption. Only this exact remaining pointer can be projected;
		// any remaining asset or product dirt still blocks cleanup.
		return map[string]string{".claude/loop-workspace.json": pointerDigest}, nil
	}
	if err != nil {
		return nil, err
	}
	receiptHash := sha256.Sum256(receiptBytes)
	if hex.EncodeToString(receiptHash[:]) != e.BootstrapSHA256 {
		return nil, fmt.Errorf("Worker bootstrap receipt differs from Main's recorded digest")
	}
	var receipt workspace.BootstrapReceipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		return nil, err
	}
	projection := make(map[string]string, len(receipt.Files)+2)
	for path, digest := range receipt.Files {
		if path == ".claude/loop-bootstrap.json" || path == ".claude/loop-workspace.json" {
			return nil, fmt.Errorf("Worker bootstrap receipt includes a reserved control path")
		}
		projection[filepath.ToSlash(filepath.Clean(path))] = digest
	}
	projection[".claude/loop-bootstrap.json"] = e.BootstrapSHA256
	projection[".claude/loop-workspace.json"] = pointerDigest
	return projection, nil
}

func fileMatchesDigest(root, rel, expected string) bool {
	if !validRuntimeCheckpointID(expected) || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	physical, err := workspace.Canonical(path)
	if err != nil || physical != path {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == expected
}

var mainHarnessProjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\.claude/assignments/native/[A-Za-z0-9][A-Za-z0-9._-]*/r[1-9][0-9]*/(task\.md|source\.json|(?:plan|result)-[a-f0-9]{64}\.json)$`),
	regexp.MustCompile(`^\.claude/assignments/[A-Za-z0-9][A-Za-z0-9._-]*\.json$`),
	regexp.MustCompile(`^\.claude/assignment-context/[a-f0-9]{64}/g[1-9][0-9]*/[A-Za-z0-9][A-Za-z0-9._-]*-e[1-9][0-9]*\.json$`),
	regexp.MustCompile(`^\.claude/assignment-context/native/[a-f0-9]{64}/g[1-9][0-9]*/[a-f0-9]{64}\.json$`),
	regexp.MustCompile(`^\.claude/assignment-context/roles/[a-f0-9]{64}\.md$`),
	regexp.MustCompile(`^\.claude/assignment-context/schemas/agent-message-[a-f0-9]{64}\.json$`),
	regexp.MustCompile(`^\.claude/workgroups/[A-Za-z0-9][A-Za-z0-9._-]{0,127}/[A-Za-z0-9][A-Za-z0-9._-]{0,127}/(manifest\.json|manifest\.draft\.json|manifest-s6-r[1-9][0-9]*\.json|readback-request\.json)$`),
	regexp.MustCompile(`^\.claude/workspace-launch/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/attempt-[1-9][0-9]*\.log(\.json)?$`),
	regexp.MustCompile(`^\.claude/workspace-launch/[A-Za-z0-9][A-Za-z0-9._-]{0,127}/g[1-9][0-9]*-[A-Za-z0-9][A-Za-z0-9._-]*-e[1-9][0-9]*\.lock$`),
	regexp.MustCompile(`^\.claude/worktree-reminders/[a-f0-9]{64}\.txt$`),
	regexp.MustCompile(`^\.worktrees/[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`),
}

func mainHarnessProjectionPath(path string) bool {
	for _, pattern := range mainHarnessProjectionPatterns {
		if pattern.MatchString(filepath.ToSlash(filepath.Clean(path))) {
			return true
		}
	}
	return false
}

func registeredMainWorktreeProjections(ctx context.Context, root string) (map[string]bool, error) {
	registry, _, err := workspace.Load(root)
	if err != nil {
		return nil, fmt.Errorf("Main workspace registry unavailable: %w", err)
	}
	if registry == nil {
		return nil, fmt.Errorf("Main workspace registry unavailable")
	}
	mainRoot, err := workspace.Canonical(root)
	if err != nil || mainRoot != registry.MainRoot {
		return nil, fmt.Errorf("worktree projection requires the original Main checkout")
	}
	if err := registry.Validate(ctx, mainRoot); err != nil {
		return nil, err
	}
	listed, err := defaultRunner.Run(ctx, mainRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	worktreePaths, err := parseWorktreeListPaths(listed)
	if err != nil {
		return nil, err
	}
	projected := map[string]bool{}
	for _, execution := range registry.AllExecutions() {
		path, err := workspace.Canonical(execution.Path)
		if err != nil || path != execution.Path || !worktreePaths[path] {
			continue
		}
		rel, err := filepath.Rel(mainRoot, path)
		if err != nil || rel == "." || filepath.Dir(rel) != ".worktrees" || !mainWorktreeNamePattern.MatchString(filepath.Base(rel)) {
			continue
		}
		projected[filepath.ToSlash(rel)] = true
	}
	return projected, nil
}

var mainWorktreeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func parseWorktreeListPaths(output string) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, field := range strings.Split(strings.ReplaceAll(output, "\x00", "\n"), "\n") {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		path := strings.TrimPrefix(field, "worktree ")
		if strings.HasPrefix(path, "\"") {
			decoded, err := strconv.Unquote(path)
			if err != nil {
				return nil, fmt.Errorf("decode registered worktree path: %w", err)
			}
			path = decoded
		}
		canonical, err := workspace.Canonical(path)
		if err == nil {
			paths[canonical] = true
		}
	}
	return paths, nil
}

var auditIndexScratchPattern = regexp.MustCompile(`^\.claude/hook-audit-index-[0-9]+\.scratch$`)

func runtimeProjection(path string, roots ...string) bool {
	if len(roots) > 0 && diagnosticstorage.Recognizes(context.Background(), roots[0], path) {
		return true
	}
	// An interrupted index publication can leave a disposable cache scratch.
	// Its precise storage name grants no issuance or professional business fact.
	if auditIndexScratchPattern.MatchString(path) {
		if len(roots) == 0 {
			return true
		}
		info, err := os.Lstat(filepath.Join(roots[0], filepath.FromSlash(path)))
		return err == nil && info.Mode().IsRegular()
	}
	switch path {
	case ".claude/integration.lock", ".claude/loop-state.json", ".claude/loop-events.jsonl", ".claude/loop-metrics.json", ".claude/hook-decisions.jsonl", ".claude/hook-decisions.jsonl.index", ".claude/runtime-checkpoints/index.json":
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == ".claude" && parts[1] == "hook-metrics" && strings.HasPrefix(parts[2], ".pending-") && strings.HasSuffix(parts[2], ".json") {
		return true
	}
	if len(parts) == 4 && parts[0] == ".claude" && parts[1] == "runtime-checkpoints" && validRuntimeCheckpointID(parts[2]) {
		switch parts[3] {
		case "events.jsonl", "manifest.json", "state.json":
			return true
		}
	}
	legacyCheckpoint := len(parts) == 7 && parts[0] == ".claude" && parts[1] == "evidence" && strings.HasPrefix(parts[2], "loop-") && strings.HasPrefix(parts[3], "g") && parts[4] == "worktree" && parts[5] != "" && parts[6] == "checkpoint.json"
	if legacyCheckpoint {
		return true
	}
	if len(parts) != 8 || parts[0] != ".claude" || parts[1] != "evidence" || !strings.HasPrefix(parts[2], "loop-") || !strings.HasPrefix(parts[3], "g") || parts[4] != "worktree" || parts[5] == "" || parts[7] != "checkpoint.json" {
		return false
	}
	revisionText := strings.TrimPrefix(parts[6], "a")
	revision, err := strconv.Atoi(revisionText)
	return strings.HasPrefix(parts[6], "a") && err == nil && revision >= 2 && strconv.Itoa(revision) == revisionText
}

func validRuntimeCheckpointID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

// branchExists returns true if `git -C root show-ref --verify` exits
// cleanly for refs/heads/<branch>. It is used to verify the target branch
// is real before we attempt any merge.
func branchExists(ctx context.Context, root, branch string) (bool, error) {
	if strings.TrimSpace(branch) == "" {
		return false, errors.New("branch name is required")
	}
	_, err := defaultRunner.Run(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		// A non-zero exit means the ref doesn't exist; anything else
		// (e.g. corrupt refs) is an actual error and should bubble up.
		// The execRunner wraps non-zero exits as ExitError; we treat any
		// error here as "not found" so callers can fall through to the
		// missing-target branch. Real corruption will surface on the
		// subsequent ref read.
		return false, nil
	}
	return true, nil
}

// revParse resolves a branch name (or rev) to its full SHA via
// `git rev-parse <rev>^{commit}`. The ^{commit} suffix dereferences
// annotated tags to their commit object, which is what callers want.
func revParse(ctx context.Context, root, rev string) (string, error) {
	if strings.TrimSpace(rev) == "" {
		return "", errors.New("rev is required")
	}
	out, err := defaultRunner.Run(ctx, root, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", rev, err)
	}
	return strings.TrimSpace(out), nil
}

// mergeBase returns the merge base SHA between source and target, or an
// error if either side does not resolve.
func mergeBase(ctx context.Context, root, source, target string) (string, error) {
	out, err := defaultRunner.Run(ctx, root, "merge-base", source, target)
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s: %w", source, target, err)
	}
	return strings.TrimSpace(out), nil
}

// mergeTreeRuns uses `git merge-tree` to compute the three-way merge
// between source and target and returns the conflict hunks, if any.
// merge-tree exits 0 even on conflict (since Git 2.38 the conflict flag
// is embedded in the output); we therefore parse the output and only
// treat a non-zero exit as an error.
//
// The function returns (conflicts, error). An empty conflicts slice with
// a nil error means the merge would apply cleanly.
func mergeTreeRuns(ctx context.Context, root, mergeBase, source, target string) ([]string, error) {
	out, err := defaultRunner.Run(ctx, root, "merge-tree", mergeBase, source, target)
	if err != nil {
		return nil, fmt.Errorf("git merge-tree: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	// Conflict markers from merge-tree look like:
	//   changed in both
	//   ...
	// We extract the "<<<<<<<" hunks; their presence indicates conflicts.
	if !strings.Contains(out, "<<<<<<<") && !strings.Contains(out, "changed in both") {
		return nil, nil
	}
	hunks := splitConflictHunks(out)
	return hunks, nil
}

func splitConflictHunks(out string) []string {
	var hunks []string
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "changed in both") ||
			strings.HasPrefix(strings.TrimSpace(line), "conflict:") ||
			strings.HasPrefix(line, "<<<<<<<") {
			hunks = append(hunks, strings.TrimSpace(line))
		}
	}
	return hunks
}

// listChangedFiles returns the set of paths that differ between base and
// head, relative to the repository root. Used to compute the locked-artifact
// diff.
func listChangedFiles(ctx context.Context, root, base, head string) ([]string, error) {
	out, err := defaultRunner.Run(ctx, root, "diff", "--no-renames", "--name-only", "-z", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..%s: %w", base, head, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(out, "\x00") {
		if line != "" {
			files = append(files, filepath.Clean(line))
		}
	}
	return files, nil
}

// listCommitsBetween returns the count of commits in `head` that are not
// in `base`. If head == base the count is 0.
func countCommitsBetween(ctx context.Context, root, base, head string) (int, error) {
	out, err := defaultRunner.Run(ctx, root, "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count %s..%s: %w", base, head, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, fmt.Errorf("parse rev-list count: %w", err)
	}
	return n, nil
}

// performMerge runs `git -C targetRoot merge --no-ff <source>` and returns
// the resulting merge commit SHA via `git rev-parse HEAD`. The merge is
// plain (no squash, no fast-forward) per BE-039 §8 / SYNC-039 §8.
func performMerge(ctx context.Context, targetRoot, source string) (string, error) {
	// Build a temporary merge message so the resulting merge commit is
	// distinguishable from a fast-forward. We never use --no-verify /
	// --no-gpg-sign / --amend / --force anywhere in this package.
	msg := "Merge worktree branch " + source
	_, err := defaultRunner.Run(ctx, targetRoot,
		"merge", "--no-ff", "-m", msg, source,
	)
	if err != nil {
		return "", fmt.Errorf("git merge --no-ff %s: %w", source, err)
	}
	head, err := revParse(ctx, targetRoot, "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD after merge: %w", err)
	}
	return head, nil
}

// removeWorktree runs non-forced `git worktree remove` ONLY when the worktree
// is clean. The caller must call worktreeClean first; this function
// performs the additional belt-and-braces check and refuses if any
// uncommitted changes are present.
func removeWorktree(ctx context.Context, repoRoot, worktreePath string) error {
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}
	clean, err := worktreeClean(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("verify clean: %w", err)
	}
	if !clean {
		return fmt.Errorf("Builder checkout %q is dirty; inspect with git -C %q status --short: %w", worktreePath, worktreePath, ErrDirtyWorktree)
	}
	if _, err := defaultRunner.Run(ctx, repoRoot, "worktree", "remove", worktreePath); err != nil {
		// If the worktree is already gone we treat that as success
		// (idempotent cleanup). Anything else is a real error.
		if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// isCommit reports whether `rev` resolves to a commit object. We use this
// as a safety net before invoking the merge — `rev-parse --verify` would
// happily accept a tag, but merge would then complain.
func isCommit(ctx context.Context, root, rev string) (bool, error) {
	out, err := defaultRunner.Run(ctx, root, "cat-file", "-t", rev)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) == "commit", nil
}

// mergeCommitReachable verifies that a previously recorded merge commit is
// still part of the target branch history. Re-verification reuses the merge
// instead of creating a second merge, so it must never run against an
// unrelated checkout or silently trust a stale checkpoint.
func mergeCommitReachable(ctx context.Context, root, mergeCommit, targetBranch string) (bool, error) {
	if strings.TrimSpace(mergeCommit) == "" || strings.TrimSpace(targetBranch) == "" {
		return false, errors.New("merge commit and target branch are required")
	}
	_, err := defaultRunner.Run(ctx, root, "merge-base", "--is-ancestor", mergeCommit, targetBranch)
	if err != nil {
		// Git uses a non-zero exit status for the ordinary "not an
		// ancestor" answer. Callers intentionally turn both that answer
		// and command failures into a closed re-verification gate.
		return false, nil
	}
	return true, nil
}

// checkoutBranch switches the worktree to `branch` (a no-op when already
// there) and returns the resulting HEAD SHA. It is used so the merge runs
// from the target branch, not the source.
func checkoutBranch(ctx context.Context, root, branch string) error {
	current, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) != strings.TrimPrefix(branch, "refs/heads/") {
		return fmt.Errorf("authority root is on %s, expected %s; switch explicitly after preserving local work", strings.TrimSpace(current), branch)
	}
	return nil
}

// RunCheck is the public adapter for the integration check command
// dispatch. It exists so callers can construct a CheckResult outside
// the Integrate state machine — useful for controller-driven flows that
// want to record a single check outcome without advancing the
// checkpoint.
func RunCheck(ctx context.Context, root, command string, run func(ctx context.Context, root, command string) error) (CheckResult, error) {
	if run == nil {
		return CheckResult{Command: command, Status: "fail", Output: ErrCheckRunnerMissing.Error()}, ErrCheckRunnerMissing
	}
	if err := run(ctx, root, command); err != nil {
		return CheckResult{Command: command, Status: "fail", Output: err.Error()}, nil
	}
	return CheckResult{Command: command, Status: "pass"}, nil
}

// Body filenames alone do not grant a clean projection. Validate the actual
// content-addressed complete packet (including a checkpoint's independent copy).
// Publication before the Runtime reference is committed may leave a real orphan;
// recognizing storage here does not assert professional issuance or reception.
func runtimeBodyProjection(ctx context.Context, root, path string) (bool, error) {
	recognized, err := loopruntime.ClassifyPacketBodyStoragePathsContext(ctx, root, []string{path})
	if err != nil {
		return false, fmt.Errorf("Runtime packet storage projection: %w", err)
	}
	return len(recognized) == 1, nil
}
