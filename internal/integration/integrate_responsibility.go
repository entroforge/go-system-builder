package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

// integrateByResponsibility persists actual completed steps. A ready checkpoint
// seen on a later invocation is an ambiguous merge window: without a recorded
// completion, neither a retry nor a Git object ID can establish what occurred.
func integrateByResponsibility(ctx context.Context, req IntegrateRequest, cfg IntegrateConfig) (result Result, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	start := time.Now()
	defer func() {
		if cfg.Root != "" {
			_ = metrics.ObserveIntegrationDuration(cfg.Root, integrationDurationStatus(result, resultErr), time.Since(start).Milliseconds())
		}
	}()
	if cfg.Root == "" {
		return Result{}, errors.New("config.Root is required")
	}
	in := req.Inspection
	if in.WorktreePath == "" || in.SourceBranch == "" || in.TargetBranch == "" {
		return Result{}, errors.New("original worktree and source/target branches are required")
	}
	if !in.NonSquashMode {
		return Result{}, ErrSquashForbidden
	}
	gitRoot := cfg.GitRoot
	if gitRoot == "" {
		gitRoot = cfg.Root
	}
	ctx, release, err := LockWorkspace(ctx, gitRoot)
	if err != nil {
		return Result{}, err
	}
	defer release()
	store := cfg.CheckpointStore
	if store == nil {
		store = DefaultCheckpointStore()
	}
	runtimeID := cfg.RuntimeID
	if runtimeID == "" && in.CanonicalResult != nil {
		runtimeID = in.CanonicalResult.AssignmentRecord.RuntimeID
	}
	if runtimeID == "" {
		runtimeID = "loop-REQ-039"
	}

	var canonical *canonicalResultFacts
	projection := in.AssignmentProjection
	if projection.ManifestRef != "" || projection.AssignmentRecord != nil || projection.AssignmentRevision != 0 {
		canonical, err = resolveCanonicalResult(cfg.Root, runtimeID, projection)
	} else if in.CanonicalResult != nil {
		canonical, err = resolveCanonicalResultByBinding(cfg.Root, runtimeID, *in.CanonicalResult)
	}
	if err != nil {
		return Result{}, fmt.Errorf("resolve original Assignment/Result: %w", err)
	}
	if canonical != nil && in.CanonicalResult != nil && !sameCanonicalResultBinding(canonical.Binding, *in.CanonicalResult) {
		return Result{}, fmt.Errorf("inspection names a different Assignment/Result")
	}
	checkpointPath := cfg.CheckpointDir
	if canonical != nil {
		expected := store.PathForRecord(cfg.Root, canonical.Record.Reference())
		if checkpointPath != "" && filepath.Clean(checkpointPath) != filepath.Clean(expected) {
			return Result{}, fmt.Errorf("checkpoint path differs from original Assignment revision")
		}
		checkpointPath = expected
	} else if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, in.BaselineGeneration, idempAssignment(req))
	}
	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, err
	}
	if found && (current.AssignmentID != idempAssignment(req) || current.BaselineGeneration != in.BaselineGeneration || current.TargetBranch != "" && current.TargetBranch != in.TargetBranch) {
		return Result{Checkpoint: current}, fmt.Errorf("checkpoint belongs to another original responsibility")
	}
	if canonical != nil && found {
		if err := ValidateCheckpointRecordBinding(current, canonical.Record); err != nil {
			return Result{Checkpoint: current}, err
		}
		if current.CanonicalMainFacts != nil && !sameCanonicalResultBinding(current.CanonicalMainFacts.Binding, canonical.Binding) {
			if current.State == StateReady || (current.State == StatePreserved || current.State == StateBlocked) && current.ResumeState == StateReady {
				return Result{Checkpoint: current}, fmt.Errorf("previous merge outcome is unknown; Main must inspect the original worktree")
			}
			// A new accepted professional result is a new Main integration/check
			// attempt. Git can merge the current branch normally (including a
			// legitimate no-change result); old checks/ACK do not certify it.
			next := cleanCheckpoint(current)
			next.State, next.ResumeState = StatePending, ""
			next.VerifiedAt, next.FailureReason, next.LastErrorCode = "", "", ""
			next.CheckReceipts = nil
			next.CanonicalMainFacts = nil
			ref := canonical.Record.Reference()
			next.AssignmentRecord = &ref
			next.CompletionReportPath = canonical.Binding.ResultPath
			current, err = store.CompareAndSwap(checkpointPath, current, next)
			if err != nil {
				return Result{Checkpoint: current}, err
			}
		}
	}
	if canonical != nil {
		if blockers := canonicalClosingBlockers(canonical); len(blockers) != 0 {
			return preserveCanonicalClosingPending(store, checkpointPath, current, found, in, canonical.Record, blockers)
		}
		cfg.RequiredChecks = append([]string(nil), canonical.Record.RequiredChecks...)
		cfg.CheckRunner = canonicalMainCheckRunner(canonical.Record, mainCheckBinding(canonical))
	}
	if found && current.State == StateComplete {
		return Result{Checkpoint: current, Reused: true}, nil
	}
	if found && current.State == StateReady {
		return Result{Checkpoint: current}, fmt.Errorf("merge outcome is unknown after interruption; Main must inspect the original worktree before continuing")
	}
	if found && (current.State == StateBlocked || current.State == StatePreserved) {
		if !req.RetryPreserved {
			return Result{Checkpoint: current, Reused: true}, nil
		}
		resume := checkpointResumeState(current)
		if resume == "" || resume == StateReady {
			return Result{Checkpoint: current}, fmt.Errorf("merge outcome is unknown after interruption; preserve the original worktree")
		}
		next := cleanCheckpoint(current)
		next.State, next.ResumeState, next.FailureReason, next.LastErrorCode = resume, "", "", ""
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, err
		}
	}
	if !found {
		current = Checkpoint{AssignmentID: idempAssignment(req), TaskID: in.TaskID, WorktreePath: in.WorktreePath, SourceBranch: in.SourceBranch, TargetBranch: in.TargetBranch, BaselineGeneration: in.BaselineGeneration, State: StatePending, CompletionReportPath: in.CompletionReportPath, ChangedPaths: append([]string(nil), in.ChangedPaths...)}
		identity := current.AssignmentID
		if canonical != nil {
			identity = fmt.Sprintf("%s:r%d", identity, canonical.Record.Revision)
			ref := canonical.Record.Reference()
			current.AssignmentRecord = &ref
		}
		current.IdempotencyKey = IdempotencyKey(identity, in.TargetBranch, in.BaselineGeneration)
	}
	if lessThan(current.State, StateMerged) {
		if !in.Ready {
			reason := strings.Join(in.Blockers, "; ")
			if reason == "" {
				reason = "inspection reported an integration blocker"
			}
			// The preserved checkpoint keeps the concrete unsatisfied
			// conditions, not only their summary in FailureReason.
			next := cleanCheckpoint(current)
			next.Blockers = append(append([]string(nil), next.Blockers...), in.Blockers...)
			sentinel := ErrMergeConflict
			if len(in.WorktreeDirty) != 0 {
				// Observed uncommitted material is its own recorded reason to
				// keep the site; it is observed, never inferred from a digest.
				next.Blockers = append(next.Blockers, dirtyWorktreeBlocker(in.WorktreeDirty))
				sentinel = ErrDirtyWorktree
			}
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, errors.New(reason), sentinel)
		}
		status, err := defaultRunner.Run(ctx, gitRoot, "status", "--porcelain=v1", "--untracked-files=no")
		if err != nil {
			return Result{Checkpoint: current}, mainWorktreeCheckFailure(gitRoot, err)
		}
		if strings.TrimSpace(status) != "" {
			return Result{Checkpoint: current}, mainWorktreeDirtyFailure(gitRoot)
		}
		if err := checkoutBranch(ctx, gitRoot, in.TargetBranch); err != nil {
			return Result{Checkpoint: current}, err
		}
		if err := protectIgnoredMergeCollisions(ctx, gitRoot, in.SourceBranch); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, cleanCheckpoint(current), err, ErrMergeConflict)
		}
		next := cleanCheckpoint(current)
		next.State = StateReady
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, err
		}
		// This is the real Git operation. Git resolves its own objects; the
		// framework neither records nor compares their IDs.
		if _, err = defaultRunner.Run(ctx, gitRoot, "merge", "--no-ff", "--no-overwrite-ignore", "-m", "Merge worktree branch "+in.SourceBranch, in.SourceBranch); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, current, err, ErrMergeConflict)
		}
		next = cleanCheckpoint(current)
		next.State = StateMerged
		if canonical != nil {
			next.CanonicalMainFacts = &assignmentresult.MainFacts{Binding: canonical.Binding, MainRoot: gitRoot, TargetBranch: in.TargetBranch, ExecutionGeneration: canonical.ExecutionGeneration, ChangedPaths: append([]string{}, in.ChangedPaths...), Checks: []assignmentresult.MainCheckFact{}}
			next.AssignmentRecord = nil
		}
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{Checkpoint: current}, fmt.Errorf("merge completed but its checkpoint was not saved; Main must inspect the original worktree: %w", err)
		}
	}
	if lessThan(current.State, StateVerified) {
		next := cleanCheckpoint(current)
		if len(in.WorktreeDirty) != 0 {
			// The worktree material that is really present becomes a recorded
			// checkpoint fact. It never blocks the merge itself; a preserved
			// checkpoint carries it so Main keeps the site and can act on it.
			next.Blockers = append(append([]string(nil), next.Blockers...), dirtyWorktreeBlocker(in.WorktreeDirty))
		}
		commands, _ := splitRequiredChecks(cfg.RequiredChecks)
		var indexed []struct {
			Index   int
			Command string
		}
		if canonical != nil {
			indexed = mainCheckIndexes(canonical.Record)
		}
		for index, command := range commands {
			checkIndex := index
			if index < len(indexed) && indexed[index].Command == command {
				checkIndex = indexed[index].Index
			}
			if cfg.CheckRunner == nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrCheckRunnerMissing, ErrCheckFailed)
			}
			evidence := checkEvidence{Directory: filepath.Join(filepath.Dir(checkpointPath), "checks"), Receipts: &next.CheckReceipts}
			before := len(next.CheckReceipts)
			if err := cfg.CheckRunner(context.WithValue(ctx, checkEvidenceKey{}, evidence), gitRoot, command); err != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
			}
			if next.CanonicalMainFacts != nil {
				fact := assignmentresult.MainCheckFact{CheckIndex: checkIndex, Command: command, Status: "pass"}
				if len(next.CheckReceipts) > before {
					fact.ReceiptRef = next.CheckReceipts[len(next.CheckReceipts)-1]
				}
				facts := *next.CanonicalMainFacts
				facts.Checks = append(facts.Checks, fact)
				next.CanonicalMainFacts = &facts
			}
		}
		next.State = StateVerified
		next.VerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if store.Clock != nil {
			next.VerifiedAt = store.Clock().UTC().Format(time.RFC3339Nano)
		}
		if in.CompletionReportPath != "" {
			next.CompletionReportPath = in.CompletionReportPath
		}
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, err
		}
	}
	if lessThan(current.State, StateAcknowledged) {
		if !req.Acknowledge {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next := cleanCheckpoint(current)
		next.State = StateAcknowledged
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, err
		}
	}
	if lessThan(current.State, StateCleanupPending) {
		if !req.Cleanup {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next := cleanCheckpoint(current)
		next.State = StateCleanupPending
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, err
		}
	}
	if current.State == StateCleanupPending && req.Cleanup {
		if err := cleanupUnmodifiedWorktree(ctx, gitRoot, current.WorktreePath); err != nil {
			return deferCleanup(store, checkpointPath, current, cleanCheckpoint(current), err)
		}
		next := cleanCheckpoint(current)
		next.State, next.FailureReason, next.LastErrorCode = StateComplete, "", ""
		current, err = store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{Checkpoint: current}, err
		}
	}
	return Result{Checkpoint: current, Reused: found}, nil
}

