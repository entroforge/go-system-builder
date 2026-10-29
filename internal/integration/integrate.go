package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

// CheckpointStoreFactory returns the CheckpointStore Integrate should use.
// The default (nil) means use the package-level default. Tests override it
// to inject a deterministic clock.
type CheckpointStoreFactory func() *CheckpointStore

// IntegrateConfig tunes Integrate's behaviour.
type IntegrateConfig struct {
	// WorkerPrivateEvidence contains only original accepted professional files.
	// It is separate from captured logs and grants no directory-wide exemption.
	WorkerPrivateEvidence map[string]string
	// WorkerPrivateArea is the original responsibility's call-only scratch
	// area; its frozen product boundaries are supplied alongside it.
	WorkerPrivateArea          string
	WorkerPrivateBaseCommit    string
	WorkerPrivateWritePaths    []string
	WorkerPrivateBaselinePaths []string
	// WorkerIgnoredEvidence proves exact system-issued/accepted ignored bytes.
	// Unknown ignored files always prevent removal, including when this is nil.
	WorkerIgnoredEvidence map[string]string
	// WorkerCleanupError defers an original receiver proof failure to resource
	// cleanup, after an already accepted Final has exposed its successor.
	WorkerCleanupError error
	// WorkerLogEvidence is the original receiver's captured output projection.
	// Verified cleanup may remove only these unchanged untracked copies; Main's
	// immutable evidence remains the source for accepted Results.
	WorkerLogEvidence map[string]string
	// CheckpointStore selects the persistence backend. nil → default.
	CheckpointStore *CheckpointStore
	// CheckpointDir, when set, overrides the canonical evidence
	// directory layout. Tests use it to point at a temp dir.
	CheckpointDir string
	// Root, when set, is the repository root used to compute the
	// canonical checkpoint path. Required.
	Root string
	// RuntimeID, when set, is the runtime identifier used to compute
	// the canonical checkpoint path. Defaults to "loop-REQ-039".
	RuntimeID string
	// CheckRunner runs integration checks; nil with required checks fails closed.
	CheckRunner RequiredCheckRunner
	// RequiredChecks lists commands to run after merge for the
	// verified transition.
	RequiredChecks []string
	// GitRoot is the repository root used for git operations. Defaults
	// to cfg.Root when unset.
	GitRoot string
}

