package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/diagnosticstorage"
	"github.com/entroforge/go-system-builder/internal/pathscope"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/targetedauthoring"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ---------------------------------------------------------------------------
// E2E cold-start verification workspace (L3-S7 §1.4.1, §3.2): the workspace
// is the only write surface reviewers may use during S7; its digest is
// pinned at registration and re-verified at seal/clean so a spec that changed
// after a result was consumed invalidates the round honestly.
// ---------------------------------------------------------------------------

// WorkspaceDigest computes sha256 over the sorted "relpath:filesha256" lines
// of every file under dir. An empty or missing workspace digests to the
// empty-string hash (the registered cold-start baseline).
func WorkspaceDigest(root, workspaceRel string) (string, error) {
	return WorkspaceDigestContext(context.Background(), root, workspaceRel)
}

// WorkspaceDigestContext always reads the current workspace; it caches no disk verdict.
func WorkspaceDigestContext(ctx context.Context, root, workspaceRel string) (digest string, readErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if ctx.Err() != nil {
			digest, readErr = "", ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if workspaceRel == "" {
		return "", nil
	}
	abs, err := repositoryContainedPath(root, workspaceRel)
	if err != nil {
		return "", fmt.Errorf("workspace %s is outside repository: %w", workspaceRel, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return sha256Of([]byte("")), nil
		}
		return "", fmt.Errorf("read workspace %s: %w", workspaceRel, err)
	}
	_ = entries
	var lines []string
	err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace contains symlink %s; symlinked evidence surfaces are not digestible", path)
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+":"+sha256Of(data))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	return sha256Of([]byte(strings.Join(lines, "\n"))), nil
}

// prepareVerificationWorkspace validates and creates the E2E cold-start
// workspace: it must live under e2e-workspace/ inside the repository so the
// reviewer write-scope rule and the digest recomputation can see it.
func prepareVerificationWorkspace(root string, plan *Plan, contexts ...context.Context) (string, error) {
	ctx := reviewRequestContext(contexts)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if plan.VerificationArtifactWorkspace == nil || strings.TrimSpace(*plan.VerificationArtifactWorkspace) == "" {
		return "", nil
	}
	workspace := *plan.VerificationArtifactWorkspace
	if filepath.IsAbs(workspace) || strings.HasPrefix(workspace, "..") {
		return "", fmt.Errorf("verification_artifact_workspace must be repository-relative (got %q)", workspace)
	}
	if !strings.HasPrefix(workspace, "e2e-workspace/") {
		return "", fmt.Errorf("verification_artifact_workspace must live under e2e-workspace/ (got %q); the reviewer write-scope rule only knows that surface", workspace)
	}
	abs, err := repositoryContainedPath(root, workspace)
	if err != nil {
		return "", fmt.Errorf("verification_artifact_workspace must stay inside repository: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create verification workspace: %w", err)
	}
	digest, err := WorkspaceDigestContext(ctx, root, workspace)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// repositoryContainedPath resolves a repository-relative path and rejects
// lexical traversal (and existing symlink escapes). Reviewer write surfaces
// are security boundaries; a prefix check such as "e2e-workspace/" is not a
// containment proof for paths containing "..".
func repositoryContainedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be repository-relative")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	relToRoot, err := filepath.Rel(rootAbs, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("path %q escapes repository", rel)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", rootErr)
	}
	// EvalSymlinks(candidate) fails when the leaf is new. Walk upward until an
	// existing ancestor is found so a symlinked parent cannot smuggle a future
	// artifact outside the repository.
	for current := abs; ; current = filepath.Dir(current) {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
			if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
				return "", fmt.Errorf("path %q follows a symlink outside repository", rel)
			}
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return abs, nil
}

// VerifyFrozenSubjects reuses the original read-only S7 subject verifier.
// The caller must first establish the Plan's pinned identity and current scope.
func VerifyFrozenSubjects(root string, plan *Plan, state map[string]any) error {
	return verifyFrozenSubjects(root, plan, state)
}

// verifyFrozenSubjects proves that the registered ReviewPlan still describes
// the bytes on disk. SubjectDigest only fingerprints the plan declarations;
// it must not be mistaken for a disk-baseline check. A changed or missing
// subject makes the round stale before any Reviewer Result is consumed.
//
// RC-03 dual-source (S7-2): the check is the union of the declared set and the
// git diff baseline. A file modified or added outside frozen_subjects is still
// product drift — the plan is stale even though the hand-written list hashes
// clean. Allowed write surfaces (.claude/, docs/reports/, e2e-workspace/) are
// excluded from the drift scan.
func verifyFrozenSubjects(root string, plan *Plan, state map[string]any, contexts ...context.Context) error {
	if err := reviewRequestContext(contexts).Err(); err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("frozen subject verification requires a plan")
	}
	if buildsurface.HasCanonical(state) {
		if err := verifyProductBaselineDeclarationContext(reviewRequestContext(contexts), root, state, plan); err != nil {
			return err
		}
		return verifyCanonicalFrozenWorktree(root, plan, state, contexts...)
	}
	if plan.ProductBaseline != nil {
		return fmt.Errorf("ReviewPlan declares a canonical product baseline but Runtime has no canonical S6 Assignment registry")
	}
	for _, subject := range plan.FrozenSubjects {
		if strings.TrimSpace(subject.Path) == "" {
			return fmt.Errorf("frozen subject has an empty path")
		}
		if len(subject.SHA256) != 64 {
			return fmt.Errorf("frozen subject %s has an invalid sha256", subject.Path)
		}
		path, err := repositoryContainedPath(root, subject.Path)
		if err != nil {
			return fmt.Errorf("frozen subject %s is outside repository: %w", subject.Path, err)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			// RC-29: an S9 repair may DELETE an S6-denominator artifact; the TR-012
			// round must still freeze that path (coverage requires it) at its
			// pre-deletion digest. An absent frozen subject is accepted only when
			// the current round's change impact itself records the same path+sha256
			// as a deleted changed artifact.
			if os.IsNotExist(readErr) && deletionCorroborated(root, state, plan, subject.Path, subject.SHA256) {
				continue
			}
			return fmt.Errorf("frozen subject %s is unreadable: %w", subject.Path, readErr)
		}
		if subject.Kind == "deleted" {
			return fmt.Errorf("frozen deleted subject %s reappeared on disk; restore its absence before registration", subject.Path)
		}
		actual := sha256Of(data)
		if actual != subject.SHA256 {
			return fmt.Errorf("frozen subject %s drifted: plan pins %s but disk contains %s", subject.Path, subject.SHA256, actual)
		}
	}
	if root != "" {
		drift, err := detectUndeclaredProductDriftWithState(root, plan, state, root, contexts...)
		if errors.Is(err, errResultPacketDriftProof) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err == nil && len(drift) > 0 {
			return fmt.Errorf("frozen baseline drift: undeclared product file(s) %s outside frozen_subjects; they remain in full review coverage. Revert only actual TASK product changes; never delete unassigned process or note files to force registration", strings.Join(drift, ", "))
		}
	}
	return nil
}