// protectIgnoredMergeCollisions handles Git versions whose ort strategy still
// overwrites ignored files despite --no-overwrite-ignore. Only names that this
// branch actually adds or changes are compared; file contents are never read.
func protectIgnoredMergeCollisions(ctx context.Context, root, branch string) error {
	changed, err := defaultRunner.Run(ctx, root, "diff", "--name-only", "-z", "--diff-filter=ACMRT", "HEAD..."+branch, "--")
	if err != nil {
		return fmt.Errorf("inspect incoming merge paths: %w", err)
	}
	ignored, err := defaultRunner.Run(ctx, root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("inspect existing ignored paths: %w", err)
	}
	existingPaths := strings.Split(ignored, "\x00")
	sort.Strings(existingPaths)
	for _, incoming := range strings.Split(changed, "\x00") {
		if incoming == "" {
			continue
		}
		for path := incoming; path != "."; path = filepath.ToSlash(filepath.Dir(path)) {
			i := sort.SearchStrings(existingPaths, path)
			if i < len(existingPaths) && existingPaths[i] == path {
				return fmt.Errorf("merge would overwrite existing path %q", path)
			}
		}
		prefix := incoming + "/"
		i := sort.SearchStrings(existingPaths, prefix)
		if i < len(existingPaths) && strings.HasPrefix(existingPaths[i], prefix) {
			return fmt.Errorf("merge would overwrite existing path %q", existingPaths[i])
		}
	}
	return nil
}