// Integrate drives the checkpoint state machine for one assignment:
//
//	pending → ready → merged → verified → acknowledged → cleanup_pending → complete
//	             \_________________________________________________→ blocked
//	             \_________________________________________________→ preserved
//
// The function is idempotent at the (assignment_id + source_head +
// target_branch + baseline_generation) key. Re-calling Integrate with the
// same idempotent key after the merge succeeded but before
// acknowledgement (CT-039-17) resumes the state machine from `merged` and
// does NOT re-merge. Opted-in S6 Records additionally select this checkpoint
// by their already-resolved immutable revision; a later revision cannot read
// the predecessor slot's verification or acknowledgement.
//
// The caller controls the merge step by setting IntegrateRequest.Cleanup.
// Acknowledge is automatic once verified; a separate flag is provided so
// BUG-06 can call Integrate twice — once for the merge/verify phase and
// again after writing the completion_ack.
func Integrate(ctx context.Context, req IntegrateRequest, cfg IntegrateConfig) (result Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	start := time.Now()
	defer func() {
		if cfg.Root == "" {
			return
		}
		_ = metrics.ObserveIntegrationDuration(cfg.Root, integrationDurationStatus(result, err), time.Since(start).Milliseconds())
	}()
	if cfg.Root == "" {
		return Result{}, errors.New("config.Root is required")
	}
	gitRoot := cfg.GitRoot
	if gitRoot == "" {
		gitRoot = cfg.Root
	}
	// Checkpoints may be stored separately, but serialization belongs to
	// the checkout whose Git state this operation actually mutates.
	ctx, release, err := LockWorkspace(ctx, gitRoot)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if req.Inspection.WorktreePath == "" {
		return Result{}, errors.New("inspection.worktree_path is required")
	}
	if req.Inspection.SourceBranch == "" {
		return Result{}, errors.New("inspection.source_branch is required")
	}
	if req.Inspection.TargetBranch == "" {
		return Result{}, errors.New("inspection.target_branch is required")
	}
	if !req.Inspection.NonSquashMode {
		return Result{}, ErrSquashForbidden
	}

	store := cfg.CheckpointStore
	if store == nil {
		store = DefaultCheckpointStore()
	}
	runtimeID := cfg.RuntimeID
	if runtimeID == "" {
		if req.Inspection.CanonicalResult != nil {
			runtimeID = req.Inspection.CanonicalResult.AssignmentRecord.RuntimeID
		} else {
			runtimeID = "loop-REQ-039"
		}
	}
	var canonical *canonicalResultFacts
	projection := req.Inspection.AssignmentProjection
	hasProjection := projection.ManifestRef != "" || projection.AssignmentRecord != nil || projection.AssignmentRecordSHA256 != "" || projection.AssignmentRevision != 0
	if hasProjection {
		canonical, err = resolveCanonicalResult(cfg.Root, runtimeID, projection)
	} else if req.Inspection.CanonicalResult != nil {
		canonical, err = resolveCanonicalResultByBinding(cfg.Root, runtimeID, *req.Inspection.CanonicalResult)
	}
	if err != nil {
		return Result{}, fmt.Errorf("revalidate current canonical Assignment/Result: %w", err)
	}
	if canonical == nil && req.Inspection.CanonicalResult != nil || canonical != nil && req.Inspection.CanonicalResult == nil {
		return Result{}, fmt.Errorf("inspection is missing or invents its canonical Result binding")
	}
	checkpointPath := cfg.CheckpointDir
	if canonical != nil {
		expectedCheckpointPath := store.PathForRecord(cfg.Root, canonical.Record.Reference())
		if expectedCheckpointPath == "" {
			return Result{}, fmt.Errorf("current canonical Assignment Record has no valid checkpoint path")
		}
		if checkpointPath != "" && filepath.Clean(checkpointPath) != filepath.Clean(expectedCheckpointPath) {
			return Result{}, fmt.Errorf("canonical Main facts require the current Assignment revision checkpoint path")
		}
		checkpointPath = expectedCheckpointPath
	} else if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, req.Inspection.BaselineGeneration, idempAssignment(req))
	}
	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, fmt.Errorf("load checkpoint: %w", err)
	}
	if canonical != nil && found {
		if err := ValidateCheckpointRecordBinding(current, canonical.Record); err != nil {
			return Result{Checkpoint: current}, err
		}
	}
	readyMerge := ""
	if found && current.State == StateReady {
		readyMerge, err = RecoverUnrecordedMerge(ctx, gitRoot, current)
		if err != nil {
			return Result{Checkpoint: current}, err
		}
	}
	if canonical != nil {
		if canonical.Binding != *req.Inspection.CanonicalResult || req.Inspection.AssignmentID != canonical.Record.AssignmentID || req.Inspection.TaskID != canonical.Record.Task.ID || req.Inspection.BaselineGeneration != canonical.Record.BaselineGeneration || len(req.Inspection.EffectiveWritePaths) > 0 && !samePathSet(req.Inspection.EffectiveWritePaths, canonical.Record.EffectiveWritePaths) {
			return Result{Checkpoint: current}, fmt.Errorf("inspection coordinates/scope differ from the current canonical Assignment")
		}
		// Recovery inspections may be reconstructed from a checkpoint and
		// therefore omit transient projection fields. The current Record is
		// the scope authority for the locked Git diff check below.
		req.Inspection.EffectiveWritePaths = append([]string{}, canonical.Record.EffectiveWritePaths...)
		for _, profile := range canonical.Record.CheckProfiles {
			if profile.Location != "main" {
				return Result{Checkpoint: current}, fmt.Errorf("required CheckProfile %s is not declared for Main execution", profile.ID)
			}
		}
		cfg.RequiredChecks = append([]string{}, canonical.Record.RequiredChecks...)
		cfg.CheckRunner = canonicalMainCheckRunner(canonical.Record, mainCheckBinding(canonical))
	} else if found && current.CanonicalMainFacts != nil {
		return Result{Checkpoint: current}, fmt.Errorf("legacy inspection cannot reuse a canonical v1.2 Main checkpoint")
	}
	if canonical != nil {
		if blockers := canonicalClosingBlockers(canonical); len(blockers) > 0 {
			return preserveCanonicalClosingPending(store, checkpointPath, current, found, req.Inspection, canonical.Record, blockers)
		}
		// A fresh/unmerged failed Inspect is already the original source of
		// truth for a dirty worktree, missing commit, or other readiness cause.
		// Preserve that concrete cause before asking Git to compute a merge base;
		// an early Inspect blocker may intentionally leave SourceHead/MergeBase
		// empty. Once a merge exists, retain the existing post-merge
		// revalidation path below.
		if !req.Inspection.Ready && current.MergeCommit == "" &&
			(req.Inspection.SourceHead == "" || req.Inspection.TargetHead == "" || req.Inspection.MergeBase == "") {
			reason := strings.Join(req.Inspection.Blockers, "; ")
			if reason == "" {
				reason = "inspect rejected integration"
			}
			return preserveFromInspection(cfg, req, canonical, reason)
		}
	}
	canonicalGitFactsChanged := false
	if canonical != nil {
		base, sourceHead := req.Inspection.MergeBase, req.Inspection.SourceHead
		alreadyMerged := found && (current.MergeCommit != "" || readyMerge != "")
		if alreadyMerged {
			base, sourceHead = current.MergeBase, current.SourceHead
			if base == "" || sourceHead == "" || current.TargetBranch != req.Inspection.TargetBranch {
				return Result{Checkpoint: current}, fmt.Errorf("canonical re-verification lacks the original merged Git coordinates")
			}
		} else {
			// Inspection is a snapshot, not an authority for its own merge base.
			// Recompute the base while holding the Main workspace lock so a
			// caller cannot present the already-integrated source with a
			// predecessor's old base and trick a fresh revision checkpoint into
			// treating an empty merge as a new integration.
			actualBase, baseErr := mergeBase(ctx, gitRoot, req.Inspection.SourceBranch, req.Inspection.TargetBranch)
			if baseErr != nil || actualBase != base {
				return preserveFromInspection(cfg, req, canonical, "canonical Main merge base changed after Inspect; fresh revision source inspection is required")
			}
			if canonical.Record.Revision > 1 {
				priorMerges, mergeErr := canonicalPriorMainMerges(ctx, gitRoot, req.Inspection.TargetBranch, sourceHead)
				if mergeErr != nil {
					return Result{Checkpoint: current}, fmt.Errorf("inspect predecessor Main merge history: %w", mergeErr)
				}
				if len(priorMerges) > 0 {
					if _, provenanceErr := resolveCanonicalPredecessorMain(ctx, cfg.Root, gitRoot, req.Inspection.TargetBranch, sourceHead, actualBase, canonical.Record, store, priorMerges); provenanceErr != nil {
						reason := "canonical revision predecessor Main provenance could not be validated: " + provenanceErr.Error()
						req.Inspection.Blockers = appendUniqueString(req.Inspection.Blockers, reason)
						return preserveFromInspection(cfg, req, canonical, "canonical revision merge reuse is pending predecessor Main-provenance validation")
					}
				}
			}
			count, countErr := countCommitsBetween(ctx, gitRoot, actualBase, sourceHead)
			if countErr != nil {
				req.Inspection.Blockers = appendUniqueString(req.Inspection.Blockers, "canonical source Git history could not be verified")
				return preserveFromInspection(cfg, req, canonical, "canonical source Git history could not be verified; fresh Main inspection is required")
			}
			if count == 0 {
				if canonical.Record.Revision > 1 {
					req.Inspection.Blockers = appendUniqueString(req.Inspection.Blockers, "canonical revision source is already integrated; predecessor Record and durable Main facts are not safely reusable yet")
					return preserveFromInspection(cfg, req, canonical, "canonical revision merge reuse is pending predecessor Main-facts validation")
				}
				return preserveFromInspection(cfg, req, canonical, "canonical source has no commits beyond the current Main merge base")
			}
		}
		actualSourceHead, sourceErr := retainedIntegrationSource(ctx, gitRoot, req.Inspection)
		if sourceErr != nil || actualSourceHead != sourceHead {
			return Result{Checkpoint: current}, fmt.Errorf("canonical source branch no longer resolves to its original inspected commit")
		}
		actualPaths, actualPathsSHA, pathsErr := canonicalGitChangedPaths(ctx, gitRoot, base, sourceHead)
		if pathsErr != nil {
			return Result{Checkpoint: current}, pathsErr
		}
		if !alreadyMerged && (req.Inspection.ChangedPathsSHA256 == "" || req.Inspection.ChangedPathsSHA256 != actualPathsSHA || !samePathSet(req.Inspection.ChangedPaths, actualPaths)) {
			return Result{Checkpoint: current}, fmt.Errorf("canonical Inspection changed paths differ from the locked Main Git diff")
		}
		if outside := outsideWriteScope(actualPaths, canonical.Record.EffectiveWritePaths); len(outside) > 0 {
			return Result{Checkpoint: current}, fmt.Errorf("%w: %s", ErrScopeViolation, strings.Join(outside, ", "))
		}
		if alreadyMerged && current.CanonicalMainFacts != nil &&
			(current.CanonicalMainFacts.MergeBase != base || current.CanonicalMainFacts.SourceHead != sourceHead || current.CanonicalMainFacts.ChangedPathsSHA256 != actualPathsSHA || !samePathSet(current.CanonicalMainFacts.ChangedPaths, actualPaths)) {
			canonicalGitFactsChanged = true
		}
		if alreadyMerged {
			// A new accepted Result reuses the same recorded non-squash merge.
			// Keep its original source, target and diff denominator while Main
			// checks are rerun against the current Main tree.
			req.Inspection.SourceHead = sourceHead
			req.Inspection.MergeBase = base
			req.Inspection.TargetHead = current.TargetHead
		}
		req.Inspection.ChangedPaths = actualPaths
		req.Inspection.ChangedPathsSHA256 = actualPathsSHA
	}
	if canonical != nil && readyMerge != "" && req.Inspection.CompletionReportPath == "" && req.Inspection.CompletionReportSHA256 == "" {
		// A ready checkpoint already pins the accepted Result in MainFacts,
		// but its completion fields are written only after checks. Restore
		// that original binding when reconstructing the interrupted request.
		req.Inspection.CompletionReportPath = canonical.Binding.ResultPath
		req.Inspection.CompletionReportSHA256 = canonical.Binding.ResultSHA256
	}
	completionBinding, reportChanged, bindingErr := resolveCompletionReportBinding(cfg.Root, req.Inspection, current, found)
	if bindingErr != nil {
		return Result{Checkpoint: current}, bindingErr
	}
	canonicalBindingChanged := false
	if canonical != nil {
		canonicalBindingChanged = current.CanonicalMainFacts == nil || current.CanonicalMainFacts.Binding != canonical.Binding
		if canonicalBindingChanged {
			reportChanged = true
		}
	}
	// A verified/acknowledged/complete checkpoint is reusable only for the
	// exact Result bytes it verified. A changed report re-enters the check
	// stage while retaining MergeCommit, so recovery never merges twice.
	reverifyResult := found && (reportChanged || canonicalGitFactsChanged) && checkpointHasVerifiedStage(current)
	if canonical != nil && found && checkpointHasVerifiedStage(current) && !canonicalBindingChanged {
		if err := validateCanonicalMainFacts(cfg.Root, checkpointPath, gitRoot, current, canonical); err != nil {
			reverifyResult = true
		}
	}
	if found && checkpointHasVerifiedStage(current) {
		if current.MergeCommit == "" {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("verified checkpoint has no merge receipt; re-inspect and reconcile"))
		}
		if current.TargetBranch == "" || current.TargetBranch != req.Inspection.TargetBranch {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("target branch changed from %s to %s after recorded merge", current.TargetBranch, req.Inspection.TargetBranch))
		}
		reachable, reachErr := mergeCommitReachable(ctx, gitRoot, current.MergeCommit, req.Inspection.TargetBranch)
		if reachErr != nil || !reachable {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("recorded merge commit %s is not reachable from target %s; re-inspect and reconcile", current.MergeCommit, req.Inspection.TargetBranch))
		}
	}

	// Before the first acknowledgement, checks must cover the current target.
	// Completed historical deliveries and cleanup-only retries retain their
	// original receipt; later assignments must not cause endless revalidation.
	if found && current.State == StateVerified && current.TestedHead != "" {
		target, resolveErr := revParse(ctx, gitRoot, current.TargetBranch)
		if resolveErr != nil {
			return Result{Checkpoint: current}, resolveErr
		}
		if target != current.TestedHead {
			reverifyResult = true
		}
	}

	if !req.Inspection.Ready && canonical != nil && found && current.MergeCommit != "" &&
		(canonicalBindingChanged || canonicalGitFactsChanged) && len(req.Inspection.Blockers) == 1 && req.Inspection.Blockers[0] == ErrMissingCommits.Error() {
		if _, err := RecoveryBaseForCheckpoint(ctx, gitRoot, current); err != nil {
			return Result{Checkpoint: current}, fmt.Errorf("canonical Result re-verification cannot reuse the recorded Main merge: %w", err)
		}
		// Inspect can report no new commits after the original source has
		// already merged. The durable non-squash receipt and freshly verified
		// original Git diff authorize only re-running Main checks for the new
		// accepted Result; they do not grant a new merge.
		req.Inspection.Ready = true
		req.Inspection.Blockers = nil
	}
	if readyMerge != "" && !req.Inspection.Ready && len(req.Inspection.Blockers) == 1 && req.Inspection.Blockers[0] == ErrMissingCommits.Error() {
		// The exact original merge explains only this readiness result.
		// Other inspection failures still stop the original chain below.
		req.Inspection.Ready = true
		req.Inspection.Blockers = nil
	}
	if !req.Inspection.Ready {
		// Post-merge ack/cleanup resume may arrive with Inspect Ready=false
		// (ErrMissingCommits after merge). Do not overwrite a verified+
		// durable checkpoint with preserved (BUG-039-38).
		resumeState := checkpointResumeState(current)
		preservedAfterMerge := found && current.State == StatePreserved &&
			current.MergeCommit != "" && !lessThan(resumeState, StateMerged)
		if found && (req.Acknowledge || req.Cleanup) &&
			(current.State == StateVerified ||
				current.State == StateAcknowledged ||
				current.State == StateCleanupPending ||
				current.State == StateComplete ||
				preservedAfterMerge) {
			// Fall through and resume from the durable record.
		} else {
			reason := "inspect rejected integration"
			if canonical != nil && canonical.Record.Revision > 1 && containsString(req.Inspection.Blockers, ErrMissingCommits.Error()) {
				req.Inspection.Blockers = appendUniqueString(req.Inspection.Blockers, "canonical revision source is already integrated; predecessor Record and durable Main facts are not safely reusable yet")
				reason = "canonical revision merge reuse is pending predecessor Main-facts validation"
			}
			return preserveFromInspection(cfg, req, canonical, reason)
		}
	}

	if !found || current.State != StateComplete || reverifyResult {
		if err := checkoutBranch(ctx, gitRoot, req.Inspection.TargetBranch); err != nil {
			if found {
				return preserveAfterCAS(ctx, store, checkpointPath, current, current, err, ErrCheckFailed)
			}
			return Result{}, err
		}
	}

	idem := IdempotencyKey(
		idempAssignment(req),
		req.Inspection.SourceHead,
		req.Inspection.TargetBranch,
		req.Inspection.BaselineGeneration,
	)

	// CT-039-17: a previous call may have committed the merge but been
	// interrupted before ack. We detect that by looking at the durable
	// state and skip the merge when it has already happened.
	if found && (current.MergeCommit != "" || readyMerge != "") && current.SourceHead != req.Inspection.SourceHead && req.Inspection.SourceHead != "" {
		return Result{}, fmt.Errorf("source changed after recorded merge; preserve worktree and reconcile new commits")
	}
	if found {
		if current.AssignmentID != idempAssignment(req) || current.TargetBranch != req.Inspection.TargetBranch || current.SourceBranch != req.Inspection.SourceBranch || current.WorktreePath != req.Inspection.WorktreePath || current.BaselineGeneration != req.Inspection.BaselineGeneration {
			return Result{}, fmt.Errorf("integration checkpoint identity mismatch")
		}
		switch current.State {
		case StateComplete:
			if reverifyResult {
				break
			}
			return Result{Checkpoint: current, Reused: true}, nil
		case StateBlocked, StatePreserved:
			if checkpointResumeState(current) == StateCleanupPending || checkpointResumeState(current) == StateAcknowledged || checkpointResumeState(current) == StateVerified {
				// A fresh successful inspection is the recovery signal. Resume from
				// the last successful stage recorded before preservation. In
				// particular, cleanup failures resume at cleanup_pending and do not
				// re-run verification checks.
				resumed := current
				resumed.State = checkpointResumeState(current)
				resumed.ResumeState = ""
				resumed.FailureReason = ""
				resumed.LastErrorCode = ""
				written, writeErr := store.CompareAndSwap(checkpointPath, current, resumed)
				if writeErr != nil {
					return Result{}, writeErr
				}
				current = written
			} else {
				if req.RetryPreserved && req.Inspection.Ready {
					break
				}
				// A previous failure stops the chain. Re-surfacing the
				// blocker is the right behaviour — we don't try to
				// resurrect a failed integration without an explicit
				// human/builder repair.
				return Result{Checkpoint: current, Reused: true}, nil
			}
		}
	}

	// Ack/cleanup must retain the original inspected coordinates. A post-merge
	// inspection has a different merge base (often the source head itself).
	if found && (current.State == StateVerified || current.State == StateAcknowledged || current.State == StateCleanupPending) {
		if current.AssignmentID != idempAssignment(req) || current.SourceBranch != req.Inspection.SourceBranch || current.TargetBranch != req.Inspection.TargetBranch || current.WorktreePath != req.Inspection.WorktreePath || current.BaselineGeneration != req.Inspection.BaselineGeneration {
			return Result{}, fmt.Errorf("cleanup checkpoint identity mismatch")
		}
		source, err := retainedIntegrationSource(ctx, gitRoot, InspectionFromCheckpoint(current))
		if err != nil || source != current.SourceHead {
			return deferCleanup(store, checkpointPath, current, current, fmt.Errorf("cleanup source changed since verification"))
		}
		base, err := mergeBase(ctx, gitRoot, current.MergeCommit, current.TargetBranch)
		if err != nil || base != current.MergeCommit {
			return Result{}, fmt.Errorf("cleanup target no longer contains verified merge")
		}
		req.Inspection.SourceHead = current.SourceHead
		req.Inspection.TargetHead = current.TargetHead
		req.Inspection.MergeBase = current.MergeBase
		idem = current.IdempotencyKey
	}

	if found && current.State == StateReady {
		if readyMerge != "" {
			current.MergeCommit = readyMerge
			prior := current
			prior.MergeCommit = ""
			current.State = StateMerged
			written, err := store.CompareAndSwap(checkpointPath, prior, current)
			if err != nil {
				return Result{}, err
			}
			current = written
			req.Inspection = InspectionFromCheckpoint(current)
		}
	}

	// Build the next-state checkpoint from the inspection + existing
	// durable record. CAS gates the transition.
	next := current
	if found && req.RetryPreserved && (current.State == StateBlocked || current.State == StatePreserved) {
		if current.AssignmentID != idempAssignment(req) || current.SourceBranch != req.Inspection.SourceBranch || current.TargetBranch != req.Inspection.TargetBranch || current.BaselineGeneration != req.Inspection.BaselineGeneration || current.WorktreePath != req.Inspection.WorktreePath {
			return Result{}, fmt.Errorf("retry checkpoint identity mismatch")
		}
		// Start a new checked attempt using CAS against the preserved record.
		// Prior failure remains in the runtime journal; never grant verified.
		next.State = StatePending
		next.FailureReason = ""
		next.LastErrorCode = ""
		next.MergeCommit = ""
		if current.MergeCommit != "" && current.SourceHead == req.Inspection.SourceHead {
			if _, err := RecoveryBaseForCheckpoint(ctx, gitRoot, current); err != nil {
				return Result{}, err
			}
			next.State = StateMerged
			next.MergeCommit = current.MergeCommit
		}
		next.VerifiedAt = ""
		next.TestedHead = ""
	} else if reverifyResult {
		next.State = StateMerged
		next.VerifiedAt = ""
	}
	if next.State == "" {
		next.State = StatePending
	}
	if next.AssignmentID == "" {
		next.AssignmentID = idempAssignment(req)
	}
	if next.TaskID == "" {
		next.TaskID = req.Inspection.TaskID
	}
	if next.WorktreePath == "" {
		next.WorktreePath = req.Inspection.WorktreePath
	}
	next.SourceBranch = req.Inspection.SourceBranch
	next.SourceHead = req.Inspection.SourceHead
	next.TargetBranch = req.Inspection.TargetBranch
	next.TargetHead = req.Inspection.TargetHead
	next.MergeBase = req.Inspection.MergeBase
	if canonical != nil {
		mainFacts := assignmentresult.MainFacts{
			Binding:             canonical.Binding,
			MainRoot:            gitRoot,
			SourceHead:          req.Inspection.SourceHead,
			MergeBase:           req.Inspection.MergeBase,
			TargetBranch:        req.Inspection.TargetBranch,
			MergeCommit:         next.MergeCommit,
			ExecutionGeneration: canonical.ExecutionGeneration,
			ChangedPaths:        append([]string{}, req.Inspection.ChangedPaths...),
			ChangedPathsSHA256:  req.Inspection.ChangedPathsSHA256,
		}
		if !lessThan(next.State, StateVerified) && !reverifyResult && current.CanonicalMainFacts != nil {
			mainFacts = *current.CanonicalMainFacts
		}
		if lessThan(next.State, StateVerified) || reverifyResult || canonicalBindingChanged {
			mainFacts.TestedHead = ""
			mainFacts.Checks = nil
			next.CheckReceipts = nil
			next.ToolCheckReceipts = nil
			next.TestedHead = ""
			next.VerifiedAt = ""
		}
		next.CanonicalMainFacts = &mainFacts
		// The immutable ResultBinding is the Record authority once Main facts
		// exist. Clear the early preserved-checkpoint slot label so checkpoint
		// history does not carry a second copy of the same identity.
		next.AssignmentRecord = nil
		next.AssignmentRecordSHA256 = ""
		next.ChangedPaths = nil
		next.ChangedPathsSHA256 = ""
	} else {
		next.CanonicalMainFacts = nil
	}
	if canonical == nil && req.Inspection.ChangedPathsSHA256 != "" {
		next.ChangedPaths = append([]string{}, req.Inspection.ChangedPaths...)
		next.ChangedPathsSHA256 = req.Inspection.ChangedPathsSHA256
	}
	next.BaselineGeneration = req.Inspection.BaselineGeneration
	next.IdempotencyKey = idem

	// Step 1: persist ready.
	if lessThan(next.State, StateReady) {
		next.State = StateReady
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist ready: %w", err)
		}
		current = written
	}

	// Step 2: perform the merge (unless durable record shows it already
	// happened). Changed deliveries use a plain `--no-ff` merge; never squash.
	// No-change deliveries retain the current target without a merge operation.
	// REQ-039 §13.6 forbids automatic merge when the integration tree
	// has uncommitted changes — we check the integration tree here too
	// so a dirty target branch is preserved instead of merged into.
	if lessThan(next.State, StateMerged) {
		clean, err := mainWorktreeClean(ctx, gitRoot)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeCheckFailure(gitRoot, err), ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeDirtyFailure(gitRoot), ErrDirtyWorktree)
		}
		branch, err := defaultRunner.Run(ctx, gitRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil || strings.TrimSpace(branch) != strings.TrimPrefix(req.Inspection.TargetBranch, "refs/heads/") {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("main workspace must remain on target branch %s; found %s", req.Inspection.TargetBranch, strings.TrimSpace(branch)), ErrMergeConflict)
		}
		target, err := revParse(ctx, gitRoot, "HEAD")
		if err != nil || target != req.Inspection.TargetHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("target changed after inspection; inspect again"), ErrMergeConflict)
		}
		source, err := retainedIntegrationSource(ctx, gitRoot, req.Inspection)
		if err != nil || source != req.Inspection.SourceHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("source changed after inspection"), ErrMergeConflict)
		}
		if err := ctx.Err(); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
		}

		sourceHead, sourceErr := retainedIntegrationSource(ctx, gitRoot, req.Inspection)
		if sourceErr != nil || sourceHead != req.Inspection.SourceHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("source changed since inspection; re-inspect"), ErrMergeConflict)
		}
		actualPaths, actualPathsSHA, err := canonicalGitChangedPaths(ctx, gitRoot, req.Inspection.MergeBase, sourceHead)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
		}
		if req.Inspection.ChangedPathsSHA256 != "" &&
			(actualPathsSHA != req.Inspection.ChangedPathsSHA256 || !samePathSet(actualPaths, req.Inspection.ChangedPaths)) {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("source changed-path set differs from inspected Git facts; re-inspect"), ErrMergeConflict)
		}
		if len(req.Inspection.EffectiveWritePaths) > 0 {
			if outside := outsideWriteScope(actualPaths, req.Inspection.EffectiveWritePaths); len(outside) > 0 {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: %s", ErrScopeViolation, strings.Join(outside, ", ")), ErrScopeViolation)
			}
		}
		// Recheck the frozen paths under the integration lock immediately before
		// merging. Rename detection is disabled so removals cannot hide a lock.
		if len(req.Inspection.LockedPaths) > 0 {
			if hits := intersectLocked(actualPaths, req.Inspection.LockedPaths); len(hits) > 0 {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: %s", ErrLockedArtifact, strings.Join(hits, ", ")), ErrLockedArtifact)
			}
		}
		if canonical != nil {
			latest, resolveErr := resolveCanonicalResultByBinding(cfg.Root, runtimeID, canonical.Binding)
			if resolveErr != nil || latest == nil {
				if resolveErr == nil {
					resolveErr = fmt.Errorf("canonical Result no longer resolves")
				}
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, resolveErr, ErrMissingCompletion)
			}
		}
		// A professional no-change delivery still requires Main checks, but
		// has nothing to merge. Retain the actual Main HEAD in the existing
		// checkpoint coordinates instead of manufacturing a source commit.
		mergeCommit := target
		if canonical != nil || sourceHead != req.Inspection.MergeBase || len(actualPaths) != 0 {
			mergeCommit, err = performMerge(ctx, gitRoot, req.Inspection.SourceHead)
			if err != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
			}
		}
		next.MergeCommit = mergeCommit
		if next.CanonicalMainFacts != nil {
			facts := *next.CanonicalMainFacts
			facts.MergeCommit = mergeCommit
			next.CanonicalMainFacts = &facts
		}
		next.State = StateMerged
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist merged: %w", err)
		}
		current = written
	}

	// Step 3: integration checks → verified.
	if lessThan(next.State, StateVerified) {
		// The report may be edited while the merge is in progress. Re-read it
		// immediately before the verification checks so those checks are tied to
		// the same bytes that Inspect observed.
		if completionBinding.Bound {
			latest, readErr := readCompletionReportBinding(cfg.Root, completionBinding.Path)
			if readErr != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, readErr, ErrMissingCompletion)
			}
			if !reportBindingMatches(completionBinding, latest) {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: before verification", ErrCompletionReportChanged), ErrCompletionReportChanged)
			}
		}
		if canonical != nil {
			latest, resolveErr := resolveCanonicalResultByBinding(cfg.Root, runtimeID, canonical.Binding)
			if resolveErr != nil || latest == nil {
				if resolveErr == nil {
					resolveErr = fmt.Errorf("canonical Result no longer resolves")
				}
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, resolveErr, ErrMissingCompletion)
			}
		}
		// Refuse to advance to verified if the target tree is now
		// dirty (e.g. an external process wrote into the integration
		// branch). The worktree-preserved recovery applies.
		clean, err := mainWorktreeClean(ctx, gitRoot)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeCheckFailure(gitRoot, err), ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeDirtyFailure(gitRoot), ErrDirtyWorktree)
		}
		checkedHead, err := revParse(ctx, gitRoot, "HEAD")
		if err != nil {
			return Result{}, err
		}
		if err := verifyCheckTarget(ctx, gitRoot, next, checkedHead); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
		}
		// Run checks. Default behaviour: if no checks are configured we
		// still advance (per BUG §4.1 "default to none if not
		// specified"). Any failing check stops the chain.
		commands, _ := splitRequiredChecks(cfg.RequiredChecks)
		if len(commands) > 0 {
			var canonicalChecks []struct {
				Index   int
				Command string
			}
			if canonical != nil {
				canonicalChecks = mainCheckIndexes(canonical.Record)
				if len(canonicalChecks) != len(commands) {
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("Main RequiredChecks sequence differs from the canonical Record"), ErrCheckFailed)
				}
			}
			for commandIndex, command := range commands {
				if cfg.CheckRunner == nil {
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrCheckRunnerMissing, ErrCheckFailed)
				}
				priorReceipts := len(next.CheckReceipts)
				checkCtx := context.WithValue(ctx, checkEvidenceKey{}, checkEvidence{Directory: filepath.Join(filepath.Dir(checkpointPath), "checks"), Receipts: &next.CheckReceipts, ToolReceipts: &next.ToolCheckReceipts})
				if err := cfg.CheckRunner(checkCtx, gitRoot, command); err != nil {
					next.FailureReason = fmt.Sprintf("integration check %s failed: %v", command, err)
					next.LastErrorCode = "LOOP_INTEGRATION_CHECK_FAILED"
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
				}
				if canonical != nil {
					if len(next.CheckReceipts) != priorReceipts+1 {
						return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("Main runner did not persist exactly one receipt for %s", command), ErrCheckFailed)
					}
					required := canonicalChecks[commandIndex]
					if required.Command != command {
						return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("Main runner command order differs from the canonical RequiredChecks sequence"), ErrCheckFailed)
					}
					profileDigest, profileErr := canonicalProfileDigest(canonical.Record, required.Index, command)
					if profileErr != nil {
						return preserveAfterCAS(ctx, store, checkpointPath, current, next, profileErr, ErrCheckFailed)
					}
					fact, factErr := captureMainCheckFact(cfg.Root, checkpointPath, next.CheckReceipts[priorReceipts], gitRoot, checkedHead, required.Index, command, mainCheckBinding(canonical), profileDigest)
					if factErr != nil {
						return preserveAfterCAS(ctx, store, checkpointPath, current, next, factErr, ErrCheckFailed)
					}
					if next.CanonicalMainFacts == nil {
						return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("canonical Main checkpoint facts disappeared during checks"), ErrCheckFailed)
					}
					facts := *next.CanonicalMainFacts
					facts.Checks = append(append([]assignmentresult.MainCheckFact(nil), facts.Checks...), fact)
					next.CanonicalMainFacts = &facts
				}
			}
		}
		finalHead, headErr := revParse(ctx, gitRoot, "HEAD")
		cleanAfter, cleanErr := mainWorktreeClean(ctx, gitRoot)
		if cleanErr != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeCheckFailure(gitRoot, cleanErr), ErrCheckFailed)
		}
		if !cleanAfter {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeDirtyFailure(gitRoot), ErrCheckFailed)
		}
		if headErr != nil || finalHead != checkedHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("integration target changed during checks"), ErrCheckFailed)
		}
		if err := verifyCheckTarget(ctx, gitRoot, next, finalHead); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
		}
		next.TestedHead = finalHead
		if canonical != nil {
			if next.CanonicalMainFacts == nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("canonical Main checkpoint facts disappeared after checks"), ErrCheckFailed)
			}
			facts := *next.CanonicalMainFacts
			facts.TestedHead = finalHead
			facts.MergeCommit = next.MergeCommit
			next.CanonicalMainFacts = &facts
		}
		// A check command can itself rewrite the completion report. Re-read
		// after checks and refuse to certify a different Result.
		if completionBinding.Bound {
			latest, readErr := readCompletionReportBinding(cfg.Root, completionBinding.Path)
			if readErr != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, readErr, ErrMissingCompletion)
			}
			if !reportBindingMatches(completionBinding, latest) {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: after verification checks", ErrCompletionReportChanged), ErrCompletionReportChanged)
			}
			completionBinding = latest
		}
		if canonical != nil {
			latest, resolveErr := resolveCanonicalResultByBinding(cfg.Root, runtimeID, canonical.Binding)
			if resolveErr != nil || latest == nil {
				if resolveErr == nil {
					resolveErr = fmt.Errorf("canonical Result changed while Main checks ran")
				}
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, resolveErr, ErrCompletionReportChanged)
			}
			if next.CanonicalMainFacts == nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("canonical Main checkpoint facts disappeared before verification"), ErrCheckFailed)
			}
			facts := *next.CanonicalMainFacts
			facts.TestedHead = finalHead
			facts.MergeCommit = next.MergeCommit
			next.CanonicalMainFacts = &facts
			next.TestedHead = finalHead
			if err := validateCanonicalMainFacts(cfg.Root, checkpointPath, gitRoot, next, canonical); err != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
			}
		}
		clean, err = mainWorktreeClean(ctx, gitRoot)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeCheckFailure(gitRoot, err), ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, mainWorktreeDirtyFailure(gitRoot), ErrDirtyWorktree)
		}
		next.State = StateVerified
		if completionBinding.Bound {
			next.CompletionReportPath = completionBinding.Path
			next.CompletionReportSHA256 = completionBinding.SHA256
		}
		verifiedAt := time.Now().UTC()
		if store.Clock != nil {
			verifiedAt = store.Clock()
		}
		next.VerifiedAt = verifiedAt.UTC().Format(time.RFC3339Nano)
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist verified: %w", err)
		}
		current = written
	}

	// Step 4: acknowledgement. The caller flips Acknowledge after writing
	// the completion_ack (BUG-06). When the caller has not yet
	// acknowledged we stop here and return the verified record — the
	// next Integrate call will resume from this point.
	if lessThan(next.State, StateAcknowledged) {
		if !req.Acknowledge {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next.State = StateAcknowledged
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist acknowledged: %w", err)
		}
		current = written
	}

	// Step 5: cleanup. Cleanup_pending marks the durable record that
	// the acknowledgement has been written and we are about to remove
	// the worktree; the actual removal (and the transition to complete)
	// only happens when the caller has set req.Cleanup. Cleanup runs
	// only when the tree is clean (BE-039 §8 / REQ-039 §13.6).
	if lessThan(next.State, StateCleanupPending) {
		if !req.Cleanup {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next.State = StateCleanupPending
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist cleanup_pending: %w", err)
		}
		current = written
	}

	if lessThan(next.State, StateComplete) {
		if !req.Cleanup {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		// The original cleanup helper owns the single coordinate/cleanliness
		// check and recovery from an interrupted installation restoration.
		if cfg.WorkerCleanupError != nil {
			return deferCleanup(store, checkpointPath, current, next, cfg.WorkerCleanupError)
		}
		area := WorkerPrivateAreaSpec{Path: cfg.WorkerPrivateArea, BaseCommit: cfg.WorkerPrivateBaseCommit, WritePaths: cfg.WorkerPrivateWritePaths, BaselinePaths: cfg.WorkerPrivateBaselinePaths}
		if err := cleanupWorktree(ctx, gitRoot, req.Inspection.WorktreePath, current, area, cfg.WorkerLogEvidence, cfg.WorkerPrivateEvidence, cfg.WorkerIgnoredEvidence); err != nil {
			return deferCleanup(store, checkpointPath, current, next, err)
		}
		next.State = StateComplete
		next.FailureReason = ""
		next.LastErrorCode = ""
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{Checkpoint: current}, fmt.Errorf("persist complete (cleanup can safely resume): %w", err)
		}
		current = written
	}

	return Result{Checkpoint: current, Reused: found}, nil
}