// deletionCorroborated requires a registered, hash-pinned deletion fact. The
// current round entry or an explicit ChangeImpact source reference selects the
// authority; a self-declared FrozenSubject.Kind never proves deletion. The
// latest live fact for the path wins, so an old deletion cannot excuse a later
// add/modify followed by an undeclared deletion.
func deletionCorroborated(root string, state map[string]any, plan *Plan, subjectPath, subjectSHA string) bool {
	if state == nil || root == "" {
		return false
	}
	round, generation := currentReviewRound(state), baselineGeneration(state)
	selected := map[string]bool{}
	review, _ := state["review"].(map[string]any)
	entry, _ := review["round_entry"].(map[string]any)
	if entry != nil && intField(entry["round"]) == round && intField(entry["baseline_generation"]) == generation {
		if id := evidenceReferenceID(state, stringField(entry["change_impact_ref"])); id != "" {
			selected[id] = true
		}
	}
	if plan != nil && plan.ReviewRound == round && plan.BaselineGeneration == generation && plan.ChangeImpact != nil {
		for _, ref := range plan.ChangeImpact.SourceRefs {
			if id := evidenceReferenceID(state, ref); id != "" {
				selected[id] = true
			}
		}
	}
	if len(selected) == 0 {
		return false
	}
	rows := evidenceEntries(state)
	for i := len(rows) - 1; i >= 0; i-- {
		row, _ := rows[i].(map[string]any)
		if row == nil || stringField(row["kind"]) != "change_impact" || stringField(row["status"]) != "valid" || row["invalidated_by"] != nil || intField(row["baseline_generation"]) != generation {
			continue
		}
		// Do not consume a future round's record. Older records need an
		// explicit plan citation; the current entry is also a legal citation.
		if intField(row["review_round"]) > round {
			return false
		}
		id := stringField(row["id"])
		data, err := loadIndexedEvidenceArtifact(root, state, id, "change_impact")
		if err != nil {
			return false
		}
		var impact struct {
			RuntimeID          string `json:"runtime_id"`
			BaselineGeneration int    `json:"baseline_generation"`
			ChangedArtifacts   []struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
				Status string `json:"status"`
			} `json:"changed_artifacts"`
		}
		if json.Unmarshal(data, &impact) != nil || impact.RuntimeID != stringField(state["runtime_id"]) || impact.BaselineGeneration != generation {
			return false
		}
		for _, artifact := range impact.ChangedArtifacts {
			if artifact.Path == subjectPath {
				return selected[id] && artifact.SHA256 == subjectSHA && artifact.Status == "deleted"
			}
		}
	}
	return false
}

// detectUndeclaredProductDrift scans the git diff baseline for product files
// that were modified, added, or untracked without being declared in
// frozen_subjects. It is the second source of the RC-03 dual-source check.
// Non-git repositories (e.g., TempDir fixtures) degrade gracefully: no extra
// drift is reported when git is unavailable.
func detectUndeclaredProductDrift(root string, plan *Plan) ([]string, error) {
	return detectUndeclaredProductDriftWithState(root, plan, nil, root)
}

var errResultPacketDriftProof = errors.New("S7 issued control artifact source cannot be verified")

func reviewRequestContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}