// dirtyWorktreeBlocker renders the material actually present in the Builder
// worktree as a checkpoint blocker. The wording keeps ErrDirtyWorktree's
// existing meaning, so the checkpoint states that the site is dirty and
// preserved. The judgement comes from the observed worktree state only; no
// digest, revision or round is compared.
func dirtyWorktreeBlocker(material []string) string {
	return fmt.Sprintf("%s: %s", ErrDirtyWorktree, strings.Join(material, "; "))
}

// Copy mutable Main facts before updating the checkpoint.
func cleanCheckpoint(in Checkpoint) Checkpoint {
	out := in
	if in.CanonicalMainFacts != nil {
		facts := *in.CanonicalMainFacts
		facts.Checks = append([]assignmentresult.MainCheckFact(nil), facts.Checks...)
		out.CanonicalMainFacts = &facts
	}
	return out
}

// AuthorOriginalsRoot is the author's own professional area inside a Worker
// checkout. The material there is delivery material, not unknown garbage: the
// delivered copy is already recorded in the Main root, so its presence does
// not refuse the reclamation of the Worker site.
const authorOriginalsRoot = ".loop-submissions"

// worktreeStatusEntry is one observed Git status entry of a Worker checkout.
type worktreeStatusEntry struct {
	code string // porcelain v1 status code
	path string // path relative to the checkout root
	line string // the entry as Git reported it
}