func mainWorktreeDirtyFailure(root string) error {
	return fmt.Errorf("Main integration checkout %q is dirty; inspect with git -C %q status --short: %w", root, root, ErrDirtyWorktree)
}

func mainWorktreeCheckFailure(root string, cause error) error {
	return fmt.Errorf("Main integration checkout %q clean check failed; inspect with git -C %q status --short: %w", root, root, cause)
}

// A stable HEAD alone does not identify the tree we intended to test. Recovery
// can run after another process switched branches, including to a branch with
// the same commit. Do not checkout here: preserve the user's current location.
func verifyCheckTarget(ctx context.Context, root string, cp Checkpoint, head string) error {
	branch, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(branch) != strings.TrimPrefix(cp.TargetBranch, "refs/heads/") {
		return fmt.Errorf("integration check requires target branch %s; current branch is %s (error: %v)", cp.TargetBranch, strings.TrimSpace(branch), err)
	}
	target, err := revParse(ctx, root, cp.TargetBranch)
	if err != nil || target != head {
		return fmt.Errorf("integration check HEAD does not match target branch %s", cp.TargetBranch)
	}
	base, err := mergeBase(ctx, root, cp.MergeCommit, head)
	if cp.MergeCommit == "" || err != nil || base != cp.MergeCommit {
		return fmt.Errorf("integration check target no longer contains recorded merge %s", cp.MergeCommit)
	}
	return nil
}