func detectUndeclaredProductDriftWithState(root string, plan *Plan, state map[string]any, authorityRoot string, contexts ...context.Context) ([]string, error) {
	parent := loopruntime.WithReviewResultReadCache(reviewRequestContext(contexts))
	if err := parent.Err(); err != nil {
		return nil, err
	}
	// Read the actual worktree first. Only paths that survive the ordinary
	// classification below need an original S7 packet issuance proof.
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	output, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames").CombinedOutput()
	cancel()
	if err != nil {
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, err
	}
	controlPaths := map[string]bool{}
	// S11's consumed defect decision is an original control artifact, not a
	// product change. Recognize only its exact current record; sibling files
	// and unregistered decisions remain subject to the product drift check.
	evidenceRows, _ := state["evidence"].([]any)
	for _, raw := range evidenceRows {
		if err := parent.Err(); err != nil {
			return nil, err
		}
		row, _ := raw.(map[string]any)
		path := stringField(row["path"])
		// S10's original recorded reports remain control history when it
		// returns to S7. Unknown files beside them are still product drift.
		kind := stringField(row["kind"])
		if (kind == "acceptance" || kind == "release_audit" || kind == "change_impact") && strings.HasPrefix(path, "s10/") && workpacket.CheckPath(root, path) == nil {
			data, err := workpacket.ReadFile(root, path, 20<<20)
			if err == nil && workpacket.Digest(data) == row["sha256"] {
				controlPaths[path] = true
				var envelope struct {
					Path string `json:"audit_manifest_path"`
					SHA  string `json:"audit_manifest_sha256"`
				}
				if (kind == "acceptance" || kind == "release_audit") && json.Unmarshal(data, &envelope) == nil && filepath.Dir(envelope.Path) == filepath.Dir(path) {
					manifest, err := workpacket.ReadFile(root, envelope.Path, 20<<20)
					if err == nil && workpacket.Digest(manifest) == envelope.SHA {
						controlPaths[envelope.Path] = true
						controlPaths[strings.TrimSuffix(envelope.Path, ".json")+".md"] = true
					}
				}
			}
		}
		if row["kind"] == "human_decision" && row["status"] == "valid" && row["consumed_by"] == "TR-027" &&
			intField(row["baseline_generation"]) == baselineGeneration(state) && intField(row["review_round"]) == plan.ReviewRound &&
			strings.HasPrefix(path, ".claude/decisions/") && workpacket.CheckPath(root, path) == nil {
			controlPaths[path] = true
		}
	}
	// Existing packet records name the exact question/answer files from all
	// stages, including earlier rounds. Sibling files are not implied by them.
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		if err := parent.Err(); err != nil {
			return nil, err
		}
		row, _ := raw.(map[string]any)
		if typedResultReceiptsEnabled(state) && row["template_id"] == "s7-review-result-v1" {
			// Typed S7 rows need their original issuance proof below, even
			// when their exact answer lives on the submissions surface.
			continue
		}
		for _, key := range []string{"view_path", "answer_path"} {
			path, _ := row[key].(string)
			if strings.HasPrefix(path, ".claude/submissions/") && workpacket.CheckPath(root, path) == nil {
				controlPaths[path] = true
			}
		}
	}
	if questionnaire := PlanQuestionnairePath(root, state); questionnaire != "" {
		if rel, err := filepath.Rel(root, strings.TrimSuffix(questionnaire, ".json")+".md"); err == nil {
			controlPaths[filepath.ToSlash(rel)] = true
		}
	}
	scope := pathscope.New(root)
	frozen := make(map[string]bool, len(plan.FrozenSubjects))
	for _, subject := range plan.FrozenSubjects {
		frozen[normalizeGitStatusPath(subject.Path)] = true
	}
	var drift []string
	normalUntracked := map[string]bool{}
	exactStatusPath := map[string]bool{}
	seenStatus := map[string]bool{}
	for _, entry := range strings.Split(string(output), "\x00") {
		if entry == "" {
			continue
		}
		// NUL mode keeps spaces, tabs, quotes and newlines inside the path.
		// With --no-renames porcelain v1 emits `XY path` for each entry.
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, fmt.Errorf("invalid NUL-delimited Git status entry")
		}
		rel := normalizeGitStatusPath(entry[3:])
		if rel == "" {
			return nil, fmt.Errorf("Git status contains an unsafe product path")
		}
		if !seenStatus[rel] {
			normalUntracked[rel] = entry[:2] == "??"
			exactStatusPath[rel] = entry[3:] == rel
			seenStatus[rel] = true
		} else {
			normalUntracked[rel] = false
			exactStatusPath[rel] = false
		}
		if frozen[rel] {
			continue
		}
		if controlPaths[rel] {
			continue
		}
		// An untracked symlink or file named .claude is not the control
		// directory, even though Git reports it as one bare path.
		if rel == ".claude" {
			info, statErr := os.Lstat(filepath.Join(root, ".claude"))
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: inspect control directory: %w", errResultPacketDriftProof, statErr)
			}
			if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				drift = append(drift, rel)
				continue
			}
		}
		if scope.Excludes(rel) || isAllowedDriftSurface(rel) || diagnosticstorage.Recognizes(parent, root, rel) {
			continue
		}
		drift = append(drift, rel)
	}
	// Git can report a staged deletion and an untracked replacement under the
	// same name. First aggregate every status; only an exclusively untracked
	// temporary can be classified as metrics storage.
	remaining := drift[:0]
	for _, path := range drift {
		if normalUntracked[path] && exactStatusPath[path] && !frozen[path] && metrics.IsTemporaryPath(path) {
			recognized, err := metrics.RecognizeTemporary(parent, root, path)
			if err != nil {
				return nil, fmt.Errorf("%w: inspect metrics temporary: %w", errResultPacketDriftProof, err)
			}
			if recognized {
				continue
			}
		}
		remaining = append(remaining, path)
	}
	drift = remaining
	storagePaths, err := loopruntime.ClassifyPacketBodyStoragePathsContext(parent, root, drift)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errResultPacketDriftProof, err)
	}
	for _, path := range storagePaths {
		controlPaths[path] = true
	}
	remaining = drift[:0]
	for _, path := range drift {
		if !controlPaths[path] {
			remaining = append(remaining, path)
		}
	}
	drift = remaining
	if typedResultReceiptsEnabled(state) {
		paths, err := loopruntime.VerifyReviewResultPacketControlPathsContext(parent, authorityRoot, state, drift)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errResultPacketDriftProof, err)
		}
		for _, path := range paths {
			if strings.HasPrefix(path, ".loop-submissions/s7/") && (!normalUntracked[path] || frozen[path]) {
				continue
			}
			if err := workpacket.CheckPath(root, path); err != nil {
				return nil, fmt.Errorf("%w: %w", errResultPacketDriftProof, err)
			}
			controlPaths[path] = true
		}
		remaining := drift[:0]
		for _, path := range drift {
			if !controlPaths[path] {
				remaining = append(remaining, path)
			}
		}
		drift = remaining
	}
	// Independent Targeted has its own registered TASK/manifest authority;
	// proximity to a legitimate questionnaire grants no exception.
	if filepath.Clean(root) == filepath.Clean(authorityRoot) {
		paths, err := targetedauthoring.ControlPaths(authorityRoot, state, drift, parent)
		if err != nil {
			return nil, fmt.Errorf("Targeted original control source: %w", err)
		}
		allowed := map[string]bool{}
		for _, path := range paths {
			if normalUntracked[path] && !frozen[path] {
				if err := workpacket.CheckPath(root, path); err != nil {
					return nil, err
				}
				allowed[path] = true
			}
		}
		remaining := drift[:0]
		for _, path := range drift {
			if !allowed[path] {
				remaining = append(remaining, path)
			}
		}
		drift = remaining
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	sort.Strings(drift)
	return drift, nil
}