// worktreeMaterial is the observed material of a Worker checkout, split by
// what it means for the reclamation of the site.
type worktreeMaterial struct {
	tracked  []worktreeStatusEntry // recorded changes of tracked files
	original []worktreeStatusEntry // the author's own professional originals
	unknown  []worktreeStatusEntry // untracked or ignored material of unknown origin
}

// keepsSite reports whether the observed material still keeps the checkout.
// Real engineering artifacts and unknown material do; the author's own
// originals are delivery material and never keep the site.
func (m worktreeMaterial) keepsSite() bool {
	return len(m.tracked) > 0 || len(m.unknown) > 0
}

// Git's own worktree removal also deletes ignored files. Preserve any checkout
// whose tracked files carry recorded changes and any checkout with unknown
// material rather than assuming ownership from a historical digest. The
// author's own professional originals are delivery material: they do not
// refuse the reclamation, so the cleanup authorization follows the recorded
// responsibility and not the presence of a file in the directory.
func cleanupUnmodifiedWorktree(ctx context.Context, root, worktree string) error {
	if worktree == "" || filepath.Clean(worktree) == filepath.Clean(root) {
		return fmt.Errorf("preserve unknown worktree location")
	}
	info, err := os.Lstat(worktree)
	if os.IsNotExist(err) {
		list, listErr := defaultRunner.Run(ctx, root, "worktree", "list", "--porcelain")
		if listErr != nil {
			return listErr
		}
		for _, row := range strings.Split(strings.ReplaceAll(list, "\x00", "\n"), "\n") {
			if !strings.HasPrefix(row, "worktree ") {
				continue
			}
			registered := strings.TrimPrefix(row, "worktree ")
			if strings.HasPrefix(registered, "\"") {
				registered, listErr = strconv.Unquote(registered)
				if listErr != nil {
					return listErr
				}
			}
			if filepath.Clean(registered) == filepath.Clean(worktree) {
				_, listErr = defaultRunner.Run(ctx, root, "worktree", "remove", worktree)
				return listErr
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("preserve non-directory worktree")
	}
	material, err := observeWorktreeStatus(ctx, worktree)
	if err != nil {
		return err
	}
	if material.keepsSite() {
		return preserveWorktreeError(worktree, material)
	}
	force := len(material.original) > 0
	if force {
		// The author's own originals are still in the checkout, and Git
		// refuses to remove a checkout that holds untracked files. The
		// observed status is read once more right before the removal, so a
		// recorded change of a tracked file keeps the site there as well.
		material, err = observeWorktreeStatus(ctx, worktree)
		if err != nil {
			return err
		}
		if material.keepsSite() {
			return preserveWorktreeError(worktree, material)
		}
		force = len(material.original) > 0
	}
	remove := []string{"worktree", "remove"}
	if force {
		// The Worker site goes with the author's own originals: their
		// delivered copy is already recorded in the Main root.
		remove = append(remove, "--force")
	}
	remove = append(remove, worktree)
	if _, err := defaultRunner.Run(ctx, root, remove...); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	return nil
}

// observeWorktreeStatus reads the actual Git status of a Worker checkout and
// classifies the material that is really present there.
func observeWorktreeStatus(ctx context.Context, worktree string) (worktreeMaterial, error) {
	status, err := defaultRunner.Run(ctx, worktree, "status", "--porcelain=v1", "--ignored=matching", "--untracked-files=all")
	if err != nil {
		return worktreeMaterial{}, fmt.Errorf("inspect worktree %q for cleanup: %w", worktree, err)
	}
	return classifyWorktreeStatus(status), nil
}

// classifyWorktreeStatus splits Git's observed porcelain status into the
// material that keeps the Worker checkout and the material that does not. The
// judgement comes from the observed status only; no digest, revision or round
// is compared.
func classifyWorktreeStatus(status string) worktreeMaterial {
	material := worktreeMaterial{}
	for _, line := range strings.Split(strings.TrimRight(status, "\r\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 || line[2] != ' ' {
			// An entry without the documented shape is unknown material.
			material.unknown = append(material.unknown, worktreeStatusEntry{code: "??", path: line, line: line})
			continue
		}
		entry := worktreeStatusEntry{code: line[:2], path: line[3:], line: line}
		if entry.code == "??" || entry.code == "!!" {
			// Untracked or ignored material.
			if isAuthorOriginal(entry.path) {
				material.original = append(material.original, entry)
				continue
			}
			material.unknown = append(material.unknown, entry)
			continue
		}
		// A recorded change of a tracked file is a real engineering artifact;
		// it keeps the site.
		material.tracked = append(material.tracked, entry)
	}
	return material
}

// isAuthorOriginal reports whether the observed path is the author's own
// professional material inside the Worker checkout. A quoted path is never
// read as a delivery: the framework does not own a name it cannot read.
func isAuthorOriginal(path string) bool {
	if strings.HasPrefix(path, "\"") {
		return false
	}
	return path == authorOriginalsRoot || strings.HasPrefix(path, authorOriginalsRoot+"/")
}

// preserveWorktreeError reports the material that keeps the Worker checkout.
func preserveWorktreeError(worktree string, material worktreeMaterial) error {
	entries := make([]string, 0, len(material.tracked)+len(material.unknown))
	for _, entry := range material.tracked {
		entries = append(entries, entry.line)
	}
	for _, entry := range material.unknown {
		entries = append(entries, entry.line)
	}
	// Show the actual retained locations without treating their names or
	// Git classification as authority to remove any of them. Git can
	// summarize ignored directories; those entries identify the directory,
	// not an exhaustive list of its contents.
	const maxEntries = 8
	detail := strings.Join(entries, "; ")
	if len(entries) > maxEntries {
		omitted := len(entries) - maxEntries
		detail = strings.Join(entries[:maxEntries], "; ") + fmt.Sprintf("; and %d more Git status entries", omitted)
	}
	return fmt.Errorf("preserve worktree %q: %s has no confirmed cleanup authorization (Git status: %s)", worktree, preservedMaterialKind(material), detail)
}

// preservedMaterialKind names the material that keeps the Worker checkout.
func preservedMaterialKind(material worktreeMaterial) string {
	switch {
	case len(material.tracked) > 0 && len(material.unknown) > 0:
		return "modified tracked material and unknown untracked or ignored material"
	case len(material.tracked) > 0:
		return "modified tracked material"
	default:
		return "unknown untracked or ignored material"
	}
}