// preserveAfterCAS writes a `preserved` checkpoint describing the failure
// mode and returns a Result with the failure reason populated. The
// worktree is intentionally retained; this satisfies BE-039 §8's
// "failure_before_cleanup_preserves_worktree_and_branch" rule.
func preserveAfterCAS(ctx context.Context, store *CheckpointStore, path string, current Checkpoint, next Checkpoint, cause error, sentinel error) (Result, error) {
	preserved := next
	preserved.ResumeState = successfulResumeState(next.State)
	preserved.State = StatePreserved
	if cause != nil {
		if preserved.FailureReason == "" {
			preserved.FailureReason = cause.Error()
		}
		if preserved.LastErrorCode == "" {
			preserved.LastErrorCode = stableErrorCode(sentinel)
		}
	}
	written, err := store.CompareAndSwap(path, current, preserved)
	if err != nil {
		return Result{Checkpoint: preserved}, fmt.Errorf("persist preserved: %w", err)
	}
	if cause != nil {
		return Result{Checkpoint: written}, fmt.Errorf("%v; Builder checkout %q preserved: %w", cause, written.WorktreePath, sentinel)
	}
	return Result{Checkpoint: written}, sentinel
}

// preserveInvalidMergeReceipt handles a verified-or-later checkpoint whose
// recorded merge is no longer reachable from the currently bound target. The
// old receipt remains in the durable record for audit, but ResumeState is
// explicitly reset to pending so a later reconciliation may create a new
// merge. Keeping this as preserved prevents a stale receipt from certifying or
// deleting a worker, while descendants of the recorded merge remain valid
// because mergeCommitReachable uses ancestry rather than HEAD equality.
func preserveInvalidMergeReceipt(store *CheckpointStore, path string, current Checkpoint, cause error) (Result, error) {
	preserved := current
	preserved.State = StatePreserved
	preserved.ResumeState = StatePending
	preserved.FailureReason = cause.Error()
	preserved.LastErrorCode = stableErrorCode(ErrMergeConflict)
	written, err := store.ForceWrite(path, preserved)
	if err != nil {
		return Result{Checkpoint: preserved}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, fmt.Errorf("%w: %v", ErrMergeConflict, cause)
}

// successfulResumeState returns the last durable stage boundary represented by
// next before it is changed to StatePreserved. The state machine only calls
// preserveAfterCAS after the stage in next has either been persisted or is the
// stage currently being attempted, so this is the exact retry boundary.
func successfulResumeState(state string) string {
	switch state {
	case StatePending, StateReady, StateMerged, StateVerified, StateAcknowledged, StateCleanupPending, StateComplete:
		return state
	default:
		return ""
	}
}

// checkpointResumeState supports checkpoints written before ResumeState was
// introduced. A recorded merge is the safest legacy boundary; otherwise a
// retry starts from pending and rebuilds the ready/merge stages.
func checkpointResumeState(cp Checkpoint) string {
	if state := successfulResumeState(cp.ResumeState); state != "" {
		return state
	}
	if cp.MergeCommit != "" {
		return StateMerged
	}
	return StatePending
}

// preserveFromInspection persists a preserved checkpoint when the
// caller hands Integrate a non-ready Inspection. Existing durable progress
// is retained; a new record is created through CAS.
func preserveFromInspection(cfg IntegrateConfig, req IntegrateRequest, canonical *canonicalResultFacts, reason string) (Result, error) {
	store := cfg.CheckpointStore
	if store == nil {
		store = DefaultCheckpointStore()
	}
	runtimeID := cfg.RuntimeID
	if runtimeID == "" {
		runtimeID = "loop-REQ-039"
	}
	checkpointPath := cfg.CheckpointDir
	if canonical != nil {
		expected := store.PathForRecord(cfg.Root, canonical.Record.Reference())
		if expected == "" || checkpointPath != "" && filepath.Clean(checkpointPath) != filepath.Clean(expected) {
			return Result{}, fmt.Errorf("preserved canonical checkpoint must use the current Assignment revision path")
		}
		checkpointPath = expected
	} else if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, req.Inspection.BaselineGeneration, idempAssignment(req))
	}

	idem := IdempotencyKey(
		idempAssignment(req),
		req.Inspection.SourceHead,
		req.Inspection.TargetBranch,
		req.Inspection.BaselineGeneration,
	)

	cp := Checkpoint{
		AssignmentID:       idempAssignment(req),
		TaskID:             req.Inspection.TaskID,
		WorktreePath:       req.Inspection.WorktreePath,
		SourceBranch:       req.Inspection.SourceBranch,
		SourceHead:         req.Inspection.SourceHead,
		TargetBranch:       req.Inspection.TargetBranch,
		TargetHead:         req.Inspection.TargetHead,
		MergeBase:          req.Inspection.MergeBase,
		BaselineGeneration: req.Inspection.BaselineGeneration,
		State:              StatePreserved,
		IdempotencyKey:     idem,
		Blockers:           append([]string(nil), req.Inspection.Blockers...),
		FailureReason:      reason,
		LastErrorCode:      "LOOP_INTEGRATION_CONFLICT",
		LockedDiff:         append([]string(nil), req.Inspection.LockedDiff...),
	}
	if canonical != nil {
		ref := canonical.Record.Reference()
		sha, err := canonical.Record.Digest()
		if err != nil {
			return Result{}, err
		}
		cp.AssignmentRecord = &ref
		cp.AssignmentRecordSHA256 = sha
	}
	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, err
	}
	if found {
		if canonical != nil {
			if err := ValidateCheckpointRecordBinding(current, canonical.Record); err != nil {
				return Result{Checkpoint: current}, err
			}
		}
		return Result{Checkpoint: current, Reused: true}, nil
	}
	written, err := store.CompareAndSwap(checkpointPath, current, cp)
	if err != nil {
		return Result{Checkpoint: cp}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, nil
}