// normalizeGitStatusPath canonicalizes separators and dot segments without
// trimming whitespace, which may be part of a legitimate Git path.
func normalizeGitStatusPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = filepath.ToSlash(filepath.Clean(value))
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || filepath.IsAbs(value) {
		return ""
	}
	return value
}

func isAllowedDriftSurface(rel string) bool {
	if rel == ".claude" || isControlPlaneDriftPath(rel) {
		return true
	}
	if rel == projectlayout.Reports || strings.HasPrefix(rel, "docs/reports/") {
		return true
	}
	if rel == projectlayout.ReleaseAudits || strings.HasPrefix(rel, "docs/reports/release-audits/") {
		return true
	}
	// Audit, blueprint and other non-product projections are not frozen product
	// surfaces; their presence as untracked/modified files must not turn every
	// S7 round stale. Only product surfaces (internal/, cmd/, pkg/, api/, etc.)
	// are drift-relevant — docs/ as a whole is an allowed surface for the
	// dual-source check (the frozen_subjects allow-list still governs product).
	if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "blueprint/") || strings.HasPrefix(rel, "schema/") {
		return true
	}
	// git status --porcelain collapses a fully-untracked directory to its bare
	// name ("?? e2e-workspace/"); filepath.Clean strips the trailing slash, so the
	// bare form must be allowed too or the cold-start workspace itself turns the
	// round stale on first write.
	if rel == "e2e-workspace" || strings.HasPrefix(rel, "e2e-workspace/") {
		return true
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return true
	}
	return false
}

func isControlPlaneDriftPath(rel string) bool {
	if worktreeReminderPath.MatchString(rel) || mainReviewPlanAuthoringPath.MatchString(rel) || assignmentDispatchContextPath.MatchString(rel) {
		return true
	}
	// .claude/glm.json is a tracked but constantly-rewritten local model
	// config (model switching rewrites it); pinning or drift-flagging it
	// stales every round on an unrelated local toggle.
	if rel == ".claude/loop-state.json" || rel == ".claude/loop-events.jsonl" || rel == ".claude/loop-metrics.json" || rel == ".claude/settings.json" || rel == ".claude/settings.local.json" || rel == ".claude/glm.json" {
		return true
	}
	// Hook bookkeeping (hook-decisions.jsonl, transient hook-*.scratch files)
	// is control-plane: it changes on every hook evaluation and must never
	// stale a review round. A transient untracked hook scratch file was
	// observed staling an otherwise clean round mid-batch.
	if strings.HasPrefix(rel, ".claude/hook") {
		return true
	}
	// Runtime lock files (.claude/loop-state.json.lock) exist only for the
	// instant of a concurrent CAS write; catching them mid-scan stales a
	// clean round on a pure race.
	if strings.HasPrefix(rel, ".claude/") && (strings.HasSuffix(rel, ".lock") || strings.HasSuffix(rel, ".lock.process")) {
		return true
	}
	for _, prefix := range []string{".claude/review/", ".claude/evidence/", ".claude/workgroups/", ".claude/plans/", ".claude/bin/", ".claude/runtime-checkpoints/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// Matches the original workspace.DispatchContextSource output, not its directory.
var assignmentDispatchContextPath = regexp.MustCompile(`^\.claude/assignment-context/[a-f0-9]{64}/g[1-9][0-9]*/[A-Za-z0-9][A-Za-z0-9._-]*-e[1-9][0-9]*\.json$`)

var worktreeReminderPath = regexp.MustCompile(`^\.claude/worktree-reminders/[0-9a-f]{64}\.txt$`)
var mainReviewPlanAuthoringPath = regexp.MustCompile(`^\.claude/submissions/[A-Za-z0-9._-]+/g[1-9][0-9]*/S7/r[1-9][0-9]*/review-plan\.json$`)

// verifyResultArtifactDigest binds an E2E result to the workspace digest it
// actually ran against (L3-S7 §3.5): results from a workspace that has since
// drifted are stale, not submittable.
func verifyResultArtifactDigest(root string, plan *Plan, ptr *PlanPointer, result *Result, lens string) error {
	return verifyResultArtifactDigestAt(root, plan, ptr, result, lens)
}

// verifyResultArtifactDigestAt is the packet-backed variant: authority and
// the pinned plan remain in Main, while the mutable cold-start workspace may
// live in the Assignment's registered execution checkout.
func verifyResultArtifactDigestAt(workspaceRoot string, plan *Plan, ptr *PlanPointer, result *Result, lens string, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if lens != "e2e" {
		return nil
	}
	if ptr.VerificationArtifactWorkspace == "" {
		if result.VerificationArtifactDigest != nil && *result.VerificationArtifactDigest != "" {
			return fmt.Errorf("result binds a verification artifact digest but the plan declares no workspace")
		}
		return nil
	}
	digest, err := WorkspaceDigestContext(ctx, workspaceRoot, ptr.VerificationArtifactWorkspace)
	if err != nil {
		return err
	}
	if result.VerificationArtifactDigest == nil || *result.VerificationArtifactDigest == "" {
		return fmt.Errorf("E2E result is missing its system-bound verification_artifact_digest; Main/system must restore the original answer adapter against the actual execution workspace, without asking the author to calculate a digest")
	}
	if *result.VerificationArtifactDigest != digest {
		return fmt.Errorf("verification artifact digest mismatch: result binds %s but the workspace now digests to %s; re-run the flows against the current spec before submitting", *result.VerificationArtifactDigest, digest)
	}
	return nil
}

// verifyRegressionAssetFingerprints checks every selected reusable asset,
// including partial reuse in cold_start. Newly authored workspace assets are
// checked separately by WorkspaceDigest.
func verifyRegressionAssetFingerprints(root string, plan *Plan) error {
	for _, asset := range sortE2EAssets(plan.E2EAssets) {
		path, err := repositoryContainedPath(root, asset.Path)
		if err != nil {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s is outside the repository", asset.AssetID),
				[]string{err.Error()},
				[]string{"select the exact repository-relative CASE/PATH file in the original plan; the plan consumer binds its fingerprint"},
				"runtime review-plan --file plan.json",
			)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s cannot be read", asset.AssetID),
				[]string{err.Error()},
				[]string{"restore the asset or change the plan to cold_start and author a fresh verification workspace"},
				"runtime review-plan --file plan.json",
			)
		}
		if actual := sha256Of(data); actual != asset.SHA256 {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s fingerprint is stale", asset.AssetID),
				[]string{fmt.Sprintf("asset %s has sha256 %s, plan declares %s", asset.Path, actual, asset.SHA256)},
				[]string{"refresh the fingerprint from the current immutable asset, or switch to cold_start"},
				"runtime review-plan --file plan.json",
			)
		}
	}
	return nil
}

// verifySealedArtifactDigests re-checks every consumed E2E assignment's
// bound digest against the workspace at close time (L3-S7 §10.1.6, §10.3):
// a workspace file that a consumed result actually depended on and that
// changed (or disappeared) after consumption invalidates the round.
//
// L3-S7 §10.3 splits workspace movement in two: changing/removing a
// spec/fixture a consumed result bound invalidates that result honestly,
// while ADDING new cold-start artifacts is ordinary artifact-revision
// growth and must not stale the round. The whole-directory digest cannot
// tell those apart, so when a bound aggregate no longer matches the live
// digest this falls back to the result's own typed evidence anchors: every
// workspace-relative `path:…#sha256=…` ref the result pinned is re-verified
// against disk. If all of them still match, the consumed evidence is
// byte-valid (the aggregate moved only through later artifact revisions)
// and the assignment passes; any missing or changed anchor fails closed.
func verifySealedArtifactDigests(root string, ptr *PlanPointer, assignments map[string]any, state map[string]any, candidateAssignmentID, candidateExecutionRoot string, contexts ...context.Context) error {
	return verifySealedArtifactDigestsWithMode(root, ptr, assignments, state, candidateAssignmentID, candidateExecutionRoot, true, contexts...)
}

// VerifySealedArtifactDigests rechecks accepted packet-backed E2E Result
// receipts against their currently registered execution checkouts. It is
// shared by the quality gate and TR-009's direct transition guard. Legacy
// Result rows keep their original close-time validation path.
func VerifySealedArtifactDigests(root string, state map[string]any, ptr *PlanPointer, assignments map[string]any, contexts ...context.Context) error {
	ctx := loopruntime.WithReviewResultReadCache(buildsurface.WithReadCache(baselineContext(contexts)))
	if err := ctx.Err(); err != nil {
		return err
	}
	if state == nil || ptr == nil {
		return errors.New("typed S7 sealed artifact verification requires the accepted Runtime and current Plan")
	}
	currentPtr := PlanPointerFromState(state)
	if currentPtr == nil || *currentPtr != *ptr {
		return errors.New("typed S7 sealed artifact verification received a non-current Plan projection")
	}
	// The projection argument is retained for source compatibility, but the
	// accepted Runtime is the authority for which E2E rows the gate must check.
	// Ignoring caller-supplied projections prevents an empty/stale projection
	// from suppressing a packet-backed row.
	reviewState, _ := state["review"].(map[string]any)
	currentAssignments, _ := reviewState["assignments"].(map[string]any)
	if currentAssignments == nil {
		return errors.New("typed S7 sealed artifact verification has no current Assignment registry")
	}
	return verifySealedArtifactDigestsWithMode(root, ptr, currentAssignments, state, "", "", false, ctx)
}