// idempAssignment extracts the assignment id used as the durable
// checkpoint identity. Canonical order (BUG-039-38):
//
//  1. Inspection.AssignmentID (populated by Inspect from the assignment)
//  2. "assignment:<id>" blocker sentinel (legacy / test harness)
//  3. WorktreePath fallback (last resort; must not be the production key)
func idempAssignment(req IntegrateRequest) string {
	if id := strings.TrimSpace(req.Inspection.AssignmentID); id != "" {
		return id
	}
	if id := deriveAssignmentFromBlockers(req.Inspection.Blockers); id != "" {
		return id
	}
	return req.Inspection.WorktreePath
}

func deriveAssignmentFromBlockers(blockers []string) string {
	for _, b := range blockers {
		if strings.HasPrefix(b, "assignment:") {
			return strings.TrimPrefix(b, "assignment:")
		}
	}
	return ""
}

// lessThan is the canonical state ordering for the integration state
// machine. We use it instead of switch+rank to keep the precedence in one
// place and to make adding intermediate states cheap.
func lessThan(a, b string) bool {
	return stateRank(a) < stateRank(b)
}

func stateRank(s string) int {
	switch s {
	case StatePending:
		return 0
	case StateReady:
		return 1
	case StateMerged:
		return 2
	case StateVerified:
		return 3
	case StateAcknowledged:
		return 4
	case StateCleanupPending:
		return 5
	case StateComplete:
		return 6
	}
	return -1
}