func HasPacketBoundE2EReceipts(state map[string]any) bool {
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	for _, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row == nil || row["lens"] != "e2e" || workstate.ReviewAssignmentActivity(row) != "consumed" {
			continue
		}
		receipt, _ := row["result_receipt"].(map[string]any)
		if receipt != nil && stringField(receipt["packet_id"]) != "" {
			return true
		}
	}
	return false
}

func verifySealedArtifactDigestsWithMode(root string, ptr *PlanPointer, assignments map[string]any, state map[string]any, candidateAssignmentID, candidateExecutionRoot string, includeLegacy bool, contexts ...context.Context) error {
	ctx := loopruntime.WithReviewResultReadCache(buildsurface.WithReadCache(baselineContext(contexts)))
	if err := ctx.Err(); err != nil {
		return err
	}
	if ptr.VerificationArtifactWorkspace == "" {
		return nil
	}
	for id, raw := range assignments {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, _ := raw.(map[string]any)
		if row == nil || row["lens"] != "e2e" || workstate.ReviewAssignmentActivity(row) != "consumed" {
			continue
		}
		artifactRoot := root
		packetBound := false
		if id == candidateAssignmentID {
			if candidateExecutionRoot == "" {
				return fmt.Errorf("assignment %s has no authenticated execution root for seal-time verification", id)
			}
			artifactRoot = candidateExecutionRoot
			packetBound = true
		} else {
			receipt, _ := row["result_receipt"].(map[string]any)
			packetID := stringField(receipt["packet_id"])
			if packetID != "" {
				packetBound = true
				if state == nil {
					return fmt.Errorf("assignment %s packet receipt has no Runtime source for verification", id)
				}
				accepted, found, err := IssuedResultReceiptContext(ctx, root, state, packetID, stringField(row["agent_id"]))
				if err != nil {
					return fmt.Errorf("assignment %s original packet receipt: %w", id, err)
				}
				if !found || accepted.ExecutionRoot == "" {
					return fmt.Errorf("assignment %s has no accepted execution-root receipt", id)
				}
				artifactRoot = accepted.ExecutionRoot
			} else if !includeLegacy {
				continue
			}
		}
		if packetBound && !includeLegacy && state == nil {
			return fmt.Errorf("assignment %s packet receipt has no Runtime source for verification", id)
		}
		current, err := WorkspaceDigestContext(ctx, artifactRoot, ptr.VerificationArtifactWorkspace)
		if err != nil {
			return fmt.Errorf("assignment %s execution workspace digest: %w", id, err)
		}
		bound, _ := row["artifact_digest"].(string)
		if bound == current {
			continue
		}
		if bound == "" {
			return fmt.Errorf("assignment %s result_ref %s has no original artifact_digest for execution workspace %s; Main must restore the original machine binding, not infer dependencies from missing metadata", id, stringField(row["result_ref"]), filepath.Join(artifactRoot, ptr.VerificationArtifactWorkspace))
		}
		var plan *Plan
		if state != nil && bound != sha256Of(nil) {
			var err error
			plan, _, err = LoadPlan(root, state)
			if err != nil {
				return err
			}
		}
		if err := verifyResultWorkspaceAnchors(root, artifactRoot, ptr.VerificationArtifactWorkspace, id, row, plan); err != nil {
			return fmt.Errorf("assignment %s consumed a result against workspace digest %s, but the workspace now digests to %s; the round is stale (L3-S7 §10.3): %w", id, bound, current, err)
		}
	}
	return ctx.Err()
}

// verifyResultWorkspaceAnchors re-verifies the dependencies declared in the
// original result. Missing declarations are unknown, never proof that a result
// did not use the workspace. Historical receipts retain their original bytes.
func verifyResultWorkspaceAnchors(root, executionRoot, workspaceRel, assignmentID string, row map[string]any, plans ...*Plan) error {
	resultRel, _ := row["result_ref"].(string)
	if resultRel == "" {
		return fmt.Errorf("consumed row has no result_ref; its workspace anchors cannot be re-verified")
	}
	data, err := workpacket.ReadFile(root, resultRel, resultReceiptReadLimit)
	if err != nil {
		return fmt.Errorf("read consumed result %s: %w", resultRel, err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode consumed result %s: %w", resultRel, err)
	}
	count, err := verifyWorkspaceDependencyRefs(executionRoot, workspaceRel, &result)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	// The empty-workspace hash is an existing machine proof of no files at
	// consumption. Later additions cannot have changed a dependency from it.
	if stringField(row["artifact_digest"]) == sha256Of(nil) {
		return nil
	}
	for _, plan := range plans {
		if resultUsesBoundReuseAssets(plan, &result) {
			return verifyRegressionAssetFingerprints(executionRoot, plan)
		}
	}
	return missingWorkspaceDependencies(executionRoot, workspaceRel, assignmentID, resultRel)
}

// The author names actual scripts/fixtures in the existing claim/check refs;
// bindIssuedPathEvidence supplies hashes from the authenticated execution root.
// Command output establishes observations, not the script/fixture dependency.
func verifyWorkspaceDependencyRefs(executionRoot, workspaceRel string, result *Result) (int, error) {
	workspaceAbs, err := repositoryContainedPath(executionRoot, workspaceRel)
	if err != nil {
		return 0, fmt.Errorf("resolve registered execution workspace: %w", err)
	}
	var groups [][]string
	for _, claim := range result.ClaimResults {
		groups = append(groups, claim.EvidenceRefs)
	}
	for _, check := range result.Checks {
		groups = append(groups, check.EvidenceRefs)
	}
	count := 0
	for _, refs := range groups {
		for _, ref := range refs {
			if !strings.HasPrefix(ref, "path:") {
				continue
			}
			rel, digest, err := parsePathEvidenceRef(ref)
			if err != nil {
				return count, err
			}
			// Resolve containment before reading, including symlink protection.
			abs, err := repositoryContainedPath(executionRoot, rel)
			if err != nil {
				return count, err
			}
			if !strings.HasPrefix(abs, workspaceAbs+string(filepath.Separator)) {
				continue
			}
			if digest == "" {
				return count, fmt.Errorf("workspace dependency %s has no original bound digest; restore the original result consumer, do not author a hash", ref)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return count, fmt.Errorf("anchor %s no longer resolvable: %w", ref, err)
			}
			if got := sha256Of(data); got != digest {
				return count, fmt.Errorf("anchor %s changed after consumption (now %s); restore the original asset bytes and retry the original seal; if restoration is impossible, Main must resolve this affected accepted result through original round recovery before reusing PASS (runtime review-plan revise requires a legal round status, real --source-ref/--affected-surface and an actual Claim change; metadata gaps alone do not authorize a revision)", ref, got)
			}
			count++
		}
	}
	return count, nil
}

// A selected reusable asset is already bound by the original Plan consumer.
// Require coverage for every executed Claim before using those existing
// bindings as proof; a partial reuse declaration does not explain other work.
func resultUsesBoundReuseAssets(plan *Plan, result *Result) bool {
	if plan == nil || len(result.ClaimResults) == 0 {
		return false
	}
	boundTargets := map[string]bool{}
	for _, asset := range plan.E2EAssets {
		if _, _, err := caseReferenceParts(asset.CaseRef); err == nil && asset.Path != "" && asset.SHA256 != "" {
			boundTargets[asset.CaseRef] = true
		}
	}
	coveredClaims := map[string]bool{}
	for _, claim := range plan.Claims {
		declared, covered := false, true
		for _, ref := range claim.SourceRefs {
			if _, _, err := caseReferenceParts(ref); err != nil {
				continue
			}
			declared = true
			covered = covered && boundTargets[ref]
		}
		coveredClaims[claim.ClaimID] = declared && covered
	}
	for _, executed := range result.ClaimResults {
		if !coveredClaims[executed.ClaimID] {
			return false
		}
	}
	return true
}

func missingWorkspaceDependencies(executionRoot, workspaceRel, assignmentID, resultRel string) error {
	return fmt.Errorf("assignment %s result %s has unknown script/fixture dependencies in execution workspace %s: claim_results[].evidence_refs or checks[].evidence_refs must name the actual used paths (path:<repository-relative-script-or-fixture>); execution logs do not replace dependency refs. If original dependency bytes are known and can be restored, restore them and retry the original seal. Otherwise hand this missing proof to Main for original round recovery before reusing PASS; accepted results stay immutable. runtime review-plan revise requires a legal round status, real --source-ref/--affected-surface and an actual Claim change; do not invent a Claim change for this metadata gap. For a new original answer, name actual used scripts/fixtures in those evidence_refs and let the original answer consumer bind hashes", assignmentID, resultRel, filepath.Join(executionRoot, filepath.FromSlash(workspaceRel)))
}

// projectedAssignments returns the assignment projection as it will look
// after this result is consumed (current assignment consumed + digest bound).
func projectedAssignments(state map[string]any, currentAssignment *PlanAssignment, result *Result) map[string]any {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	out := map[string]any{}
	for id, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row != nil {
			out[id] = row
		}
	}
	digest := ""
	if result.VerificationArtifactDigest != nil {
		digest = *result.VerificationArtifactDigest
	}
	runtimeID := stringField(state["runtime_id"])
	resultRef := filepath.ToSlash(filepath.Join(".claude", "evidence", runtimeID, fmt.Sprintf("g%d", baselineGeneration(state)), "reviews", result.ProducerAgentID, result.ResultID+".json"))
	out[currentAssignment.AssignmentID] = map[string]any{
		"lens": currentAssignment.Lens, "status": "consumed", "result_ref": resultRef,
		"agent_id": result.ProducerAgentID, "artifact_digest": digest,
	}
	return out
}

// ---------------------------------------------------------------------------
// Capture buffer (L3-S7 §3.6 auto-capture): `loop-harness capture step`
// appends one sanitized timeline step per call; submit merges them into
// findings whose encounter timeline is empty.
// ---------------------------------------------------------------------------