// stableErrorCode maps internal sentinels to the LOOP_* codes the caller
// surfaces in the controller error envelope. The codes here are the ones
// defined in SYNC-039 §11; the Integrator does not emit caller-safe
// message text, that is the controller's job.
func stableErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrCheckFailed):
		return "LOOP_INTEGRATION_CHECK_FAILED"
	case errors.Is(err, ErrMergeConflict):
		return "LOOP_INTEGRATION_CONFLICT"
	case errors.Is(err, ErrDirtyWorktree):
		return "LOOP_INTEGRATION_PARTIAL"
	case errors.Is(err, ErrLockedArtifact):
		return "LOOP_LOCKED_ARTIFACT"
	case errors.Is(err, ErrScopeViolation):
		return "LOOP_SCOPE_VIOLATION"
	case errors.Is(err, ErrSquashForbidden):
		return "LOOP_SQUASH_MERGE"
	default:
		return "LOOP_INTEGRATION_PARTIAL"
	}
}

// LessThan is exposed as a public helper so external callers (e.g.
// tests in sibling packages) can reason about state precedence without
// duplicating the rank table.
func LessThan(a, b string) bool { return lessThan(a, b) }

func integrationDurationStatus(result Result, err error) string {
	if err != nil {
		switch {
		case errors.Is(err, ErrDirtyWorktree), errors.Is(err, ErrMergeConflict), errors.Is(err, ErrSquashForbidden), errors.Is(err, ErrScopeViolation):
			return "preserved"
		default:
			return "error"
		}
	}
	switch result.Checkpoint.State {
	case StatePreserved:
		return "preserved"
	case StateBlocked:
		return "blocked"
	case StateComplete, StateVerified, StateMerged, StateAcknowledged, StateCleanupPending, StateReady:
		return "success"
	default:
		return "success"
	}
}

// The platform may delete a completed no-change source branch with its native
// checkout. Preserve its original coordinates; the recorded empty range can
// still resolve the same immutable Git object. A live/moved branch, a retained
// checkout, canonical work or a nonempty range keeps its original checks.
func retainedIntegrationSource(ctx context.Context, root string, in Inspection) (string, error) {
	source, err := revParse(ctx, root, in.SourceBranch)
	if err == nil {
		return source, nil
	}
	if in.CanonicalResult != nil || in.SourceHead == "" || in.SourceHead != in.MergeBase || len(in.ChangedPaths) != 0 {
		return "", err
	}
	if _, pathErr := os.Lstat(in.WorktreePath); !os.IsNotExist(pathErr) {
		return "", err
	}
	paths, digest, rangeErr := canonicalGitChangedPaths(ctx, root, in.MergeBase, in.SourceHead)
	if rangeErr != nil || len(paths) != 0 || digest != in.ChangedPathsSHA256 {
		return "", err
	}
	return revParse(ctx, root, in.SourceHead)
}