// secretPatterns are the redaction gate: any capture field matching one is
// rejected — the buffer never persists secrets (L3-S7 §6.3).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*[:=]`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]`),
	regexp.MustCompile(`(?i)secret\s*[:=]`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{12,}`),
	regexp.MustCompile(`(?i)token\s*[:=]\s*[A-Za-z0-9._-]{8,}`),
}

// CaptureStep is one sanitized timeline step.
type CaptureStep struct {
	Sequence   int      `json:"sequence"`
	FindingID  string   `json:"finding_id,omitempty"`
	ClaimID    string   `json:"claim_id,omitempty"`
	Action     string   `json:"action"`
	Observed   string   `json:"observed"`
	Evidence   []string `json:"evidence_refs,omitempty"`
	CapturedAt string   `json:"captured_at"`
	// Provenance is populated by capture exec. It is optional so legacy
	// hand-authored `capture step` buffers remain readable and mergeable.
	Provenance *CaptureProvenance `json:"provenance,omitempty"`
}

// SanitizeCapture rejects any field that smells like a secret.
func SanitizeCapture(step CaptureStep) error {
	fields := map[string]string{
		"action":   step.Action,
		"observed": step.Observed,
	}
	for index, ref := range step.Evidence {
		fields[fmt.Sprintf("evidence_refs[%d]", index)] = ref
	}
	for name, value := range fields {
		for _, pattern := range secretPatterns {
			if pattern.MatchString(value) {
				return fmt.Errorf("capture field %q matches a secret pattern (%s); record the redacted ref, not the value (L3-S7 §6.3)", name, pattern.String())
			}
		}
	}
	return nil
}

// CaptureFile returns the buffer path for one assignment.
func CaptureFile(root, runtimeID string, generation int, assignmentID string) string {
	return filepath.Join(root, ".claude", "evidence", runtimeID,
		fmt.Sprintf("g%d", generation), "captures", assignmentID, "steps.jsonl")
}

// LoadCaptureSteps is the compatibility helper used by read-only capture
// inspectors. Submit paths must use LoadCaptureStepsStrict so malformed lines
// cannot silently disappear from S8 evidence.
func LoadCaptureSteps(path string) []CaptureStep {
	steps, _ := LoadCaptureStepsStrict(path)
	return steps
}

// LoadCaptureStepsStrict reads the buffer and reports the first malformed
// JSONL line with its line number. A missing buffer is empty, not an error.
func LoadCaptureStepsStrict(path string) ([]CaptureStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read capture buffer %s: %w", path, err)
	}
	var steps []CaptureStep
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var step CaptureStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			return nil, fmt.Errorf("capture buffer %s line %d is malformed JSON: %w", path, lineNumber+1, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// mergeCapturedTimeline fills an empty encounter timeline from the buffer.
// Findings whose Reviewer wrote a real timeline are never rewritten.
func mergeCapturedTimeline(findings []Finding, steps []CaptureStep) {
	if len(steps) == 0 {
		return
	}
	timeline := make([]TimelineStep, 0, len(steps))
	for _, step := range steps {
		timeline = append(timeline, TimelineStep{
			Sequence:           step.Sequence,
			Action:             step.Action,
			ObservedCheckpoint: step.Observed,
			EvidenceRefs:       step.Evidence,
		})
	}
	for i := range findings {
		if len(findings[i].Encounter.Timeline) == 0 {
			findings[i].Encounter.Timeline = timeline
		}
	}
}

// mergeCapturedTimelineChecked is the submit-time variant. Assignment-wide
// capture is safe only when there is one Finding. Multiple Findings require a
// finding_id or claim_id on every step so the same wall is not copied into
// unrelated investigation cases.
func mergeCapturedTimelineChecked(findings []Finding, steps []CaptureStep) error {
	if len(steps) == 0 || len(findings) == 0 {
		return nil
	}
	if len(findings) == 1 {
		for _, step := range steps {
			if step.FindingID != "" && step.FindingID != findings[0].FindingID ||
				step.ClaimID != "" && step.ClaimID != findings[0].ClaimID {
				return fmt.Errorf("capture step %d is correlated to finding=%s claim=%s, outside the submitted finding set", step.Sequence, step.FindingID, step.ClaimID)
			}
		}
		mergeCapturedTimeline(findings, steps)
		return nil
	}

	byFinding := make(map[string]int, len(findings))
	byClaim := make(map[string]int, len(findings))
	for index, finding := range findings {
		byFinding[finding.FindingID] = index
		byClaim[finding.ClaimID] = index
	}
	grouped := make(map[int][]CaptureStep)
	for _, step := range steps {
		index := -1
		if step.FindingID != "" && step.ClaimID != "" {
			findingIndex, findingOK := byFinding[step.FindingID]
			claimIndex, claimOK := byClaim[step.ClaimID]
			if !findingOK || !claimOK || findingIndex != claimIndex {
				return fmt.Errorf("capture step %d correlation conflict: finding_id=%s and claim_id=%s do not identify the same submitted Finding", step.Sequence, step.FindingID, step.ClaimID)
			}
		}
		if step.FindingID != "" {
			if candidate, ok := byFinding[step.FindingID]; ok {
				index = candidate
			}
		}
		if index < 0 && step.ClaimID != "" {
			if candidate, ok := byClaim[step.ClaimID]; ok {
				index = candidate
			}
		}
		if index < 0 {
			return fmt.Errorf("capture timeline is ambiguous for %d findings: step %d has no finding_id or claim_id", len(findings), step.Sequence)
		}
		grouped[index] = append(grouped[index], step)
	}
	for index, group := range grouped {
		if len(findings[index].Encounter.Timeline) == 0 {
			mergeCapturedTimeline(findings[index:index+1], group)
		}
	}
	return nil
}

// verifyCanonicalFrozenWorktree complements a canonical declaration freshly
// verified in the same synchronous validation with live disk and drift checks.
func verifyCanonicalFrozenWorktree(root string, plan *Plan, state map[string]any, contexts ...context.Context) error {
	if err := verifyCanonicalFrozenSubjectFiles(root, plan, contexts...); err != nil {
		return err
	}
	drift, err := detectUndeclaredProductDriftWithState(root, plan, state, root, contexts...)
	if err != nil {
		return fmt.Errorf("canonical frozen baseline drift scan: %w", err)
	}
	if len(drift) > 0 {
		return fmt.Errorf("frozen baseline drift: undeclared product file(s) %s outside frozen_subjects; revert or include them in a newly authorized review round", strings.Join(drift, ", "))
	}
	return nil
}
